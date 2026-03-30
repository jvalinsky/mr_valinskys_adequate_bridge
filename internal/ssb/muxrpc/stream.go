package muxrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

type Stream interface {
	Source() *ByteSource
	Sink() *ByteSink
}

type ByteSourceReader interface {
	Next(ctx context.Context) bool
	Bytes() ([]byte, error)
	Err() error
}

type SinkWriter interface {
	io.Writer
	io.Closer
}

type ByteSource struct {
	buf *frameBuffer

	mu     sync.Mutex
	closed chan struct{}
	failed error

	streamCtx context.Context
	cancel    context.CancelFunc
}

func NewByteSource(ctx context.Context) *ByteSource {
	bs := &ByteSource{
		buf: &frameBuffer{
			store: new(bytes.Buffer),
		},
		closed: make(chan struct{}),
	}
	bs.streamCtx, bs.cancel = context.WithCancel(ctx)
	return bs
}

func (bs *ByteSource) Cancel(err error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.failed != nil {
		return
	}

	if err == nil {
		bs.failed = io.EOF
	} else {
		bs.failed = err
	}
	close(bs.closed)
}

func (bs *ByteSource) Err() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if errors.Is(bs.failed, io.EOF) || errors.Is(bs.failed, context.Canceled) {
		return nil
	}

	return bs.failed
}

func (bs *ByteSource) Next(ctx context.Context) bool {
	bs.mu.Lock()
	if bs.failed != nil && bs.buf.frames == 0 {
		bs.mu.Unlock()
		return false
	}
	if bs.buf.frames > 0 {
		bs.mu.Unlock()
		return true
	}
	bs.mu.Unlock()

	select {
	case <-bs.streamCtx.Done():
		bs.mu.Lock()
		defer bs.mu.Unlock()
		if bs.failed == nil {
			bs.failed = bs.streamCtx.Err()
		}
		return bs.buf.Frames() > 0

	case <-ctx.Done():
		bs.mu.Lock()
		defer bs.mu.Unlock()
		if bs.failed == nil {
			bs.failed = ctx.Err()
		}
		return false

	case <-bs.closed:
		return bs.buf.Frames() > 0

	case <-bs.buf.waitForMore():
		return true
	}
}

func (bs *ByteSource) Bytes() ([]byte, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.buf.frames == 0 {
		if bs.failed != nil {
			return nil, bs.failed
		}

		// Wait for at least one frame
		bs.mu.Unlock()
		if !bs.Next(context.Background()) {
			bs.mu.Lock()
			return nil, bs.failed
		}
		bs.mu.Lock()
	}

	pktLen, rd, err := bs.buf.getNextFrameReader()
	if err != nil {
		return nil, err
	}

	b := make([]byte, pktLen)
	_, err = io.ReadFull(rd, b)
	return b, err
}

func (bs *ByteSource) Source() *ByteSource {
	return bs
}

func (bs *ByteSource) WritePacket(p *codec.Packet) error {
	return bs.consume(p.Body)
}

type PacketWriter interface {
	WritePacket(p codec.Packet) error
}

func (bs *ByteSource) consume(body []byte) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.failed != nil {
		return bs.failed
	}

	err := bs.buf.writeBody(body)
	if err != nil {
		return err
	}

	return nil
}

type ByteSink struct {
	pkr       PacketWriter
	req       int32
	flag      codec.Flag
	encoding  RequestEncoding
	closed    bool
	closeErr  error
	mu        sync.Mutex
	writer    *bytes.Buffer
	usePacker bool
	isCounter bool // true if this is a response (negative ID)
}

func NewByteSink(p PacketWriter) *ByteSink {
	return &ByteSink{
		pkr:       p,
		writer:    new(bytes.Buffer),
		usePacker: p != nil,
	}
}

func (bs *ByteSink) SetReqID(req int32) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.req = req
}

func (bs *ByteSink) SetCounterpart(v bool) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.isCounter = v
}

func (bs *ByteSink) SetEncoding(enc RequestEncoding) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.encoding = enc
}

func (bs *ByteSink) SetFlag(f codec.Flag) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.flag = f
}

