// Package server implements minato-server's HTTP surface: the REST API used
// by the CLI and (in later milestones) the Git smart HTTP receiver.
package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/KchaiI/slipway/internal/api"
	"github.com/KchaiI/slipway/internal/deploy"
	"github.com/KchaiI/slipway/internal/release"
)

// Config carries the server's runtime configuration.
type Config struct {
	Domain      string // apps are exposed as <app>.<Domain>
	IngressPort int    // host port that reaches ingress (URL rendering)
	ServerHost  string // hostname of minato-server itself (git remote URLs)
	DataDir     string // persistent dir for git repos and build contexts
}

// Server wires the HTTP handlers to the Kubernetes-facing components.
type Server struct {
	cfg      Config
	client   kubernetes.Interface
	deployer *deploy.Deployer
	releases *release.Store

	locks sync.Map // app name -> *sync.Mutex, serializes deploys per app
}

func New(cfg Config, client kubernetes.Interface) *Server {
	return &Server{
		cfg:      cfg,
		client:   client,
		deployer: deploy.NewDeployer(client, cfg.Domain, cfg.IngressPort),
		releases: release.NewStore(client),
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /v1/apps", s.handleCreateApp)
	mux.HandleFunc("GET /v1/apps", s.handleListApps)
	mux.HandleFunc("GET /v1/apps/{app}", s.handleAppStatus)
	mux.HandleFunc("DELETE /v1/apps/{app}", s.handleDestroyApp)
	mux.HandleFunc("POST /v1/apps/{app}/deployments", s.handleDeployImage)
	mux.HandleFunc("GET /v1/apps/{app}/releases", s.handleReleases)

	return logRequests(mux)
}

// lock returns the per-app mutex, creating it on first use.
func (s *Server) lock(app string) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(app, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (s *Server) appURL(app string) string { return s.deployer.URL(app) }

func (s *Server) gitURL(app string) string {
	if s.cfg.IngressPort == 80 {
		return fmt.Sprintf("http://%s.%s/git/%s.git", s.cfg.ServerHost, s.cfg.Domain, app)
	}
	return fmt.Sprintf("http://%s.%s:%d/git/%s.git", s.cfg.ServerHost, s.cfg.Domain, s.cfg.IngressPort, app)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, api.Error{Error: fmt.Sprintf(format, args...)})
}
