package blobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type mockBlobStore struct {
	blobs map[string][]byte
}

func (m *mockBlobStore) Put(r io.Reader) ([]byte, error) {
	data, _ := io.ReadAll(r)
	h := sha256.Sum256(data)
	hash := h[:]
	m.blobs[string(hash)] = data
	return hash, nil
}

func (m *mockBlobStore) Get(hash []byte) (io.ReadCloser, error) {
	if data, ok := m.blobs[string(hash)]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, errors.New("not found")
}

func (m *mockBlobStore) GetRange(hash []byte, start, size int64) (io.ReadCloser, error) {
	if data, ok := m.blobs[string(hash)]; ok {
		if start >= int64(len(data)) {
			return nil, errors.New("offset out of bounds")
		}
		end := start + size
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		return io.NopCloser(bytes.NewReader(data[start:end])), nil
	}
	return nil, errors.New("not found")
}

func (m *mockBlobStore) Has(hash []byte) (bool, error) {
	_, ok := m.blobs[string(hash)]
	return ok, nil
}

func (m *mockBlobStore) Size(hash []byte) (int64, error) {
	if data, ok := m.blobs[string(hash)]; ok {
		return int64(len(data)), nil
	}
	return 0, nil
}

func TestWantManager(t *testing.T) {
	wm := NewWantManager(&mockBlobStore{})
	ref := refs.MustNewBlobRef(make([]byte, 32))
	subCtx, cancelSub := context.WithCancel(context.Background())
	events, stop := wm.Subscribe(subCtx)
	defer stop()
	defer cancelSub()

	if err := wm.Want(ref); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if !ev.Want || !ev.Ref.Equal(*ref) {
			t.Fatalf("unexpected want event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for want event")
	}

	if !wm.IsWanted(ref) {
		t.Error("expected ref to be wanted")
	}

	wants, _ := wm.Wants()
	if len(wants) != 1 {
		t.Errorf("expected 1 want, got %d", len(wants))
	}

	if err := wm.CancelWant(ref); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Want || !ev.Ref.Equal(*ref) {
			t.Fatalf("unexpected cancel event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancel event")
	}

	if wm.IsWanted(ref) {
		t.Error("expected ref to not be wanted")
	}
	if !wm.IsCanceled(ref) {
		t.Error("expected ref to be canceled")
	}
}

func TestBlobsHandler(t *testing.T) {
	bs := &mockBlobStore{blobs: make(map[string][]byte)}
	wm := NewWantManager(bs)
	self := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)

	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := NewPlugin(self, bs, wm, nil)
	server := muxrpc.NewRPC(ctx, p2, handler, nil)
	defer server.Terminate()

	client := muxrpc.NewRPC(ctx, p1, nil, nil)
	defer client.Terminate()

	t.Run("has", func(t *testing.T) {
		h := sha256.Sum256([]byte("data"))
		ref, _ := refs.NewBlobRef(h[:])
		bs.blobs[string(ref.Hash())] = []byte("data")

		var has bool
		err := client.Async(ctx, &has, muxrpc.TypeJSON, muxrpc.Method{"blobs", "has"}, ref.String())
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Error("expected true")
		}
	})

	t.Run("get", func(t *testing.T) {
		h := sha256.Sum256([]byte("data"))
		ref, _ := refs.NewBlobRef(h[:])
		bs.blobs[string(ref.Hash())] = []byte("data")

		src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"blobs", "get"}, map[string]string{"hash": ref.String()})
		if err != nil {
			t.Fatal(err)
		}
		if src.Next(ctx) {
			got, _ := src.Bytes()
			if string(got) != "data" {
				t.Errorf("got %s, want data", got)
			}
		}
	})

	t.Run("createWants stays open when there are no wants", func(t *testing.T) {
		src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"blobs", "createWants"})
		if err != nil {
			t.Fatal(err)
		}

		shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		if src.Next(shortCtx) {
			t.Fatal("expected no immediate wants")
		}
		if !errors.Is(src.Err(), context.DeadlineExceeded) {
			t.Fatalf("expected source to remain open until caller context deadline, got %v", src.Err())
		}
	})

	t.Run("createWants emits wants added after stream opens", func(t *testing.T) {
		src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"blobs", "createWants"})
		if err != nil {
			t.Fatal(err)
		}
		defer src.Cancel(nil)

		h := sha256.Sum256([]byte("wanted after open"))
		ref, _ := refs.NewBlobRef(h[:])
		if err := wm.Want(ref); err != nil {
			t.Fatal(err)
		}

		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if !src.Next(readCtx) {
			t.Fatalf("expected live want event, err=%v", src.Err())
		}
		got, err := src.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		var gotRef string
		if err := json.Unmarshal(got, &gotRef); err != nil {
			t.Fatalf("decode want frame %q: %v", got, err)
		}
		if gotRef != ref.Ref() {
			t.Fatalf("want ref = %s, want %s", gotRef, ref.Ref())
		}
	})

	t.Run("add", func(t *testing.T) {
		sink, err := client.Sink(ctx, muxrpc.TypeBinary, muxrpc.Method{"blobs", "add"})
		if err != nil {
			t.Fatal(err)
		}
		sink.Write([]byte("new blob"))
		sink.Close()

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		found := false
		for _, data := range bs.blobs {
			if string(data) == "new blob" {
				found = true
				break
			}
		}
		if !found {
			t.Error("blob not added")
		}
	})
}

type fakeByteSource struct {
	frames [][]byte
	idx    int
	err    error
}

func (s *fakeByteSource) Next(ctx context.Context) bool {
	return s.idx < len(s.frames)
}

func (s *fakeByteSource) Bytes() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	b := s.frames[s.idx]
	s.idx++
	return b, nil
}

func (s *fakeByteSource) Err() error { return nil }

func TestMuxRPCBlobReaderEnforcesMaxSize(t *testing.T) {
	reader := &muxRPCBlobReader{
		ctx: context.Background(),
		src: &fakeByteSource{frames: [][]byte{[]byte("1234"), []byte("56")}},
		max: 5,
	}
	got, err := io.ReadAll(reader)
	if !errors.Is(err, ErrBlobTooLarge) {
		t.Fatalf("expected ErrBlobTooLarge, got data=%q err=%v", got, err)
	}
}
