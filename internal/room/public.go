package room

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/presentation"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
)

type bridgeRoomHandler struct {
	stock             http.Handler
	roomConfig        roomdb.RoomConfig
	bridgeBotLister   ActiveBridgeAccountLister
	bridgeBotDetailer ActiveBridgeAccountDetailer
	members           roomdb.MembersService
	authTokens        roomdb.AuthWithSSBService
}

type roomModeStatus struct {
	Label              string
	Summary            string
	CanSelfServeInvite bool
}

type landingPageData struct {
	ShowInvitesNav bool
	Mode           roomModeStatus
	InviteURL      string
	BotsURL        string
	SignInURL      string
	BotCount       int
}

type botPageData struct {
	ShowInvitesNav bool
	Mode           roomModeStatus
	InviteURL      string
	HomeURL        string
	SignInURL      string
	Bots           []botCardData
	Query          string
	Sort           string
	SortOptions    []botSortOption
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
	ShowInvitesNav    bool
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

func newBridgeRoomHandler(stock http.Handler, roomConfig roomdb.RoomConfig, bridgeBotLister ActiveBridgeAccountLister, bridgeBotDetailer ActiveBridgeAccountDetailer) http.Handler {
	return newBridgeRoomHandlerWithAuth(stock, roomConfig, bridgeBotLister, bridgeBotDetailer, nil, nil)
}

func newBridgeRoomHandlerWithAuth(
	stock http.Handler,
	roomConfig roomdb.RoomConfig,
	bridgeBotLister ActiveBridgeAccountLister,
	bridgeBotDetailer ActiveBridgeAccountDetailer,
	members roomdb.MembersService,
	authTokens roomdb.AuthWithSSBService,
) http.Handler {
	if stock == nil {
		stock = http.NotFoundHandler()
	}
	inner := bridgeRoomHandler{
		stock:             stock,
		roomConfig:        roomConfig,
		bridgeBotLister:   bridgeBotLister,
		bridgeBotDetailer: bridgeBotDetailer,
		members:           members,
		authTokens:        authTokens,
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
		ShowInvitesNav: h.showInvitesNav(r),
		Mode:           h.modeStatus(r.Context()),
		InviteURL:      "/create-invite",
		BotsURL:        "/bots",
		SignInURL:      "/login",
		BotCount:       len(bots),
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
		ShowInvitesNav: h.showInvitesNav(r),
		Mode:           h.modeStatus(r.Context()),
		InviteURL:      "/create-invite",
		HomeURL:        "/",
		SignInURL:      "/login",
		Bots:           bots,
		Query:          searchQuery,
		Sort:           sortMode,
		SortOptions:    buildBotSortOptions(sortMode),
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
		ShowInvitesNav:    h.showInvitesNav(r),
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

func (h bridgeRoomHandler) showInvitesNav(r *http.Request) bool {
	if h.roomConfig == nil {
		return false
	}

	mode, err := h.roomConfig.GetPrivacyMode(r.Context())
	if err != nil {
		return false
	}

	switch mode {
	case roomdb.ModeOpen:
		return true
	case roomdb.ModeCommunity:
		member, ok := h.memberFromRequest(r)
		return ok && isMemberOrHigher(member.Role)
	case roomdb.ModeRestricted:
		member, ok := h.memberFromRequest(r)
		return ok && isModeratorOrHigher(member.Role)
	default:
		return false
	}
}

func (h bridgeRoomHandler) memberFromRequest(r *http.Request) (roomdb.Member, bool) {
	if h.authTokens == nil || h.members == nil {
		return roomdb.Member{}, false
	}
	cookie, err := r.Cookie(authTokenCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return roomdb.Member{}, false
	}

	memberID, err := h.authTokens.CheckToken(r.Context(), cookie.Value)
	if err != nil {
		return roomdb.Member{}, false
	}

	member, err := h.members.GetByID(r.Context(), memberID)
	if err != nil {
		return roomdb.Member{}, false
	}
	return member, true
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
