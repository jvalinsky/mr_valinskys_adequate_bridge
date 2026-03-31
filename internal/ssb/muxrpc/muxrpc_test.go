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
	s := m.String()
	return s == "test.hello" || s == "test.source" || s == "test.sink" || s == "test.duplex"
}

func (h *mockHandler) HandleCall(ctx context.Context, req *Request) {
	h.handled = true
	switch req.Method.String() {
	case "test.hello":
		req.Return(ctx, "hello world")
	case "test.source":
		sink, _ := req.ResponseSink()
		sink.Write([]byte("item 1"))
		sink.Write([]byte("item 2"))
		sink.Close()
	case "test.sink":
		src, _ := req.ResponseSource()
		for src.Next(ctx) {
			_, _ = src.Bytes()
		}
		req.Return(ctx, "done")
	case "test.duplex":
		src, _ := req.ResponseSource()
		sink, _ := req.ResponseSink()
		if src.Next(ctx) {
			b, _ := src.Bytes()
			sink.Write(b)
		}
		sink.Close()
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

func TestStreams(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handler := &mockHandler{}
	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	// Test Source
	t.Run("Source", func(t *testing.T) {
		src, err := client.Source(ctx, TypeBinary, Method{"test", "source"})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for src.Next(ctx) {
			_, err := src.Bytes()
			if err != nil {
				break
			}
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 items, got %d", count)
		}
	})

	// Test Sink
	t.Run("Sink", func(t *testing.T) {
		sink, err := client.Sink(ctx, TypeBinary, Method{"test", "sink"})
		if err != nil {
			t.Fatal(err)
		}
		sink.Write([]byte("data"))
		sink.Close()
	})

	// Test Duplex
	t.Run("Duplex", func(t *testing.T) {
		src, sink, err := client.Duplex(ctx, TypeBinary, Method{"test", "duplex"})
		if err != nil {
			t.Fatal(err)
		}
		msg := []byte("ping")
		sink.Write(msg)
		if src.Next(ctx) {
			got, _ := src.Bytes()
			if string(got) != string(msg) {
				t.Errorf("got %s, want %s", got, msg)
			}
		}
		sink.Close()
	})
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
