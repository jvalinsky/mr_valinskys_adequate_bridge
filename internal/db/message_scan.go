package db

import "database/sql"

func scanMessagesRows(rows *sql.Rows) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		msg, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func scanMessageRow(scanner interface {
	Scan(dest ...interface{}) error
}) (Message, error) {
	var msg Message
	var ssbMsgRef, messageState, rawATJSON, rawSSBJSON, publishError, deferReason, deletedReason, rootATURI, parentATURI sql.NullString
	var publishedAt, lastPublishAttemptAt, lastDeferAttemptAt, deletedAt sql.NullTime
	var deletedSeq sql.NullInt64
	if err := scanner.Scan(
		&msg.ATURI,
		&msg.ATCID,
		&ssbMsgRef,
		&msg.ATDID,
		&msg.Type,
		&messageState,
		&rawATJSON,
		&rawSSBJSON,
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
		&rootATURI,
		&parentATURI,
		&msg.CreatedAt,
	); err != nil {
		return Message{}, err
	}
	msg.CreatedAt = msg.CreatedAt.UTC()
	msg.SSBMsgRef = ssbMsgRef.String
	msg.MessageState = messageState.String
	msg.RawATJson = rawATJSON.String
	msg.RawSSBJson = rawSSBJSON.String
	msg.PublishError = publishError.String
	msg.DeferReason = deferReason.String
	msg.DeletedReason = deletedReason.String
	msg.RootATURI = rootATURI.String
	msg.ParentATURI = parentATURI.String
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
	return msg, nil
}
