package server

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KchaiI/slipway/internal/deploy"
)

// Git smart HTTP receiver. Only the push half of the protocol
// (git-receive-pack) is implemented; minato repos are a deploy target, not a
// code host. The post-receive hook installed in every repo POSTs back to
// /internal/hooks/post-receive, which runs the build/deploy pipeline and
// streams progress; the hook writes that stream to stderr, which
// git-receive-pack forwards to the pusher as "remote:" lines.
const postReceiveHook = `#!/bin/sh
# Installed by minato. Runs the build/deploy pipeline and relays its progress
# to the pusher via stderr (sideband channel 2).
exec curl -fsSN --max-time 600 --data-binary @- \
  "http://127.0.0.1:8080/internal/hooks/post-receive?app=%s" 1>&2
`

func (s *Server) repoPath(app string) string {
	return filepath.Join(s.cfg.DataDir, "repos", app+".git")
}

func (s *Server) contextDir(app string) string {
	return filepath.Join(s.cfg.DataDir, "contexts", app)
}

// initRepo creates the app's bare repository with the pipeline hook installed.
func (s *Server) initRepo(app string) error {
	repo := s.repoPath(app)
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		return err
	}
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", repo).CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %v: %s", err, out)
	}
	hook := fmt.Sprintf(postReceiveHook, app)
	return os.WriteFile(filepath.Join(repo, "hooks", "post-receive"), []byte(hook), 0o755)
}

func (s *Server) deleteRepo(app string) error {
	if err := os.RemoveAll(s.repoPath(app)); err != nil {
		return err
	}
	return os.RemoveAll(s.contextDir(app))
}

// requireRepo resolves the {repo} path segment ("<app>.git") and ensures the
// repository exists.
func (s *Server) requireRepo(w http.ResponseWriter, r *http.Request) (string, bool) {
	app := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if err := deploy.ValidateAppName(app); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return "", false
	}
	if _, err := os.Stat(s.repoPath(app)); err != nil {
		writeError(w, http.StatusNotFound, "app %q not found (create it with: minato apps create %s)", app, app)
		return "", false
	}
	return app, true
}

// handleInfoRefs implements GET /git/{app}.git/info/refs (ref advertisement).
func (s *Server) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireRepo(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("service") != "git-receive-pack" {
		writeError(w, http.StatusForbidden, "only git push is supported by minato remotes")
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache")
	// pkt-line service announcement followed by a flush packet.
	fmt.Fprintf(w, "%04x# service=git-receive-pack\n0000", 4+len("# service=git-receive-pack\n"))

	cmd := exec.CommandContext(r.Context(), "git", "receive-pack", "--stateless-rpc", "--advertise-refs", s.repoPath(app))
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		writeError(w, http.StatusInternalServerError, "advertise refs: %v", err)
	}
}

// handleReceivePack implements POST /git/{app}.git/git-receive-pack (the push
// itself). The build/deploy pipeline runs inside the repo's post-receive hook
// before this handler returns.
func (s *Server) handleReceivePack(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireRepo(w, r)
	if !ok {
		return
	}
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad gzip body: %v", err)
			return
		}
		defer gz.Close()
		body = gz
	}

	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.Header().Set("Cache-Control", "no-cache")

	cmd := exec.CommandContext(r.Context(), "git", "receive-pack", "--stateless-rpc", s.repoPath(app))
	cmd.Stdin = body
	cmd.Stdout = &flushWriter{w: w}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Headers are already sent; all we can do is log.
		fmt.Fprintf(os.Stderr, "receive-pack for %s: %v\n", app, err)
	}
}

// handleContext serves build context tarballs to build-job init containers.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	file := r.PathValue("file")
	if deploy.ValidateAppName(app) != nil || file != filepath.Base(file) {
		writeError(w, http.StatusBadRequest, "bad context path")
		return
	}
	http.ServeFile(w, r, filepath.Join(s.contextDir(app), file))
}

// flushWriter flushes after every write so protocol packets and sideband
// progress reach the git client immediately.
type flushWriter struct{ w http.ResponseWriter }

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}
