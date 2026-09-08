package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const connectScope = "mcp:connect"

type OAuthClient struct {
	GrantTypes []string `json:"grant_types"`
	ID         string   `json:"client_id"`
	Name       string   `json:"client_name"`
	Redirects  []string `json:"redirect_uris"`
	Method     string   `json:"token_endpoint_auth_method"`
	SecretHash string   `json:"secret_hash,omitempty"`
	Created    int64    `json:"client_id_issued_at"`
}
type OAuthPending struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	Name       string    `json:"name"`
	Redirect   string    `json:"redirect_uri"`
	Expires    time.Time `json:"expires"`
	State      string    `json:"-"`
	Challenge  string    `json:"-"`
	CookieHash string    `json:"-"`
	Decision   string    `json:"-"`
	Code       string    `json:"-"`
}
type oauthCode struct {
	Client, Redirect, Challenge, Resource string
	Expires                               time.Time
}
type OAuthConnection struct {
	ID       string    `json:"id"`
	ClientID string    `json:"client_id"`
	Name     string    `json:"name"`
	Expires  time.Time `json:"expires"`
	Resource string    `json:"-"`
}
type oauthRefresh struct {
	Connection OAuthConnection
	Used       bool
}

type OAuthProvider struct {
	refresh                       map[string]oauthRefresh
	mu                            sync.Mutex
	issuer, resource, clientsFile string
	clients                       map[string]OAuthClient
	pending                       map[string]*OAuthPending
	codes                         map[string]oauthCode
	tokens                        map[string]OAuthConnection
	now                           func() time.Time
	notify                        func()
	revoke                        func(string)
}

func secret() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func newOAuthProvider(issuer, data string) (*OAuthProvider, error) {
	u, e := url.Parse(issuer)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("OAuth requires an HTTPS public URL with no path, query or credentials")
	}
	issuer = strings.TrimRight(issuer, "/")
	p := &OAuthProvider{issuer: issuer, resource: issuer + "/mcp", clientsFile: filepath.Join(data, "oauth-clients.json"), refresh: map[string]oauthRefresh{}, clients: map[string]OAuthClient{}, pending: map[string]*OAuthPending{}, codes: map[string]oauthCode{}, tokens: map[string]OAuthConnection{}, now: time.Now}
	raw, e := os.ReadFile(p.clientsFile)
	if e == nil {
		if e = json.Unmarshal(raw, &p.clients); e != nil {
			return nil, fmt.Errorf("OAuth client registry: %w", e)
		}
		if len(p.clients) > 256 {
			return nil, errors.New("OAuth client registry exceeds limit")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return p, nil
}
func (p *OAuthProvider) saveClientsLocked() error {
	raw, e := json.Marshal(p.clients)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p.clientsFile), ".oauth-clients-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, p.clientsFile)
}
func validRedirect(s string) bool {
	if len(s) > 2048 {
		return false
	}
	u, e := url.Parse(s)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}
func oauthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func oauthError(w http.ResponseWriter, status int, code, detail string) {
	oauthJSON(w, status, map[string]string{"error": code, "error_description": detail})
}
func (p *OAuthProvider) cleanupLocked() {
	now := p.now()
	for k, v := range p.pending {
		if !now.Before(v.Expires) {
			delete(p.pending, k)
		}
	}
	for k, v := range p.codes {
		if !now.Before(v.Expires) {
			delete(p.codes, k)
		}
	}
	for k, v := range p.refresh {
		if !now.Before(v.Connection.Expires) {
			delete(p.refresh, k)
		}
	}
	for k, v := range p.tokens {
		if !now.Before(v.Expires) {
			delete(p.tokens, k)
		}
	}
}
func (p *OAuthProvider) metadata(w http.ResponseWriter, r *http.Request) {
	oauthJSON(w, 200, map[string]any{"issuer": p.issuer, "authorization_endpoint": p.issuer + "/authorize", "token_endpoint": p.issuer + "/token", "registration_endpoint": p.issuer + "/register", "revocation_endpoint": p.issuer + "/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}, "revocation_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}, "scopes_supported": []string{connectScope}, "authorization_response_iss_parameter_supported": true})
}
func (p *OAuthProvider) register(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Name          string   `json:"client_name"`
		Redirects     []string `json:"redirect_uris"`
		Method        string   `json:"token_endpoint_auth_method"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		Scope         string   `json:"scope"`
	}
	if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
		oauthError(w, 400, "invalid_client_metadata", "invalid registration JSON")
		return
	}
	if len(q.Redirects) == 0 || len(q.Redirects) > 10 || len(q.Name) > 100 {
		oauthError(w, 400, "invalid_client_metadata", "provide 1–10 redirect URIs and a name up to 100 bytes")
		return
	}
	for _, v := range q.Redirects {
		if !validRedirect(v) {
			oauthError(w, 400, "invalid_redirect_uri", "redirects must use HTTPS or loopback HTTP, without fragments or credentials")
			return
		}
	}
	if q.Method == "" {
		q.Method = "client_secret_basic"
	}
	if q.Method != "none" && q.Method != "client_secret_basic" {
		oauthError(w, 400, "invalid_client_metadata", "supported auth methods: none, client_secret_basic")
		return
	}
	for _, v := range q.GrantTypes {
		if v != "authorization_code" && v != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata", "only authorization_code and refresh_token are supported")
			return
		}
	}
	for _, v := range q.ResponseTypes {
		if v != "code" {
			oauthError(w, 400, "invalid_client_metadata", "only code responses are supported")
			return
		}
	}
	if q.Scope != "" && q.Scope != connectScope {
		oauthError(w, 400, "invalid_client_metadata", "unsupported scope")
		return
	}
	if len(q.GrantTypes) == 0 {
		q.GrantTypes = []string{"authorization_code"}
	}
	if !slices.Contains(q.GrantTypes, "authorization_code") {
		oauthError(w, 400, "invalid_client_metadata", "authorization_code must be supported")
		return
	}
	c := OAuthClient{GrantTypes: q.GrantTypes, ID: randomID(), Name: cleanReason(q.Name), Redirects: q.Redirects, Method: q.Method, Created: p.now().Unix()}
	if c.Name == "" {
		c.Name = "Unnamed MCP client"
	}
	sec := ""
	if c.Method == "client_secret_basic" {
		sec = secret()
		c.SecretHash = digest(sec)
	}
	p.mu.Lock()
	if len(p.clients) >= 256 {
		p.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "client registration limit reached")
		return
	}
	p.clients[c.ID] = c
	e := p.saveClientsLocked()
	if e != nil {
		delete(p.clients, c.ID)
	}
	p.mu.Unlock()
	if e != nil {
		oauthError(w, 500, "server_error", "cannot persist client registration")
		return
	}
	out := map[string]any{"client_id": c.ID, "client_name": c.Name, "client_id_issued_at": c.Created, "redirect_uris": c.Redirects, "token_endpoint_auth_method": c.Method, "grant_types": c.GrantTypes, "response_types": []string{"code"}, "scope": connectScope}
	if sec != "" {
		out["client_secret"] = sec
		out["client_secret_expires_at"] = 0
	}
	oauthJSON(w, 201, out)
}
func uniqueParams(v url.Values) bool {
	for _, vs := range v {
		if len(vs) != 1 {
			return false
		}
	}
	return true
}
func (p *OAuthProvider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !uniqueParams(q) {
		oauthError(w, 400, "invalid_request", "duplicate parameters")
		return
	}
	p.mu.Lock()
	p.cleanupLocked()
	c, ok := p.clients[q.Get("client_id")]
	p.mu.Unlock()
	if !ok || !slices.Contains(c.Redirects, q.Get("redirect_uri")) {
		oauthError(w, 400, "invalid_request", "unknown client or unregistered redirect URI")
		return
	}
	if len(q.Get("state")) > 2048 {
		oauthError(w, 400, "invalid_request", "state too long")
		return
	}
	// Report errors through the exact registered redirect, preserving state/issuer.
	fail := func(code, detail string) {
		u, _ := url.Parse(q.Get("redirect_uri"))
		v := u.Query()
		v.Set("error", code)
		v.Set("error_description", detail)
		v.Set("iss", p.issuer)
		if q.Get("state") != "" {
			v.Set("state", q.Get("state"))
		}
		u.RawQuery = v.Encode()
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only code is supported")
		return
	}
	if q.Get("resource") != p.resource {
		fail("invalid_target", "resource must identify this MCP endpoint")
		return
	}
	if q.Get("scope") != "" && q.Get("scope") != connectScope {
		fail("invalid_scope", "only mcp:connect is supported")
		return
	}
	challenge := q.Get("code_challenge")
	decoded, e := base64.RawURLEncoding.DecodeString(challenge)
	if q.Get("code_challenge_method") != "S256" || e != nil || len(decoded) != 32 || len(challenge) != 43 {
		fail("invalid_request", "PKCE S256 challenge required")
		return
	}
	nonce := secret()
	pending := &OAuthPending{ID: randomID(), ClientID: c.ID, Name: c.Name, Redirect: q.Get("redirect_uri"), Expires: p.now().Add(5 * time.Minute), State: q.Get("state"), Challenge: challenge, CookieHash: digest(nonce), Decision: "pending"}
	p.mu.Lock()
	if len(p.pending) >= 16 {
		p.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "too many pending authorizations")
		return
	}
	p.pending[pending.ID] = pending
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "cu_oauth_" + pending.ID, Value: nonce, Path: "/oauth/continue", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	if p.notify != nil {
		p.notify()
	}
	p.waitPage(w, pending)
}

var oauthWaiting = template.Must(template.New("oauth").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="2;url=/oauth/continue?id={{.ID}}"><title>Computer Use — approve locally</title></head><body><h1>Approve this connection on your desktop</h1><p>Unverified client name: <strong>{{.Name}}</strong></p><p>Redirect URI: <code>{{.Redirect}}</code></p><p>Open the Computer Use tray console and approve or deny this OAuth connection. Connecting only grants access to MCP; viewing and controlling your windows still need separate permission.</p><p>This page waits for your local decision. There is no remote Approve button.</p></body></html>`))

