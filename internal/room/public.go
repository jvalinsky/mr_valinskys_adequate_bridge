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
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/presentation"
	websecurity "github.com/mr_valinskys_adequate_bridge/internal/web/security"
	ssbrefs "github.com/ssbc/go-ssb-refs"
	"github.com/ssbc/go-ssb-room/v2/roomdb"
	roomweb "github.com/ssbc/go-ssb-room/v2/web"
)

// ActiveBridgeAccountLister exposes listing capabilities needed by the public room UI.
type ActiveBridgeAccountLister interface {
	ListActiveBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error)
}

// ActiveBridgeAccountDetailer exposes bot-detail capabilities needed by the public room UI.
type ActiveBridgeAccountDetailer interface {
	GetActiveBridgedAccountWithStats(ctx context.Context, atDID string) (*db.BridgedAccountStats, error)
	ListRecentPublishedMessagesByDID(ctx context.Context, atDID string, limit int) ([]db.Message, error)
}

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
	return websecurity.SecurityHeadersMiddleware(false)(inner)
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
	ref, err := ssbrefs.ParseFeedRef(strings.TrimSpace(feedID))
	if err != nil {
		return "", ""
	}

	feedURI := ref.URI()
	uri, err := url.Parse(feedURI)
	if err != nil {
		return feedURI, feedURI
	}

	return feedURI, roomweb.StringifySSBURI(uri, userAgent)
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

