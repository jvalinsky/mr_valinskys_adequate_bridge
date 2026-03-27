// Package bridge coordinates firehose ingestion, mapping, publishing, and persistence.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	indigorepo "github.com/bluesky-social/indigo/repo"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
)

var supportedCollections = map[string]struct{}{
	mapper.RecordTypePost:   {},
	mapper.RecordTypeLike:   {},
	mapper.RecordTypeRepost: {},
}

// Processor processes ATProto commits into persisted and optionally published SSB messages.
type Processor struct {
	db         *db.DB
	logger     *log.Logger
	publisher  Publisher
	blobBridge BlobBridge
}

// RetryConfig controls retry candidate selection and scheduling.
type RetryConfig struct {
	Limit       int
	ATDID       string
	MaxAttempts int
	BaseBackoff time.Duration
}

// RetryResult summarizes one retry run.
type RetryResult struct {
	Selected  int
	Attempted int
	Published int
	Failed    int
	Deferred  int
}

// DeferredResolveResult summarizes one deferred-resolution run.
type DeferredResolveResult struct {
	Selected  int
	Attempted int
	Published int
	Deferred  int
	Failed    int
}

// Publisher publishes one mapped message for a DID.
type Publisher interface {
	Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error)
}

// BlobBridge maps ATProto blobs onto SSB blob refs for one record payload.
type BlobBridge interface {
	BridgeRecordBlobs(ctx context.Context, atDID string, mapped map[string]interface{}, rawRecordJSON []byte) error
}

// Option configures Processor behavior.
type Option func(*Processor)

// WithPublisher sets the publish implementation used by Processor.
func WithPublisher(p Publisher) Option {
	return func(proc *Processor) {
		proc.publisher = p
	}
}

// WithBlobBridge sets the blob bridge implementation used by Processor.
func WithBlobBridge(b BlobBridge) Option {
	return func(proc *Processor) {
		proc.blobBridge = b
	}
}

// NewProcessor constructs a Processor with optional publisher/blob integrations.
func NewProcessor(database *db.DB, logger *log.Logger, opts ...Option) *Processor {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	proc := &Processor{
		db:     database,
		logger: logger,
	}
	for _, opt := range opts {
		opt(proc)
	}
	return proc
}

// HandleCommit satisfies firehose.EventHandler and persists supported records.
func (p *Processor) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	if evt == nil || evt.Repo == "" {
		return nil
	}

	acc, err := p.db.GetBridgedAccount(ctx, evt.Repo)
	if err != nil {
		return fmt.Errorf("lookup bridged account %s: %w", evt.Repo, err)
	}
	if acc == nil || !acc.Active {
		return nil
	}

	var rr *indigorepo.Repo

	processed := 0
	for _, op := range evt.Ops {
		switch op.Action {
		case "create", "update":
			if op.Cid == nil {
				continue
			}
			if rr == nil {
				var parseErr error
				rr, parseErr = firehose.ParseCommit(ctx, evt)
				if parseErr != nil {
					return fmt.Errorf("parse commit: %w", parseErr)
				}
			}

			if err := p.processOp(ctx, rr, evt.Repo, op.Path, op.Cid.String(), evt.Seq); err != nil {
				p.logger.Printf("event=record_skip did=%s path=%s seq=%d err=%v", evt.Repo, op.Path, evt.Seq, err)
				continue
			}
			processed++
		case "delete":
			if err := p.processDeleteOp(ctx, evt.Repo, op.Path, evt.Seq); err != nil {
				p.logger.Printf("event=record_delete_skip did=%s path=%s seq=%d err=%v", evt.Repo, op.Path, evt.Seq, err)
				continue
			}
			processed++
		}
	}

	if evt.Seq > 0 {
		if err := p.db.SetBridgeState(ctx, "firehose_seq", fmt.Sprintf("%d", evt.Seq)); err != nil {
			p.logger.Printf("event=cursor_persist_error did=%s seq=%d err=%v", evt.Repo, evt.Seq, err)
		}
	}

	if processed > 0 {
		p.logger.Printf("event=commit_processed did=%s seq=%d processed=%d", evt.Repo, evt.Seq, processed)
	}

	return nil
}

