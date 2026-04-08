package main

import (
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

var sbotInstance *sbot.Sbot

// ---------------------------------------------------------------------------
// serve command
// ---------------------------------------------------------------------------

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

	sbotInstance, err = sbot.New(sbotOpts)
	if err != nil {
		return fmt.Errorf("create sbot: %w", err)
	}

	sbotInstance.SetMessageLogger(func(author string, seq int64, msgType string, key string) {
		logger.Printf("RECV author=%s seq=%d type=%s key=%s", author, seq, msgType, key)
	})

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := sbotInstance.Serve(); err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("sbot serve error: %v", err)
		}
	}()

	if initialPeers != "" {
		loadInitialPeers(ctx, initialPeers, logger)
	}

	r := chi.NewRouter()
	r.Use(websecurity.RequestLogMiddleware(logger))
	r.Use(websecurity.SecurityHeadersMiddleware(true))

	ui := newClientUIHandler(sbotInstance, logger, slogger)
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

func loadInitialPeers(ctx context.Context, peersFile string, logger *log.Logger) {
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
			if _, err := sbotInstance.Connect(connCtx, addr, pk); err != nil {
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

func (h *clientUIHandler) Mount(r chi.Router) {
	// Web UI
	r.Get("/", h.handleDashboard)
	r.Get("/feed", h.handleFeed)
	r.Get("/feeds", h.handleFeedsList)
	r.Get("/profile", h.handleProfile)
	r.Post("/profile", h.handleProfileAction)
	r.Get("/profile/{feedId}", h.handleUserProfile)
	r.Get("/compose", h.handleCompose)
	r.Post("/compose", h.handleCompose)
	r.Get("/following", h.handleFollowing)
	r.Post("/following", h.handleFollowingAction)
	r.Get("/followers", h.handleFollowers)
	r.Post("/followers", h.handleFollowersAction)
	r.Get("/peers", h.handlePeers)
	r.Post("/peers/add", h.handlePeersAdd)
	r.Get("/blobs", h.handleBlobs)
	r.Post("/blobs/upload", h.handleBlobsUpload)
	r.Get("/blobs/{hash}", h.handleBlobsGet)
	r.Get("/room", h.handleRoom)
	r.Post("/room", h.handleRoom)
	r.Get("/messages", h.handleMessages)
	r.Get("/message/{feedId}/{seq}", h.handleMessageDetail)
	r.Get("/replication", h.handleReplication)
	r.Get("/events", h.handleEvents)
	r.Get("/settings", h.handleSettings)

	// JSON API
	r.Get("/api/state", h.handleAPIState)
	r.Get("/api/feed", h.handleAPIFeed)
	r.Get("/api/feed/{feedId}", h.handleAPIFeedByID)
	r.Get("/api/feeds", h.handleAPIFeeds)
	r.Get("/api/message/{feedId}/{seq}", h.handleAPIMessage)
	r.Get("/api/peers", h.handleAPIPeers)
	r.Get("/api/messages", h.handleAPIMessages)
	r.Post("/api/publish", h.handleAPIPublish)
	r.Post("/api/connect", h.handleAPIConnect)
	r.Post("/api/follow", h.handleAPIFollow)
	r.Get("/api/replication", h.handleAPIReplication)
	r.Get("/api/whoami", h.handleAPIWhoami)
	r.Get("/api/blob/{hash}", h.handleBlobsGet)
	r.Get("/api/dm/conversations", h.handleAPIConversations)
	r.Get("/api/dm/{feed}", h.handleAPIConversation)
	r.Post("/api/dm", h.handleAPISendDM)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

const cssStyle = `
:root {
    color-scheme: light;
    --bg: #f4f1ea;
    --panel: #fffdf9;
    --ink: #1a2622;
    --muted: #4a5753;
    --line: #d7d2c6;
    --brand: #1a6b5a;
    --brand-strong: #124a3e;
    --warn: #8a5d12;
    --danger: #b33030;
    --ok: #1a6b35;
    --shadow: 0 10px 24px rgba(28, 41, 36, 0.08);
    --radius: 14px;
}

* { box-sizing: border-box; }

body { font-family: "Avenir Next", "Segoe UI", sans-serif; max-width: 1300px; margin: 0 auto; padding: 20px; background: var(--bg); color: var(--ink); }

.app-header { background: linear-gradient(135deg, var(--brand), var(--brand-strong)); color: #f8fffd; box-shadow: var(--shadow); border-radius: var(--radius); margin-bottom: 20px; }
.header-content { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; flex-wrap: wrap; gap: 12px; }
.brand { font-size: 1.1rem; font-weight: 800; letter-spacing: 0.02em; }
.nav-row { display: flex; flex-wrap: wrap; gap: 8px; }
.nav-link { text-decoration: none; font-size: 0.9rem; font-weight: 700; padding: 8px 12px; border-radius: 999px; border: 1px solid transparent; color: #f0f8f5; }
.nav-link:hover { border-color: rgba(240, 248, 245, 0.35); background: rgba(255, 255, 255, 0.10); }
.nav-link.is-active { background: #f8fffd; color: var(--brand-strong); }

h1 { color: var(--brand-strong); border-bottom: 2px solid var(--brand); padding-bottom: 10px; margin-top: 20px; }
h2 { color: var(--brand); margin-top: 24px; }
a { color: var(--brand); }

.panel { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 20px; box-shadow: var(--shadow); margin: 15px 0; }
.post { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; margin: 10px 0; border-left: 4px solid var(--brand); box-shadow: var(--shadow); }
.post-header { color: var(--muted); font-size: 0.85em; margin-bottom: 8px; }
.post-content { color: var(--ink); white-space: pre-wrap; }
.author { color: var(--brand); font-weight: bold; }
.empty { background: var(--panel); border: 1px dashed var(--line); padding: 30px; text-align: center; border-radius: var(--radius); color: var(--muted); }

.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 15px; margin: 15px 0; }
.stat-card { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; text-align: center; box-shadow: var(--shadow); }
.stat-card .value { font-size: 2em; color: var(--brand-strong); font-weight: bold; }
.stat-card .label { font-size: 0.85em; color: var(--muted); margin-top: 5px; }

input, textarea { width: 100%%; padding: 10px 12px; margin: 8px 0; border: 1px solid var(--line); border-radius: 8px; box-sizing: border-box; background: var(--panel); color: var(--ink); font-size: 0.95em; }
input:focus, textarea:focus { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px rgba(26, 107, 90, 0.15); }
button { background: var(--brand); color: #fff; padding: 10px 20px; border: none; border-radius: 8px; cursor: pointer; margin-top: 10px; font-weight: 700; font-size: 0.95em; }
button:hover { background: var(--brand-strong); }
.field { margin: 15px 0; }
.field label { display: block; font-weight: 700; margin-bottom: 5px; color: var(--ink); }
code { background: var(--bg); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; font-family: monospace; }
pre { background: var(--bg); padding: 15px; border-radius: var(--radius); overflow-x: auto; font-size: 0.85em; border: 1px solid var(--line); font-family: monospace; }
table { width: 100%%; border-collapse: collapse; margin: 15px 0; }
th, td { padding: 12px 14px; text-align: left; border-bottom: 1px solid var(--line); }
th { color: var(--muted); font-size: 0.85em; text-transform: uppercase; font-weight: 700; }
tr:hover td { background: var(--bg); }
.badge { display: inline-block; padding: 3px 10px; border-radius: 999px; font-size: 0.8em; background: var(--brand); color: #fff; font-weight: 600; }
.badge.warn { background: var(--warn); }
.badge.danger { background: var(--danger); }
.badge.ok { background: var(--ok); }
details summary { cursor: pointer; color: var(--brand); font-size: 0.9em; margin-top: 12px; font-weight: 600; }
ul { list-style: none; padding: 0; }
li { background: var(--panel); border: 1px solid var(--line); padding: 12px 15px; margin: 8px 0; border-radius: var(--radius); }
.pagination { display: flex; gap: 8px; margin-top: 15px; }
.pagination a { background: var(--panel); padding: 8px 14px; border-radius: 8px; margin: 0 3px; color: var(--brand); text-decoration: none; border: 1px solid var(--line); font-weight: 600; }
.pagination a:hover { background: var(--brand); color: #fff; }
`

func navHTML() string {
	return `<header class="app-header">
<div class="header-content">
  <span class="brand">SSB Client</span>
  <nav class="nav-row">
    <a href="/" class="nav-link">Dashboard</a>
    <a href="/feed" class="nav-link">Feed</a>
    <a href="/feeds" class="nav-link">Feeds</a>
    <a href="/compose" class="nav-link">Compose</a>
    <a href="/profile" class="nav-link">Profile</a>
    <a href="/following" class="nav-link">Following</a>
    <a href="/blobs" class="nav-link">Blobs</a>
    <a href="/peers" class="nav-link">Peers</a>
    <a href="/replication" class="nav-link">Replication</a>
    <a href="/room" class="nav-link">Room</a>
    <a href="/messages" class="nav-link">Messages</a>
    <a href="/settings" class="nav-link">Settings</a>
  </nav>
</div>
</header>`
}

func htmlPage(title, body string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - SSB Client</title>
    <style>%s</style>
</head>
<body>
%s
<main class="app-main">
%s
</main>
</body>
</html>`, html.EscapeString(title), cssStyle, navHTML(), body)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func parseLimit(r *http.Request, defaultLimit int) int {
	limit := defaultLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	return limit
}

// readFeedMessages reads up to limit messages from a feed log, newest first.
func readFeedMessages(feedLog feedlog.Log, limit int) []feedlog.StoredMessage {
	seq, err := feedLog.Seq()
	if err != nil || seq < 1 {
		return nil
	}
	startSeq := seq - int64(limit)
	if startSeq < 1 {
		startSeq = 1
	}
	var msgs []feedlog.StoredMessage
	for i := seq; i >= startSeq; i-- {
		msg, err := feedLog.Get(i)
		if err != nil {
			continue
		}
		msgs = append(msgs, *msg)
	}
	return msgs
}

// ---------------------------------------------------------------------------
// Web UI handlers
// ---------------------------------------------------------------------------

func (h *clientUIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()
	store := h.sbot.Store()
	peers := h.sbot.Peers()

	feeds, _ := store.Logs().List()
	rxLog, _ := store.ReceiveLog()
	rxSeq := int64(0)
	if rxLog != nil {
		rxSeq, _ = rxLog.Seq()
	}
	userSeq := int64(-1)
	if userLog, err := store.Logs().Get(whoami); err == nil {
		userSeq, _ = userLog.Seq()
	}

	uptime := time.Since(h.startTime).Truncate(time.Second)

	ebtPeers := 0
	if sm := h.sbot.StateMatrix(); sm != nil {
		ebtPeers = len(sm.Export())
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Dashboard</h1>
<div class="stat-grid">
  <div class="stat-card"><div class="value">%d</div><div class="label">Connected Peers</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Known Feeds</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Receive Log Seq</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">User Feed Seq</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">EBT Peers</div></div>
  <div class="stat-card"><div class="value">%s</div><div class="label">Uptime</div></div>
</div>
<div class="panel">
  <h2>Identity</h2>
  <p>Feed ID: <code>%s</code></p>
</div>`,
		len(peers), len(feeds), rxSeq, userSeq, ebtPeers, uptime.String(),
		html.EscapeString(whoami))

	if len(peers) > 0 {
		fmt.Fprintf(&body, `<div class="panel"><h2>Connected Peers</h2><table><tr><th>Feed ID</th><th>Address</th><th>Latency</th></tr>`)
		for _, p := range peers {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td></tr>`,
				html.EscapeString(p.ID.String()),
				html.EscapeString(p.Conn.RemoteAddr().String()),
				p.Latency().String())
		}
		fmt.Fprintf(&body, `</table></div>`)
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Dashboard", body.String()))
}

type FeedPost struct {
	Author    string
	Sequence  int64
	Timestamp int64
	Content   string
	Type      string
	RawJSON   string
}

func (h *clientUIHandler) handleFeed(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 50)
	filterType := r.URL.Query().Get("type")
	filterAuthor := r.URL.Query().Get("author")
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	var allPosts []FeedPost

	// If author filter is set, only read that feed
	if filterAuthor != "" {
		feedLog, err := store.Logs().Get(filterAuthor)
		if err == nil {
			for _, msg := range readFeedMessages(feedLog, limit) {
				post := msgToPost(msg)
				if filterType != "" && post.Type != filterType {
					continue
				}
				allPosts = append(allPosts, post)
			}
		}
	} else {
		// User's own feed
		if userLog, err := store.Logs().Get(whoami); err == nil {
			for _, msg := range readFeedMessages(userLog, limit) {
				post := msgToPost(msg)
				if filterType != "" && post.Type != filterType {
					continue
				}
				allPosts = append(allPosts, post)
			}
		}

		// Receive log (others)
		if rxLog, err := store.ReceiveLog(); err == nil {
			for _, msg := range readFeedMessages(rxLog, limit) {
				if msg.Metadata.Author == whoami {
					continue
				}
				post := msgToPost(msg)
				if filterType != "" && post.Type != filterType {
					continue
				}
				allPosts = append(allPosts, post)
			}
		}
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Feed</h1>
<p><a href="/compose">Compose new post</a></p>
<p>Showing %d messages</p>`, len(allPosts))

	if len(allPosts) == 0 {
		fmt.Fprintf(&body, `<div class="empty">No messages yet. Connect to peers to start receiving!</div>`)
	}

	for _, post := range allPosts {
		timestamp := time.Unix(post.Timestamp/1000, 0).Format("2006-01-02 15:04:05")
		escapedContent := html.EscapeString(post.Content)
		authorDisplay := html.EscapeString(post.Author)

		fmt.Fprintf(&body, `<div class="post">
  <div class="post-header">
    <span class="author">%s</span>
    <span class="badge">%s</span>
    seq=%d &middot; %s
  </div>`, authorDisplay, html.EscapeString(post.Type), post.Sequence, timestamp)

		if post.Content != "" {
			fmt.Fprintf(&body, `<div class="post-content">%s</div>`, escapedContent)
		}

		fmt.Fprintf(&body, `<details><summary>Raw JSON</summary><pre>%s</pre></details>
</div>`, html.EscapeString(post.RawJSON))
	}

	fmt.Fprintf(&body, `<div class="pagination">
  <a href="/feed?limit=25">25</a> <a href="/feed?limit=50">50</a>
  <a href="/feed?limit=100">100</a> <a href="/feed?limit=200">200</a>
</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Feed", body.String()))
}

func msgToPost(msg feedlog.StoredMessage) FeedPost {
	post := FeedPost{
		Author:    msg.Metadata.Author,
		Sequence:  msg.Metadata.Sequence,
		Timestamp: msg.Metadata.Timestamp,
		RawJSON:   prettyJSON(msg.Value),
	}
	var content map[string]interface{}
	if err := json.Unmarshal(msg.Value, &content); err == nil {
		if t, ok := content["type"].(string); ok {
			post.Type = t
		}
		if c, ok := content["text"].(string); ok {
			post.Content = c
		}
	}
	return post
}

func prettyJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return string(data)
	}
	return buf.String()
}

func (h *clientUIHandler) handleFeedsList(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	feeds, _ := store.Logs().List()

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Known Feeds (%d)</h1>
<table><tr><th>Feed ID</th><th>Sequence</th><th>Actions</th></tr>`, len(feeds))

	for _, feedID := range feeds {
		seq := int64(0)
		if feedLog, err := store.Logs().Get(feedID); err == nil {
			seq, _ = feedLog.Seq()
		}
		escapedID := html.EscapeString(feedID)
		encodedID := url.PathEscape(feedID)
		fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%d</td><td><a href="/feed?author=%s">View Feed</a> · <a href="/profile/%s">Profile</a></td></tr>`,
			escapedID, seq, encodedID, encodedID)
	}
	fmt.Fprintf(&body, `</table>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Feeds", body.String()))
}

type Profile struct {
	Name        string
	Description string
	Image       string
}

func (h *clientUIHandler) handleProfile(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	profile := h.getProfile(store, whoami)

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>My Profile</h1>
<div class="section">
  <p>Feed ID: <code>%s</code></p>
  <p>Display Name: %s</p>
  <p>Description: %s</p>
</div>
<div class="section">
  <h2>Update Profile</h2>
  <form method="POST" action="/profile">
    <div class="field"><label>Display Name</label><input type="text" name="name" value="%s" placeholder="Your name"></div>
    <div class="field"><label>Description</label><textarea name="description" rows="3" placeholder="Tell us about yourself">%s</textarea></div>
    <button type="submit">Save Profile</button>
  </form>
</div>`,
		html.EscapeString(whoami),
		html.EscapeString(profile.Name),
		html.EscapeString(profile.Description),
		html.EscapeString(profile.Name),
		html.EscapeString(profile.Description))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("My Profile", body.String()))
}

func (h *clientUIHandler) handleProfileAction(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := strings.TrimSpace(r.Form.Get("name"))
	description := strings.TrimSpace(r.Form.Get("description"))

	if name == "" && description == "" {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		h.slog.Error("failed to get publisher", "error", err)
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	if name != "" {
		content := map[string]interface{}{"type": "about", "name": name}
		if _, err = pub.PublishJSON(content); err != nil {
			h.slog.Error("failed to publish about name", "error", err)
		}
	}

	if description != "" {
		content := map[string]interface{}{"type": "about", "description": description}
		if _, err = pub.PublishJSON(content); err != nil {
			h.slog.Error("failed to publish about description", "error", err)
		}
	}

	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

func (h *clientUIHandler) getProfile(store *feedlog.StoreImpl, feedID string) Profile {
	p := Profile{}
	userLog, err := store.Logs().Get(feedID)
	if err != nil {
		return p
	}
	seq, _ := userLog.Seq()
	for i := seq; i >= 1; i-- {
		msg, err := userLog.Get(i)
		if err != nil {
			continue
		}
		var content map[string]interface{}
		if err := json.Unmarshal(msg.Value, &content); err != nil {
			continue
		}
		if content["type"] == "about" {
			if name, ok := content["name"].(string); ok && name != "" && p.Name == "" {
				p.Name = name
			}
			if desc, ok := content["description"].(string); ok && desc != "" && p.Description == "" {
				p.Description = desc
			}
		}
		if p.Name != "" && p.Description != "" {
			break
		}
	}
	return p
}

func (h *clientUIHandler) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	if !strings.HasPrefix(feedId, "@") {
		feedId = "@" + feedId
	}
	store := h.sbot.Store()
	profile := h.getProfile(store, feedId)
	escapedID := html.EscapeString(feedId)

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>User Profile</h1>
<div class="section">
  <p>Feed ID: <code>%s</code></p>
  <p>Display Name: %s</p>
  <p>Description: %s</p>
</div>
<form method="POST" action="/following">
  <input type="hidden" name="feed" value="%s">
  <button type="submit">Follow</button>
</form>
<p><a href="/feed?author=%s">View Feed</a></p>`,
		escapedID,
		html.EscapeString(profile.Name),
		html.EscapeString(profile.Description),
		escapedID,
		url.QueryEscape(feedId))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Profile", body.String()))
}

func (h *clientUIHandler) handleCompose(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		text := strings.TrimSpace(r.Form.Get("text"))
		if text != "" {
			pub, err := h.sbot.Publisher()
			if err != nil {
				h.slog.Error("failed to get publisher", "error", err)
				http.Error(w, "Failed to publish", http.StatusInternalServerError)
				return
			}
			content := map[string]interface{}{"type": "post", "text": text}
			msgRef, err := pub.PublishJSON(content)
			if err != nil {
				h.slog.Error("failed to publish post", "error", err)
				http.Error(w, "Failed to publish: "+err.Error(), http.StatusInternalServerError)
				return
			}
			h.slog.Info("published post", "ref", msgRef.String())
		}
		http.Redirect(w, r, "/feed", http.StatusSeeOther)
		return
	}

	body := `<h1>Compose Post</h1>
<div class="section">
  <form method="POST" action="/compose">
    <div class="field"><label>Message</label><textarea name="text" rows="4" placeholder="What's on your mind?"></textarea></div>
    <button type="submit">Post</button>
  </form>
</div>`

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Compose", body))
}

type Contact struct {
	FeedID    string
	Following bool
	Blocking  bool
	Sequence  int64
}

func (h *clientUIHandler) handleFollowing(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	var following []Contact
	userLog, err := store.Logs().Get(whoami)
	if err == nil {
		seq, _ := userLog.Seq()
		for i := seq; i >= 1; i-- {
			msg, err := userLog.Get(i)
			if err != nil {
				continue
			}
			var content map[string]interface{}
			if err := json.Unmarshal(msg.Value, &content); err != nil {
				continue
			}
			if content["type"] == "contact" {
				contactStr, ok := content["contact"].(string)
				if !ok {
					continue
				}
				followingBool, _ := content["following"].(bool)
				blockingBool, _ := content["blocking"].(bool)
				if followingBool && !blockingBool {
					following = append(following, Contact{
						FeedID:    strings.TrimPrefix(contactStr, "@"),
						Following: followingBool,
						Sequence:  msg.Metadata.Sequence,
					})
				}
			}
		}
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Following (%d)</h1>
<div class="section">
  <h2>Follow someone</h2>
  <form method="POST" action="/following">
    <input type="text" name="feed" placeholder="@feedid.ed25519">
    <button type="submit">Follow</button>
  </form>
</div>`, len(following))

	if len(following) == 0 {
		fmt.Fprintf(&body, `<div class="empty">You aren't following anyone yet.</div>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Feed ID</th><th>Seq</th><th>Action</th></tr>`)
		for _, c := range following {
			escapedFeed := html.EscapeString(c.FeedID)
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%d</td><td>
<form method="POST" action="/following?action=unfollow" style="display:inline">
<input type="hidden" name="feed" value="%s"><button type="submit">Unfollow</button>
</form></td></tr>`, escapedFeed, c.Sequence, escapedFeed)
		}
		fmt.Fprintf(&body, `</table>`)
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Following", body.String()))
}

func (h *clientUIHandler) handleFollowingAction(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	feed := strings.TrimSpace(r.Form.Get("feed"))
	action := r.URL.Query().Get("action")

	if feed == "" {
		http.Redirect(w, r, "/following", http.StatusSeeOther)
		return
	}
	if !strings.HasPrefix(feed, "@") {
		feed = "@" + feed
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		h.slog.Error("failed to get publisher", "error", err)
		http.Error(w, "Failed to publish", http.StatusInternalServerError)
		return
	}

	following := action != "unfollow"
	content := map[string]interface{}{
		"type":      "contact",
		"contact":   feed,
		"following": following,
		"blocking":  false,
	}
	if _, err = pub.PublishJSON(content); err != nil {
		h.slog.Error("failed to publish contact", "error", err)
	}

	http.Redirect(w, r, "/following", http.StatusSeeOther)
}

func (h *clientUIHandler) handleFollowers(w http.ResponseWriter, r *http.Request) {
	body := `<h1>Followers</h1>
<div class="section"><p><em>Followers detection requires scanning peer feeds for contact messages referencing you.</em></p></div>`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Followers", body))
}

func (h *clientUIHandler) handleFollowersAction(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/followers", http.StatusSeeOther)
}

func (h *clientUIHandler) handleBlobs(w http.ResponseWriter, r *http.Request) {
	body := `<h1>Blob Storage</h1>
<div class="section">
  <h2>Upload Blob</h2>
  <form method="POST" action="/blobs/upload" enctype="multipart/form-data">
    <input type="file" name="file">
    <button type="submit">Upload</button>
  </form>
</div>
<div class="section">
  <p>Blobs are automatically fetched from peers when referenced in messages.</p>
  <p>Use blob references in posts like: &amp;&lt;hash&gt;.sha256</p>
</div>`

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Blobs", body))
}

func (h *clientUIHandler) handleBlobsUpload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/blobs", http.StatusSeeOther)
		return
	}
	defer file.Close()

	blobStore := h.sbot.BlobStore().BlobStore()
	hash, err := blobStore.Put(file)
	if err != nil {
		h.slog.Error("failed to store blob", "error", err)
		http.Redirect(w, r, "/blobs", http.StatusSeeOther)
		return
	}

	h.slog.Info("uploaded blob", "hash", fmt.Sprintf("%x", hash), "size", header.Size)
	http.Redirect(w, r, "/blobs", http.StatusSeeOther)
}

func (h *clientUIHandler) handleBlobsGet(w http.ResponseWriter, r *http.Request) {
	hashStr := chi.URLParam(r, "hash")

	hashBytes, err := base64.URLEncoding.DecodeString(hashStr)
	if err != nil {
		hashBytes, err = hex.DecodeString(hashStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	blobStore := h.sbot.BlobStore().BlobStore()
	reader, err := blobStore.Get(hashBytes)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, reader)
}

func (h *clientUIHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers := h.sbot.Peers()

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Connected Peers (%d)</h1>`, len(peers))

	if len(peers) > 0 {
		fmt.Fprintf(&body, `<table>
<tr><th>Feed ID</th><th>Address</th><th>Read</th><th>Write</th><th>Latency</th></tr>`)
		for _, peer := range peers {
			fmt.Fprintf(&body, `<tr>
  <td><code>%s</code></td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
</tr>`,
				html.EscapeString(peer.ID.String()),
				html.EscapeString(peer.Conn.RemoteAddr().String()),
				formatBytes(peer.ReadBytes()),
				formatBytes(peer.WriteBytes()),
				peer.Latency().String())
		}
		fmt.Fprintf(&body, `</table>`)
	} else {
		fmt.Fprintf(&body, `<div class="empty">No peers connected.</div>`)
	}

	fmt.Fprintf(&body, `<div class="section">
  <h2>Connect to Peer</h2>
  <form method="POST" action="/peers/add">
    <div class="field"><label>Address</label><input type="text" name="address" placeholder="host:port"></div>
    <div class="field"><label>Public Key</label><input type="text" name="pubkey" placeholder="base64 pubkey (.ed25519 suffix optional)"></div>
    <button type="submit">Connect</button>
  </form>
</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Peers", body.String()))
}

func (h *clientUIHandler) handlePeersAdd(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	address := strings.TrimSpace(r.Form.Get("address"))
	pubkeyStr := strings.TrimSpace(r.Form.Get("pubkey"))

	if address == "" || pubkeyStr == "" {
		http.Redirect(w, r, "/peers", http.StatusSeeOther)
		return
	}

	pubkeyDecoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(pubkeyStr, ".ed25519"))
	if err != nil || len(pubkeyDecoded) != 32 {
		h.slog.Warn("invalid pubkey format")
		http.Redirect(w, r, "/peers", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if _, err = h.sbot.Connect(ctx, address, pubkeyDecoded); err != nil {
		h.slog.Error("failed to connect to peer", "address", address, "error", err)
	}

	http.Redirect(w, r, "/peers", http.StatusSeeOther)
}

func (h *clientUIHandler) handleRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		inviteCode := strings.TrimSpace(r.Form.Get("invite"))
		if inviteCode != "" {
			if err := h.consumeInvite(r.Context(), inviteCode); err != nil {
				h.slog.Error("failed to use invite code", "error", err)
				http.Redirect(w, r, "/room?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
				return
			}
			h.slog.Info("successfully joined room using invite code")
		}
		roomAddr := strings.TrimSpace(r.Form.Get("room_address"))
		if roomAddr != "" {
			h.slog.Info("would connect to room", "address", roomAddr)
		}
		http.Redirect(w, r, "/room", http.StatusSeeOther)
		return
	}

	errorMsg := r.URL.Query().Get("error")

	peers := h.sbot.Peers()
	var roomPeers []string
	for _, p := range peers {
		roomPeers = append(roomPeers, p.ID.String())
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Room Connection</h1>`)

	if errorMsg != "" {
		fmt.Fprintf(&body, `<div class="panel" style="border-left: 4px solid var(--danger);">
  <strong>Error:</strong> %s
</div>`, html.EscapeString(errorMsg))
	}

	fmt.Fprintf(&body, `<div class="stat-grid">
  <div class="stat-card"><div class="value">%d</div><div class="label">Connected Peers</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Room Peers</div></div>
</div>`, len(peers), len(roomPeers))

	fmt.Fprintf(&body, `<div class="panel">
  <h2>Join a Room</h2>
  <form method="POST" action="/room">
    <div class="field">
      <label for="invite">HTTP Invite URL</label>
      <input type="text" id="invite" name="invite" placeholder="http://room.example.com/join?token=xxx">
      <small style="color: var(--muted)">Paste a full HTTP invite URL from a Room2 server</small>
    </div>
    <button type="submit">Join Room</button>
  </form>
</div>

<div class="panel">
  <h2>About SSB Rooms</h2>
  <p>SSB Rooms provide relay services for peers behind NAT or firewalls. They enable:</p>
  <ul>
    <li><strong>Tunnel connections</strong> - Connect through the room's relay</li>
    <li><strong>Invite codes</strong> - HTTP URLs that let new users join</li>
    <li><strong>Moderation</strong> - Room operators can deny problematic keys</li>
  </ul>
  <p>To join a room, get an invite URL from an existing member or the room operator.</p>
</div>

<div class="panel">
  <h2>Connected Room Peers (%d/%d)</h2>`,
		len(peers), len(roomPeers))

	if len(roomPeers) == 0 {
		fmt.Fprintf(&body, `<p class="empty">Not connected to any room peers. Join a room to start.</p>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Feed ID</th></tr>`)
		for _, feed := range roomPeers {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td></tr>`, html.EscapeString(feed))
		}
		fmt.Fprintf(&body, `</table>`)
	}
	fmt.Fprintf(&body, `</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Room", body.String()))
}

func (h *clientUIHandler) consumeInvite(ctx context.Context, inviteCode string) error {
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return fmt.Errorf("empty invite code")
	}

	var roomHTTPAddr string
	var token string

	parsedURL, err := url.Parse(inviteCode)
	if err != nil {
		return fmt.Errorf("invalid invite URL: %w", err)
	}

	if parsedURL.Host != "" {
		if t := parsedURL.Query().Get("token"); t != "" {
			token = t
		} else if t := parsedURL.Query().Get("invite"); t != "" {
			token = t
		} else {
			return fmt.Errorf("no token found in invite URL")
		}

		host := parsedURL.Hostname()
		port := parsedURL.Port()
		if host == "localhost" || host == "127.0.0.1" {
			// In E2E demo, the bridge is known as 'bridge' inside the container
			host = "bridge"
		}
		if port == "" {
			if parsedURL.Scheme == "https" {
				roomHTTPAddr = "https://" + host
			} else {
				roomHTTPAddr = "http://" + host
			}
		} else {
			roomHTTPAddr = parsedURL.Scheme + "://" + host + ":" + port
		}
		h.slog.Info("consuming invite", "invite_url", inviteCode, "target_http_addr", roomHTTPAddr)
	} else {
		return fmt.Errorf("invite code must be a full URL (e.g., http://127.0.0.1:8976/join?token=xxx)")
	}

	if roomHTTPAddr == "" {
		return fmt.Errorf("room HTTP address not provided; set --room-http-addr or use full invite URL")
	}

	whoami, err := h.sbot.Whoami()
	if err != nil {
		return fmt.Errorf("get identity: %w", err)
	}

	body := map[string]string{
		"id":     whoami,
		"invite": token,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	consumeURL := roomHTTPAddr + "/invite/consume"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, consumeURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if result.MultiserverAddress == "" {
		return fmt.Errorf("no multiserver address returned")
	}

	if err := h.connectToPeer(ctx, result.MultiserverAddress); err != nil {
		return fmt.Errorf("connect to room: %w", err)
	}

	return nil
}

func (h *clientUIHandler) connectToPeer(ctx context.Context, address string) error {
	var pubkey []byte
	var hostPort string

	parts := strings.Split(address, "~shs:")
	if len(parts) == 2 {
		hostPort = strings.TrimPrefix(parts[0], "net:")
		pkBase64 := strings.TrimPrefix(parts[1], "shs:")
		var err error
		pubkey, err = base64.StdEncoding.DecodeString(pkBase64)
		if err != nil {
			return fmt.Errorf("decode pubkey: %w", err)
		}
	} else {
		hostPort = strings.TrimPrefix(address, "net:")
	}

	_, err := h.sbot.Connect(ctx, hostPort, pubkey)
	return err
}

func (h *clientUIHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	var statusMsg string
	var statusClass string

	if r.Method == "POST" {
		r.ParseForm()
		recipient := strings.TrimSpace(r.Form.Get("recipient"))
		message := strings.TrimSpace(r.Form.Get("message"))
		if recipient != "" && message != "" {
			pub, err := h.sbot.Publisher()
			if err != nil {
				h.slog.Error("failed to get publisher", "error", err)
				statusMsg = "Failed to send: could not access publisher"
				statusClass = "error"
			} else {
				_, err := pub.PublishPrivate(message, recipient)
				if err != nil {
					h.slog.Error("failed to send DM", "recipient", recipient, "error", err)
					statusMsg = fmt.Sprintf("Failed to send: %v", err)
					statusClass = "error"
				} else {
					h.slog.Info("sent DM", "recipient", recipient)
					statusMsg = "Message sent successfully"
					statusClass = "success"
				}
			}
		} else {
			statusMsg = "Recipient and message are required"
			statusClass = "error"
		}
		http.Redirect(w, r, "/messages?status="+url.QueryEscape(statusMsg)+"&class="+statusClass, http.StatusSeeOther)
		return
	}

	r.ParseForm()
	if status := r.Form.Get("status"); status != "" {
		statusMsg = status
		statusClass = r.Form.Get("class")
		if statusClass == "" {
			statusClass = "info"
		}
	}

	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Messages</h1>`)

	if statusMsg != "" {
		cssClass := "info"
		if statusClass == "success" {
			cssClass = "success"
		} else if statusClass == "error" {
			cssClass = "error"
		}
		fmt.Fprintf(&body, `<div class="status %s">%s</div>`, cssClass, html.EscapeString(statusMsg))
	}

	fmt.Fprintf(&body, `<div class="section">
  <h2>Send a Direct Message</h2>
  <form method="POST" action="/messages">
    <div class="field"><label>Recipient</label><input type="text" name="recipient" placeholder="@feedid.ed25519" value="%s"></div>
    <div class="field"><label>Message</label><textarea name="message" rows="3" placeholder="Your message..."></textarea></div>
    <button type="submit">Send Encrypted DM</button>
  </form>
  <p><small>Messages are encrypted using box2 (curve25519 + nacl secretbox)</small></p>
</div>`, html.EscapeString(r.FormValue("recipient")))

	// Show recent messages from user's log
	fmt.Fprintf(&body, `<div class="section"><h2>Recent Messages (own feed)</h2>`)
	userLog, err := store.Logs().Get(whoami)
	if err == nil {
		msgs := readFeedMessages(userLog, 20)
		if len(msgs) == 0 {
			fmt.Fprintf(&body, `<p>No messages yet.</p>`)
		} else {
			fmt.Fprintf(&body, `<table><tr><th>Seq</th><th>Type</th><th>Content</th></tr>`)
			for _, msg := range msgs {
				post := msgToPost(msg)
				content := html.EscapeString(post.Content)
				if content == "" {
					content = "<em>" + html.EscapeString(post.Type) + "</em>"
				}
				fmt.Fprintf(&body, `<tr><td><a href="/message/%s/%d">%d</a></td><td><span class="badge">%s</span></td><td>%s</td></tr>`,
					url.PathEscape(whoami), post.Sequence, post.Sequence,
					html.EscapeString(post.Type), content)
			}
			fmt.Fprintf(&body, `</table>`)
		}
	}
	fmt.Fprintf(&body, `</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Messages", body.String()))
}

func (h *clientUIHandler) handleMessageDetail(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	seqStr := chi.URLParam(r, "seq")

	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid sequence number", http.StatusBadRequest)
		return
	}

	store := h.sbot.Store()
	feedLog, err := store.Logs().Get(feedId)
	if err != nil {
		http.Error(w, "feed not found", http.StatusNotFound)
		return
	}

	msg, err := feedLog.Get(seq)
	if err != nil {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Message Detail</h1>
<div class="section">
  <table>
    <tr><th>Key</th><td><code>%s</code></td></tr>
    <tr><th>Author</th><td><code>%s</code></td></tr>
    <tr><th>Sequence</th><td>%d</td></tr>
    <tr><th>Timestamp</th><td>%s</td></tr>
    <tr><th>Previous</th><td><code>%s</code></td></tr>
    <tr><th>Hash</th><td>%s</td></tr>
  </table>
</div>
<div class="section">
  <h2>Raw Content</h2>
  <pre>%s</pre>
</div>
<p><a href="/feed?author=%s">Back to feed</a></p>`,
		html.EscapeString(msg.Key),
		html.EscapeString(msg.Metadata.Author),
		msg.Metadata.Sequence,
		time.Unix(msg.Metadata.Timestamp/1000, 0).Format("2006-01-02 15:04:05 MST"),
		html.EscapeString(msg.Metadata.Previous),
		html.EscapeString(msg.Metadata.Hash),
		html.EscapeString(prettyJSON(msg.Value)),
		url.QueryEscape(feedId))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Message", body.String()))
}

func (h *clientUIHandler) handleReplication(w http.ResponseWriter, r *http.Request) {
	sm := h.sbot.StateMatrix()
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Replication Status</h1>
<div class="stat-grid">
  <div class="stat-card"><div class="value">%s</div><div class="label">EBT Enabled</div></div>`, boolToYesNo(sm != nil))

	feedCount := 0
	rxSeq := int64(0)
	if logs := store.Logs(); logs != nil {
		feeds, _ := logs.List()
		feedCount = len(feeds)
	}
	if rxLog, _ := store.ReceiveLog(); rxLog != nil {
		rxSeq, _ = rxLog.Seq()
	}

	userSeq := int64(0)
	if userLog, _ := store.Logs().Get(whoami); userLog != nil {
		userSeq, _ = userLog.Seq()
	}

	fmt.Fprintf(&body, `  <div class="stat-card"><div class="value">%d</div><div class="label">Known Feeds</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Messages Received</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">User Feed Seq</div></div>
</div>`,
		feedCount, rxSeq, userSeq)

	if sm == nil {
		fmt.Fprintf(&body, `<div class="panel">
  <h2>EBT Not Enabled</h2>
  <p>Enable EBT in your configuration for replicated feeds and state tracking.</p>
</div>`)
	} else {
		matrix := sm.Export()

		fmt.Fprintf(&body, `<div class="panel">
  <h2>Replicated Feeds (%d)</h2>
  <table><tr><th>Feed ID</th><th>State Value</th><th>Status</th></tr>`,
			len(matrix))

		for feedID, state := range matrix {
			stateStr := "N/A"
			statusStr := `<span class="badge warn">Unknown</span>`
			if seq, ok := state["seq"]; ok {
				if seq == -1 {
					stateStr = "Unfollow"
					statusStr = `<span class="badge">Unfollowed</span>`
				} else {
					stateStr = fmt.Sprintf("%d", seq)
					if seq > 0 {
						statusStr = `<span class="badge ok">Active</span>`
					} else {
						statusStr = `<span class="badge">Pending</span>`
					}
				}
			}
			fmt.Fprintf(&body, `<tr>
    <td><code>%s</code></td>
    <td>%s</td>
    <td>%s</td>
  </tr>`,
				html.EscapeString(feedID),
				stateStr,
				statusStr)
		}
		fmt.Fprintf(&body, `</table></div>`)

		data, _ := json.MarshalIndent(matrix, "", "  ")
		fmt.Fprintf(&body, `<div class="panel">
  <h2>EBT State Format</h2>
  <p>State values: -1 = unfollowed, 0 = pending, positive = sequence number (bit 0 = receive enabled)</p>
  <details><summary>View Raw JSON</summary>
  <pre>%s</pre>
  </details>
</div>`,
			html.EscapeString(string(data)))
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Replication", body.String()))
}

func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func (h *clientUIHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	lastRxSeq := int64(0)
	lastUserSeq := int64(0)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}

		if rxLog, err := store.ReceiveLog(); err == nil {
			currentRxSeq, _ := rxLog.Seq()
			if currentRxSeq > lastRxSeq {
				for seq := lastRxSeq + 1; seq <= currentRxSeq; seq++ {
					msg, err := rxLog.Get(seq)
					if err != nil {
						continue
					}
					var content map[string]interface{}
					json.Unmarshal(msg.Value, &content)
					msgType, _ := content["type"].(string)
					if msgType == "post" || msgType == "contact" || msgType == "about" {
						data, _ := json.Marshal(map[string]interface{}{
							"type":      "message",
							"sequence":  seq,
							"author":    msg.Metadata.Author,
							"timestamp": msg.Metadata.Timestamp,
						})
						fmt.Fprintf(w, "data: %s\n\n", data)
						flusher.Flush()
					}
				}
				lastRxSeq = currentRxSeq
			}
		}

		if userLog, err := store.Logs().Get(whoami); err == nil {
			userSeq, _ := userLog.Seq()
			if userSeq > lastUserSeq {
				for seq := lastUserSeq + 1; seq <= userSeq; seq++ {
					msg, err := userLog.Get(seq)
					if err != nil {
						continue
					}
					var content map[string]interface{}
					json.Unmarshal(msg.Value, &content)
					msgType, _ := content["type"].(string)
					if msgType == "post" || msgType == "contact" || msgType == "about" {
						data, _ := json.Marshal(map[string]interface{}{
							"type":      "message",
							"sequence":  seq,
							"author":    msg.Metadata.Author,
							"timestamp": msg.Metadata.Timestamp,
						})
						fmt.Fprintf(w, "data: %s\n\n", data)
						flusher.Flush()
					}
				}
				lastUserSeq = userSeq
			}
		}
	}
}

func (h *clientUIHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()

	body := fmt.Sprintf(`<h1>Settings</h1>
<div class="section">
  <h2>Identity</h2>
  <p>Your feed ID: <code>%s</code></p>
  <p>Export your identity secret for backup.</p>
  <form method="POST" action="/settings/export">
    <button type="submit">Export Identity</button>
  </form>
</div>
<div class="section">
  <h2>About</h2>
  <p>SSB Client — dev/testing tool for Mr. Valinsky's Adequate Bridge</p>
  <p>Version: 0.2.0</p>
</div>`, html.EscapeString(whoami))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Settings", body))
}

// ---------------------------------------------------------------------------
// JSON API handlers
// ---------------------------------------------------------------------------

func (h *clientUIHandler) handleAPIWhoami(w http.ResponseWriter, r *http.Request) {
	whoami, err := h.sbot.Whoami()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"feedId": whoami})
}

func (h *clientUIHandler) handleAPIState(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	peers := h.sbot.Peers()

	rxLog, _ := store.ReceiveLog()
	rxSeq := int64(0)
	if rxLog != nil {
		rxSeq, _ = rxLog.Seq()
	}

	userSeq := int64(-1)
	if userLog, err := store.Logs().Get(whoami); err == nil {
		userSeq, _ = userLog.Seq()
	}

	feeds, _ := store.Logs().List()

	peerIDs := make([]string, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.ID.String())
	}

	ebtPeers := 0
	if sm := h.sbot.StateMatrix(); sm != nil {
		ebtPeers = len(sm.Export())
	}

	state := map[string]interface{}{
		"identity":       whoami,
		"peers":          len(peers),
		"feedsCount":     len(feeds),
		"receiveLogSeq":  rxSeq,
		"userFeedSeq":    userSeq,
		"connectedPeers": peerIDs,
		"ebtPeers":       ebtPeers,
		"uptimeSeconds":  int64(time.Since(h.startTime).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (h *clientUIHandler) handleAPIFeeds(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	feedIDs, _ := store.Logs().List()

	type FeedInfo struct {
		FeedID   string `json:"feedId"`
		Sequence int64  `json:"sequence"`
	}

	feedList := make([]FeedInfo, 0, len(feedIDs))
	for _, id := range feedIDs {
		seq := int64(0)
		if feedLog, err := store.Logs().Get(id); err == nil {
			seq, _ = feedLog.Seq()
		}
		feedList = append(feedList, FeedInfo{FeedID: id, Sequence: seq})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"feeds": feedList,
		"count": len(feedList),
	})
}

func (h *clientUIHandler) handleAPIFeed(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	limit := parseLimit(r, 50)
	filterType := r.URL.Query().Get("type")

	var messages []map[string]interface{}

	collectMessages := func(feedLog feedlog.Log, skipAuthor string) {
		for _, msg := range readFeedMessages(feedLog, limit) {
			if skipAuthor != "" && msg.Metadata.Author == skipAuthor {
				continue
			}
			item := map[string]interface{}{
				"author":    msg.Metadata.Author,
				"sequence":  msg.Metadata.Sequence,
				"timestamp": msg.Metadata.Timestamp,
				"key":       msg.Key,
				"content":   json.RawMessage(msg.Value),
			}
			if filterType != "" {
				var c map[string]interface{}
				json.Unmarshal(msg.Value, &c)
				if t, _ := c["type"].(string); t != filterType {
					continue
				}
			}
			messages = append(messages, item)
		}
	}

	if userLog, err := store.Logs().Get(whoami); err == nil {
		collectMessages(userLog, "")
	}
	if rxLog, err := store.ReceiveLog(); err == nil {
		collectMessages(rxLog, whoami)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIFeedByID(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	limit := parseLimit(r, 50)
	filterType := r.URL.Query().Get("type")

	store := h.sbot.Store()
	feedLog, err := store.Logs().Get(feedId)
	if err != nil {
		http.Error(w, `{"error":"feed not found"}`, http.StatusNotFound)
		return
	}

	var messages []map[string]interface{}
	for _, msg := range readFeedMessages(feedLog, limit) {
		if filterType != "" {
			var c map[string]interface{}
			json.Unmarshal(msg.Value, &c)
			if t, _ := c["type"].(string); t != filterType {
				continue
			}
		}
		messages = append(messages, map[string]interface{}{
			"author":    msg.Metadata.Author,
			"sequence":  msg.Metadata.Sequence,
			"timestamp": msg.Metadata.Timestamp,
			"key":       msg.Key,
			"content":   json.RawMessage(msg.Value),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIMessage(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	seqStr := chi.URLParam(r, "seq")

	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid sequence"}`, http.StatusBadRequest)
		return
	}

	store := h.sbot.Store()
	feedLog, err := store.Logs().Get(feedId)
	if err != nil {
		http.Error(w, `{"error":"feed not found"}`, http.StatusNotFound)
		return
	}

	msg, err := feedLog.Get(seq)
	if err != nil {
		http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
		return
	}

	result := map[string]interface{}{
		"key":       msg.Key,
		"author":    msg.Metadata.Author,
		"sequence":  msg.Metadata.Sequence,
		"timestamp": msg.Metadata.Timestamp,
		"previous":  msg.Metadata.Previous,
		"hash":      msg.Metadata.Hash,
		"content":   json.RawMessage(msg.Value),
		"received":  msg.Received,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *clientUIHandler) handleAPIPeers(w http.ResponseWriter, r *http.Request) {
	peers := h.sbot.Peers()

	type PeerInfo struct {
		ID         string `json:"id"`
		Address    string `json:"address"`
		Connected  bool   `json:"connected"`
		ReadBytes  int64  `json:"readBytes"`
		WriteBytes int64  `json:"writeBytes"`
		LatencyMs  int64  `json:"latencyMs"`
	}

	peerList := make([]PeerInfo, 0, len(peers))
	for _, p := range peers {
		peerList = append(peerList, PeerInfo{
			ID:         p.ID.String(),
			Address:    p.Conn.RemoteAddr().String(),
			Connected:  true,
			ReadBytes:  p.ReadBytes(),
			WriteBytes: p.WriteBytes(),
			LatencyMs:  p.Latency().Milliseconds(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": peerList,
		"count": len(peerList),
	})
}

func (h *clientUIHandler) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	limit := parseLimit(r, 50)
	msgType := r.URL.Query().Get("type")
	author := r.URL.Query().Get("author")

	var messages []map[string]interface{}

	// If author is specified, query that specific feed
	targetFeed := whoami
	if author != "" {
		targetFeed = author
	}

	feedLog, err := store.Logs().Get(targetFeed)
	if err == nil {
		for _, msg := range readFeedMessages(feedLog, limit) {
			if msgType != "" {
				var c map[string]interface{}
				json.Unmarshal(msg.Value, &c)
				if t, _ := c["type"].(string); t != msgType {
					continue
				}
			}
			messages = append(messages, map[string]interface{}{
				"author":    msg.Metadata.Author,
				"sequence":  msg.Metadata.Sequence,
				"timestamp": msg.Metadata.Timestamp,
				"key":       msg.Key,
				"content":   json.RawMessage(msg.Value),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIPublish(w http.ResponseWriter, r *http.Request) {
	var content map[string]interface{}

	// Accept both JSON body and form-encoded data
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
	} else {
		r.ParseForm()
		contentType := strings.TrimSpace(r.Form.Get("type"))
		if contentType == "" {
			contentType = "post"
		}
		content = map[string]interface{}{"type": contentType}
		if text := strings.TrimSpace(r.Form.Get("text")); text != "" {
			content["text"] = text
		}
		for k, v := range r.Form {
			if k != "type" && k != "text" {
				content[k] = v[0]
			}
		}
	}

	if content["type"] == nil || content["type"] == "" {
		content["type"] = "post"
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"key":     msgRef.String(),
	})
}

func (h *clientUIHandler) handleAPIConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		PubKey  string `json:"pubkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Address == "" || req.PubKey == "" {
		http.Error(w, `{"error":"address and pubkey required"}`, http.StatusBadRequest)
		return
	}

	pubkeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(req.PubKey, ".ed25519"))
	if err != nil || len(pubkeyBytes) != 32 {
		http.Error(w, `{"error":"invalid pubkey format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	peer, err := h.sbot.Connect(ctx, req.Address, pubkeyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"connect failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"peer":    peer.ID.String(),
		"address": peer.Conn.RemoteAddr().String(),
	})
}

func (h *clientUIHandler) handleAPIFollow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Feed      string `json:"feed"`
		Following *bool  `json:"following"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Feed == "" {
		http.Error(w, `{"error":"feed required"}`, http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Feed, "@") {
		req.Feed = "@" + req.Feed
	}

	following := true
	if req.Following != nil {
		following = *req.Following
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	content := map[string]interface{}{
		"type":      "contact",
		"contact":   req.Feed,
		"following": following,
		"blocking":  false,
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"key":       msgRef.String(),
		"feed":      req.Feed,
		"following": following,
	})
}

func (h *clientUIHandler) handleAPIReplication(w http.ResponseWriter, r *http.Request) {
	sm := h.sbot.StateMatrix()
	if sm == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
		return
	}

	matrix := sm.Export()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": true,
		"peers":   len(matrix),
		"matrix":  matrix,
	})
}

func (h *clientUIHandler) handleAPIConversations(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"feed":          whoami,
		"conversations": []string{},
		"note":          "DM storage requires database integration",
	})
}

func (h *clientUIHandler) handleAPIConversation(w http.ResponseWriter, r *http.Request) {
	feedID := chi.URLParam(r, "feed")
	if feedID == "" {
		http.Error(w, "feed required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"with":     feedID,
		"messages": []interface{}{},
		"note":     "DM storage requires database integration",
	})
}

func (h *clientUIHandler) handleAPISendDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipient string          `json:"recipient"`
		Content   json.RawMessage `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Recipient == "" || len(req.Content) == 0 {
		http.Error(w, "recipient and content required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "placeholder",
		"message":   "DM sending requires ssb-client with DM support compiled in",
		"recipient": req.Recipient,
	})
}

// ---------------------------------------------------------------------------
// CLI identity commands (offline — no server needed)
// ---------------------------------------------------------------------------

func runIdentityWhoami(c *cli.Context) error {
	secretPath := filepath.Join(repoPath, "secret")
	kp, err := keys.Load(secretPath)
	if err != nil {
		return fmt.Errorf("no identity found. run 'ssb-client identity create' first: %w", err)
	}
	fmt.Printf("%s\n", kp.FeedRef().String())
	return nil
}

func runIdentityCreate(c *cli.Context) error {
	if repoPath == "" {
		repoPath = defaultRepoPath
	}
	secretPath := filepath.Join(repoPath, "secret")

	if _, err := keys.Load(secretPath); err == nil {
		return fmt.Errorf("identity already exists. remove %s to create a new one", secretPath)
	}

	kp, err := keys.Generate()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}
	if err := keys.Save(kp, secretPath); err != nil {
		return fmt.Errorf("save keypair: %w", err)
	}

	fmt.Printf("Created new identity: %s\n", kp.FeedRef().String())
	fmt.Printf("Save the secret file at %s for backup!\n", secretPath)
	return nil
}

func runIdentityExport(c *cli.Context) error {
	secretPath := filepath.Join(repoPath, "secret")
	kp, err := keys.Load(secretPath)
	if err != nil {
		return fmt.Errorf("no identity found: %w", err)
	}

	fmt.Printf(`{
  "curve": "ed25519",
  "id": "%s",
  "private": "%s.ed25519",
  "public": "%s.ed25519"
}
`,
		kp.FeedRef().String(),
		keys.EncodePrivateKey(kp),
		keys.EncodePublicKey(kp),
	)
	return nil
}

func runIdentityImport(c *cli.Context) error {
	if repoPath == "" {
		repoPath = defaultRepoPath
	}
	secretPath := filepath.Join(repoPath, "secret")

	if _, err := keys.Load(secretPath); err == nil {
		return fmt.Errorf("identity already exists. remove %s to import a new one", secretPath)
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	kp, err := keys.ParseSecret(strings.NewReader(strings.TrimSpace(string(data))))
	if err != nil {
		return fmt.Errorf("import keypair: %w", err)
	}

	if err := keys.Save(kp, secretPath); err != nil {
		return fmt.Errorf("save keypair: %w", err)
	}

	fmt.Printf("Imported identity: %s\n", kp.FeedRef().String())
	return nil
}

// ---------------------------------------------------------------------------
// CLI client commands (hit running server's HTTP API)
// ---------------------------------------------------------------------------

func serverURL(path string) string {
	return fmt.Sprintf("http://%s%s", httpListenAddr, path)
}

func apiGet(path string) error {
	resp, err := http.Get(serverURL(path))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	// Pretty-print the JSON
	fmt.Println(prettyJSON(body))
	return nil
}

func apiPost(path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	resp, err := http.Post(serverURL(path), "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	fmt.Println(prettyJSON(body))
	return nil
}

func runState(c *cli.Context) error {
	return apiGet("/api/state")
}

func runFeeds(c *cli.Context) error {
	return apiGet("/api/feeds")
}

func runFeed(c *cli.Context) error {
	author := c.String("author")
	limit := c.Int("limit")
	msgType := c.String("type")

	if author != "" {
		path := fmt.Sprintf("/api/feed/%s?limit=%d", url.PathEscape(author), limit)
		if msgType != "" {
			path += "&type=" + url.QueryEscape(msgType)
		}
		return apiGet(path)
	}

	path := fmt.Sprintf("/api/messages?limit=%d", limit)
	if msgType != "" {
		path += "&type=" + url.QueryEscape(msgType)
	}
	return apiGet(path)
}

func runMessage(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: ssb-client message <feedId> <sequence>")
	}
	feedId := c.Args().Get(0)
	seq := c.Args().Get(1)
	return apiGet(fmt.Sprintf("/api/message/%s/%s", url.PathEscape(feedId), seq))
}

func runPublish(c *cli.Context) error {
	raw := c.String("raw")
	if raw != "" {
		var content map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		return apiPost("/api/publish", content)
	}

	content := map[string]interface{}{
		"type": c.String("type"),
	}
	if text := c.String("text"); text != "" {
		content["text"] = text
	}
	if contact := c.String("contact"); contact != "" {
		content["contact"] = contact
		if c.IsSet("following") {
			content["following"] = c.Bool("following")
		}
		if c.IsSet("blocking") {
			content["blocking"] = c.Bool("blocking")
		}
	}

	return apiPost("/api/publish", content)
}

func runRoomLogin(c *cli.Context) error {
	roomURL := c.Args().First()
	if roomURL == "" {
		return fmt.Errorf("missing room HTTP URL")
	}

	// For CLI commands, we might not have sbotInstance if we're not in 'serve' mode.
	// But SIP 6 requires a running MUXRPC connection.
	// So the user should run 'serve' first.
	if sbotInstance == nil {
		return fmt.Errorf("sbot not running (run serve first)")
	}

	// Register auth handler if not already registered
	sbotInstance.HandlerMux().Register(muxrpc.Method{"httpAuth"}, &clientAuthHandler{kp: sbotInstance.KeyPair})

	// Get cid and cc
	cid := sbotInstance.KeyPair.FeedRef().String()

	ccBytes := make([]byte, 32)
	if _, err := crypto_rand.Read(ccBytes); err != nil {
		return err
	}
	cc := base64.StdEncoding.EncodeToString(ccBytes)

	// Build URL
	u, err := url.Parse(roomURL)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(u.Path, "/login") {
		u.Path = path.Join(u.Path, "login")
	}
	q := u.Query()
	q.Set("ssb-http-auth", "1")
	q.Set("cid", cid)
	q.Set("cc", cc)
	u.RawQuery = q.Encode()

	fmt.Printf("Logging in to %s...\n", u.String())

	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed: %s (body: %s)", resp.Status, string(body))
	}

	// Check for session cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "auth_token" {
			fmt.Printf("Login successful! Session cookie received: %s\n", cookie.Value)
			return nil
		}
	}

	fmt.Println("Login request sent. Check if session was established.")
	return nil
}

func runPeersList(c *cli.Context) error {
	return apiGet("/api/peers")
}

func runPeersAdd(c *cli.Context) error {
	return runPeersConnect(c)
}

func runPeersConnect(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: ssb-client peers connect <address> <pubkey>")
	}
	return apiPost("/api/connect", map[string]string{
		"address": c.Args().Get(0),
		"pubkey":  c.Args().Get(1),
	})
}

func runReplication(c *cli.Context) error {
	return apiGet("/api/replication")
}
