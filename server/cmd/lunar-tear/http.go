package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"lunar-tear/server/internal/service"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type httpServerConfig struct {
	Host             string
	Port             int
	ResourcesBaseURL string
	AssetsReady      bool
}

type healthStatus struct {
	OK          bool   `json:"ok"`
	Host        string `json:"host"`
	HTTPPort    int    `json:"httpPort"`
	GRPCPort    int    `json:"grpcPort"`
	AssetsReady bool   `json:"assetsReady"`
}

func newHTTPHandler(cfg httpServerConfig, h2s *http2.Server) http.Handler {
	octoServer := service.NewOctoHTTPServer(cfg.ResourcesBaseURL)
	octoHandler := h2c.NewHandler(octoServer.Handler(), h2s)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthStatus{
			OK:          true,
			Host:        cfg.Host,
			HTTPPort:    cfg.Port,
			GRPCPort:    443,
			AssetsReady: cfg.AssetsReady,
		})
	})
	mux.Handle("/", octoHandler)
	return mux
}

func startHTTP(cfg httpServerConfig) {
	h2s := &http2.Server{}
	log.Printf("Octo HTTP server listening on :%d (HTTP/1.1 + h2c)", cfg.Port)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: newHTTPHandler(cfg, h2s),
	}
	http2.ConfigureServer(srv, h2s)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("HTTP server on %d failed: %v", cfg.Port, err)
	}
}
