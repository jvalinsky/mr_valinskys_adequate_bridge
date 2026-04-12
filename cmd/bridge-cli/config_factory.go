package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/urfave/cli/v2"
)

type appMode string

const (
	appModeStart    appMode = "start"
	appModeBackfill appMode = "backfill"
	appModeRetry    appMode = "retry"
)

func buildAppConfigFromCLI(c *cli.Context, mode appMode) (AppConfig, error) {
	hmacKey, err := parseHMACKey(c.String("hmac-key"))
	if err != nil {
		return AppConfig{}, err
	}

	repoPath, err := resolveSharedRepoPath(c)
	if err != nil {
		return AppConfig{}, err
	}

	cfg := AppConfig{
		DBPath:          dbPath,
		RepoPath:        repoPath,
		BotSeed:         botSeed,
		HMACKey:         hmacKey,
		AppKey:          c.String("app-key"),
		PublishWorkers:  c.Int("publish-workers"),
		PLCURL:          c.String("plc-url"),
		AtprotoInsecure: c.Bool("atproto-insecure"),
	}

	switch mode {
	case appModeStart:
		cfg.SSBListenAddr = c.String("ssb-listen-addr")
		cfg.FirehoseEnable = c.Bool("firehose-enable")
		cfg.RelayURL = relayURL
		cfg.XRPCReadHost = c.String("xrpc-host")
		cfg.RoomEnable = c.Bool("room-enable")
		cfg.RoomListenAddr = c.String("room-listen-addr")
		cfg.RoomHTTPAddr = c.String("room-http-listen-addr")
		cfg.RoomMode = c.String("room-mode")
		cfg.RoomDomain = c.String("room-https-domain")
		cfg.RoomTLSCert = c.String("room-tls-cert")
		cfg.RoomTLSKey = c.String("room-tls-key")
		cfg.MCPListenAddr = c.String("mcp-listen-addr")
		cfg.MetricsListenAddr = c.String("metrics-listen-addr")
		cfg.MaxMsgsPerDIDPerMin = c.Int("max-msgs-per-did-per-min")
		cfg.BridgedPeerSyncIntv = c.Duration("bridged-room-peer-sync-interval")
		cfg.ReverseSyncEnable = c.Bool("reverse-sync-enable")
		cfg.ReverseCredentialsFile = c.String("reverse-credentials-file")
		cfg.ReverseSyncScanInterval = c.Duration("reverse-sync-scan-interval")
		cfg.ReverseSyncBatchSize = c.Int("reverse-sync-batch-size")
		cfg.HTTPTimeout = c.Duration("http-timeout")
		cfg.SSBDialTimeout = c.Duration("ssb-dial-timeout")
	case appModeBackfill:
		cfg.XRPCReadHost = c.String("xrpc-host")
		cfg.HTTPTimeout = c.Duration("http-timeout")
	case appModeRetry:
		// base shared fields only
	default:
		return AppConfig{}, fmt.Errorf("unsupported app mode %q", mode)
	}

	return cfg, nil
}

func newATProtoHTTPClient(insecure bool, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return client
}
