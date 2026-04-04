package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/atindex"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// atprotoMCPDeps bundles the dependencies for ATProto MCP tools.
type atprotoMCPDeps struct {
	database   *db.DB
	httpClient *http.Client
	appviewURL string
	plcURL     string
}

// registerATProtoTools registers all ATProto MCP tools on s.
func registerATProtoTools(s *server.MCPServer, deps atprotoMCPDeps) {
	// ---- atproto_resolve_handle ----
	s.AddTool(
		mcp.NewTool("atproto_resolve_handle",
			mcp.WithDescription("Resolve an ATProto handle to a DID"),
			mcp.WithString("handle", mcp.Required(), mcp.Description("Handle to resolve (e.g. alice.bsky.social)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handle, err := req.RequireString("handle")
			if err != nil {
				return mcpError(err.Error())
			}
			url := fmt.Sprintf("%s/xrpc/com.atproto.identity.resolveHandle?handle=%s",
				deps.appviewURL, strings.TrimSpace(handle))
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("resolve handle: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_resolve_did ----
	s.AddTool(
		mcp.NewTool("atproto_resolve_did",
			mcp.WithDescription("Resolve a DID to its DID document via PLC directory"),
			mcp.WithString("did", mcp.Required(), mcp.Description("DID to resolve (e.g. did:plc:...)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			url := fmt.Sprintf("%s/%s", deps.plcURL, strings.TrimSpace(did))
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("resolve did: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_get_profile ----
	s.AddTool(
		mcp.NewTool("atproto_get_profile",
			mcp.WithDescription("Get a Bluesky profile for a DID or handle"),
			mcp.WithString("actor", mcp.Required(), mcp.Description("DID or handle")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			actor, err := req.RequireString("actor")
			if err != nil {
				return mcpError(err.Error())
			}
			url := fmt.Sprintf("%s/xrpc/app.bsky.actor.getProfile?actor=%s",
				deps.appviewURL, strings.TrimSpace(actor))
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("get profile: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_get_record ----
	s.AddTool(
		mcp.NewTool("atproto_get_record",
			mcp.WithDescription("Get a single ATProto record by AT URI"),
			mcp.WithString("at_uri", mcp.Required(), mcp.Description("AT URI (at://did/collection/rkey)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			atURI, err := req.RequireString("at_uri")
			if err != nil {
				return mcpError(err.Error())
			}
			parts := parseATURI(strings.TrimSpace(atURI))
			if parts == nil {
				return mcpError("invalid AT URI format, expected at://did/collection/rkey")
			}
			url := fmt.Sprintf("%s/xrpc/com.atproto.repo.getRecord?repo=%s&collection=%s&rkey=%s",
				deps.appviewURL, parts.repo, parts.collection, parts.rkey)
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("get record: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_list_records ----
	s.AddTool(
		mcp.NewTool("atproto_list_records",
			mcp.WithDescription("List records in a collection for a DID"),
			mcp.WithString("did", mcp.Required(), mcp.Description("Repository DID")),
			mcp.WithString("collection", mcp.Required(), mcp.Description("Collection NSID (e.g. app.bsky.feed.post)")),
			mcp.WithNumber("limit", mcp.Description("Max records (default 50, max 100)")),
			mcp.WithString("cursor", mcp.Description("Pagination cursor")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			collection, err := req.RequireString("collection")
			if err != nil {
				return mcpError(err.Error())
			}
			limit := 50
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if limit > 100 {
				limit = 100
			}
			url := fmt.Sprintf("%s/xrpc/com.atproto.repo.listRecords?repo=%s&collection=%s&limit=%d",
				deps.appviewURL, strings.TrimSpace(did), strings.TrimSpace(collection), limit)
			if cursor := getString(req.GetArguments(), "cursor"); cursor != "" {
				url += "&cursor=" + cursor
			}
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("list records: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_describe_repo ----
	s.AddTool(
		mcp.NewTool("atproto_describe_repo",
			mcp.WithDescription("Get repository metadata for a DID"),
			mcp.WithString("did", mcp.Required(), mcp.Description("Repository DID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			did, err := req.RequireString("did")
			if err != nil {
				return mcpError(err.Error())
			}
			url := fmt.Sprintf("%s/xrpc/com.atproto.repo.describeRepo?repo=%s",
				deps.appviewURL, strings.TrimSpace(did))
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("describe repo: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_get_post_thread ----
	s.AddTool(
		mcp.NewTool("atproto_get_post_thread",
			mcp.WithDescription("Get a post with its reply thread"),
			mcp.WithString("at_uri", mcp.Required(), mcp.Description("AT URI of the post")),
			mcp.WithNumber("depth", mcp.Description("Reply depth (default 6)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			atURI, err := req.RequireString("at_uri")
			if err != nil {
				return mcpError(err.Error())
			}
			depth := 6
			if d, ok := req.GetArguments()["depth"].(float64); ok && d > 0 {
				depth = int(d)
			}
			url := fmt.Sprintf("%s/xrpc/app.bsky.feed.getPostThread?uri=%s&depth=%d",
				deps.appviewURL, strings.TrimSpace(atURI), depth)
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("get post thread: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- atproto_search_posts ----
	s.AddTool(
		mcp.NewTool("atproto_search_posts",
			mcp.WithDescription("Search posts on Bluesky"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 25, max 100)")),
			mcp.WithString("cursor", mcp.Description("Pagination cursor")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return mcpError(err.Error())
			}
			limit := 25
			if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if limit > 100 {
				limit = 100
			}
			url := fmt.Sprintf("%s/xrpc/app.bsky.feed.searchPosts?q=%s&limit=%d",
				deps.appviewURL, strings.TrimSpace(query), limit)
			if cursor := getString(req.GetArguments(), "cursor"); cursor != "" {
				url += "&cursor=" + cursor
			}
			body, err := atprotoXRPCGet(ctx, deps.httpClient, url)
			if err != nil {
				return mcpErrorf("search posts: %v", err)
			}
			return mcp.NewToolResultText(string(body)), nil
		},
	)

	// ---- Tools that need bridge DB ----

	if deps.database != nil {
		// ---- atproto_tracking_status ----
		s.AddTool(
			mcp.NewTool("atproto_tracking_status",
				mcp.WithDescription("Tracked repos list with sync states and relay source cursor"),
				mcp.WithString("state", mcp.Description("Filter by sync state")),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				state := getString(req.GetArguments(), "state")
				repos, err := deps.database.ListTrackedATProtoRepos(ctx, state)
				if err != nil {
					return mcpErrorf("list tracked repos: %v", err)
				}
				source, _ := deps.database.GetATProtoSource(ctx, "default-relay")
				return mcpToolResult(map[string]any{
					"repos":  repos,
					"count":  len(repos),
					"source": source,
				})
			},
		)

		// ---- atproto_track ----
		s.AddTool(
			mcp.NewTool("atproto_track",
				mcp.WithDescription("Start tracking a DID for ATProto indexing"),
				mcp.WithString("did", mcp.Required(), mcp.Description("DID to track")),
				mcp.WithString("reason", mcp.Description("Reason for tracking (default: mcp_manual)")),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				did, err := req.RequireString("did")
				if err != nil {
					return mcpError(err.Error())
				}
				reason := getString(req.GetArguments(), "reason")
				if reason == "" {
					reason = "mcp_manual"
				}
				indexer := buildATProtoIndexer(deps)
				if indexer == nil {
					return mcpError("indexer not available (missing database)")
				}
				if err := indexer.TrackRepo(ctx, strings.TrimSpace(did), reason); err != nil {
					return mcpErrorf("track repo: %v", err)
				}
				repo, _ := indexer.GetRepoInfo(ctx, strings.TrimSpace(did))
				return mcpToolResult(map[string]any{
					"tracked": true,
					"repo":    repo,
				})
			},
		)

		// ---- atproto_untrack ----
		s.AddTool(
			mcp.NewTool("atproto_untrack",
				mcp.WithDescription("Stop tracking a DID"),
				mcp.WithString("did", mcp.Required(), mcp.Description("DID to untrack")),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				did, err := req.RequireString("did")
				if err != nil {
					return mcpError(err.Error())
				}
				indexer := buildATProtoIndexer(deps)
				if indexer == nil {
					return mcpError("indexer not available")
				}
				if err := indexer.UntrackRepo(ctx, strings.TrimSpace(did)); err != nil {
					return mcpErrorf("untrack repo: %v", err)
				}
				return mcpToolResult(map[string]any{"untracked": true, "did": strings.TrimSpace(did)})
			},
		)

		// ---- atproto_firehose_status ----
		s.AddTool(
			mcp.NewTool("atproto_firehose_status",
				mcp.WithDescription("Firehose cursor positions and lag info"),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				replayCursor, _, _ := deps.database.GetBridgeState(ctx, "atproto_event_cursor")
				legacyCursor, _, _ := deps.database.GetBridgeState(ctx, "firehose_seq")
				eventHead, eventHeadOK, _ := deps.database.GetLatestATProtoEventCursor(ctx)

				var relaySourceCursor int64
				source, _ := deps.database.GetATProtoSource(ctx, "default-relay")
				if source != nil {
					relaySourceCursor = source.LastSeq
				}

				result := map[string]any{
					"bridge_replay_cursor":   replayCursor,
					"legacy_firehose_cursor": legacyCursor,
					"relay_source_cursor":    relaySourceCursor,
					"source":                 source,
				}
				if eventHeadOK {
					result["event_log_head_cursor"] = eventHead
				}
				return mcpToolResult(result)
			},
		)
	}
}

// buildATProtoIndexer creates a lightweight atindex.Service for tracking operations.
func buildATProtoIndexer(deps atprotoMCPDeps) *atindex.Service {
	if deps.database == nil {
		return nil
	}
	pdsResolver := backfill.DIDPDSResolver{
		PLCURL:     deps.plcURL,
		HTTPClient: deps.httpClient,
	}
	return atindex.New(
		deps.database,
		pdsResolver,
		backfill.XRPCRepoFetcher{HTTPClient: deps.httpClient},
		"", // relay URL not needed for track/untrack
		nil,
	)
}

// atprotoXRPCGet performs a GET request and returns the response body.
func atprotoXRPCGet(ctx context.Context, client *http.Client, url string) (json.RawMessage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// atURIParts holds parsed components of an AT URI.
type atURIParts struct {
	repo       string
	collection string
	rkey       string
}

// parseATURI parses "at://did/collection/rkey" into components.
func parseATURI(uri string) *atURIParts {
	uri = strings.TrimPrefix(uri, "at://")
	parts := strings.SplitN(uri, "/", 3)
	if len(parts) != 3 {
		return nil
	}
	return &atURIParts{
		repo:       parts[0],
		collection: parts[1],
		rkey:       parts[2],
	}
}

// runMCPATProto creates and runs the atproto MCP server over stdio.
func runMCPATProto(dbPath, plcURL, appviewURL string, insecure bool) error {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	if strings.TrimSpace(appviewURL) == "" {
		appviewURL = defaultLiveReadXRPCHost
	}
	if strings.TrimSpace(plcURL) == "" {
		plcURL = "https://plc.directory"
	}

	deps := atprotoMCPDeps{
		httpClient: httpClient,
		appviewURL: strings.TrimRight(appviewURL, "/"),
		plcURL:     strings.TrimRight(plcURL, "/"),
	}

	// Open bridge DB if path is provided (optional for pure XRPC tools).
	if strings.TrimSpace(dbPath) != "" {
		database, err := db.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer database.Close()
		deps.database = database
	}

	s := server.NewMCPServer(
		"atproto",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	registerATProtoTools(s, deps)

	return server.ServeStdio(s)
}
