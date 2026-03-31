// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/presentation"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

// Database defines the persistence surface required by UI handlers.
type Database interface {
	CheckBridgeHealth(ctx context.Context, maxStale time.Duration) (*db.BridgeHealthStatus, error)
	CountBridgedAccounts(ctx context.Context) (int, error)
	CountMessages(ctx context.Context) (int, error)
	CountPublishedMessages(ctx context.Context) (int, error)
	CountPublishFailures(ctx context.Context) (int, error)
	CountDeferredMessages(ctx context.Context) (int, error)
	CountDeletedMessages(ctx context.Context) (int, error)
	CountBlobs(ctx context.Context) (int, error)
	GetBridgeState(ctx context.Context, key string) (string, bool, error)
	ListTopDeferredReasons(ctx context.Context, limit int) ([]db.DeferredReasonCount, error)
	ListTopIssueAccounts(ctx context.Context, limit int) ([]db.AccountIssueSummary, error)
	ListBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error)
	ListMessagesPage(ctx context.Context, query db.MessageListQuery) (db.MessagePage, error)
	ListMessageTypes(ctx context.Context) ([]string, error)
	GetMessage(ctx context.Context, atURI string) (*db.Message, error)
	GetPublishFailures(ctx context.Context, limit int) ([]db.Message, error)
	GetRecentBlobs(ctx context.Context, limit int) ([]db.Blob, error)
	GetAllBridgeState(ctx context.Context) ([]db.BridgeState, error)
	GetKnownPeers(ctx context.Context) ([]db.KnownPeer, error)
	AddKnownPeer(ctx context.Context, p db.KnownPeer) error
	ResetMessageForRetry(ctx context.Context, atURI string) error
	GetBlobBySSBRef(ctx context.Context, ssbBlobRef string) (*db.Blob, error)
	GetLatestDeferredReason(ctx context.Context) (string, bool, error)
	GetBridgedAccount(ctx context.Context, atDID string) (*db.BridgedAccount, error)
	ListPublishedMessagesGlobal(ctx context.Context, limit int) ([]db.Message, error)
}

// BlobStore defines the blob retrieval surface for the UI.
type BlobStore interface {
	Get(hash []byte) (io.ReadCloser, error)
}

// SSBStatusProvider provides real-time status of the internal SSB node.
type SSBStatusProvider interface {
	GetPeers() []PeerStatus
	GetEBTState() map[string]map[string]int64
	ConnectPeer(ctx context.Context, addr string, pubKey []byte) error
}

type PeerStatus struct {
	Addr       string
	Feed       string
	ReadBytes  int64
	WriteBytes int64
	Latency    time.Duration
}

// UIHandler serves admin pages backed by bridge database state.
type UIHandler struct {
	db        Database
	logger    *log.Logger
	atpClient PDSClientInterface
	blobStore BlobStore
	ssbStatus SSBStatusProvider
}

// NewUIHandler creates a UIHandler bound to database.
func NewUIHandler(database Database, logger *log.Logger, atpClient PDSClientInterface, blobStore BlobStore, ssbStatus SSBStatusProvider) *UIHandler {
	return &UIHandler{
		db:        database,
		logger:    logutil.Ensure(logger),
		atpClient: atpClient,
		blobStore: blobStore,
		ssbStatus: ssbStatus,
	}
}

// Mount registers admin UI routes on r.
func (h *UIHandler) Mount(r chi.Router) {
	r.Get("/healthz", h.handleHealthz)
	r.Get("/", h.handleDashboard)
	r.Get("/accounts", h.handleAccounts)
	r.Get("/messages", h.handleMessages)
	r.Get("/messages/detail", h.handleMessageDetail)
	r.Get("/failures", h.handleFailures)
	r.Get("/blobs", h.handleBlobs)
	r.Get("/state", h.handleState)
	r.Post("/messages/retry", h.handleMessageRetry)
	r.Get("/post", h.handlePost)
	r.Post("/post", h.handlePostAction)
	r.Get("/feed", h.handleFeed)
	r.Get("/blobs/view", h.handleBlobView)
	r.Get("/connections", h.handleConnections)
	r.Post("/connections/add", h.handleConnectionAdd)
	r.Post("/connections/connect", h.handleConnectionConnect)
}

func (h *UIHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	var peers []PeerStatus
	var ebtState map[string]map[string]int64
	if h.ssbStatus != nil {
		peers = h.ssbStatus.GetPeers()
		ebtState = h.ssbStatus.GetEBTState()
	}

	knownPeers, err := h.db.GetKnownPeers(r.Context())
	if err != nil {
		h.writeInternalError(w, "handleConnections", "failed to load known peers", err)
		return
	}

	tplPeers := make([]templates.PeerStatus, 0, len(peers))
	for _, p := range peers {
		tplPeers = append(tplPeers, templates.PeerStatus{
			Addr:       p.Addr,
			Feed:       p.Feed,
			ReadBytes:  p.ReadBytes,
			WriteBytes: p.WriteBytes,
			Latency:    p.Latency,
		})
	}

	tplKnown := make([]templates.KnownPeer, 0, len(knownPeers))
	for _, p := range knownPeers {
		tplKnown = append(tplKnown, templates.KnownPeer{
			Addr:      p.Addr,
			PubKey:    base64.StdEncoding.EncodeToString(p.PubKey),
			CreatedAt: p.CreatedAt,
		})
	}

	data := templates.ConnectionsData{
		Chrome: templates.PageChrome{
			ActiveNav: "connections",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Admin", Href: "/"},
				{Label: "Connections"},
			},
		},
		Peers:      tplPeers,
		KnownPeers: tplKnown,
		EBTState:   ebtState,
	}

	if err := templates.RenderConnections(w, data); err != nil {
		h.writeInternalError(w, "handleConnections", "failed to render connections page", err)
	}
}

