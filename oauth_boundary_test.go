package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func (f *oauthFixture) initializeHTTP(t *testing.T, token string) string {
	t.Helper()
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
	status, h, raw := f.request(t, "POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"boundary-test","version":"1"}}}`), headers)
	if status != 200 {
		t.Fatalf("initialize %d %s", status, raw)
	}
	id := h.Get("Mcp-Session-Id")
	if id == "" {
		t.Fatal("missing session ID")
	}
	headers["Mcp-Session-Id"] = id
	headers["MCP-Protocol-Version"] = "2025-11-25"
	status, _, raw = f.request(t, "POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), headers)
	if status != 202 {
		t.Fatalf("initialized %d %s", status, raw)
	}
	status, _, raw = f.request(t, "POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), headers)
	if status != 200 {
		t.Fatalf("tools/list %d %s", status, raw)
	}
	return id
}
func TestOAuthSessionBindingAndRevokeClosesDesktopGrants(t *testing.T) {
	f := newOAuthFixture(t, true)
	first := f.accessToken(t)
	second := f.accessToken(t)
	id := f.initializeHTTP(t, first)
	headers := map[string]string{"Authorization": "Bearer " + second, "Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Mcp-Session-Id": id, "MCP-Protocol-Version": "2025-11-25"}
	status, _, _ := f.request(t, "POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), headers)
	if status != 403 {
		t.Fatalf("cross-token session hijack %d", status)
	}
	headers["Authorization"] = "Bearer " + first
	status, _, _ = f.request(t, "POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), headers)
	if status != 200 {
		t.Fatalf("owner session failed %d", status)
	}
	f.b.mu.Lock()
	f.b.grants["test-grant"] = &Grant{ID: "test-grant", Client: id, Capability: "observe", Scope: Scope{"window", "abc"}, Expires: time.Now().Add(time.Minute)}
	f.b.mu.Unlock()
	info, e := f.p.verify(f.ctx, first, nil)
	if e != nil {
		t.Fatal(e)
	}
	f.p.revokeConnection(info.UserID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.b.mu.Lock()
		_, client := f.b.clients[id]
		_, grant := f.b.grants["test-grant"]
		f.b.mu.Unlock()
		if !client && !grant {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.b.mu.Lock()
	_, present := f.b.grants["test-grant"]
	f.b.mu.Unlock()
	if present {
		t.Fatal("OAuth revoke retained desktop grant")
	}
	status, _, _ = f.request(t, "POST", "/mcp", strings.NewReader(`{}`), headers)
	if status != 401 {
		t.Fatal("revoked bearer accepted")
	}
}
func TestOAuthExpiryClosesSession(t *testing.T) {
	f := newOAuthFixture(t, true)
	token := f.accessToken(t)
	f.p.mu.Lock()
	v := f.p.tokens[digest(token)]
	v.Expires = time.Now().Add(250 * time.Millisecond)
	f.p.tokens[digest(token)] = v
	f.p.mu.Unlock()
	id := f.initializeHTTP(t, token)
	time.Sleep(350 * time.Millisecond)
	f.b.mu.Lock()
	_, present := f.b.clients[id]
	f.b.mu.Unlock()
	if present {
		t.Fatal("expired token retained MCP connection")
	}
	if _, e := f.p.verify(f.ctx, token, nil); e == nil {
		t.Fatal("expired token accepted")
	}
}
func TestOAuthAuthorizationRejectsDowngradeAndOpenRedirect(t *testing.T) {
	f := newOAuthFixture(t, true)
	c := f.register(t, "none")
	q := url.Values{"client_id": {c["client_id"].(string)}, "redirect_uri": {"https://evil.example/"}, "response_type": {"code"}, "resource": {f.s.URL + "/mcp"}, "state": {"kept"}}
	status, h, _ := f.request(t, "GET", "/authorize?"+q.Encode(), nil, nil)
	if status != 400 || h.Get("Location") != "" {
		t.Fatal("redirected to unregistered URI")
	}
	q.Set("redirect_uri", "http://127.0.0.1:45678/callback")
	for _, method := range []string{"", "plain"} {
		q.Set("code_challenge_method", method)
		status, h, _ = f.request(t, "GET", "/authorize?"+q.Encode(), nil, nil)
		u, _ := url.Parse(h.Get("Location"))
		if status != 302 || u.Query().Get("error") != "invalid_request" || u.Query().Get("state") != "kept" {
			t.Fatal("PKCE downgrade accepted or error not delivered correctly")
		}
	}
	if pending, _ := f.p.state(); len(pending) != 0 {
		t.Fatal("invalid OAuth request reached approval UI")
	}
}
func TestOAuthRevocationEndpoint(t *testing.T) {
	f := newOAuthFixture(t, true)
	c := f.register(t, "none")
	form := f.approvedCode(t, c)
	status, raw := f.exchange(t, form, c)
	if status != 200 {
		t.Fatal("token exchange")
	}
	var token map[string]any
	json.Unmarshal(raw, &token)
	form = url.Values{"client_id": {c["client_id"].(string)}, "token": {token["access_token"].(string)}}
	status, _, _ = f.request(t, "POST", "/revoke", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if status != http.StatusOK {
		t.Fatal("revoke endpoint")
	}
	if _, e := f.p.verify(f.ctx, token["access_token"].(string), nil); e == nil {
		t.Fatal("revoked token valid")
	}
}
