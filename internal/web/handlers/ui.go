// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/presentation"
	"github.com/mr_valinskys_adequate_bridge/internal/web/templates"
)

// UIHandler serves admin pages backed by bridge database state.
type UIHandler struct {
	db *db.DB
}

// NewUIHandler creates a UIHandler bound to database.
func NewUIHandler(database *db.DB) *UIHandler {
	return &UIHandler{db: database}
}

// Mount registers admin UI routes on r.
func (h *UIHandler) Mount(r chi.Router) {
	r.Get("/", h.handleDashboard)
	r.Get("/accounts", h.handleAccounts)
	r.Get("/messages", h.handleMessages)
	r.Get("/messages/detail", h.handleMessageDetail)
	r.Get("/failures", h.handleFailures)
	r.Get("/blobs", h.handleBlobs)
	r.Get("/state", h.handleState)
}

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	accountCount, err := h.db.CountBridgedAccounts(r.Context())
	if err != nil {
		http.Error(w, "Failed to get account count", http.StatusInternalServerError)
		return
	}

	messageCount, err := h.db.CountMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get message count", http.StatusInternalServerError)
		return
	}

	publishedCount, err := h.db.CountPublishedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get published count", http.StatusInternalServerError)
		return
	}

	publishFailureCount, err := h.db.CountPublishFailures(r.Context())
	if err != nil {
		http.Error(w, "Failed to get failure count", http.StatusInternalServerError)
		return
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deferred count", http.StatusInternalServerError)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deleted count", http.StatusInternalServerError)
		return
	}

	blobCount, err := h.db.CountBlobs(r.Context())
	if err != nil {
		http.Error(w, "Failed to get blob count", http.StatusInternalServerError)
		return
	}

	cursorValue, _, err := h.db.GetBridgeState(r.Context(), "firehose_seq")
	if err != nil {
		http.Error(w, "Failed to get cursor state", http.StatusInternalServerError)
		return
	}

	bridgeStatus, ok, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_status")
	if err != nil {
		http.Error(w, "Failed to get bridge status", http.StatusInternalServerError)
		return
	}
	if !ok || strings.TrimSpace(bridgeStatus) == "" {
		bridgeStatus = "unknown"
	}

	lastHeartbeat, _, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_last_heartbeat_at")
	if err != nil {
		http.Error(w, "Failed to get bridge heartbeat", http.StatusInternalServerError)
		return
	}

	healthLabel, healthDesc, healthTone := runtimeHealth(lastHeartbeat)

	reasonStats, err := h.db.ListTopDeferredReasons(r.Context(), 5)
	if err != nil {
		http.Error(w, "Failed to get deferred reason summary", http.StatusInternalServerError)
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
		http.Error(w, "Failed to get account issue summary", http.StatusInternalServerError)
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
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.ListBridgedAccountsWithStats(r.Context())
	if err != nil {
		http.Error(w, "Failed to get accounts", http.StatusInternalServerError)
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
		Chrome:   templates.PageChrome{ActiveNav: "accounts"},
		Accounts: rows,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	query := parseMessageListQuery(r)

	page, err := h.db.ListMessagesPage(r.Context(), query)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	recordTypes, err := h.db.ListMessageTypes(r.Context())
	if err != nil {
		http.Error(w, "Failed to get message types", http.StatusInternalServerError)
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
		Chrome:   templates.PageChrome{ActiveNav: "messages"},
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
		http.Error(w, "Template error", http.StatusInternalServerError)
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
		http.Error(w, "Failed to get message", http.StatusInternalServerError)
		return
	}
	if message == nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}

	data := templates.MessageDetailData{
		Chrome:                templates.PageChrome{ActiveNav: "messages"},
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
		FilterByDIDURL:        "/messages?did=" + url.QueryEscape(message.ATDID),
		FilterByStateURL:      "/messages?state=" + url.QueryEscape(message.MessageState),
		FilterByTypeURL:       "/messages?type=" + url.QueryEscape(message.Type),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessageDetail(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleFailures(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetPublishFailures(r.Context(), 300)
	if err != nil {
		http.Error(w, "Failed to get publish failures", http.StatusInternalServerError)
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
		Chrome:        templates.PageChrome{ActiveNav: "failures"},
		FailedRows:    failedRows,
		DeferredRows:  deferredRows,
		ReasonGroups:  reasonGroups,
		FailedCount:   len(failedRows),
		DeferredCount: len(deferredRows),
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleBlobs(w http.ResponseWriter, r *http.Request) {
	blobs, err := h.db.GetRecentBlobs(r.Context(), 200)
	if err != nil {
		http.Error(w, "Failed to get blobs", http.StatusInternalServerError)
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
		Chrome: templates.PageChrome{ActiveNav: "blobs"},
		Blobs:  rows,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetAllBridgeState(r.Context())
	if err != nil {
		http.Error(w, "Failed to get bridge state", http.StatusInternalServerError)
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
		http.Error(w, "Failed to get deferred count", http.StatusInternalServerError)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deleted count", http.StatusInternalServerError)
		return
	}

	latestReason, _, err := h.db.GetLatestDeferredReason(r.Context())
	if err != nil {
		http.Error(w, "Failed to get latest deferred reason", http.StatusInternalServerError)
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
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
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
