package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
)

func TestRoomMemberIngestHistoryFrameRebuildsSignedMessageForReceiveLog(t *testing.T) {
	manager, receiveLog := newRoomMemberIngestTestManager(t)

	sourceKeys, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate source keys: %v", err)
	}
	payload, msgRef := signedRoomHistoryPayload(t, sourceKeys, true)

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
	if got, want := rxMsg.Metadata.Hash, msgRef; got != want {
		t.Fatalf("receive log hash = %s, want %s", got, want)
	}
}

func TestRoomMemberIngestHistoryFrameAcceptsDirectSignedMessage(t *testing.T) {
	manager, receiveLog := newRoomMemberIngestTestManager(t)

	sourceKeys, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate source keys: %v", err)
	}
	payload, msgRef := signedRoomHistoryPayload(t, sourceKeys, false)

	if err := manager.ingestHistoryFrame(sourceKeys.FeedRef(), payload); err != nil {
		t.Fatalf("ingest direct history frame: %v", err)
	}

	rxMsg, err := receiveLog.Get(1)
	if err != nil {
		t.Fatalf("read receive log message: %v", err)
	}
	if _, err := legacy.VerifySignedMessageJSON(rxMsg.Value); err != nil {
		t.Fatalf("verify receive log signed message: %v", err)
	}
	if got, want := rxMsg.Metadata.Hash, msgRef; got != want {
		t.Fatalf("receive log hash = %s, want %s", got, want)
	}
}

func TestDecodeRoomHistoryEnvelopeClassifiesNonClassicFrame(t *testing.T) {
	_, err := decodeRoomHistoryEnvelope([]byte(`{"name":["ebt","replicate"],"args":[{}]}`))
	if !errors.Is(err, errRoomHistoryFrameNonClassic) {
		t.Fatalf("decode non-classic frame error = %v, want %v", err, errRoomHistoryFrameNonClassic)
	}

	manager, _ := newRoomMemberIngestTestManager(t)
	sourceKeys, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate source keys: %v", err)
	}
	incompleteClassic, err := json.Marshal(roomHistoryEnvelope{
		Value: roomHistorySignedValue{Author: sourceKeys.FeedRef().String()},
	})
	if err != nil {
		t.Fatalf("marshal incomplete classic frame: %v", err)
	}
	err = manager.ingestHistoryFrame(sourceKeys.FeedRef(), incompleteClassic)
	if err == nil || errors.Is(err, errRoomHistoryFrameNonClassic) {
		t.Fatalf("decode incomplete classic frame error = %v, want strict classic validation error later", err)
	}
}

func newRoomMemberIngestTestManager(t *testing.T) (*roomMemberIngestManager, feedlog.Log) {
	t.Helper()

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
	t.Cleanup(func() {
		_ = node.Shutdown()
	})

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
	return manager, receiveLog
}

func signedRoomHistoryPayload(t *testing.T, sourceKeys *keys.KeyPair, keyed bool) ([]byte, string) {
	t.Helper()

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

	value := roomHistorySignedValue{
		Author:    sourceKeys.FeedRef().String(),
		Sequence:  msg.Sequence,
		Timestamp: msg.Timestamp,
		Hash:      msg.Hash,
		Content:   json.RawMessage(contentJSON),
		Signature: base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519",
	}
	if !keyed {
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal direct room history value: %v", err)
		}
		return payload, msgRef.String()
	}

	payload, err := json.Marshal(roomHistoryEnvelope{
		Key:   msgRef.String(),
		Value: value,
	})
	if err != nil {
		t.Fatalf("marshal room history envelope: %v", err)
	}
	return payload, msgRef.String()
}