func (h *UIHandler) writeInternalError(w http.ResponseWriter, handler, message string, err error) {
	h.logger.Printf("event=handler_error handler=%s error=%v", handler, err)
	http.Error(w, message, http.StatusInternalServerError)
}

func (h *UIHandler) handleConnectionAdd(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.FormValue("addr"))
	pubkeyB64 := strings.TrimSpace(r.FormValue("pubkey"))

	if addr == "" || pubkeyB64 == "" {
		http.Error(w, "missing addr or pubkey", http.StatusBadRequest)
		return
	}

	// Pubkey should be base64
	pubkey, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		http.Error(w, "invalid pubkey base64", http.StatusBadRequest)
		return
	}

	if len(pubkey) != 32 {
		http.Error(w, "pubkey must be 32 bytes", http.StatusBadRequest)
		return
	}

	p := db.KnownPeer{
		Addr:   addr,
		PubKey: pubkey,
	}

	if err := h.db.AddKnownPeer(r.Context(), p); err != nil {
		h.writeInternalError(w, "handleConnectionAdd", "failed to save peer", err)
		return
	}

	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

func (h *UIHandler) handleConnectionConnect(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.FormValue("addr"))
	pubkeyB64 := strings.TrimSpace(r.FormValue("pubkey"))

	if addr == "" || pubkeyB64 == "" {
		http.Error(w, "missing addr or pubkey", http.StatusBadRequest)
		return
	}

	pubkey, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		http.Error(w, "invalid pubkey base64", http.StatusBadRequest)
		return
	}

	if h.ssbStatus == nil {
		http.Error(w, "ssb status provider not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.ssbStatus.ConnectPeer(r.Context(), addr, pubkey); err != nil {
		h.writeInternalError(w, "handleConnectionConnect", "failed to connect to peer", err)
		return
	}

	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

func (h *UIHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	health, err := h.db.CheckBridgeHealth(r.Context(), 60*time.Second)
	if err != nil {
		h.logger.Printf("event=handler_error handler=healthz error=%v", err)
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	if !health.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "unhealthy status=%s heartbeat=%s", health.Status, health.LastHeartbeat)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	accountCount, err := h.db.CountBridgedAccounts(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get account count", err)
		return
	}

	messageCount, err := h.db.CountMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get message count", err)
		return
	}

	publishedCount, err := h.db.CountPublishedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get published count", err)
		return
	}

	publishFailureCount, err := h.db.CountPublishFailures(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get failure count", err)
		return
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deferred count", err)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deleted count", err)
		return
	}

	blobCount, err := h.db.CountBlobs(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get blob count", err)
		return
	}

	cursorValue, _, err := h.db.GetBridgeState(r.Context(), "firehose_seq")
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get cursor state", err)
		return
	}

	bridgeStatus, ok, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_status")
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get bridge status", err)
		return
	}
	if !ok || strings.TrimSpace(bridgeStatus) == "" {
		bridgeStatus = "unknown"
	}

	lastHeartbeat, _, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_last_heartbeat_at")
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get bridge heartbeat", err)
		return
	}

	healthLabel, healthDesc, healthTone := runtimeHealth(lastHeartbeat)

	reasonStats, err := h.db.ListTopDeferredReasons(r.Context(), 5)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deferred reason summary", err)
		return
	}
	topReasons := make([]templates.DeferredReasonView, 0, len(reasonStats))
	for _, stat := range reasonStats {
		topReasons = append(topReasons, templates.DeferredReasonView{
			Reason:      stat.Reason,
			Count:       stat.Count,
			MessagesURL: "/messages?state=deferred&q=" + url.QueryEscape(stat.Reason),
		})
	}

	issueAccounts, err := h.db.ListTopIssueAccounts(r.Context(), 5)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get account issue summary", err)
		return
	}
	topAccounts := make([]templates.IssueAccountView, 0, len(issueAccounts))
	for _, acc := range issueAccounts {
		topAccounts = append(topAccounts, templates.IssueAccountView{
			ATDID:          acc.ATDID,
			Active:         acc.Active,
			TotalMessages:  acc.TotalMessages,
			IssueMessages:  acc.IssueMessages,
			FailedMessages: acc.FailedMessages,
			DeferredCount:  acc.DeferredCount,
			DeletedCount:   acc.DeletedCount,
			MessagesURL:    "/messages?did=" + url.QueryEscape(acc.ATDID) + "&has_issue=1",
		})
	}

	metrics := []templates.DashboardMetric{
		{Label: "Bridged Accounts", Value: accountCount, Tone: "neutral", Href: "/accounts", Note: "Open account roster"},
		{Label: "Messages Bridged", Value: messageCount, Tone: "neutral", Href: "/messages", Note: "Browse stream"},
		{Label: "Messages Published", Value: publishedCount, Tone: "success", Href: "/messages?state=published", Note: "Published state"},
		{Label: "Publish Failures", Value: publishFailureCount, Tone: "danger", Href: "/failures", Note: "Failed rows"},
		{Label: "Messages Deferred", Value: deferredCount, Tone: "warning", Href: "/messages?state=deferred", Note: "Deferred rows"},
		{Label: "Messages Deleted", Value: deletedCount, Tone: "neutral", Href: "/messages?state=deleted", Note: "Deleted tombstones"},
		{Label: "Blobs Bridged", Value: blobCount, Tone: "neutral", Href: "/blobs", Note: "Blob mappings"},
	}

	data := templates.DashboardData{
		Chrome: templates.PageChrome{
			ActiveNav: "dashboard",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: ""},
			},
			Status: templates.PageStatus{
				Visible: true,
				Tone:    healthTone,
				Title:   "Runtime health: " + healthLabel,
				Body:    healthDesc,
			},
		},
		Metrics:                  metrics,
		BridgeStatus:             bridgeStatus,
		LastHeartbeat:            lastHeartbeat,
		FirehoseCursor:           cursorValue,
		RuntimeHealth:            healthLabel,
		RuntimeHealthDescription: healthDesc,
		TopDeferredReasons:       topReasons,
		TopIssueAccounts:         topAccounts,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderDashboard(w, data); err != nil {
		h.writeInternalError(w, "dashboard", "Template error", err)
	}
}

