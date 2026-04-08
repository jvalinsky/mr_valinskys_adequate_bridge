package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

type reverseUIDatabase interface {
	ListReverseIdentityMappings(ctx context.Context) ([]db.ReverseIdentityMapping, error)
	AddReverseIdentityMapping(ctx context.Context, mapping db.ReverseIdentityMapping) error
	RemoveReverseIdentityMapping(ctx context.Context, ssbFeedID string) error
	ListReverseEvents(ctx context.Context, query db.ReverseEventListQuery) ([]db.ReverseEvent, error)
}

func (h *UIHandler) handleReverse(w http.ResponseWriter, r *http.Request) {
	reverseDB, ok := h.db.(reverseUIDatabase)
	if !ok {
		h.writeInternalError(w, "reverse", "Reverse-sync UI is unavailable for this database backend", nil)
		return
	}

	mappings, err := reverseDB.ListReverseIdentityMappings(r.Context())
	if err != nil {
		h.writeInternalError(w, "reverse", "Failed to get reverse mappings", err)
		return
	}

	query := db.ReverseEventListQuery{
		State:  strings.TrimSpace(r.URL.Query().Get("state")),
		Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Search: strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  200,
	}
	events, err := reverseDB.ListReverseEvents(r.Context(), query)
	if err != nil {
		h.writeInternalError(w, "reverse", "Failed to get reverse events", err)
		return
	}

	mappingRows := make([]templates.ReverseMappingRow, 0, len(mappings))
	for _, mapping := range mappings {
		statusText := "runtime unavailable"
		statusClass := "state-pending"
		if h.reverseSync != nil {
			status := h.reverseSync.CredentialStatus(mapping.ATDID)
			statusText = status.Reason
			if status.Configured {
				statusText = "configured"
				statusClass = "state-published"
			} else {
				statusClass = "state-deferred"
			}
		}
		mappingRows = append(mappingRows, templates.ReverseMappingRow{
			SSBFeedID:        mapping.SSBFeedID,
			ATDID:            mapping.ATDID,
			Active:           mapping.Active,
			AllowPosts:       mapping.AllowPosts,
			AllowReplies:     mapping.AllowReplies,
			AllowFollows:     mapping.AllowFollows,
			CredentialStatus: statusText,
			CredentialClass:  statusClass,
			UpdatedAt:        mapping.UpdatedAt,
		})
	}

	eventRows := make([]templates.ReverseEventRow, 0, len(events))
	for _, event := range events {
		issue := strings.TrimSpace(event.DeferReason)
		if issue == "" {
			issue = strings.TrimSpace(event.ErrorText)
		}
		eventRows = append(eventRows, templates.ReverseEventRow{
			SourceSSBMsgRef:  event.SourceSSBMsgRef,
			SourceSSBAuthor:  event.SourceSSBAuthor,
			ATDID:            event.ATDID,
			Action:           event.Action,
			State:            event.EventState,
			StateClass:       messageStateClass(event.EventState),
			Attempts:         event.Attempts,
			ReceiveLogSeq:    event.ReceiveLogSeq,
			TargetSSBFeedID:  event.TargetSSBFeedID,
			TargetATDID:      event.TargetATDID,
			TargetATURI:      event.TargetATURI,
			ResultATURI:      event.ResultATURI,
			Issue:            issue,
			UpdatedAt:        event.UpdatedAt,
			Retryable:        event.EventState == db.ReverseEventStateFailed || event.EventState == db.ReverseEventStateDeferred,
		})
	}

	statusTone := "success"
	statusTitle := "Reverse sync enabled"
	statusBody := "Allowlisted SSB feeds can publish posts, replies, and follow changes into ATProto."
	enabled := h.reverseSync != nil && h.reverseSync.Enabled()
	if !enabled {
		statusTone = "warning"
		statusTitle = "Reverse sync disabled"
		statusBody = "Mappings and queue data remain visible, but receive-log processing is not currently running."
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderReverse(w, templates.ReverseData{
		Chrome: templates.PageChrome{
			ActiveNav: "reverse",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Reverse Sync", Href: ""},
			},
			Status: templates.PageStatus{
				Visible: true,
				Tone:    statusTone,
				Title:   statusTitle,
				Body:    statusBody,
			},
		},
		Enabled: enabled,
		Mappings: mappingRows,
		Events:   eventRows,
		Filters: templates.ReverseFilterState{
			State:  query.State,
			Action: query.Action,
			Search: query.Search,
		},
		StateOptions: buildReverseStateOptions(query.State),
		ActionOptions: buildReverseActionOptions(query.Action),
	}); err != nil {
		h.writeInternalError(w, "reverse", "Template error", err)
	}
}

