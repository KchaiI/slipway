package server

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KchaiI/slipway/internal/api"
	"github.com/KchaiI/slipway/internal/build"
	"github.com/KchaiI/slipway/internal/deploy"
	"github.com/KchaiI/slipway/internal/release"
)

const pipelineTimeout = 8 * time.Minute

// handlePostReceive is called by the post-receive hook of an app repository.
// The request body carries the hook's stdin ("old new ref" lines); the
// response body is streamed back to the pusher as remote output.
func (s *Server) handlePostReceive(w http.ResponseWriter, r *http.Request) {
	app := r.URL.Query().Get("app")
	if deploy.ValidateAppName(app) != nil {
		writeError(w, http.StatusBadRequest, "bad app")
		return
	}
	sha, ref := pickPushedRef(r.Body)

	pw := newProgressWriter(w)
	if sha == "" {
		pw.Println("minato: no branch update in this push; nothing to deploy")
		return
	}

	// The pipeline outlives the push connection on purpose: an interrupted
	// `git push` must not leave a half-deployed release behind.
	ctx, cancel := context.WithTimeout(context.Background(), pipelineTimeout)
	defer cancel()
	if err := s.buildAndDeploy(ctx, app, sha, ref, pw.Println); err != nil {
		pw.Printf("!      Deploy of %s failed: %v", shortSHA(sha), err)
		pw.Printf("!      The previous release keeps serving traffic.")
	}
}

// pickPushedRef selects which pushed branch to deploy: refs/heads/main if
// present, otherwise the first updated branch.
func pickPushedRef(body interface{ Read([]byte) (int, error) }) (sha, ref string) {
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "refs/heads/") {
			continue
		}
		if strings.Trim(fields[1], "0") == "" { // branch deletion
			continue
		}
		if ref == "" || fields[2] == "refs/heads/main" {
			sha, ref = fields[1], fields[2]
		}
	}
	return sha, ref
}

// buildAndDeploy runs the full pipeline for one pushed commit:
// release allocation -> context export -> Kaniko build -> rollout.
func (s *Server) buildAndDeploy(ctx context.Context, app, sha, ref string, progress func(string)) error {
	mu := s.lock(app)
	mu.Lock()
	defer mu.Unlock()

	ns := deploy.Namespace(app)
	branch := strings.TrimPrefix(ref, "refs/heads/")
	rel, err := s.releases.Allocate(ctx, ns, "", shortSHA(sha),
		fmt.Sprintf("deploy %s (%s)", shortSHA(sha), branch), release.StatusBuilding)
	if err != nil {
		return err
	}
	image := s.builder.ImageRef(app, rel.Version)
	if err := s.releases.SetImage(ctx, ns, rel.Version, image); err != nil {
		return err
	}

	failed := func(err error) error {
		_ = s.releases.SetStatus(ctx, ns, rel.Version, release.StatusFailed)
		return err
	}

	progress(fmt.Sprintf("-----> Building %s v%d from %s (%s)", app, rel.Version, shortSHA(sha), branch))
	if err := build.PrepareContext(s.repoPath(app), sha, s.builder.ContextPath(app, rel.Version)); err != nil {
		return failed(err)
	}
	if err := s.builder.Run(ctx, app, rel.Version, progress); err != nil {
		return failed(err)
	}

	progress(fmt.Sprintf("-----> Deploying v%d ...", rel.Version))
	if _, err := s.rolloutRelease(ctx, app, rel, image, progress); err != nil {
		return err
	}
	progress(fmt.Sprintf("-----> %s is live!", s.appURL(app)))
	return nil
}

// rolloutRelease applies the app resources for a release, waits for the
// rollout, and finalizes the release status.
func (s *Server) rolloutRelease(ctx context.Context, app string, rel *api.Release, image string, progress func(string)) (*api.Release, error) {
	ns := deploy.Namespace(app)
	version := fmt.Sprintf("v%d", rel.Version)

	failed := func(err error) (*api.Release, error) {
		_ = s.releases.SetStatus(ctx, ns, rel.Version, release.StatusFailed)
		return nil, err
	}
	if err := s.deployer.Apply(ctx, app, image, version); err != nil {
		return failed(err)
	}
	if err := s.deployer.WaitRollout(ctx, app, rolloutTimeout); err != nil {
		return failed(err)
	}
	if err := s.releases.SetStatus(ctx, ns, rel.Version, release.StatusLive); err != nil {
		return nil, err
	}
	rel.Status = release.StatusLive
	rel.Image = image
	if progress != nil {
		progress(fmt.Sprintf("       v%d is running (%s)", rel.Version, image))
	}
	return rel, nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// progressWriter streams human-readable lines over a chunked HTTP response.
type progressWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
}

func newProgressWriter(w http.ResponseWriter) *progressWriter {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	return &progressWriter{w: w}
}

func (p *progressWriter) Println(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(p.w, line)
	if fl, ok := p.w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (p *progressWriter) Printf(format string, args ...any) {
	p.Println(fmt.Sprintf(format, args...))
}
