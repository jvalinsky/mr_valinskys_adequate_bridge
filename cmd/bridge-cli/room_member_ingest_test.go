package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
)

func TestRoomMemberIngestHistoryFrameRebuildsSignedMessageForReceiveLog(t *testing.T) {
	tempDir := t.TempDir()

	bridgeKeys, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate bridge keys: %v", err)
	}
	node, err := sbot.New(sbot.Options{
		RepoPath:   filepath.Join(tempDir, "bridge-repo"),
		ListenAddr: "127.0.0.1:0",
		KeyPair:    bridgeKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("create bridge sbot: %v", err)
	}
	defer node.Shutdown()

	receiveLog, err := node.Store().ReceiveLog()
	if err != nil {
		t.Fatalf("open receive log: %v", err)
	}

	manager := &roomMemberIngestManager{
		cfg: roomMemberIngestManagerConfig{
			Sbot:       node,
			ReceiveLog: receiveLog,
			Store:      node.Store(),
		},
		logger: log.New(io.Discard, "", 0),
	}

	sourceKeys, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate source keys: %v", err)
	}

	content := map[string]any{
		"type": "post",
		"text": "hello from room member ingest",
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}

	msg := &legacy.Message{
		Author:    sourceKeys.FeedRef(),
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Hash:      legacy.HashAlgorithm,
		Content:   content,
	}
	msgRef, sig, err := msg.Sign(sourceKeys, nil)
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}

	payload, err := json.Marshal(roomHistoryEnvelope{
		Key: msgRef.String(),
		Value: roomHistorySignedValue{
			Author:    sourceKeys.FeedRef().String(),
			Sequence:  msg.Sequence,
			Timestamp: msg.Timestamp,
			Hash:      msg.Hash,
			Content:   json.RawMessage(contentJSON),
			Signature: base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519",
		},
	})
	if err != nil {
		t.Fatalf("marshal room history envelope: %v", err)
	}

	if err := manager.ingestHistoryFrame(sourceKeys.FeedRef(), payload); err != nil {
		t.Fatalf("ingest history frame: %v", err)
	}

	rxMsg, err := receiveLog.Get(1)
	if err != nil {
		t.Fatalf("read receive log message: %v", err)
	}
	if _, err := legacy.VerifySignedMessageJSON(rxMsg.Value); err != nil {
		t.Fatalf("verify receive log signed message: %v", err)
	}
	if got, want := rxMsg.Metadata.Hash, msgRef.String(); got != want {
		t.Fatalf("receive log hash = %s, want %s", got, want)
	}
}