func (h *UIHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.ListBridgedAccountsWithStats(r.Context())
	if err != nil {
		h.writeInternalError(w, "accounts", "Failed to get accounts", err)
		return
	}

	rows := make([]templates.AccountRow, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, templates.AccountRow{
			ATDID:             account.ATDID,
			SSBFeedID:         account.SSBFeedID,
			Active:            account.Active,
			TotalMessages:     account.TotalMessages,
			PublishedMessages: account.PublishedMessages,
			FailedMessages:    account.FailedMessages,
			DeferredMessages:  account.DeferredMessages,
			LastPublishedAt:   formatOptionalTime(account.LastPublishedAt),
			CreatedAt:         account.CreatedAt,
			MessagesURL:       "/messages?did=" + url.QueryEscape(account.ATDID),
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderAccounts(w, templates.AccountsData{
		Chrome: templates.PageChrome{
			ActiveNav: "accounts",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Accounts", Href: ""},
			},
		},
		Accounts: rows,
	}); err != nil {
		h.writeInternalError(w, "accounts", "Template error", err)
	}
}

func (h *UIHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	query := parseMessageListQuery(r)

	page, err := h.db.ListMessagesPage(r.Context(), query)
	if err != nil {
		h.writeInternalError(w, "messages", "Failed to get messages", err)
		return
	}

	recordTypes, err := h.db.ListMessageTypes(r.Context())
	if err != nil {
		h.writeInternalError(w, "messages", "Failed to get message types", err)
		return
	}

	rows := make([]templates.MessageRow, 0, len(page.Messages))
	for _, message := range page.Messages {
		issueText, issueClass := messageIssueSummary(message)
		rows = append(rows, templates.MessageRow{
			ATURI:           message.ATURI,
			ShortATURI:      truncateMiddle(message.ATURI, 66),
			DetailURL:       fmt.Sprintf("/messages/detail?at_uri=%s", url.QueryEscape(message.ATURI)),
			ATDID:           message.ATDID,
			ShortATDID:      truncateMiddle(message.ATDID, 44),
			Type:            message.Type,
			TypeLabel:       messageTypeLabel(message.Type),
			State:           message.MessageState,
			StateLabel:      messageStateLabel(message.MessageState),
			StateClass:      messageStateClass(message.MessageState),
			SSBMsgRef:       message.SSBMsgRef,
			ShortSSBMsgRef:  truncateMiddle(message.SSBMsgRef, 46),
			IssueText:       issueText,
			IssueClass:      issueClass,
			IssueDetail:     fullIssueText(message),
			PublishAttempts: message.PublishAttempts,
			DeferAttempts:   message.DeferAttempts,
			TotalAttempts:   message.PublishAttempts + message.DeferAttempts,
			CreatedAt:       message.CreatedAt,
		})
	}

	pagination := templates.MessagePagination{}
	if page.HasPrev {
		pagination.HasPrev = true
		pagination.PrevURL = buildMessagePageURL(query, page.PrevCursor, "prev")
	}
	if page.HasNext {
		pagination.HasNext = true
		pagination.NextURL = buildMessagePageURL(query, page.NextCursor, "next")
	}

	unsupportedKeysetSort := query.Sort != "newest" && query.Sort != "oldest"

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessages(w, templates.MessagesData{
		Chrome: templates.PageChrome{
			ActiveNav: "messages",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Messages", Href: ""},
			},
		},
		Messages: rows,
		Filters: templates.MessagesFilterState{
			Search:   query.Search,
			ATDID:    query.ATDID,
			Type:     query.Type,
			State:    query.State,
			Sort:     query.Sort,
			Limit:    query.Limit,
			HasIssue: query.HasIssue,
		},
		TypeOptions:           buildTypeOptions(recordTypes, query.Type),
		StateOptions:          buildStateOptions(query.State),
		SortOptions:           buildSortOptions(query.Sort),
		LimitOptions:          buildLimitOptions(query.Limit),
		ActiveFilters:         buildActiveMessageFilters(query),
		ResultCount:           len(rows),
		Pagination:            pagination,
		UnsupportedKeysetSort: unsupportedKeysetSort,
	}); err != nil {
		h.writeInternalError(w, "messages", "Template error", err)
	}
}

