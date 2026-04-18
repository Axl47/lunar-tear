package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"
)

func TestHealthz(t *testing.T) {
	handler := newHTTPHandler(httpServerConfig{
		Host:             "192.168.4.21",
		Port:             8080,
		ResourcesBaseURL: "",
		AssetsReady:      true,
	}, &http2.Server{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status healthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode health response: %v", err)
	}

	if !status.OK {
		t.Fatal("expected ok=true")
	}
	if status.Host != "192.168.4.21" {
		t.Fatalf("unexpected host %q", status.Host)
	}
	if status.HTTPPort != 8080 {
		t.Fatalf("unexpected httpPort %d", status.HTTPPort)
	}
	if status.GRPCPort != 443 {
		t.Fatalf("unexpected grpcPort %d", status.GRPCPort)
	}
	if !status.AssetsReady {
		t.Fatal("expected assetsReady=true")
	}
}