// feedIDSuffix returns the last few chars of a feed ID for display. It is used
// by the detail template heading but kept as a plain string method rather than a
// template func because it is called only once.
func feedIDSuffix(feed string) string {
	if len(feed) < 12 {
		return feed
	}
	return "…" + feed[len(feed)-10:]
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
      color-scheme: light;
      --paper: #f3ebdd;
      --panel: rgba(255, 250, 244, 0.96);
      --panel-strong: rgba(255, 253, 250, 0.98);
      --ink: #132820;
      --muted: #334239;
      --line: rgba(22, 49, 40, 0.18);
      --line-strong: rgba(22, 49, 40, 0.28);
      --accent: #0d7f64;
      --accent-strong: #0a5f4a;
      --signal: #c04e2e;
      --warn: #6f5300;
      --shadow: 0 24px 52px rgba(16, 38, 29, 0.12);
      --radius-lg: 28px;
      --radius-md: 18px;
      --radius-sm: 999px;
    }

    * {
      box-sizing: border-box;
    }

    body {
      margin: 0;
      min-height: 100vh;
      color: var(--ink);
      font-family: "Avenir Next", "Trebuchet MS", "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(13, 127, 100, 0.14), transparent 34%),
        radial-gradient(circle at top right, rgba(192, 78, 46, 0.12), transparent 28%),
        linear-gradient(180deg, #faf6ef 0%, var(--paper) 52%, #ebe3d2 100%);
    }

    a {
      color: inherit;
    }

    .page-shell {
      width: min(1180px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 24px 0 56px;
    }

    .topbar {
      display: flex;
      flex-wrap: wrap;
      justify-content: space-between;
      gap: 14px;
      align-items: center;
      margin-bottom: 28px;
    }

    .brand {
      display: flex;
      gap: 12px;
      align-items: center;
      font-weight: 700;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--muted);
      font-size: 0.82rem;
    }

    .brand-mark {
      width: 14px;
      height: 14px;
      border-radius: 50%;
      background: linear-gradient(135deg, var(--accent), var(--signal));
      box-shadow: 0 0 0 6px rgba(13, 127, 100, 0.08);
    }

    .topnav {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }

    .pill-link,
    .pill-link:visited {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 42px;
      padding: 0 16px;
      border-radius: var(--radius-sm);
      border: 1px solid var(--line-strong);
      background: rgba(255, 252, 247, 0.84);
      color: var(--ink);
      text-decoration: none;
      font-weight: 600;
    }

    .hero,
    .panel,
    .bot-card,
    .empty-state,
    .detail-hero,
    .message-card {
      background: var(--panel);
      border: 1px solid var(--line);
      box-shadow: var(--shadow);
      backdrop-filter: blur(8px);
    }

    .hero {
      display: grid;
      grid-template-columns: minmax(0, 1.6fr) minmax(260px, 0.9fr);
      gap: 26px;
      padding: 32px;
      border-radius: var(--radius-lg);
      margin-bottom: 28px;
    }

    .eyebrow {
      margin: 0 0 10px;
      font-size: 0.85rem;
      letter-spacing: 0.16em;
      text-transform: uppercase;
      color: var(--signal);
      font-weight: 700;
    }

    h1,
    h2 {
      margin: 0;
      font-family: "Iowan Old Style", "Palatino Linotype", "Book Antiqua", Georgia, serif;
      line-height: 1.04;
      letter-spacing: -0.03em;
    }

    h1 {
      font-size: clamp(2.4rem, 5vw, 4.4rem);
      max-width: 12ch;
    }

    h2 {
      font-size: clamp(1.7rem, 3vw, 2.6rem);
    }

    .page-title-wide {
      font-size: clamp(1.6rem, 3vw, 2.6rem);
      max-width: none;
    }

    .lead,
    .body-copy {
      color: var(--muted);
      line-height: 1.68;
      font-size: 1rem;
    }

    .lead {
      max-width: 58ch;
      margin: 18px 0 0;
    }

    .hero-actions,
    .bot-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-top: 22px;
    }

    .page-header-actions,
    .action-row-compact {
      margin-top: 0;
    }

    .button,
    .button:visited {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 46px;
      padding: 0 18px;
      border-radius: var(--radius-sm);
      border: 1px solid transparent;
      text-decoration: none;
      font-weight: 700;
      cursor: pointer;
      font: inherit;
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.45);
      transition: transform 120ms ease, background 120ms ease, border-color 120ms ease, box-shadow 120ms ease;
    }

    .button:hover {
      transform: translateY(-1px);
    }

    .button:focus-visible,
    .pill-link:focus-visible,
    .directory-field input:focus-visible,
    .directory-field select:focus-visible,
    .payload-toggle summary:focus-visible {
      outline: 2px solid var(--accent);
      outline-offset: 2px;
    }

    .button-primary {
      background: linear-gradient(135deg, var(--accent), var(--accent-strong));
      color: #fbfffd;
    }

    .button-secondary {
      background: rgba(255, 251, 246, 0.94);
      color: var(--ink);
      border-color: var(--line-strong);
    }

    .button-muted {
      background: rgba(51, 66, 57, 0.10);
      color: var(--muted);
      border-color: rgba(51, 66, 57, 0.10);
      cursor: default;
    }

    .button-small {
      min-height: 40px;
      padding: 0 14px;
      font-size: 0.95rem;
    }

    .status-panel {
      display: grid;
      gap: 14px;
      align-content: start;
      padding: 24px;
      border-radius: 22px;
      background: linear-gradient(180deg, rgba(17, 61, 49, 0.94), rgba(9, 33, 27, 0.94));
      color: #f7f4ed;
    }

    .status-panel p {
      margin: 0;
    }

    .status-kicker {
      color: rgba(255, 255, 255, 0.7);
      font-size: 0.8rem;
      font-weight: 700;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }

    .status-mode {
      font-size: 2rem;
      font-weight: 800;
      line-height: 1;
    }

    .detail-grid,
    .bot-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 20px;
    }

    .panel,
    .bot-card,
    .empty-state {
      border-radius: var(--radius-md);
      padding: 24px;
    }

    .panel h2,
    .bot-card h2,
    .empty-state h2 {
      margin-bottom: 10px;
    }

    .page-header {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 16px 24px;
      align-items: end;
      margin-bottom: 24px;
    }

    .page-header-main {
      min-width: 0;
    }

    .page-header-actions {
      justify-self: end;
      align-self: end;
    }

    .page-header .lead {
      margin: 16px 0 0;
      max-width: 64ch;
    }

    .directory-controls {
      margin-top: 6px;
      display: grid;
      gap: 14px 16px;
      grid-template-columns: minmax(0, 1.75fr) minmax(220px, 0.9fr) auto;
      align-items: end;
    }

    .directory-field {
      display: grid;
      gap: 7px;
      min-width: 0;
    }

    .directory-field label {
      font-size: 0.78rem;
      letter-spacing: 0.1em;
      text-transform: uppercase;
      color: var(--muted);
      font-weight: 700;
    }

    .directory-field input,
    .directory-field select {
      min-height: 46px;
      border-radius: 12px;
      border: 1px solid var(--line-strong);
      padding: 0 14px;
      font: inherit;
      color: var(--ink);
      background: var(--panel-strong);
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
    }

    .directory-field input::placeholder {
      color: rgba(51, 66, 57, 0.75);
    }

    .directory-actions {
      justify-self: end;
    }

    /* --- Enhanced Bot Card Styles --- */
    .bot-card {
      display: grid;
      gap: 16px;
      text-decoration: none;
      color: inherit;
      transition: transform 180ms ease, box-shadow 180ms ease, border-color 180ms ease;
      cursor: pointer;
      position: relative;
    }

    .bot-card.bot-card-alert {
      border-color: rgba(213, 93, 54, 0.42);
      box-shadow: 0 30px 62px rgba(213, 93, 54, 0.14);
    }

    .bot-alert-note {
      font-size: 0.78rem;
      font-weight: 700;
      color: var(--signal);
      letter-spacing: 0.03em;
      text-transform: uppercase;
    }

    a.bot-card:hover,
    a.bot-card:focus-visible {
      transform: translateY(-3px);
      box-shadow: 0 32px 68px rgba(16, 38, 29, 0.18);
      border-color: var(--accent);
      outline: none;
    }

    .bot-card-header {
      display: flex;
      align-items: flex-start;
      gap: 12px;
      min-width: 0;
    }

    .bot-pulse {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--accent);
      box-shadow: 0 0 0 0 rgba(13, 127, 100, 0.4);
      animation: pulse-ring 2s ease-out infinite;
      flex-shrink: 0;
    }

    @keyframes pulse-ring {
      0% { box-shadow: 0 0 0 0 rgba(13, 127, 100, 0.4); }
      70% { box-shadow: 0 0 0 8px rgba(13, 127, 100, 0); }
      100% { box-shadow: 0 0 0 0 rgba(13, 127, 100, 0); }
    }

    .bot-card-title {
      min-width: 0;
      flex: 1 1 auto;
      font-size: 1.05rem;
      font-weight: 700;
      font-family: "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      color: var(--ink);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .bot-card-subtitle {
      margin: 0;
      font-size: 0.84rem;
      color: var(--muted);
      overflow-wrap: anywhere;
    }

    .stats-bar {
      display: flex;
      flex-wrap: wrap;
      gap: 8px 10px;
      margin-top: 0;
    }

    .stat-pill {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      padding: 4px 10px;
      border-radius: 999px;
      border: 1px solid transparent;
      font-size: 0.82rem;
      font-weight: 700;
      letter-spacing: 0.02em;
    }

    .stat-total {
      background: rgba(22, 49, 40, 0.08);
      border-color: rgba(22, 49, 40, 0.12);
      color: var(--ink);
    }

    .stat-published {
      background: rgba(15, 139, 109, 0.12);
      border-color: rgba(15, 139, 109, 0.12);
      color: var(--accent-strong);
    }

    .stat-failed {
      background: rgba(213, 93, 54, 0.12);
      border-color: rgba(213, 93, 54, 0.14);
      color: var(--signal);
    }

    .stat-deferred {
      background: rgba(200, 160, 40, 0.18);
      border-color: rgba(144, 108, 0, 0.18);
      color: var(--warn);
    }

    .bot-meta {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 8px 14px;
      font-size: 0.84rem;
      color: var(--muted);
    }

    .bot-meta-item {
      min-width: 0;
      display: block;
      overflow-wrap: anywhere;
    }

    .bot-card .card-cta {
      font-size: 0.82rem;
      font-weight: 700;
      color: var(--accent);
      margin-top: 4px;
    }

    /* --- Detail Page --- */
    .detail-hero {
      display: grid;
      gap: 22px;
      padding: 32px;
      border-radius: var(--radius-lg);
      margin-bottom: 28px;
    }

    .detail-stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
      gap: 14px;
      margin-top: 4px;
    }

    .detail-stat-card {
      padding: 16px;
      border-radius: 14px;
      text-align: center;
    }

    .detail-stat-card.published { background: rgba(15, 139, 109, 0.10); }
    .detail-stat-card.failed { background: rgba(213, 93, 54, 0.10); }
    .detail-stat-card.deferred { background: rgba(200, 160, 40, 0.10); }
    .detail-stat-card.total { background: rgba(22, 49, 40, 0.06); }

    .detail-stat-value {
      display: block;
      font-size: 2rem;
      font-weight: 800;
      line-height: 1.1;
    }

    .detail-stat-label {
      display: block;
      font-size: 0.78rem;
      font-weight: 700;
      letter-spacing: 0.10em;
      text-transform: uppercase;
      color: var(--muted);
      margin-top: 4px;
    }

    .detail-stat-card.published .detail-stat-value { color: var(--accent-strong); }
    .detail-stat-card.failed .detail-stat-value { color: var(--signal); }
    .detail-stat-card.deferred .detail-stat-value { color: var(--warn); }
    .detail-stat-card.total .detail-stat-value { color: var(--ink); }

    .field-label {
      display: block;
      font-size: 0.78rem;
      letter-spacing: 0.12em;
      text-transform: uppercase;
      color: var(--muted);
      font-weight: 700;
      margin-bottom: 6px;
    }

    .mono {
      display: block;
      margin: 0;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid rgba(22, 49, 40, 0.08);
      background: rgba(255, 253, 250, 0.9);
      color: var(--ink);
      font-family: "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 0.92rem;
      line-height: 1.5;
      word-break: break-word;
    }

    .mono.uri {
      color: var(--accent-strong);
    }

    .empty-state {
      text-align: center;
    }

    .message-stream {
      display: grid;
      gap: 16px;
    }

    .message-card {
      border-radius: 18px;
      padding: 18px;
      display: grid;
      gap: 14px;
    }

    .message-card-header {
      display: flex;
      flex-wrap: wrap;
      justify-content: space-between;
      gap: 10px;
      align-items: start;
    }

    .message-type-pill {
      display: inline-flex;
      align-items: center;
      padding: 5px 10px;
      border-radius: 999px;
      background: rgba(15, 139, 109, 0.12);
      color: var(--accent-strong);
      font-size: 0.78rem;
      font-weight: 700;
      letter-spacing: 0.03em;
    }

    .message-meta {
      display: grid;
      gap: 10px;
    }

    .message-field-grid {
      display: grid;
      gap: 12px;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    }

    .message-field-card {
      border: 1px solid var(--line);
      border-radius: 14px;
      background: rgba(255, 252, 247, 0.85);
      padding: 12px 14px;
    }

    .message-field-card strong {
      display: block;
      font-size: 0.74rem;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      margin-bottom: 6px;
    }

    .message-field-card span {
      display: block;
      font-size: 0.92rem;
      line-height: 1.5;
      word-break: break-word;
    }

    .payload-toggle {
      border-top: 1px solid var(--line);
      padding-top: 12px;
    }

    .payload-toggle summary {
      cursor: pointer;
      font-weight: 700;
      color: var(--accent-strong);
    }

    .payload-grid {
      margin-top: 12px;
      display: grid;
      gap: 12px;
      grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    }

    .payload-block pre {
      margin: 8px 0 0;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: rgba(255, 253, 250, 0.88);
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 0.82rem;
      line-height: 1.5;
    }

    .footer-note {
      margin-top: 32px;
      padding-top: 18px;
      border-top: 1px solid rgba(22, 49, 40, 0.12);
      color: var(--muted);
      font-size: 0.94rem;
      line-height: 1.6;
    }

    @media (max-width: 900px) {
      .hero,
      .detail-grid,
      .bot-grid {
        grid-template-columns: 1fr;
      }

      .directory-controls {
        grid-template-columns: minmax(0, 1fr) minmax(220px, 0.75fr);
      }

      .directory-actions {
        grid-column: 1 / -1;
        justify-self: start;
      }

      .page-header {
        grid-template-columns: 1fr;
        align-items: start;
      }

      .page-header-actions {
        justify-self: start;
      }

      .page-shell {
        width: min(100vw - 20px, 1180px);
      }

      .hero,
      .panel,
      .empty-state,
      .detail-hero {
        padding: 20px;
      }

      .bot-card {
        padding: 16px;
      }
    }

    @media (max-width: 680px) {
      .directory-controls,
      .detail-stats-grid,
      .bot-meta {
        grid-template-columns: 1fr;
      }
    }
  </style>
