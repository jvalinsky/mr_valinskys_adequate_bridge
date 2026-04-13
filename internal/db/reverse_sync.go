package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	ReverseActionPost     = "post"
	ReverseActionReply    = "reply"
	ReverseActionFollow   = "follow"
	ReverseActionUnfollow = "unfollow"
	ReverseActionVote     = "vote"
	ReverseActionAbout    = "about"
	ReverseActionRepost   = "repost"

	ReverseEventStatePending   = "pending"
	ReverseEventStatePublished = "published"
	ReverseEventStateFailed    = "failed"
	ReverseEventStateDeferred  = "deferred"
	ReverseEventStateSkipped   = "skipped"
)

type ReverseIdentityMapping struct {
	SSBFeedID    string
	ATDID        string
	Active       bool
	AllowPosts   bool
	AllowReplies bool
	AllowFollows bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ReverseEvent struct {
	SourceSSBMsgRef string
	SourceSSBAuthor string
	SourceSSBSeq    *int64
	ReceiveLogSeq   int64
	ATDID           string
	Action          string
	EventState      string
	Attempts        int
	LastAttemptAt   *time.Time
	PublishedAt     *time.Time
	ErrorText       string
	DeferReason     string
	TargetSSBRef    string
	TargetSSBFeedID string
	TargetATDID     string
	TargetATURI     string
	TargetATCID     string
	ResultATURI     string
	ResultATCID     string
	ResultCollection string
	RawSSBJSON      string
	RawATJSON       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReverseEventListQuery struct {
	State  string
	Action string
	Search string
	Limit  int
}

func normalizeReverseEventListQuery(query ReverseEventListQuery) ReverseEventListQuery {
	query.State = strings.TrimSpace(query.State)
	query.Action = strings.TrimSpace(query.Action)
	query.Search = strings.TrimSpace(query.Search)
	if query.Limit <= 0 {
		query.Limit = 200
	}
	if query.Limit > 1000 {
		query.Limit = 1000
	}
	return query
}

func (db *DB) AddReverseIdentityMapping(ctx context.Context, mapping ReverseIdentityMapping) error {
	if strings.TrimSpace(mapping.SSBFeedID) == "" {
		return fmt.Errorf("add reverse identity mapping: ssb feed id is required")
	}
	if strings.TrimSpace(mapping.ATDID) == "" {
		return fmt.Errorf("add reverse identity mapping %s: at did is required", mapping.SSBFeedID)
	}
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO reverse_identity_mappings (
			ssb_feed_id, at_did, active, allow_posts, allow_replies, allow_follows, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(ssb_feed_id) DO UPDATE SET
			at_did=excluded.at_did,
			active=excluded.active,
			allow_posts=excluded.allow_posts,
			allow_replies=excluded.allow_replies,
			allow_follows=excluded.allow_follows,
			updated_at=CURRENT_TIMESTAMP`,
		strings.TrimSpace(mapping.SSBFeedID),
		strings.TrimSpace(mapping.ATDID),
		mapping.Active,
		mapping.AllowPosts,
		mapping.AllowReplies,
		mapping.AllowFollows,
	)
	if err != nil {
		return fmt.Errorf("add reverse identity mapping %s: %w", mapping.SSBFeedID, err)
	}
	return nil
}

func (db *DB) GetReverseIdentityMapping(ctx context.Context, ssbFeedID string) (*ReverseIdentityMapping, error) {
	row := db.conn.QueryRowContext(
		ctx,
		`SELECT ssb_feed_id, at_did, active, allow_posts, allow_replies, allow_follows, created_at, updated_at
		 FROM reverse_identity_mappings
		 WHERE ssb_feed_id = ?`,
		strings.TrimSpace(ssbFeedID),
	)
	mapping, err := scanReverseIdentityMapping(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get reverse identity mapping %s: %w", ssbFeedID, err)
	}
	return &mapping, nil
}

func (db *DB) ListReverseIdentityMappings(ctx context.Context) ([]ReverseIdentityMapping, error) {
	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT ssb_feed_id, at_did, active, allow_posts, allow_replies, allow_follows, created_at, updated_at
		 FROM reverse_identity_mappings
		 ORDER BY active DESC, updated_at DESC, ssb_feed_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list reverse identity mappings: %w", err)
	}
	defer rows.Close()
	return scanReverseIdentityMappings(rows)
}

func (db *DB) RemoveReverseIdentityMapping(ctx context.Context, ssbFeedID string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`UPDATE reverse_identity_mappings
		 SET active = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE ssb_feed_id = ?`,
		strings.TrimSpace(ssbFeedID),
	)
	if err != nil {
		return fmt.Errorf("remove reverse identity mapping %s: %w", ssbFeedID, err)
	}
	return nil
}

func (db *DB) ResolveATDIDBySSBFeed(ctx context.Context, ssbFeedID string) (string, bool, error) {
	ssbFeedID = strings.TrimSpace(ssbFeedID)
	if ssbFeedID == "" {
		return "", false, nil
	}

	var atDID string
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_did
		 FROM reverse_identity_mappings
		 WHERE ssb_feed_id = ? AND active = 1
		 ORDER BY updated_at DESC
		 LIMIT 1`,
		ssbFeedID,
	).Scan(&atDID)
	switch err {
	case nil:
		return atDID, true, nil
	case sql.ErrNoRows:
	default:
		return "", false, fmt.Errorf("resolve at did by reverse mapping %s: %w", ssbFeedID, err)
	}

	err = db.conn.QueryRowContext(
		ctx,
		`SELECT at_did
		 FROM bridged_accounts
		 WHERE ssb_feed_id = ? AND active = 1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		ssbFeedID,
	).Scan(&atDID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve at did by bridged account %s: %w", ssbFeedID, err)
	}
	return atDID, true, nil
}

func (db *DB) AddReverseEvent(ctx context.Context, event ReverseEvent) error {
	if strings.TrimSpace(event.SourceSSBMsgRef) == "" {
		return fmt.Errorf("add reverse event: source msg ref is required")
	}
	if strings.TrimSpace(event.SourceSSBAuthor) == "" {
		return fmt.Errorf("add reverse event %s: source author is required", event.SourceSSBMsgRef)
	}
	if strings.TrimSpace(event.ATDID) == "" {
		return fmt.Errorf("add reverse event %s: at did is required", event.SourceSSBMsgRef)
	}
	if strings.TrimSpace(event.Action) == "" {
		return fmt.Errorf("add reverse event %s: action is required", event.SourceSSBMsgRef)
	}
	if strings.TrimSpace(event.EventState) == "" {
		event.EventState = ReverseEventStatePending
	}
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO reverse_events (
			source_ssb_msg_ref, source_ssb_author, source_ssb_seq, receive_log_seq, at_did, action, event_state,
			attempts, last_attempt_at, published_at, error_text, defer_reason, target_ssb_ref, target_ssb_feed_id,
			target_at_did, target_at_uri, target_at_cid, result_at_uri, result_at_cid, result_collection,
			raw_ssb_json, raw_at_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_ssb_msg_ref) DO UPDATE SET
			source_ssb_author=excluded.source_ssb_author,
			source_ssb_seq=excluded.source_ssb_seq,
			receive_log_seq=excluded.receive_log_seq,
			at_did=excluded.at_did,
			action=excluded.action,
			event_state=excluded.event_state,
			attempts=reverse_events.attempts + excluded.attempts,
			last_attempt_at=excluded.last_attempt_at,
			published_at=excluded.published_at,
			error_text=excluded.error_text,
			defer_reason=excluded.defer_reason,
			target_ssb_ref=excluded.target_ssb_ref,
			target_ssb_feed_id=excluded.target_ssb_feed_id,
			target_at_did=excluded.target_at_did,
			target_at_uri=excluded.target_at_uri,
			target_at_cid=excluded.target_at_cid,
			result_at_uri=excluded.result_at_uri,
			result_at_cid=excluded.result_at_cid,
			result_collection=excluded.result_collection,
			raw_ssb_json=excluded.raw_ssb_json,
			raw_at_json=excluded.raw_at_json,
			updated_at=CURRENT_TIMESTAMP`,
		event.SourceSSBMsgRef,
		event.SourceSSBAuthor,
		nullableInt64(event.SourceSSBSeq),
		event.ReceiveLogSeq,
		event.ATDID,
		event.Action,
		event.EventState,
		event.Attempts,
		event.LastAttemptAt,
		event.PublishedAt,
		event.ErrorText,
		event.DeferReason,
		nullIfBlank(event.TargetSSBRef),
		nullIfBlank(event.TargetSSBFeedID),
		nullIfBlank(event.TargetATDID),
		nullIfBlank(event.TargetATURI),
		nullIfBlank(event.TargetATCID),
		nullIfBlank(event.ResultATURI),
		nullIfBlank(event.ResultATCID),
		nullIfBlank(event.ResultCollection),
		event.RawSSBJSON,
		event.RawATJSON,
	)
	if err != nil {
		return fmt.Errorf("add reverse event %s: %w", event.SourceSSBMsgRef, err)
	}
	return nil
}

func (db *DB) GetReverseEvent(ctx context.Context, sourceSSBMsgRef string) (*ReverseEvent, error) {
	row := db.conn.QueryRowContext(
		ctx,
		`SELECT source_ssb_msg_ref, source_ssb_author, source_ssb_seq, receive_log_seq, at_did, action, event_state,
		        attempts, last_attempt_at, published_at, error_text, defer_reason, target_ssb_ref, target_ssb_feed_id,
		        target_at_did, target_at_uri, target_at_cid, result_at_uri, result_at_cid, result_collection,
		        raw_ssb_json, raw_at_json, created_at, updated_at
		 FROM reverse_events
		 WHERE source_ssb_msg_ref = ?`,
		strings.TrimSpace(sourceSSBMsgRef),
	)
	event, err := scanReverseEvent(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get reverse event %s: %w", sourceSSBMsgRef, err)
	}
	return &event, nil
}

func (db *DB) ListReverseEvents(ctx context.Context, query ReverseEventListQuery) ([]ReverseEvent, error) {
	query = normalizeReverseEventListQuery(query)
	args := make([]any, 0, 8)
	var builder strings.Builder
	builder.WriteString(`SELECT source_ssb_msg_ref, source_ssb_author, source_ssb_seq, receive_log_seq, at_did, action, event_state,
	        attempts, last_attempt_at, published_at, error_text, defer_reason, target_ssb_ref, target_ssb_feed_id,
	        target_at_did, target_at_uri, target_at_cid, result_at_uri, result_at_cid, result_collection,
	        raw_ssb_json, raw_at_json, created_at, updated_at
	 FROM reverse_events
	 WHERE 1=1`)

	if query.State != "" {
		builder.WriteString(` AND event_state = ?`)
		args = append(args, query.State)
	}
	if query.Action != "" {
		builder.WriteString(` AND action = ?`)
		args = append(args, query.Action)
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		builder.WriteString(` AND (
			source_ssb_msg_ref LIKE ? OR
			source_ssb_author LIKE ? OR
			at_did LIKE ? OR
			COALESCE(target_ssb_feed_id, '') LIKE ? OR
			COALESCE(target_at_did, '') LIKE ? OR
			COALESCE(result_at_uri, '') LIKE ?
		)`)
		args = append(args, search, search, search, search, search, search)
	}
	builder.WriteString(` ORDER BY receive_log_seq DESC, created_at DESC LIMIT ?`)
	args = append(args, query.Limit)

	rows, err := db.conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list reverse events: %w", err)
	}
	defer rows.Close()
	return scanReverseEvents(rows)
}

func (db *DB) ResetReverseEventForRetry(ctx context.Context, sourceSSBMsgRef string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`UPDATE reverse_events
		 SET event_state = ?, error_text = '', defer_reason = '', updated_at = CURRENT_TIMESTAMP
		 WHERE source_ssb_msg_ref = ?`,
		ReverseEventStatePending,
		strings.TrimSpace(sourceSSBMsgRef),
	)
	if err != nil {
		return fmt.Errorf("reset reverse event %s: %w", sourceSSBMsgRef, err)
	}
	return nil
}

