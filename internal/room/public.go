package room

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/presentation"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
)

type bridgeRoomHandler struct {
	stock             http.Handler
	roomConfig        roomdb.RoomConfig
	bridgeBotLister   ActiveBridgeAccountLister
	bridgeBotDetailer ActiveBridgeAccountDetailer
}

type roomModeStatus struct {
	Label              string
	Summary            string
	CanSelfServeInvite bool
}

type landingPageData struct {
	Mode      roomModeStatus
	InviteURL string
	BotsURL   string
	SignInURL string
	BotCount  int
}

type botPageData struct {
	Mode        roomModeStatus
	InviteURL   string
	HomeURL     string
	SignInURL   string
	Bots        []botCardData
	Query       string
	Sort        string
	SortOptions []botSortOption
}

type botSortOption struct {
	Value    string
	Label    string
	Selected bool
}

type botCardData struct {
	ATDID             string
	SSBFeedID         string
	FeedURI           string
	FeedHref          string
	DetailURL         string
	TotalMessages     int
	PublishedMessages int
	FailedMessages    int
	DeferredMessages  int
	LastPublishedAt   string
	CreatedAt         string
	CreatedUnix       int64
}

type botDetailPageData struct {
	Mode              roomModeStatus
	InviteURL         string
	HomeURL           string
	BotsURL           string
	SignInURL         string
	Bot               botCardData
	PublishedMessages []publishedMessageData
}

type publishedMessageData struct {
	ATURI          string
	SSBMsgRef      string
	Type           string
	PublishedAt    string
	OriginalFields []presentation.DetailField
	BridgedFields  []presentation.DetailField
	RawATProtoJSON string
	RawSSBJSON     string
	HasRawATProto  bool
	HasRawSSB      bool
}

var templateFuncs = template.FuncMap{
	"abbreviateDID":  abbreviateDID,
	"abbreviateFeed": abbreviateFeed,
}

var landingTemplate = template.Must(template.New("room-landing").Funcs(templateFuncs).Parse(publicLayoutTemplate + landingContentTemplate))
var botsTemplate = template.Must(template.New("room-bots").Funcs(templateFuncs).Parse(publicLayoutTemplate + botsContentTemplate))
var botDetailTemplate = template.Must(template.New("room-bot-detail").Funcs(templateFuncs).Parse(publicLayoutTemplate + botDetailContentTemplate))

func newBridgeRoomHandler(stock http.Handler, roomConfig roomdb.RoomConfig, bridgeBotLister ActiveBridgeAccountLister, bridgeBotDetailer ActiveBridgeAccountDetailer) http.Handler {
	if stock == nil {
		stock = http.NotFoundHandler()
	}
	inner := bridgeRoomHandler{
		stock:             stock,
		roomConfig:        roomConfig,
		bridgeBotLister:   bridgeBotLister,
		bridgeBotDetailer: bridgeBotDetailer,
	}
	return security.SecurityHeadersMiddleware(false)(inner)
}

func (h bridgeRoomHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("ok"))
		}
		return
	case r.URL.Path == "/create-invite" && inviteCreationMethod(r.Method):
		if h.modeStatus(r.Context()).CanSelfServeInvite {
			h.stock.ServeHTTP(w, r)
			return
		}
		h.handleInviteCreationUnavailable(w, r)
		return
	case r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.handleLanding(w, r)
		return
	case r.URL.Path == "/bots" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.handleBots(w, r)
		return
	case strings.HasPrefix(r.URL.Path, "/bots/") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.handleBotDetail(w, r)
		return
	default:
		h.stock.ServeHTTP(w, r)
	}
}

func inviteCreationMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost:
		return true
	default:
		return false
	}
}

func wantsJSONResponse(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json")
}

func (h bridgeRoomHandler) handleInviteCreationUnavailable(w http.ResponseWriter, r *http.Request) {
	if wantsJSONResponse(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "failed",
				"error":  "self-serve invite creation is only available when room mode is open",
			})
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func setPublicCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=30")
}

