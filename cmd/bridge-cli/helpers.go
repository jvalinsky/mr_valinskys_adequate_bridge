package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/atindex"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/blobbridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	"github.com/urfave/cli/v2"
)

func newBridgeLogRuntime(c *cli.Context, defaultService string) (*logutil.Runtime, error) {
	serviceName := strings.TrimSpace(c.String("otel-service-name"))
	if serviceName == "" {
		serviceName = defaultService
	}
	return logutil.NewRuntime(logutil.Config{
		Endpoint:    c.String("otel-logs-endpoint"),
		Protocol:    c.String("otel-logs-protocol"),
		Insecure:    c.Bool("otel-logs-insecure"),
		ServiceName: serviceName,
		CommandName: c.Command.Name,
		LocalOutput: c.String("local-log-output"),
	})
}

func shutdownLogRuntime(rt *logutil.Runtime) {
	if rt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = rt.Shutdown(ctx)
}

// parseHMACKey parses a 32-byte key from base64, hex, or raw input.
func parseHMACKey(raw string) (*[32]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		hex.DecodeString,
	}

	for _, decode := range decoders {
		b, err := decode(raw)
		if err != nil {
			continue
		}
		if len(b) == 32 {
			var key [32]byte
			copy(key[:], b)
			return &key, nil
		}
	}

	if len(raw) == 32 {
		var key [32]byte
		copy(key[:], []byte(raw))
		return &key, nil
	}

	return nil, fmt.Errorf("hmac key must decode to 32 bytes")
}

func resolveLiveXRPCHost(explicitHost string) (string, error) {
	if strings.TrimSpace(explicitHost) == "" {
		explicitHost = defaultLiveReadXRPCHost
	}
	return backfill.NormalizeServiceEndpoint(explicitHost)
}

func resolveLiveBlobHostResolver(explicitHost, plcURL string, insecure bool) (blobbridge.HostResolver, error) {
	if strings.TrimSpace(explicitHost) != "" {
		host, err := backfill.NormalizeServiceEndpoint(explicitHost)
		if err != nil {
			return nil, err
		}
		return backfill.FixedHostResolver{Host: host}, nil
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return backfill.DIDPDSResolver{
		PLCURL:     plcURL,
		HTTPClient: httpClient,
	}, nil
}

func resolveBackfillHostResolver(fixedHost, plcURL string, insecure bool) (backfill.HostResolver, error) {
	if strings.TrimSpace(fixedHost) != "" {
		host, err := backfill.NormalizeServiceEndpoint(fixedHost)
		if err != nil {
			return nil, err
		}
		return backfill.FixedHostResolver{Host: host}, nil
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return backfill.DIDPDSResolver{
		PLCURL:     plcURL,
		HTTPClient: httpClient,
	}, nil
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// readFirehoseCursor reads and parses the persisted firehose cursor sequence.
func readFirehoseCursor(ctx context.Context, database *db.DB) (int64, bool, error) {
	source, err := database.GetATProtoSource(ctx, "default-relay")
	if err == nil && source != nil && source.LastSeq > 0 {
		return source.LastSeq, true, nil
	}
	value, ok, err := database.GetBridgeState(ctx, "firehose_seq")
	if err != nil || !ok || strings.TrimSpace(value) == "" {
		return 0, ok, err
	}
	seq, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse firehose_seq state %q: %w", value, err)
	}
	return seq, true, nil
}

// dedupeStrings trims values, drops empties, and preserves first-seen order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func resolveSharedRepoPath(c *cli.Context) (string, error) {
	const defaultRepoPath = ".ssb-bridge"

	repoPath := strings.TrimSpace(c.String("repo-path"))
	repoPathSet := c.IsSet("repo-path")

	legacyValues := make([]string, 0, 2)
	if c.IsSet("ssb-repo-path") {
		legacyValues = append(legacyValues, strings.TrimSpace(c.String("ssb-repo-path")))
	}
	if c.IsSet("room-repo-path") {
		legacyValues = append(legacyValues, strings.TrimSpace(c.String("room-repo-path")))
	}

	legacyValues = dedupeStrings(legacyValues)
	switch {
	case repoPathSet:
		for _, legacy := range legacyValues {
			if legacy != "" && legacy != repoPath {
				return "", fmt.Errorf("conflicting repo flags: --repo-path=%q conflicts with legacy repo path %q; use --repo-path only", repoPath, legacy)
			}
		}
	case len(legacyValues) > 0:
		repoPath = legacyValues[0]
		if len(legacyValues) > 1 {
			return "", fmt.Errorf("conflicting legacy repo flags: %q vs %q; use a single --repo-path value", legacyValues[0], legacyValues[1])
		}
	default:
		repoPath = defaultRepoPath
	}

	if strings.TrimSpace(repoPath) == "" {
		return "", fmt.Errorf("repo path must not be empty")
	}
	return repoPath, nil
}

type compositeBlobStore struct {
	primary handlers.BlobStore
	fsPath  string
}

func (c *compositeBlobStore) Get(hash []byte) (io.ReadCloser, error) {
	if c.primary != nil {
		rc, err := c.primary.Get(hash)
		if err == nil {
			return rc, nil
		}
	}
	if c.fsPath != "" {
		return os.Open(filepath.Join(c.fsPath, fmt.Sprintf("%x", hash)))
	}
	return nil, os.ErrNotExist
}

func setBridgeStateBestEffort(ctx context.Context, database *db.DB, key, value string, logger *log.Logger) {
	if database == nil || strings.TrimSpace(key) == "" {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
	}
	if err := database.SetBridgeState(ctx, key, value); err != nil {
		logger.Printf("event=bridge_state_persist_error key=%s err=%v", key, err)
	}
}

// runRoomTunnelBootstrap connects the bridge sbot to the embedded room server
// and periodically re-announces on the room tunnel. It polls for readiness
// instead of using fixed sleeps, and retries the full sequence on failure.
func runRoomTunnelBootstrap(ctx context.Context, ssbRT *ssbruntime.Runtime, roomRT *room.Runtime, logger *log.Logger) {
	const reannounceEvery = 30 * time.Second

	bridgeFeed := ssbRT.Node().KeyPair.FeedRef()

	// Ensure bridge is a room admin so it can announce.
	if err := roomRT.AddMember(ctx, bridgeFeed, roomdb.RoleAdmin); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			logger.Printf("event=room_add_member_failed err=%v", err)
		}
	}

	for {
		peer, err := ssbRT.Node().Connect(ctx, roomRT.Addr(), roomRT.RoomFeed().PubKey())
		if err != nil {
			logger.Printf("event=room_tunnel_connect_failed err=%v", err)
			if !waitForRetry(ctx, 2*time.Second) {
				return
			}
			continue
		}
		rpc := peer.RPC()
		if rpc == nil {
			logger.Printf("event=room_tunnel_connect_failed err=no_muxrpc_endpoint")
			if !waitForRetry(ctx, 2*time.Second) {
				return
			}
			continue
		}

		if err := announceRoomPeer(ctx, rpc); err != nil {
			logger.Printf("event=room_tunnel_announce_failed err=%v", err)
			_ = peer.Conn.Close()
			if !waitForRetry(ctx, 2*time.Second) {
				return
			}
			continue
		}
		logger.Printf("event=room_tunnel_announce_success feed=%s room=%s", bridgeFeed.Ref(), roomRT.Addr())

		ticker := time.NewTicker(reannounceEvery)
		keepSession := true
		for keepSession {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if err := announceRoomPeer(ctx, rpc); err != nil {
					logger.Printf("event=room_tunnel_reannounce_failed err=%v", err)
					_ = peer.Conn.Close()
					ticker.Stop()
					if !waitForRetry(ctx, 2*time.Second) {
						return
					}
					keepSession = false
				}
			}
		}
	}
}

