package muxrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

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

type Type = RequestEncoding

func (rt RequestEncoding) IsValid() bool {
	return rt >= 0 && rt <= TypeJSON
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
	handlers map[string]Handler
}

func (hm *HandlerMux) Handled(m Method) bool {
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
	for i := len(req.Method); i > 0; i-- {
		m := req.Method[:i]
		h, ok := hm.handlers[m.String()]
		if ok {
			h.HandleCall(ctx, req)
			return
		}
	}
	req.CloseWithError(fmt.Errorf("no such method: %s", req.Method))
}

func (hm *HandlerMux) HandleConnect(ctx context.Context, edp Endpoint) {
	for _, h := range hm.handlers {
		go h.HandleConnect(ctx, edp)
	}
}

func (hm *HandlerMux) Register(m Method, h Handler) {
	if hm.handlers == nil {
		hm.handlers = make(map[string]Handler)
	}
	hm.handlers[m.String()] = h
}

type Endpoint interface {
	Async(ctx context.Context, ret interface{}, tipe RequestEncoding, method Method, args ...interface{}) error
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

func (req *Request) ResponseSink() (*ByteSink, error) {
	if req.Type != "source" && req.Type != "duplex" {
		return nil, fmt.Errorf("muxrpc: wrong stream type")
	}
	return req.sink, nil
}

func (req *Request) ResponseSource() (*ByteSource, error) {
	if req.Type != "sink" && req.Type != "duplex" {
		return nil, fmt.Errorf("muxrpc: wrong stream type")
	}
	return req.source, nil
}

func (req *Request) Return(ctx context.Context, v interface{}) error {
	if req.Type != "async" && req.Type != "sync" {
		return fmt.Errorf("cannot return value on %q stream", req.Type)
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
			return fmt.Errorf("muxrpc: error marshaling return value: %w", err)
		}
	}

	if _, err := req.sink.Write(b); err != nil {
		return fmt.Errorf("muxrpc: error writing return value: %w", err)
	}

	return nil
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
	ctx context.Context

	conn     Conn
	packer   *Packer
	handler  Handler
	manifest *Manifest

	mu      sync.Mutex
	streams map[int32]*Request
	nextID  int32
}

func NewRPC(ctx context.Context, conn Conn, handler Handler, manifest *Manifest) *rpc {
	r := &rpc{
		ctx:      ctx,
		conn:     conn,
		packer:   NewPacker(conn),
		handler:  handler,
		streams:  make(map[int32]*Request),
		manifest: manifest,
	}
	go r.serve()
	return r
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

func (r *rpc) serve() {
	for {
		var hdr codec.Header
		err := r.packer.NextHeader(r.ctx, &hdr)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("[muxrpc] serve error: %v\n", err)
			}
			return
		}

		stream := r.packer.NewPacketStream(hdr.Req, hdr.Flag)
		go r.handlePacket(hdr, stream)
	}
}

func (r *rpc) handlePacket(hdr codec.Header, stream *PacketStream) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if hdr.Req > 0 {
		req := &Request{
			id:         hdr.Req,
			Method:     ParseMethod("unknown"),
			Type:       CallType("async"),
			sink:       NewByteSinkFromStream(stream),
			source:     NewByteSource(r.ctx),
			remoteAddr: r.conn.RemoteAddr(),
			endpoint:   r,
		}
		r.streams[hdr.Req] = req
		stream.SetRequest(req)

		args := stream.Bytes()
		if len(args) > 0 {
			json.Unmarshal(args, &req.RawArgs)
			if req.RawArgs == nil {
				req.RawArgs = args
			}
		}

		if r.handler != nil {
			r.handler.HandleCall(r.ctx, req)
		}
	} else {
		if req, ok := r.streams[-hdr.Req]; ok {
			if hdr.Flag.Get(codec.FlagEndErr) {
				req.source.Cancel(fmt.Errorf("remote error"))
			}
			if err := req.sink.Consume(hdr.Len, hdr.Flag, stream); err != nil {
				fmt.Printf("[muxrpc] stream consume error: %v\n", err)
			}
			if !hdr.Flag.Get(codec.FlagStream) {
				req.sink.Close()
				delete(r.streams, -hdr.Req)
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
	src, err := r.Source(ctx, tipe, method, args...)
	if err != nil {
		return err
	}

	b, err := src.Bytes()
	if err != nil && err != io.EOF {
		return err
	}

	if len(b) > 0 && tipe == TypeJSON && ret != nil {
		if err := json.Unmarshal(b, ret); err != nil {
			return fmt.Errorf("muxrpc: failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func (r *rpc) Source(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, error) {
	encFlag, err := tipe.AsCodecFlag()
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	id := r.nextID
	r.nextID++
	r.mu.Unlock()

	sink := NewByteSink(r.packer)
	sink.SetReqID(id)
	src := NewByteSource(ctx)

	req := &Request{
		id:         id,
		Method:     method,
		Type:       "source",
		sink:       sink,
		source:     src,
		remoteAddr: r.conn.RemoteAddr(),
		endpoint:   r,
	}

	var argBytes []byte
	if len(args) > 0 {
		argBytes, err = json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("muxrpc: failed to marshal args: %w", err)
		}
	}

	pkt := codec.Packet{
		Flag: codec.FlagStream | encFlag,
		Req:  id,
		Body: argBytes,
	}

	if err := r.packer.WritePacket(pkt); err != nil {
		return nil, fmt.Errorf("muxrpc: failed to send packet: %w", err)
	}

	r.mu.Lock()
	r.streams[id] = req
	r.mu.Unlock()

	return src, nil
}

func (r *rpc) Sink(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSink, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *rpc) Duplex(ctx context.Context, tipe RequestEncoding, method Method, args ...interface{}) (*ByteSource, *ByteSink, error) {
	return nil, nil, fmt.Errorf("not implemented")
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
