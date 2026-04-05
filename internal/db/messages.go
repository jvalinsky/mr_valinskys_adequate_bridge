package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/config"
)

const messageSelectColumns = `SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at`

func (db *DB) AddMessage(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.MessageState) == "" {
		msg.MessageState = MessageStatePending
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().Truncate(time.Millisecond).UTC()
	} else {
		msg.CreatedAt = msg.CreatedAt.Truncate(time.Millisecond).UTC()
	}

	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO messages (
			at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(at_uri) DO UPDATE SET
		 	at_cid=excluded.at_cid,
		 	ssb_msg_ref=excluded.ssb_msg_ref,
		 	at_did=excluded.at_did,
		 	type=excluded.type,
		 	message_state=excluded.message_state,
		 	raw_at_json=excluded.raw_at_json,
		 	raw_ssb_json=excluded.raw_ssb_json,
		 	published_at=excluded.published_at,
		 	publish_error=excluded.publish_error,
		 	publish_attempts=messages.publish_attempts + excluded.publish_attempts,
		 	last_publish_attempt_at=excluded.last_publish_attempt_at,
		 	defer_reason=excluded.defer_reason,
		 	defer_attempts=messages.defer_attempts + excluded.defer_attempts,
		 	last_defer_attempt_at=excluded.last_defer_attempt_at,
		 	deleted_at=excluded.deleted_at,
		 	deleted_seq=excluded.deleted_seq,
		 	deleted_reason=excluded.deleted_reason`,
		msg.ATURI,
		msg.ATCID,
		msg.SSBMsgRef,
		msg.ATDID,
		msg.Type,
		msg.MessageState,
		msg.RawATJson,
		msg.RawSSBJson,
		msg.PublishedAt,
		msg.PublishError,
		msg.PublishAttempts,
		msg.LastPublishAttemptAt,
		msg.DeferReason,
		msg.DeferAttempts,
		msg.LastDeferAttemptAt,
		msg.DeletedAt,
		msg.DeletedSeq,
		msg.DeletedReason,
		msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("add message %s: %w", msg.ATURI, err)
	}
	return nil
}

func (db *DB) GetMessage(ctx context.Context, atURI string) (*Message, error) {
	var msg Message
	var ssbMsgRef, messageState, rawATJson, rawSSBJson, publishError, deferReason, deletedReason sql.NullString
	var publishedAt, lastPublishAttemptAt, lastDeferAttemptAt, deletedAt sql.NullTime
	var deletedSeq sql.NullInt64
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE at_uri = ?`,
		atURI,
	).Scan(
		&msg.ATURI,
		&msg.ATCID,
		&ssbMsgRef,
		&msg.ATDID,
		&msg.Type,
		&messageState,
		&rawATJson,
		&rawSSBJson,
		&publishedAt,
		&publishError,
		&msg.PublishAttempts,
		&lastPublishAttemptAt,
		&deferReason,
		&msg.DeferAttempts,
		&lastDeferAttemptAt,
		&deletedAt,
		&deletedSeq,
		&deletedReason,
		&msg.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get message %s: %w", atURI, err)
	}

	msg.SSBMsgRef = ssbMsgRef.String
	msg.MessageState = messageState.String
	msg.RawATJson = rawATJson.String
	msg.RawSSBJson = rawSSBJson.String
	msg.PublishError = publishError.String
	msg.DeferReason = deferReason.String
	msg.DeletedReason = deletedReason.String
	if publishedAt.Valid {
		t := publishedAt.Time
		msg.PublishedAt = &t
	}
	if lastPublishAttemptAt.Valid {
		t := lastPublishAttemptAt.Time
		msg.LastPublishAttemptAt = &t
	}
	if lastDeferAttemptAt.Valid {
		t := lastDeferAttemptAt.Time
		msg.LastDeferAttemptAt = &t
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		msg.DeletedAt = &t
	}
	if deletedSeq.Valid {
		seq := deletedSeq.Int64
		msg.DeletedSeq = &seq
	}
	return &msg, nil
}

func (db *DB) CountMessages(ctx context.Context) (int, error) {
	count, err := db.Queries().CountMessages(ctx)
	return int(count), err
}

func (db *DB) CountMessagesByDID(ctx context.Context, atDID string) (int, error) {
	count, err := db.Queries().CountMessagesByDID(ctx, atDID)
	return int(count), err
}

func (db *DB) GetRecentMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan recent messages: %w", err)
	}
	return messages, nil
}

