package main

import (
	"flag"
	"testing"

	"github.com/urfave/cli/v2"
)

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