func (h bridgeRoomHandler) handleLanding(w http.ResponseWriter, r *http.Request) {
	bots, err := h.listActiveBotsWithStats(r.Context(), r.UserAgent())
	if err != nil {
		http.Error(w, "Failed to list bridged bots", http.StatusInternalServerError)
		return
	}

	data := landingPageData{
		Mode:      h.modeStatus(r.Context()),
		InviteURL: "/create-invite",
		BotsURL:   "/bots",
		SignInURL: "/login",
		BotCount:  len(bots),
	}

	setPublicCacheHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := landingTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h bridgeRoomHandler) handleBots(w http.ResponseWriter, r *http.Request) {
	bots, err := h.listActiveBotsWithStats(r.Context(), r.UserAgent())
	if err != nil {
		http.Error(w, "Failed to list bridged bots", http.StatusInternalServerError)
		return
	}

	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	sortMode := normalizeBotSort(r.URL.Query().Get("sort"))
	bots = filterAndSortBots(bots, searchQuery, sortMode)

	data := botPageData{
		Mode:        h.modeStatus(r.Context()),
		InviteURL:   "/create-invite",
		HomeURL:     "/",
		SignInURL:   "/login",
		Bots:        bots,
		Query:       searchQuery,
		Sort:        sortMode,
		SortOptions: buildBotSortOptions(sortMode),
	}

	setPublicCacheHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := botsTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h bridgeRoomHandler) handleBotDetail(w http.ResponseWriter, r *http.Request) {
	rawDID := strings.TrimPrefix(r.URL.Path, "/bots/")
	did, _ := url.PathUnescape(rawDID)
	did = strings.TrimSpace(did)
	if did == "" {
		http.Redirect(w, r, "/bots", http.StatusSeeOther)
		return
	}
	if _, err := syntax.ParseDID(did); err != nil {
		http.NotFound(w, r)
		return
	}

	if h.bridgeBotDetailer == nil {
		http.NotFound(w, r)
		return
	}

	acc, err := h.bridgeBotDetailer.GetActiveBridgedAccountWithStats(r.Context(), did)
	if err != nil {
		http.Error(w, "Failed to load bot details", http.StatusInternalServerError)
		return
	}
	if acc == nil {
		http.NotFound(w, r)
		return
	}

	bot := toBotCardData(*acc, r.UserAgent())
	published, err := h.bridgeBotDetailer.ListRecentPublishedMessagesByDID(r.Context(), did, 12)
	if err != nil {
		http.Error(w, "Failed to load bot messages", http.StatusInternalServerError)
		return
	}

	publishedCards := make([]publishedMessageData, 0, len(published))
	for _, message := range published {
		publishedCards = append(publishedCards, toPublishedMessageData(message))
	}

	data := botDetailPageData{
		Mode:              h.modeStatus(r.Context()),
		InviteURL:         "/create-invite",
		HomeURL:           "/",
		BotsURL:           "/bots",
		SignInURL:         "/login",
		Bot:               bot,
		PublishedMessages: publishedCards,
	}

	setPublicCacheHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := botDetailTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h bridgeRoomHandler) modeStatus(ctx context.Context) roomModeStatus {
	if h.roomConfig == nil {
		return roomModeStatus{
			Label:              "Unknown",
			Summary:            "Room mode could not be read. Self-serve invites may be unavailable.",
			CanSelfServeInvite: false,
		}
	}

	mode, err := h.roomConfig.GetPrivacyMode(ctx)
	if err != nil {
		return roomModeStatus{
			Label:              "Unknown",
			Summary:            "Room mode could not be read. Self-serve invites may be unavailable.",
			CanSelfServeInvite: false,
		}
	}

	switch mode {
	case roomdb.ModeOpen:
		return roomModeStatus{
			Label:              "Open",
			Summary:            "Anyone visiting this page can create a room invite and join through the stock room flow.",
			CanSelfServeInvite: true,
		}
	case roomdb.ModeCommunity:
		return roomModeStatus{
			Label:              "Community",
			Summary:            "Self-serve invites are disabled. Existing room members can sign in to create invites.",
			CanSelfServeInvite: false,
		}
	case roomdb.ModeRestricted:
		return roomModeStatus{
			Label:              "Restricted",
			Summary:            "Self-serve invites are disabled. Moderator or admin access is required to create invites.",
			CanSelfServeInvite: false,
		}
	default:
		return roomModeStatus{
			Label:              "Unknown",
			Summary:            "Room mode is unknown. Self-serve invites are disabled until the room policy is clear.",
			CanSelfServeInvite: false,
		}
	}
}

func (h bridgeRoomHandler) listActiveBotsWithStats(ctx context.Context, userAgent string) ([]botCardData, error) {
	if h.bridgeBotLister == nil {
		return nil, nil
	}

	accounts, err := h.bridgeBotLister.ListActiveBridgedAccountsWithStats(ctx)
	if err != nil {
		return nil, err
	}

	bots := make([]botCardData, 0, len(accounts))
	for _, account := range accounts {
		bots = append(bots, toBotCardData(account, userAgent))
	}
	return bots, nil
}

