package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	var connected atomic.Bool
	h := healthHandler(&connected)

	// Not connected → 503.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disconnected: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// Connected → 200.
	connected.Store(true)
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("connected: got %d, want %d", rec.Code, http.StatusOK)
	}
}