func (h *UIHandler) handleMessageDetail(w http.ResponseWriter, r *http.Request) {
	atURI := strings.TrimSpace(r.URL.Query().Get("at_uri"))
	if atURI == "" {
		http.Error(w, "Missing at_uri", http.StatusBadRequest)
		return
	}

	message, err := h.db.GetMessage(r.Context(), atURI)
	if err != nil {
		h.writeInternalError(w, "message_detail", "Failed to get message", err)
		return
	}
	if message == nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}

	data := templates.MessageDetailData{
		Chrome: templates.PageChrome{
			ActiveNav: "messages",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Messages", Href: "/messages"},
				{Label: "Message Detail", Href: ""},
			},
		},
		ATURI:                 message.ATURI,
		ATCID:                 message.ATCID,
		ATDID:                 message.ATDID,
		Type:                  message.Type,
		State:                 message.MessageState,
		SSBMsgRef:             message.SSBMsgRef,
		PublishAttempts:       message.PublishAttempts,
		DeferAttempts:         message.DeferAttempts,
		CreatedAt:             formatTime(message.CreatedAt),
		PublishedAt:           formatOptionalTime(message.PublishedAt),
		LastPublishAttemptAt:  formatOptionalTime(message.LastPublishAttemptAt),
		LastDeferAttemptAt:    formatOptionalTime(message.LastDeferAttemptAt),
		DeletedAt:             formatOptionalTime(message.DeletedAt),
		DeletedSeq:            formatOptionalSeq(message.DeletedSeq),
		PublishError:          message.PublishError,
		DeferReason:           message.DeferReason,
		DeletedReason:         message.DeletedReason,
		OriginalMessageFields: presentation.SummarizeATProtoMessage(*message),
		BridgedMessageFields:  presentation.SummarizeSSBMessage(*message),
		RawATProtoJSON:        presentation.PrettyJSON(message.RawATJson),
		RawSSBJSON:            presentation.PrettyJSON(message.RawSSBJson),
		RawWireFormat:         formatMuxRPCHex(message.RawSSBJson),
		ShowRawWire:           true,
		FilterByDIDURL:        "/messages?did=" + url.QueryEscape(message.ATDID),
		FilterByStateURL:      "/messages?state=" + url.QueryEscape(message.MessageState),
		FilterByTypeURL:       "/messages?type=" + url.QueryEscape(message.Type),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessageDetail(w, data); err != nil {
		h.writeInternalError(w, "message_detail", "Template error", err)
	}
}

func (h *UIHandler) handleFailures(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetPublishFailures(r.Context(), 300)
	if err != nil {
		h.writeInternalError(w, "failures", "Failed to get publish failures", err)
		return
	}

	failedRows := make([]templates.FailureRow, 0)
	deferredRows := make([]templates.FailureRow, 0)
	reasonGroupMap := make(map[string]*templates.FailureReasonGroup)

	for _, message := range messages {
		reason := issueReason(message)
		row := templates.FailureRow{
			ATURI:           message.ATURI,
			ATDID:           message.ATDID,
			Type:            message.Type,
			State:           message.MessageState,
			Reason:          reason,
			PublishAttempts: message.PublishAttempts,
			CreatedAt:       message.CreatedAt,
		}
		if message.MessageState == db.MessageStateDeferred {
			deferredRows = append(deferredRows, row)
		} else {
			failedRows = append(failedRows, row)
		}

		groupKey := message.MessageState + "\x00" + reason
		group, ok := reasonGroupMap[groupKey]
		if !ok {
			reasonGroupMap[groupKey] = &templates.FailureReasonGroup{
				State:  messageStateLabel(message.MessageState),
				Reason: reason,
				Count:  1,
			}
		} else {
			group.Count++
		}
	}

	reasonGroups := make([]templates.FailureReasonGroup, 0, len(reasonGroupMap))
	for _, group := range reasonGroupMap {
		reasonGroups = append(reasonGroups, *group)
	}
	sort.Slice(reasonGroups, func(i, j int) bool {
		if reasonGroups[i].Count == reasonGroups[j].Count {
			if reasonGroups[i].State == reasonGroups[j].State {
				return reasonGroups[i].Reason < reasonGroups[j].Reason
			}
			return reasonGroups[i].State < reasonGroups[j].State
		}
		return reasonGroups[i].Count > reasonGroups[j].Count
	})

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderFailures(w, templates.FailuresData{
		Chrome: templates.PageChrome{
			ActiveNav: "failures",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Failures", Href: ""},
			},
		},
		FailedRows:    failedRows,
		DeferredRows:  deferredRows,
		ReasonGroups:  reasonGroups,
		FailedCount:   len(failedRows),
		DeferredCount: len(deferredRows),
	}); err != nil {
		h.writeInternalError(w, "failures", "Template error", err)
	}
}