func (p *Processor) processOp(ctx context.Context, rr *indigorepo.Repo, atDID, path, atCID string, seq int64) error {
	collection, ok := collectionFromPath(path)
	if !ok || !isSupportedCollection(collection) {
		return nil
	}

	_, recordCBOR, err := rr.GetRecordBytes(ctx, path)
	if err != nil {
		return fmt.Errorf("get record bytes: %w", err)
	}
	if recordCBOR == nil {
		return fmt.Errorf("nil record bytes")
	}

	recordJSON, err := cborToJSON(*recordCBOR)
	if err != nil {
		return fmt.Errorf("decode cbor to json: %w", err)
	}

	atURI := fmt.Sprintf("at://%s/%s", atDID, path)
	if err := p.ProcessRecord(ctx, atDID, atURI, atCID, collection, recordJSON); err != nil {
		return err
	}
	p.logger.Printf("event=record_mapped did=%s at_uri=%s record_type=%s seq=%d", atDID, atURI, collection, seq)
	return nil
}

func (p *Processor) processDeleteOp(ctx context.Context, atDID, path string, seq int64) error {
	collection, ok := collectionFromPath(path)
	if !ok || !isSupportedCollection(collection) {
		return nil
	}

	atURI := fmt.Sprintf("at://%s/%s", atDID, path)
	existing, err := p.db.GetMessage(ctx, atURI)
	if err != nil {
		return fmt.Errorf("load existing message for delete %s: %w", atURI, err)
	}

	now := time.Now().UTC()
	atCID := fmt.Sprintf("deleted-seq-%d", seq)
	if existing != nil && strings.TrimSpace(existing.ATCID) != "" {
		atCID = existing.ATCID
	}

	tombstone := map[string]interface{}{
		"type":        "bridge/tombstone",
		"bridge_op":   "delete",
		"at_uri":      atURI,
		"record_type": collection,
		"deleted_seq": seq,
	}
	rawSSBJSON, err := json.Marshal(tombstone)
	if err != nil {
		return fmt.Errorf("marshal tombstone payload: %w", err)
	}

	msg := db.Message{
		ATURI:         atURI,
		ATCID:         atCID,
		SSBMsgRef:     "",
		ATDID:         atDID,
		Type:          collection,
		MessageState:  db.MessageStateDeleted,
		RawATJson:     fmt.Sprintf(`{"op":"delete","at_uri":%q,"seq":%d}`, atURI, seq),
		RawSSBJson:    string(rawSSBJSON),
		DeletedAt:     &now,
		DeletedSeq:    &seq,
		DeletedReason: fmt.Sprintf("atproto_delete seq=%d", seq),
	}

	if p.publisher != nil {
		msg.PublishAttempts = 1
		msg.LastPublishAttemptAt = &now
		ssbMsgRef, publishErr := p.publisher.Publish(ctx, atDID, tombstone)
		if publishErr != nil {
			msg.PublishError = fmt.Sprintf("delete_tombstone_publish_failed: %v", publishErr)
			p.logger.Printf("event=delete_tombstone_publish_failed did=%s at_uri=%s seq=%d err=%v", atDID, atURI, seq, publishErr)
		} else {
			publishedAt := time.Now().UTC()
			msg.SSBMsgRef = ssbMsgRef
			msg.PublishedAt = &publishedAt
			p.logger.Printf("event=deleted did=%s at_uri=%s seq=%d ssb_msg_ref=%s", atDID, atURI, seq, ssbMsgRef)
		}
	}

	if err := p.db.AddMessage(ctx, msg); err != nil {
		return fmt.Errorf("persist delete tombstone: %w", err)
	}

	return nil
}

