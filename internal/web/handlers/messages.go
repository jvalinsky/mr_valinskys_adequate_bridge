// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/presentation"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

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

	seenBlobRef := make(map[string]struct{})
	seenBlobCID := make(map[string]struct{})
	appendAssociatedBlob := func(blob *db.Blob) {
		if blob == nil {
			return
		}
		trimmedRef := strings.TrimSpace(blob.SSBBlobRef)
		trimmedCID := strings.TrimSpace(blob.ATCID)
		if trimmedRef != "" {
			if _, exists := seenBlobRef[trimmedRef]; exists {
				return
			}
		}
		if trimmedCID != "" {
			if _, exists := seenBlobCID[trimmedCID]; exists {
				return
			}
		}
		if trimmedRef != "" {
			seenBlobRef[trimmedRef] = struct{}{}
		}
		if trimmedCID != "" {
			seenBlobCID[trimmedCID] = struct{}{}
		}
		data.AssociatedBlobs = append(data.AssociatedBlobs, templates.BlobRow{
			ATCID:        blob.ATCID,
			SSBBlobRef:   blob.SSBBlobRef,
			Size:         blob.Size,
			MimeType:     blob.MimeType,
			DownloadedAt: blob.DownloadedAt,
		})
	}

	// Extract blob references from SSB JSON and look them up by SSB ref.
	blobRefs := findBlobRefs(message.RawSSBJson)
	for _, ref := range blobRefs {
		blob, err := h.db.GetBlobBySSBRef(r.Context(), ref)
		if err == nil {
			appendAssociatedBlob(blob)
		}
	}

	// Also resolve blob references directly from the ATProto record payload.
	for _, atCID := range findATProtoBlobCIDs(message.RawATJson) {
		blob, err := h.db.GetBlob(r.Context(), atCID)
		if err == nil {
			appendAssociatedBlob(blob)
		}
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

func (h *UIHandler) handleFailuresRetry(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetPublishFailures(r.Context(), 1000)
	if err != nil {
		h.writeInternalError(w, "failures_retry", "Failed to get failures", err)
		return
	}

	count := 0
	for _, msg := range messages {
		if err := h.db.ResetMessageForRetry(r.Context(), msg.ATURI); err != nil {
			h.logger.Printf("event=failures_retry_reset_error at_uri=%s err=%v", msg.ATURI, err)
			continue
		}
		count++
	}

	h.logger.Printf("event=failures_retry_reset count=%d", count)
	http.Redirect(w, r, "/failures", http.StatusSeeOther)
}