func (h *UIHandler) handleMessageRetry(w http.ResponseWriter, r *http.Request) {
	atURI := strings.TrimSpace(r.FormValue("at_uri"))
	if atURI == "" {
		http.Error(w, "Missing at_uri", http.StatusBadRequest)
		return
	}

	if err := h.db.ResetMessageForRetry(r.Context(), atURI); err != nil {
		h.writeInternalError(w, "message_retry", "Failed to reset message", err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/messages/detail?at_uri=%s", url.QueryEscape(atURI)), http.StatusSeeOther)
}

func (h *UIHandler) handleBlobs(w http.ResponseWriter, r *http.Request) {
	blobs, err := h.db.GetRecentBlobs(r.Context(), 200)
	if err != nil {
		h.writeInternalError(w, "blobs", "Failed to get blobs", err)
		return
	}

	rows := make([]templates.BlobRow, 0, len(blobs))
	for _, blob := range blobs {
		rows = append(rows, templates.BlobRow{
			ATCID:        blob.ATCID,
			SSBBlobRef:   blob.SSBBlobRef,
			Size:         blob.Size,
			MimeType:     blob.MimeType,
			DownloadedAt: blob.DownloadedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderBlobs(w, templates.BlobsData{
		Chrome: templates.PageChrome{
			ActiveNav: "blobs",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Blobs", Href: ""},
			},
		},
		Blobs: rows,
	}); err != nil {
		h.writeInternalError(w, "blobs", "Template error", err)
	}
}

func (h *UIHandler) handleBlobView(w http.ResponseWriter, r *http.Request) {
	refStr := r.URL.Query().Get("ref")
	if refStr == "" {
		http.Error(w, "Missing ref parameter", http.StatusBadRequest)
		return
	}

	blobMeta, err := h.db.GetBlobBySSBRef(r.Context(), refStr)
	if err != nil {
		h.writeInternalError(w, "blob_view", "Failed to lookup blob metadata", err)
		return
	}
	if blobMeta == nil {
		http.Error(w, "Blob metadata not found", http.StatusNotFound)
		return
	}

	if h.blobStore == nil {
		http.Error(w, "Blob store not configured", http.StatusServiceUnavailable)
		return
	}

	ref, err := refs.ParseBlobRef(refStr)
	if err != nil {
		http.Error(w, "Invalid SSB blob reference", http.StatusBadRequest)
		return
	}

	rc, err := h.blobStore.Get(ref.Hash())
	if err != nil {
		http.Error(w, "Blob data not found in store", http.StatusNotFound)
		return
	}
	defer rc.Close()

	if blobMeta.MimeType != "" {
		w.Header().Set("Content-Type", blobMeta.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.FormatInt(blobMeta.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	if _, err := io.Copy(w, rc); err != nil {
		h.logger.Printf("event=blob_serve_error ref=%s error=%v", refStr, err)
	}
}

func (h *UIHandler) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetAllBridgeState(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get bridge state", err)
		return
	}

	runtimeRows := make([]templates.StateRow, 0)
	firehoseRows := make([]templates.StateRow, 0)
	otherRows := make([]templates.StateRow, 0)
	heartbeatValue := ""

	for _, s := range state {
		row := templates.StateRow{Key: s.Key, Value: s.Value, UpdatedAt: s.UpdatedAt}
		switch {
		case strings.HasPrefix(s.Key, "bridge_runtime_"):
			runtimeRows = append(runtimeRows, row)
			if s.Key == "bridge_runtime_last_heartbeat_at" {
				heartbeatValue = s.Value
			}
		case strings.Contains(s.Key, "firehose"):
			firehoseRows = append(firehoseRows, row)
		default:
			otherRows = append(otherRows, row)
		}
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get deferred count", err)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get deleted count", err)
		return
	}

	latestReason, _, err := h.db.GetLatestDeferredReason(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get latest deferred reason", err)
		return
	}

	heartbeatStale, heartbeatAge := heartbeatFreshness(heartbeatValue)
	statusTone := "success"
	statusTitle := "State health: runtime heartbeat fresh"
	statusBody := "Runtime and firehose keys are grouped for faster incident inspection."
	if heartbeatStale {
		statusTone = "warning"
		statusTitle = "State health: heartbeat stale"
		statusBody = "Runtime heartbeat appears stale; verify bridge runtime and firehose connectivity."
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderState(w, templates.StateData{
		Chrome: templates.PageChrome{
			ActiveNav: "state",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "State", Href: ""},
			},
			Status: templates.PageStatus{
				Visible: true,
				Tone:    statusTone,
				Title:   statusTitle,
				Body:    statusBody,
			},
		},
		RuntimeState:      runtimeRows,
		FirehoseState:     firehoseRows,
		OtherState:        otherRows,
		DeferredCount:     deferredCount,
		DeletedCount:      deletedCount,
		LatestDeferReason: latestReason,
		HeartbeatStale:    heartbeatStale,
		HeartbeatAge:      heartbeatAge,
	}); err != nil {
		h.writeInternalError(w, "state", "Template error", err)
	}
}

func (h *UIHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.ListBridgedAccountsWithStats(r.Context())
	if err != nil {
		h.writeInternalError(w, "handlePost", "Failed to get accounts", err)
		return
	}

	var accountRows []templates.AccountRow
	for _, account := range accounts {
		accountRows = append(accountRows, templates.AccountRow{
			ATDID:             account.ATDID,
			SSBFeedID:         account.SSBFeedID,
			Active:            account.Active,
			TotalMessages:     account.TotalMessages,
			PublishedMessages: account.PublishedMessages,
			FailedMessages:    account.FailedMessages,
			DeferredMessages:  account.DeferredMessages,
			LastPublishedAt:   formatOptionalTime(account.LastPublishedAt),
			CreatedAt:         account.CreatedAt,
			MessagesURL:       "/messages?did=" + url.QueryEscape(account.ATDID),
		})
	}

	data := templates.PostData{
		Chrome: templates.PageChrome{
			ActiveNav: "post",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Compose Post", Href: ""},
			},
		},
		Accounts: accountRows,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderPost(w, data); err != nil {
		h.writeInternalError(w, "handlePost", "Template error", err)
	}
}

func (h *UIHandler) handlePostAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	atDID := strings.TrimSpace(r.FormValue("at_did"))
	text := strings.TrimSpace(r.FormValue("text"))

	if atDID == "" {
		http.Error(w, "Author DID is required", http.StatusBadRequest)
		return
	}

	if text == "" {
		http.Error(w, "Message text is required", http.StatusBadRequest)
		return
	}

	account, err := h.db.GetBridgedAccount(r.Context(), atDID)
	if err != nil {
		h.writeInternalError(w, "handlePostAction", "Failed to get bridged account", err)
		return
	}
	if account == nil || !account.Active {
		http.Error(w, "Invalid or inactive account", http.StatusBadRequest)
		return
	}

	var imageBlob *lexutil.LexBlob

	if len(r.MultipartForm.File["image"]) > 0 {
		fh := r.MultipartForm.File["image"][0]
		file, err := fh.Open()
		if err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to open uploaded file", err)
			return
		}
		defer file.Close()

		buffer := make([]byte, 512)
		_, err = file.Read(buffer)
		if err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to read uploaded file", err)
			return
		}

		file.Seek(0, io.SeekStart)
		mimeType := http.DetectContentType(buffer)

		if !strings.HasPrefix(mimeType, "image/") {
			http.Error(w, "Uploaded file must be an image", http.StatusBadRequest)
			return
		}

		blob, err := h.atpClient.UploadBlob(r.Context(), atDID, file, mimeType)
		if err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to upload blob", err)
			return
		}

		imageBlob = blob
	}

	postURI, err := h.atpClient.CreatePost(r.Context(), atDID, text, imageBlob)
	if err != nil {
		h.writeInternalError(w, "handlePostAction", "Failed to create post", err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/messages/detail?at_uri=%s", url.QueryEscape(postURI)), http.StatusSeeOther)
}

func (h *UIHandler) handleFeed(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	messages, err := h.db.ListPublishedMessagesGlobal(r.Context(), limit)
	if err != nil {
		h.writeInternalError(w, "handleFeed", "Failed to get global feed", err)
		return
	}

	rows := make([]templates.FeedRow, 0, len(messages))
	for _, msg := range messages {
		rows = append(rows, templates.FeedRow{
			ATURI:     msg.ATURI,
			ATDID:     msg.ATDID,
			Type:      msg.Type,
			CreatedAt: msg.CreatedAt,
			Text:      extractSSBText(msg.RawSSBJson),
			HasImage:  hasSSBImage(msg.RawSSBJson),
			ImageRef:  getSSBImageRef(msg.RawSSBJson),
		})
	}

	data := templates.FeedData{
		Chrome: templates.PageChrome{
			ActiveNav: "feed",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Global Feed", Href: ""},
			},
		},
		Feed: rows,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderFeed(w, data); err != nil {
		h.writeInternalError(w, "handleFeed", "Template error", err)
	}
}

func extractSSBText(rawJSON string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return ""
	}
	text, _ := m["text"].(string)
	if text == "" {
		// Check for legacy SSB content object
		if content, ok := m["content"].(map[string]interface{}); ok {
			text, _ = content["text"].(string)
		}
	}
	return text
}

func hasSSBImage(rawJSON string) bool {
	return getSSBImageRef(rawJSON) != ""
}

func getSSBImageRef(rawJSON string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return ""
	}
	content, _ := m["content"].(map[string]interface{})
	if content == nil {
		content = m // Flat format
	}

	mentions, _ := content["mentions"].([]interface{})
	for _, item := range mentions {
		mi, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		link, _ := mi["link"].(string)
		if strings.HasPrefix(link, "&") {
			return link
		}
	}
	return ""
}

