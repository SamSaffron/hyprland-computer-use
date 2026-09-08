package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type oauthFixture struct {
	b   *Broker
	p   *OAuthProvider
	h   *HTTPGateway
	s   *httptest.Server
	c   *http.Client
	ctx context.Context
}

func newOAuthFixture(t *testing.T, enabled bool) *oauthFixture {
	t.Helper()
	b, _, _ := shareFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	f := &oauthFixture{b: b, ctx: ctx}
	f.s = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { cancel(); f.s.Close() })
	var e error
	if enabled {
		f.p, e = newOAuthProvider(f.s.URL, t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
	}
	f.h, e = newHTTPGateway(ctx, b, f.s.URL, f.p)
	if e != nil {
		t.Fatal(e)
	}
	b.oauth = f.p
	f.c = f.s.Client()
	f.c.Jar, _ = cookiejar.New(nil)
	f.c.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	return f
}
func (f *oauthFixture) request(t *testing.T, method, path string, body io.Reader, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	r, e := http.NewRequest(method, f.s.URL+path, body)
	if e != nil {
		t.Fatal(e)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, e := f.c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, resp.Header, raw
}
func (f *oauthFixture) register(t *testing.T, method string) map[string]any {
	t.Helper()
	q := map[string]any{"client_name": "Test MCP app", "redirect_uris": []string{"http://127.0.0.1:45678/callback"}, "token_endpoint_auth_method": method}
	raw, _ := json.Marshal(q)
	status, _, body := f.request(t, "POST", "/register", bytes.NewReader(raw), map[string]string{"Content-Type": "application/json"})
	if status != 201 {
		t.Fatalf("register %d %s", status, body)
	}
	var result map[string]any
	json.Unmarshal(body, &result)
	return result
}
func (f *oauthFixture) authorization(t *testing.T, c map[string]any) (string, url.Values) {
	t.Helper()
	verifier := secret()
	h := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {c["client_id"].(string)}, "redirect_uri": {"http://127.0.0.1:45678/callback"}, "resource": {f.s.URL + "/mcp"}, "scope": {connectScope}, "state": {"state-to-preserve"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(h[:])}}
	status, _, body := f.request(t, "GET", "/authorize?"+q.Encode(), nil, nil)
	if status != 200 {
		t.Fatalf("authorize %d %s", status, body)
	}
	pending, _ := f.p.state()
	if len(pending) != 1 {
		t.Fatalf("pending %v", pending)
	}
	return pending[0].ID, url.Values{"grant_type": {"authorization_code"}, "client_id": {c["client_id"].(string)}, "redirect_uri": {"http://127.0.0.1:45678/callback"}, "resource": {f.s.URL + "/mcp"}, "code_verifier": {verifier}}
}
func (f *oauthFixture) approvedCode(t *testing.T, c map[string]any) url.Values {
	t.Helper()
	id, form := f.authorization(t, c)
	if e := f.p.decide(id, true); e != nil {
		t.Fatal(e)
	}
	status, head, raw := f.request(t, "GET", "/oauth/continue?id="+id, nil, nil)
	if status != 302 {
		t.Fatalf("continue %d %s", status, raw)
	}
	u, _ := url.Parse(head.Get("Location"))
	if u.Query().Get("state") != "state-to-preserve" || u.Query().Get("iss") != f.s.URL {
		t.Fatal("state/issuer not preserved")
	}
	form.Set("code", u.Query().Get("code"))
	return form
}
func (f *oauthFixture) exchange(t *testing.T, form url.Values, client map[string]any) (int, []byte) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	if sec, ok := client["client_secret"].(string); ok {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(client["client_id"].(string))+":"+url.QueryEscape(sec)))
	}
	code, _, raw := f.request(t, "POST", "/token", strings.NewReader(form.Encode()), headers)
	return code, raw
}
func (f *oauthFixture) accessToken(t *testing.T) string {
	c := f.register(t, "none")
	form := f.approvedCode(t, c)
	status, body := f.exchange(t, form, c)
	if status != 200 {
		t.Fatalf("token %d %s", status, body)
	}
	var out map[string]any
	json.Unmarshal(body, &out)
	return out["access_token"].(string)
}
func TestOAuthDiscoveryAndNoUnauthenticatedMetadata(t *testing.T) {
	f := newOAuthFixture(t, true)
	status, head, _ := f.request(t, "POST", "/mcp", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if status != 401 || !strings.Contains(head.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("challenge %d %v", status, head)
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		status, _, raw := f.request(t, "GET", path, nil, nil)
		if status != 200 || !bytes.Contains(raw, []byte(f.s.URL+"/mcp")) {
			t.Fatal("resource discovery")
		}
	}
	status, _, raw := f.request(t, "GET", "/.well-known/oauth-authorization-server", nil, nil)
	if status != 200 || !bytes.Contains(raw, []byte(`"S256"`)) {
		t.Fatal("issuer discovery")
	}
}
func TestOAuthPKCEResourceRedirectAndCodeReplay(t *testing.T) {
	f := newOAuthFixture(t, true)
	c := f.register(t, "none")
	form := f.approvedCode(t, c)
	for _, field := range []string{"code_verifier", "resource", "redirect_uri", "client_id"} {
		old := form.Get(field)
		form.Set(field, "wrong")
		if status, _ := f.exchange(t, form, c); status < 400 {
			t.Fatalf("accepted wrong %s", field)
		}
		form.Set(field, old)
	}
	status, body := f.exchange(t, form, c)
	if status != 200 {
		t.Fatalf("exchange %d %s", status, body)
	}
	if status, _ := f.exchange(t, form, c); status != 400 {
		t.Fatal("authorization code reused")
	}
	var out map[string]any
	json.Unmarshal(body, &out)
	token := out["access_token"].(string)
	if strings.Contains(string(body), "refresh_token") {
		t.Fatal("unexpected refresh grant")
	}
	info, e := f.p.verify(context.Background(), token, nil)
	if e != nil || info.UserID == "" {
		t.Fatal("issued token invalid")
	}
	if _, e = f.p.verify(context.Background(), "wrong", nil); e == nil {
		t.Fatal("unknown token accepted")
	}
	f.p.revokeConnection(info.UserID)
	if _, e = f.p.verify(context.Background(), token, nil); e == nil {
		t.Fatal("revoked token accepted")
	}
}
func TestOAuthBrowserCookieAndLocalDenial(t *testing.T) {
	f := newOAuthFixture(t, true)
	c := f.register(t, "none")
	id, _ := f.authorization(t, c)
	jar := f.c.Jar
	f.c.Jar = nil
	status, _, _ := f.request(t, "GET", "/oauth/continue?id="+id, nil, nil)
	if status != 400 {
		t.Fatal("missing browser cookie accepted")
	}
	f.c.Jar = jar
	if e := f.p.decide(id, false); e != nil {
		t.Fatal(e)
	}
	status, head, _ := f.request(t, "GET", "/oauth/continue?id="+id, nil, nil)
	if status != 302 || !strings.Contains(head.Get("Location"), "error=access_denied") {
		t.Fatal("denial not returned")
	}
	if f.p.decide(id, true) == nil {
		t.Fatal("denied request resurrected")
	}
}
func TestOAuthRegistrationValidationAndPersistence(t *testing.T) {
	f := newOAuthFixture(t, true)
	for _, uri := range []string{"http://evil.example/callback", "https://good.example/#fragment", "https://user:pass@good.example/", "javascript:alert(1)", "http://127.0.0.1.evil.example/cb"} {
		raw, _ := json.Marshal(map[string]any{"redirect_uris": []string{uri}})
		status, _, _ := f.request(t, "POST", "/register", bytes.NewReader(raw), nil)
		if status != 400 {
			t.Fatalf("bad redirect %s", uri)
		}
	}
	c := f.register(t, "")
	if c["token_endpoint_auth_method"] != "client_secret_basic" || c["client_secret"] == nil {
		t.Fatal("RFC7591 default must be confidential/basic")
	}
	raw, e := os.ReadFile(f.p.clientsFile)
	if e != nil || bytes.Contains(raw, []byte(c["client_secret"].(string))) {
		t.Fatal("raw secret persisted")
	}
	p, e := newOAuthProvider(f.s.URL, strings.TrimSuffix(f.p.clientsFile, "/oauth-clients.json"))
	if e != nil {
		t.Fatal(e)
	}
	if p.clients[c["client_id"].(string)].ID == "" {
		t.Fatal("client registration lost on restart")
	}
	form := f.approvedCode(t, c)
	status, body := f.exchange(t, form, c)
	if status != 200 {
		t.Fatalf("confidential flow %d %s", status, body)
	}
}
func TestOAuthHostOriginAndTransportPolicy(t *testing.T) {
	f := newOAuthFixture(t, true)
	status, _, _ := f.request(t, "POST", "/mcp", strings.NewReader(`{}`), map[string]string{"Origin": "https://evil.example"})
	if status != 403 {
		t.Fatal("cross-origin MCP admitted")
	}
	req, _ := http.NewRequest("GET", f.s.URL+"/.well-known/oauth-protected-resource", nil)
	req.Host = "evil.example"
	r, e := f.c.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("rebound Host admitted")
	}
	for _, args := range []struct {
		addr, public string
		oauth        bool
		cert, key    string
	}{{"0.0.0.0:8080", "http://example.com", false, "", ""}, {"0.0.0.0:8080", "https://example.com", true, "", ""}, {"127.0.0.1:8080", "http://localhost:8080", true, "", ""}, {"127.0.0.1:8080", "", true, "", ""}} {
		if _, e := httpOrigin(args.addr, args.public, args.oauth, args.cert, args.key); e == nil {
			t.Fatalf("unsafe HTTP config %+v", args)
		}
	}
}
func TestOfficialMCPClientOAuthFlowAndSeparateDesktopPermissions(t *testing.T) {
	f := newOAuthFixture(t, true)
	fetch := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		resp, e := f.c.Get(args.URL)
		if e != nil {
			return nil, e
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		pending, _ := f.p.state()
		if len(pending) != 1 {
			t.Fatalf("SDK authorization pending %v", pending)
		}
		if e = f.p.decide(pending[0].ID, true); e != nil {
			return nil, e
		}
		resp, e = f.c.Get(f.s.URL + "/oauth/continue?id=" + pending[0].ID)
		if e != nil {
			return nil, e
		}
		resp.Body.Close()
		u, e := url.Parse(resp.Header.Get("Location"))
		if e != nil {
			return nil, e
		}
		return &auth.AuthorizationResult{Code: u.Query().Get("code"), State: u.Query().Get("state"), Iss: u.Query().Get("iss")}, nil
	}
	handler, e := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{Client: f.c, RedirectURL: "http://127.0.0.1:45678/callback", DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{RedirectURIs: []string{"http://127.0.0.1:45678/callback"}, TokenEndpointAuthMethod: "none", ClientName: "Official SDK test"}}, AuthorizationCodeFetcher: fetch})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "interop-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.s.URL + "/mcp", HTTPClient: f.c, OAuthHandler: handler, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_windows", Arguments: map[string]any{}})
	if e != nil || result.IsError {
		t.Fatalf("discovery %v %+v", e, result)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "view_window", Arguments: map[string]any{"window_id": "abc"}})
	if e != nil || result.IsError {
		t.Fatalf("request %v %+v", e, result)
	}
	raw, _ := json.Marshal(result)
	if !bytes.Contains(raw, []byte("approval_required")) {
		t.Fatal("OAuth granted desktop pixel authority")
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "oauth_approve", Arguments: map[string]any{}})
	if e == nil && !result.IsError {
		t.Fatal("MCP can approve OAuth")
	}
	f.b.mu.Lock()
	count := len(f.b.clients)
	f.b.mu.Unlock()
	if count != 2 {
		t.Fatalf("OAuth probe exposed as recipient: %d clients", count)
	}
	liveID := session.ID()
	session.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.h.mu.Lock()
		_, present := f.h.peers[liveID]
		f.h.mu.Unlock()
		if !present {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP disconnect retained its live peer")
}
func TestHTTPWithoutOAuthAndNoAuthEndpoints(t *testing.T) {
	f := newOAuthFixture(t, false)
	status, _, _ := f.request(t, "GET", "/.well-known/oauth-authorization-server", nil, nil)
	if status != 404 {
		t.Fatal("OAuth surfaced in disabled mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "noauth", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.s.URL + "/mcp", HTTPClient: f.c, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "computer_status", Arguments: map[string]any{}})
	if e != nil || result.IsError {
		t.Fatalf("no-auth HTTP %v", e)
	}
}
