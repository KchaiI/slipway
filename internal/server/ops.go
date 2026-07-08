package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/KchaiI/slipway/internal/api"
	"github.com/KchaiI/slipway/internal/deploy"
	"github.com/KchaiI/slipway/internal/release"
)

func (s *Server) handleScale(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	var req api.ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"web\": <replicas>}")
		return
	}
	if req.Web < 0 || req.Web > 20 {
		writeError(w, http.StatusBadRequest, "replicas must be between 0 and 20")
		return
	}
	if err := s.deployer.Scale(r.Context(), app, req.Web, 3*time.Minute); err != nil {
		writeError(w, http.StatusInternalServerError, "scale: %v", err)
		return
	}
	st, err := s.deployer.Status(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleRollback redeploys the previous (superseded) release as a brand-new
// release version, Heroku style.
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	mu := s.lock(app)
	mu.Lock()
	defer mu.Unlock()

	ns := deploy.Namespace(app)
	h, err := s.releases.Load(r.Context(), ns)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if h.Live() == nil {
		writeError(w, http.StatusConflict, "nothing deployed yet")
		return
	}
	prev := h.Previous()
	if prev == nil {
		writeError(w, http.StatusConflict, "no previous release to roll back to")
		return
	}

	rel, err := s.releases.Allocate(r.Context(), ns, prev.Image, prev.Commit,
		fmt.Sprintf("rollback to v%d", prev.Version), release.StatusBuilding)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	rel, err = s.rolloutRelease(r.Context(), app, rel, prev.Image, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rollback: %v", err)
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}
