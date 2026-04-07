package muxrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

type CallType string

func (t CallType) Flags() codec.Flag {
	switch t {
	case "source", "sink", "duplex":
		return codec.FlagStream
	default:
		return 0
	}
}

type RequestEncoding uint

const (
	TypeBinary RequestEncoding = iota
	TypeString
	TypeJSON
)

func (rt RequestEncoding) String() string {
	switch rt {
	case TypeBinary:
		return "binary"
	case TypeString:
		return "string"
	case TypeJSON:
		return "json"
	default:
		return "unknown"
	}
}

type Type = RequestEncoding

func (rt RequestEncoding) IsValid() bool {
	return rt <= TypeJSON
}

func (rt RequestEncoding) AsCodecFlag() (codec.Flag, error) {
	if !rt.IsValid() {
		return 0, fmt.Errorf("muxrpc: invalid request encoding %d", rt)
	}
	switch rt {
	case TypeBinary:
		return 0, nil
	case TypeString:
		return codec.FlagString, nil
	case TypeJSON:
		return codec.FlagJSON, nil
	default:
		return 0, fmt.Errorf("muxrpc: invalid request encoding %d", rt)
	}
}

type Method []string

func (m Method) String() string {
	return strings.Join(m, ".")
}

func ParseMethod(s string) Method {
	return strings.Split(s, ".")
}

type Handler interface {
	Handled(Method) bool
	HandleCall(ctx context.Context, req *Request)
	HandleConnect(ctx context.Context, edp Endpoint)
}

type HandlerMux struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func (hm *HandlerMux) Handled(m Method) bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	if hm == nil || hm.handlers == nil {
		return false
	}
	for _, h := range hm.handlers {
		if h.Handled(m) {
			return true
		}
	}
	return false
}

func (hm *HandlerMux) HandleCall(ctx context.Context, req *Request) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	for i := len(req.Method); i > 0; i-- {
		m := req.Method[:i]
		key := m.String()
		h, ok := hm.handlers[key]
		if ok {
			h.HandleCall(ctx, req)
			return
		}
	}
	req.CloseWithError(fmt.Errorf("no such method: %s", req.Method))
}

func (hm *HandlerMux) HandleConnect(ctx context.Context, edp Endpoint) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	for _, h := range hm.handlers {
		go func(h Handler) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("muxrpc: HandleConnect panic", "recover", r)
				}
			}()
			h.HandleConnect(ctx, edp)
		}(h)
	}
}

func (hm *HandlerMux) Register(m Method, h Handler) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if hm.handlers == nil {
		hm.handlers = make(map[string]Handler)
	}
	hm.handlers[m.String()] = h
}

