// Package security provides HTTP middleware for admin UI exposure hardening.
package security

import (
	"context"
	crypto_rand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sensitiveQueryTerms = []string{"pass", "password", "token", "secret", "auth", "key"}

const (
	// DefaultCSRFCookieName is the session cookie that stores the CSRF token.
	DefaultCSRFCookieName = "bridge_csrf_token"
	// DefaultCSRFFormFieldName is the hidden form field used by HTML forms.
	DefaultCSRFFormFieldName = "csrf_token"
)

type csrfTokenContextKey struct{}

// CSRFConfig configures CSRF middleware behavior.
type CSRFConfig struct {
	CookieName         string
	FormFieldName      string
	ExemptPathPrefixes []string
}

// DefaultCSRFConfig returns defaults suitable for server-rendered form flows.
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		CookieName:    DefaultCSRFCookieName,
		FormFieldName: DefaultCSRFFormFieldName,
	}
}

// CSRFTokenFromContext returns the token set by CSRFMiddleware for template rendering.
func CSRFTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if token, ok := ctx.Value(csrfTokenContextKey{}).(string); ok {
		return token
	}
	return ""
}

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

// CSRFMiddleware enforces same-origin plus synchronizer-token checks for unsafe methods.
func CSRFMiddleware(cfg CSRFConfig) func(http.Handler) http.Handler {
	cfg = normalizeCSRFConfig(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("event=csrf_check path=%s method=%s origin=%q referer=%q host=%s ua=%s",
				r.URL.Path, r.Method, r.Header.Get("Origin"), r.Header.Get("Referer"), r.Host, r.UserAgent())

			token := csrfCookieValue(r, cfg.CookieName)
			if !validCSRFToken(token) {
				generated, err := newCSRFToken()
				if err != nil {
					http.Error(w, "Unable to initialize CSRF protection", http.StatusInternalServerError)
					return
				}
				token = generated
			}

			setCSRFCookie(w, r, cfg.CookieName, token)
			r = r.WithContext(context.WithValue(r.Context(), csrfTokenContextKey{}, token))

			if !csrfUnsafeMethod(r.Method) || csrfPathExempt(r.URL.Path, cfg.ExemptPathPrefixes) {
				next.ServeHTTP(w, r)
				return
			}
			if !sameOriginRequest(r) {
				log.Printf("event=csrf_reject reason=no_valid_origin path=%s method=%s origin=%q referer=%q host=%s",
					r.URL.Path, r.Method, r.Header.Get("Origin"), r.Header.Get("Referer"), r.Host)
				w.Header().Set("X-CSRF-Reject-Reason", "no_valid_origin")
				http.Error(w, "Forbidden: no valid origin", http.StatusForbidden)
				return
			}

			formToken := strings.TrimSpace(r.FormValue(cfg.FormFieldName))
			if !secureCompare(formToken, token) {
				log.Printf("event=csrf_reject reason=token_mismatch path=%s method=%s has_cookie=%t has_form=%t",
					r.URL.Path, r.Method, token != "", formToken != "")
				http.Error(w, "Forbidden: token mismatch", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func normalizeCSRFConfig(cfg CSRFConfig) CSRFConfig {
	if strings.TrimSpace(cfg.CookieName) == "" {
		cfg.CookieName = DefaultCSRFCookieName
	}
	if strings.TrimSpace(cfg.FormFieldName) == "" {
		cfg.FormFieldName = DefaultCSRFFormFieldName
	}
	return cfg
}

func csrfCookieValue(r *http.Request, cookieName string) string {
	c, err := r.Cookie(cookieName)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func validCSRFToken(token string) bool {
	token = strings.TrimSpace(token)
	return token != "" && len(token) <= 512
}

func newCSRFToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := crypto_rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func setCSRFCookie(w http.ResponseWriter, r *http.Request, cookieName, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func csrfUnsafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func csrfPathExempt(path string, prefixes []string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}

	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func sameOriginRequest(r *http.Request) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xfp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xfp != "" {
		scheme = xfp
	}

	host := strings.TrimSpace(r.Host)
	if xfh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xfh != "" {
		host = xfh
	}

	if host == "" {
		log.Printf("event=same_origin_check reason=empty_host r.Host=%q X-Forwarded-Host=%q", r.Host, r.Header.Get("X-Forwarded-Host"))
		return false
	}
	log.Printf("event=same_origin_check target_scheme=%q target_host=%q", scheme, host)

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		match := originMatchesRequest(origin, scheme, host)
		log.Printf("event=same_origin_check origin_header=%q match=%t", origin, match)
		return match
	}

	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer == "" {
		return false
	}
	refURL, err := url.Parse(referer)
	if err != nil {
		return false
	}
	if refURL.Scheme == "" || refURL.Host == "" {
		return false
	}
	return originMatchesRequest(refURL.Scheme+"://"+refURL.Host, scheme, host)
}

func originMatchesRequest(rawOrigin, scheme, host string) bool {
	originURL, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil {
		log.Printf("event=origin_check reason=parse_failed origin=%q err=%v", rawOrigin, err)
		return false
	}
	if originURL.Scheme == "" || originURL.Host == "" {
		log.Printf("event=origin_check reason=empty_scheme_or_host origin=%q parsed_scheme=%q parsed_host=%q", rawOrigin, originURL.Scheme, originURL.Host)
		return false
	}
	schemeMatch := strings.EqualFold(originURL.Scheme, scheme)
	hostMatch := strings.EqualFold(originURL.Host, host)
	log.Printf("event=origin_check origin=%q scheme=%q target=%q match=%t host_origin=%q host_target=%q match=%t",
		rawOrigin, originURL.Scheme, scheme, schemeMatch, originURL.Host, host, hostMatch)
	if !schemeMatch {
		return false
	}
	return hostMatch
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
