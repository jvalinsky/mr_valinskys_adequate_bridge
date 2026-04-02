// Package bridge coordinates firehose ingestion, mapping, publishing, and persistence.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
)

var supportedCollections = map[string]struct{}{
	mapper.RecordTypePost:    {},
	mapper.RecordTypeLike:    {},
	mapper.RecordTypeFollow:  {},
	mapper.RecordTypeBlock:   {},
	mapper.RecordTypeProfile: {},
}

// Database defines the persistence surface required by the bridge processor.
type Database interface {
	GetBridgedAccount(ctx context.Context, atDID string) (*db.BridgedAccount, error)
	AddMessage(ctx context.Context, msg db.Message) error
	GetMessage(ctx context.Context, atURI string) (*db.Message, error)
	SetBridgeState(ctx context.Context, key, value string) error
	GetDeferredCandidates(ctx context.Context, limit int) ([]db.Message, error)
	GetRetryCandidates(ctx context.Context, limit int, atDID string, maxAttempts int) ([]db.Message, error)
}

// Processor processes ATProto commits into persisted and optionally published SSB messages.
type Processor struct {
	db                 Database
	logger             *log.Logger
	publisher          Publisher
	blobBridge         BlobBridge
	dependencyResolver DependencyResolver
	feedResolver       FeedResolver

	feedMu       sync.Mutex
	feedInFlight map[string]*feedResolutionCall
}

// NewProcessor constructs a Processor with optional publisher/blob integrations.
func NewProcessor(database Database, logger *log.Logger, opts ...Option) *Processor {
	logger = logutil.Ensure(logger)
	proc := &Processor{
		db:           database,
		logger:       logger,
		feedInFlight: make(map[string]*feedResolutionCall),
	}
	for _, opt := range opts {
		opt(proc)
	}
	return proc
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
	BridgeRecordBlobs(ctx context.Context, atDID, collection string, mapped map[string]interface{}, rawRecordJSON []byte) error
}

