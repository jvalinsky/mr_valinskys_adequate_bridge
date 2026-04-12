package main

import (
	"strings"
	"testing"
)

func TestResolveServeUIAuthConfig(t *testing.T) {
	t.Run("loopback without auth is allowed", func(t *testing.T) {
		user, pass, configured, err := resolveServeUIAuthConfig("127.0.0.1:8080", "", "", func(string) string {
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if configured {
			t.Fatalf("expected auth not configured")
		}
		if user != "" || pass != "" {
			t.Fatalf("expected empty credentials")
		}
	})

	t.Run("non-loopback without auth is rejected", func(t *testing.T) {
		_, _, _, err := resolveServeUIAuthConfig("0.0.0.0:8080", "", "", func(string) string {
			return ""
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "refusing to serve UI/API on non-loopback") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing username is rejected when pass env is set", func(t *testing.T) {
		_, _, _, err := resolveServeUIAuthConfig("127.0.0.1:8080", "", "SSB_UI_PASS", func(key string) string {
			if key == "SSB_UI_PASS" {
				return "secret"
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "--ui-auth-user is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing pass env is rejected when username is set", func(t *testing.T) {
		_, _, _, err := resolveServeUIAuthConfig("127.0.0.1:8080", "admin", "", func(string) string {
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "--ui-auth-pass-env is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty password env value is rejected", func(t *testing.T) {
		_, _, _, err := resolveServeUIAuthConfig("127.0.0.1:8080", "admin", "SSB_UI_PASS", func(string) string {
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "empty or unset") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-loopback with configured auth is allowed", func(t *testing.T) {
		user, pass, configured, err := resolveServeUIAuthConfig("0.0.0.0:8080", "admin", "SSB_UI_PASS", func(key string) string {
			if key == "SSB_UI_PASS" {
				return "secret"
			}
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !configured {
			t.Fatalf("expected auth to be configured")
		}
		if user != "admin" || pass != "secret" {
			t.Fatalf("unexpected auth values user=%q pass=%q", user, pass)
		}
	})
}