func (bs *ByteSink) isStream() bool {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.flag.Get(codec.FlagStream)
}

func (bs *ByteSink) Packer() *Packer {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if pkr, ok := bs.pkr.(*Packer); ok {
		return pkr
	}
	return nil
}

func (bs *ByteSink) Write(p []byte) (int, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.closed {
		return 0, io.EOF
	}

	if bs.usePacker && bs.pkr != nil && bs.isStreamUnlocked() {
		return bs.flush(p, false)
	}

	bs.writer.Write(p)
	return len(p), nil
}

func (bs *ByteSink) isStreamUnlocked() bool {
	return bs.flag.Get(codec.FlagStream)
}

func (bs *ByteSink) flush(p []byte, end bool) (int, error) {
	encFlag, err := bs.encoding.AsCodecFlag()
	if err != nil {
		return 0, err
	}

	flag := encFlag | codec.FlagStream
	if end {
		flag |= codec.FlagEndErr
	}

	id := bs.req
	if bs.isCounter {
		id = -id
	}

	pkt := codec.Packet{
		Flag: flag,
		Req:  id,
		Body: p,
	}
	err = bs.pkr.WritePacket(pkt)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (bs *ByteSink) Close() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.closed {
		return bs.closeErr
	}
	bs.closed = true
	if bs.usePacker && bs.pkr != nil {
		if bs.isStreamUnlocked() {
			// Send empty end packet
			_, bs.closeErr = bs.flush(nil, true)
		} else {
			encFlag, _ := bs.encoding.AsCodecFlag()
			id := bs.req
			if bs.isCounter {
				id = -id
			}
			pkt := codec.Packet{
				Flag: encFlag | codec.FlagEndErr,
				Req:  id,
				Body: bs.writer.Bytes(),
			}
			bs.closeErr = bs.pkr.WritePacket(pkt)
		}
	}

	return bs.closeErr
}

func (bs *ByteSink) CloseWithError(err error) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.closed {
		return bs.closeErr
	}
	bs.closed = true
	bs.closeErr = err

	if bs.usePacker && bs.pkr != nil {
		errBytes := []byte(fmt.Sprintf(`{"name":"Error","message":"%v"}`, err))
		pkt := codec.Packet{
			Flag: codec.FlagJSON | codec.FlagEndErr,
			Req:  -bs.req,
			Body: errBytes,
		}
		bs.pkr.WritePacket(pkt)
	}

	return bs.closeErr
}

func (bs *ByteSink) Consume(p *codec.Packet) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.closed {
		return io.EOF
	}

	bs.writer.Write(p.Body)
	return nil
}

func (bs *ByteSink) Bytes() []byte {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.writer.Bytes()
}

func (bs *ByteSink) Sink() *ByteSink {
	return bs
}

func (bs *ByteSink) Writer() PacketWriter {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.pkr
}

type PacketStream struct {
	packer PacketWriter
	req    int32
	flag   codec.Flag
	buf    *bytes.Buffer
	reqPtr *Request
}

func (ps *PacketStream) SetRequest(req *Request) {
	ps.reqPtr = req
}

func (ps *PacketStream) SetPackerAndReq(pkr PacketWriter, req int32) {
	ps.packer = pkr
	ps.req = req
}

func (ps *PacketStream) SetFlag(flag codec.Flag) {
	ps.flag = flag
}

func (ps *PacketStream) Read(p []byte) (int, error) {
	if ps.buf == nil {
		return 0, io.EOF
	}
	return ps.buf.Read(p)
}

