package muxrpc

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
)

type byteStreamConn struct {
	ctx    context.Context
	source *ByteSource
	sink   *ByteSink
	remote net.Addr

	mu      sync.Mutex
	readBuf []byte
	closed  bool
	once    sync.Once
}

type byteStreamAddr string

func (a byteStreamAddr) Network() string { return "muxrpc-stream" }
func (a byteStreamAddr) String() string  { return string(a) }

func NewByteStreamConn(ctx context.Context, source *ByteSource, sink *ByteSink, remote net.Addr) Conn {
	if ctx == nil {
		ctx = context.Background()
	}
	if remote == nil {
		remote = byteStreamAddr("muxrpc-stream")
	}
	return &byteStreamConn{
		ctx:    ctx,
		source: source,
		sink:   sink,
		remote: remote,
	}
}

func (c *byteStreamConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return 0, io.EOF
		}
		if len(c.readBuf) > 0 {
			n := copy(p, c.readBuf)
			c.readBuf = c.readBuf[n:]
			c.mu.Unlock()
			return n, nil
		}
		c.mu.Unlock()

		if c.source == nil {
			return 0, io.EOF
		}
		if !c.source.Next(c.ctx) {
			if err := c.source.Err(); err != nil && !errors.Is(err, context.Canceled) {
				return 0, err
			}
			return 0, io.EOF
		}

		payload, err := c.source.Bytes()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return 0, io.EOF
			}
			return 0, err
		}
		if len(payload) == 0 {
			continue
		}

		c.mu.Lock()
		c.readBuf = append(c.readBuf[:0], payload...)
		c.mu.Unlock()
	}
}

func (c *byteStreamConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, io.EOF
	}
	if c.sink == nil {
		return 0, io.EOF
	}
	return c.sink.Write(p)
}

func (c *byteStreamConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.readBuf = nil
		c.mu.Unlock()
		if c.source != nil {
			c.source.Cancel(nil)
		}
		if c.sink != nil {
			_ = c.sink.Close()
		}
	})
	return nil
}

func (c *byteStreamConn) RemoteAddr() net.Addr {
	return c.remote
}