func formatMuxRPCHex(rawJSON string) string {
	if strings.TrimSpace(rawJSON) == "" {
		return ""
	}
	body := []byte(rawJSON)
	l := uint32(len(body))

	header := make([]byte, 9)
	header[0] = 0x0a // Flag (JSON + Stream)
	binary.BigEndian.PutUint32(header[1:], l)
	binary.BigEndian.PutUint32(header[5:], 1) // ReqID=1

	full := append(header, body...)
	return hex.Dump(full)
}

func issueReason(message db.Message) string {
	if message.MessageState == db.MessageStateDeferred && strings.TrimSpace(message.DeferReason) != "" {
		return message.DeferReason
	}
	if message.MessageState == db.MessageStateDeleted && strings.TrimSpace(message.DeletedReason) != "" {
		return message.DeletedReason
	}
	if strings.TrimSpace(message.PublishError) != "" {
		return message.PublishError
	}
	if strings.TrimSpace(message.DeferReason) != "" {
		return message.DeferReason
	}
	if strings.TrimSpace(message.DeletedReason) != "" {
		return message.DeletedReason
	}
	return "(none)"
}

func fullIssueText(message db.Message) string {
	return strings.TrimSpace(issueReason(message))
}

func parseMessageListQuery(r *http.Request) db.MessageListQuery {
	values := r.URL.Query()
	return db.MessageListQuery{
		Search:    strings.TrimSpace(values.Get("q")),
		ATDID:     strings.TrimSpace(values.Get("did")),
		Type:      strings.TrimSpace(values.Get("type")),
		State:     sanitizeMessageState(values.Get("state")),
		Sort:      sanitizeMessageSort(values.Get("sort")),
		Limit:     parseMessageLimit(values.Get("limit")),
		HasIssue:  parseBoolFlag(values.Get("has_issue")),
		Cursor:    strings.TrimSpace(values.Get("cursor")),
		Direction: sanitizeMessageDirection(values.Get("dir")),
	}
}

