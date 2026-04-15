package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type fakeBridgedRoomPeerAccountLister struct {
	mu       sync.Mutex
	accounts []db.BridgedAccount
}

func (f *fakeBridgedRoomPeerAccountLister) GetAllBridgedAccounts(context.Context) ([]db.BridgedAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]db.BridgedAccount, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeBridgedRoomPeerAccountLister) setAccounts(accounts []db.BridgedAccount) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts = make([]db.BridgedAccount, len(accounts))
	copy(f.accounts, accounts)
}

func deriveFeedForDID(t *testing.T, seed, did string) string {
	t.Helper()
	mgr := bots.NewManager([]byte(seed), nil, nil)
	feed, err := mgr.GetFeedID(did)
	if err != nil {
		t.Fatalf("derive feed for %s: %v", did, err)
	}
	return feed.String()
}

func TestBridgedRoomPeerManagerReconcileStartsAndStopsSessions(t *testing.T) {
	const seed = "test-bridged-room-peer-seed"
	const did = "did:plc:active-peer"

	lister := &fakeBridgedRoomPeerAccountLister{}
	lister.setAccounts([]db.BridgedAccount{
		{ATDID: did, SSBFeedID: deriveFeedForDID(t, seed, did), Active: true},
		{ATDID: "did:plc:inactive-peer", SSBFeedID: deriveFeedForDID(t, seed, "did:plc:inactive-peer"), Active: false},
	})

	manager, err := newBridgedRoomPeerManager(bridgedRoomPeerManagerConfig{
		AccountLister: lister,
		BotSeed:       seed,
		SyncInterval:  10 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.ensureMemberFn = func(context.Context, refs.FeedRef) error { return nil }

	var runMu sync.Mutex
	started := make(map[string]int)
	stopped := make(map[string]int)
	manager.runSessionFn = func(ctx context.Context, sess *bridgedRoomPeerSession) {
		runMu.Lock()
		started[sess.did]++
		runMu.Unlock()
		<-ctx.Done()
		runMu.Lock()
		stopped[sess.did]++
		runMu.Unlock()
	}

	manager.mu.Lock()
	manager.started = true
	manager.ctx = context.Background()
	manager.mu.Unlock()

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile active: %v", err)
	}

	requireEventually(t, time.Second, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		_, ok := manager.sessions[did]
		return ok
	}, "active DID session to start")
	requireEventually(t, time.Second, func() bool {
		runMu.Lock()
		defer runMu.Unlock()
		return started[did] == 1
	}, "session run loop start")

	lister.setAccounts([]db.BridgedAccount{
		{ATDID: did, SSBFeedID: deriveFeedForDID(t, seed, did), Active: false},
	})

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile inactive: %v", err)
	}

	requireEventually(t, time.Second, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return len(manager.sessions) == 0
	}, "session map to be empty after deactivation")

	runMu.Lock()
	if stopped[did] != 1 {
		runMu.Unlock()
		t.Fatalf("expected one session stop for %s, got %d", did, stopped[did])
	}
	runMu.Unlock()
}

func TestBridgedRoomPeerManagerReconcileSkipsInvalidMappings(t *testing.T) {
	const seed = "test-bridged-room-peer-seed-invalid"
	const did = "did:plc:mismatch"

	validFeed := deriveFeedForDID(t, seed, did)
	wrongFeed := deriveFeedForDID(t, seed, "did:plc:other")

	lister := &fakeBridgedRoomPeerAccountLister{}
	lister.setAccounts([]db.BridgedAccount{
		{ATDID: did, SSBFeedID: wrongFeed, Active: true},
		{ATDID: "did:plc:invalid-feed", SSBFeedID: "not-a-feed-ref", Active: true},
		{ATDID: "did:plc:valid", SSBFeedID: validFeed, Active: false},
	})

	manager, err := newBridgedRoomPeerManager(bridgedRoomPeerManagerConfig{
		AccountLister: lister,
		BotSeed:       seed,
	}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	memberCalls := 0
	manager.ensureMemberFn = func(context.Context, refs.FeedRef) error {
		memberCalls++
		return nil
	}
	manager.runSessionFn = func(context.Context, *bridgedRoomPeerSession) {}

	manager.mu.Lock()
	manager.started = true
	manager.ctx = context.Background()
	manager.mu.Unlock()

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	manager.mu.Lock()
	sessionCount := len(manager.sessions)
	manager.mu.Unlock()

	if sessionCount != 0 {
		t.Fatalf("expected no sessions for invalid/mismatched mappings, got %d", sessionCount)
	}
	if memberCalls != 0 {
		t.Fatalf("expected no membership calls for invalid/mismatched mappings, got %d", memberCalls)
	}
}

func TestBridgedRoomPeerManagerReconcileRetriesAfterMembershipFailure(t *testing.T) {
	const seed = "test-bridged-room-peer-seed-member"
	const did = "did:plc:member-retry"

	lister := &fakeBridgedRoomPeerAccountLister{}
	lister.setAccounts([]db.BridgedAccount{
		{ATDID: did, SSBFeedID: deriveFeedForDID(t, seed, did), Active: true},
	})

	manager, err := newBridgedRoomPeerManager(bridgedRoomPeerManagerConfig{
		AccountLister: lister,
		BotSeed:       seed,
	}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	failMembership := true
	manager.ensureMemberFn = func(context.Context, refs.FeedRef) error {
		if failMembership {
			return fmt.Errorf("transient membership failure")
		}
		return nil
	}
	manager.runSessionFn = func(ctx context.Context, _ *bridgedRoomPeerSession) {
		<-ctx.Done()
	}

	manager.mu.Lock()
	manager.started = true
	manager.ctx = context.Background()
	manager.mu.Unlock()

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile with membership failure: %v", err)
	}

	manager.mu.Lock()
	initialSessionCount := len(manager.sessions)
	manager.mu.Unlock()
	if initialSessionCount != 0 {
		t.Fatalf("expected no session after membership failure, got %d", initialSessionCount)
	}

	failMembership = false
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile retry: %v", err)
	}

	requireEventually(t, time.Second, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		_, ok := manager.sessions[did]
		return ok
	}, "session start after membership recovery")

	manager.stopSession(did)
}

func requireEventually(t *testing.T, timeout time.Duration, fn func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}
