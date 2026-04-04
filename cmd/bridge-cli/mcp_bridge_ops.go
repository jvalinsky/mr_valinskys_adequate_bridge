package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerBridgeOpsTools registers all bridge-ops MCP tools on s.
func registerBridgeOpsTools(s *server.MCPServer, database *db.DB, seed string) {
	// ---- bridge_status ----
	s.AddTool(
		mcp.NewTool("bridge_status",
			mcp.WithDescription("Full operational snapshot: health, message counts by state, cursor positions, runtime status"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleBridgeStatus(ctx, database)
		},
	)

	// ---- bridge_accounts ----
	s.AddTool(
		mcp.NewTool("bridge_accounts",
			mcp.WithDescription("List all bridged accounts with per-account message statistics"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			accounts, err := database.ListBridgedAccountsWithStats(ctx)
			if err != nil {
				return mcpErrorf("list accounts: %v", err)
			}
			return mcpToolResult(accounts)
		},
	)

	// ---- bridge_account_add ----
	s.AddTool(
		mcp.NewTool("bridge_account_add",
			mcp.WithDescription("Register a DID for bridging, derives SSB feed ID from bot seed"),
			mcp.WithString("did", mcp.Required(), mcp.Description("ATProto DID to bridge (e.g. did:plc:...)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			did = strings.TrimSpace(did)
			if did == "" {
				return mcpError("did must not be empty")
			}
			manager := bots.NewManager([]byte(seed), nil, nil, nil)
			feedRef, err := manager.GetFeedID(did)
			if err != nil {
				return mcpErrorf("derive feed id: %v", err)
			}
			acc := db.BridgedAccount{
				ATDID:     did,
				SSBFeedID: feedRef.Ref(),
				Active:    true,
			}
			if err := database.AddBridgedAccount(ctx, acc); err != nil {
				return mcpErrorf("add account: %v", err)
			}
			return mcpToolResult(map[string]any{
				"added":      true,
				"did":        did,
				"ssb_feed":   feedRef.Ref(),
			})
		},
	)

	// ---- bridge_account_remove ----
	s.AddTool(
		mcp.NewTool("bridge_account_remove",
			mcp.WithDescription("Deactivate a bridged account"),
			mcp.WithString("did", mcp.Required(), mcp.Description("ATProto DID to deactivate")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			acc, err := database.GetBridgedAccount(ctx, strings.TrimSpace(did))
			if err != nil {
				return mcpErrorf("get account: %v", err)
			}
			if acc == nil {
				return mcpError("account not found")
			}
			acc.Active = false
			if err := database.AddBridgedAccount(ctx, *acc); err != nil {
				return mcpErrorf("deactivate account: %v", err)
			}
			return mcpToolResult(map[string]any{"deactivated": true, "did": did})
		},
	)

	// ---- bridge_account_detail ----
	s.AddTool(
		mcp.NewTool("bridge_account_detail",
			mcp.WithDescription("Detailed info for a single bridged account"),
			mcp.WithString("did", mcp.Required(), mcp.Description("ATProto DID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			acc, err := database.GetBridgedAccount(ctx, strings.TrimSpace(did))
			if err != nil {
				return mcpErrorf("get account: %v", err)
			}
			if acc == nil {
				return mcpError("account not found")
			}
			return mcpToolResult(acc)
		},
	)

	// ---- bridge_messages ----
	s.AddTool(
		mcp.NewTool("bridge_messages",
			mcp.WithDescription("Browse messages with filters and pagination"),
			mcp.WithString("state", mcp.Description("Filter by state: pending, published, failed, deferred, deleted")),
			mcp.WithString("did", mcp.Description("Filter by ATProto DID")),
			mcp.WithString("type", mcp.Description("Filter by message type (e.g. app.bsky.feed.post)")),
			mcp.WithString("search", mcp.Description("Search in AT URI, error, defer reason")),
			mcp.WithString("sort", mcp.Description("Sort order: newest (default), oldest")),
			mcp.WithNumber("limit", mcp.Description("Results per page (default 50, max 200)")),
			mcp.WithString("cursor", mcp.Description("Pagination cursor from previous result")),
			mcp.WithString("direction", mcp.Description("Pagination direction: next or prev")),
			mcp.WithBoolean("has_issue", mcp.Description("Filter to only messages with issues")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			limit := 50
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if limit > 200 {
				limit = 200
			}
			query := db.MessageListQuery{
				State:     getString(args, "state"),
				ATDID:     getString(args, "did"),
				Type:      getString(args, "type"),
				Search:    getString(args, "search"),
				Sort:      getString(args, "sort"),
				Limit:     limit,
				Cursor:    getString(args, "cursor"),
				Direction: getString(args, "direction"),
			}
			if b, ok := args["has_issue"].(bool); ok && b {
				query.HasIssue = true
			}
			query = db.NormalizeMessageListQuery(query)
			page, err := database.ListMessagesPage(ctx, query)
			if err != nil {
				return mcpErrorf("list messages: %v", err)
			}
			return mcpToolResult(map[string]any{
				"messages":    page.Messages,
				"count":       len(page.Messages),
				"has_next":    page.HasNext,
				"has_prev":    page.HasPrev,
				"next_cursor": page.NextCursor,
				"prev_cursor": page.PrevCursor,
			})
		},
	)

	// ---- bridge_message ----
	s.AddTool(
		mcp.NewTool("bridge_message",
			mcp.WithDescription("Full message detail by AT URI including raw JSON payloads"),
			mcp.WithString("at_uri", mcp.Required(), mcp.Description("AT URI of the message (e.g. at://did:plc:.../app.bsky.feed.post/...)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			atURI, err := req.RequireString("at_uri")
			if err != nil {
				return mcpError(err.Error())
			}
			msg, err := database.GetMessage(ctx, strings.TrimSpace(atURI))
			if err != nil {
				return mcpErrorf("get message: %v", err)
			}
			if msg == nil {
				return mcpError("message not found")
			}
			return mcpToolResult(msg)
		},
	)

	// ---- bridge_failures ----
	s.AddTool(
		mcp.NewTool("bridge_failures",
			mcp.WithDescription("Triage view: publish failures and deferred messages grouped by reason"),
			mcp.WithNumber("limit", mcp.Description("Max failures to return (default 100)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit := 100
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			failures, err := database.GetPublishFailures(ctx, limit)
			if err != nil {
				return mcpErrorf("get failures: %v", err)
			}
			reasons, err := database.ListTopDeferredReasons(ctx, 20)
			if err != nil {
				return mcpErrorf("get deferred reasons: %v", err)
			}
			return mcpToolResult(map[string]any{
				"failures":         failures,
				"failure_count":    len(failures),
				"deferred_reasons": reasons,
			})
		},
	)

	// ---- bridge_retry ----
	s.AddTool(
		mcp.NewTool("bridge_retry",
			mcp.WithDescription("Reset a specific message for retry by AT URI"),
			mcp.WithString("at_uri", mcp.Required(), mcp.Description("AT URI of the message to retry")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			atURI, err := req.RequireString("at_uri")
			if err != nil {
				return mcpError(err.Error())
			}
			if err := database.ResetMessageForRetry(ctx, strings.TrimSpace(atURI)); err != nil {
				return mcpErrorf("retry failed: %v", err)
			}
			return mcpToolResult(map[string]any{"retried": true, "at_uri": strings.TrimSpace(atURI)})
		},
	)

	// ---- bridge_deferred ----
	s.AddTool(
		mcp.NewTool("bridge_deferred",
			mcp.WithDescription("Deferred message analysis: reason breakdown with counts"),
			mcp.WithNumber("limit", mcp.Description("Max deferred reasons to return (default 20)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit := 20
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			reasons, err := database.ListTopDeferredReasons(ctx, limit)
			if err != nil {
				return mcpErrorf("get deferred reasons: %v", err)
			}
			deferredCount, err := database.CountDeferredMessages(ctx)
			if err != nil {
				return mcpErrorf("count deferred: %v", err)
			}
			return mcpToolResult(map[string]any{
				"total_deferred":    deferredCount,
				"top_reasons":       reasons,
				"reasons_returned":  len(reasons),
			})
		},
	)

	// ---- bridge_blobs ----
	s.AddTool(
		mcp.NewTool("bridge_blobs",
			mcp.WithDescription("Recent blob mappings and total count"),
			mcp.WithNumber("limit", mcp.Description("Max recent blobs to return (default 50)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit := 50
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			blobs, err := database.GetRecentBlobs(ctx, limit)
			if err != nil {
				return mcpErrorf("get blobs: %v", err)
			}
			total, err := database.CountBlobs(ctx)
			if err != nil {
				return mcpErrorf("count blobs: %v", err)
			}
			return mcpToolResult(map[string]any{
				"blobs":       blobs,
				"returned":    len(blobs),
				"total_count": total,
			})
		},
	)

	// ---- bridge_state ----
	s.AddTool(
		mcp.NewTool("bridge_state",
			mcp.WithDescription("All key-value bridge state (cursors, runtime status, heartbeat)"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			states, err := database.GetAllBridgeState(ctx)
			if err != nil {
				return mcpErrorf("get state: %v", err)
			}
			return mcpToolResult(states)
		},
	)
}

// handleBridgeStatus collects all dashboard metrics into a single snapshot.
func handleBridgeStatus(ctx context.Context, database *db.DB) (*mcp.CallToolResult, error) {
	health, err := database.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		return mcpErrorf("health check: %v", err)
	}

	accounts, _ := database.CountBridgedAccounts(ctx)
	messages, _ := database.CountMessages(ctx)
	published, _ := database.CountPublishedMessages(ctx)
	failures, _ := database.CountPublishFailures(ctx)
	deferred, _ := database.CountDeferredMessages(ctx)
	deleted, _ := database.CountDeletedMessages(ctx)
	blobs, _ := database.CountBlobs(ctx)

	replayCursor, _, _ := database.GetBridgeState(ctx, "atproto_event_cursor")
	legacyCursor, _, _ := database.GetBridgeState(ctx, "firehose_seq")
	bridgeStatus, _, _ := database.GetBridgeState(ctx, "bridge_runtime_status")
	lastHeartbeat, _, _ := database.GetBridgeState(ctx, "bridge_runtime_last_heartbeat_at")

	eventHead, eventHeadOK, _ := database.GetLatestATProtoEventCursor(ctx)

	var relaySourceCursor int64
	source, _ := database.GetATProtoSource(ctx, "default-relay")
	if source != nil {
		relaySourceCursor = source.LastSeq
	}

	result := map[string]any{
		"health": map[string]any{
			"healthy":        health.Healthy,
			"status":         health.Status,
			"last_heartbeat": health.LastHeartbeat,
		},
		"counts": map[string]any{
			"accounts":  accounts,
			"messages":  messages,
			"published": published,
			"failures":  failures,
			"deferred":  deferred,
			"deleted":   deleted,
			"blobs":     blobs,
		},
		"cursors": map[string]any{
			"bridge_replay":      replayCursor,
			"legacy_firehose":    legacyCursor,
			"relay_source":       relaySourceCursor,
		},
		"runtime": map[string]any{
			"status":         bridgeStatus,
			"last_heartbeat": lastHeartbeat,
		},
	}
	if eventHeadOK {
		result["cursors"].(map[string]any)["event_log_head"] = strconv.FormatInt(eventHead, 10)
	}
	return mcpToolResult(result)
}

// getString safely extracts a string from an arguments map.
func getString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// runMCPBridgeOps creates and runs the bridge-ops MCP server over stdio.
func runMCPBridgeOps(dbPath, seed string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	s := server.NewMCPServer(
		"bridge-ops",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	registerBridgeOpsTools(s, database, seed)

	return server.ServeStdio(s)
}
