package security

import (
	"crypto/subtle"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sensitiveQueryTerms = []string{"pass", "password", "token", "secret", "auth", "key"}

func RequireAuthForBind(listenAddr string) bool {
	return !IsLoopbackBindAddr(listenAddr)
}

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

func RequestLogMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}
