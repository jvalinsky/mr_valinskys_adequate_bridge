package livee2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseShellEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "live.env")
	content := strings.Join([]string{
		"# comment",
		"  LIVE_ATPROTO_SOURCE_IDENTIFIER = did:plc:source  ",
		"export LIVE_ATPROTO_SOURCE_APP_PASSWORD='source-app-pass'",
		"LIVE_ATPROTO_FOLLOW_TARGET_DID=\"did:plc:target\"",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	values, err := parseShellEnvFile(path)
	if err != nil {
		t.Fatalf("parse env file: %v", err)
	}

	if got, want := values["LIVE_ATPROTO_SOURCE_IDENTIFIER"], "did:plc:source"; got != want {
		t.Fatalf("source identifier mismatch: got %q want %q", got, want)
	}
	if got, want := values["LIVE_ATPROTO_SOURCE_APP_PASSWORD"], "source-app-pass"; got != want {
		t.Fatalf("source app password mismatch: got %q want %q", got, want)
	}
	if got, want := values["LIVE_ATPROTO_FOLLOW_TARGET_DID"], "did:plc:target"; got != want {
		t.Fatalf("target DID mismatch: got %q want %q", got, want)
	}
}

func TestResolveLiveAuthConfigFromFileAndEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "live.env")
	content := strings.Join([]string{
		"LIVE_ATPROTO_SOURCE_IDENTIFIER=alice.test",
		"LIVE_ATPROTO_SOURCE_APP_PASSWORD=from-file",
		"LIVE_ATPROTO_FOLLOW_TARGET_DID=did:plc:target",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	env := map[string]string{
		"LIVE_ATPROTO_CONFIG_FILE":         path,
		"LIVE_ATPROTO_SOURCE_APP_PASSWORD": "from-env",
	}
	cfg, err := resolveLiveAuthConfig(mapEnvLookup(env))
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}

	if got, want := cfg.SourceIdentifier, "alice.test"; got != want {
		t.Fatalf("source identifier mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.SourceAppPassword, "from-env"; got != want {
		t.Fatalf("source app password mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.FollowTargetDID, "did:plc:target"; got != want {
		t.Fatalf("follow target DID mismatch: got %q want %q", got, want)
	}
}

func TestResolveLiveAuthConfigAllowsTargetCredentialsWithoutTargetDID(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"LIVE_ATPROTO_SOURCE_IDENTIFIER":   "did:plc:source",
		"LIVE_ATPROTO_SOURCE_APP_PASSWORD": "source-app-pass",
		"LIVE_ATPROTO_TARGET_IDENTIFIER":   "did:plc:target",
		"LIVE_ATPROTO_TARGET_APP_PASSWORD": "target-app-pass",
	}
	cfg, err := resolveLiveAuthConfig(mapEnvLookup(env))
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if got, want := cfg.TargetIdentifier, "did:plc:target"; got != want {
		t.Fatalf("target identifier mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.TargetAppPassword, "target-app-pass"; got != want {
		t.Fatalf("target app password mismatch: got %q want %q", got, want)
	}
	if cfg.FollowTargetDID != "" {
		t.Fatalf("expected empty follow target DID when credentials are used for resolution, got %q", cfg.FollowTargetDID)
	}
}

func TestResolveLiveAuthConfigRequiresTargetDidOrTargetCredentials(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"LIVE_ATPROTO_SOURCE_IDENTIFIER":   "did:plc:source",
		"LIVE_ATPROTO_SOURCE_APP_PASSWORD": "source-app-pass",
	}
	_, err := resolveLiveAuthConfig(mapEnvLookup(env))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "LIVE_ATPROTO_FOLLOW_TARGET_DID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLiveE2EEnabledFromConfigFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "live.env")
	content := strings.Join([]string{
		"LIVE_E2E_ENABLED=1",
		"LIVE_ATPROTO_SOURCE_IDENTIFIER=did:plc:source",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	env := map[string]string{
		"LIVE_ATPROTO_CONFIG_FILE": path,
	}
	if !liveE2EEnabled(mapEnvLookup(env)) {
		t.Fatal("expected live E2E to be enabled from config file")
	}
}

func mapEnvLookup(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
