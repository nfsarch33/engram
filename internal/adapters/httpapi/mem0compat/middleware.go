// Package mem0compat exposes a Mem0 OSS wire-compatible HTTP surface backed
// by the Engram service. It exists so any client (Mem0 OSS Go MCP wrapper,
// the Mem0 OSS web UI, ad-hoc curl scripts) can target Engram with no code
// change beyond pointing MEM0_BASE_URL at the shim port.
//
// The shim does NOT introduce a new dependency; everything is net/http and
// encoding/json. The handler delegates all business logic to engramsvc.
package mem0compat

import (
	"crypto/subtle"
	"net/http"
)

// APIKeyMiddleware enforces the X-API-Key header on every request except the
// public probes (/healthz and /auth/setup-status). An empty configured key
// disables the gate entirely; this matches engramd's existing surface where
// the API key is optional during development.
//
// The constant-time compare prevents trivial timing oracles even though the
// key is not high-value (operators rotate it freely).
func APIKeyMiddleware(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expected == "" || isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			got := r.Header.Get("X-API-Key")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isPublicPath returns true for endpoints the Mem0 OSS UI and mem0-mcp-go
// probe before injecting credentials.
func isPublicPath(p string) bool {
	switch p {
	case "/healthz", "/auth/setup-status":
		return true
	}
	return false
}
