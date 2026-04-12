package security

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	collogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
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

func TestCSRFMiddlewareIssuesCookieOnSafeRequest(t *testing.T) {
	handler := CSRFMiddleware(DefaultCSRFConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := CSRFTokenFromContext(r.Context())
		if strings.TrimSpace(token) == "" {
			t.Fatalf("expected csrf token in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/accounts", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	cookie := findCookieByName(rr.Result().Cookies(), DefaultCSRFCookieName)
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatalf("expected csrf cookie %q to be set", DefaultCSRFCookieName)
	}
}

func TestCSRFMiddlewareRejectsUnsafeWithoutToken(t *testing.T) {
	handler := CSRFMiddleware(DefaultCSRFConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cookie := issueCSRFCookie(t, handler)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/accounts/remove", strings.NewReader("at_did=did:plc:alice"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareRejectsMismatchedToken(t *testing.T) {
	handler := CSRFMiddleware(DefaultCSRFConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cookie := issueCSRFCookie(t, handler)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/accounts/remove", strings.NewReader("csrf_token=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareRejectsForeignOrigin(t *testing.T) {
	handler := CSRFMiddleware(DefaultCSRFConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cookie := issueCSRFCookie(t, handler)
	form := url.Values{}
	form.Set(DefaultCSRFFormFieldName, cookie.Value)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/accounts/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.example.com")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareAllowsValidOriginAndToken(t *testing.T) {
	handler := CSRFMiddleware(DefaultCSRFConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cookie := issueCSRFCookie(t, handler)
	form := url.Values{}
	form.Set(DefaultCSRFFormFieldName, cookie.Value)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/accounts/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareAllowsUnsafeExemptPath(t *testing.T) {
	cfg := DefaultCSRFConfig()
	cfg.ExemptPathPrefixes = []string{"/api/atproto"}
	handler := CSRFMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/atproto/repos/track", strings.NewReader(`{"did":"did:plc:alice"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
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

func TestRequestLogMiddlewareRedactionPreservedInOTLPExport(t *testing.T) {
	reqCh := make(chan *collogsv1.ExportLogsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}

		var req collogsv1.ExportLogsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal export request: %v", err)
			return
		}
		select {
		case reqCh <- &req:
		default:
		}

		respBody, _ := proto.Marshal(&collogsv1.ExportLogsServiceResponse{})
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	rt, err := logutil.NewRuntime(logutil.Config{
		Endpoint:    u.Host,
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "bridge-ui",
		CommandName: "serve-ui",
		LocalOutput: "none",
	})
	if err != nil {
		t.Fatalf("new log runtime: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.Shutdown(ctx)
	}()

	mw := RequestLogMiddleware(rt.Logger("ui"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/state?cursor=123&token=abc123&password=shh", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.Shutdown(flushCtx); err != nil {
		t.Fatalf("shutdown log runtime: %v", err)
	}

	select {
	case exportReq := <-reqCh:
		record := findExportedLogRecordByBody(exportReq, "event=ui_request")
		if record == nil {
			t.Fatalf("expected exported ui_request record")
		}
		body := record.GetBody().GetStringValue()
		if strings.Contains(body, "abc123") || strings.Contains(body, "shh") {
			t.Fatalf("expected body redaction, got %q", body)
		}
		if !strings.Contains(body, "REDACTED") {
			t.Fatalf("expected REDACTED marker in body, got %q", body)
		}

		attrs := flattenOTLPAttrs(record.GetAttributes())
		path := attrs["path"]
		if strings.Contains(path, "abc123") || strings.Contains(path, "shh") {
			t.Fatalf("expected path attribute redaction, got %q", path)
		}
		if !strings.Contains(path, "REDACTED") {
			t.Fatalf("expected REDACTED path attribute, got %q (body=%q attrs=%v)", path, body, attrs)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for OTLP export")
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
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "strict-origin-when-cross-origin",
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

func issueCSRFCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected cookie issue request to return 204, got %d", rr.Code)
	}
	cookie := findCookieByName(rr.Result().Cookies(), DefaultCSRFCookieName)
	if cookie == nil {
		t.Fatalf("expected csrf cookie %q to be set", DefaultCSRFCookieName)
	}
	return cookie
}

func findCookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

func findExportedLogRecordByBody(req *collogsv1.ExportLogsServiceRequest, contains string) *logsv1.LogRecord {
	if req == nil {
		return nil
	}
	for _, resourceLogs := range req.ResourceLogs {
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for _, record := range scopeLogs.LogRecords {
				if strings.Contains(record.GetBody().GetStringValue(), contains) {
					return record
				}
			}
		}
	}
	return nil
}

func flattenOTLPAttrs(attrs []*commonv1.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil {
			continue
		}
		out[kv.Key] = anyValueString(kv.Value)
	}
	return out
}

func anyValueString(v *commonv1.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return val.StringValue
	default:
		return ""
	}
}

func TestIsLoopbackBindAddr(t *testing.T) {
	tests := []struct {
		addr     string
		expected bool
	}{
		{"127.0.0.1", false}, // SplitHostPort fails without port
		{"127.0.0.1:8080", true},
		{"localhost", false}, // SplitHostPort fails without port
		{"localhost:8080", true},
		{"[::1]", false}, // SplitHostPort fails without port
		{"[::1]:8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
		{":8080", false}, // All interfaces
		{"invalid", false},
	}

	for _, test := range tests {
		result := IsLoopbackBindAddr(test.addr)
		if result != test.expected {
			t.Errorf("IsLoopbackBindAddr(%q) = %v, want %v", test.addr, result, test.expected)
		}
	}
}

func TestSanitizedPathWithQuery(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/path?key=value&password=secret&token=abc&other=123", nil)
	result := sanitizedPathWithQuery(req.URL)

	if !strings.Contains(result, "/path") {
		t.Errorf("Expected path, got %v", result)
	}
	if strings.Contains(result, "secret") {
		t.Errorf("Expected password to be redacted, got %v", result)
	}
	if strings.Contains(result, "abc") {
		t.Errorf("Expected token to be redacted, got %v", result)
	}
	if !strings.Contains(result, "other=123") {
		t.Errorf("Expected other param to be present, got %v", result)
	}

	// Test request with nil URL
	reqNil := &http.Request{}
	resultNil := sanitizedPathWithQuery(reqNil.URL)
	if resultNil != "" {
		t.Errorf("Expected empty string for nil URL, got %v", resultNil)
	}
}

func TestSanitizedPathWithQueryEdgeCases(t *testing.T) {
	// Test empty path gets converted to "/"
	u1, _ := url.Parse("http://example.com")
	u1.Path = ""
	result1 := sanitizedPathWithQuery(u1)
	if result1 != "/" {
		t.Errorf("Expected '/', got %q", result1)
	}

	// Test no query string returns just path
	u2, _ := url.Parse("http://example.com/just-path")
	result2 := sanitizedPathWithQuery(u2)
	if result2 != "/just-path" {
		t.Errorf("Expected '/just-path', got %q", result2)
	}
}
