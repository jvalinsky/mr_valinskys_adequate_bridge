package blobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/protocoltrace"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

const DefaultMaxSize = 5 << 20

var ErrBlobTooLarge = errors.New("blobs: blob exceeds max size")

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
	GetRange(hash []byte, start, size int64) (io.ReadCloser, error)
	Has(hash []byte) (bool, error)
	Size(hash []byte) (int64, error)
}

type WantManager interface {
	Want(ref *refs.BlobRef) error
	CancelWant(ref *refs.BlobRef) error
	Wants() ([]refs.BlobRef, error)
	Subscribe(ctx context.Context) (<-chan WantEvent, func())
}

type WantEvent struct {
	Ref  refs.BlobRef
	Want bool
}

type Store struct {
	bs feedlog.BlobStore
	wm *WantManagerImpl
}

func NewStore(bs feedlog.BlobStore) *Store {
	return &Store{
		bs: bs,
		wm: NewWantManager(bs),
	}
}

func (s *Store) BlobStore() BlobStore {
	return s.bs
}

func (s *Store) WantManager() WantManager {
	return s.wm
}

func (s *Store) Register(mux *muxrpc.HandlerMux, self *refs.FeedRef, edp muxrpc.Endpoint) {
	plug := NewPlugin(self, s.bs, s.wm, edp)
	mux.Register(muxrpc.Method{"blobs", "add"}, plug)
	mux.Register(muxrpc.Method{"blobs", "get"}, plug)
	mux.Register(muxrpc.Method{"blobs", "getSlice"}, plug)
	mux.Register(muxrpc.Method{"blobs", "has"}, plug)
	mux.Register(muxrpc.Method{"blobs", "size"}, plug)
	mux.Register(muxrpc.Method{"blobs", "want"}, plug)
	mux.Register(muxrpc.Method{"blobs", "createWants"}, plug)
}

type WantManagerImpl struct {
	bs       BlobStore
	mu       sync.RWMutex
	wants    map[string]time.Time
	canceled map[string]struct{}
	nextSub  uint64
	subs     map[uint64]chan WantEvent
}

func NewWantManager(bs BlobStore) *WantManagerImpl {
	return &WantManagerImpl{
		bs:       bs,
		wants:    make(map[string]time.Time),
		canceled: make(map[string]struct{}),
		subs:     make(map[uint64]chan WantEvent),
	}
}

func (wm *WantManagerImpl) Want(ref *refs.BlobRef) error {
	if ref == nil {
		return errors.New("blobs: nil blob ref")
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	refStr := ref.Ref()
	delete(wm.canceled, refStr)
	wm.wants[refStr] = time.Now()
	wm.notifyLocked(WantEvent{Ref: *ref, Want: true})
	return nil
}

func (wm *WantManagerImpl) CancelWant(ref *refs.BlobRef) error {
	if ref == nil {
		return errors.New("blobs: nil blob ref")
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	refStr := ref.Ref()
	delete(wm.wants, refStr)
	wm.canceled[refStr] = struct{}{}
	wm.notifyLocked(WantEvent{Ref: *ref, Want: false})
	return nil
}

func (wm *WantManagerImpl) Wants() ([]refs.BlobRef, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	var result []refs.BlobRef
	for refStr := range wm.wants {
		ref, err := refs.ParseBlobRef(refStr)
		if err != nil {
			continue
		}
		result = append(result, *ref)
	}
	return result, nil
}

func (wm *WantManagerImpl) IsWanted(ref *refs.BlobRef) bool {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	_, wanted := wm.wants[ref.Ref()]
	return wanted
}

func (wm *WantManagerImpl) IsCanceled(ref *refs.BlobRef) bool {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	_, canceled := wm.canceled[ref.Ref()]
	return canceled
}

func (wm *WantManagerImpl) Subscribe(ctx context.Context) (<-chan WantEvent, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan WantEvent, 16)

	wm.mu.Lock()
	wm.nextSub++
	id := wm.nextSub
	wm.subs[id] = ch
	wm.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			wm.mu.Lock()
			if _, ok := wm.subs[id]; ok {
				delete(wm.subs, id)
				close(ch)
			}
			wm.mu.Unlock()
		})
	}
	go func() {
		<-ctx.Done()
		cancel()
	}()

	return ch, cancel
}

