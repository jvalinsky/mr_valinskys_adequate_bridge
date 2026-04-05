// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) writeInternalError(w http.ResponseWriter, handler, message string, err error) {
	h.logger.Printf("event=handler_error handler=%s error=%v", handler, err)
	http.Error(w, message, http.StatusInternalServerError)
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

func findBlobRefs(ssbJSON string) []string {
	re := regexp.MustCompile(`&[A-Za-z0-9+/=]+\.sha256`)
	matches := re.FindAllString(ssbJSON, -1)
	if len(matches) == 0 {
		return nil
	}
	// Dedupe
	seen := make(map[string]struct{})
	var res []string
	for _, m := range matches {
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			res = append(res, m)
		}
	}
	return res
}

func findATProtoBlobCIDs(rawATJSON string) []string {
	rawATJSON = strings.TrimSpace(rawATJSON)
	if rawATJSON == "" {
		return nil
	}

	var payload any
	if err := json.Unmarshal([]byte(rawATJSON), &payload); err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var cids []string

	var walk func(node any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			if cid := extractATProtoBlobCID(typed); cid != "" {
				if _, ok := seen[cid]; !ok {
					seen[cid] = struct{}{}
					cids = append(cids, cid)
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}

	walk(payload)
	sort.Strings(cids)
	return cids
}

func extractATProtoBlobCID(node map[string]any) string {
	typeValue, _ := node["$type"].(string)
	if typeValue != "blob" {
		return ""
	}

	if fromRef := extractATProtoBlobRefCID(node["ref"]); fromRef != "" {
		return fromRef
	}

	if legacyCID, ok := node["cid"].(string); ok {
		return strings.TrimSpace(legacyCID)
	}
	return ""
}

func extractATProtoBlobRefCID(ref any) string {
	switch typed := ref.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if link, ok := typed["$link"].(string); ok {
			return strings.TrimSpace(link)
		}
		if cid, ok := typed["cid"].(string); ok {
			return strings.TrimSpace(cid)
		}
	}
	return ""
}

// Message-related helpers

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
	query := db.MessageListQuery{
		Search:    strings.TrimSpace(values.Get("q")),
		ATDID:     strings.TrimSpace(values.Get("did")),
		Type:      strings.TrimSpace(values.Get("type")),
		State:     strings.TrimSpace(values.Get("state")),
		Sort:      strings.TrimSpace(values.Get("sort")),
		Limit:     parseMessageLimit(values.Get("limit")),
		HasIssue:  parseBoolFlag(values.Get("has_issue")),
		Cursor:    strings.TrimSpace(values.Get("cursor")),
		Direction: strings.TrimSpace(values.Get("dir")),
	}
	return db.NormalizeMessageListQuery(query)
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
	values := make(map[string]string)
	if query.Search != "" {
		values["q"] = query.Search
	}
	if query.ATDID != "" {
		values["did"] = query.ATDID
	}
	if query.Type != "" {
		values["type"] = query.Type
	}
	if query.State != "" {
		values["state"] = query.State
	}
	if query.Sort != "" {
		values["sort"] = query.Sort
	}
	values["limit"] = strconv.Itoa(query.Limit)
	if query.HasIssue {
		values["has_issue"] = "1"
	}
	if strings.TrimSpace(cursor) != "" {
		values["cursor"] = cursor
	}
	values["dir"] = direction

	// Build query string manually with proper URL encoding
	var params []string
	for k, v := range values {
		encoded := url.QueryEscape(v)
		params = append(params, k+"="+encoded)
	}
	sort.Strings(params)
	if len(params) == 0 {
		return "/messages"
	}
	return "/messages?" + strings.Join(params, "&")
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

// Time and formatting helpers

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
