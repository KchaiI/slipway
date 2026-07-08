// minato-server is the control plane of the minato PaaS. It exposes the REST
// API consumed by the minato CLI and orchestrates all Kubernetes resources.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/KchaiI/slipway/internal/kube"
	"github.com/KchaiI/slipway/internal/server"
)

func main() {
	cfg := server.Config{
		Domain:      envOr("MINATO_DOMAIN", "localtest.me"),
		IngressPort: envIntOr("MINATO_INGRESS_PORT", 80),
		ServerHost:  envOr("MINATO_SERVER_HOST", "minato"),
		DataDir:     envOr("MINATO_DATA_DIR", "/data"),
	}
	listen := envOr("MINATO_LISTEN", ":8080")

	client, err := kube.NewClient()
	if err != nil {
		slog.Error("kubernetes client init failed", "err", err)
		os.Exit(1)
	}

	srv := server.New(cfg, client)
	slog.Info("minato-server listening", "addr", listen, "domain", cfg.Domain)
	if err := http.ListenAndServe(listen, srv.Handler()); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
