package security

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireAuthForBind(t *testing.T) {
	tests := []struct {
		addr        string
		requireAuth bool
	}{
		{addr: "127.0.0.1:8080", requireAuth: false},
		{addr: "localhost:8080", requireAuth: false},
		{addr: "[::1]:8080", requireAuth: false},
		{addr: "0.0.0.0:8080", requireAuth: true},
		{addr: "[::]:8080", requireAuth: true},
		{addr: "example.com:8080", requireAuth: true},
	}

	for _, tc := range tests {
		if got := RequireAuthForBind(tc.addr); got != tc.requireAuth {
			t.Fatalf("addr=%s expected requireAuth=%v got=%v", tc.addr, tc.requireAuth, got)
		}
	}
}

func TestBasicAuthMiddleware(t *testing.T) {
	mw := BasicAuthMiddleware("admin", "s3cr3t")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("rejects missing auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("rejects wrong auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("admin", "wrong")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("accepts correct auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("admin", "s3cr3t")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rr.Code)
		}
	})
}

func TestRequestLogMiddlewareRedactsSensitiveQueryFields(t *testing.T) {
	var buf bytes.Buffer
	logger := logBuffer(&buf)
	mw := RequestLogMiddleware(logger)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/state?cursor=123&token=abc123&password=shh", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	logLine := buf.String()
	if strings.Contains(logLine, "abc123") || strings.Contains(logLine, "shh") {
		t.Fatalf("expected sensitive values to be redacted, got log: %s", logLine)
	}
	if !strings.Contains(logLine, "REDACTED") {
		t.Fatalf("expected log to include redacted marker, got: %s", logLine)
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("sets base security headers", func(t *testing.T) {
		handler := SecurityHeadersMiddleware(false)(inner)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		expected := map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":       "DENY",
			"Referrer-Policy":       "strict-origin-when-cross-origin",
			"Content-Security-Policy": "default-src 'self'; style-src 'unsafe-inline' 'self'; script-src 'unsafe-inline' 'self'",
		}
		for header, want := range expected {
			if got := rr.Header().Get(header); got != want {
				t.Fatalf("header %s: expected %q, got %q", header, want, got)
			}
		}
		if cc := rr.Header().Get("Cache-Control"); cc != "" {
			t.Fatalf("expected no Cache-Control header, got %q", cc)
		}
	})

	t.Run("noCache adds Cache-Control no-store", func(t *testing.T) {
		handler := SecurityHeadersMiddleware(true)(inner)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if got := rr.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("expected Cache-Control: no-store, got %q", got)
		}
		if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("expected X-Frame-Options: DENY, got %q", got)
		}
	})
}

func logBuffer(buf *bytes.Buffer) *log.Logger {
	return log.New(buf, "", 0)
}
