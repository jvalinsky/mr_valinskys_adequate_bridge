package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

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
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()

	if h.index != nil {
		if err := h.ensureIndexSynced(); err != nil {
			h.slog.Warn("ui index sync failed in feed page", "error", err)
		} else {
			timelineModeValue := timelineMode(mode)
			if timelineModeValue == "" {
				switch {
				case filterAuthor != "":
					timelineModeValue = timelineModeProfile
				case channel != "":
					timelineModeValue = timelineModeChannel
				default:
					timelineModeValue = timelineModeNetwork
				}
			}

			items, err := h.index.queryTimeline(timelineQuery{
				Mode:     timelineModeValue,
				Author:   filterAuthor,
				Channel:  channel,
				Search:   searchQuery,
				Limit:    limit,
				SelfFeed: whoami,
			})
			if err == nil {
				var body strings.Builder
				fmt.Fprintf(&body, `<h1>Feed</h1>
<p><a href="/compose">Compose new post</a></p>
<div class="panel">
  <strong>Timeline Modes:</strong>
  <a href="/feed?mode=inbox">Inbox</a> ·
  <a href="/feed?mode=network">Network</a> ·
  <a href="/feed?mode=mentions">Mentions</a>
</div>
<form method="GET" action="/feed" class="panel">
  <div class="field"><label>Search</label><input type="text" name="q" value="%s" placeholder="Search text, author, raw JSON"></div>
  <div class="field"><label>Channel</label><input type="text" name="channel" value="%s" placeholder="channel name"></div>
  <div class="field"><label>Author</label><input type="text" name="author" value="%s" placeholder="@feed.ed25519"></div>
  <div class="field"><label>Mode</label><input type="text" name="mode" value="%s" placeholder="inbox|network|profile|channel|mentions"></div>
  <button type="submit">Apply Filters</button>
</form>
<p>Showing %d messages (mode=%s)</p>`,
					html.EscapeString(searchQuery),
					html.EscapeString(channel),
					html.EscapeString(filterAuthor),
					html.EscapeString(mode),
					len(items),
					html.EscapeString(string(timelineModeValue)))

				if len(items) == 0 {
					fmt.Fprintf(&body, `<div class="empty">No messages match the selected mode and filters.</div>`)
				}

				for _, item := range items {
					timestamp := time.Unix(item.TimestampMS/1000, 0).Format("2006-01-02 15:04:05")
					content := item.Text
					if item.IsPrivate && item.PrivateText != "" {
						content = item.PrivateText
					}

					actualType := item.Type
					if actualType == "" || actualType == "unknown" {
						actualType = extractTypeFromRawJSON(item.RawJSON)
					}
					typeClass := fmt.Sprintf("post-type-%s", actualType)
					badgeType := actualType
					if badgeType == "" {
						badgeType = "unknown"
					}

					authorShort := item.Author
					if len(authorShort) > 32 {
						authorShort = authorShort[:32] + "..."
					}

					postText := content
					if postText == "" {
						postText = extractTextFromRawJSON(item.RawJSON)
					}

					fmt.Fprintf(&body, `<div class="post %s">
  <div class="post-header">
    <span class="author" title="%s">%s</span>
    <span class="badge">%s</span>
    <span class="seq">seq=%d</span>
    <span class="timestamp">%s</span>
  </div>`,
						html.EscapeString(typeClass),
						html.EscapeString(item.Author),
						html.EscapeString(authorShort),
						html.EscapeString(badgeType),
						item.Sequence,
						html.EscapeString(timestamp))

					if postText != "" {
						escapedContent := html.EscapeString(postText)
						escapedContent = strings.ReplaceAll(escapedContent, "\n", "<br>")
						fmt.Fprintf(&body, `<div class="post-content">%s</div>`, escapedContent)
					} else {
						actionBody := renderMessageAction(item)
						fmt.Fprintf(&body, `<div class="post-content message-action">%s</div>`, actionBody)
					}

					fmt.Fprintf(&body, `<div class="post-actions">
    <a href="/message/%s/%d">Message Detail</a>
    <a href="/feed?author=%s&mode=profile">Author Feed</a>`,
						url.PathEscape(item.Author),
						item.Sequence,
						url.QueryEscape(item.Author))

					if item.Root != "" || item.Branch != "" {
						threadKey := item.Key
						if item.Root != "" {
							threadKey = item.Root
						}
						fmt.Fprintf(&body, ` · <a href="/api/thread/%s" target="_blank" rel="noopener">Thread API</a>`, url.PathEscape(threadKey))
					}

					fmt.Fprintf(&body, `</div><details><summary>Raw JSON</summary><pre>%s</pre></details></div>`,
						html.EscapeString(prettyJSONMust(item)))
				}

				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, htmlPage("Feed", body.String()))
				return
			}
			h.slog.Warn("timeline query failed in feed page; falling back to legacy feed", "error", err)
		}
	}

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
		escapedContent = strings.ReplaceAll(escapedContent, "\n", "<br>")
		authorDisplay := post.Author
		if len(authorDisplay) > 32 {
			authorDisplay = authorDisplay[:32] + "..."
		}

		actualType := post.Type
		if actualType == "" || actualType == "unknown" {
			actualType = extractTypeFromRawJSON(post.RawJSON)
		}
		typeClass := fmt.Sprintf("post-type-%s", actualType)
		badgeType := actualType
		if badgeType == "" {
			badgeType = "unknown"
		}

		fmt.Fprintf(&body, `<div class="post %s">
  <div class="post-header">
    <span class="author" title="%s">%s</span>
    <span class="badge">%s</span>
    <span class="seq">seq=%d</span>
    <span class="timestamp">%s</span>
  </div>`,
			html.EscapeString(typeClass),
			html.EscapeString(post.Author),
			html.EscapeString(authorDisplay),
			html.EscapeString(badgeType),
			post.Sequence,
			timestamp)

		if post.Content != "" {
			fmt.Fprintf(&body, `<div class="post-content">%s</div>`, escapedContent)
		} else {
			actionBody := renderLegacyMessageAction(post.Type, post.RawJSON)
			fmt.Fprintf(&body, `<div class="post-content message-action">%s</div>`, actionBody)
		}

		fmt.Fprintf(&body, `<div class="post-actions">
    <a href="/message/%s/%d">Message Detail</a>
    <a href="/feed?author=%s&mode=profile">Author Feed</a>
  </div>
  <details><summary>Raw JSON</summary><pre>%s</pre></details>
</div>`,
			url.PathEscape(post.Author),
			post.Sequence,
			url.QueryEscape(post.Author),
			html.EscapeString(post.RawJSON))
	}

	fmt.Fprintf(&body, `<div class="pagination">
  <a href="/feed?limit=25">25</a> <a href="/feed?limit=50">50</a>
  <a href="/feed?limit=100">100</a> <a href="/feed?limit=200">200</a>
</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Feed", body.String()))
}

func prettyJSONMust(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return prettyJSON(raw)
}

func renderMessageAction(item indexedMessage) string {
	msgType := item.Type
	if msgType == "" || msgType == "unknown" {
		msgType = extractTypeFromRawJSON(item.RawJSON)
	}

	switch msgType {
	case "post":
		if item.Text != "" {
			return html.EscapeString(item.Text)
		}
		return "Posted a message"
	case "follow":
		contact := item.Contact
		if contact == "" {
			return "Started following someone"
		}
		shortContact := contact
		if len(shortContact) > 32 {
			shortContact = shortContact[:32] + "..."
		}
		return fmt.Sprintf(`Started following <span class="contact-ref">%s</span>`, html.EscapeString(shortContact))
	case "unfollow":
		return "Unfollowed " + html.EscapeString(item.Contact)
	case "contact":
		if item.Following {
			return fmt.Sprintf(`Now following <span class="contact-ref">%s</span>`, html.EscapeString(item.Contact))
		} else if item.Blocking {
			return fmt.Sprintf(`Blocked <span class="contact-ref">%s</span>`, html.EscapeString(item.Contact))
		}
		return "Updated contact"
	case "like", "vote":
		if item.VoteValue == 1 {
			if item.VoteLink != "" {
				shortLink := item.VoteLink
				if len(shortLink) > 20 {
					shortLink = shortLink[:20] + "..."
				}
				return fmt.Sprintf(`Liked <span class="contact-ref">%s</span>`, html.EscapeString(shortLink))
			}
			return "Liked a message"
		} else if item.VoteValue == 0 {
			return "Removed like"
		}
		return fmt.Sprintf("Voted %d on a message", item.VoteValue)
	case "about":
		if item.Text != "" {
			return html.EscapeString(item.Text)
		}
		return "Updated profile"
	case "pub":
		return "Published server announcement"
	case "gist":
		return "Updated status"
	case "hz":
		return "HTTP overlay message"
	default:
		if msgType == "" {
			return "Message (no type specified)"
		}
		return fmt.Sprintf("Message type: %s", html.EscapeString(msgType))
	}
}

func extractTypeFromRawJSON(rawJSON string) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		return ""
	}
	if content, ok := parsed["content"].(map[string]interface{}); ok {
		if t, ok := content["type"].(string); ok {
			return t
		}
	}
	if t, ok := parsed["type"].(string); ok {
		return t
	}
	return ""
}

func extractTextFromRawJSON(rawJSON string) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		return ""
	}
	if content, ok := parsed["content"].(map[string]interface{}); ok {
		if text, ok := content["text"].(string); ok {
			return text
		}
	}
	return ""
}

func renderLegacyMessageAction(msgType string, rawJSON string) string {
	if (msgType == "" || msgType == "unknown") && rawJSON != "" {
		msgType = extractTypeFromRawJSON(rawJSON)
	}

	switch msgType {
	case "post":
		return "Posted a message"
	case "follow":
		return "Started following someone"
	case "unfollow":
		return "Unfollowed someone"
	case "contact":
		return "Updated contact"
	case "like", "vote":
		return "Liked a message"
	case "about":
		return "Updated profile"
	case "pub":
		return "Published server announcement"
	case "gist":
		return "Updated status"
	default:
		if msgType == "" {
			return "Message (no type)"
		}
		return fmt.Sprintf("Message type: %s", html.EscapeString(msgType))
	}
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
		queryEncodedID := url.QueryEscape(feedID)
		pathEncodedID := url.PathEscape(feedID)
		fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%d</td><td><a href="/feed?author=%s">View Feed</a> · <a href="/profile/%s">Profile</a></td></tr>`,
			escapedID, seq, queryEncodedID, pathEncodedID)
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
		channel := strings.TrimSpace(r.Form.Get("channel"))
		root := strings.TrimSpace(r.Form.Get("root"))
		branch := strings.TrimSpace(r.Form.Get("branch"))
		fork := strings.TrimSpace(r.Form.Get("fork"))
		if text != "" {
			pub, err := h.sbot.Publisher()
			if err != nil {
				h.slog.Error("failed to get publisher", "error", err)
				http.Redirect(w, r, "/compose?status="+url.QueryEscape("Failed to publish: publisher unavailable"), http.StatusSeeOther)
				return
			}
			content := map[string]interface{}{"type": "post", "text": text}
			if channel != "" {
				content["channel"] = channel
			}
			if root != "" {
				content["root"] = root
			}
			if branch != "" {
				content["branch"] = branch
			}
			if fork != "" {
				content["fork"] = fork
			}
			msgRef, err := pub.PublishJSON(content)
			if err != nil {
				h.slog.Error("failed to publish post", "error", err)
				http.Redirect(w, r, "/compose?status="+url.QueryEscape("Failed to publish: "+err.Error()), http.StatusSeeOther)
				return
			}
			h.slog.Info("published post", "ref", msgRef.String())
			http.Redirect(w, r, "/compose?status="+url.QueryEscape("Post published: "+msgRef.String()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/compose?status="+url.QueryEscape("Message text is required"), http.StatusSeeOther)
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	fork := strings.TrimSpace(r.URL.Query().Get("fork"))

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Compose Post</h1>`)
	if status != "" {
		fmt.Fprintf(&body, `<div class="panel"><strong>Status:</strong> %s</div>`, html.EscapeString(status))
	}
	fmt.Fprintf(&body, `<div class="section">
  <form method="POST" action="/compose">
    <div class="field"><label>Channel (optional)</label><input type="text" name="channel" value="%s" placeholder="e.g. ssb"></div>
    <div class="field"><label>Root Message Key (optional)</label><input type="text" name="root" value="%s" placeholder="%%...sha256"></div>
    <div class="field"><label>Branch Message Key (optional)</label><input type="text" name="branch" value="%s" placeholder="%%...sha256"></div>
    <div class="field"><label>Fork Message Key (optional)</label><input type="text" name="fork" value="%s" placeholder="%%...sha256"></div>
    <div class="field"><label>Message</label><textarea name="text" rows="4" placeholder="What's on your mind?"></textarea></div>
    <button type="submit">Post</button>
  </form>
</div>`,
		html.EscapeString(channel),
		html.EscapeString(root),
		html.EscapeString(branch),
		html.EscapeString(fork))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Compose", body.String()))
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
	} else if replicateFeed, ok := replicationTargetFromContact(content); ok {
		h.sbot.Replicate(replicateFeed)
	}

	http.Redirect(w, r, "/following", http.StatusSeeOther)
}

func (h *clientUIHandler) handleFollowers(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()
	if h.index == nil {
		body := `<h1>Followers</h1><div class="section"><p><em>Follower graph requires ui-index; restart with index enabled.</em></p></div>`
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, htmlPage("Followers", body))
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.slog.Warn("ui index sync failed in followers page", "error", err)
	}

	rel, err := h.index.queryFollowers(whoami)
	if err != nil {
		http.Error(w, "failed to load follower graph", http.StatusInternalServerError)
		return
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Followers (%d)</h1><div class="panel"><p>Computed from latest <code>contact</code> messages across known feeds.</p></div>`, len(rel.Followers))

	if len(rel.Followers) == 0 {
		fmt.Fprintf(&body, `<div class="empty">No followers detected yet.</div>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Follower Feed</th><th>Actions</th></tr>`)
		for _, feed := range rel.Followers {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td><a href="/feed?author=%s&mode=profile">View Feed</a> · <a href="/profile/%s">Profile</a></td></tr>`,
				html.EscapeString(feed),
				url.QueryEscape(feed),
				url.PathEscape(feed))
		}
		fmt.Fprintf(&body, `</table>`)
	}

	fmt.Fprintf(&body, `<h2>You Follow (%d)</h2>`, len(rel.Following))
	if len(rel.Following) == 0 {
		fmt.Fprintf(&body, `<div class="empty">You are not following anyone.</div>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Feed</th><th>Actions</th></tr>`)
		for _, feed := range rel.Following {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td><a href="/feed?author=%s&mode=profile">View Feed</a></td></tr>`,
				html.EscapeString(feed),
				url.QueryEscape(feed))
		}
		fmt.Fprintf(&body, `</table>`)
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Followers", body.String()))
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
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	statusClass := strings.TrimSpace(r.URL.Query().Get("class"))

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Connected Peers (%d)</h1>`, len(peers))
	if status != "" {
		cssClass := "info"
		if statusClass == "success" {
			cssClass = "success"
		} else if statusClass == "error" {
			cssClass = "error"
		}
		fmt.Fprintf(&body, `<div class="status %s">%s</div>`, cssClass, html.EscapeString(status))
	}

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
    <div class="field"><label>Multiserver URI</label><input type="text" name="multiserver" placeholder="net:host:port~shs:base64pubkey"></div>
    <div class="field"><label>Address (fallback)</label><input type="text" name="address" placeholder="host:port"></div>
    <div class="field"><label>Public Key (fallback)</label><input type="text" name="pubkey" placeholder="base64 pubkey (.ed25519 suffix optional)"></div>
    <button type="submit">Connect</button>
  </form>
</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Peers", body.String()))
}

func (h *clientUIHandler) handlePeersAdd(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	multiserver := strings.TrimSpace(r.Form.Get("multiserver"))
	address := strings.TrimSpace(r.Form.Get("address"))
	pubkeyStr := strings.TrimSpace(r.Form.Get("pubkey"))

	targetAddr, pubkeyDecoded, err := resolvePeerConnectTarget(multiserver, address, pubkeyStr)
	if err != nil {
		http.Redirect(w, r, "/peers?status="+url.QueryEscape("Connect failed: "+err.Error())+"&class=error", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if _, err = h.sbot.Connect(ctx, targetAddr, pubkeyDecoded); err != nil {
		h.slog.Error("failed to connect to peer", "address", targetAddr, "error", err)
		http.Redirect(w, r, "/peers?status="+url.QueryEscape("Connect failed: "+err.Error())+"&class=error", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/peers?status="+url.QueryEscape("Connected to "+targetAddr)+"&class=success", http.StatusSeeOther)
}

func (h *clientUIHandler) handleRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		action := strings.TrimSpace(r.Form.Get("action"))
		switch action {
		case "create_invite":
			token, err := h.createRoomInvite(r.Context())
			if err != nil {
				http.Redirect(w, r, "/room?status="+url.QueryEscape("Failed to create invite: "+err.Error())+"&class=error", http.StatusSeeOther)
				return
			}

			createdInvite := token
			if base := strings.TrimRight(strings.TrimSpace(h.sbot.RoomHTTPAddr()), "/"); base != "" {
				createdInvite = base + "/join?token=" + token
			}
			http.Redirect(w, r, "/room?status="+url.QueryEscape("Invite created")+"&class=success&createdInvite="+url.QueryEscape(createdInvite), http.StatusSeeOther)
			return
		default:
			inviteCode := strings.TrimSpace(r.Form.Get("invite"))
			if inviteCode == "" {
				http.Redirect(w, r, "/room?status="+url.QueryEscape("Invite URL is required")+"&class=error", http.StatusSeeOther)
				return
			}
			if err := h.consumeInvite(r.Context(), inviteCode); err != nil {
				h.slog.Error("failed to use invite code", "error", err)
				http.Redirect(w, r, "/room?status="+url.QueryEscape("Join failed: "+err.Error())+"&class=error", http.StatusSeeOther)
				return
			}
			h.slog.Info("successfully joined room using invite code")
			http.Redirect(w, r, "/room?status="+url.QueryEscape("Joined room successfully")+"&class=success", http.StatusSeeOther)
			return
		}
	}

	statusMsg := strings.TrimSpace(r.URL.Query().Get("status"))
	statusClass := strings.TrimSpace(r.URL.Query().Get("class"))
	createdInvite := strings.TrimSpace(r.URL.Query().Get("createdInvite"))

	peers := h.sbot.Peers()
	var roomPeers []string
	for _, p := range peers {
		roomPeers = append(roomPeers, p.ID.String())
	}
	sort.Strings(roomPeers)

	attendants := []string{}
	endpoints := []string{}
	if state := h.sbot.RoomState(); state != nil {
		for _, p := range state.Attendants() {
			attendants = append(attendants, p.ID.String())
		}
		for _, p := range state.Peers() {
			endpoints = append(endpoints, p.ID.String())
		}
		sort.Strings(attendants)
		sort.Strings(endpoints)
	}

	memberCount := 0
	activeInvites := 0
	totalInvites := 0
	privacyMode := "unknown"
	if roomDB := h.sbot.RoomDB(); roomDB != nil {
		ctx := r.Context()
		if members, err := roomDB.Members().List(ctx); err == nil {
			memberCount = len(members)
		}
		if count, err := roomDB.Invites().Count(ctx, true); err == nil {
			activeInvites = int(count)
		}
		if count, err := roomDB.Invites().Count(ctx, false); err == nil {
			totalInvites = int(count)
		}
		if mode, err := roomDB.RoomConfig().GetPrivacyMode(ctx); err == nil {
			switch mode {
			case roomdb.ModeOpen:
				privacyMode = "open"
			case roomdb.ModeCommunity:
				privacyMode = "community"
			case roomdb.ModeRestricted:
				privacyMode = "restricted"
			}
		}
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Room Connection</h1>`)
	if statusMsg != "" {
		cssClass := "info"
		if statusClass == "success" {
			cssClass = "success"
		} else if statusClass == "error" {
			cssClass = "error"
		}
		fmt.Fprintf(&body, `<div class="status %s">%s</div>`, cssClass, html.EscapeString(statusMsg))
	}
	if createdInvite != "" {
		fmt.Fprintf(&body, `<div class="panel"><strong>New Invite:</strong> <code>%s</code></div>`, html.EscapeString(createdInvite))
	}

	fmt.Fprintf(&body, `<div class="stat-grid">
  <div class="stat-card"><div class="value">%s</div><div class="label">Room Enabled</div></div>
  <div class="stat-card"><div class="value">%s</div><div class="label">Privacy Mode</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Connected Peers</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Attendants</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Room Members</div></div>
  <div class="stat-card"><div class="value">%d / %d</div><div class="label">Active / Total Invites</div></div>
</div>`,
		boolToYesNo(h.sbot.RoomEnabled()),
		html.EscapeString(privacyMode),
		len(peers),
		len(attendants),
		memberCount,
		activeInvites, totalInvites)

	fmt.Fprintf(&body, `<div class="panel">
  <h2>Join a Room</h2>
  <form method="POST" action="/room">
    <div class="field">
      <label for="invite">HTTP Invite URL</label>
      <input type="text" id="invite" name="invite" placeholder="http://room.example.com/join?token=xxx">
      <small style="color: var(--muted)">Paste a full HTTP invite URL from a Room2 server</small>
    </div>
    <input type="hidden" name="action" value="join">
    <button type="submit">Join Room</button>
  </form>
</div>

<div class="panel">
  <h2>Create Invite</h2>
  <form method="POST" action="/room">
    <input type="hidden" name="action" value="create_invite">
    <button type="submit">Create Room Invite</button>
  </form>
  <p><small>Invite token/URL is shown once after creation.</small></p>
</div>

<div class="panel">
  <h2>Tunnel Endpoints (%d)</h2>`,
		len(endpoints))

	if len(endpoints) == 0 {
		fmt.Fprintf(&body, `<p class="empty">No tunnel endpoints observed yet.</p>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Feed ID</th></tr>`)
		for _, feed := range endpoints {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td></tr>`, html.EscapeString(feed))
		}
		fmt.Fprintf(&body, `</table>`)
	}
	fmt.Fprintf(&body, `</div>

<div class="panel">
  <h2>Attendants (%d)</h2>`, len(attendants))
	if len(attendants) == 0 {
		fmt.Fprintf(&body, `<p class="empty">No attendants registered.</p>`)
	} else {
		fmt.Fprintf(&body, `<table><tr><th>Feed ID</th></tr>`)
		for _, feed := range attendants {
			fmt.Fprintf(&body, `<tr><td><code>%s</code></td></tr>`, html.EscapeString(feed))
		}
		fmt.Fprintf(&body, `</table>`)
	}

	fmt.Fprintf(&body, `</div>
<div class="panel">
  <h2>Room API</h2>
  <p><a href="/api/room/state" target="_blank" rel="noopener">/api/room/state</a> ·
  <a href="/api/room/invites" target="_blank" rel="noopener">/api/room/invites</a></p>
</div>`)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Room", body.String()))
}

func (h *clientUIHandler) createRoomInvite(ctx context.Context) (string, error) {
	roomDB := h.sbot.RoomDB()
	if roomDB == nil {
		return "", fmt.Errorf("room database is unavailable")
	}

	whoami, err := h.sbot.Whoami()
	if err != nil {
		return "", fmt.Errorf("load identity: %w", err)
	}
	feedRef, err := refs.ParseFeedRef(whoami)
	if err != nil {
		return "", fmt.Errorf("parse identity: %w", err)
	}

	createdBy := int64(0)
	members := roomDB.Members()
	if member, err := members.GetByFeed(ctx, *feedRef); err == nil {
		createdBy = member.ID
	} else {
		memberID, addErr := members.Add(ctx, *feedRef, roomdb.RoleMember)
		if addErr != nil {
			return "", fmt.Errorf("ensure room member: %w", addErr)
		}
		createdBy = memberID
	}

	token, err := roomDB.Invites().Create(ctx, createdBy)
	if err != nil {
		return "", fmt.Errorf("create invite: %w", err)
	}
	return token, nil
}

func (h *clientUIHandler) consumeInvite(ctx context.Context, inviteCode string) error {
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return fmt.Errorf("empty invite code")
	}

	token, roomHTTPAddr, err := resolveInviteConsumeTarget(inviteCode, h.sbot.RoomHTTPAddr())
	if err != nil {
		return err
	}
	h.slog.Info("consuming invite", "invite_url", inviteCode, "target_http_addr", roomHTTPAddr)

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
	httpReq.Header.Set("X-Forwarded-Proto", "https")

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

func resolveInviteConsumeTarget(inviteCode, configuredRoomHTTPAddr string) (token string, roomHTTPAddr string, err error) {
	parsedURL, err := url.Parse(strings.TrimSpace(inviteCode))
	if err != nil {
		return "", "", fmt.Errorf("invalid invite URL: %w", err)
	}
	if parsedURL.Host == "" {
		return "", "", fmt.Errorf("invite code must be a full URL (e.g., http://127.0.0.1:8976/join?token=xxx)")
	}

	if t := parsedURL.Query().Get("token"); t != "" {
		token = t
	} else if t := parsedURL.Query().Get("invite"); t != "" {
		token = t
	} else {
		return "", "", fmt.Errorf("no token found in invite URL")
	}

	if configured := strings.TrimRight(strings.TrimSpace(configuredRoomHTTPAddr), "/"); configured != "" {
		host := parsedURL.Hostname()
		if host == "localhost" || host == "127.0.0.1" {
			return token, configured, nil
		}
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if host == "localhost" || host == "127.0.0.1" {
		// In the container demo, the bridge is reachable by service name rather than loopback.
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

	return token, roomHTTPAddr, nil
}

func (h *clientUIHandler) connectToPeer(ctx context.Context, address string) error {
	hostPort, pubkey, err := parseMultiserverConnectAddress(address)
	if err != nil {
		return err
	}
	_, err = h.sbot.Connect(ctx, hostPort, pubkey)
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
				payload := map[string]interface{}{"type": "post", "text": message}
				msgRef, err := pub.PublishPrivate(payload, recipient)
				if err != nil {
					h.slog.Error("failed to send DM", "recipient", recipient, "error", err)
					statusMsg = fmt.Sprintf("Failed to send: %v", err)
					statusClass = "error"
				} else {
					if h.index != nil {
						_ = h.index.recordSentPrivate(msgRef.String(), normalizeFeed(recipient), message, nowMillis())
					}
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

	if h.index != nil {
		if err := h.ensureIndexSynced(); err == nil {
			if conversations, err := h.index.queryConversations(); err == nil {
				fmt.Fprintf(&body, `<div class="section"><h2>Conversation Inbox (%d)</h2>`, len(conversations))
				if len(conversations) == 0 {
					fmt.Fprintf(&body, `<p>No conversations yet.</p>`)
				} else {
					fmt.Fprintf(&body, `<table><tr><th>Peer</th><th>Messages</th><th>Last Activity</th><th>Actions</th></tr>`)
					for _, conv := range conversations {
						lastTs := time.Unix(conv.LastTs/1000, 0).Format("2006-01-02 15:04:05")
						fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%d</td><td>%s</td><td><a href="/api/conversations/%s" target="_blank" rel="noopener">Open API</a></td></tr>`,
							html.EscapeString(conv.Peer),
							conv.MessageCount,
							html.EscapeString(lastTs),
							url.PathEscape(conv.Peer))
					}
					fmt.Fprintf(&body, `</table>`)
				}
				fmt.Fprintf(&body, `</div>`)
			}
		}
	}

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

	if h.index != nil {
		if err := h.ensureIndexSynced(); err == nil {
			if thread, root, err := h.index.queryThread(msg.Key); err == nil && len(thread) > 0 {
				fmt.Fprintf(&body, `<div class="section">
  <h2>Thread Context</h2>
  <p>Root: <code>%s</code> · Messages: %d</p>
  <table><tr><th>Author</th><th>Type</th><th>Seq</th><th>Text</th><th>Root</th><th>Branch</th></tr>`,
					html.EscapeString(root),
					len(thread))
				for _, item := range thread {
					text := item.Text
					if text == "" {
						text = item.PrivateText
					}
					fmt.Fprintf(&body, `<tr><td><code>%s</code></td><td>%s</td><td>%d</td><td>%s</td><td><code>%s</code></td><td><code>%s</code></td></tr>`,
						html.EscapeString(item.Author),
						html.EscapeString(item.Type),
						item.Sequence,
						html.EscapeString(text),
						html.EscapeString(item.Root),
						html.EscapeString(item.Branch))
				}
				fmt.Fprintf(&body, `</table></div>`)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Message", body.String()))
}

func (h *clientUIHandler) handleReplication(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	statusClass := strings.TrimSpace(r.URL.Query().Get("class"))
	snapshot, err := collectReplicationSnapshot(h.sbot)
	if err != nil {
		http.Error(w, "failed to build replication snapshot", http.StatusInternalServerError)
		return
	}

	behind := 0
	for _, row := range snapshot.Rows {
		if row.Status == "behind" {
			behind++
		}
	}
	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Replication Status</h1>
<div class="stat-grid">
  <div class="stat-card"><div class="value">%s</div><div class="label">EBT Enabled</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Tracked Feeds</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Behind Feeds</div></div>
  <div class="stat-card"><div class="value">%d</div><div class="label">Matrix Peers</div></div>
</div>`,
		boolToYesNo(snapshot.Enabled),
		len(snapshot.Rows),
		behind,
		snapshot.MatrixPeers)

	if status != "" {
		cssClass := "info"
		if statusClass == "success" {
			cssClass = "success"
		} else if statusClass == "error" {
			cssClass = "error"
		}
		fmt.Fprintf(&body, `<div class="status %s">%s</div>`, cssClass, html.EscapeString(status))
	}

	if !snapshot.Enabled {
		fmt.Fprintf(&body, `<div class="panel">
  <h2>EBT Not Enabled</h2>
  <p>Enable EBT in your configuration for replicated feeds and state tracking.</p>
</div>`)
	}

	fmt.Fprintf(&body, `<div class="panel">
  <h2>Feed Replication Diagnostics</h2>
  <p>Use these controls to follow/unfollow feeds and force replication targeting.</p>
  <table><tr><th>Feed ID</th><th>Local Seq</th><th>Target Seq</th><th>Lag</th><th>Status</th><th>Receive</th><th>Last Update (UTC)</th><th>Actions</th></tr>`)

	for _, row := range snapshot.Rows {
		lastUpdate := "-"
		if row.LastUpdate != "" {
			lastUpdate = row.LastUpdate
		}
		fmt.Fprintf(&body, `<tr>
  <td><code>%s</code></td>
  <td>%d</td>
  <td>%d</td>
  <td>%d</td>
  <td><span class="badge %s">%s</span></td>
  <td>%s</td>
  <td>%s</td>
  <td>
    <form method="POST" action="/replication" style="display:inline">
      <input type="hidden" name="feed" value="%s">
      <input type="hidden" name="action" value="replicate">
      <button type="submit">Replicate</button>
    </form>
    <form method="POST" action="/replication" style="display:inline">
      <input type="hidden" name="feed" value="%s">
      <input type="hidden" name="action" value="follow">
      <button type="submit">Follow</button>
    </form>
    <form method="POST" action="/replication" style="display:inline">
      <input type="hidden" name="feed" value="%s">
      <input type="hidden" name="action" value="unfollow">
      <button type="submit">Unfollow</button>
    </form>
  </td>
</tr>`,
			html.EscapeString(row.FeedID),
			row.LocalSeq,
			row.FrontierSeq,
			row.Lag,
			replicationStatusBadgeClass(row.Status),
			html.EscapeString(row.Status),
			boolToYesNo(row.Receive),
			html.EscapeString(lastUpdate),
			html.EscapeString(row.FeedID),
			html.EscapeString(row.FeedID),
			html.EscapeString(row.FeedID))
	}
	fmt.Fprintf(&body, `</table></div>`)

	data, _ := json.MarshalIndent(snapshot.Matrix, "", "  ")
	fmt.Fprintf(&body, `<div class="panel">
  <h2>Raw EBT Matrix</h2>
  <details><summary>View JSON</summary><pre>%s</pre></details>
</div>`,
		html.EscapeString(string(data)))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Replication", body.String()))
}

func (h *clientUIHandler) handleReplicationAction(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := strings.ToLower(strings.TrimSpace(r.Form.Get("action")))
	feed := normalizeFeed(strings.TrimSpace(r.Form.Get("feed")))
	if feed == "" {
		http.Redirect(w, r, "/replication?status="+url.QueryEscape("Feed ID is required")+"&class=error", http.StatusSeeOther)
		return
	}

	switch action {
	case "replicate":
		h.sbot.Replicate(feed)
		http.Redirect(w, r, "/replication?status="+url.QueryEscape("Replication target added: "+feed)+"&class=success", http.StatusSeeOther)
		return
	case "follow", "unfollow":
		pub, err := h.sbot.Publisher()
		if err != nil {
			http.Redirect(w, r, "/replication?status="+url.QueryEscape("Publish failed: "+err.Error())+"&class=error", http.StatusSeeOther)
			return
		}
		following := action == "follow"
		content := map[string]interface{}{
			"type":      "contact",
			"contact":   feed,
			"following": following,
			"blocking":  false,
		}
		if _, err := pub.PublishJSON(content); err != nil {
			http.Redirect(w, r, "/replication?status="+url.QueryEscape("Publish failed: "+err.Error())+"&class=error", http.StatusSeeOther)
			return
		}
		if following {
			h.sbot.Replicate(feed)
		}
		msg := "Unfollowed " + feed
		if following {
			msg = "Followed " + feed
		}
		http.Redirect(w, r, "/replication?status="+url.QueryEscape(msg)+"&class=success", http.StatusSeeOther)
		return
	default:
		http.Redirect(w, r, "/replication?status="+url.QueryEscape("Unknown action: "+action)+"&class=error", http.StatusSeeOther)
		return
	}
}

func replicationStatusBadgeClass(status string) string {
	switch status {
	case "behind":
		return "warn"
	case "unfollowed":
		return "danger"
	case "in-sync":
		return "ok"
	default:
		return ""
	}
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
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	var body strings.Builder
	fmt.Fprintf(&body, `<h1>Settings</h1>`)
	if status != "" {
		fmt.Fprintf(&body, `<div class="panel"><strong>Status:</strong> %s</div>`, html.EscapeString(status))
	}

	fmt.Fprintf(&body, `<div class="section">
  <h2>Identity</h2>
  <p>Your feed ID: <code>%s</code></p>
  <p>Export your identity secret for backup.</p>
  <form method="POST" action="/settings/export">
    <button type="submit">Export Identity</button>
  </form>
</div>
<div class="section">
  <h2>Import Identity</h2>
  <p>Paste secret JSON (same format as export). Import writes to <code>%s</code>. Restart server after import.</p>
  <form method="POST" action="/settings/import">
    <div class="field"><label>Secret JSON</label><textarea name="secret_json" rows="6" placeholder="{...secret...}"></textarea></div>
    <button type="submit">Import Identity</button>
  </form>
</div>
<div class="section">
  <h2>Diagnostics</h2>
  <p><a href="/settings/diagnostics">Open Diagnostics JSON</a></p>
</div>
<div class="section">
  <h2>About</h2>
  <p>SSB Client - dev/testing tool for Mr. Valinsky's Adequate Bridge</p>
  <p>Version: 0.3.0-parity-wip</p>
</div>`,
		html.EscapeString(whoami),
		html.EscapeString(filepath.Join(repoPath, "secret")))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlPage("Settings", body.String()))
}

func (h *clientUIHandler) handleSettingsExport(w http.ResponseWriter, r *http.Request) {
	if h.keyPair == nil {
		http.Redirect(w, r, "/settings?status="+url.QueryEscape("identity is not loaded"), http.StatusSeeOther)
		return
	}

	payload := fmt.Sprintf(`{
  "curve": "ed25519",
  "id": "%s",
  "private": "%s.ed25519",
  "public": "%s.ed25519"
}
`,
		h.keyPair.FeedRef().String(),
		keys.EncodePrivateKey(h.keyPair),
		keys.EncodePublicKey(h.keyPair))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="ssb-identity-export.json"`)
	_, _ = io.WriteString(w, payload)
}

func (h *clientUIHandler) handleSettingsImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings?status="+url.QueryEscape("invalid import form"), http.StatusSeeOther)
		return
	}

	secretJSON := strings.TrimSpace(r.Form.Get("secret_json"))
	if secretJSON == "" {
		http.Redirect(w, r, "/settings?status="+url.QueryEscape("secret_json is required"), http.StatusSeeOther)
		return
	}

	kp, err := keys.ParseSecret(strings.NewReader(secretJSON))
	if err != nil {
		http.Redirect(w, r, "/settings?status="+url.QueryEscape("failed to parse secret: "+err.Error()), http.StatusSeeOther)
		return
	}

	secretPath := filepath.Join(repoPath, "secret")
	if err := keys.Save(kp, secretPath); err != nil {
		http.Redirect(w, r, "/settings?status="+url.QueryEscape("failed to save secret: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings?status="+url.QueryEscape("identity imported to "+secretPath+" (restart required)"), http.StatusSeeOther)
}

func (h *clientUIHandler) handleSettingsDiagnostics(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureIndexSynced(); err != nil {
		h.slog.Warn("ui index sync failed in diagnostics", "error", err)
	}
	whoami, _ := h.sbot.Whoami()
	peers := h.sbot.Peers()
	feeds, _ := h.sbot.Store().Logs().List()

	writeJSONResponse(w, map[string]interface{}{
		"identity": whoami,
		"repoPath": repoPath,
		"uptime":   time.Since(h.startTime).String(),
		"peers":    len(peers),
		"feeds":    len(feeds),
		"index": map[string]interface{}{
			"enabled": h.index != nil,
			"path":    h.indexPath,
		},
		"quickLinks": []string{
			"/api/state",
			"/api/capabilities",
			"/api/replication",
			"/api/timeline?mode=network&limit=25",
		},
	})
}
