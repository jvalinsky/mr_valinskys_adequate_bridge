package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
)

func TestFlattenManifest(t *testing.T) {
	methods := flattenManifest(map[string]interface{}{
		"manifest": "sync",
		"room": map[string]interface{}{
			"metadata":   "async",
			"attendants": "source",
		},
		"tunnel": map[string]interface{}{
			"connect":   "duplex",
			"endpoints": "source",
		},
	})
	want := []string{"manifest", "room.attendants", "room.metadata", "tunnel.connect", "tunnel.endpoints"}
	if strings.Join(methods, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
}

func TestSelectTargetUsesConfiguredFeed(t *testing.T) {
	author, _, _ := signedProbePayload(t, 1)
	target, err := selectTarget(author, "@self.ed25519", nil, nil)
	if err != nil {
		t.Fatalf("select configured target: %v", err)
	}
	if target.String() != author {
		t.Fatalf("target = %s, want %s", target.String(), author)
	}
}

func TestSelectTargetSkipsSelf(t *testing.T) {
	self, _, _ := signedProbePayload(t, 1)
	other, _, _ := signedProbePayload(t, 1)
	target, err := selectTarget("", self, []string{self}, []string{self, other})
	if err != nil {
		t.Fatalf("select target: %v", err)
	}
	if target.String() != other {
		t.Fatalf("target = %s, want %s", target.String(), other)
	}
}

func TestValidateClassicHistoryFrame(t *testing.T) {
	author, key, raw := signedProbePayload(t, 2)
	payload, err := json.Marshal(struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}{
		Key:   key,
		Value: raw,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	frame, err := validateClassicHistoryFrame(payload, author)
	if err != nil {
		t.Fatalf("validate frame: %v", err)
	}
	if frame.Key != key || frame.MessageRef != key || frame.Author != author || frame.Sequence != 2 || !frame.SignatureValid || frame.RawSHA256 == "" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func signedProbePayload(t *testing.T, sequence int64) (author string, key string, raw []byte) {
	t.Helper()
	kp, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	msg := legacy.Message{
		Previous:  nil,
		Author:    kp.FeedRef(),
		Sequence:  sequence,
		Timestamp: 12345 + sequence,
		Hash:      "sha256",
		Content: map[string]interface{}{
			"type": "post",
			"text": "probe",
		},
	}
	ref, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	raw, err = msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatalf("marshal signed message: %v", err)
	}
	return kp.FeedRef().String(), ref.String(), raw
}