</head>
<body>
  <div class="page-shell">
    <header class="topbar">
      <div class="brand">
        <span class="brand-mark" aria-hidden="true"></span>
        <span>ATProto to SSB Bridge Room</span>
      </div>
      <nav class="topnav">
        <a class="pill-link" href="/">Room</a>
        <a class="pill-link" href="/bots">Bots</a>
        <a class="pill-link" href="/login">Sign In</a>
      </nav>
    </header>
    {{template "content" .}}
    <p class="footer-note">Room invites, room sign-in, members, aliases, and admin pages are served by the embedded stock room web app. This public layer adds a bridge-specific landing page and bot directory on top.</p>
  </div>
  <script>
    document.addEventListener("click", async function (event) {
      var button = event.target.closest("[data-copy]");
      if (!button) return;
      event.preventDefault();
      event.stopPropagation();
      var value = button.getAttribute("data-copy");
      var originalLabel = button.textContent;
      try {
        if (navigator.clipboard && window.isSecureContext) {
          await navigator.clipboard.writeText(value);
        } else {
          var textArea = document.createElement("textarea");
          textArea.value = value;
          textArea.setAttribute("readonly", "");
          textArea.style.position = "absolute";
          textArea.style.left = "-9999px";
          document.body.appendChild(textArea);
          textArea.select();
          document.execCommand("copy");
          document.body.removeChild(textArea);
        }
        button.textContent = "Copied";
        window.setTimeout(function () {
          button.textContent = originalLabel;
        }, 1200);
      } catch (error) {
        window.prompt("Copy this value", value);
      }
    });
  </script>
