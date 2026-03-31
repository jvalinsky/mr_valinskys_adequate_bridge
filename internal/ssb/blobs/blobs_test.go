package blobs

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type mockBlobStore struct {
	blobs map[string][]byte
}

func (m *mockBlobStore) Put(r io.Reader) ([]byte, error) {
	data, _ := io.ReadAll(r)
	hash := refs.MustNewBlobRef(make([]byte, 32)).Hash() // dummy
	m.blobs[string(hash)] = data
	return hash, nil
}

func (m *mockBlobStore) Get(hash []byte) (io.ReadCloser, error) {
	if data, ok := m.blobs[string(hash)]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
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

	if err := wm.Want(ref); err != nil {
		t.Fatal(err)
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

	if wm.IsWanted(ref) {
		t.Error("expected ref to not be wanted")
	}
	if !wm.IsCanceled(ref) {
		t.Error("expected ref to be canceled")
	}
}