func (h *UIHandler) handleReverseMappingUpsert(w http.ResponseWriter, r *http.Request) {
	reverseDB, ok := h.db.(reverseUIDatabase)
	if !ok {
		h.writeInternalError(w, "reverse_mapping_upsert", "Reverse-sync UI is unavailable for this database backend", nil)
		return
	}

	mapping := db.ReverseIdentityMapping{
		SSBFeedID:    strings.TrimSpace(r.FormValue("ssb_feed_id")),
		ATDID:        strings.TrimSpace(r.FormValue("at_did")),
		Active:       formCheckboxValue(r, "active", false),
		AllowPosts:   formCheckboxValue(r, "allow_posts", false),
		AllowReplies: formCheckboxValue(r, "allow_replies", false),
		AllowFollows: formCheckboxValue(r, "allow_follows", false),
	}
	if mapping.SSBFeedID == "" || mapping.ATDID == "" {
		http.Error(w, "Missing ssb_feed_id or at_did", http.StatusBadRequest)
		return
	}
	if err := reverseDB.AddReverseIdentityMapping(r.Context(), mapping); err != nil {
		h.writeInternalError(w, "reverse_mapping_upsert", "Failed to save reverse mapping", err)
		return
	}
	http.Redirect(w, r, "/reverse", http.StatusSeeOther)
}

func (h *UIHandler) handleReverseMappingRemove(w http.ResponseWriter, r *http.Request) {
	reverseDB, ok := h.db.(reverseUIDatabase)
	if !ok {
		h.writeInternalError(w, "reverse_mapping_remove", "Reverse-sync UI is unavailable for this database backend", nil)
		return
	}
	ssbFeedID := strings.TrimSpace(r.FormValue("ssb_feed_id"))
	if ssbFeedID == "" {
		http.Error(w, "Missing ssb_feed_id", http.StatusBadRequest)
		return
	}
	if err := reverseDB.RemoveReverseIdentityMapping(r.Context(), ssbFeedID); err != nil {
		h.writeInternalError(w, "reverse_mapping_remove", "Failed to disable reverse mapping", err)
		return
	}
	http.Redirect(w, r, "/reverse", http.StatusSeeOther)
}

func (h *UIHandler) handleReverseEventRetry(w http.ResponseWriter, r *http.Request) {
	sourceSSBMsgRef := strings.TrimSpace(r.FormValue("source_ssb_msg_ref"))
	if sourceSSBMsgRef == "" {
		http.Error(w, "Missing source_ssb_msg_ref", http.StatusBadRequest)
		return
	}
	if h.reverseSync == nil {
		http.Error(w, "Reverse sync is not available", http.StatusServiceUnavailable)
		return
	}
	if err := h.reverseSync.RetryEvent(r.Context(), sourceSSBMsgRef); err != nil {
		h.writeInternalError(w, "reverse_event_retry", "Failed to retry reverse event", err)
		return
	}
	redirectURL := "/reverse"
	if q := strings.TrimSpace(r.FormValue("redirect_query")); q != "" {
		redirectURL += "?" + q
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func buildReverseStateOptions(selected string) []templates.FilterOption {
	values := []string{"", db.ReverseEventStatePending, db.ReverseEventStatePublished, db.ReverseEventStateDeferred, db.ReverseEventStateFailed, db.ReverseEventStateSkipped}
	options := make([]templates.FilterOption, 0, len(values))
	for _, value := range values {
		label := "All states"
		if value != "" {
			label = strings.Title(value)
		}
		options = append(options, templates.FilterOption{Value: value, Label: label, Selected: value == selected})
	}
	return options
}

func buildReverseActionOptions(selected string) []templates.FilterOption {
	values := []string{"", db.ReverseActionPost, db.ReverseActionReply, db.ReverseActionFollow, db.ReverseActionUnfollow}
	options := make([]templates.FilterOption, 0, len(values))
	for _, value := range values {
		label := "All actions"
		if value != "" {
			label = strings.Title(value)
		}
		options = append(options, templates.FilterOption{Value: value, Label: label, Selected: value == selected})
	}
	return options
}

func formCheckboxValue(r *http.Request, key string, defaultValue bool) bool {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func reverseRedirectQuery(r *http.Request) string {
	values := url.Values{}
	for _, key := range []string{"state", "action", "q"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			values.Set(key, value)
		}
	}
	return values.Encode()
}
