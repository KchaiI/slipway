package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/KchaiI/slipway/internal/api"
	"github.com/KchaiI/slipway/internal/deploy"
	"github.com/KchaiI/slipway/internal/release"
)

const rolloutTimeout = 2 * time.Minute

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req api.CreateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: %v", err)
		return
	}
	if err := deploy.ValidateAppName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	ctx := r.Context()
	if exists, err := s.deployer.AppExists(ctx, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	} else if exists {
		writeError(w, http.StatusConflict, "app %q already exists", req.Name)
		return
	}
	if err := s.createApp(ctx, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "create app: %v", err)
		return
	}
	writeJSON(w, http.StatusCreated, api.App{
		Name:   req.Name,
		URL:    s.appURL(req.Name),
		GitURL: s.gitURL(req.Name),
	})
}

// createApp provisions everything a fresh app needs.
func (s *Server) createApp(ctx context.Context, app string) error {
	if err := s.deployer.EnsureNamespace(ctx, app); err != nil {
		return err
	}
	if err := s.releases.Init(ctx, deploy.Namespace(app)); err != nil {
		return err
	}
	return s.initRepo(app)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	names, err := s.deployer.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	apps := make([]api.App, 0, len(names))
	for _, n := range names {
		apps = append(apps, api.App{Name: n, URL: s.appURL(n), GitURL: s.gitURL(n)})
	}
	writeJSON(w, http.StatusOK, apps)
}

// requireApp validates the {app} path segment and confirms the app exists.
func (s *Server) requireApp(w http.ResponseWriter, r *http.Request) (string, bool) {
	app := r.PathValue("app")
	if err := deploy.ValidateAppName(app); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return "", false
	}
	exists, err := s.deployer.AppExists(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return "", false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "app %q not found", app)
		return "", false
	}
	return app, true
}

func (s *Server) handleAppStatus(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	st, err := s.deployer.Status(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	st.GitURL = s.gitURL(app)
	if h, err := s.releases.Load(r.Context(), deploy.Namespace(app)); err == nil {
		st.Release = h.Live()
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDestroyApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	if err := s.deployer.DeleteNamespace(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := s.deleteRepo(app); err != nil {
		writeError(w, http.StatusInternalServerError, "delete repository: %v", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	h, err := s.releases.Load(r.Context(), deploy.Namespace(app))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, h.Releases)
}

// handleDeployImage deploys a prebuilt image as a new release. This is the
// non-git deploy path, used by tests and as the building block for the git
// pipeline.
func (s *Server) handleDeployImage(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	var req api.DeployImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		writeError(w, http.StatusBadRequest, "body must be {\"image\": \"...\"}")
		return
	}
	rel, err := s.deployRelease(r.Context(), app, req.Image, "",
		fmt.Sprintf("deploy image %s", req.Image))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}

// deployRelease allocates a release for an image, rolls it out, and records
// the outcome. Deploys of the same app are serialized.
func (s *Server) deployRelease(ctx context.Context, app, image, commit, description string) (*api.Release, error) {
	mu := s.lock(app)
	mu.Lock()
	defer mu.Unlock()

	ns := deploy.Namespace(app)
	rel, err := s.releases.Allocate(ctx, ns, image, commit, description, release.StatusBuilding)
	if err != nil {
		return nil, err
	}
	version := fmt.Sprintf("v%d", rel.Version)

	if err := s.deployer.Apply(ctx, app, image, version); err != nil {
		_ = s.releases.SetStatus(ctx, ns, rel.Version, release.StatusFailed)
		return nil, err
	}
	if err := s.deployer.WaitRollout(ctx, app, rolloutTimeout); err != nil {
		_ = s.releases.SetStatus(ctx, ns, rel.Version, release.StatusFailed)
		return nil, errors.Join(fmt.Errorf("release %s failed", version), err)
	}
	if err := s.releases.SetStatus(ctx, ns, rel.Version, release.StatusLive); err != nil {
		return nil, err
	}
	rel.Status = release.StatusLive
	return rel, nil
}