// ProcessRecord maps, resolves, publishes, and persists one ATProto record.
func (p *Processor) ProcessRecord(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
	mapped, err := mapper.MapRecord(collection, recordJSON)
	if err != nil {
		return fmt.Errorf("map record: %w", err)
	}

	p.resolveMappedRefs(ctx, mapped)

	var blobErr error
	if p.blobBridge != nil {
		if err := p.blobBridge.BridgeRecordBlobs(ctx, atDID, mapped, recordJSON); err != nil {
			blobErr = err
			p.logger.Printf("event=blob_bridge_error did=%s at_uri=%s record_type=%s err=%v", atDID, atURI, collection, err)
		}
	}

	rawSSBJSON, err := json.Marshal(mapped)
	if err != nil {
		return fmt.Errorf("marshal mapped record: %w", err)
	}

	msg := db.Message{
		ATURI:        atURI,
		ATCID:        atCID,
		SSBMsgRef:    "",
		ATDID:        atDID,
		Type:         collection,
		MessageState: db.MessageStatePending,
		RawATJson:    string(recordJSON),
		RawSSBJson:   string(rawSSBJSON),
	}

	unresolved := mapper.UnresolvedATProtoRefs(mapped)
	if len(unresolved) > 0 {
		now := time.Now().UTC()
		msg.MessageState = db.MessageStateDeferred
		msg.DeferReason = strings.Join(unresolved, ";")
		msg.DeferAttempts = 1
		msg.LastDeferAttemptAt = &now
		if blobErr != nil {
			msg.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
		}
		p.logger.Printf("event=publish_deferred did=%s at_uri=%s record_type=%s unresolved=%q", atDID, atURI, collection, msg.DeferReason)
	} else if p.publisher != nil {
		attemptedAt := time.Now().UTC()
		msg.PublishAttempts = 1
		msg.LastPublishAttemptAt = &attemptedAt
		ssbMsgRef, publishErr := p.publisher.Publish(ctx, atDID, mapped)
		if publishErr != nil {
			msg.MessageState = db.MessageStateFailed
			msg.PublishError = publishErr.Error()
			if blobErr != nil {
				msg.PublishError = fmt.Sprintf("%s; blob_fallback=%v", msg.PublishError, blobErr)
			}
			p.logger.Printf("event=publish_failed did=%s at_uri=%s record_type=%s err=%v", atDID, atURI, collection, publishErr)
		} else {
			msg.MessageState = db.MessageStatePublished
			now := time.Now().UTC()
			msg.SSBMsgRef = ssbMsgRef
			msg.PublishedAt = &now
			if blobErr != nil {
				msg.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
			}
			p.logger.Printf("event=published did=%s at_uri=%s record_type=%s ssb_msg_ref=%s", atDID, atURI, collection, ssbMsgRef)
		}
	} else if blobErr != nil {
		msg.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
	}

	if err := p.db.AddMessage(ctx, msg); err != nil {
		return fmt.Errorf("persist message: %w", err)
	}

	return nil
}

func (p *Processor) resolveMappedRefs(ctx context.Context, mapped map[string]interface{}) {
	mapper.ReplaceATProtoRefs(mapped,
		func(uri string) string {
			msg, err := p.db.GetMessage(ctx, uri)
			if err != nil {
				p.logger.Printf("uri lookup failed (%s): %v", uri, err)
				return ""
			}
			if msg == nil {
				return ""
			}
			return msg.SSBMsgRef
		},
		func(did string) string {
			acc, err := p.db.GetBridgedAccount(ctx, did)
			if err != nil {
				p.logger.Printf("did lookup failed (%s): %v", did, err)
				return ""
			}
			if acc == nil || !acc.Active {
				return ""
			}
			return acc.SSBFeedID
		},
	)
}

// ResolveDeferredMessages retries unresolved-reference records once dependencies are available.
func (p *Processor) ResolveDeferredMessages(ctx context.Context, limit int) (DeferredResolveResult, error) {
	if limit <= 0 {
		limit = 100
	}

	candidates, err := p.db.GetDeferredCandidates(ctx, limit)
	if err != nil {
		return DeferredResolveResult{}, fmt.Errorf("query deferred candidates: %w", err)
	}

	res := DeferredResolveResult{Selected: len(candidates)}
	for _, candidate := range candidates {
		res.Attempted++

		outcome, resolveErr := p.resolveDeferredMessage(ctx, candidate)
		if resolveErr != nil {
			res.Failed++
			p.logger.Printf("event=deferred_resolve_failed did=%s at_uri=%s err=%v", candidate.ATDID, candidate.ATURI, resolveErr)
			continue
		}

		switch outcome {
		case db.MessageStatePublished:
			res.Published++
		case db.MessageStateDeferred:
			res.Deferred++
		default:
			res.Failed++
		}
	}

	return res, nil
}

