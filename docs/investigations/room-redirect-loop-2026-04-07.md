# Investigation: room.snek.cc Redirect Loop (2026-04-07)

## Symptom
Users attempting to access `https://room.snek.cc/` were met with an `ERR_TOO_MANY_REDIRECTS` error in their browser.

## Diagnosis
The issue was caused by a conflict between the bridge's internal HTTPS enforcement logic and the reverse proxy (Caddy) configuration on `snek.cc`.

1.  **Internal Redirect Logic**: The `httpRedirectHandler` in `internal/room/runtime.go` was designed to redirect any non-TLS request to HTTPS if `httpsDomain` was configured.
2.  **Proxy Behavior**: Caddy terminates SSL at the edge and communicates with the bridge over plain HTTP (`127.0.0.1:8976`).
3.  **Conflict**: Because the bridge only checked `r.TLS == nil`, it perceived all proxied requests as insecure and issued a 301 redirect to `https://room.snek.cc`. The browser would follow this redirect, hit Caddy again, and the loop would continue.

## Resolution
Modified `internal/room/runtime.go` to make the `httpRedirectHandler` aware of reverse proxies.

The handler now checks for the `X-Forwarded-Proto: https` header. If this header is present, the handler assumes the request was already secured by a proxy and skips the redirect.

```go
if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
    // ... perform redirect ...
}
```

## Verification
- **Unit Test**: Created a test case to verify that `X-Forwarded-Proto: https` successfully suppresses the redirect.
- **Local Test**: Verified that local/loopback requests (which don't use the proxy) still behave correctly.
- **Production**: Deployment to `snek.cc` (pending `nixos-rebuild`) will resolve the issue.

## Prevention
- Always check for proxy headers when implementing "force HTTPS" logic in applications intended to run behind a reverse proxy.
- Ensure reverse proxies (Caddy, Nginx) are configured to send `X-Forwarded-Proto` (Caddy does this by default).
