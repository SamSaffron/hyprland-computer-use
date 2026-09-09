package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type httpPeer struct {
	id, principal string
	server        *mcp.Server
	cancel        context.CancelFunc
}
type httpAdmission struct {
	id       string
	consumed bool
}
type httpAdmissionKey struct{}

type HTTPGateway struct {
	b       *Broker
	ctx     context.Context
	mu      sync.Mutex
	peers   map[string]*httpPeer
	origin  string
	oauth   *OAuthProvider
	handler http.Handler
}

func newHTTPGateway(ctx context.Context, b *Broker, origin string, p *OAuthProvider) (*HTTPGateway, error) {
	u, e := url.Parse(origin)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("public URL must be an HTTP(S) origin without path or credentials")
	}
	origin = strings.TrimRight(origin, "/")
	h := &HTTPGateway{b: b, ctx: ctx, peers: map[string]*httpPeer{}, origin: origin, oauth: p}
	transport := mcp.NewStreamableHTTPHandler(h.newServer, &mcp.StreamableHTTPOptions{JSONResponse: true, SessionTimeout: 5 * time.Minute, MaxRequestBodyBytes: 1 << 20, DisableLocalhostProtection: true})
	// This wrapper enforces an exact configured Host and Origin, including behind
	// a TLS reverse proxy. Forwarded headers are never trusted to choose an origin.
	var endpoint http.Handler = http.HandlerFunc(h.admitMCP(transport))
	mux := http.NewServeMux()
	if p != nil {
		p.revoke = h.revokePrincipal
		p.notify = func() {
			b.mu.Lock()
			b.open++
			b.noteLocked("oauth_requested", "local approval required for a new HTTP client")
			b.mu.Unlock()
		}
		endpoint = auth.RequireBearerToken(p.verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: origin + "/.well-known/oauth-protected-resource/mcp", Scopes: []string{connectScope}})(endpoint)
		p.routes(mux)
	}
	mux.Handle("/mcp", endpoint)
	h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != u.Host {
			http.Error(w, "unrecognized Host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != origin {
			http.Error(w, "untrusted Origin", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id, MCP-Protocol-Version")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Session-Id, MCP-Protocol-Version, Last-Event-ID")
			w.WriteHeader(204)
			return
		}
		limit := int64(32768)
		if r.URL.Path == "/mcp" {
			limit = 1 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		if len(r.URL.RawQuery) > 8192 {
			http.Error(w, "query too long", 414)
			return
		}
		if r.URL.Path == "/mcp" && r.URL.RawQuery != "" {
			http.Error(w, "MCP credentials belong in the Authorization header, not URLs", 400)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return h, nil
}
func (h *HTTPGateway) admitMCP(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A missing session header on POST is the only request that can create a
		// peer. Reserve capacity before the SDK factory so overload has an
		// explicit HTTP response and concurrent initializations cannot exceed it.
		if r.Method != http.MethodPost || r.Header.Get("Mcp-Session-Id") != "" {
			next.ServeHTTP(w, r)
			return
		}
		id, err := randomID()
		if err != nil {
			http.Error(w, "unable to allocate MCP session", http.StatusInternalServerError)
			return
		}
		h.mu.Lock()
		if len(h.peers) >= 64 {
			h.mu.Unlock()
			w.Header().Set("Retry-After", "5")
			http.Error(w, "MCP peer capacity reached", http.StatusServiceUnavailable)
			return
		}
		// The reservation occupies the peers map immediately and is replaced by
		// the real peer synchronously when newServer runs.
		h.peers[id] = nil
		h.mu.Unlock()
		admission := &httpAdmission{id: id}
		r = r.WithContext(context.WithValue(r.Context(), httpAdmissionKey{}, admission))
		defer func() {
			if !admission.consumed {
				h.mu.Lock()
				delete(h.peers, id)
				h.mu.Unlock()
			}
		}()
		next.ServeHTTP(w, r)
	}
}

func (h *HTTPGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.handler.ServeHTTP(w, r) }
func (h *HTTPGateway) newServer(r *http.Request) *mcp.Server {
	admission, ok := r.Context().Value(httpAdmissionKey{}).(*httpAdmission)
	if !ok || admission == nil || admission.consumed {
		return nil
	}
	id := admission.id
	admission.consumed = true
	principal := ""
	label := "HTTP MCP " + id[:8]
	expires := time.Time{}
	if info := auth.TokenInfoFromContext(r.Context()); info != nil {
		principal = info.UserID
		expires = info.Expiration
		if name, ok := info.Extra["client_name"].(string); ok {
			label = "OAuth " + name + " · " + id[:8]
		}
	}
	h.mu.Lock()
	peerCtx, cancel := context.WithCancel(h.ctx)
	peer := &httpPeer{id: id, principal: principal, cancel: cancel}
	h.peers[id] = peer
	ready := make(chan *mcp.ServerSession, 1)
	peer.server = h.b.newMCPServer(peerCtx, id, &mcp.ServerOptions{GetSessionID: func() string { return id }})
	// A client's OAuth retry may send a disposable initialize probe. Do not
	// offer that ghost session as a sharing recipient. Activate when the client begins using MCP (tools/list, status, etc.),
	// not on an initialize/initialized probe alone.
	var activate sync.Once
	peer.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if strings.HasPrefix(method, "tools/") {
				if ss, ok := req.GetSession().(*mcp.ServerSession); ok {
					activate.Do(func() { h.b.mu.Lock(); h.b.clients[id] = label; h.b.mu.Unlock(); ready <- ss })
				}
			}
			return next(ctx, method, req)
		}
	})
	h.mu.Unlock()
	go func() {
		defer cancel()
		defer func() { h.mu.Lock(); delete(h.peers, id); h.mu.Unlock(); h.b.disconnect(id) }()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		var ss *mcp.ServerSession
		select {
		case ss = <-ready:
		case <-peerCtx.Done():
		case <-timer.C:
		}
		if ss == nil {
			for s := range peer.server.Sessions() {
				s.Close()
			}
			return
		}
		done := make(chan struct{})
		go func() { ss.Wait(); close(done) }()
		var expiry <-chan time.Time
		if !expires.IsZero() {
			t := time.NewTimer(time.Until(expires))
			defer t.Stop()
			expiry = t.C
		}
		select {
		case <-done:
			return
		case <-peerCtx.Done():
		case <-expiry:
		}
		ss.Close()
		<-done
	}()
	return peer.server
}
func (h *HTTPGateway) revokePrincipal(principal string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, peer := range h.peers {
		if peer != nil && peer.principal == principal {
			peer.cancel()
		}
	}
}
func isLoopbackListen(addr string) bool {
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func httpOrigin(addr, public string, oauth bool, cert, key string) (string, error) {
	if (cert == "") != (key == "") {
		return "", errors.New("provide both --tls-cert and --tls-key")
	}
	if public == "" {
		if oauth {
			return "", errors.New("--oauth requires --public-url https://...")
		}
		if !isLoopbackListen(addr) {
			return "", errors.New("unauthenticated HTTP must bind a literal loopback address")
		}
		scheme := "http"
		if cert != "" {
			scheme = "https"
		}
		public = scheme + "://" + addr
	}
	if !oauth && !isLoopbackListen(addr) {
		return "", errors.New("unauthenticated HTTP must bind loopback; enable --oauth for remote access")
	}
	if oauth && cert == "" && !isLoopbackListen(addr) {
		return "", errors.New("OAuth without direct TLS must bind loopback behind an HTTPS reverse proxy")
	}
	u, e := url.Parse(public)
	if e != nil {
		return "", e
	}
	if oauth && u.Scheme != "https" {
		return "", errors.New("OAuth public endpoints require HTTPS")
	}
	if cert != "" && u.Scheme != "https" {
		return "", errors.New("TLS listener requires an HTTPS public URL")
	}
	return public, nil
}
func startHTTP(ctx context.Context, b *Broker, addr, public string, oauth bool, cert, key, data string) (*http.Server, error) {
	origin, e := httpOrigin(addr, public, oauth, cert, key)
	if e != nil {
		return nil, e
	}
	var p *OAuthProvider
	if oauth {
		p, e = newOAuthProvider(origin, data)
		if e != nil {
			return nil, e
		}
	}
	gateway, e := newHTTPGateway(ctx, b, origin, p)
	if e != nil {
		return nil, e
	}
	if cert != "" {
		if _, e = tls.LoadX509KeyPair(cert, key); e != nil {
			return nil, e
		}
	}
	listener, e := net.Listen("tcp", addr)
	if e != nil {
		return nil, e
	}
	server := &http.Server{Handler: gateway, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 65 * time.Second, MaxHeaderBytes: 32768}
	b.mu.Lock()
	b.oauth = p
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(c)
	}()
	go func() {
		var err error
		if cert != "" {
			err = server.ServeTLS(listener, cert, key)
		} else {
			err = server.Serve(listener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "HTTP MCP stopped: %v\n", err)
		}
	}()
	return server, nil
}
