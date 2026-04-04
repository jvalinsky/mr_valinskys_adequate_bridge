package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ssbMCPDeps bundles dependencies for SSB MCP tools.
type ssbMCPDeps struct {
	ssbRT   *ssbruntime.Runtime
	roomOps handlers.RoomOpsProvider
}

// registerSSBTools registers all SSB MCP tools on s.
func registerSSBTools(s *server.MCPServer, deps ssbMCPDeps) {
	// ---- ssb_whoami ----
	s.AddTool(
		mcp.NewTool("ssb_whoami",
			mcp.WithDescription("Bridge's own SSB feed reference"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil || deps.ssbRT.Node() == nil {
				return mcpError("ssb runtime not available")
			}
			whoami, err := deps.ssbRT.Node().Whoami()
			if err != nil {
				return mcpErrorf("whoami: %v", err)
			}
			return mcpToolResult(map[string]any{"feed": whoami})
		},
	)

	// ---- ssb_peers ----
	s.AddTool(
		mcp.NewTool("ssb_peers",
			mcp.WithDescription("Connected SSB peers with addr, feed, bytes transferred, latency"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			peers := deps.ssbRT.GetPeers()
			return mcpToolResult(map[string]any{
				"peers": peers,
				"count": len(peers),
			})
		},
	)

	// ---- ssb_connect ----
	s.AddTool(
		mcp.NewTool("ssb_connect",
			mcp.WithDescription("Connect to an SSB peer by address and public key"),
			mcp.WithString("addr", mcp.Required(), mcp.Description("Network address (host:port)")),
			mcp.WithString("pub_key", mcp.Required(), mcp.Description("Ed25519 public key (base64, 32 bytes)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			addr, err := req.RequireString("addr")
			if err != nil {
				return mcpError(err.Error())
			}
			pubKeyStr, err := req.RequireString("pub_key")
			if err != nil {
				return mcpError(err.Error())
			}
			pubKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKeyStr))
			if err != nil {
				return mcpErrorf("decode public key: %v", err)
			}
			if err := deps.ssbRT.ConnectPeer(ctx, strings.TrimSpace(addr), pubKey); err != nil {
				return mcpErrorf("connect peer: %v", err)
			}
			return mcpToolResult(map[string]any{"connected": true, "addr": strings.TrimSpace(addr)})
		},
	)

	// ---- ssb_ebt_state ----
	s.AddTool(
		mcp.NewTool("ssb_ebt_state",
			mcp.WithDescription("Full EBT replication frontier — shows which feeds each peer knows about"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			state := deps.ssbRT.GetEBTState()
			return mcpToolResult(state)
		},
	)

	// ---- ssb_feed_read ----
	s.AddTool(
		mcp.NewTool("ssb_feed_read",
			mcp.WithDescription("Read messages from a feed by ref with sequence range"),
			mcp.WithString("feed", mcp.Required(), mcp.Description("SSB feed reference (@...ed25519)")),
			mcp.WithNumber("start_seq", mcp.Description("Start sequence (default 1)")),
			mcp.WithNumber("limit", mcp.Description("Max messages to return (default 10, max 100)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil || deps.ssbRT.Node() == nil {
				return mcpError("ssb runtime not available")
			}
			feed, err := req.RequireString("feed")
			if err != nil {
				return mcpError(err.Error())
			}
			startSeq := int64(1)
			if s, ok := req.GetArguments()["start_seq"].(float64); ok && s > 0 {
				startSeq = int64(s)
			}
			limit := 10
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if limit > 100 {
				limit = 100
			}

			messages := make([]map[string]any, 0, limit)
			for seq := startSeq; seq < startSeq+int64(limit); seq++ {
				data, err := deps.ssbRT.Node().GetMessage(strings.TrimSpace(feed), seq)
				if err != nil {
					break // end of feed
				}
				messages = append(messages, map[string]any{
					"sequence": seq,
					"value":    string(data),
				})
			}
			return mcpToolResult(map[string]any{
				"feed":     feed,
				"messages": messages,
				"count":    len(messages),
			})
		},
	)

	// ---- ssb_feed_latest ----
	s.AddTool(
		mcp.NewTool("ssb_feed_latest",
			mcp.WithDescription("Latest sequence number and message for a feed"),
			mcp.WithString("feed", mcp.Required(), mcp.Description("SSB feed reference (@...ed25519)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil || deps.ssbRT.Node() == nil {
				return mcpError("ssb runtime not available")
			}
			feed, err := req.RequireString("feed")
			if err != nil {
				return mcpError(err.Error())
			}
			seq, err := deps.ssbRT.Node().GetFeedSeq(strings.TrimSpace(feed))
			if err != nil {
				return mcpErrorf("get feed seq: %v", err)
			}
			result := map[string]any{
				"feed":     feed,
				"sequence": seq,
			}
			if seq >= 0 {
				data, err := deps.ssbRT.Node().GetMessage(strings.TrimSpace(feed), seq)
				if err == nil {
					result["latest_message"] = string(data)
				}
			}
			return mcpToolResult(result)
		},
	)

	// ---- ssb_publish ----
	s.AddTool(
		mcp.NewTool("ssb_publish",
			mcp.WithDescription("Publish JSON content as a bridged DID's SSB feed"),
			mcp.WithString("did", mcp.Required(), mcp.Description("ATProto DID whose SSB feed to publish to")),
			mcp.WithString("content", mcp.Required(), mcp.Description("JSON content to publish (as string)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			contentStr, err := req.RequireString("content")
			if err != nil {
				return mcpError(err.Error())
			}
			var content map[string]interface{}
			if jsonErr := jsonUnmarshalString(contentStr, &content); jsonErr != nil {
				return mcpErrorf("invalid JSON content: %v", jsonErr)
			}
			msgRef, err := deps.ssbRT.Publish(ctx, strings.TrimSpace(did), content)
			if err != nil {
				return mcpErrorf("publish: %v", err)
			}
			return mcpToolResult(map[string]any{
				"published":    true,
				"message_ref":  msgRef,
				"did":          strings.TrimSpace(did),
			})
		},
	)

	// ---- ssb_blob_has ----
	s.AddTool(
		mcp.NewTool("ssb_blob_has",
			mcp.WithDescription("Check if a blob exists in the SSB blob store"),
			mcp.WithString("ref", mcp.Required(), mcp.Description("SSB blob reference (&...sha256)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			blobRef, err := req.RequireString("ref")
			if err != nil {
				return mcpError(err.Error())
			}
			blobStore := deps.ssbRT.BlobStore()
			if blobStore == nil {
				return mcpError("blob store not available")
			}
			hash, err := decodeBlobRef(strings.TrimSpace(blobRef))
			if err != nil {
				return mcpErrorf("decode blob ref: %v", err)
			}
			rc, err := blobStore.Get(hash)
			exists := err == nil
			if rc != nil {
				rc.Close()
			}
			return mcpToolResult(map[string]any{"ref": blobRef, "exists": exists})
		},
	)

	// ---- ssb_blob_get ----
	s.AddTool(
		mcp.NewTool("ssb_blob_get",
			mcp.WithDescription("Get blob content from SSB blob store (returns base64 for binary)"),
			mcp.WithString("ref", mcp.Required(), mcp.Description("SSB blob reference (&...sha256)")),
			mcp.WithNumber("max_size", mcp.Description("Max bytes to return (default 1MB)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.ssbRT == nil {
				return mcpError("ssb runtime not available")
			}
			blobRef, err := req.RequireString("ref")
			if err != nil {
				return mcpError(err.Error())
			}
			maxSize := int64(1024 * 1024)
			if m, ok := req.GetArguments()["max_size"].(float64); ok && m > 0 {
				maxSize = int64(m)
			}
			blobStore := deps.ssbRT.BlobStore()
			if blobStore == nil {
				return mcpError("blob store not available")
			}
			hash, err := decodeBlobRef(strings.TrimSpace(blobRef))
			if err != nil {
				return mcpErrorf("decode blob ref: %v", err)
			}
			rc, err := blobStore.Get(hash)
			if err != nil {
				return mcpErrorf("get blob: %v", err)
			}
			defer rc.Close()
			data, err := io.ReadAll(io.LimitReader(rc, maxSize))
			if err != nil {
				return mcpErrorf("read blob: %v", err)
			}
			return mcpToolResult(map[string]any{
				"ref":     blobRef,
				"size":    len(data),
				"content": base64.StdEncoding.EncodeToString(data),
			})
		},
	)

	// ---- Room tools (only if roomOps available) ----
	if deps.roomOps != nil {
		registerSSBRoomTools(s, deps.roomOps)
	}
}

// registerSSBRoomTools registers room management tools.
func registerSSBRoomTools(s *server.MCPServer, roomOps handlers.RoomOpsProvider) {
	// ---- ssb_room_status ----
	s.AddTool(
		mcp.NewTool("ssb_room_status",
			mcp.WithDescription("Room overview: mode, member/alias/attendant/tunnel counts, health status"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			overview, err := roomOps.Overview(ctx)
			if err != nil {
				return mcpErrorf("room overview: %v", err)
			}
			return mcpToolResult(overview)
		},
	)

	// ---- ssb_room_members ----
	s.AddTool(
		mcp.NewTool("ssb_room_members",
			mcp.WithDescription("List room members with roles"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			members, err := roomOps.MembersList(ctx)
			if err != nil {
				return mcpErrorf("list members: %v", err)
			}
			return mcpToolResult(map[string]any{"members": members, "count": len(members)})
		},
	)

	// ---- ssb_room_member_add ----
	s.AddTool(
		mcp.NewTool("ssb_room_member_add",
			mcp.WithDescription("Add a member to the room with a role"),
			mcp.WithString("feed", mcp.Required(), mcp.Description("SSB feed reference (@...ed25519)")),
			mcp.WithString("role", mcp.Required(), mcp.Description("Role: member, moderator, or admin")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			feedStr, err := req.RequireString("feed")
			if err != nil {
				return mcpError(err.Error())
			}
			roleStr, err := req.RequireString("role")
			if err != nil {
				return mcpError(err.Error())
			}
			feedRef, err := refs.ParseFeedRef(strings.TrimSpace(feedStr))
			if err != nil {
				return mcpErrorf("invalid feed ref: %v", err)
			}
			role := parseRole(roleStr)
			if role == roomdb.RoleUnknown {
				return mcpError("invalid role, must be: member, moderator, or admin")
			}
			// RoomOpsProvider doesn't expose Add directly, use the room SQLite
			// provider's underlying DB. For now, return an error suggesting using
			// the admin UI for member addition.
			// However, we can work around this by using MemberRemove/MemberRoleSet
			// pattern or by accessing the room DB directly if we had it.
			// For v1, expose this through a member add that first checks existence.
			return mcpErrorf("member add not yet supported via MCP — use admin UI or ssb_room_member_role to change existing member roles (feed=%s, role=%s)", feedRef.String(), role.String())
		},
	)

	// ---- ssb_room_member_remove ----
	s.AddTool(
		mcp.NewTool("ssb_room_member_remove",
			mcp.WithDescription("Remove a member from the room by member ID"),
			mcp.WithNumber("member_id", mcp.Required(), mcp.Description("Member ID (from ssb_room_members)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireFloat("member_id")
			if err != nil {
				return mcpError(err.Error())
			}
			if err := roomOps.MemberRemove(ctx, int64(id)); err != nil {
				return mcpErrorf("remove member: %v", err)
			}
			return mcpToolResult(map[string]any{"removed": true, "member_id": int64(id)})
		},
	)

	// ---- ssb_room_member_role ----
	s.AddTool(
		mcp.NewTool("ssb_room_member_role",
			mcp.WithDescription("Change a member's role"),
			mcp.WithNumber("member_id", mcp.Required(), mcp.Description("Member ID")),
			mcp.WithString("role", mcp.Required(), mcp.Description("New role: member, moderator, or admin")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireFloat("member_id")
			if err != nil {
				return mcpError(err.Error())
			}
			roleStr, err := req.RequireString("role")
			if err != nil {
				return mcpError(err.Error())
			}
			role := parseRole(roleStr)
			if role == roomdb.RoleUnknown {
				return mcpError("invalid role, must be: member, moderator, or admin")
			}
			if err := roomOps.MemberRoleSet(ctx, int64(id), role); err != nil {
				return mcpErrorf("set role: %v", err)
			}
			return mcpToolResult(map[string]any{"updated": true, "member_id": int64(id), "role": role.String()})
		},
	)

	// ---- ssb_room_invites ----
	s.AddTool(
		mcp.NewTool("ssb_room_invites",
			mcp.WithDescription("List room invites with status"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			invites, err := roomOps.InvitesList(ctx)
			if err != nil {
				return mcpErrorf("list invites: %v", err)
			}
			return mcpToolResult(map[string]any{"invites": invites, "count": len(invites)})
		},
	)

	// ---- ssb_room_invite_create ----
	s.AddTool(
		mcp.NewTool("ssb_room_invite_create",
			mcp.WithDescription("Create a new room invite token"),
			mcp.WithNumber("created_by", mcp.Description("Member ID creating the invite (default 1)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			createdBy := int64(1)
			if c, ok := req.GetArguments()["created_by"].(float64); ok && c > 0 {
				createdBy = int64(c)
			}
			token, err := roomOps.InviteCreate(ctx, createdBy)
			if err != nil {
				return mcpErrorf("create invite: %v", err)
			}
			return mcpToolResult(map[string]any{
				"created":    true,
				"token":      token,
				"join_url":   roomOps.JoinURL(token),
				"created_by": createdBy,
			})
		},
	)

	// ---- ssb_room_invite_revoke ----
	s.AddTool(
		mcp.NewTool("ssb_room_invite_revoke",
			mcp.WithDescription("Revoke a room invite"),
			mcp.WithNumber("invite_id", mcp.Required(), mcp.Description("Invite ID to revoke")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireFloat("invite_id")
			if err != nil {
				return mcpError(err.Error())
			}
			if err := roomOps.InviteRevoke(ctx, int64(id)); err != nil {
				return mcpErrorf("revoke invite: %v", err)
			}
			return mcpToolResult(map[string]any{"revoked": true, "invite_id": int64(id)})
		},
	)

	// ---- ssb_room_aliases ----
	s.AddTool(
		mcp.NewTool("ssb_room_aliases",
			mcp.WithDescription("List room aliases"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			aliases, err := roomOps.AliasesList(ctx)
			if err != nil {
				return mcpErrorf("list aliases: %v", err)
			}
			return mcpToolResult(map[string]any{"aliases": aliases, "count": len(aliases)})
		},
	)

	// ---- ssb_room_denied ----
	s.AddTool(
		mcp.NewTool("ssb_room_denied",
			mcp.WithDescription("List denied keys"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			denied, err := roomOps.DeniedList(ctx)
			if err != nil {
				return mcpErrorf("list denied: %v", err)
			}
			return mcpToolResult(map[string]any{"denied_keys": denied, "count": len(denied)})
		},
	)

	// ---- ssb_room_deny ----
	s.AddTool(
		mcp.NewTool("ssb_room_deny",
			mcp.WithDescription("Add a feed to the denied list"),
			mcp.WithString("feed", mcp.Required(), mcp.Description("SSB feed reference to deny")),
			mcp.WithString("comment", mcp.Description("Reason for denying")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			feedStr, err := req.RequireString("feed")
			if err != nil {
				return mcpError(err.Error())
			}
			feedRef, err := refs.ParseFeedRef(strings.TrimSpace(feedStr))
			if err != nil {
				return mcpErrorf("invalid feed ref: %v", err)
			}
			comment := getString(req.GetArguments(), "comment")
			if err := roomOps.DeniedAdd(ctx, *feedRef, comment); err != nil {
				return mcpErrorf("deny feed: %v", err)
			}
			return mcpToolResult(map[string]any{"denied": true, "feed": feedRef.String()})
		},
	)
}

// parseRole converts a role string to roomdb.Role.
func parseRole(s string) roomdb.Role {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "member":
		return roomdb.RoleMember
	case "moderator":
		return roomdb.RoleModerator
	case "admin":
		return roomdb.RoleAdmin
	default:
		return roomdb.RoleUnknown
	}
}

// decodeBlobRef decodes an SSB blob reference like "&XXXX.sha256" to raw hash bytes.
func decodeBlobRef(ref string) ([]byte, error) {
	ref = strings.TrimPrefix(ref, "&")
	ref = strings.TrimSuffix(ref, ".sha256")
	return base64.StdEncoding.DecodeString(ref)
}

// jsonUnmarshalString is a convenience wrapper around json.Unmarshal for string input.
func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// runMCPSSB creates and runs the SSB MCP server over stdio.
func runMCPSSB(dbPath, repoPath, seed string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	// Open SSB runtime without a network listener (no port conflict with running bridge).
	ssbRT, err := ssbruntime.Open(context.Background(), ssbruntime.Config{
		RepoPath:   repoPath,
		ListenAddr: "", // no network listener
		MasterSeed: []byte(seed),
		GossipDB:   database,
	}, nil)
	if err != nil {
		return fmt.Errorf("init ssb runtime: %w", err)
	}
	defer ssbRT.Close()

	deps := ssbMCPDeps{
		ssbRT: ssbRT,
	}

	// Try to open room ops provider.
	roomRepoPath := repoPath + "/room"
	roomProvider, err := handlers.OpenSQLiteRoomOpsProvider(roomRepoPath, "", roomdb.RoleAdmin, nil)
	if err == nil {
		deps.roomOps = roomProvider
		defer roomProvider.Close()
	}

	s := server.NewMCPServer(
		"ssb",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	registerSSBTools(s, deps)

	return server.ServeStdio(s)
}
