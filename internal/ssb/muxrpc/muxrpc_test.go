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

func TestManifestAllCallTypes(t *testing.T) {
	m := NewManifest()
	m.RegisterAsync("test.async")
	m.RegisterSource("test.source")
	m.RegisterSink("test.sink")
	m.RegisterDuplex("test.duplex")
	m.RegisterSync("test.sync")

	b, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	type manifestEntry struct {
		Type  string   `json:"type"`
		Names []string `json:"names"`
	}
	var entries []manifestEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	typeMap := make(map[string][]string)
	for _, e := range entries {
		typeMap[e.Type] = e.Names
	}

	if names, ok := typeMap["async"]; !ok {
		t.Error("missing async in manifest")
	} else if len(names) != 1 || names[0] != "test.async" {
		t.Errorf("unexpected async names: %v", names)
	}

	if names, ok := typeMap["source"]; !ok {
		t.Error("missing source in manifest")
	} else if len(names) != 1 || names[0] != "test.source" {
		t.Errorf("unexpected source names: %v", names)
	}

	if names, ok := typeMap["sink"]; !ok {
		t.Error("missing sink in manifest")
	} else if len(names) != 1 || names[0] != "test.sink" {
		t.Errorf("unexpected sink names: %v", names)
	}

	if names, ok := typeMap["duplex"]; !ok {
		t.Error("missing duplex in manifest")
	} else if len(names) != 1 || names[0] != "test.duplex" {
		t.Errorf("unexpected duplex names: %v", names)
	}

	if names, ok := typeMap["sync"]; !ok {
		t.Error("missing sync in manifest")
	} else if len(names) != 1 || names[0] != "test.sync" {
		t.Errorf("unexpected sync names: %v", names)
	}
}

func TestManifestWireJSONUsesNestedMuxrpcShape(t *testing.T) {
	m := NewManifest()
	m.RegisterSync("manifest")
	m.RegisterSource("createHistoryStream")
	m.RegisterDuplex("ebt.replicate")
	m.RegisterAsync("room.metadata")

	b, err := m.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal wire manifest: %v", err)
	}
	if got["manifest"] != "sync" {
		t.Fatalf("manifest = %#v, want sync", got["manifest"])
	}
	if got["createHistoryStream"] != "source" {
		t.Fatalf("createHistoryStream = %#v, want source", got["createHistoryStream"])
	}
	ebt, ok := got["ebt"].(map[string]interface{})
	if !ok || ebt["replicate"] != "duplex" {
		t.Fatalf("ebt.replicate = %#v, want duplex", got["ebt"])
	}
	room, ok := got["room"].(map[string]interface{})
	if !ok || room["metadata"] != "async" {
		t.Fatalf("room.metadata = %#v, want async", got["room"])
	}
}

func TestRPCManifestBuiltIn(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manifest := NewManifest()
	manifest.RegisterSync("manifest")
	manifest.RegisterSource("createHistoryStream")

	server := NewRPC(ctx, p2, nil, manifest)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	var got map[string]interface{}
	if err := client.Sync(ctx, &got, TypeJSON, Method{"manifest"}); err != nil {
		t.Fatalf("manifest sync failed: %v", err)
	}
	if got["manifest"] != "sync" || got["createHistoryStream"] != "source" {
		t.Fatalf("unexpected manifest response: %#v", got)
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
