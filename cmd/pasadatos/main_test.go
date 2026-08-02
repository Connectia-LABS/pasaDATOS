package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRunningDesktopInstanceReadsVersion(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service":      "pasaDATOS",
			"version":      "1.0.0",
			"desktop_mode": true,
		})
	}))
	defer server.Close()

	info, running := runningDesktopInstance(server.URL)
	if !running {
		t.Fatal("se esperaba detectar una instancia de escritorio")
	}
	if info.Version != "1.0.0" {
		t.Fatalf("versión inesperada: %q", info.Version)
	}
}

func TestStopExistingInstanceUsesNativeToken(t *testing.T) {
	t.Parallel()
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/native/exit" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-pasaDATOS-Native"); got != "token-prueba" {
			t.Fatalf("token inesperado: %q", got)
		}
		called.Store(true)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	if err := stopExistingInstance(server.URL, "token-prueba"); err != nil {
		t.Fatalf("stopExistingInstance devolvió error: %v", err)
	}
	if !called.Load() {
		t.Fatal("no se invocó el cierre de la instancia anterior")
	}
}