func (wm *WantManagerImpl) notifyLocked(ev WantEvent) {
	for _, ch := range wm.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

type Handler struct {
	self *refs.FeedRef
	bs   BlobStore
	wm   WantManager
	edp  muxrpc.Endpoint
}

func NewPlugin(self *refs.FeedRef, bs BlobStore, wm WantManager, edp muxrpc.Endpoint) *Handler {
	return &Handler{
		self: self,
		bs:   bs,
		wm:   wm,
		edp:  edp,
	}
}

func (h *Handler) Handled(m muxrpc.Method) bool {
	return m[0] == "blobs"
}

func (h *Handler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	switch {
	case req.Method.String() == "blobs.has":
		h.handleHas(ctx, req)
	case req.Method.String() == "blobs.size":
		h.handleSize(ctx, req)
	case req.Method.String() == "blobs.want":
		h.handleWant(ctx, req)
	case req.Method.String() == "blobs.get":
		h.handleGet(ctx, req)
	case req.Method.String() == "blobs.getSlice":
		h.handleGetSlice(ctx, req)
	case req.Method.String() == "blobs.add":
		h.handleAdd(ctx, req)
	case req.Method.String() == "blobs.createWants":
		h.handleCreateWants(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("unknown method: %s", req.Method))
	}
}

func (h *Handler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *Handler) handleHas(ctx context.Context, req *muxrpc.Request) {
	var args []string
	if err := decodeArgs(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if len(args) == 0 {
		req.CloseWithError(errors.New("blobs.has: no refs provided"))
		return
	}

	ref, err := refs.ParseBlobRef(args[0])
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.has: invalid ref: %w", err))
		return
	}

	has, err := h.bs.Has(ref.Hash())
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.has: check failed: %w", err))
		return
	}

	slog.Debug("blobs.has", "ref", args[0], "has", has)
	req.Return(ctx, has)
}

func (h *Handler) handleSize(ctx context.Context, req *muxrpc.Request) {
	var args []string
	if err := decodeArgs(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if len(args) == 0 {
		req.CloseWithError(errors.New("blobs.size: no refs provided"))
		return
	}

	ref, err := refs.ParseBlobRef(args[0])
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.size: invalid ref: %w", err))
		return
	}

	size, err := h.bs.Size(ref.Hash())
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.size: lookup failed: %w", err))
		return
	}

	req.Return(ctx, size)
}

func (h *Handler) handleWant(ctx context.Context, req *muxrpc.Request) {
	var args []string
	if err := decodeArgs(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if len(args) == 0 {
		req.CloseWithError(errors.New("blobs.want: no refs provided"))
		return
	}

	ref, err := refs.ParseBlobRef(args[0])
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.want: invalid ref: %w", err))
		return
	}

	if err := h.wm.Want(ref); err != nil {
		req.CloseWithError(fmt.Errorf("blobs.want: failed: %w", err))
		return
	}

	req.Return(ctx, true)
}

func (h *Handler) handleGet(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("blobs.get is a source handler"))
		return
	}

	var args []struct {
		Hash string `json:"hash"`
	}
	if err := decodeArgs(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if len(args) == 0 || args[0].Hash == "" {
		req.CloseWithError(errors.New("blobs.get: no hash provided"))
		return
	}

	hashStr := args[0].Hash
	slog.Debug("blobs.get", "hash", hashStr)

	ref, err := refs.ParseBlobRef(hashStr)
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.get: invalid ref: %w", err))
		return
	}
	start := time.Now()
	bytesSent := 0
	errKind := ""
	defer func() {
		protocoltrace.Emit(protocoltrace.Event{
			Phase:    "blob_get",
			Method:   "blobs.get",
			BlobRef:  ref.String(),
			Bytes:    bytesSent,
			ErrKind:  errKind,
			Duration: time.Since(start),
		})
	}()

	rc, err := h.bs.Get(ref.Hash())
	if err != nil {
		errKind = protocoltrace.ErrKind(err)
		slog.Debug("blobs.get not found", "hash", hashStr)
		req.CloseWithError(fmt.Errorf("blobs.get: not found"))
		return
	}
	defer rc.Close()

	slog.Debug("blobs.get found, streaming", "hash", hashStr)

	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.get: get sink: %w", err))
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := sink.Write(buf[:n]); werr != nil {
				errKind = protocoltrace.ErrKind(werr)
				return
			}
			bytesSent += n
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			errKind = protocoltrace.ErrKind(err)
			return
		}
	}
	slog.Debug("blobs.get done", "hash", hashStr)
	_ = req.Close()
}

