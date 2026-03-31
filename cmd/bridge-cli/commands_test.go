package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

func TestAccountCommands(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.sqlite")
	ctx := context.Background()
	botSeed := "test-seed"

	// Test Account Add
	did := "did:plc:alice"
	if err := runAccountAdd(ctx, dbPath, botSeed, did); err != nil {
		t.Fatalf("runAccountAdd failed: %v", err)
	}

	// Test Account List
	if err := runAccountList(ctx, dbPath); err != nil {
		t.Fatalf("runAccountList failed: %v", err)
	}

	// Test Account Remove
	if err := runAccountRemove(ctx, dbPath, did); err != nil {
		t.Fatalf("runAccountRemove failed: %v", err)
	}

	// Verify deactivation
	database, _ := db.Open(dbPath)
	defer database.Close()
	acc, _ := database.GetBridgedAccount(ctx, did)
	if acc.Active {
		t.Error("expected account to be inactive")
	}
}

func TestStatsCommand(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test-stats.sqlite")
	ctx := context.Background()

	database, _ := db.Open(dbPath)
	database.AddBridgedAccount(ctx, db.BridgedAccount{ATDID: "did:plc:alice", Active: true})
	database.AddMessage(ctx, db.Message{
		ATURI:        "at://alice/post/1",
		ATCID:        "c1",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
	})
	database.Close()

	if err := runStats(ctx, dbPath); err != nil {
		t.Fatalf("runStats failed: %v", err)
	}
}