type Endpoint interface {
	Async(ctx context.Context, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error
	Sync(ctx context.Context, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error
	Source(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, error)
	Sink(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSink, error)
	Duplex(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, *ByteSink, error)
	Terminate() error
	Remote() net.Addr
}

type Request struct {
	Stream Stream `json:"-"`

	Method  Method          `json:"name"`
	RawArgs json.RawMessage `json:"args"`
	Type    CallType        `json:"type"`

	sink   *ByteSink
	source *ByteSource

	id    int32
	abort context.CancelFunc

	remoteAddr net.Addr
	endpoint   *rpc
}

func (req Request) Endpoint() Endpoint {
	return req.endpoint
}

func (req Request) RemoteAddr() net.Addr {
	return req.remoteAddr
}

func (req *Request) ID() int32 {
	return req.id
}

func (req *Request) Sink() *ByteSink {
	return req.sink
}

func (req *Request) Source() *ByteSource {
	return req.source
}

func (req *Request) ResponseSink() (*ByteSink, error) {
	if req.Type != "source" && req.Type != "duplex" {
		return nil, errors.New("muxrpc: wrong stream type")
	}
	return req.sink, nil
}

func (req *Request) ResponseSource() (*ByteSource, error) {
	if req.Type != "sink" && req.Type != "duplex" {
		return nil, errors.New("muxrpc: wrong stream type")
	}
	return req.source, nil
}

func (req *Request) Return(ctx context.Context, v interface{}) error {
	if req.Type != "async" && req.Type != "sync" {
		return errors.New("cannot return value on stream")
	}

	var b []byte
	var err error
	switch tv := v.(type) {
	case string:
		req.sink.SetEncoding(TypeString)
		b = []byte(tv)
	case []byte:
		req.sink.SetEncoding(TypeBinary)
		b = tv
	default:
		req.sink.SetEncoding(TypeJSON)
		b, err = json.Marshal(v)
		if err != nil {
			return err
		}
	}

	if _, err := req.sink.Write(b); err != nil {
		return err
	}

	return req.Close()
}

func (req *Request) CloseWithError(cerr error) error {
	if cerr == nil || cerr == io.EOF {
		if req.source != nil {
			req.source.Cancel(nil)
		}
		if req.sink != nil {
			req.sink.Close()
		}
	} else {
		if req.source != nil {
			req.source.Cancel(cerr)
		}
		if req.sink != nil {
			req.sink.CloseWithError(cerr)
		}
	}
	if req.endpoint != nil {
		req.endpoint.closeStream(req)
	}
	return nil
}

func (req *Request) Close() error {
	return req.CloseWithError(io.EOF)
}

type Conn interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

type rpc struct {
	ctx      context.Context
	conn     Conn
	packer   *Packer
	handler  Handler
	manifest *Manifest

	mu      sync.Mutex
	streams map[int32]*Request
	nextID  int32
	out     []codec.Packet
	sendCh  chan struct{}
	done    chan struct{}
}

func NewRPC(ctx context.Context, conn Conn, handler Handler, manifest *Manifest) *rpc {
	r := &rpc{
		ctx:      ctx,
		conn:     conn,
		handler:  handler,
		manifest: manifest,
		streams:  make(map[int32]*Request),
		nextID:   1,
		sendCh:   make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	r.packer = NewPacker(r.conn)
	go r.serve()
	go r.sender()
	return r
}

func (r *rpc) Wait() <-chan struct{} {
	return r.done
}

type Server = rpc

func NewServer(ctx context.Context, conn Conn, handler Handler, manifest *Manifest) *Server {
	return NewRPC(ctx, conn, handler, manifest)
}

func (r *rpc) Terminate() error {
	return r.packer.Close()
}

func (r *rpc) Remote() net.Addr {
	return r.conn.RemoteAddr()
}

func (r *rpc) WritePacket(pkt codec.Packet) error {
	r.mu.Lock()
	r.out = append(r.out, pkt)
	r.mu.Unlock()

	select {
	case r.sendCh <- struct{}{}:
	default:
	}
	return nil
}

func (r *rpc) Produce() []codec.Packet {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.out
	r.out = nil
	return out
}

func (r *rpc) serve() {
	defer close(r.done)
	errCount := 0
	for {
		pkt, err := r.packer.NextPacket(r.ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) {
				return
			}
			errCount++
			slog.Debug("muxrpc: serve read error", "error", err, "count", errCount)
			if errCount > 10 {
				slog.Error("muxrpc: serve too many read errors, terminating", "error", err)
				return
			}
			time.Sleep(time.Duration(errCount) * 10 * time.Millisecond)
			continue
		}
		errCount = 0
		r.HandlePacket(pkt)
	}
}

func (r *rpc) sender() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.sendCh:
			for _, outPkt := range r.Produce() {
				if err := r.packer.WritePacket(outPkt); err != nil {
					slog.Debug("muxrpc: sender write error", "error", err)
				}
			}
		}
	}
}