func (db *DB) ListRecentPublishedMessagesByDID(ctx context.Context, atDID string, limit int) ([]Message, error) {
	atDID = strings.TrimSpace(atDID)
	if atDID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = config.DefaultPublishedLimit
	}

	rows, err := db.conn.QueryContext(
		ctx,
		messageSelectColumns+`
		 FROM messages
		 WHERE at_did = ?
		   AND message_state = ?
		   AND TRIM(COALESCE(ssb_msg_ref, '')) <> ''
		 ORDER BY COALESCE(published_at, created_at) DESC, created_at DESC, at_uri DESC
		 LIMIT ?`,
		atDID,
		MessageStatePublished,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent published messages for did %s: %w", atDID, err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan recent published messages for did %s: %w", atDID, err)
	}
	return messages, nil
}

func (db *DB) ResetMessageForRetry(ctx context.Context, atURI string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`UPDATE messages
		 SET message_state = ?,
		     publish_error = '',
		     publish_attempts = 0,
		     last_publish_attempt_at = NULL,
		     defer_reason = '',
		     defer_attempts = 0,
		     last_defer_attempt_at = NULL
		 WHERE at_uri = ?`,
		MessageStatePending,
		atURI,
	)
	if err != nil {
		return fmt.Errorf("reset message %s: %w", atURI, err)
	}
	return nil
}

func (db *DB) ListPublishedMessagesGlobal(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}

	rows, err := db.conn.QueryContext(
		ctx,
		messageSelectColumns+`
		 FROM messages
		 WHERE message_state = ?
		   AND TRIM(COALESCE(ssb_msg_ref, '')) <> ''
		 ORDER BY COALESCE(published_at, created_at) DESC, created_at DESC, at_uri DESC
		 LIMIT ?`,
		MessageStatePublished,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query global published messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan global published messages: %w", err)
	}
	return messages, nil
}

func (db *DB) ListMessages(ctx context.Context, query MessageListQuery) ([]Message, error) {
	query = normalizeMessageListQuery(query)

	var builder strings.Builder
	builder.WriteString(messageSelectColumns)
	builder.WriteString(` FROM messages WHERE 1=1`)

	args := make([]interface{}, 0, 12)
	appendMessageListFilters(&builder, &args, query)

	builder.WriteString(` ORDER BY `)
	builder.WriteString(messageOrderClause(query.Sort))
	builder.WriteString(` LIMIT ?`)
	args = append(args, query.Limit)

	rows, err := db.conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan messages: %w", err)
	}
	return messages, nil
}

func (db *DB) ListMessagesPage(ctx context.Context, query MessageListQuery) (MessagePage, error) {
	query = normalizeMessageListQuery(query)
	page := MessagePage{}

	if !supportsMessageKeysetSort(query.Sort) {
		legacyRows, err := db.ListMessages(ctx, MessageListQuery{
			Search:   query.Search,
			Type:     query.Type,
			State:    query.State,
			Sort:     query.Sort,
			Limit:    query.Limit + 1,
			ATDID:    query.ATDID,
			HasIssue: query.HasIssue,
		})
		if err != nil {
			return page, err
		}
		if len(legacyRows) > query.Limit {
			page.HasNext = true
			legacyRows = legacyRows[:query.Limit]
		}
		page.Messages = legacyRows
		if page.HasNext && len(legacyRows) > 0 {
			page.NextCursor = encodeMessageListCursor(messageListCursor{
				CreatedAt: legacyRows[len(legacyRows)-1].CreatedAt,
				ATURI:     legacyRows[len(legacyRows)-1].ATURI,
			})
		}
		return page, nil
	}

	var cursor messageListCursor
	cursorProvided := strings.TrimSpace(query.Cursor) != ""
	if cursorProvided {
		decoded, ok := decodeMessageListCursor(query.Cursor)
		if !ok {
			cursorProvided = false
		} else {
			cursor = decoded
		}
	}

	reverseQuery := false
	var builder strings.Builder
	builder.WriteString(messageSelectColumns)
	builder.WriteString(` FROM messages WHERE 1=1`)
	args := make([]interface{}, 0, 16)
	appendMessageListFilters(&builder, &args, query)

	if cursorProvided {
		clause, clauseArgs, reverse := messageKeysetClause(query.Sort, query.Direction, cursor)
		if clause != "" {
			builder.WriteString(` AND `)
			builder.WriteString(clause)
			args = append(args, clauseArgs...)
			reverseQuery = reverse
		}
	}

	builder.WriteString(` ORDER BY `)
	builder.WriteString(messageKeysetOrder(query.Sort, reverseQuery))
	builder.WriteString(` LIMIT ?`)
	args = append(args, query.Limit+1)

	rows, err := db.conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return page, err
	}

	hasMore := len(messages) > query.Limit
	if hasMore {
		messages = messages[:query.Limit]
	}
	if reverseQuery {
		reverseMessages(messages)
	}

	page.Messages = messages
	if query.Direction == "prev" {
		page.HasPrev = hasMore
		page.HasNext = cursorProvided
	} else {
		page.HasPrev = cursorProvided
		page.HasNext = hasMore
	}

	if len(messages) > 0 {
		first := messageListCursor{CreatedAt: messages[0].CreatedAt, ATURI: messages[0].ATURI}
		last := messageListCursor{CreatedAt: messages[len(messages)-1].CreatedAt, ATURI: messages[len(messages)-1].ATURI}
		if page.HasPrev {
			page.PrevCursor = encodeMessageListCursor(first)
		}
		if page.HasNext {
			page.NextCursor = encodeMessageListCursor(last)
		}
	}

	return page, nil
}

