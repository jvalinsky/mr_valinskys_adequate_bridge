package publishqueue

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"testing"
)

type recordingPublisher struct {
	mu      sync.Mutex
	perDID  map[string][]int
	counter int
}

func (r *recordingPublisher) Publish(_ context.Context, atDID string, content map[string]interface{}) (string, error) {
	seq, _ := content["seq"].(int)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perDID == nil {
		r.perDID = make(map[string][]int)
	}
	r.perDID[atDID] = append(r.perDID[atDID], seq)
	r.counter++
	return fmt.Sprintf("%%%d.sha256", r.counter), nil
}

func TestWorkerPublisherPreservesPerDIDOrder(t *testing.T) {
	delegate := &recordingPublisher{}
	publisher := New(delegate, 4, log.New(io.Discard, "", 0))
	defer publisher.Close()

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, err := publisher.Publish(ctx, "did:plc:alice", map[string]interface{}{"seq": i})
			if err != nil {
				t.Errorf("publish alice[%d]: %v", i, err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, err := publisher.Publish(ctx, "did:plc:bob", map[string]interface{}{"seq": i})
			if err != nil {
				t.Errorf("publish bob[%d]: %v", i, err)
				return
			}
		}
	}()

	wg.Wait()

	delegate.mu.Lock()
	defer delegate.mu.Unlock()

	assertSequence := func(did string) {
		seqs := delegate.perDID[did]
		if len(seqs) != 50 {
			t.Fatalf("expected 50 entries for %s, got %d", did, len(seqs))
		}
		for i, seq := range seqs {
			if seq != i {
				t.Fatalf("expected %s[%d]=%d, got %d", did, i, i, seq)
			}
		}
	}

	assertSequence("did:plc:alice")
	assertSequence("did:plc:bob")
}

func TestPublisherWhenNil(t *testing.T) {
	var p *WorkerPublisher = nil
	_, err := p.Publish(context.Background(), "did:plc:alice", map[string]interface{}{})
	if err == nil {
		t.Errorf("Expected error for nil publisher")
	}
	
	err = p.Close()
	if err != nil {
		t.Errorf("Expected nil error for closing nil publisher")
	}
}

func TestPublisherWhenDelegateNil(t *testing.T) {
	p := &WorkerPublisher{}
	_, err := p.Publish(context.Background(), "did:plc:alice", map[string]interface{}{})
	if err == nil {
		t.Errorf("Expected error for nil delegate")
	}
}

func TestNewWithZeroWorkers(t *testing.T) {
	mock := &recordingPublisher{}
	p := New(mock, 0, log.New(io.Discard, "", 0))
	defer p.Close()

	ref, err := p.Publish(context.Background(), "did:plc:alice", map[string]interface{}{"seq": 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == "" {
		t.Fatal("expected non-empty ref")
	}
}

func TestPublishContextCancellationWaitingResponse(t *testing.T) {
	// Delegate that blocks forever
	blocker := make(chan struct{})
	delegate := publisherFunc(func(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
		<-blocker
		return "", nil
	})
	p := New(delegate, 1, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel while waiting for response
		cancel()
	}()
	_, err := p.Publish(ctx, "did:plc:alice", map[string]interface{}{})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	close(blocker)
	p.Close()
}

type publisherFunc func(ctx context.Context, atDID string, content map[string]interface{}) (string, error)

func (f publisherFunc) Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
	return f(ctx, atDID, content)
}

func TestPublishContextCancellationEnqueuing(t *testing.T) {
	mock := &recordingPublisher{}
	p := New(mock, 1, log.New(io.Discard, "", 0))
	
	// Create context that is already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	_, err := p.Publish(ctx, "did:plc:alice", map[string]interface{}{})
	if err == nil || err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
	p.Close()
}
