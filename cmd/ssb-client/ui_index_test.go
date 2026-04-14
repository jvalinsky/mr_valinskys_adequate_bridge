package main

import (
	"encoding/json"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
)

func TestBuildIndexedMessageContact(t *testing.T) {
	payload := map[string]interface{}{
		"type":      "contact",
		"contact":   "someone.ed25519",
		"following": true,
		"blocking":  false,
	}
	raw, _ := json.Marshal(payload)

	msg := feedlog.StoredMessage{
		Key:      "%abc.sha256",
		Value:    raw,
		Received: 123,
		Metadata: &feedlog.Metadata{Author: "@me.ed25519", Sequence: 4, Timestamp: 111},
	}

	indexed := buildIndexedMessage(msg, "@me.ed25519", nil)
	if indexed.Type != "contact" {
		t.Fatalf("type mismatch: got %q", indexed.Type)
	}
	if indexed.Contact != "@someone.ed25519" {
		t.Fatalf("contact mismatch: got %q", indexed.Contact)
	}
	if !indexed.Following {
		t.Fatal("expected following=true")
	}
	if indexed.Blocking {
		t.Fatal("expected blocking=false")
	}
}

func TestBuildIndexedMessageVote(t *testing.T) {
	payload := map[string]interface{}{
		"type": "vote",
		"vote": map[string]interface{}{
			"link":  "%root.sha256",
			"value": 1,
		},
		"root": "%root.sha256",
	}
	raw, _ := json.Marshal(payload)

	msg := feedlog.StoredMessage{
		Key:      "%vote.sha256",
		Value:    raw,
		Received: 123,
		Metadata: &feedlog.Metadata{Author: "@me.ed25519", Sequence: 7, Timestamp: 222},
	}

	indexed := buildIndexedMessage(msg, "@me.ed25519", nil)
	if indexed.Type != "vote" {
		t.Fatalf("type mismatch: got %q", indexed.Type)
	}
	if indexed.VoteLink != "%root.sha256" {
		t.Fatalf("vote link mismatch: got %q", indexed.VoteLink)
	}
	if indexed.VoteValue != 1 {
		t.Fatalf("vote value mismatch: got %d", indexed.VoteValue)
	}
	if indexed.Root != "%root.sha256" {
		t.Fatalf("root mismatch: got %q", indexed.Root)
	}
}

func TestFirstRefHelpers(t *testing.T) {
	branch := []interface{}{"%a.sha256", "%b.sha256"}
	if got := firstRef(branch); got != "%a.sha256" {
		t.Fatalf("firstRef array mismatch: got %q", got)
	}

	linkObj := map[string]interface{}{"link": "%x.sha256"}
	if got := firstRef(linkObj); got != "%x.sha256" {
		t.Fatalf("firstRef map mismatch: got %q", got)
	}

	if got := normalizeFeed("abc.ed25519"); got != "@abc.ed25519" {
		t.Fatalf("normalizeFeed mismatch: got %q", got)
	}
}