func sanitizeMessageState(state string) string {
	switch strings.TrimSpace(state) {
	case "", db.MessageStatePending, db.MessageStatePublished, db.MessageStateFailed, db.MessageStateDeferred, db.MessageStateDeleted:
		return strings.TrimSpace(state)
	default:
		return ""
	}
}

func sanitizeMessageSort(sort string) string {
	switch strings.TrimSpace(sort) {
	case "oldest", "attempts_desc", "attempts_asc", "type_asc", "type_desc", "state_asc", "state_desc":
		return strings.TrimSpace(sort)
	default:
		return "newest"
	}
}

func sanitizeMessageDirection(direction string) string {
	switch strings.TrimSpace(direction) {
	case "prev":
		return "prev"
	default:
		return "next"
	}
}

func parseMessageLimit(raw string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 100
	}
	switch parsed {
	case 50, 100, 200, 500:
		return parsed
	default:
		return 100
	}
}

func parseBoolFlag(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func buildTypeOptions(recordTypes []string, selected string) []templates.FilterOption {
	options := []templates.FilterOption{{
		Value:    "",
		Label:    "All types",
		Selected: selected == "",
	}}

	seen := map[string]struct{}{}
	for _, recordType := range recordTypes {
		recordType = strings.TrimSpace(recordType)
		if recordType == "" {
			continue
		}
		if _, ok := seen[recordType]; ok {
			continue
		}
		seen[recordType] = struct{}{}
		options = append(options, templates.FilterOption{
			Value:    recordType,
			Label:    messageTypeLabel(recordType),
			Selected: recordType == selected,
		})
	}
	if selected != "" {
		if _, ok := seen[selected]; !ok {
			options = append(options, templates.FilterOption{
				Value:    selected,
				Label:    messageTypeLabel(selected),
				Selected: true,
			})
		}
	}
	return options
}

func buildStateOptions(selected string) []templates.FilterOption {
	return []templates.FilterOption{
		{Value: "", Label: "All states", Selected: selected == ""},
		{Value: db.MessageStatePending, Label: messageStateLabel(db.MessageStatePending), Selected: selected == db.MessageStatePending},
		{Value: db.MessageStatePublished, Label: messageStateLabel(db.MessageStatePublished), Selected: selected == db.MessageStatePublished},
		{Value: db.MessageStateDeferred, Label: messageStateLabel(db.MessageStateDeferred), Selected: selected == db.MessageStateDeferred},
		{Value: db.MessageStateFailed, Label: messageStateLabel(db.MessageStateFailed), Selected: selected == db.MessageStateFailed},
		{Value: db.MessageStateDeleted, Label: messageStateLabel(db.MessageStateDeleted), Selected: selected == db.MessageStateDeleted},
	}
}

func buildSortOptions(selected string) []templates.FilterOption {
	return []templates.FilterOption{
		{Value: "newest", Label: "Newest first", Selected: selected == "newest"},
		{Value: "oldest", Label: "Oldest first", Selected: selected == "oldest"},
		{Value: "attempts_desc", Label: "Most retries", Selected: selected == "attempts_desc"},
		{Value: "attempts_asc", Label: "Fewest retries", Selected: selected == "attempts_asc"},
		{Value: "type_asc", Label: "Type A-Z", Selected: selected == "type_asc"},
		{Value: "type_desc", Label: "Type Z-A", Selected: selected == "type_desc"},
		{Value: "state_asc", Label: "State A-Z", Selected: selected == "state_asc"},
		{Value: "state_desc", Label: "State Z-A", Selected: selected == "state_desc"},
	}
}

func buildLimitOptions(selected int) []templates.IntFilterOption {
	return []templates.IntFilterOption{
		{Value: 50, Label: "50", Selected: selected == 50},
		{Value: 100, Label: "100", Selected: selected == 100},
		{Value: 200, Label: "200", Selected: selected == 200},
		{Value: 500, Label: "500", Selected: selected == 500},
	}
}

func buildActiveMessageFilters(query db.MessageListQuery) []templates.ActiveFilter {
	var filters []templates.ActiveFilter
	if query.Search != "" {
		filters = append(filters, templates.ActiveFilter{Label: "Search", Value: query.Search})
	}
	if query.ATDID != "" {
		filters = append(filters, templates.ActiveFilter{Label: "DID", Value: query.ATDID})
	}
	if query.Type != "" {
		filters = append(filters, templates.ActiveFilter{Label: "Type", Value: messageTypeLabel(query.Type)})
	}
	if query.State != "" {
		filters = append(filters, templates.ActiveFilter{Label: "State", Value: messageStateLabel(query.State)})
	}
	if query.HasIssue {
		filters = append(filters, templates.ActiveFilter{Label: "Issue", Value: "Only rows with issues"})
	}
	return filters
}

func buildMessagePageURL(query db.MessageListQuery, cursor, direction string) string {
	values := url.Values{}
	if query.Search != "" {
		values.Set("q", query.Search)
	}
	if query.ATDID != "" {
		values.Set("did", query.ATDID)
	}
	if query.Type != "" {
		values.Set("type", query.Type)
	}
	if query.State != "" {
		values.Set("state", query.State)
	}
	if query.Sort != "" {
		values.Set("sort", query.Sort)
	}
	values.Set("limit", strconv.Itoa(query.Limit))
	if query.HasIssue {
		values.Set("has_issue", "1")
	}
	if strings.TrimSpace(cursor) != "" {
		values.Set("cursor", cursor)
	}
	values.Set("dir", direction)
	return "/messages?" + values.Encode()
}

func messageTypeLabel(recordType string) string {
	recordType = strings.TrimSpace(recordType)
	if recordType == "" {
		return "unknown"
	}
	parts := strings.Split(recordType, ".")
	return parts[len(parts)-1]
}

func messageStateLabel(state string) string {
	switch strings.TrimSpace(state) {
	case db.MessageStatePending:
		return "Pending"
	case db.MessageStatePublished:
		return "Published"
	case db.MessageStateFailed:
		return "Failed"
	case db.MessageStateDeferred:
		return "Deferred"
	case db.MessageStateDeleted:
		return "Deleted"
	default:
		return "Unknown"
	}
}

func messageStateClass(state string) string {
	switch strings.TrimSpace(state) {
	case db.MessageStatePublished:
		return "state-published"
	case db.MessageStateFailed:
		return "state-failed"
	case db.MessageStateDeferred:
		return "state-deferred"
	case db.MessageStateDeleted:
		return "state-deleted"
	default:
		return "state-pending"
	}
}

func messageIssueSummary(message db.Message) (string, string) {
	switch strings.TrimSpace(message.MessageState) {
	case db.MessageStateFailed:
		if strings.TrimSpace(message.PublishError) != "" {
			return compactIssueText(message.PublishError), ""
		}
	case db.MessageStateDeferred:
		if strings.TrimSpace(message.DeferReason) != "" {
			return summarizeDeferredIssue(message.DeferReason), "warning"
		}
	case db.MessageStateDeleted:
		if strings.TrimSpace(message.DeletedReason) != "" {
			return compactIssueText(message.DeletedReason), "muted"
		}
	}
	switch {
	case strings.TrimSpace(message.PublishError) != "":
		return compactIssueText(message.PublishError), ""
	case strings.TrimSpace(message.DeferReason) != "":
		return summarizeDeferredIssue(message.DeferReason), "warning"
	case strings.TrimSpace(message.DeletedReason) != "":
		return compactIssueText(message.DeletedReason), "muted"
	default:
		return "No issue", "muted"
	}
}

func summarizeDeferredIssue(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "Deferred"
	}

	switch {
	case strings.Contains(reason, "_atproto_reply_root=") || strings.Contains(reason, "_atproto_reply_parent="):
		return "Waiting on reply target bridge"
	case strings.Contains(reason, "_atproto_contact="):
		return "Waiting on contact bridge"
	case strings.Contains(reason, "_atproto_subject="):
		return "Waiting on subject bridge"
	case strings.Contains(reason, "_atproto_quote_subject="):
		return "Waiting on quoted post bridge"
	case strings.Contains(reason, "_atproto_about_did="):
		return "Waiting on author feed bridge"
	default:
		return compactIssueText(reason)
	}
}

