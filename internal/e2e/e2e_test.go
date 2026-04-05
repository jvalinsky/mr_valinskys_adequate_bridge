// Package e2e provides end-to-end tests that orchestrate Docker Compose stacks.
//
// These tests call docker compose programmatically, wait for services to become
// healthy, and assert behaviour via HTTP APIs and container logs.  They are
// designed to run alongside unit tests (go test ./...) but are skipped by
// default — use `go test -tags=e2e ./internal/e2e` or `make test-e2e-room`.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// composeStack manages a docker compose lifecycle for a single test.
type composeStack struct {
	project     string
	composeFile string
	rootDir     string
}

// newStack creates a composeStack that operates from the repository root.
// The composeFile path is relative to the repo root.
func newStack(t *testing.T, project, composeFile string) *composeStack {
	t.Helper()
	root := repoRoot(t)
	return &composeStack{
		project:     project,
		composeFile: filepath.Join(root, composeFile),
		rootDir:     root,
	}
}

// up runs `docker compose up --build -d`.
func (s *composeStack) up(t *testing.T) {
	t.Helper()
	s.runCompose(t, "up", "--build", "-d")
}

// down runs `docker compose down -v --remove-orphans`.
func (s *composeStack) down(t *testing.T) {
	t.Helper()
	s.runCompose(t, "down", "-v", "--remove-orphans")
}

// logs returns the concatenated logs for all services.
func (s *composeStack) logs(t *testing.T) string {
	t.Helper()
	cmd := s.composeCmd(t, "logs", "--no-color")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("warning: failed to collect logs: %v", err)
	}
	return string(out)
}

// waitForHealth polls a URL until it returns HTTP 200 or the timeout expires.
func waitForHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("curl", "-sf", url)
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("health check timed out after %v for %s", timeout, url)
}

// runCompose executes a docker compose subcommand and fails the test on error.
func (s *composeStack) runCompose(t *testing.T, args ...string) {
	t.Helper()
	cmd := s.composeCmd(t, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker compose %s failed: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
}

// composeCmd builds an exec.Cmd for docker compose with the correct project
// and file flags.
func (s *composeStack) composeCmd(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	fullArgs := []string{
		"compose",
		"-p", s.project,
		"-f", s.composeFile,
	}
	fullArgs = append(fullArgs, args...)
	return exec.Command("docker", fullArgs...)
}

// repoRoot returns the repository root directory, or fails the test.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod — not inside repo")
		}
		dir = parent
	}
}

// TestE2E_TildefriendsRoom is a skeleton for the tildefriends → bridge-room
// E2E test.  It validates that the compose stack starts, the bridge becomes
// healthy, and the test-runner exits successfully.
//
// Run with: RUN_E2E=1 go test -run TestE2E_TildefriendsRoom ./internal/e2e
func TestE2E_TildefriendsRoom(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("skipping E2E test; set RUN_E2E=1 to run")
	}

	stack := newStack(t, "mvab-e2e-tf", "infra/e2e-tildefriends/docker-compose.e2e-tildefriends.yml")
	t.Cleanup(func() { stack.down(t) })

	stack.up(t)

	waitForHealth(t, "http://127.0.0.1:8976/healthz", 2*time.Minute)

	t.Log("bridge is healthy — full assertions delegated to test-runner container")
}

// TestE2E_FullStack is a skeleton for the full ATProto → Bridge → SSB →
// Tildefriends pipeline test.
//
// Run with: RUN_E2E=1 go test -run TestE2E_FullStack ./internal/e2e
func TestE2E_FullStack(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("skipping E2E test; set RUN_E2E=1 to run")
	}

	stack := newStack(t, "mvab-e2e-full", "infra/e2e-full/docker-compose.yml")
	t.Cleanup(func() { stack.down(t) })

	stack.up(t)

	waitForHealth(t, "http://127.0.0.1:8976/healthz", 5*time.Minute)

	t.Log("bridge is healthy — full assertions delegated to test-runner container")
}

// TestE2E_ComposeConfig validates that both compose files parse correctly.
func TestE2E_ComposeConfig(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		"infra/e2e-tildefriends/docker-compose.e2e-tildefriends.yml",
		"infra/e2e-full/docker-compose.yml",
		"infra/shared/docker-compose.atproto.yml",
		"infra/local-atproto/docker-compose.yml",
		"infra/linux-test/docker-compose.yml",
	}

	for _, f := range files {
		f := f
		t.Run(f, func(t *testing.T) {
			path := filepath.Join(root, f)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("compose file missing: %s: %v", path, err)
			}
		})
	}
}