</body>
</html>
`

const landingContentTemplate = `
{{define "pageTitle"}}Bridge Room{{end}}
{{define "content"}}
  <section class="hero">
    <div>
      <p class="eyebrow">Room access and discovery</p>
      <h1>Join the room, then follow the bridged-account bots.</h1>
      <p class="lead">Use the room's built-in invite flow to onboard an SSB peer, then browse the active bridged-account directory and open feed URIs in a compatible client.</p>
      <div class="hero-actions">
        {{if .Mode.CanSelfServeInvite}}
          <a class="button button-primary" href="{{.InviteURL}}">Create room invite</a>
        {{else}}
          <span class="button button-muted">Self-serve invites disabled</span>
        {{end}}
        <a class="button button-secondary" href="{{.BotsURL}}">Browse bridged bots</a>
        <a class="button button-secondary" href="{{.SignInURL}}">Open room sign-in</a>
      </div>
    </div>
    <aside class="status-panel">
      <p class="status-kicker">Current room mode</p>
      <p class="status-mode">{{.Mode.Label}}</p>
      <p class="body-copy">{{.Mode.Summary}}</p>
      <p class="body-copy">{{.BotCount}} active bridged bot{{if ne .BotCount 1}}s{{end}} currently listed in the directory.</p>
    </aside>
  </section>

  <section class="detail-grid">
    <article class="panel">
      <h2>Public invite flow</h2>
      <p class="body-copy">The invite button sends you into the stock room invite route at <span class="mono">/create-invite</span>. Invite consumption, fallback pages, and room membership are handled by the embedded room app without a second implementation here.</p>
    </article>
    <article class="panel">
      <h2>Bot directory</h2>
      <p class="body-copy">Each bridged DID has a deterministic SSB bot feed. The directory exposes the DID, the feed ID, a canonical <span class="mono">ssb:feed/...</span> URI, and copy actions so peers can follow from their preferred app.</p>
    </article>
  </section>
{{end}}
`

const botsContentTemplate = `
{{define "pageTitle"}}Bridged Bots{{end}}
{{define "content"}}
  <header class="page-header">
    <div class="page-header-main">
      <p class="eyebrow">Bridged-account directory</p>
      <h1>Follow the bridge bots from your SSB client.</h1>
      <p class="lead">These are the active bridged accounts currently published by the bridge runtime. Click a card for full details, or open the feed URI in a compatible app.</p>
    </div>
    <div class="hero-actions page-header-actions">
      <a class="button button-secondary button-small" href="{{.HomeURL}}">Back to room</a>
      {{if .Mode.CanSelfServeInvite}}
        <a class="button button-primary button-small" href="{{.InviteURL}}">Need an invite?</a>
      {{else}}
        <a class="button button-secondary button-small" href="{{.SignInURL}}">Room sign-in</a>
      {{end}}
    </div>
  </header>

  <form method="GET" action="/bots" class="panel">
    <div class="directory-controls">
      <div class="directory-field">
        <label for="bots-q">Search DID/feed</label>
        <input id="bots-q" type="search" name="q" value="{{.Query}}" placeholder="did:plc:..., @feed.ed25519, ssb:feed/...">
      </div>
      <div class="directory-field">
        <label for="bots-sort">Sort</label>
        <select id="bots-sort" name="sort">
          {{range .SortOptions}}
            <option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>
          {{end}}
        </select>
      </div>
      <div class="hero-actions action-row-compact directory-actions">
        <button class="button button-primary button-small" type="submit">Apply</button>
      </div>
    </div>
  </form>

  {{if .Bots}}
    <section class="bot-grid">
      {{range .Bots}}
        <a class="bot-card {{if or (gt .FailedMessages 0) (gt .DeferredMessages .PublishedMessages)}}bot-card-alert{{end}}" href="{{.DetailURL}}">
          <div class="bot-card-header">
            <span class="bot-pulse" aria-label="Active"></span>
            <span class="bot-card-title">{{abbreviateDID .ATDID}}</span>
          </div>
          <p class="bot-card-subtitle">{{abbreviateFeed .SSBFeedID}}</p>
          <div class="stats-bar">
            <span class="stat-pill stat-total">{{.TotalMessages}} msgs</span>
            {{if gt .PublishedMessages 0}}<span class="stat-pill stat-published">{{.PublishedMessages}} published</span>{{end}}
            {{if gt .FailedMessages 0}}<span class="stat-pill stat-failed">{{.FailedMessages}} failed</span>{{end}}
            {{if gt .DeferredMessages 0}}<span class="stat-pill stat-deferred">{{.DeferredMessages}} deferred</span>{{end}}
          </div>
          <div class="bot-meta">
            {{if .CreatedAt}}<span class="bot-meta-item">Bridged since {{.CreatedAt}}</span>{{end}}
            {{if .LastPublishedAt}}<span class="bot-meta-item">Last published {{.LastPublishedAt}}</span>{{end}}
          </div>
          {{if or (gt .FailedMessages 0) (gt .DeferredMessages .PublishedMessages)}}
            <span class="bot-alert-note">Needs triage attention</span>
          {{end}}
          <span class="card-cta">View details →</span>
        </a>
      {{end}}
    </section>
  {{else}}
    <section class="empty-state">
      <h2>No active bridged bots yet</h2>
      <p class="body-copy">Start the bridge with bridged accounts configured, or add accounts through the bridge CLI, and they will appear here automatically.</p>
    </section>
  {{end}}
{{end}}
`

const botDetailContentTemplate = `
{{define "pageTitle"}}Bot · {{abbreviateDID .Bot.ATDID}}{{end}}
{{define "content"}}
  <header class="page-header">
    <div class="page-header-main">
      <p class="eyebrow">Bot detail</p>
      <h1 class="page-title-wide">{{abbreviateDID .Bot.ATDID}}</h1>
    </div>
    <div class="hero-actions page-header-actions">
      <a class="button button-secondary button-small" href="{{.BotsURL}}">← Back to directory</a>
      {{if .Bot.FeedHref}}
        <a class="button button-primary button-small" href="{{.Bot.FeedHref}}">Open feed URI</a>
      {{end}}
    </div>
  </header>

  <section class="detail-hero">
    <div class="detail-stats-grid">
      <div class="detail-stat-card total">
        <span class="detail-stat-value">{{.Bot.TotalMessages}}</span>
        <span class="detail-stat-label">Total</span>
      </div>
      <div class="detail-stat-card published">
        <span class="detail-stat-value">{{.Bot.PublishedMessages}}</span>
        <span class="detail-stat-label">Published</span>
      </div>
      <div class="detail-stat-card failed">
        <span class="detail-stat-value">{{.Bot.FailedMessages}}</span>
        <span class="detail-stat-label">Failed</span>
      </div>
      <div class="detail-stat-card deferred">
        <span class="detail-stat-value">{{.Bot.DeferredMessages}}</span>
        <span class="detail-stat-label">Deferred</span>
      </div>
    </div>

    <div>
      <span class="field-label">ATProto DID</span>
      <p class="mono">{{.Bot.ATDID}}</p>
    </div>
    <div>
      <span class="field-label">SSB feed ID</span>
      <p class="mono">{{.Bot.SSBFeedID}}</p>
    </div>
    <div>
      <span class="field-label">Canonical feed URI</span>
      <p class="mono uri">{{if .Bot.FeedURI}}{{.Bot.FeedURI}}{{else}}Unavailable for this feed{{end}}</p>
    </div>

    <div class="bot-meta">
      {{if .Bot.CreatedAt}}<span class="bot-meta-item">Bridged since {{.Bot.CreatedAt}}</span>{{end}}
      {{if .Bot.LastPublishedAt}}<span class="bot-meta-item">Last published {{.Bot.LastPublishedAt}}</span>{{end}}
    </div>

    <div class="bot-actions">
      <button class="button button-secondary button-small" type="button" data-copy="{{.Bot.ATDID}}">Copy DID</button>
      <button class="button button-secondary button-small" type="button" data-copy="{{.Bot.SSBFeedID}}">Copy feed ID</button>
      {{if .Bot.FeedURI}}
        <button class="button button-secondary button-small" type="button" data-copy="{{.Bot.FeedURI}}">Copy feed URI</button>
      {{end}}
    </div>
  </section>

  <section class="panel">
    <div class="page-header">
      <div class="page-header-main">
        <p class="eyebrow">Recent bridged output</p>
        <h2>Published messages</h2>
        <p class="lead">Recent records this bot has already bridged into SSB, rendered from the stored ATProto and bridged SSB payloads.</p>
      </div>
    </div>

    {{if .PublishedMessages}}
      <div class="message-stream">
        {{range .PublishedMessages}}
          <article class="message-card">
            <div class="message-card-header">
              <div class="stats-bar">
                <span class="message-type-pill">{{.Type}}</span>
                {{if .PublishedAt}}<span class="stat-pill stat-published">Published {{.PublishedAt}}</span>{{end}}
              </div>
              <div class="bot-actions action-row-compact">
                <button class="button button-secondary button-small" type="button" data-copy="{{.ATURI}}">Copy AT URI</button>
                {{if .SSBMsgRef}}<button class="button button-secondary button-small" type="button" data-copy="{{.SSBMsgRef}}">Copy SSB ref</button>{{end}}
              </div>
            </div>

            <div class="message-meta">
              <div>
                <span class="field-label">AT URI</span>
                <p class="mono">{{.ATURI}}</p>
              </div>
              {{if .SSBMsgRef}}
                <div>
                  <span class="field-label">SSB message ref</span>
                  <p class="mono">{{.SSBMsgRef}}</p>
                </div>
              {{end}}
            </div>

            {{if .OriginalFields}}
              <div>
                <span class="field-label">Source record</span>
                <div class="message-field-grid">
                  {{range .OriginalFields}}
                    <div class="message-field-card">
                      <strong>{{.Label}}</strong>
                      <span>{{.Value}}</span>
                    </div>
                  {{end}}
                </div>
              </div>
            {{end}}

            {{if .BridgedFields}}
              <div>
                <span class="field-label">Bridged SSB message</span>
                <div class="message-field-grid">
                  {{range .BridgedFields}}
                    <div class="message-field-card">
                      <strong>{{.Label}}</strong>
                      <span>{{.Value}}</span>
                    </div>
                  {{end}}
                </div>
              </div>
            {{end}}

            {{if or .HasRawATProto .HasRawSSB}}
              <details class="payload-toggle">
                <summary>Show stored payloads</summary>
                <div class="payload-grid">
                  {{if .HasRawATProto}}
                    <div class="payload-block">
                      <span class="field-label">ATProto JSON</span>
                      <pre>{{.RawATProtoJSON}}</pre>
                    </div>
                  {{end}}
                  {{if .HasRawSSB}}
                    <div class="payload-block">
                      <span class="field-label">SSB JSON</span>
                      <pre>{{.RawSSBJSON}}</pre>
                    </div>
                  {{end}}
                </div>
              </details>
            {{end}}
          </article>
        {{end}}
      </div>
    {{else}}
      <section class="empty-state">
        <h2>No published messages yet</h2>
        <p class="body-copy">This bridged account is registered, but the bridge has not published any SSB messages for it yet.</p>
      </section>
    {{end}}
  </section>
{{end}}
`