func compactIssueText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return "No issue"
	}
	if len(text) <= 88 {
		return text
	}
	return text[:85] + "..."
}

func runtimeHealth(lastHeartbeat string) (label string, description string, tone string) {
	parsed, ok := parseTimestampString(lastHeartbeat)
	if !ok {
		return "unknown", "No runtime heartbeat timestamp has been recorded yet.", "neutral"
	}
	age := time.Since(parsed)
	if age <= 90*time.Second {
		return "healthy", fmt.Sprintf("Heartbeat %s ago.", humanizeDuration(age)), "success"
	}
	return "stale", fmt.Sprintf("Heartbeat %s ago; runtime may be delayed or stopped.", humanizeDuration(age)), "warning"
}

func heartbeatFreshness(lastHeartbeat string) (stale bool, ageLabel string) {
	parsed, ok := parseTimestampString(lastHeartbeat)
	if !ok {
		return true, "unknown"
	}
	age := time.Since(parsed)
	return age > 90*time.Second, humanizeDuration(age) + " ago"
}

func parseTimestampString(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func truncateMiddle(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max < 8 {
		return value[:max]
	}
	head := (max - 1) / 2
	tail := max - head - 1
	return value[:head] + "…" + value[len(value)-tail:]
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatOptionalTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatOptionalSeq(seq *int64) string {
	if seq == nil {
		return ""
	}
	return strconv.FormatInt(*seq, 10)
}