func (r *rpc) HandlePacket(p *codec.Packet) {
	slog.Debug("muxrpc handle packet", "req", p.Req, "flag", p.Flag, "body_len", len(p.Body))
	if p.Req > 0 {
		r.mu.Lock()
		existingReq, hasExisting := r.streams[p.Req]
		r.mu.Unlock()

		if hasExisting {
			slog.Debug("muxrpc handle packet follow-up detected", "req", p.Req)
			if len(p.Body) > 0 {
				if err := existingReq.source.WritePacket(p); err != nil {
				}
			}
			if p.Flag.Get(codec.FlagEndErr) {
				existingReq.source.Cancel(nil)
				r.closeStream(existingReq)
			}
		} else {
			req := &Request{
				id:         p.Req,
				Method:     ParseMethod("unknown"),
				Type:       CallType("async"),
				sink:       NewByteSink(r.packer),
				source:     NewByteSource(r.ctx),
				remoteAddr: r.conn.RemoteAddr(),
				endpoint:   r,
			}
			req.sink.SetReqID(p.Req)

			r.mu.Lock()
			r.streams[p.Req] = req
			r.mu.Unlock()

			if len(p.Body) > 0 {
				if err := json.Unmarshal(p.Body, req); err != nil {
					req.RawArgs = json.RawMessage(p.Body)
				}
			}

			// Set sink flags after unmarshaling Type
			req.sink.SetReqID(p.Req)
			req.sink.SetCounterpart(true)
			if req.Type.Flags().Get(codec.FlagStream) {
				req.sink.SetFlag(codec.FlagStream)
			}

			if r.handler != nil {
				go r.handler.HandleCall(r.ctx, req)
			}
		}
	} else if p.Req < 0 {
		r.mu.Lock()
		req, ok := r.streams[-p.Req]
		r.mu.Unlock()
		if ok {
			if len(p.Body) > 0 {
				if err := req.source.WritePacket(p); err != nil {
				}
			}
			// Stream end: FlagEndErr signals termination regardless of FlagStream.
			// Both stream end (EndErr+Stream) and async error (EndErr only) should close.
			if p.Flag.Get(codec.FlagEndErr) {
				req.source.Cancel(nil)
				r.closeStream(req)
			}
		}
	}
}

func (r *rpc) closeStream(req *Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, req.id)
}