func announceRoomPeer(ctx context.Context, endpoint muxrpc.Endpoint) error {
	var announced bool
	if err := endpoint.Sync(ctx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		return err
	}
	if !announced {
		return fmt.Errorf("room tunnel announce returned false")
	}
	return nil
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func runRuntimeHeartbeatScheduler(ctx context.Context, database *db.DB, logger *log.Logger, interval time.Duration) {
	if database == nil {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC().Format(time.RFC3339)
			setBridgeStateBestEffort(ctx, database, bridgeRuntimeStatusKey, "live", logger)
			setBridgeStateBestEffort(ctx, database, bridgeRuntimeLastHeartbeatKey, now, logger)
		}
	}
}

// runRetryScheduler periodically retries failed unpublished messages.
func runRetryScheduler(ctx context.Context, processor *bridge.Processor, logger *log.Logger) {
	if processor == nil {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := processor.RetryFailedMessages(ctx, bridge.RetryConfig{
				Limit:       100,
				MaxAttempts: 8,
				BaseBackoff: 5 * time.Second,
			})
			if err != nil {
				logger.Printf("event=retry_scheduler_error err=%v", err)
				continue
			}
			if result.Attempted > 0 || result.Deferred > 0 || result.Failed > 0 {
				logger.Printf(
					"event=retry_scheduler selected=%d attempted=%d published=%d failed=%d deferred=%d",
					result.Selected,
					result.Attempted,
					result.Published,
					result.Failed,
					result.Deferred,
				)
			}
		}
	}
}

func runDeferredResolverScheduler(ctx context.Context, processor *bridge.Processor, logger *log.Logger) {
	if processor == nil {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := processor.ResolveDeferredMessages(ctx, 500)
			if err != nil {
				logger.Printf("event=deferred_scheduler_error err=%v", err)
				continue
			}
			if result.Attempted > 0 || result.Deferred > 0 || result.Failed > 0 || result.Published > 0 {
				logger.Printf(
					"event=deferred_scheduler selected=%d attempted=%d published=%d deferred=%d failed=%d",
					result.Selected,
					result.Attempted,
					result.Published,
					result.Deferred,
					result.Failed,
				)
			}
		}
	}
}

func runATProtoTrackScheduler(ctx context.Context, database *db.DB, indexer *atindex.Service, logger *log.Logger) {
	if database == nil || indexer == nil {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	trackActiveRepos(ctx, database, indexer, logger)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			trackActiveRepos(ctx, database, indexer, logger)
		}
	}
}

func trackActiveRepos(ctx context.Context, database *db.DB, indexer *atindex.Service, logger *log.Logger) {
	accounts, err := database.GetAllBridgedAccounts(ctx)
	if err != nil {
		logger.Printf("event=auto_backfill_error err=%v", err)
		return
	}
	for _, account := range accounts {
		if !account.Active {
			continue
		}
		if err := indexer.TrackRepo(ctx, account.ATDID, "active_bridged_account"); err != nil {
			logger.Printf("event=atindex_track_failed did=%s err=%v", account.ATDID, err)
		}
	}
}
