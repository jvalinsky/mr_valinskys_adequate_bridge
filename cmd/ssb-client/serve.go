package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

func runServe(c *cli.Context) error {
	if repoPath == "" {
		repoPath = defaultRepoPath
	}
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	if httpListenAddr == "" {
		httpListenAddr = defaultHTTPListen
	}

	logger := log.New(os.Stdout, "ssb-client: ", log.LstdFlags)

	slogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	secretPath := filepath.Join(repoPath, "secret")
	var keyPair *keys.KeyPair

	kp, err := keys.Load(secretPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load identity: %w", err)
		}
		logger.Printf("No identity found, generating new keypair...")
		kp, err = keys.Generate()
		if err != nil {
			return fmt.Errorf("generate keypair: %w", err)
		}
		if err := keys.Save(kp, secretPath); err != nil {
			return fmt.Errorf("save keypair: %w", err)
		}
		logger.Printf("New identity created: %s", kp.FeedRef().String())
	}
	keyPair = kp

	sbotOpts := sbot.Options{
		RepoPath:     repoPath,
		ListenAddr:   listenAddr,
		KeyPair:      keyPair,
		AppKey:       appKey,
		EnableEBT:    enableEBT,
		EnableRoom:   enableRoom,
		RoomMode:     roomMode,
		RoomHTTPAddr: roomHTTPAddr,
	}

	ssbClient, err := sbot.New(sbotOpts)
	if err != nil {
		return fmt.Errorf("create sbot: %w", err)
	}

	ssbClient.SetMessageLogger(func(author string, seq int64, msgType string, key string) {
		logger.Printf("RECV author=%s seq=%d type=%s key=%s", author, seq, msgType, key)
	})

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := ssbClient.Serve(); err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("sbot serve error: %v", err)
		}
	}()

	if initialPeers != "" {
		loadInitialPeers(ctx, initialPeers, logger, ssbClient)
	}

	r := chi.NewRouter()
	r.Use(websecurity.RequestLogMiddleware(logger))
	r.Use(websecurity.SecurityHeadersMiddleware(true))

	ui := newClientUIHandler(ssbClient, logger, slogger)
	ui.Mount(r)

	server := &http.Server{
		Addr:    httpListenAddr,
		Handler: r,
	}

	slog.Info("SSB client serving", "http_addr", httpListenAddr)
	slog.Info("SSB identity", "id", keyPair.FeedRef().String())
	slog.Info("SSB muxrpc listening", "addr", listenAddr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func loadInitialPeers(ctx context.Context, peersFile string, logger *log.Logger, sb *sbot.Sbot) {
	data, err := os.ReadFile(peersFile)
	if err != nil {
		logger.Printf("Failed to read peers file: %v", err)
		return
	}

	var peers []struct {
		Address string `json:"address"`
		PubKey  string `json:"pubkey"`
		FeedID  string `json:"feedId"`
	}

	if err := json.Unmarshal(data, &peers); err != nil {
		logger.Printf("Failed to parse peers file: %v", err)
		return
	}

	for _, peer := range peers {
		pubkeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(peer.PubKey, ".ed25519"))
		if err != nil || len(pubkeyBytes) != 32 {
			logger.Printf("Invalid pubkey for peer %s: %v", peer.Address, err)
			continue
		}
		go func(addr, feedID string, pk []byte) {
			connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if _, err := sb.Connect(connCtx, addr, pk); err != nil {
				logger.Printf("Failed to connect to initial peer %s: %v", addr, err)
			} else {
				logger.Printf("Connected to initial peer: %s (%s)", addr, feedID)
			}
		}(peer.Address, peer.FeedID, pubkeyBytes)
	}
}

// ---------------------------------------------------------------------------
// Web UI handler + routes
// ---------------------------------------------------------------------------

type clientUIHandler struct {
	sbot      *sbot.Sbot
	log       *log.Logger
	slog      *slog.Logger
	startTime time.Time
}

func newClientUIHandler(sb *sbot.Sbot, logger *log.Logger, slogger *slog.Logger) *clientUIHandler {
	return &clientUIHandler{
		sbot:      sb,
		log:       logger,
		slog:      slogger,
		startTime: time.Now(),
	}
}
