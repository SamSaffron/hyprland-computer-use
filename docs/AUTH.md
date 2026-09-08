# Optional HTTP MCP and built-in OAuth

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

OAuth is a **mode you opt into**, not a prerequisite for local use. No external identity provider or API-key provisioning is needed in built-in-provider mode. The desktop owner authorizes connections in the local Quickshell console.

## Modes

| Mode | Start | Network/auth behavior |
|---|---|---|
| Local default | `hyprland-computer-use serve` | Private Unix sockets; use `hyprland-computer-use mcp` as a stdio bridge. No HTTP listener or OAuth endpoints. |
| Local HTTP | `hyprland-computer-use serve --http 127.0.0.1:8099` | Streamable HTTP at `/mcp`, without OAuth. Literal loopback binding only. |
| Built-in OAuth | `hyprland-computer-use serve --http ... --oauth --public-url https://...` | OAuth-protected Streamable HTTP, local consent, standard discovery and client registration. |

Open the permission console from the tray, or run `hyprland-computer-use console` in the same desktop session; see the [quick start](../README.md). All modes expose the same ten MCP tools and desktop permission policy. OAuth is for the HTTP transport; the stdio bridge does not acquire an OAuth login requirement.

### Direct HTTPS

```sh
./build/hyprland-computer-use serve \
  --http 0.0.0.0:8443 \
  --public-url https://desktop.example.com:8443 \
  --oauth \
  --tls-cert /path/to/fullchain.pem \
  --tls-key /path/to/private-key.pem
```

Use a certificate trusted by the MCP client/browser. The disposable-lab test used its own explicitly trusted test CA; it did not disable TLS verification.

### Behind an HTTPS reverse proxy

```sh
./build/hyprland-computer-use serve \
  --http 127.0.0.1:8099 \
  --public-url https://desktop.example.com \
  --oauth
```

Proxy the complete origin, including discovery and OAuth routes, not only `/mcp`. Preserve the configured public `Host`. The proxy owns TLS termination; keep the unencrypted backend bound to loopback. Forwarded headers do not determine the issuer, resource URL or accepted origin.

Point an OAuth-capable Streamable HTTP MCP client at `https://desktop.example.com/mcp`. The provider is discovered automatically from the 401 challenge and metadata.

## Connection flow

1. An unauthenticated MCP request receives **401** with `WWW-Authenticate: Bearer resource_metadata="…"` and scope `mcp:connect`.
2. The client discovers the resource and authorization server and dynamically registers, if necessary.
3. The client opens the authorization URL in a browser, using the authorization-code flow with **PKCE S256** and the MCP resource indicator.
4. The browser shows a waiting page. **Only the local console can approve or deny**; no remote HTTP approval endpoint exists. The console labels the app name as unverified and displays its exact callback URI, including the warning that loopback callbacks do not establish app identity.
5. After local approval, the same browser (bound with an HttpOnly, Secure, SameSite cookie) receives a one-use code at the exact registered redirect URI. OAuth state and issuer are preserved.
6. Token exchange grants **MCP connection access for one hour**, not desktop authority. The client can discover window metadata, then request or receive proactive window grants normally.
7. **Revoke connection** invalidates that OAuth token family, terminates its HTTP MCP sessions, and revokes/cancels their desktop grants and recordings.

**Pause / Revoke desktop grants is not OAuth logout.** Window metadata remains available to authenticated clients while paused. Use **Revoke connection** to remove connection-level access too.

## Implemented protocol surface

Based on the MCP [2025-11-25 authorization specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization):

- RFC9728 protected-resource metadata at `/.well-known/oauth-protected-resource` and `/.well-known/oauth-protected-resource/mcp`.
- RFC8414 authorization-server metadata at `/.well-known/oauth-authorization-server`.
- RFC7591 dynamic client registration at `/register`: public clients (`none`) and confidential clients (`client_secret_basic`). The omitted-method default is `client_secret_basic`.
- `/authorize`: authorization-code flow only, PKCE S256 required, exact registered redirects, explicit MCP resource, fixed `mcp:connect` scope. No implicit or password grant.
- `/token`: one-use code exchange, client/redirect/PKCE/resource binding; opaque, resource-bound access tokens. Tokens are accepted only in the Authorization header, not URL query parameters.
- Rotating, single-use refresh tokens for clients registered for `refresh_token`. Reuse revokes the whole token family. **Refresh cannot extend the locally approved one-hour connection lifetime** or change resource/scope; a new local approval is required after expiry.
- `/revoke`: RFC7009-style access/refresh-token revocation; local console revocation is also supported.
- RFC9207 issuer identification in authorization responses.
- Official Go SDK Streamable HTTP transport and bearer-token middleware. HTTP sessions are bound to the authenticated authorization grant, not trusted merely because a client supplies a session ID.

Client ID Metadata Documents (CIMD) are not implemented; clients must support dynamic registration. This avoids introducing arbitrary metadata-URL fetching/SSRF in the authorization server. This is a tested implementation of the discovery/code/PKCE flow, **not an independent OAuth security certification**.

## Lifecycle and storage

- Client registrations persist in private `oauth-clients.json` under the broker data directory. Confidential client secrets are stored only as hashes; the registration response is the one-time plaintext delivery.
- Access tokens, refresh tokens, codes and local OAuth approvals are memory-only. Restarting the broker invalidates them, but retains client registration so clients can reauthorize.
- Authorization requests expire after five minutes; issued codes after one minute. Local OAuth connection approval lasts one hour.
- Unused initialize-only HTTP probes are not shown as sharing recipients and are closed after ten seconds. Clients appear after beginning tool discovery/use. This also handles an initialize probe made by the SDK's OAuth retry flow without presenting a phantom second agent.
- HTTP DELETE terminates an MCP session. Abruptly vanishing HTTP clients are cleaned up after the SDK's five-minute idle-session timeout; a TCP disconnect alone is not a reliable HTTP-session logout signal. Stdio EOF still revokes immediately.
- Bounds: 256 persisted registrations, 16 pending authorization requests, 64 codes, 256 access tokens, 1,024 refresh-history entries, and 64 pending/live HTTP sessions. Registration abuse and public-internet deployment need further hardening/audit.
- Host validation uses the explicit configured origin. Foreign browser Origins are rejected; no wildcard CORS or remote UI socket is exposed. Native/server-side MCP clients need no Origin exception.
- Same-user desktop processes remain trusted, including a shell reached through a controlled terminal. OAuth does not create an OS sandbox.