func (h *Handler) handleGetSlice(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("blobs.getSlice is a source handler"))
		return
	}

	var args struct {
		Hash  string `json:"hash"`
		Start int64  `json:"start"`
		Size  int64  `json:"size"`
	}
	if err := decodeSingleArg(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if args.Hash == "" {
		req.CloseWithError(errors.New("blobs.getSlice: no hash provided"))
		return
	}

	if args.Size <= 0 {
		args.Size = 4 << 10 // default 4KB
	}

	slog.Debug("blobs.getSlice", "hash", args.Hash, "start", args.Start, "size", args.Size)

	ref, err := refs.ParseBlobRef(args.Hash)
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.getSlice: invalid ref: %w", err))
		return
	}
	start := time.Now()
	bytesSent := 0
	errKind := ""
	defer func() {
		protocoltrace.Emit(protocoltrace.Event{
			Phase:    "blob_get_slice",
			Method:   "blobs.getSlice",
			BlobRef:  ref.String(),
			Bytes:    bytesSent,
			ErrKind:  errKind,
			Duration: time.Since(start),
		})
	}()

	rc, err := h.bs.GetRange(ref.Hash(), args.Start, args.Size)
	if err != nil {
		errKind = protocoltrace.ErrKind(err)
		slog.Debug("blobs.getSlice not found", "hash", args.Hash)
		req.CloseWithError(fmt.Errorf("blobs.getSlice: not found"))
		return
	}
	defer rc.Close()

	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.getSlice: get sink: %w", err))
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := sink.Write(buf[:n]); werr != nil {
				errKind = protocoltrace.ErrKind(werr)
				return
			}
			bytesSent += n
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			errKind = protocoltrace.ErrKind(err)
			return
		}
	}
	slog.Debug("blobs.getSlice done", "hash", args.Hash)
	_ = req.Close()
}

func (h *Handler) handleAdd(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sink" {
		req.CloseWithError(fmt.Errorf("blobs.add is a sink handler"))
		return
	}

	src, err := req.ResponseSource()
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.add: get source: %w", err))
		return
	}

	reader := &muxRPCBlobReader{ctx: ctx, src: src, max: DefaultMaxSize}
	start := time.Now()
	hash, err := h.bs.Put(reader)
	if err != nil {
		protocoltrace.Emit(protocoltrace.Event{
			Phase:    "blob_add",
			Method:   "blobs.add",
			Bytes:    int(reader.bytesRead),
			ErrKind:  protocoltrace.ErrKind(err),
			Duration: time.Since(start),
		})
		req.CloseWithError(fmt.Errorf("blobs.add: store failed: %w", err))
		return
	}

	blobRef, _ := refs.NewBlobRef(hash)
	protocoltrace.Emit(protocoltrace.Event{
		Phase:    "blob_add",
		Method:   "blobs.add",
		BlobRef:  blobRef.String(),
		Bytes:    int(reader.bytesRead),
		Duration: time.Since(start),
	})
	slog.Debug("blobs.add", "size", reader.bytesRead, "hash", blobRef.String())
	req.Return(ctx, blobRef.String())
}

func decodeArgs(raw []byte, v interface{}) error {
	return json.Unmarshal(raw, v)
}

func decodeSingleArg(raw []byte, v interface{}) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return errors.New("missing argument")
		}
		if len(arr) != 1 {
			return errors.New("expected exactly one argument")
		}
		return json.Unmarshal(arr[0], v)
	}
	return json.Unmarshal(raw, v)
}

func (h *Handler) handleCreateWants(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("blobs.createWants is a source handler"))
		return
	}

	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.createWants: get sink: %w", err))
		return
	}

	events, cancel := h.wm.Subscribe(ctx)
	defer cancel()

	wants, err := h.wm.Wants()
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.createWants: get wants: %w", err))
		return
	}

	for _, want := range wants {
		data, _ := json.Marshal(want.Ref())
		if _, err := sink.Write(data); err != nil {
			return
		}
		protocoltrace.Emit(protocoltrace.Event{
			Phase:   "blob_want",
			Method:  "blobs.createWants",
			BlobRef: want.Ref(),
		})
	}

	for {
		select {
		case <-ctx.Done():
			_ = req.Close()
			return
		case ev, ok := <-events:
			if !ok {
				_ = req.Close()
				return
			}
			protocoltrace.Emit(protocoltrace.Event{
				Phase:   "blob_want",
				Method:  "blobs.createWants",
				BlobRef: ev.Ref.Ref(),
			})
			if !ev.Want {
				continue
			}
			data, _ := json.Marshal(ev.Ref.Ref())
			if _, err := sink.Write(data); err != nil {
				return
			}
		}
	}
}

type muxRPCBlobReader struct {
	ctx       context.Context
	src       muxrpc.ByteSourceReader
	max       int64
	buf       []byte
	bytesRead int64
	err       error
}

func (r *muxRPCBlobReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	for len(r.buf) == 0 {
		if !r.src.Next(r.ctx) {
			if err := r.src.Err(); err != nil {
				r.err = err
				return 0, err
			}
			r.err = io.EOF
			return 0, io.EOF
		}
		b, err := r.src.Bytes()
		if err != nil {
			if err == io.EOF {
				continue
			}
			r.err = err
			return 0, err
		}
		if len(b) == 0 {
			continue
		}
		if r.max > 0 && r.bytesRead+int64(len(b)) > r.max {
			r.err = ErrBlobTooLarge
			return 0, r.err
		}
		r.bytesRead += int64(len(b))
		r.buf = append(r.buf[:0], b...)
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}
