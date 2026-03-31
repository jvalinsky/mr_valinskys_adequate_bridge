package sbot

import (
	"os"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
)

func TestSbot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sbot-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	aliceKeys, _ := keys.Generate()

	node, err := New(Options{
		RepoPath:   tempDir,
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create sbot: %v", err)
	}
	defer node.Shutdown()

	go func() {
		node.Serve()
	}()

	// Wait for node to start
	time.Sleep(100 * time.Millisecond)

	whoami, err := node.Whoami()
	if err != nil {
		t.Fatal(err)
	}
	if whoami != aliceKeys.FeedRef().String() {
		t.Errorf("expected %s, got %s", aliceKeys.FeedRef(), whoami)
	}

	// Test store access
	if node.Store() == nil {
		t.Error("expected non-nil store")
	}
	if node.EBT() == nil {
		t.Error("expected non-nil EBT handler")
	}
}

func TestSbotRoom(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sbot-room-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	aliceKeys, _ := keys.Generate()

	node, err := New(Options{
		RepoPath:   tempDir,
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		EnableRoom: true,
		RoomMode:   "open",
	})
	if err != nil {
		t.Fatalf("failed to create sbot with room: %v", err)
	}
	defer node.Shutdown()

	if node.handlerMux == nil {
		t.Error("expected handlerMux")
	}
}