func (p *OAuthProvider) waitPage(w http.ResponseWriter, v *OAuthPending) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	oauthWaiting.Execute(w, v)
}
func (p *OAuthProvider) continueAuth(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	cookie, e := r.Cookie("cu_oauth_" + id)
	p.mu.Lock()
	p.cleanupLocked()
	pending := p.pending[id]
	if e != nil || pending == nil || subtle.ConstantTimeCompare([]byte(digest(cookie.Value)), []byte(pending.CookieHash)) != 1 {
		p.mu.Unlock()
		oauthError(w, 400, "invalid_request", "authorization expired or browser cookie missing")
		return
	}
	v := *pending
	if v.Decision == "pending" {
		p.mu.Unlock()
		p.waitPage(w, &v)
		return
	}
	delete(p.pending, id)
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "cu_oauth_" + id, Value: "", Path: "/oauth/continue", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	u, _ := url.Parse(v.Redirect)
	q := u.Query()
	q.Set("iss", p.issuer)
	if v.State != "" {
		q.Set("state", v.State)
	}
	if v.Decision == "approved" {
		q.Set("code", v.Code)
	} else {
		q.Set("error", "access_denied")
	}
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}
func (p *OAuthProvider) decide(id string, approve bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupLocked()
	v := p.pending[id]
	if v == nil || v.Decision != "pending" {
		return errors.New("OAuth authorization expired or already decided")
	}
	if !approve {
		v.Decision = "denied"
		return nil
	}
	if len(p.codes) >= 64 {
		return errors.New("authorization code limit reached")
	}
	v.Decision = "approved"
	v.Code = secret()
	p.codes[digest(v.Code)] = oauthCode{v.ClientID, v.Redirect, v.Challenge, p.resource, p.now().Add(time.Minute)}
	return nil
}
func (p *OAuthProvider) authenticateClient(r *http.Request) (OAuthClient, bool) {
	id, sec, basic := r.BasicAuth()
	if basic {
		var e error
		id, e = url.QueryUnescape(id)
		if e != nil {
			return OAuthClient{}, false
		}
		sec, e = url.QueryUnescape(sec)
		if e != nil {
			return OAuthClient{}, false
		}
		if r.PostForm.Get("client_id") != "" && r.PostForm.Get("client_id") != id {
			return OAuthClient{}, false
		}
	} else {
		id = r.PostForm.Get("client_id")
	}
	p.mu.Lock()
	c, ok := p.clients[id]
	p.mu.Unlock()
	if !ok {
		return c, false
	}
	if c.Method == "none" {
		return c, !basic && r.PostForm.Get("client_secret") == ""
	}
	return c, basic && subtle.ConstantTimeCompare([]byte(digest(sec)), []byte(c.SecretHash)) == 1
}
func (p *OAuthProvider) token(w http.ResponseWriter, r *http.Request) {
	if e := r.ParseForm(); e != nil || !uniqueParams(r.PostForm) || r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request", "invalid form")
		return
	}
	c, ok := p.authenticateClient(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="computer-use"`)
		oauthError(w, 401, "invalid_client", "client authentication failed")
		return
	}
	if r.PostForm.Get("grant_type") == "refresh_token" {
		p.refreshToken(w, r, c)
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, 400, "unsupported_grant_type", "only authorization_code and refresh_token are supported")
		return
	}
	if r.PostForm.Get("resource") != p.resource {
		oauthError(w, 400, "invalid_target", "resource must identify this MCP endpoint")
		return
	}
	verifier := r.PostForm.Get("code_verifier")
	if len(verifier) < 43 || len(verifier) > 128 || strings.IndexFunc(verifier, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r))
	}) >= 0 {
		oauthError(w, 400, "invalid_grant", "invalid PKCE verifier")
		return
	}
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	key := digest(r.PostForm.Get("code"))
	p.mu.Lock()
	p.cleanupLocked()
	code, ok := p.codes[key]
	if !ok || code.Client != c.ID || code.Redirect != r.PostForm.Get("redirect_uri") || code.Resource != r.PostForm.Get("resource") || subtle.ConstantTimeCompare([]byte(code.Challenge), []byte(challenge)) != 1 {
		p.mu.Unlock()
		oauthError(w, 400, "invalid_grant", "invalid, expired, replayed or mismatched authorization code")
		return
	}
	if len(p.tokens) >= 256 {
		p.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "connection token limit reached")
		return
	}
	delete(p.codes, key)
	raw := secret()
	v := OAuthConnection{ID: randomID(), ClientID: c.ID, Name: c.Name, Expires: p.now().Add(time.Hour), Resource: p.resource}
	p.tokens[digest(raw)] = v
	refresh := ""
	if slices.Contains(c.GrantTypes, "refresh_token") {
		refresh = secret()
		p.refresh[digest(refresh)] = oauthRefresh{Connection: v}
	}
	p.mu.Unlock()
	out := map[string]any{"access_token": raw, "token_type": "Bearer", "expires_in": 3600, "scope": connectScope}
	if refresh != "" {
		out["refresh_token"] = refresh
	}
	oauthJSON(w, 200, out)
}
func (p *OAuthProvider) verify(ctx context.Context, raw string, r *http.Request) (*auth.TokenInfo, error) {
	p.mu.Lock()
	v, ok := p.tokens[digest(raw)]
	p.mu.Unlock()
	if !ok || !p.now().Before(v.Expires) || v.Resource != p.resource {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{UserID: v.ID, Scopes: []string{connectScope}, Expiration: v.Expires, Extra: map[string]any{"client_name": v.Name, "client_id": v.ClientID}}, nil
}
func (p *OAuthProvider) revokeConnection(id string) {
	p.mu.Lock()
	for k, v := range p.refresh {
		if v.Connection.ID == id {
			delete(p.refresh, k)
		}
	}
	for k, v := range p.tokens {
		if v.ID == id {
			delete(p.tokens, k)
		}
	}
	p.mu.Unlock()
	if p.revoke != nil {
		p.revoke(id)
	}
}
func (p *OAuthProvider) revokeToken(w http.ResponseWriter, r *http.Request) {
	if e := r.ParseForm(); e != nil || !uniqueParams(r.PostForm) || r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request", "invalid form")
		return
	}
	c, ok := p.authenticateClient(r)
	if !ok {
		oauthError(w, 401, "invalid_client", "client authentication failed")
		return
	}
	p.mu.Lock()
	v, ok := p.tokens[digest(r.PostForm.Get("token"))]
	if !ok {
		if rt, found := p.refresh[digest(r.PostForm.Get("token"))]; found {
			v = rt.Connection
			ok = true
		}
	}
	p.mu.Unlock()
	if ok && v.ClientID == c.ID {
		p.revokeConnection(v.ID)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
}
func (p *OAuthProvider) state() ([]OAuthPending, []OAuthConnection) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupLocked()
	pending := []OAuthPending{}
	connections := []OAuthConnection{}
	for _, v := range p.pending {
		if v.Decision == "pending" {
			pending = append(pending, OAuthPending{ID: v.ID, ClientID: v.ClientID, Name: v.Name, Redirect: v.Redirect, Expires: v.Expires})
		}
	}
	seen := map[string]bool{}
	for _, v := range p.tokens {
		if !seen[v.ID] {
			connections = append(connections, v)
			seen[v.ID] = true
		}
	}
	slices.SortFunc(pending, func(a, b OAuthPending) int { return a.Expires.Compare(b.Expires) })
	slices.SortFunc(connections, func(a, b OAuthConnection) int { return a.Expires.Compare(b.Expires) })
	return pending, connections
}
func (p *OAuthProvider) routes(mux *http.ServeMux) {
	metadata := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{Resource: p.resource, AuthorizationServers: []string{p.issuer}, ScopesSupported: []string{connectScope}, BearerMethodsSupported: []string{"header"}, ResourceName: "Computer Use MCP"})
	mux.Handle("GET /.well-known/oauth-protected-resource", metadata)
	mux.Handle("GET /.well-known/oauth-protected-resource/mcp", metadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", p.metadata)
	mux.HandleFunc("POST /register", p.register)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("GET /oauth/continue", p.continueAuth)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("POST /revoke", p.revokeToken)
}
