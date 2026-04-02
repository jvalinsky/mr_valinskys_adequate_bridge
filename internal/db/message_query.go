package db

import "strings"

func normalizeMessageLimit(limit int) int {
	switch {
	case limit <= 0:
		return 100
	case limit > 500:
		return 500
	default:
		return limit
	}
}

// NormalizeMessageListQuery canonicalizes message-list query fields used by
// both DB list operations and HTTP handlers.
func NormalizeMessageListQuery(query MessageListQuery) MessageListQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Type = strings.TrimSpace(query.Type)
	query.State = normalizeMessageState(strings.TrimSpace(query.State))
	query.Sort = normalizeMessageSort(strings.TrimSpace(query.Sort))
	query.Limit = normalizeMessageLimit(query.Limit)
	query.ATDID = strings.TrimSpace(query.ATDID)
	query.Direction = normalizeMessageDirection(query.Direction)
	return query
}

func normalizeMessageListQuery(query MessageListQuery) MessageListQuery {
	return NormalizeMessageListQuery(query)
}

func normalizeMessageState(state string) string {
	switch strings.TrimSpace(state) {
	case "", MessageStatePending, MessageStatePublished, MessageStateFailed, MessageStateDeferred, MessageStateDeleted:
		return strings.TrimSpace(state)
	default:
		return ""
	}
}

func normalizeMessageDirection(direction string) string {
	switch strings.TrimSpace(direction) {
	case "prev":
		return "prev"
	default:
		return "next"
	}
}

func normalizeMessageSort(sort string) string {
	switch sort {
	case "oldest", "attempts_desc", "attempts_asc", "type_asc", "type_desc", "state_asc", "state_desc":
		return sort
	default:
		return "newest"
	}
}

func appendMessageListFilters(builder *strings.Builder, args *[]interface{}, query MessageListQuery) {
	if query.Search != "" {
		search := "%" + query.Search + "%"
		builder.WriteString(` AND (at_uri LIKE ? OR at_did LIKE ? OR COALESCE(ssb_msg_ref, '') LIKE ? OR COALESCE(publish_error, '') LIKE ? OR COALESCE(defer_reason, '') LIKE ? OR COALESCE(deleted_reason, '') LIKE ?)`)
		*args = append(*args, search, search, search, search, search, search)
	}
	if query.Type != "" {
		builder.WriteString(` AND type = ?`)
		*args = append(*args, query.Type)
	}
	if query.State != "" {
		builder.WriteString(` AND message_state = ?`)
		*args = append(*args, query.State)
	}
	if query.ATDID != "" {
		builder.WriteString(` AND at_did = ?`)
		*args = append(*args, query.ATDID)
	}
	if query.HasIssue {
		builder.WriteString(` AND (TRIM(COALESCE(publish_error, '')) <> '' OR TRIM(COALESCE(defer_reason, '')) <> '' OR TRIM(COALESCE(deleted_reason, '')) <> '')`)
	}
}

func messageOrderClause(sort string) string {
	switch sort {
	case "oldest":
		return "created_at ASC, at_uri ASC"
	case "attempts_desc":
		return "(publish_attempts + defer_attempts) DESC, created_at DESC, at_uri DESC"
	case "attempts_asc":
		return "(publish_attempts + defer_attempts) ASC, created_at DESC, at_uri DESC"
	case "type_asc":
		return "type ASC, created_at DESC, at_uri DESC"
	case "type_desc":
		return "type DESC, created_at DESC, at_uri DESC"
	case "state_asc":
		return "message_state ASC, created_at DESC, at_uri DESC"
	case "state_desc":
		return "message_state DESC, created_at DESC, at_uri DESC"
	default:
		return "created_at DESC, at_uri DESC"
	}
}

func supportsMessageKeysetSort(sort string) bool {
	switch sort {
	case "newest", "oldest":
		return true
	default:
		return false
	}
}
