package replication

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestNoteRoundTrip(t *testing.T) {
	// A valid ed25519 feed ref
	feed := "@6i7C39uHOD79Lz2I8N75M/E5xv8C0S6P2/9I8N75M/E=.ed25519"

	tests := []struct {
		name string
		note Note
	}{
		{"Replicate False", Note{Seq: 10, Replicate: false, Receive: true}},
		{"Replicate True, Receive True", Note{Seq: 10, Replicate: true, Receive: true}},
		{"Replicate True, Receive False", Note{Seq: 10, Replicate: true, Receive: false}},
		{"Seq 0", Note{Seq: 0, Replicate: true, Receive: true}},
		{"Seq -1", Note{Seq: -1, Replicate: true, Receive: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.note)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var nf NetworkFrontier
			wrapped := []byte(`{"` + feed + `": ` + string(b) + `}`)
			if err := json.Unmarshal(wrapped, &nf); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			got, ok := nf[feed]
			if !ok {
				t.Fatalf("Note for %s not found in unmarshaled result", feed)
			}

			if !tt.note.Replicate {
				if got.Replicate {
					t.Errorf("Replicate mismatch: got %v, want %v", got.Replicate, tt.note.Replicate)
				}
				return
			}

			if got.Replicate != tt.note.Replicate || got.Receive != tt.note.Receive {
				t.Errorf("Flags mismatch: got %+v, want %+v", got, tt.note)
			}

			expectedSeq := tt.note.Seq
			if expectedSeq == -1 {
				expectedSeq = 0 // Original behavior: -1 normalizes to 0
			}
			if got.Seq != expectedSeq {
				t.Errorf("Seq mismatch: got %d, want %d", got.Seq, expectedSeq)
			}
		})
	}
}

func TestStateMatrix(t *testing.T) {
	alice := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	bob := refs.MustNewFeedRef(append(make([]byte, 31), 1), refs.RefAlgoFeedSSB1)

	sm, _ := NewStateMatrix("", alice, nil)

	// Test SetFeedSeq
	sm.SetFeedSeq(alice, 10)

	frontier, _ := sm.Inspect(alice)
	if frontier[alice.String()].Seq != 10 {
		t.Errorf("expected seq 10, got %d", frontier[alice.String()].Seq)
	}

	// Test Changed (initial state)
	wants, _ := sm.Changed(alice, nil)
	if len(wants) != 1 || wants[alice.String()].Seq != 10 {
		t.Errorf("unexpected wants: %+v", wants)
	}

	// Test Update from peer
	bobFrontier := NetworkFrontier{
		alice.String(): Note{Seq: 5, Replicate: true, Receive: true},
	}
	sm.Update(bob, bobFrontier)

	// Test Changed (after peer update)
	wants, _ = sm.Changed(alice, bob)
	if wants[alice.String()].Seq != 5 {
		t.Errorf("expected peer's seq 5, got %d", wants[alice.String()].Seq)
	}
}

type mockWriter struct {
	data [][]byte
}

func (m *mockWriter) Write(ctx context.Context, data []byte) error {
	m.data = append(m.data, data)
	return nil
}

type mockByteSource struct {
	data [][]byte
	idx  int
}

func (m *mockByteSource) Next(ctx context.Context) bool {
	return m.idx < len(m.data)
}

func (m *mockByteSource) Bytes() ([]byte, error) {
	d := m.data[m.idx]
	m.idx++
	return d, nil
}

func (m *mockByteSource) Err() error { return nil }

type mockFeedManager struct {
	messages map[string]map[int64][]byte
}

func (m *mockFeedManager) GetFeedSeq(author *refs.FeedRef) (int64, error) {
	if feeds, ok := m.messages[author.String()]; ok {
		return int64(len(feeds)), nil
	}
	return -1, nil
}

func (m *mockFeedManager) GetMessage(author *refs.FeedRef, seq int64) ([]byte, error) {
	if feeds, ok := m.messages[author.String()]; ok {
		if msg, ok := feeds[seq]; ok {
			return msg, nil
		}
	}
	return nil, ErrNotFound
}

type mockLister struct {
	feeds []refs.FeedRef
}

func (m *mockLister) ListFeeds() ([]refs.FeedRef, error) {
	return m.feeds, nil
}

func TestEBTHandler(t *testing.T) {
	alice := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	bob := refs.MustNewFeedRef(append(make([]byte, 31), 1), refs.RefAlgoFeedSSB1)

	sm, _ := NewStateMatrix("", alice, nil)
	sm.SetFeedSeq(alice, 1)

	fm := &mockFeedManager{
		messages: map[string]map[int64][]byte{
			alice.String(): {
				1: []byte("alice message 1"),
			},
		},
	}

	handler := NewEBTHandler(alice, fm, sm, &mockLister{})

	tx := &mockWriter{}
	rx := &mockByteSource{
		data: [][]byte{
			[]byte(`{"` + alice.String() + `": 0}`), // peer wants alice feed from seq 0
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run HandleDuplex in a way we can stop it
	go func() {
		handler.HandleDuplex(ctx, tx, rx, "bob-addr", bob)
	}()

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	if len(tx.data) < 2 {
		t.Errorf("expected at least 2 packets (initial state + 1 message), got %d", len(tx.data))
	}
}

func TestNote_MarshalJSON_Uninitialized(t *testing.T) {
	note := Note{
		Seq:       -1,
		Replicate: true,
		Receive:   false,
	}

	b, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if string(b) != "-1" {
		t.Errorf("uninitialized feed (Seq=-1, Replicate=true) should marshal to -1, got %s", string(b))
	}
}

func TestNote_MarshalJSON_Normal(t *testing.T) {
	note := Note{
		Seq:       5,
		Replicate: true,
		Receive:   true,
	}

	b, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if string(b) != "10" {
		t.Errorf("Seq=5, Receive=true should marshal to 10 (5<<1|0), got %s", string(b))
	}
}