func normalizeBotSort(raw string) string {
	switch strings.TrimSpace(raw) {
	case "newest", "deferred_desc":
		return strings.TrimSpace(raw)
	default:
		return "activity_desc"
	}
}

func buildBotSortOptions(selected string) []botSortOption {
	return []botSortOption{
		{Value: "activity_desc", Label: "Most active", Selected: selected == "activity_desc"},
		{Value: "newest", Label: "Newest bridged", Selected: selected == "newest"},
		{Value: "deferred_desc", Label: "Most deferred", Selected: selected == "deferred_desc"},
	}
}

func filterAndSortBots(bots []botCardData, searchQuery string, sortMode string) []botCardData {
	searchQuery = strings.ToLower(strings.TrimSpace(searchQuery))
	filtered := make([]botCardData, 0, len(bots))
	for _, bot := range bots {
		if searchQuery == "" {
			filtered = append(filtered, bot)
			continue
		}
		haystack := strings.ToLower(bot.ATDID + " " + bot.SSBFeedID + " " + bot.FeedURI)
		if strings.Contains(haystack, searchQuery) {
			filtered = append(filtered, bot)
		}
	}

	sortMode = normalizeBotSort(sortMode)
	sort.SliceStable(filtered, func(i, j int) bool {
		left := filtered[i]
		right := filtered[j]

		switch sortMode {
		case "newest":
			return left.CreatedUnix > right.CreatedUnix
		case "deferred_desc":
			if left.DeferredMessages == right.DeferredMessages {
				if left.FailedMessages == right.FailedMessages {
					return left.TotalMessages > right.TotalMessages
				}
				return left.FailedMessages > right.FailedMessages
			}
			return left.DeferredMessages > right.DeferredMessages
		default:
			if left.TotalMessages == right.TotalMessages {
				return left.PublishedMessages > right.PublishedMessages
			}
			return left.TotalMessages > right.TotalMessages
		}
	})

	return filtered
}

func toBotCardData(acc db.BridgedAccountStats, userAgent string) botCardData {
	bot := botCardData{
		ATDID:             strings.TrimSpace(acc.ATDID),
		SSBFeedID:         strings.TrimSpace(acc.SSBFeedID),
		TotalMessages:     acc.TotalMessages,
		PublishedMessages: acc.PublishedMessages,
		FailedMessages:    acc.FailedMessages,
		DeferredMessages:  acc.DeferredMessages,
		DetailURL:         "/bots/" + url.PathEscape(strings.TrimSpace(acc.ATDID)),
		CreatedAt:         formatHumanTime(acc.CreatedAt),
		CreatedUnix:       acc.CreatedAt.Unix(),
	}

	if acc.LastPublishedAt != nil {
		bot.LastPublishedAt = formatHumanTime(*acc.LastPublishedAt)
	}

	feedURI, feedHref := feedLinks(bot.SSBFeedID, userAgent)
	bot.FeedURI = feedURI
	bot.FeedHref = feedHref
	return bot
}

func feedLinks(feedID, userAgent string) (string, string) {
	ref, err := refs.ParseFeedRef(strings.TrimSpace(feedID))
	if err != nil {
		return "", ""
	}

	feedURI := (&refs.FeedURI{Ref: ref}).String()
	feedHref := "ssb://" + ref.String()
	return feedURI, feedHref
}

func toPublishedMessageData(message db.Message) publishedMessageData {
	return publishedMessageData{
		ATURI:          strings.TrimSpace(message.ATURI),
		SSBMsgRef:      strings.TrimSpace(message.SSBMsgRef),
		Type:           strings.TrimSpace(message.Type),
		PublishedAt:    formatHumanTime(derefTime(message.PublishedAt, message.CreatedAt)),
		OriginalFields: presentation.SummarizeATProtoMessage(message),
		BridgedFields:  presentation.SummarizeSSBMessage(message),
		RawATProtoJSON: presentation.PrettyJSON(message.RawATJson),
		RawSSBJSON:     presentation.PrettyJSON(message.RawSSBJson),
		HasRawATProto:  strings.TrimSpace(message.RawATJson) != "",
		HasRawSSB:      strings.TrimSpace(message.RawSSBJson) != "",
	}
}