func (ps *PacketStream) Write(p []byte) (int, error) {
	if ps.packer == nil {
		return len(p), nil
	}

	encFlag, _ := ps.reqPtr.sink.encoding.AsCodecFlag()
	pkt := codec.Packet{
		Flag: ps.flag&codec.FlagStream | encFlag,
		Req:  ps.req,
		Body: p,
	}
	err := ps.packer.WritePacket(pkt)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (ps *PacketStream) Bytes() []byte {
	if ps.buf == nil {
		return nil
	}
	return ps.buf.Bytes()
}

type frameBuffer struct {
	mu    sync.Mutex
	store *bytes.Buffer

	waiting []chan<- struct{}

	currentFrameTotal uint32
	currentFrameRead  uint32

	frames uint32

	lenBuf [4]byte
}

func (fb *frameBuffer) Frames() uint32 {
	return atomic.LoadUint32(&fb.frames)
}

func (fb *frameBuffer) writeBody(body []byte) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	pktLen := len(body)
	binary.LittleEndian.PutUint32(fb.lenBuf[:], uint32(pktLen))
	fb.store.Write(fb.lenBuf[:])
	fb.store.Write(body)

	atomic.AddUint32(&fb.frames, 1)

	if n := len(fb.waiting); n > 0 {
		for _, ch := range fb.waiting {
			close(ch)
		}
		fb.waiting = make([]chan<- struct{}, 0)
	}
	return nil
}

func (fb *frameBuffer) copyBody(pktLen uint32, rd io.Reader) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	binary.LittleEndian.PutUint32(fb.lenBuf[:], uint32(pktLen))
	fb.store.Write(fb.lenBuf[:])

	copied, err := io.Copy(fb.store, rd)
	if err != nil {
		return err
	}

	if uint32(copied) != pktLen {
		return errors.New("frameBuffer: failed to consume whole body")
	}

	atomic.AddUint32(&fb.frames, 1)

	if n := len(fb.waiting); n > 0 {
		for _, ch := range fb.waiting {
			close(ch)
		}
		fb.waiting = make([]chan<- struct{}, 0)
	}
	return nil
}

func (fb *frameBuffer) waitForMore() <-chan struct{} {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	ch := make(chan struct{})
	if fb.frames > 0 {
		close(ch)
		return ch
	}

	fb.waiting = append(fb.waiting, ch)
	return ch
}

func (fb *frameBuffer) getNextFrameReader() (uint32, io.Reader, error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if fb.currentFrameTotal != 0 {
		diff := int64(fb.currentFrameTotal - fb.currentFrameRead)
		if diff > 0 {
			io.Copy(io.Discard, io.LimitReader(fb.store, diff))
		}
	}

	_, err := fb.store.Read(fb.lenBuf[:])
	if err != nil {
		return 0, nil, fmt.Errorf("muxrpc: didnt get length of next body (frames:%d): %w", fb.frames, err)
	}
	pktLen := binary.LittleEndian.Uint32(fb.lenBuf[:])

	fb.currentFrameRead = 0
	fb.currentFrameTotal = pktLen

	rd := &countingReader{
		rd:   io.LimitReader(fb.store, int64(pktLen)),
		read: &fb.currentFrameRead,
	}

	atomic.AddUint32(&fb.frames, ^uint32(0))
	return pktLen, rd, nil
}

type countingReader struct {
	rd io.Reader

	read *uint32
}

func (cr *countingReader) Read(b []byte) (int, error) {
	n, err := cr.rd.Read(b)
	if err == nil && n > 0 {
		*cr.read += uint32(n)
	}
	return n, err
}

type ByteSourceAdapter struct {
	source *ByteSource
}

func NewByteSourceAdapter(source *ByteSource) *ByteSourceAdapter {
	return &ByteSourceAdapter{source: source}
}

func (a *ByteSourceAdapter) Next(ctx context.Context) bool {
	return a.source.Next(ctx)
}

func (a *ByteSourceAdapter) Bytes() ([]byte, error) {
	return a.source.Bytes()
}

func (a *ByteSourceAdapter) Err() error {
	return a.source.Err()
}

type ByteSinkWriter struct {
	sink *ByteSink
}

func NewByteSinkWriter(sink *ByteSink) *ByteSinkWriter {
	return &ByteSinkWriter{sink: sink}
}

func (w *ByteSinkWriter) Write(ctx context.Context, data []byte) error {
	n, err := w.sink.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

type PacketStreamWriter struct {
	ps *PacketStream
}

func NewPacketStreamWriter(ps *PacketStream) *PacketStreamWriter {
	return &PacketStreamWriter{ps: ps}
}

func (w *PacketStreamWriter) Write(ctx context.Context, data []byte) error {
	n, err := w.ps.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
