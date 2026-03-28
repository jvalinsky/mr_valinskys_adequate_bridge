package main

import (
	"context"
	"flag"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/urfave/cli/v2"
)

func TestResolveLiveXRPCHost(t *testing.T) {
	t.Run("defaults to appview", func(t *testing.T) {
		got, err := resolveLiveXRPCHost("")
		if err != nil {
			t.Fatalf("resolveLiveXRPCHost: %v", err)
		}
		if got != defaultLiveReadXRPCHost {
			t.Fatalf("expected default live read host %q, got %q", defaultLiveReadXRPCHost, got)
		}
	})

	t.Run("normalizes explicit override", func(t *testing.T) {
		got, err := resolveLiveXRPCHost(" https://example.com/path/ ")
		if err != nil {
			t.Fatalf("resolveLiveXRPCHost explicit: %v", err)
		}
		if got != "https://example.com/path" {
			t.Fatalf("expected normalized explicit host, got %q", got)
		}
	})
}

func TestResolveSharedRepoPath(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		ctx := testCLIContext(t)
		got, err := resolveSharedRepoPath(ctx)
		if err != nil {
			t.Fatalf("resolveSharedRepoPath: %v", err)
		}
		if got != ".ssb-bridge" {
			t.Fatalf("expected default repo path, got %q", got)
		}
	})

	t.Run("legacy alias only", func(t *testing.T) {
		ctx := testCLIContext(t, "--ssb-repo-path", ".ssb-legacy")
		got, err := resolveSharedRepoPath(ctx)
		if err != nil {
			t.Fatalf("resolveSharedRepoPath: %v", err)
		}
		if got != ".ssb-legacy" {
			t.Fatalf("expected legacy repo path, got %q", got)
		}
	})

	t.Run("repo path overrides matching legacy", func(t *testing.T) {
		ctx := testCLIContext(t, "--repo-path", ".ssb-shared", "--room-repo-path", ".ssb-shared")
		got, err := resolveSharedRepoPath(ctx)
		if err != nil {
			t.Fatalf("resolveSharedRepoPath: %v", err)
		}
		if got != ".ssb-shared" {
			t.Fatalf("expected shared repo path, got %q", got)
		}
	})

	t.Run("rejects conflicting legacy values", func(t *testing.T) {
		ctx := testCLIContext(t, "--ssb-repo-path", ".ssb-a", "--room-repo-path", ".ssb-b")
		if _, err := resolveSharedRepoPath(ctx); err == nil {
			t.Fatalf("expected conflict error")
		}
	})

	t.Run("rejects conflict between repo path and legacy alias", func(t *testing.T) {
		ctx := testCLIContext(t, "--repo-path", ".ssb-shared", "--ssb-repo-path", ".ssb-other")
		if _, err := resolveSharedRepoPath(ctx); err == nil {
			t.Fatalf("expected conflict error")
		}
	})
}

func testCLIContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("repo-path", "", "")
	set.String("ssb-repo-path", "", "")
	set.String("room-repo-path", "", "")
	if err := set.Parse(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	return cli.NewContext(nil, set, nil)
}

func TestRunRuntimeHeartbeatSchedulerUpdatesBridgeState(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runRuntimeHeartbeatScheduler(ctx, database, log.New(io.Discard, "", 0), 5*time.Millisecond)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		status, ok, err := database.GetBridgeState(context.Background(), bridgeRuntimeStatusKey)
		if err != nil {
			t.Fatalf("read bridge status: %v", err)
		}
		heartbeat, hbOK, err := database.GetBridgeState(context.Background(), bridgeRuntimeLastHeartbeatKey)
		if err != nil {
			t.Fatalf("read bridge heartbeat: %v", err)
		}
		if ok && hbOK && status == "live" && heartbeat != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("runtime heartbeat scheduler did not write expected bridge state keys")
}
