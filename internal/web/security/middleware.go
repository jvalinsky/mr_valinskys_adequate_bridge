// Package security provides HTTP middleware for admin UI exposure hardening.
package security

import (
	"crypto/subtle"
	"log"

	"github.com/mr_valinskys_adequate_bridge/internal/logutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sensitiveQueryTerms = []string{"pass", "password", "token", "secret", "auth", "key"}

// RequireAuthForBind reports whether listenAddr should require HTTP auth.
func RequireAuthForBind(listenAddr string) bool {
	return !IsLoopbackBindAddr(listenAddr)
}

// IsLoopbackBindAddr reports whether listenAddr resolves to a loopback host.
func IsLoopbackBindAddr(listenAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return false
	}

	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	if host == "" {
		return false
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// BasicAuthMiddleware enforces constant-time HTTP Basic authentication.
func BasicAuthMiddleware(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || !secureCompare(user, username) || !secureCompare(pass, password) {
				w.Header().Set("WWW-Authenticate", `Basic realm="bridge-admin", charset="UTF-8"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogMiddleware logs request metadata with sensitive query values redacted.
func RequestLogMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	logger = logutil.Ensure(logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			path := sanitizedPathWithQuery(r.URL)
			logger.Printf(
				"event=ui_request method=%s path=%q status=%d duration_ms=%d remote=%q",
				r.Method,
				path,
				rec.status,
				time.Since(start).Milliseconds(),
				r.RemoteAddr,
			)
		})
	}
}

func sanitizedPathWithQuery(u *url.URL) string {
	if u == nil {
		return ""
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	if u.RawQuery == "" {
		return path
	}

	q := u.Query()
	for key, values := range q {
		if isSensitiveQueryKey(key) {
			for i := range values {
				values[i] = "REDACTED"
			}
			q[key] = values
		}
	}

	return path + "?" + q.Encode()
}

func isSensitiveQueryKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, term := range sensitiveQueryTerms {
		if strings.Contains(k, term) {
			return true
		}
	}
	return false
}

func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SecurityHeadersMiddleware sets common security response headers.
// When noCache is true it also sets Cache-Control: no-store, which is
// appropriate for authenticated admin pages but not public content.
func SecurityHeadersMiddleware(noCache bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline' 'self'; script-src 'unsafe-inline' 'self'")
			if noCache {
				w.Header().Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}
