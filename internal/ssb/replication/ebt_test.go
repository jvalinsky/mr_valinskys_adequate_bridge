package replication

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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

			if got.Replicate != tt.note.Replicate {
				t.Errorf("Replicate mismatch: got %v, want %v", got.Replicate, tt.note.Replicate)
			}

			if !tt.note.Replicate {
				return
			}

			if got.Receive != tt.note.Receive {
				t.Errorf("Receive mismatch: got %v, want %v", got.Receive, tt.note.Receive)
			}

			if got.Seq != tt.note.Seq {
				t.Errorf("Seq mismatch: got %d, want %d", got.Seq, tt.note.Seq)
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
	// Alice is at 10, Bob told us he has 5. We should tell Bob we are at 10.
	if wants[alice.String()].Seq != 10 {
		t.Errorf("expected our own seq 10, got %d", wants[alice.String()].Seq)
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
	mu        sync.Mutex
	messages  map[string]map[int64][]byte
	appendErr error
	appended  [][]byte
}

func (m *mockFeedManager) GetFeedSeq(author *refs.FeedRef) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if feeds, ok := m.messages[author.String()]; ok {
		return int64(len(feeds)), nil
	}
	return -1, nil
}

func (m *mockFeedManager) GetMessage(author *refs.FeedRef, seq int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if feeds, ok := m.messages[author.String()]; ok {
		if msg, ok := feeds[seq]; ok {
			return msg, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockFeedManager) AppendSignedMessage(raw []byte) (*refs.FeedRef, int64, error) {
	return m.AppendReplicatedMessage(raw)
}

func (m *mockFeedManager) AppendReplicatedMessage(raw []byte) (*refs.FeedRef, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appendErr != nil {
		return nil, 0, m.appendErr
	}
	m.appended = append(m.appended, append([]byte(nil), raw...))
	return nil, 0, nil
}

func (m *mockFeedManager) put(feed refs.FeedRef, seq int64, msg []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.messages == nil {
		m.messages = make(map[string]map[int64][]byte)
	}
	if m.messages[feed.String()] == nil {
		m.messages[feed.String()] = make(map[int64][]byte)
	}
	m.messages[feed.String()][seq] = msg
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

func TestEBTCreateStreamHistoryHonorsStartLimitAndRawBytes(t *testing.T) {
	alice := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	sm, _ := NewStateMatrix("", alice, nil)
	fm := &mockFeedManager{messages: map[string]map[int64][]byte{
		alice.String(): {
			1: []byte("skip"),
			2: []byte("send-2"),
			3: []byte("send-3"),
		},
	}}
	handler := NewEBTHandler(alice, fm, sm, &mockLister{})
	tx := &mockWriter{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := handler.createStreamHistory(ctx, tx, CreateHistArgs{ID: alice, Seq: 2, Limit: 2, Live: false}); err != nil {
		t.Fatalf("createStreamHistory: %v", err)
	}
	if len(tx.data) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(tx.data))
	}
	if string(tx.data[0]) != "send-2" || string(tx.data[1]) != "send-3" {
		t.Fatalf("unexpected messages: %q", tx.data)
	}
}

func TestEBTCreateStreamHistoryLiveStreamsNewMessage(t *testing.T) {
	alice := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	sm, _ := NewStateMatrix("", alice, nil)
	fm := &mockFeedManager{messages: map[string]map[int64][]byte{}}
	handler := NewEBTHandler(alice, fm, sm, &mockLister{})
	tx := &mockWriter{}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- handler.createStreamHistory(ctx, tx, CreateHistArgs{ID: alice, Seq: 1, Limit: 1, Live: true})
	}()
	time.Sleep(50 * time.Millisecond)
	fm.put(*alice, 1, []byte("live-1"))

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("createStreamHistory: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for live history")
	}
	if len(tx.data) != 1 || string(tx.data[0]) != "live-1" {
		t.Fatalf("unexpected live messages: %q", tx.data)
	}
}

func TestEBTHandleDuplexIgnoresAppendRejectionAndContinues(t *testing.T) {
	alice := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	bob := refs.MustNewFeedRef(append(make([]byte, 31), 1), refs.RefAlgoFeedSSB1)
	sm, _ := NewStateMatrix("", alice, nil)
	fm := &mockFeedManager{appendErr: errors.New("reject append")}
	handler := NewEBTHandler(alice, fm, sm, &mockLister{})
	tx := &mockWriter{}
	rx := &mockByteSource{data: [][]byte{
		[]byte("not-json-signed-message"),
		[]byte(`{"` + alice.String() + `": 0}`),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = handler.HandleDuplex(ctx, tx, rx, "bob-addr", bob)
	if len(tx.data) == 0 {
		t.Fatal("expected initial state or frontier response after rejected append")
	}
	if len(fm.appended) != 0 {
		t.Fatalf("rejected append should not be recorded, got %d", len(fm.appended))
	}
}

func TestNote_MarshalJSON_Uninitialized(t *testing.T) {
	tests := []struct {
		name string
		note Note
	}{
		{"Replicate False", Note{Seq: 0, Replicate: false, Receive: true}},
		{"Seq -1", Note{Seq: -1, Replicate: true, Receive: true}},
		{"Both", Note{Seq: -1, Replicate: false, Receive: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.note)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(b) != "-1" {
				t.Errorf("%s: expected -1, got %s", tt.name, string(b))
			}
		})
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
