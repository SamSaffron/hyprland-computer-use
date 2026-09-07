package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"
)

func TestOAuthRefreshRotationReplayAndConsentLifetime(t *testing.T) {
	for _, method := range []string{"none", "client_secret_basic"} {
		t.Run(method, func(t *testing.T) {
			f := newOAuthFixture(t, true)
			body, _ := json.Marshal(map[string]any{"client_name": "Refresh client", "redirect_uris": []string{"http://127.0.0.1:45678/callback"}, "token_endpoint_auth_method": method, "grant_types": []string{"authorization_code", "refresh_token"}})
			status, _, raw := f.request(t, "POST", "/register", bytes.NewReader(body), nil)
			if status != 201 {
				t.Fatalf("register %d %s", status, raw)
			}
			var c map[string]any
			json.Unmarshal(raw, &c)
			form := f.approvedCode(t, c)
			status, raw = f.exchange(t, form, c)
			if status != 200 {
				t.Fatalf("exchange %d %s", status, raw)
			}
			var original map[string]any
			json.Unmarshal(raw, &original)
			old := original["access_token"].(string)
			before, e := f.p.verify(f.ctx, old, nil)
			if e != nil {
				t.Fatal(e)
			}
			refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {c["client_id"].(string)}, "refresh_token": {original["refresh_token"].(string)}}
			refresh.Set("resource", "https://wrong.example/mcp")
			if status, _ = f.exchange(t, refresh, c); status != 400 {
				t.Fatal("refresh changed audience")
			}
			refresh.Del("resource")
			refresh.Set("scope", "admin")
			if status, _ = f.exchange(t, refresh, c); status != 400 {
				t.Fatal("refresh expanded scope")
			}
			refresh.Del("scope")
			status, raw = f.exchange(t, refresh, c)
			if status != 200 {
				t.Fatalf("refresh %d %s", status, raw)
			}
			var rotated map[string]any
			json.Unmarshal(raw, &rotated)
			if rotated["refresh_token"] == original["refresh_token"] {
				t.Fatal("refresh did not rotate")
			}
			after, e := f.p.verify(f.ctx, rotated["access_token"].(string), nil)
			if e != nil || !after.Expiration.Equal(before.Expiration) || after.UserID != before.UserID {
				t.Fatal("refresh extended consent or changed principal")
			}
			_, connections := f.p.state()
			if len(connections) != 1 {
				t.Fatal("duplicate OAuth UI connections")
			}
			if status, _ = f.exchange(t, refresh, c); status != 400 {
				t.Fatal("refresh replay accepted")
			}
			for _, token := range []string{old, rotated["access_token"].(string)} {
				if _, e = f.p.verify(f.ctx, token, nil); e == nil {
					t.Fatal("replay did not revoke token family")
				}
			}
		})
	}
}
