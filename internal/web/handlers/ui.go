// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/web/templates"
)

// UIHandler serves admin pages backed by bridge database state.
type UIHandler struct {
	db *db.DB
}

// NewUIHandler creates a UIHandler bound to database.
func NewUIHandler(database *db.DB) *UIHandler {
	return &UIHandler{
		db: database,
	}
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
	if !ok || bridgeStatus == "" {
		bridgeStatus = "unknown"
	}
	lastHeartbeat, _, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_last_heartbeat_at")
	if err != nil {
		http.Error(w, "Failed to get bridge heartbeat", http.StatusInternalServerError)
		return
	}

	data := templates.DashboardData{
		AccountCount:        accountCount,
		MessageCount:        messageCount,
		PublishedCount:      publishedCount,
		PublishFailureCount: publishFailureCount,
		DeferredCount:       deferredCount,
		DeletedCount:        deletedCount,
		BlobCount:           blobCount,
		FirehoseCursor:      cursorValue,
		BridgeStatus:        bridgeStatus,
		LastHeartbeat:       lastHeartbeat,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderDashboard(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.GetAllBridgedAccounts(r.Context())
	if err != nil {
		http.Error(w, "Failed to get accounts", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.AccountRow, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, templates.AccountRow{
			ATDID:     account.ATDID,
			SSBFeedID: account.SSBFeedID,
			Active:    account.Active,
			CreatedAt: account.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderAccounts(w, templates.AccountsData{Accounts: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	query := parseMessageListQuery(r)

	messages, err := h.db.ListMessages(r.Context(), query)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}
	recordTypes, err := h.db.ListMessageTypes(r.Context())
	if err != nil {
		http.Error(w, "Failed to get message types", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.MessageRow, 0, len(messages))
	for _, message := range messages {
		issueText, issueClass := messageIssueSummary(message)
		rows = append(rows, templates.MessageRow{
			ATURI:           message.ATURI,
			DetailURL:       fmt.Sprintf("/messages/detail?at_uri=%s", url.QueryEscape(message.ATURI)),
			ATDID:           message.ATDID,
			Type:            message.Type,
			TypeLabel:       messageTypeLabel(message.Type),
			State:           message.MessageState,
			StateLabel:      messageStateLabel(message.MessageState),
			StateClass:      messageStateClass(message.MessageState),
			SSBMsgRef:       message.SSBMsgRef,
			IssueText:       issueText,
			IssueClass:      issueClass,
			PublishAttempts: message.PublishAttempts,
			DeferAttempts:   message.DeferAttempts,
			TotalAttempts:   message.PublishAttempts + message.DeferAttempts,
			CreatedAt:       message.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessages(w, templates.MessagesData{
		Messages:      rows,
		Filters:       templates.MessagesFilterState{Search: query.Search, Type: query.Type, State: query.State, Sort: query.Sort, Limit: query.Limit},
		TypeOptions:   buildTypeOptions(recordTypes, query.Type),
		StateOptions:  buildStateOptions(query.State),
		SortOptions:   buildSortOptions(query.Sort),
		LimitOptions:  buildLimitOptions(query.Limit),
		ActiveFilters: buildActiveMessageFilters(query),
		ResultCount:   len(rows),
		ReachedLimit:  len(rows) == query.Limit,
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
		OriginalMessageFields: summarizeATProtoMessage(*message),
		BridgedMessageFields:  summarizeSSBMessage(*message),
		RawATProtoJSON:        prettyJSON(message.RawATJson),
		RawSSBJSON:            prettyJSON(message.RawSSBJson),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessageDetail(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleFailures(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetPublishFailures(r.Context(), 200)
	if err != nil {
		http.Error(w, "Failed to get publish failures", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.FailureRow, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, templates.FailureRow{
			ATURI:           message.ATURI,
			ATDID:           message.ATDID,
			Type:            message.Type,
			State:           message.MessageState,
			Reason:          issueReason(message),
			PublishAttempts: message.PublishAttempts,
			CreatedAt:       message.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderFailures(w, templates.FailuresData{Failures: rows}); err != nil {
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
	if err := templates.RenderBlobs(w, templates.BlobsData{Blobs: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetAllBridgeState(r.Context())
	if err != nil {
		http.Error(w, "Failed to get bridge state", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.StateRow, 0, len(state))
	for _, s := range state {
		rows = append(rows, templates.StateRow{
			Key:       s.Key,
			Value:     s.Value,
			UpdatedAt: s.UpdatedAt,
		})
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

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderState(w, templates.StateData{
		State:             rows,
		DeferredCount:     deferredCount,
		DeletedCount:      deletedCount,
		LatestDeferReason: latestReason,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func issueReason(message db.Message) string {
	if message.MessageState == db.MessageStateDeferred && message.DeferReason != "" {
		return message.DeferReason
	}
	return message.PublishError
}

func parseMessageListQuery(r *http.Request) db.MessageListQuery {
	values := r.URL.Query()
	return db.MessageListQuery{
		Search: strings.TrimSpace(values.Get("q")),
		Type:   strings.TrimSpace(values.Get("type")),
		State:  sanitizeMessageState(values.Get("state")),
		Sort:   sanitizeMessageSort(values.Get("sort")),
		Limit:  parseMessageLimit(values.Get("limit")),
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
		{Value: "state_asc", Label: "State A-Z", Selected: selected == "state_asc"},
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
	if query.Type != "" {
		filters = append(filters, templates.ActiveFilter{Label: "Type", Value: messageTypeLabel(query.Type)})
	}
	if query.State != "" {
		filters = append(filters, templates.ActiveFilter{Label: "State", Value: messageStateLabel(query.State)})
	}
	return filters
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
		return "bg-green-100 text-green-800"
	case db.MessageStateFailed:
		return "bg-red-100 text-red-800"
	case db.MessageStateDeferred:
		return "bg-amber-100 text-amber-800"
	case db.MessageStateDeleted:
		return "bg-slate-200 text-slate-800"
	default:
		return "bg-gray-100 text-gray-800"
	}
}

func messageIssueSummary(message db.Message) (string, string) {
	switch strings.TrimSpace(message.MessageState) {
	case db.MessageStateFailed:
		if strings.TrimSpace(message.PublishError) != "" {
			return compactIssueText(message.PublishError), "text-red-700"
		}
	case db.MessageStateDeferred:
		if strings.TrimSpace(message.DeferReason) != "" {
			return summarizeDeferredIssue(message.DeferReason), "text-amber-700"
		}
	case db.MessageStateDeleted:
		if strings.TrimSpace(message.DeletedReason) != "" {
			return compactIssueText(message.DeletedReason), "text-slate-700"
		}
	}
	switch {
	case strings.TrimSpace(message.PublishError) != "":
		return compactIssueText(message.PublishError), "text-red-700"
	case strings.TrimSpace(message.DeferReason) != "":
		return summarizeDeferredIssue(message.DeferReason), "text-amber-700"
	case strings.TrimSpace(message.DeletedReason) != "":
		return compactIssueText(message.DeletedReason), "text-slate-700"
	default:
		return "No issue", "text-gray-500"
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

func prettyJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(empty)"
	}

	var decoded interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}

	formatted, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return raw
	}
	return string(formatted)
}

func summarizeATProtoMessage(message db.Message) []templates.DetailField {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(message.RawATJson), &payload); err != nil {
		return fallbackSummaryFields(message.RawATJson, "ATProto")
	}

	collector := newDetailCollector()
	collector.add("Collection", message.Type)
	collector.add("Text", stringAt(payload, "text"))
	collector.add("Operation", stringAt(payload, "op"))
	collector.add("Created At", stringAt(payload, "createdAt"))
	collector.add("Subject URI", nestedString(payload, "subject", "uri"))
	collector.add("Subject URI", stringAt(payload, "subject"))
	collector.add("Reply Root", nestedString(payload, "reply", "root", "uri"))
	collector.add("Reply Parent", nestedString(payload, "reply", "parent", "uri"))
	collector.add("Embed Type", nestedString(payload, "embed", "$type"))
	collector.add("Languages", joinArray(payload["langs"]))
	collector.add("Sequence", jsonScalar(payload["seq"]))
	if len(collector.fields) > 0 {
		return collector.fields
	}
	return fallbackSummaryFields(message.RawATJson, "ATProto")
}

func summarizeSSBMessage(message db.Message) []templates.DetailField {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(message.RawSSBJson), &payload); err != nil {
		return fallbackSummaryFields(message.RawSSBJson, "SSB")
	}

	collector := newDetailCollector()
	collector.add("Type", stringAt(payload, "type"))
	collector.add("Text", stringAt(payload, "text"))
	collector.add("Subject", stringAt(payload, "_atproto_subject"))
	collector.add("Contact DID", stringAt(payload, "_atproto_contact"))
	collector.add("Reply Root", stringAt(payload, "_atproto_reply_root"))
	collector.add("Reply Parent", stringAt(payload, "_atproto_reply_parent"))
	collector.add("Root", stringAt(payload, "root"))
	collector.add("Branch", stringAt(payload, "branch"))
	collector.add("Following", jsonScalar(payload["following"]))
	collector.add("Vote", voteSummary(payload["vote"]))
	if len(collector.fields) > 0 {
		return collector.fields
	}
	return fallbackSummaryFields(message.RawSSBJson, "SSB")
}

func fallbackSummaryFields(raw string, label string) []templates.DetailField {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []templates.DetailField{{
		Label: label + " Payload",
		Value: raw,
	}}
}

type detailCollector struct {
	fields []templates.DetailField
	seen   map[string]struct{}
}

func newDetailCollector() *detailCollector {
	return &detailCollector{seen: make(map[string]struct{})}
}

func (c *detailCollector) add(label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	key := label + "\x00" + value
	if _, ok := c.seen[key]; ok {
		return
	}
	c.seen[key] = struct{}{}
	c.fields = append(c.fields, templates.DetailField{
		Label: label,
		Value: value,
	})
}

func stringAt(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	return jsonScalar(m[key])
}

func nestedString(m map[string]interface{}, path ...string) string {
	var current interface{} = m
	for _, key := range path {
		next, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next[key]
	}
	return jsonScalar(current)
}

func jsonScalar(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case json.Number:
		return val.String()
	default:
		return ""
	}
}

func joinArray(v interface{}) string {
	items, ok := v.([]interface{})
	if !ok || len(items) == 0 {
		return ""
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value := jsonScalar(item); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ", ")
}

func voteSummary(v interface{}) string {
	vote, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	expression := strings.TrimSpace(jsonScalar(vote["expression"]))
	value := strings.TrimSpace(jsonScalar(vote["value"]))
	switch {
	case expression != "" && value != "":
		return expression + " (" + value + ")"
	case expression != "":
		return expression
	default:
		return value
	}
}
