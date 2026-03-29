package muxrpc

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

type mockHandler struct {
	handled bool
}

func (h *mockHandler) Handled(m Method) bool {
	return m.String() == "test.hello"
}

func (h *mockHandler) HandleCall(ctx context.Context, req *Request) {
	h.handled = true
	if req.Method.String() == "test.hello" {
		req.Return(ctx, "hello world")
	}
}

func (h *mockHandler) HandleConnect(ctx context.Context, edp Endpoint) {}

func TestRPCRoundTrip(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := &mockHandler{}
	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	// Test Async
	var res string
	err := client.Async(ctx, &res, TypeString, Method{"test", "hello"})
	if err != nil {
		t.Fatalf("Async failed: %v", err)
	}

	if res != "hello world" {
		t.Errorf("got res %q, want %q", res, "hello world")
	}

	if !handler.handled {
		t.Error("handler NOT invoked")
	}
}

func TestManifest(t *testing.T) {
	m := NewManifest()
	m.RegisterAsync("test.hello")
	
	ok, found := m.Handled(Method{"test", "hello"})
	if !ok || !found {
		t.Error("should be handled")
	}

	b, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	var entries []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
}

func TestMuxrpcMisc(t *testing.T) {
	m := Method{"a", "b"}
	if m.String() != "a.b" {
		t.Errorf("got %s", m.String())
	}

	p := ParseMethod("c.d")
	if len(p) != 2 || p[0] != "c" {
		t.Errorf("got %v", p)
	}

	f, _ := TypeJSON.AsCodecFlag()
	if !f.Get(codec.FlagJSON) {
		t.Error("missing JSON flag")
	}
}