func (db *DB) ListMessageTypes(ctx context.Context) ([]string, error) {
	return querySlice(ctx, db.conn,
		"list message types",
		`SELECT DISTINCT type
		 FROM messages
		 WHERE TRIM(COALESCE(type, '')) <> ''
		 ORDER BY type ASC`,
		nil,
		scanMessageTypeRow,
	)
}

func (db *DB) CountPublishedMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> ''`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count published messages: %w", err)
	}
	return count, nil
}

func (db *DB) CountPublishFailures(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateFailed).Scan(&count); err != nil {
		return 0, fmt.Errorf("count publish failures: %w", err)
	}
	return count, nil
}

func (db *DB) CountDeferredMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateDeferred).Scan(&count); err != nil {
		return 0, fmt.Errorf("count deferred messages: %w", err)
	}
	return count, nil
}

func (db *DB) CountDeletedMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateDeleted).Scan(&count); err != nil {
		return 0, fmt.Errorf("count deleted messages: %w", err)
	}
	return count, nil
}

func (db *DB) ListTopDeferredReasons(ctx context.Context, limit int) ([]DeferredReasonCount, error) {
	if limit <= 0 {
		limit = 5
	}

	return querySlice(ctx, db.conn,
		"list top deferred reasons",
		`SELECT defer_reason, COUNT(*) AS reason_count
		 FROM messages
		 WHERE message_state = ?
		   AND TRIM(COALESCE(defer_reason, '')) <> ''
		 GROUP BY defer_reason
		 ORDER BY reason_count DESC, defer_reason ASC
		 LIMIT ?`,
		[]any{
			MessageStateDeferred,
			limit,
		},
		scanDeferredReasonCountRow,
	)
}

func (db *DB) ListTopIssueAccounts(ctx context.Context, limit int) ([]AccountIssueSummary, error) {
	if limit <= 0 {
		limit = 5
	}

	return querySlice(ctx, db.conn,
		"list top issue accounts",
		`SELECT
		   ba.at_did,
		   ba.ssb_feed_id,
		   ba.active,
		   COALESCE(m.total_messages, 0) AS total_messages,
		   COALESCE(m.issue_messages, 0) AS issue_messages,
		   COALESCE(m.failed_messages, 0) AS failed_messages,
		   COALESCE(m.deferred_messages, 0) AS deferred_messages,
		   COALESCE(m.deleted_messages, 0) AS deleted_messages
		 FROM bridged_accounts ba
		 LEFT JOIN (
		   SELECT
		     at_did,
		     COUNT(*) AS total_messages,
		     SUM(CASE WHEN message_state IN ('failed', 'deferred', 'deleted') THEN 1 ELSE 0 END) AS issue_messages,
		     SUM(CASE WHEN message_state = 'failed' THEN 1 ELSE 0 END) AS failed_messages,
		     SUM(CASE WHEN message_state = 'deferred' THEN 1 ELSE 0 END) AS deferred_messages,
		     SUM(CASE WHEN message_state = 'deleted' THEN 1 ELSE 0 END) AS deleted_messages
		   FROM messages
		   GROUP BY at_did
		 ) m ON m.at_did = ba.at_did
		 WHERE COALESCE(m.issue_messages, 0) > 0
		 ORDER BY issue_messages DESC, total_messages DESC, ba.at_did ASC
		 LIMIT ?`,
		[]any{limit},
		scanAccountIssueSummaryRow,
	)
}

func (db *DB) GetPublishFailures(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE message_state IN (?, ?)
		 ORDER BY COALESCE(last_publish_attempt_at, last_defer_attempt_at, created_at) DESC
		 LIMIT ?`,
		MessageStateFailed,
		MessageStateDeferred,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query publish failures: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan publish failures: %w", err)
	}
	return messages, nil
}