func (r *rpc) Async(ctx context.Context, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error {
	return r.callAndDecode(ctx, "async", ret, tipe, method, args...)
}

func (r *rpc) Sync(ctx context.Context, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error {
	return r.callAndDecode(ctx, "sync", ret, tipe, method, args...)
}

func (r *rpc) callAndDecode(ctx context.Context, callType string, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error {
	src, err := r.call(ctx, callType, tipe, method, args...)
	if err != nil {
		return err
	}

	b, err := src.Bytes()
	if err != nil && err != io.EOF {
		return err
	}

	if len(b) > 0 && ret != nil {
		switch tipe {
		case TypeJSON:
			if err := json.Unmarshal(b, ret); err != nil {
				return err
			}
		case TypeString:
			if s, ok := ret.(*string); ok {
				*s = string(b)
			} else {
				return fmt.Errorf("muxrpc: expected *string for TypeString, got %T", ret)
			}
		case TypeBinary:
			if bb, ok := ret.(*[]byte); ok {
				*bb = b
			} else {
				return fmt.Errorf("muxrpc: expected *[]byte for TypeBinary, got %T", ret)
			}
		}
	}

	return nil
}

func (r *rpc) Sink(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSink, error) {
	req, err := r.doCall(ctx, "sink", tipe, method, args...)
	if err != nil {
		return nil, err
	}
	return req.sink, nil
}

func (r *rpc) Duplex(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, *ByteSink, error) {
	req, err := r.doCall(ctx, "duplex", tipe, method, args...)
	if err != nil {
		return nil, nil, err
	}
	return req.source, req.sink, nil
}

func (r *rpc) Source(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, error) {
	req, err := r.doCall(ctx, "source", tipe, method, args...)
	if err != nil {
		return nil, err
	}
	return req.source, nil
}

func (r *rpc) call(ctx context.Context, callType string, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, error) {
	req, err := r.doCall(ctx, callType, tipe, method, args...)
	if err != nil {
		return nil, err
	}
	return req.source, nil
}

func (r *rpc) doCall(ctx context.Context, callType string, tipe RequestEncoding, method Method, args ...interface{}) (*Request, error) {
	r.mu.Lock()
	id := r.nextID
	r.nextID++
	r.mu.Unlock()

	sink := NewByteSink(r)
	sink.SetReqID(id)
	sink.SetCounterpart(false)
	sink.SetEncoding(tipe)
	if callType != "async" && callType != "sync" {
		sink.SetFlag(codec.FlagStream)
	}
	src := NewByteSource(ctx)

	req := &Request{
		id:         id,
		Method:     method,
		Type:       CallType(callType),
		sink:       sink,
		source:     src,
		remoteAddr: r.conn.RemoteAddr(),
		endpoint:   r,
	}

	if len(args) > 0 {
		req.RawArgs, _ = json.Marshal(args)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("muxrpc: failed to marshal request: %w", err)
	}

	pkt := codec.Packet{
		Flag: codec.FlagJSON,
		Req:  id,
		Body: body,
	}
	if callType != "async" && callType != "sync" {
		pkt.Flag |= codec.FlagStream
	}

	if err := r.WritePacket(pkt); err != nil {
		return nil, fmt.Errorf("muxrpc: failed to send packet: %w", err)
	}

	r.mu.Lock()
	r.streams[id] = req
	r.mu.Unlock()

	return req, nil
}

type Manifest struct {
	async  map[string]bool
	source map[string]bool
	sink   map[string]bool
	duplex map[string]bool
	sync   map[string]bool
}

func NewManifest() *Manifest {
	return &Manifest{
		async:  make(map[string]bool),
		source: make(map[string]bool),
		sink:   make(map[string]bool),
		duplex: make(map[string]bool),
		sync:   make(map[string]bool),
	}
}

func (m *Manifest) Handled(method Method) (bool, bool) {
	name := method.String()
	if m.async[name] || m.source[name] || m.sink[name] || m.duplex[name] || m.sync[name] {
		return true, true
	}
	return false, false
}

func (m *Manifest) RegisterAsync(name string) {
	m.async[name] = true
}

func (m *Manifest) RegisterSource(name string) {
	m.source[name] = true
}

func (m *Manifest) RegisterSink(name string) {
	m.sink[name] = true
}

func (m *Manifest) RegisterDuplex(name string) {
	m.duplex[name] = true
}

func (m *Manifest) RegisterSync(name string) {
	m.sync[name] = true
}

func (m *Manifest) ToJSON() ([]byte, error) {
	type manifestEntry struct {
		Type  string   `json:"type"`
		Names []string `json:"names"`
	}

	var entries []manifestEntry

	var asyncNames []string
	for k := range m.async {
		asyncNames = append(asyncNames, k)
	}
	if len(asyncNames) > 0 {
		entries = append(entries, manifestEntry{Type: "async", Names: asyncNames})
	}

	var sourceNames []string
	for k := range m.source {
		sourceNames = append(sourceNames, k)
	}
	if len(sourceNames) > 0 {
		entries = append(entries, manifestEntry{Type: "source", Names: sourceNames})
	}

	var sinkNames []string
	for k := range m.sink {
		sinkNames = append(sinkNames, k)
	}
	if len(sinkNames) > 0 {
		entries = append(entries, manifestEntry{Type: "sink", Names: sinkNames})
	}

	var duplexNames []string
	for k := range m.duplex {
		duplexNames = append(duplexNames, k)
	}
	if len(duplexNames) > 0 {
		entries = append(entries, manifestEntry{Type: "duplex", Names: duplexNames})
	}

	return json.Marshal(entries)
}
