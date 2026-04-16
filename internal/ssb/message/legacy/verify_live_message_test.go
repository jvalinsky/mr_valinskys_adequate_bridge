package legacy_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestVerifyCapturedLiveBridgeAboutMessage(t *testing.T) {
	// From flume.sqlite on snek.cc: actual bridge-authored about message.
	pubkeyB64 := "BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY="
	sigB64 := "MEjdUhciFbkLZGqVvbwcrNyG/YDhOlK1QrMpQkMn4WV6fFGkDsSzPDhccGKYhxGULEb5vDI6KZgRV2/OQBr0Cg=="
	contentB64 := "eyJhYm91dCI6IkBCa21nUDFHVnJsajdRdkNtYXpPVGJ6Uys4WStDOUh4aTRURXVYa1J1ZFNZPS5lZDI1NTE5IiwiYmFubmVyIjp7ImxpbmsiOiJcdTAwMjZPRTJ1MVpKSEhZMzRMcVMyeDJlb0RUdWVvZnozRFlOL2taSEVhOE9oNmRjPS5zaGEyNTYiLCJzaXplIjo2NDk5NzMsInR5cGUiOiJpbWFnZS9qcGVnIn0sImRlc2NyaXB0aW9uIjoiTVMgQ29tcHV0YXRpb25hbCBMaW5ndWlzdGljcyBzdHVkZW50XG5GaWxtIHBob3RvZ3JhcGh5IPCfjp7vuI8g8J+TuFxuSW50ZXJlc3RlZCBpbiBsYW5ndWFnZXM6IGdhLCBkZSwgamEsIHpoLCBoYXciLCJpbWFnZSI6eyJsaW5rIjoiXHUwMDI2M0NhQmpkMVVORGhRS2ljVC9NWWxrTlplbXFLZjhuT1BNNmlGYnplL2s5TT0uc2hhMjU2Iiwic2l6ZSI6MjEwODIwLCJ0eXBlIjoiaW1hZ2UvanBlZyJ9LCJuYW1lIjoiSmFjayIsInR5cGUiOiJhYm91dCJ9"

	timestamp := int64(1776175989621)

	// Decode
	pubkey, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		t.Fatalf("failed to decode pubkey: %v", err)
	}

	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatalf("failed to decode signature: %v", err)
	}

	contentJSON, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		t.Fatalf("failed to decode content: %v", err)
	}

	// Parse content as map
	var content map[string]interface{}
	if err := json.Unmarshal(contentJSON, &content); err != nil {
		t.Fatalf("failed to parse content: %v", err)
	}

	// Create feed ref
	feedRef, err := refs.NewFeedRef(pubkey, refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	t.Logf("Feed ref: %s", feedRef.String())

	// Build message struct
	msg := &legacy.Message{
		Previous:  nil,
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: timestamp,
		Hash:      legacy.HashAlgorithm,
		Content:   content,
	}

	// Get canonical bytes for signing
	contentToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	t.Logf("Content to sign (len=%d):\n%s", len(contentToSign), string(contentToSign))

	// Verify signature
	if !ed25519.Verify(pubkey, contentToSign, sig) {
		t.Fatal("direct Ed25519 signature verification failed")
	}

	rawSigned, err := msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatalf("failed to marshal signed message: %v", err)
	}
	if _, err := legacy.VerifySignedMessageJSON(rawSigned); err != nil {
		t.Fatalf("legacy signed-message verifier rejected captured message: %v", err)
	}
}
