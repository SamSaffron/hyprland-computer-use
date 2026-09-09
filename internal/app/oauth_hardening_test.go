package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOAuthRegistrationRateLimit(t *testing.T) {
	provider, err := newOAuthProvider("https://example.test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	provider.routes(mux)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/register", strings.NewReader("{"))
		req.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("request %d: got %d", i+1, response.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/register", strings.NewReader("{"))
	req.RemoteAddr = "192.0.2.10:9999" // Source ports must not create a new budget.
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit response: status=%d retry=%q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestOAuthRegistryRejectsUnsafeExistingFile(t *testing.T) {
	data := t.TempDir()
	path := filepath.Join(data, "oauth-clients.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newOAuthProvider("https://example.test", data); err == nil {
		t.Fatal("loaded a group/world-readable OAuth registry")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newOAuthProvider("https://example.test", data); err == nil {
		t.Fatal("loaded a null OAuth registry")
	}
}