func (db *DB) GetRetryCandidates(ctx context.Context, limit int, atDID string, maxAttempts int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}

	var query strings.Builder
	query.WriteString(
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE message_state = ?
		   AND (ssb_msg_ref IS NULL OR ssb_msg_ref = '')
		   AND publish_attempts < ?`,
	)

	args := []interface{}{MessageStateFailed, maxAttempts}
	if strings.TrimSpace(atDID) != "" {
		query.WriteString(" AND at_did = ?")
		args = append(args, strings.TrimSpace(atDID))
	}
	query.WriteString(" ORDER BY COALESCE(last_publish_attempt_at, created_at) ASC LIMIT ?")
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query retry candidates: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan retry candidates: %w", err)
	}
	return messages, nil
}

func (db *DB) GetDeferredCandidates(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT m.at_uri, m.at_cid, m.ssb_msg_ref, m.at_did, m.type, m.message_state, m.raw_at_json, m.raw_ssb_json, m.published_at, m.publish_error, m.publish_attempts, m.last_publish_attempt_at, m.defer_reason, m.defer_attempts, m.last_defer_attempt_at, m.deleted_at, m.deleted_seq, m.deleted_reason, m.created_at
		 FROM messages m
		 LEFT JOIN messages dep ON dep.at_uri = SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://'), CASE WHEN INSTR(SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://')), ';') > 0 THEN INSTR(SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://')), ';') - 1 ELSE LENGTH(m.defer_reason) END) AND dep.message_state = ?
		 WHERE m.message_state = ?
		 ORDER BY CASE WHEN dep.at_uri IS NOT NULL THEN 1 ELSE 0 END ASC, COALESCE(m.last_defer_attempt_at, m.created_at) ASC
		 LIMIT ?`,
		MessageStateDeferred,
		MessageStateDeferred,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query deferred candidates: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan deferred candidates: %w", err)
	}
	return messages, nil
}

// GetExpiredDeferredMessages returns deferred messages older than maxAge, ordered by age.
func (db *DB) GetExpiredDeferredMessages(ctx context.Context, maxAge time.Duration, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = config.DefaultMessageLimit
	}

	cutoff := time.Now().UTC().Add(-maxAge)

	rows, err := db.conn.QueryContext(
		ctx,
		messageSelectColumns+`
		 FROM messages
		 WHERE message_state = ?
		   AND COALESCE(last_defer_attempt_at, created_at) < ?
		 ORDER BY COALESCE(last_defer_attempt_at, created_at) ASC
		 LIMIT ?`,
		MessageStateDeferred,
		cutoff,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query expired deferred messages: %w", err)
	}
	defer rows.Close()

	return scanMessagesRows(rows)
}

func (db *DB) GetLatestDeferredReason(ctx context.Context) (string, bool, error) {
	var reason sql.NullString
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT defer_reason
		 FROM messages
		 WHERE message_state = ? AND defer_reason IS NOT NULL AND defer_reason <> ''
		 ORDER BY COALESCE(last_defer_attempt_at, created_at) DESC
		 LIMIT 1`,
		MessageStateDeferred,
	).Scan(&reason)

	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get latest deferred reason: %w", err)
	}
	if !reason.Valid || strings.TrimSpace(reason.String) == "" {
		return "", false, nil
	}
	return reason.String, true, nil
}

func scanMessageTypeRow(rows *sql.Rows) (string, error) {
	var recordType string
	if err := rows.Scan(&recordType); err != nil {
		return "", err
	}
	return recordType, nil
}

func scanDeferredReasonCountRow(rows *sql.Rows) (DeferredReasonCount, error) {
	var stat DeferredReasonCount
	if err := rows.Scan(&stat.Reason, &stat.Count); err != nil {
		return DeferredReasonCount{}, err
	}
	return stat, nil
}

func scanAccountIssueSummaryRow(rows *sql.Rows) (AccountIssueSummary, error) {
	var stat AccountIssueSummary
	var active bool
	if err := rows.Scan(
		&stat.ATDID,
		&stat.SSBFeedID,
		&active,
		&stat.TotalMessages,
		&stat.IssueMessages,
		&stat.FailedMessages,
		&stat.DeferredCount,
		&stat.DeletedCount,
	); err != nil {
		return AccountIssueSummary{}, err
	}
	stat.Active = active
	return stat, nil
}