func (db *DB) GetLatestPublishedReverseFollow(ctx context.Context, atDID, targetATDID string) (*ReverseEvent, error) {
	row := db.conn.QueryRowContext(
		ctx,
		`SELECT source_ssb_msg_ref, source_ssb_author, source_ssb_seq, receive_log_seq, at_did, action, event_state,
		        attempts, last_attempt_at, published_at, error_text, defer_reason, target_ssb_ref, target_ssb_feed_id,
		        target_at_did, target_at_uri, target_at_cid, result_at_uri, result_at_cid, result_collection,
		        raw_ssb_json, raw_at_json, created_at, updated_at
		 FROM reverse_events
		 WHERE at_did = ?
		   AND action = ?
		   AND event_state = ?
		   AND target_at_did = ?
		   AND TRIM(COALESCE(result_at_uri, '')) <> ''
		 ORDER BY COALESCE(published_at, updated_at, created_at) DESC
		 LIMIT 1`,
		strings.TrimSpace(atDID),
		ReverseActionFollow,
		ReverseEventStatePublished,
		strings.TrimSpace(targetATDID),
	)
	event, err := scanReverseEvent(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest published reverse follow %s -> %s: %w", atDID, targetATDID, err)
	}
	return &event, nil
}

func scanReverseIdentityMapping(row scannable) (ReverseIdentityMapping, error) {
	var mapping ReverseIdentityMapping
	err := row.Scan(
		&mapping.SSBFeedID,
		&mapping.ATDID,
		&mapping.Active,
		&mapping.AllowPosts,
		&mapping.AllowReplies,
		&mapping.AllowFollows,
		&mapping.CreatedAt,
		&mapping.UpdatedAt,
	)
	return mapping, err
}

