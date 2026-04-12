package xrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLexDoQuery tests query (GET) requests.
func TestLexDoQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/xrpc/com.example.echo" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("message") != "hello" {
			t.Errorf("missing query param: %s", r.URL.Query().Get("message"))
		}
		response := map[string]string{"echo": "hello"}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.example.echo", map[string]any{"message": "hello"}, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
	if result["echo"] != "hello" {
		t.Errorf("wrong response: %v", result)
	}
}

// TestLexDoProcedure tests procedure (POST) requests.
func TestLexDoProcedure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["input"] != "world" {
			t.Errorf("wrong body: %v", body)
		}
		response := map[string]string{"output": "world"}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var result map[string]string
	err := client.LexDo(context.Background(), "procedure", "application/json", "com.example.process",
		nil, map[string]string{"input": "world"}, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
	if result["output"] != "world" {
		t.Errorf("wrong response: %v", result)
	}
}

// TestLexDoBearerAuth tests Bearer token authentication.
func TestLexDoBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer mytoken" {
			t.Errorf("wrong auth header: %s", authHeader)
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{
		Host: server.URL,
		Auth: &AuthInfo{AccessJwt: "mytoken"},
	}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.example.auth", nil, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoAdminAuth tests Basic authentication for admin endpoints.
func TestLexDoAdminAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		// Should be "Basic admin:secrettoken" base64 encoded
		if !bytes.HasPrefix([]byte(authHeader), []byte("Basic ")) {
			t.Errorf("wrong auth header: %s", authHeader)
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	adminToken := "secrettoken"
	client := &Client{
		Host:       server.URL,
		AdminToken: &adminToken,
	}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.atproto.admin.getUser", nil, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoCustomHeaders tests custom headers.
func TestLexDoCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value" {
			t.Errorf("missing custom header")
		}
		if r.Header.Get("User-Agent") != "custom-agent" {
			t.Errorf("wrong user agent: %s", r.Header.Get("User-Agent"))
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	userAgent := "custom-agent"
	client := &Client{
		Host:      server.URL,
		UserAgent: &userAgent,
		Headers:   map[string]string{"X-Custom": "value"},
	}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.example.test", nil, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoRateLimitParsing tests rate limit header parsing.
func TestLexDoRateLimitParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ratelimit-limit", "100")
		w.Header().Set("ratelimit-remaining", "50")
		w.Header().Set("ratelimit-reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
		w.Header().Set("ratelimit-policy", "throttled")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "overLimit"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.example.test", nil, nil, &result)
	if err == nil {
		t.Error("expected error for rate limit")
	}

	xrpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("wrong error type: %T", err)
	}
	if xrpcErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("wrong status code: %d", xrpcErr.StatusCode)
	}
	if xrpcErr.Ratelimit == nil {
		t.Error("ratelimit info missing")
	} else {
		if xrpcErr.Ratelimit.Limit != 100 {
			t.Errorf("wrong limit: %d", xrpcErr.Ratelimit.Limit)
		}
		if xrpcErr.Ratelimit.Remaining != 50 {
			t.Errorf("wrong remaining: %d", xrpcErr.Ratelimit.Remaining)
		}
		if xrpcErr.Ratelimit.Policy != "throttled" {
			t.Errorf("wrong policy: %s", xrpcErr.Ratelimit.Policy)
		}
	}
}

// TestLexDoErrorHandling tests error response parsing.
func TestLexDoErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(XRPCError{
			ErrStr:  "InvalidRequest",
			Message: "Missing required parameter",
		})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	err := client.LexDo(context.Background(), "query", "", "com.example.test", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	xrpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("wrong error type: %T", err)
	}
	if xrpcErr.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong status code: %d", xrpcErr.StatusCode)
	}
}

// TestLexDoParameterEncoding tests query parameter encoding.
func TestLexDoParameterEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("string") != "hello world" {
			t.Errorf("wrong string param: %s", query.Get("string"))
		}
		if query.Get("number") != "42" {
			t.Errorf("wrong number param: %s", query.Get("number"))
		}
		// Array parameters
		values := query["array"]
		if len(values) != 3 {
			t.Errorf("wrong array length: %d", len(values))
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var result map[string]string
	params := map[string]any{
		"string": "hello world",
		"number": 42,
		"array":  []string{"a", "b", "c"},
	}
	err := client.LexDo(context.Background(), "query", "", "com.example.test", params, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoBufferResponse tests reading response into a Buffer.
func TestLexDoBufferResponse(t *testing.T) {
	expectedBody := []byte("binary data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(expectedBody)
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var buf bytes.Buffer
	err := client.LexDo(context.Background(), "query", "", "com.example.binary", nil, nil, &buf)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), expectedBody) {
		t.Errorf("wrong response: %s", buf.String())
	}
}

// TestLexDoBodyHandling tests request body encoding.
func TestLexDoBodyHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Body should be properly set
		body, _ := io.ReadAll(r.Body)
		var data map[string]string
		json.Unmarshal(body, &data)
		if data["test"] != "value" {
			t.Errorf("wrong body: %v", data)
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	var result map[string]string
	err := client.LexDo(context.Background(), "procedure", "application/json", "com.example.test",
		nil, map[string]string{"test": "value"}, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoReaderBody tests request with io.Reader body.
func TestLexDoReaderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("wrong content type: %s", r.Header.Get("Content-Type"))
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	bodyData := bytes.NewReader([]byte("binary"))
	var result map[string]string
	err := client.LexDo(context.Background(), "procedure", "application/octet-stream", "com.example.test",
		nil, bodyData, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoURLPath tests URL path construction.
func TestLexDoURLPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/com.atproto.server.createSession" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL + "/"}
	var result map[string]string
	err := client.LexDo(context.Background(), "query", "", "com.atproto.server.createSession", nil, nil, &result)
	if err != nil {
		t.Fatalf("LexDo failed: %v", err)
	}
}

// TestLexDoContextCancellation tests context cancellation.
func TestLexDoContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	var result map[string]string
	err := client.LexDo(ctx, "query", "", "com.example.test", nil, nil, &result)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestXRPCErrorString tests error formatting.
func TestXRPCErrorString(t *testing.T) {
	tests := []struct {
		err     *XRPCError
		want    string
	}{
		{&XRPCError{ErrStr: "NotFound"}, "NotFound"},
		{&XRPCError{ErrStr: "Invalid", Message: "Bad request"}, "Invalid: Bad request"},
		{nil, "unknown xrpc error"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.err), func(t *testing.T) {
			if tt.err.Error() != tt.want {
				t.Errorf("got %q, want %q", tt.err.Error(), tt.want)
			}
		})
	}
}

// BenchmarkLexDo benchmarks the LexDo method.
func BenchmarkLexDo(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := &Client{Host: server.URL}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result map[string]string
		client.LexDo(context.Background(), "query", "", "com.example.test", nil, nil, &result)
	}
}
