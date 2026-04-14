package muxrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type funcHandler struct {
	h func(ctx context.Context, req *Request)
}

func (f *funcHandler) Handled(m Method) bool { return true }
func (f *funcHandler) HandleCall(ctx context.Context, req *Request) {
	if f.h != nil {
		f.h(ctx, req)
	}
}
func (f *funcHandler) HandleConnect(ctx context.Context, edp Endpoint) {}

func TestRPCSink(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan []byte, 10)
	handler := &funcHandler{
		h: func(ctx context.Context, req *Request) {
			if req.Type != "sink" {
				return
			}
			src := req.source
			for src.Next(ctx) {
				b, _ := src.Bytes()
				received <- b
			}
			req.Close()
		},
	}

	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	sink, err := client.Sink(ctx, TypeBinary, Method{"test", "sink"})
	if err != nil {
		t.Fatalf("Sink failed: %v", err)
	}

	sink.Write([]byte("hello"))
	sink.Write([]byte("world"))
	sink.Close()

	var got []byte
	select {
	case b := <-received:
		got = append(got, b...)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for packet 1")
	}
	select {
	case b := <-received:
		got = append(got, b...)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for packet 2")
	}

	if string(got) != "helloworld" {
		t.Errorf("got %q, want %q", string(got), "helloworld")
	}
}

func TestRPCDuplex(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := &funcHandler{
		h: func(ctx context.Context, req *Request) {
			if req.Type != "duplex" {
				return
			}
			src := req.source
			sink := req.sink
			for src.Next(ctx) {
				b, _ := src.Bytes()
				sink.Write(append([]byte("echo: "), b...))
			}
			sink.Close()
		},
	}

	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	src, sink, err := client.Duplex(ctx, TypeString, Method{"test", "echo"})
	if err != nil {
		t.Fatalf("Duplex failed: %v", err)
	}

	sink.Write([]byte("foo"))
	if !src.Next(ctx) {
		t.Fatal("expected echo for foo")
	}
	b, _ := src.Bytes()
	if string(b) != "echo: foo" {
		t.Errorf("got %q", string(b))
	}

	sink.Write([]byte("bar"))
	if !src.Next(ctx) {
		t.Fatal("expected echo for bar")
	}
	b, _ = src.Bytes()
	if string(b) != "echo: bar" {
		t.Errorf("got %q", string(b))
	}

	sink.Close()
}

func TestRPCAsyncReturnsRemoteError(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := &funcHandler{
		h: func(ctx context.Context, req *Request) {
			req.CloseWithError(fmt.Errorf("boom async"))
		},
	}

	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	var out string
	err := client.Async(ctx, &out, TypeJSON, Method{"test", "fail"})
	if err == nil {
		t.Fatal("expected remote error")
	}

	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("expected *RemoteError, got %T (%v)", err, err)
	}
	if remoteErr.Name != "Error" || !strings.Contains(remoteErr.Message, "boom async") {
		t.Fatalf("unexpected remote error payload: %+v", remoteErr)
	}
}

func TestRPCSyncReturnsRemoteError(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := &funcHandler{
		h: func(ctx context.Context, req *Request) {
			req.CloseWithError(fmt.Errorf("boom sync"))
		},
	}

	server := NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	var out bool
	err := client.Sync(ctx, &out, TypeJSON, Method{"test", "fail"})
	if err == nil {
		t.Fatal("expected remote error")
	}
	if strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("expected remote error, got decode failure: %v", err)
	}
}
