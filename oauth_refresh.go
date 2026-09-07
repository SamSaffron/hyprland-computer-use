package main

import (
	"net/http"
	"slices"
)

// Refresh rotates single-use credentials, but cannot extend the one-hour
// connection approved locally. Reuse revokes the entire token family.
func (p *OAuthProvider) refreshToken(w http.ResponseWriter, r *http.Request, c OAuthClient) {
	if !slices.Contains(c.GrantTypes, "refresh_token") {
		oauthError(w, 400, "unauthorized_client", "client is not registered for refresh tokens")
		return
	}
	if resource := r.PostForm.Get("resource"); resource != "" && resource != p.resource {
		oauthError(w, 400, "invalid_target", "refresh cannot change the MCP resource")
		return
	}
	if scope := r.PostForm.Get("scope"); scope != "" && scope != connectScope {
		oauthError(w, 400, "invalid_scope", "refresh cannot expand scopes")
		return
	}
	key := digest(r.PostForm.Get("refresh_token"))
	p.mu.Lock()
	p.cleanupLocked()
	rt, ok := p.refresh[key]
	if !ok || rt.Connection.ClientID != c.ID || rt.Connection.Resource != p.resource {
		p.mu.Unlock()
		oauthError(w, 400, "invalid_grant", "invalid or expired refresh token")
		return
	}
	if rt.Used {
		id := rt.Connection.ID
		p.mu.Unlock()
		p.revokeConnection(id)
		oauthError(w, 400, "invalid_grant", "refresh token reuse revoked this connection")
		return
	}
	remaining := int(rt.Connection.Expires.Sub(p.now()).Seconds())
	if remaining < 1 {
		p.mu.Unlock()
		oauthError(w, 400, "invalid_grant", "local connection approval expired")
		return
	}
	if len(p.tokens) >= 256 || len(p.refresh) >= 1024 {
		p.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "token limit reached")
		return
	}
	access, refresh := secret(), secret()
	rt.Used = true
	p.refresh[key] = rt
	p.refresh[digest(refresh)] = oauthRefresh{Connection: rt.Connection}
	p.tokens[digest(access)] = rt.Connection
	p.mu.Unlock()
	oauthJSON(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": remaining, "scope": connectScope})
}