func (p *Processor) resolveDeferredMessage(ctx context.Context, msg db.Message) (string, error) {
	mapped, err := mapper.MapRecord(msg.Type, []byte(msg.RawATJson))
	if err != nil {
		return db.MessageStateFailed, fmt.Errorf("map deferred record: %w", err)
	}
	p.resolveMappedRefs(ctx, mapped)
	unresolved := mapper.UnresolvedATProtoRefs(mapped)

	var blobErr error
	if p.blobBridge != nil {
		if err := p.blobBridge.BridgeRecordBlobs(ctx, msg.ATDID, mapped, []byte(msg.RawATJson)); err != nil {
			blobErr = err
		}
	}

	rawSSBJSON, err := json.Marshal(mapped)
	if err != nil {
		return db.MessageStateFailed, fmt.Errorf("marshal deferred mapped record: %w", err)
	}

	now := time.Now().UTC()
	update := db.Message{
		ATURI:              msg.ATURI,
		ATCID:              msg.ATCID,
		SSBMsgRef:          "",
		ATDID:              msg.ATDID,
		Type:               msg.Type,
		MessageState:       db.MessageStateDeferred,
		RawATJson:          msg.RawATJson,
		RawSSBJson:         string(rawSSBJSON),
		PublishError:       "",
		DeferReason:        "",
		DeferAttempts:      1,
		LastDeferAttemptAt: &now,
	}

	if len(unresolved) > 0 {
		update.DeferReason = strings.Join(unresolved, ";")
		if blobErr != nil {
			update.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
		}
		if err := p.db.AddMessage(ctx, update); err != nil {
			return db.MessageStateDeferred, fmt.Errorf("persist deferred unresolved: %w", err)
		}
		return db.MessageStateDeferred, nil
	}

	if p.publisher == nil {
		update.MessageState = db.MessageStatePending
		if blobErr != nil {
			update.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
		}
		if err := p.db.AddMessage(ctx, update); err != nil {
			return db.MessageStatePending, fmt.Errorf("persist deferred pending: %w", err)
		}
		return db.MessageStatePending, nil
	}

	update.PublishAttempts = 1
	update.LastPublishAttemptAt = &now
	update.DeferReason = ""

	ssbMsgRef, publishErr := p.publisher.Publish(ctx, msg.ATDID, mapped)
	if publishErr != nil {
		update.MessageState = db.MessageStateFailed
		update.PublishError = publishErr.Error()
		if blobErr != nil {
			update.PublishError = fmt.Sprintf("%s; blob_fallback=%v", update.PublishError, blobErr)
		}
		if err := p.db.AddMessage(ctx, update); err != nil {
			return db.MessageStateFailed, fmt.Errorf("persist deferred publish failure: %w", err)
		}
		return db.MessageStateFailed, publishErr
	}

	publishedAt := time.Now().UTC()
	update.MessageState = db.MessageStatePublished
	update.SSBMsgRef = ssbMsgRef
	update.PublishedAt = &publishedAt
	if blobErr != nil {
		update.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
	}
	if err := p.db.AddMessage(ctx, update); err != nil {
		return db.MessageStatePublished, fmt.Errorf("persist deferred publish success: %w", err)
	}
	p.logger.Printf("event=deferred_published did=%s at_uri=%s ssb_msg_ref=%s", msg.ATDID, msg.ATURI, ssbMsgRef)
	return db.MessageStatePublished, nil
}

