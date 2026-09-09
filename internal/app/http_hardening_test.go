package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPAdmissionReturnsServiceUnavailableAtCapacity(t *testing.T) {
	h := &HTTPGateway{ctx: context.Background(), peers: map[string]*httpPeer{}}
	for i := 0; i < 64; i++ {
		h.peers[string(rune(i))] = &httpPeer{}
	}
	called := false
	handler := h.admitMCP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	response := httptest.NewRecorder()
	handler(response, req)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d retry=%q", response.Code, response.Header().Get("Retry-After"))
	}
	if called {
		t.Fatal("over-capacity request reached MCP transport")
	}
}
