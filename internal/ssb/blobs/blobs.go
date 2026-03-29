package blobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

const DefaultMaxSize = 5 << 20

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
	Has(hash []byte) (bool, error)
	Size(hash []byte) (int64, error)
}

type WantManager interface {
	Want(ref *refs.BlobRef) error
	CancelWant(ref *refs.BlobRef) error
	Wants() ([]refs.BlobRef, error)
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
}

func NewWantManager(bs BlobStore) *WantManagerImpl {
	return &WantManagerImpl{
		bs:       bs,
		wants:    make(map[string]time.Time),
		canceled: make(map[string]struct{}),
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
	case req.Method.String() == "blobs.add":
		h.handleAdd(ctx, req)
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

	var args struct {
		Hash string `json:"hash"`
	}
	if err := decodeArgs(req.RawArgs, &args); err != nil {
		req.CloseWithError(err)
		return
	}

	if args.Hash == "" {
		req.CloseWithError(errors.New("blobs.get: no hash provided"))
		return
	}

	ref, err := refs.ParseBlobRef(args.Hash)
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.get: invalid ref: %w", err))
		return
	}

	rc, err := h.bs.Get(ref.Hash())
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.get: not found"))
		return
	}
	defer rc.Close()

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
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}
	}
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

	var allData []byte
	for src.Next(ctx) {
		b, err := src.Bytes()
		if err != nil {
			if err == io.EOF {
				break
			}
			req.CloseWithError(fmt.Errorf("blobs.add: read: %w", err))
			return
		}
		allData = append(allData, b...)
	}

	hash, err := h.bs.Put(bytes.NewReader(allData))
	if err != nil {
		req.CloseWithError(fmt.Errorf("blobs.add: store failed: %w", err))
		return
	}

	blobRef, _ := refs.NewBlobRef(hash)
	req.Return(ctx, blobRef.String())
}

func decodeArgs(raw []byte, v interface{}) error {
	return json.Unmarshal(raw, v)
}
