package main

import "testing"

func TestResolveInviteConsumeTargetPrefersConfiguredLoopbackBase(t *testing.T) {
	t.Parallel()

	token, baseURL, err := resolveInviteConsumeTarget(
		"http://127.0.0.1:9876/join?token=abc123",
		"http://127.0.0.1:9876",
	)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if token != "abc123" {
		t.Fatalf("token mismatch: got %q want %q", token, "abc123")
	}
	if baseURL != "http://127.0.0.1:9876" {
		t.Fatalf("base url mismatch: got %q want %q", baseURL, "http://127.0.0.1:9876")
	}
}

func TestResolveInviteConsumeTargetFallsBackToBridgeForLoopbackWithoutConfiguredBase(t *testing.T) {
	t.Parallel()

	token, baseURL, err := resolveInviteConsumeTarget(
		"http://localhost:8976/join?invite=invite-token",
		"",
	)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if token != "invite-token" {
		t.Fatalf("token mismatch: got %q want %q", token, "invite-token")
	}
	if baseURL != "http://bridge:8976" {
		t.Fatalf("base url mismatch: got %q want %q", baseURL, "http://bridge:8976")
	}
}

func TestResolveInviteConsumeTargetUsesInviteHostForRemoteRooms(t *testing.T) {
	t.Parallel()

	token, baseURL, err := resolveInviteConsumeTarget(
		"https://room.example.com/join?token=remote-token",
		"http://127.0.0.1:9876",
	)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if token != "remote-token" {
		t.Fatalf("token mismatch: got %q want %q", token, "remote-token")
	}
	if baseURL != "https://room.example.com" {
		t.Fatalf("base url mismatch: got %q want %q", baseURL, "https://room.example.com")
	}
}