func derefTime(value *time.Time, fallback time.Time) time.Time {
	if value == nil || value.IsZero() {
		return fallback
	}
	return *value
}

func formatHumanTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2 Jan 2006, 15:04 UTC")
}

func abbreviateDID(did string) string {
	did = strings.TrimSpace(did)
	if len(did) <= 24 {
		return did
	}
	return did[:16] + "…" + did[len(did)-6:]
}

func abbreviateFeed(feed string) string {
	feed = strings.TrimSpace(feed)
	if len(feed) <= 20 {
		return feed
	}
	return feed[:12] + "…" + feed[len(feed)-8:]
}

const publicLayoutTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{template "pageTitle" .}}</title>
  <link rel="icon" type="image/png" sizes="32x32" href="/assets/favicon/favicon-32x32.png">
  <style>
    :root {
      --paper: #f3ebdd;
      --accent: #0d7f64;
    }
    body {
      font-family: system-ui, sans-serif;
      margin: 0;
      min-height: 100vh;
      background: var(--paper);
      color: #132820;
    }
    .page-shell {
      max-width: 1200px;
      margin: 0 auto;
      padding: 24px;
    }
    .topbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }
    .brand { font-weight: bold; }
    nav a {
      margin-left: 16px;
      color: var(--accent);
      text-decoration: none;
    }
    .hero, .panel, .bot-card, .page-title-wide {
      background: white;
      border-radius: 12px;
      padding: 24px;
      margin-bottom: 24px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
    }
    .page-header-main { margin-bottom: 24px; }
    .page-header-actions { margin-bottom: 24px; display: flex; gap: 8px; flex-wrap: wrap; }
    .action-row-compact { margin-bottom: 24px; display: flex; gap: 8px; flex-wrap: wrap; }
    .eyebrow { color: #666; font-size: 0.85em; text-transform: uppercase; letter-spacing: 0.05em; }
    .bot-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
      gap: 16px;
    }
    .stats-bar { margin-top: 12px; }
    .stat-pill {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 0.85em;
      margin-right: 8px;
    }
    .stat-total { background: #eee; }
    .stat-published { background: #d4edda; color: #155724; }
    .stat-failed { background: #f8d7da; color: #721c24; }
    .stat-deferred { background: #fff3cd; color: #856404; }
    a.bot-card { text-decoration: none; color: inherit; display: block; }
    a.bot-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,0.15); }
    .btn-primary, .btn-secondary, .btn-copy, .btn-small {
      padding: 8px 16px;
      border-radius: 6px;
      border: none;
      cursor: pointer;
      font-size: 0.9em;
    }
    .btn-primary { background: var(--accent); color: white; text-decoration: none; }
    .btn-secondary { background: #e0e0e0; color: #333; text-decoration: none; }
    .btn-copy { background: #f5f5f5; border: 1px solid #ddd; }
    .btn-small { background: var(--accent); color: white; font-size: 0.8em; }
    .directory-actions form { display: flex; gap: 8px; }
    .directory-actions input, .directory-actions select { padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
    .directory-actions button { padding: 8px 16px; background: var(--accent); color: white; border: none; border-radius: 4px; cursor: pointer; }
    .message-card { background: #f9f9f9; padding: 12px; margin-bottom: 12px; border-radius: 6px; }
    .raw-payload { background: #f5f5f5; padding: 16px; margin: 12px 0; border-radius: 6px; overflow-x: auto; }
    .raw-payload pre { margin: 0; font-size: 0.85em; }
    details.panel { background: white; border-radius: 12px; padding: 24px; margin-bottom: 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    details.panel summary { cursor: pointer; font-weight: bold; }
  </style>
</head>
<body>
  <div class="page-shell">
    <header class="topbar">
      <div class="brand">ATProto to SSB Bridge Room</div>
      <nav>
        <a href="/">Room</a>
        <a href="/bots">Bots</a>
        <a href="/login">Sign In</a>
      </nav>
    </header>
    {{template "content" .}}
  </div>
</body>
</html>
`

const landingContentTemplate = `
{{define "pageTitle"}}Bridge Room{{end}}
{{define "content"}}
  <section class="hero">
    {{if .Mode.CanSelfServeInvite}}
    <h1>Create room invite</h1>
    <p>Anyone visiting this page can create a room invite.</p>
    {{else}}
    <h1>Self-serve invites disabled</h1>
    <p>Existing room members can sign in to create invites.</p>
    {{end}}
    <p>Mode: {{.Mode.Label}}</p>
    <a href="/bots">Browse bridged bots</a>
    {{if .Mode.CanSelfServeInvite}}
    <a href="/create-invite">Open room sign-in</a>
    {{end}}
  </section>
  <section class="panel">
    <h2>{{.BotCount}} active bridged bot{{if ne .BotCount 1}}s{{end}} currently listed in the directory.</h2>
    <a href="/bots">Browse bridged bots</a>
  </section>
{{end}}
`

const botsContentTemplate = `
{{define "pageTitle"}}Bridged Bots{{end}}
{{define "content"}}
  <header class="page-header-main">
    <h1>Bridged Bots</h1>
  </header>
  <header class="page-header-actions directory-actions">
    <form method="get" action="/bots">
      <input type="search" name="q" placeholder="Search DID/feed" value="{{.Query}}">
      <select name="sort">
        <option value="activity_desc" {{if eq .Sort "activity_desc"}}selected{{end}}>Most active</option>
        <option value="newest" {{if eq .Sort "newest"}}selected{{end}}>Newest bridged</option>
        <option value="deferred_desc" {{if eq .Sort "deferred_desc"}}selected{{end}}>Most deferred</option>
      </select>
      <button type="submit">Search</button>
    </form>
  </header>
  <div class="action-row-compact directory-actions">
    {{if .Mode.CanSelfServeInvite}}
    <a href="/create-invite" class="btn-primary">Create room invite</a>
    {{end}}
  </div>
  {{if .Bots}}
    <div class="bot-grid">
      {{range .Bots}}
        <a class="bot-card" href="{{.DetailURL}}">
          <strong>{{abbreviateDID .ATDID}}</strong>
          <p>{{abbreviateFeed .SSBFeedID}}</p>
          <div class="stats-bar">
            <span class="stat-pill stat-total">{{.TotalMessages}} msgs</span>
            <span class="stat-pill stat-published">{{.PublishedMessages}} published</span>
            {{if gt .FailedMessages 0}}<span class="stat-pill stat-failed">{{.FailedMessages}} failed</span>{{end}}
            {{if gt .DeferredMessages 0}}<span class="stat-pill stat-deferred">{{.DeferredMessages}} deferred</span>{{end}}
          </div>
          <button class="btn-small">View details</button>
        </a>
      {{end}}
    </div>
  {{else}}
    <p>No active bridged bots yet.</p>
  {{end}}
{{end}}
`

const botDetailContentTemplate = `
{{define "pageTitle"}}Bot · {{abbreviateDID .Bot.ATDID}}{{end}}
{{define "content"}}
  <header class="page-header-main">
    <a href="/bots">← Back to directory</a>
    <p class="eyebrow">Bot detail</p>
    <h1>{{.Bot.ATDID}}</h1>
  </header>
  <header class="page-header-actions">
    <button class="btn-copy" data-copy="{{.Bot.ATDID}}">Copy DID</button>
    <button class="btn-copy" data-copy="{{.Bot.SSBFeedID}}">Copy feed ID</button>
    <button class="btn-copy" data-copy="{{.Bot.FeedURI}}">Copy feed URI</button>
    <a href="{{.Bot.FeedHref}}" class="btn-secondary">Open feed URI</a>
  </header>
  <div class="page-title-wide">
    <h2>SSB Feed</h2>
    <code>{{.Bot.SSBFeedID}}</code>
  </div>
  <div class="panel">
    <h2>Published messages</h2>
    {{if .PublishedMessages}}
    {{range .PublishedMessages}}
    <div class="message-card">
      <p>{{.Type}}</p>
      {{if .SSBMsgRef}}<code>{{.SSBMsgRef}}</code>{{end}}
    </div>
    {{end}}
    {{else}}
    <p>No published messages yet.</p>
    {{end}}
  </div>
  <details class="panel">
    <summary>Show stored payloads</summary>
    {{range .PublishedMessages}}
    {{if .HasRawATProto}}
    <div class="raw-payload">
      <h4>ATProto (source)</h4>
      <pre>{{.RawATProtoJSON}}</pre>
    </div>
    {{end}}
    {{if .HasRawSSB}}
    <div class="raw-payload">
      <h4>SSB (bridged)</h4>
      <pre>{{.RawSSBJSON}}</pre>
    </div>
    {{end}}
    {{end}}
  </details>
{{end}}
`