// RetryFailedMessages retries previously failed unpublished records.
func (p *Processor) RetryFailedMessages(ctx context.Context, cfg RetryConfig) (RetryResult, error) {
	if p.publisher == nil {
		return RetryResult{}, fmt.Errorf("retry requires configured publisher")
	}

	if cfg.Limit <= 0 {
		cfg.Limit = 100
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 5 * time.Second
	}

	candidates, err := p.db.GetRetryCandidates(ctx, cfg.Limit, cfg.ATDID, cfg.MaxAttempts)
	if err != nil {
		return RetryResult{}, fmt.Errorf("query retry candidates: %w", err)
	}

	result := RetryResult{Selected: len(candidates)}
	now := time.Now().UTC()
	for _, msg := range candidates {
		if !retryDue(msg, now, cfg.BaseBackoff) {
			result.Deferred++
			continue
		}
		result.Attempted++

		if err := p.retryMessage(ctx, msg); err != nil {
			result.Failed++
			p.logger.Printf("event=retry_failed did=%s at_uri=%s attempts=%d err=%v", msg.ATDID, msg.ATURI, msg.PublishAttempts, err)
			continue
		}

		result.Published++
	}

	return result, nil
}

func (p *Processor) retryMessage(ctx context.Context, msg db.Message) error {
	now := time.Now().UTC()
	update := db.Message{
		ATURI:                msg.ATURI,
		ATCID:                msg.ATCID,
		SSBMsgRef:            "",
		ATDID:                msg.ATDID,
		Type:                 msg.Type,
		MessageState:         db.MessageStateFailed,
		RawATJson:            msg.RawATJson,
		RawSSBJson:           msg.RawSSBJson,
		PublishAttempts:      1,
		LastPublishAttemptAt: &now,
		DeferReason:          "",
		DeferAttempts:        0,
		LastDeferAttemptAt:   nil,
		DeletedAt:            msg.DeletedAt,
		DeletedSeq:           msg.DeletedSeq,
		DeletedReason:        msg.DeletedReason,
	}

	var mapped map[string]interface{}
	if err := json.Unmarshal([]byte(msg.RawSSBJson), &mapped); err != nil {
		update.PublishError = fmt.Sprintf("retry decode mapped payload: %v", err)
		if persistErr := p.db.AddMessage(ctx, update); persistErr != nil {
			return fmt.Errorf("persist retry decode failure: %w", persistErr)
		}
		return err
	}

	ssbMsgRef, err := p.publisher.Publish(ctx, msg.ATDID, mapped)
	if err != nil {
		update.PublishError = err.Error()
		if persistErr := p.db.AddMessage(ctx, update); persistErr != nil {
			return fmt.Errorf("persist retry failure: %w", persistErr)
		}
		return err
	}

	publishedAt := time.Now().UTC()
	update.MessageState = db.MessageStatePublished
	update.SSBMsgRef = ssbMsgRef
	update.PublishedAt = &publishedAt
	update.PublishError = ""
	if err := p.db.AddMessage(ctx, update); err != nil {
		return fmt.Errorf("persist retry success: %w", err)
	}

	p.logger.Printf("event=retry_published did=%s at_uri=%s ssb_msg_ref=%s", msg.ATDID, msg.ATURI, ssbMsgRef)
	return nil
}

func retryDue(msg db.Message, now time.Time, baseBackoff time.Duration) bool {
	if msg.PublishAttempts <= 0 {
		return true
	}

	lastAttempt := msg.CreatedAt
	if msg.LastPublishAttemptAt != nil {
		lastAttempt = *msg.LastPublishAttemptAt
	}

	return !now.Before(lastAttempt.Add(retryBackoff(baseBackoff, msg.PublishAttempts)))
}

func retryBackoff(base time.Duration, attempts int) time.Duration {
	if base <= 0 {
		base = 5 * time.Second
	}
	if attempts <= 1 {
		return base
	}

	backoff := base
	for i := 1; i < attempts; i++ {
		if backoff >= 10*time.Minute {
			return 10 * time.Minute
		}
		backoff *= 2
	}
	if backoff > 10*time.Minute {
		return 10 * time.Minute
	}
	return backoff
}

func collectionFromPath(path string) (string, bool) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func isSupportedCollection(collection string) bool {
	_, ok := supportedCollections[collection]
	return ok
}

func cborToJSON(rawCBOR []byte) ([]byte, error) {
	decoded, err := lexutil.CborDecodeValue(rawCBOR)
	if err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}
