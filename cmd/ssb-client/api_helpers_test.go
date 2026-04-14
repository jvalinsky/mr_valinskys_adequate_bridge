package main

import "testing"

func TestReplicationTargetFromContact(t *testing.T) {
	t.Parallel()

	t.Run("follows contact with boolean flags", func(t *testing.T) {
		target, ok := replicationTargetFromContact(map[string]interface{}{
			"type":      "contact",
			"contact":   "@alice.ed25519",
			"following": true,
			"blocking":  false,
		})
		if !ok {
			t.Fatalf("expected replicate target")
		}
		if target != "@alice.ed25519" {
			t.Fatalf("target mismatch: got %q", target)
		}
	})

	t.Run("normalizes feed without at-prefix", func(t *testing.T) {
		target, ok := replicationTargetFromContact(map[string]interface{}{
			"type":      "contact",
			"contact":   "bob.ed25519",
			"following": true,
		})
		if !ok {
			t.Fatalf("expected replicate target")
		}
		if target != "@bob.ed25519" {
			t.Fatalf("target mismatch: got %q", target)
		}
	})

	t.Run("handles form-encoded booleans", func(t *testing.T) {
		target, ok := replicationTargetFromContact(map[string]interface{}{
			"type":      "contact",
			"contact":   "@carol.ed25519",
			"following": "true",
			"blocking":  "false",
		})
		if !ok {
			t.Fatalf("expected replicate target")
		}
		if target != "@carol.ed25519" {
			t.Fatalf("target mismatch: got %q", target)
		}
	})

	t.Run("does not replicate unfollow", func(t *testing.T) {
		if _, ok := replicationTargetFromContact(map[string]interface{}{
			"type":      "contact",
			"contact":   "@alice.ed25519",
			"following": false,
		}); ok {
			t.Fatalf("expected no replicate target")
		}
	})

	t.Run("does not replicate blocking contact", func(t *testing.T) {
		if _, ok := replicationTargetFromContact(map[string]interface{}{
			"type":      "contact",
			"contact":   "@alice.ed25519",
			"following": true,
			"blocking":  true,
		}); ok {
			t.Fatalf("expected no replicate target")
		}
	})

	t.Run("ignores non-contact message types", func(t *testing.T) {
		if _, ok := replicationTargetFromContact(map[string]interface{}{
			"type": "post",
			"text": "hello",
		}); ok {
			t.Fatalf("expected no replicate target")
		}
	})
}

func TestResolvePeerConnectTarget(t *testing.T) {
	t.Parallel()

	t.Run("parses multiserver input", func(t *testing.T) {
		addr, pk, err := resolvePeerConnectTarget("net:localhost:8008~shs:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != "localhost:8008" {
			t.Fatalf("address mismatch: %q", addr)
		}
		if len(pk) != 32 {
			t.Fatalf("expected 32-byte key, got %d", len(pk))
		}
	})

	t.Run("parses address field when it contains multiserver", func(t *testing.T) {
		addr, pk, err := resolvePeerConnectTarget("", "net:127.0.0.1:8011~shs:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != "127.0.0.1:8011" {
			t.Fatalf("address mismatch: %q", addr)
		}
		if len(pk) != 32 {
			t.Fatalf("expected 32-byte key, got %d", len(pk))
		}
	})

	t.Run("parses host and public key fallback", func(t *testing.T) {
		addr, pk, err := resolvePeerConnectTarget("", "room.example.com:8008", "@AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=.ed25519")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != "room.example.com:8008" {
			t.Fatalf("address mismatch: %q", addr)
		}
		if len(pk) != 32 {
			t.Fatalf("expected 32-byte key, got %d", len(pk))
		}
	})

	t.Run("fails without key for plain host", func(t *testing.T) {
		if _, _, err := resolvePeerConnectTarget("", "localhost:8008", ""); err == nil {
			t.Fatalf("expected error")
		}
	})
}