type feedResolutionCall struct {
	done chan struct{}
	ref  string
	err  error
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

// WithDependencyResolver sets the record dependency resolver used by Processor.
func WithDependencyResolver(resolver DependencyResolver) Option {
	return func(proc *Processor) {
		proc.dependencyResolver = resolver
	}
}

// WithFeedResolver sets the DID-to-feed resolver used by Processor.
func WithFeedResolver(resolver FeedResolver) Option {
	return func(proc *Processor) {
		proc.feedResolver = resolver
	}
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

	var rr *atrepo.Repo

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

func (p *Processor) processOp(ctx context.Context, rr *atrepo.Repo, atDID, path, atCID string, seq int64) error {
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
	return p.ProcessDeletedRecord(ctx, atDID, atURI, "", collection, nil, seq)
}

// HandleRecordEvent processes a normalized atindex record event.
func (p *Processor) HandleRecordEvent(ctx context.Context, event db.ATProtoRecordEvent) error {
	seq := int64(0)
	if event.Seq != nil {
		seq = *event.Seq
	}
	if event.Action == "delete" {
		return p.ProcessDeletedRecord(ctx, event.DID, event.ATURI, event.ATCID, event.Collection, []byte(event.RecordJSON), seq)
	}
	return p.ProcessRecord(ctx, event.DID, event.ATURI, event.ATCID, event.Collection, []byte(event.RecordJSON))
}

// ProcessDeletedRecord translates a normalized delete event into a bridge message update.
func (p *Processor) ProcessDeletedRecord(ctx context.Context, atDID, atURI, atCID, collection string, rawRecordJSON []byte, seq int64) error {
	existing, err := p.db.GetMessage(ctx, atURI)
	if err != nil {
		return fmt.Errorf("load existing message for delete %s: %w", atURI, err)
	}

	now := time.Now().UTC()
	if strings.TrimSpace(atCID) == "" {
		if existing != nil && strings.TrimSpace(existing.ATCID) != "" {
			atCID = existing.ATCID
		} else {
			atCID = fmt.Sprintf("deleted-seq-%d", seq)
		}
	}

	rawATJSON := fmt.Sprintf(`{"op":"delete","at_uri":%q,"seq":%d}`, atURI, seq)
	rawSSBJSON := ""
	if existing != nil {
		if strings.TrimSpace(existing.RawATJson) != "" {
			rawATJSON = existing.RawATJson
		}
		rawSSBJSON = existing.RawSSBJson
	}

	msg := db.Message{
		ATURI:         atURI,
		ATCID:         atCID,
		SSBMsgRef:     "",
		ATDID:         atDID,
		Type:          collection,
		MessageState:  db.MessageStateDeleted,
		RawATJson:     rawATJSON,
		RawSSBJson:    rawSSBJSON,
		DeletedAt:     &now,
		DeletedSeq:    &seq,
		DeletedReason: fmt.Sprintf("atproto_delete seq=%d", seq),
	}

	if len(rawRecordJSON) > 0 {
		rawATJSON = string(rawRecordJSON)
	}

	if collection == mapper.RecordTypeProfile || collection == mapper.RecordTypePost || strings.TrimSpace(rawATJSON) == "" || strings.Contains(rawATJSON, `"op":"delete"`) {
		if err := p.db.AddMessage(ctx, msg); err != nil {
			return fmt.Errorf("persist deleted record: %w", err)
		}
		return nil
	}

	mapped, err := p.mapMappedRecord(ctx, atDID, collection, []byte(rawATJSON), true)
	if err != nil {
		return fmt.Errorf("map delete reversal: %w", err)
	}

	rawDeleteJSON, err := json.Marshal(mapped)
	if err != nil {
		return fmt.Errorf("marshal delete reversal: %w", err)
	}
	msg.RawSSBJson = string(rawDeleteJSON)

	unresolved := mapper.UnresolvedATProtoRefs(mapped)
	if len(unresolved) > 0 {
		msg.MessageState = db.MessageStateDeferred
		msg.DeferReason = strings.Join(unresolved, ";")
		msg.DeferAttempts = 1
		msg.LastDeferAttemptAt = &now
	} else {
		msg.MessageState = db.MessageStatePending
	}
	if len(unresolved) == 0 && p.publisher != nil {
		msg.PublishAttempts = 1
		msg.LastPublishAttemptAt = &now
		ssbMsgRef, publishErr := p.publisher.Publish(ctx, atDID, mapped)
		if publishErr != nil {
			msg.MessageState = db.MessageStateFailed
			msg.PublishError = publishErr.Error()
			p.logger.Printf("event=delete_publish_failed did=%s at_uri=%s seq=%d err=%v", atDID, atURI, seq, publishErr)
		} else {
			publishedAt := time.Now().UTC()
			msg.MessageState = db.MessageStatePublished
			msg.SSBMsgRef = ssbMsgRef
			msg.PublishedAt = &publishedAt
			p.logger.Printf("event=deleted did=%s at_uri=%s seq=%d ssb_msg_ref=%s", atDID, atURI, seq, ssbMsgRef)
		}
	}

	if err := p.db.AddMessage(ctx, msg); err != nil {
		return fmt.Errorf("persist deleted record: %w", err)
	}

	return nil
}

// ProcessRecord maps, resolves, publishes, and persists one ATProto record.
func (p *Processor) ProcessRecord(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
	ctx = ensureDependencyContext(ctx, atDID, atURI)

	mapped, err := p.mapMappedRecord(ctx, atDID, collection, recordJSON, false)
	if err != nil {
		return fmt.Errorf("map record: %w", err)
	}

	var blobErr error
	if p.blobBridge != nil {
		if err := p.blobBridge.BridgeRecordBlobs(ctx, atDID, collection, mapped, recordJSON); err != nil {
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
		// Strip internal bridge fields before publishing to avoid crashing
		// Planetary's strict Codable decoders during FFI batch processing.
		mapper.SanitizeForPublish(mapped)
		if !mapper.ReadyForPublish(mapped) {
			// Required fields missing (e.g. contact without target, vote without link).
			// Keep as deferred rather than publishing a malformed message.
			now := time.Now().UTC()
			msg.MessageState = db.MessageStateDeferred
			msg.DeferReason = "missing_required_fields_after_sanitize"
			msg.DeferAttempts = 1
			msg.LastDeferAttemptAt = &now
			p.logger.Printf("event=publish_deferred_incomplete did=%s at_uri=%s record_type=%s", atDID, atURI, collection)
		} else {
			rawSSBJSON, marshalErr := json.Marshal(mapped)
			if marshalErr == nil {
				msg.RawSSBJson = string(rawSSBJSON)
			}
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
		}
	} else if blobErr != nil {
		msg.PublishError = fmt.Sprintf("blob_fallback=%v", blobErr)
	}

	if err := p.db.AddMessage(ctx, msg); err != nil {
		return fmt.Errorf("persist message: %w", err)
	}

	return nil
}

func (p *Processor) mapMappedRecord(ctx context.Context, atDID, collection string, recordJSON []byte, deleted bool) (map[string]interface{}, error) {
	var (
		mapped map[string]interface{}
		err    error
	)
	if deleted {
		mapped, err = mapDeleteRecord(atDID, collection, recordJSON)
	} else {
		mapped, err = mapper.MapRecord(collection, atDID, recordJSON)
	}
	if err != nil {
		return nil, err
	}

	p.resolveMappedRefs(ctx, mapped)
	if err := p.hydrateRecordDependencies(ctx, mapped); err != nil {
		return nil, fmt.Errorf("hydrate dependencies: %w", err)
	}
	p.resolveMappedRefs(ctx, mapped)
	return mapped, nil
}

func mapDeleteRecord(atDID, collection string, recordJSON []byte) (map[string]interface{}, error) {
	mapped, err := mapper.MapRecord(collection, atDID, recordJSON)
	if err != nil {
		return nil, err
	}

	switch collection {
	case mapper.RecordTypeLike:
		vote, ok := mapped["vote"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("mapped like delete missing vote payload")
		}
		vote["value"] = 0
		vote["expression"] = "Like"
	case mapper.RecordTypeFollow, mapper.RecordTypeBlock:
		mapped["following"] = false
		mapped["blocking"] = false
	default:
		return nil, fmt.Errorf("record type does not support delete translation: %s", collection)
	}

	return mapped, nil
}

func (p *Processor) resolveMappedRefs(ctx context.Context, mapped map[string]interface{}) {
	mapper.ReplaceATProtoRefs(mapped,
		func(uri string) string {
			return p.resolveMessageReference(ctx, uri)
		},
		func(did string) string {
			return p.resolveFeedReference(ctx, did)
		},
	)
}

// ResolveDeferredMessages retries unresolved-reference records once dependencies are available.
// It uses cascading resolution: when a record is successfully published, any other
// candidates in the batch that depend on it are immediately re-attempted. This collapses
// chain resolution (e.g. reply threads A→B→C where all are deferred) into a single pass.
func (p *Processor) ResolveDeferredMessages(ctx context.Context, limit int) (DeferredResolveResult, error) {
	if limit <= 0 {
		limit = 100
	}

	candidates, err := p.db.GetDeferredCandidates(ctx, limit)
	if err != nil {
		return DeferredResolveResult{}, fmt.Errorf("query deferred candidates: %w", err)
	}

	// Build reverse index: dependency URI → indices of candidates that need it.
	depIndex := make(map[string][]int)
	for i, c := range candidates {
		for _, uri := range parseDeferReasonURIs(c.DeferReason) {
			depIndex[uri] = append(depIndex[uri], i)
		}
	}

	resolved := make(map[int]bool, len(candidates))
	res := DeferredResolveResult{Selected: len(candidates)}

	// resolveAt attempts resolution of the candidate at index i, cascading on success.
	var resolveAt func(i int)
	resolveAt = func(i int) {
		if resolved[i] {
			return
		}
		resolved[i] = true
		res.Attempted++

		outcome, resolveErr := p.resolveDeferredMessage(ctx, candidates[i])
		if resolveErr != nil {
			res.Failed++
			p.logger.Printf("event=deferred_resolve_failed did=%s at_uri=%s err=%v", candidates[i].ATDID, candidates[i].ATURI, resolveErr)
			return
		}

		switch outcome {
		case db.MessageStatePublished:
			res.Published++
			// Cascade: re-attempt any batch candidates that depend on this record.
			for _, depIdx := range depIndex[candidates[i].ATURI] {
				resolveAt(depIdx)
			}
		case db.MessageStateDeferred:
			res.Deferred++
		default:
			res.Failed++
		}
	}

	for i := range candidates {
		resolveAt(i)
	}

	return res, nil
}

// parseDeferReasonURIs extracts AT URIs from a defer_reason string.
// The format is "_atproto_key=at://...;_atproto_key2=at://..." with semicolons
// separating multiple key=value pairs.
func parseDeferReasonURIs(reason string) []string {
	if reason == "" {
		return nil
	}
	var uris []string
	for _, part := range strings.Split(reason, ";") {
		if idx := strings.Index(part, "at://"); idx >= 0 {
			uris = append(uris, part[idx:])
		}
	}
	return uris
}

func (p *Processor) resolveDeferredMessage(ctx context.Context, msg db.Message) (string, error) {
	ctx = ensureDependencyContext(ctx, msg.ATDID, msg.ATURI)

	mapped, err := p.mapMappedRecord(ctx, msg.ATDID, msg.Type, []byte(msg.RawATJson), msg.DeletedAt != nil)
	if err != nil {
		return db.MessageStateFailed, fmt.Errorf("map deferred record: %w", err)
	}
	unresolved := mapper.UnresolvedATProtoRefs(mapped)

	var blobErr error
	if p.blobBridge != nil && msg.DeletedAt == nil {
		if err := p.blobBridge.BridgeRecordBlobs(ctx, msg.ATDID, msg.Type, mapped, []byte(msg.RawATJson)); err != nil {
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
		DeletedAt:          msg.DeletedAt,
		DeletedSeq:         msg.DeletedSeq,
		DeletedReason:      msg.DeletedReason,
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

	// Strip internal bridge fields and validate before publishing.
	mapper.SanitizeForPublish(mapped)
	if !mapper.ReadyForPublish(mapped) {
		update.DeferReason = "missing_required_fields_after_sanitize"
		if err := p.db.AddMessage(ctx, update); err != nil {
			return db.MessageStateDeferred, fmt.Errorf("persist deferred incomplete: %w", err)
		}
		return db.MessageStateDeferred, nil
	}

	rawSSBJSON, marshalErr := json.Marshal(mapped)
	if marshalErr == nil {
		update.RawSSBJson = string(rawSSBJSON)
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

func (p *Processor) hydrateRecordDependencies(ctx context.Context, mapped map[string]interface{}) error {
	if p == nil {
		return fmt.Errorf("processor is nil")
	}
	if p.dependencyResolver == nil {
		return nil
	}

	for _, dep := range unresolvedDependencies(mapped) {
		switch dep.Key {
		case "_atproto_subject", "_atproto_quote_subject", "_atproto_reply_root", "_atproto_reply_parent":
			_ = p.dependencyResolver.EnsureRecord(withDependencyReason(ctx, dep.Key), dep.Value)
		}
	}
	return nil
}

func (p *Processor) resolveMessageReference(ctx context.Context, uri string) string {
	msg, err := p.db.GetMessage(ctx, uri)
	if err != nil {
		p.logger.Printf("uri lookup failed (%s): %v", uri, err)
		return ""
	}
	if msg == nil {
		return ""
	}
	return strings.TrimSpace(msg.SSBMsgRef)
}

func (p *Processor) resolveFeedReference(ctx context.Context, did string) string {
	acc, err := p.db.GetBridgedAccount(ctx, did)
	if err != nil {
		p.logger.Printf("did lookup failed (%s): %v", did, err)
		return ""
	}
	if acc != nil && acc.Active && strings.TrimSpace(acc.SSBFeedID) != "" {
		logDependencyEvent(p.logger, ctx, "dependency_fetch_skip", "", did, "local_account", nil)
		return acc.SSBFeedID
	}
	if p.feedResolver == nil {
		logDependencyEvent(p.logger, ctx, "dependency_fetch_skip", "", did, "no_feed_resolver", nil)
		return ""
	}

	ref, err := p.lookupFeed(ctx, did)
	if err != nil {
		logDependencyEvent(p.logger, ctx, "dependency_fetch_error", "", did, "resolve_feed", err)
		return ""
	}
	return ref
}

func (p *Processor) lookupFeed(ctx context.Context, did string) (string, error) {
	did = strings.TrimSpace(did)
	if did == "" {
		return "", fmt.Errorf("dependency did is empty")
	}

	call, wait := p.acquireFeedLookup(did)
	if wait {
		logDependencyEvent(p.logger, ctx, "dependency_fetch_skip", "", did, "inflight_wait", nil)
		<-call.done
		return call.ref, call.err
	}
	defer p.finishFeedLookup(did, call)

	logDependencyEvent(p.logger, ctx, "dependency_fetch_start", "", did, "resolve_feed_start", nil)
	ref, err := p.feedResolver.ResolveFeed(ctx, did)
	call.ref = ref
	call.err = err
	if err != nil {
		return "", err
	}
	logDependencyEvent(p.logger, ctx, "dependency_fetch_success", "", did, "resolve_feed_success", nil)
	return ref, nil
}

func (p *Processor) acquireFeedLookup(did string) (*feedResolutionCall, bool) {
	p.feedMu.Lock()
	defer p.feedMu.Unlock()

	if call, ok := p.feedInFlight[did]; ok {
		return call, true
	}

	call := &feedResolutionCall{done: make(chan struct{})}
	p.feedInFlight[did] = call
	return call, false
}

func (p *Processor) finishFeedLookup(did string, call *feedResolutionCall) {
	p.feedMu.Lock()
	delete(p.feedInFlight, did)
	close(call.done)
	p.feedMu.Unlock()
}

type unresolvedDependency struct {
	Key   string
	Value string
}

func unresolvedDependencies(mapped map[string]interface{}) []unresolvedDependency {
	keys := []string{
		"_atproto_reply_root",
		"_atproto_reply_parent",
		"_atproto_subject",
		"_atproto_quote_subject",
		"_atproto_contact",
	}

	deps := make([]unresolvedDependency, 0, len(keys))
	for _, key := range keys {
		raw, ok := mapped[key]
		if !ok {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(raw))
		if value == "" {
			continue
		}
		deps = append(deps, unresolvedDependency{
			Key:   key,
			Value: value,
		})
	}
	return deps
}