func scanReverseIdentityMappings(rows *sql.Rows) ([]ReverseIdentityMapping, error) {
	var mappings []ReverseIdentityMapping
	for rows.Next() {
		mapping, err := scanReverseIdentityMapping(rows)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	return mappings, rows.Err()
}

func scanReverseEvent(row scannable) (ReverseEvent, error) {
	var event ReverseEvent
	var sourceSeq sql.NullInt64
	var targetSSBRef, targetSSBFeedID, targetATDID, targetATURI, targetATCID sql.NullString
	var resultATURI, resultATCID, resultCollection sql.NullString
	var lastAttemptAt, publishedAt sql.NullString
	err := row.Scan(
		&event.SourceSSBMsgRef,
		&event.SourceSSBAuthor,
		&sourceSeq,
		&event.ReceiveLogSeq,
		&event.ATDID,
		&event.Action,
		&event.EventState,
		&event.Attempts,
		&lastAttemptAt,
		&publishedAt,
		&event.ErrorText,
		&event.DeferReason,
		&targetSSBRef,
		&targetSSBFeedID,
		&targetATDID,
		&targetATURI,
		&targetATCID,
		&resultATURI,
		&resultATCID,
		&resultCollection,
		&event.RawSSBJSON,
		&event.RawATJSON,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
	if err != nil {
		return event, err
	}
	if sourceSeq.Valid {
		event.SourceSSBSeq = &sourceSeq.Int64
	}
	event.LastAttemptAt = parseNullableTime(lastAttemptAt)
	event.PublishedAt = parseNullableTime(publishedAt)
	event.TargetSSBRef = targetSSBRef.String
	event.TargetSSBFeedID = targetSSBFeedID.String
	event.TargetATDID = targetATDID.String
	event.TargetATURI = targetATURI.String
	event.TargetATCID = targetATCID.String
	event.ResultATURI = resultATURI.String
	event.ResultATCID = resultATCID.String
	event.ResultCollection = resultCollection.String
	return event, nil
}

func scanReverseEvents(rows *sql.Rows) ([]ReverseEvent, error) {
	var events []ReverseEvent
	for rows.Next() {
		event, err := scanReverseEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullIfBlank(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.TrimSpace(s)
}
