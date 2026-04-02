package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	comatproto "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

const (
	maxDependencyDepth   = 16
	maxDependencyRecords = 256
)

// DependencyResolver ensures referenced ATProto records are available locally.
type DependencyResolver interface {
	EnsureRecord(ctx context.Context, atURI string) error
}

// FeedResolver resolves an ATProto DID to the deterministic SSB feed ref used by the bridge.
type FeedResolver interface {
	ResolveFeed(ctx context.Context, did string) (string, error)
}

// DependencyProcessFunc reprocesses a fetched dependency record through the bridge processor.
type DependencyProcessFunc func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error

// RecordFetcher fetches a single ATProto record by AT URI.
type RecordFetcher interface {
	FetchRecord(ctx context.Context, atURI string) (FetchedRecord, error)
}

// FetchedRecord is one decoded dependency record fetched from ATProto.
type FetchedRecord struct {
	ATDID      string
	ATURI      string
	ATCID      string
	Collection string
	RecordJSON []byte
}

// XRPCRecordFetcher fetches individual records using com.atproto.repo.getRecord
// against a single fixed XRPC host (typically the AppView).
type XRPCRecordFetcher struct {
	client lexutil.LexClient
}

// NewXRPCRecordFetcher constructs a record fetcher backed by an ATProto XRPC client.
func NewXRPCRecordFetcher(client lexutil.LexClient) RecordFetcher {
	return &XRPCRecordFetcher{client: client}
}

// PDSAwareRecordFetcher resolves each DID's PDS endpoint before fetching,
// so dependency records are read from the authoritative PDS rather than a
// single fixed AppView host.
type PDSAwareRecordFetcher struct {
	resolver HostResolver
	fallback lexutil.LexClient
}

// HostResolver resolves an ATProto DID to its PDS endpoint URL.
// This is a local alias so callers in the bridge package do not need to
// import the backfill package directly.
type HostResolver interface {
	ResolvePDSEndpoint(ctx context.Context, did string) (string, error)
}

// NewPDSAwareRecordFetcher constructs a fetcher that resolves each DID's PDS.
// If PDS resolution fails, the fallback client (typically the public AppView)
// is used instead.
func NewPDSAwareRecordFetcher(resolver HostResolver, fallback lexutil.LexClient) RecordFetcher {
	return &PDSAwareRecordFetcher{resolver: resolver, fallback: fallback}
}

// FetchRecord resolves the DID's PDS, then fetches via com.atproto.repo.getRecord.
// Falls back to the AppView client when PDS resolution fails.
func (f *PDSAwareRecordFetcher) FetchRecord(ctx context.Context, atURI string) (FetchedRecord, error) {
	if f == nil {
		return FetchedRecord{}, fmt.Errorf("pds-aware record fetcher is nil")
	}

	parsed, err := syntax.ParseATURI(strings.TrimSpace(atURI))
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("parse dependency at-uri %q: %w", atURI, err)
	}

	collection := parsed.Collection().String()
	rkey := parsed.RecordKey().String()
	repo := parsed.Authority().String()
	if collection == "" || rkey == "" || repo == "" {
		return FetchedRecord{}, fmt.Errorf("dependency at-uri %q is missing repo/collection/rkey", atURI)
	}

	client := f.resolveClient(ctx, repo)

	out, err := comatproto.RepoGetRecord(ctx, client, "", collection, repo, rkey)
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("repo.getRecord %q: %w", atURI, err)
	}
	if out == nil || out.Value == nil || out.Value.Val == nil {
		return FetchedRecord{}, fmt.Errorf("repo.getRecord %q returned no record payload", atURI)
	}

	recordJSON, err := json.Marshal(out.Value.Val)
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("marshal repo.getRecord payload %q: %w", atURI, err)
	}

	fetchedURI := strings.TrimSpace(out.Uri)
	if fetchedURI == "" {
		fetchedURI = parsed.Normalize().String()
	}
	fetchedDID := repo
	if normalized, parseErr := syntax.ParseATURI(fetchedURI); parseErr == nil {
		fetchedDID = normalized.Authority().String()
	}

	atCID := ""
	if out.Cid != nil {
		atCID = strings.TrimSpace(*out.Cid)
	}

	return FetchedRecord{
		ATDID:      fetchedDID,
		ATURI:      fetchedURI,
		ATCID:      atCID,
		Collection: collection,
		RecordJSON: recordJSON,
	}, nil
}

// resolveClient returns a per-PDS XRPC client for the given DID, falling back
// to the AppView client if resolution fails.
func (f *PDSAwareRecordFetcher) resolveClient(ctx context.Context, did string) lexutil.LexClient {
	if f.resolver == nil {
		return f.fallback
	}
	pdsHost, err := f.resolver.ResolvePDSEndpoint(ctx, did)
	if err != nil || strings.TrimSpace(pdsHost) == "" {
		return f.fallback
	}
	return &xrpc.Client{Host: pdsHost}
}

// FetchRecord fetches and JSON-encodes one ATProto record.
func (f *XRPCRecordFetcher) FetchRecord(ctx context.Context, atURI string) (FetchedRecord, error) {
	if f == nil || f.client == nil {
		return FetchedRecord{}, fmt.Errorf("xrpc record fetcher client is nil")
	}

	parsed, err := syntax.ParseATURI(strings.TrimSpace(atURI))
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("parse dependency at-uri %q: %w", atURI, err)
	}

	collection := parsed.Collection().String()
	rkey := parsed.RecordKey().String()
	repo := parsed.Authority().String()
	if collection == "" || rkey == "" || repo == "" {
		return FetchedRecord{}, fmt.Errorf("dependency at-uri %q is missing repo/collection/rkey", atURI)
	}

	out, err := comatproto.RepoGetRecord(ctx, f.client, "", collection, repo, rkey)
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("repo.getRecord %q: %w", atURI, err)
	}
	if out == nil || out.Value == nil || out.Value.Val == nil {
		return FetchedRecord{}, fmt.Errorf("repo.getRecord %q returned no record payload", atURI)
	}

	recordJSON, err := json.Marshal(out.Value.Val)
	if err != nil {
		return FetchedRecord{}, fmt.Errorf("marshal repo.getRecord payload %q: %w", atURI, err)
	}

	fetchedURI := strings.TrimSpace(out.Uri)
	if fetchedURI == "" {
		fetchedURI = parsed.Normalize().String()
	}
	fetchedDID := repo
	if normalized, parseErr := syntax.ParseATURI(fetchedURI); parseErr == nil {
		fetchedDID = normalized.Authority().String()
	}

	atCID := ""
	if out.Cid != nil {
		atCID = strings.TrimSpace(*out.Cid)
	}

	return FetchedRecord{
		ATDID:      fetchedDID,
		ATURI:      fetchedURI,
		ATCID:      atCID,
		Collection: collection,
		RecordJSON: recordJSON,
	}, nil
}

// ATProtoDependencyResolver fetches and reprocesses external record dependencies on demand.
type ATProtoDependencyResolver struct {
	db      *db.DB
	logger  *log.Logger
	fetcher RecordFetcher
	process DependencyProcessFunc

	mu       sync.Mutex
	inflight map[string]*recordDependencyCall
}

type recordDependencyCall struct {
	done chan struct{}
	err  error
}

// NewATProtoDependencyResolver constructs the default resolver used by the bridge runtime.
func NewATProtoDependencyResolver(database *db.DB, logger *log.Logger, fetcher RecordFetcher, process DependencyProcessFunc) DependencyResolver {
	logger = logutil.Ensure(logger)
	return &ATProtoDependencyResolver{
		db:       database,
		logger:   logger,
		fetcher:  fetcher,
		process:  process,
		inflight: make(map[string]*recordDependencyCall),
	}
}

// EnsureRecord makes a dependency record available in the local bridge DB and publish log.
func (r *ATProtoDependencyResolver) EnsureRecord(ctx context.Context, atURI string) error {
	if r == nil {
		return fmt.Errorf("dependency resolver is nil")
	}

	atURI = strings.TrimSpace(atURI)
	if atURI == "" {
		return fmt.Errorf("dependency at-uri is empty")
	}

	childCtx, _, skip, err := beginDependencyRecord(ctx, atURI)
	if err != nil {
		logDependencyEvent(r.logger, ctx, "dependency_fetch_error", atURI, "", "guardrail", err)
		return err
	}
	if skip {
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_skip", atURI, "", "already_visited", nil)
		return nil
	}

	call, wait := r.acquire(atURI)
	if wait {
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_skip", atURI, "", "inflight_wait", nil)
		<-call.done
		return call.err
	}
	defer r.finish(atURI, call, &err)

	msg, lookupErr := r.db.GetMessage(childCtx, atURI)
	if lookupErr != nil {
		err = fmt.Errorf("lookup local dependency %s: %w", atURI, lookupErr)
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", atURI, "", "db_lookup", err)
		return err
	}

	switch {
	case msg != nil && strings.TrimSpace(msg.SSBMsgRef) != "":
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_skip", atURI, msg.ATDID, "local_resolved", nil)
		return nil
	case msg != nil && (msg.MessageState == db.MessageStateDeferred || msg.MessageState == db.MessageStatePending) && strings.TrimSpace(msg.RawATJson) != "" && isSupportedCollection(msg.Type):
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_start", atURI, msg.ATDID, "local_reprocess", nil)
		err = r.process(childCtx, msg.ATDID, msg.ATURI, msg.ATCID, msg.Type, []byte(msg.RawATJson))
		if err != nil {
			logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", atURI, msg.ATDID, "local_reprocess", err)
			return err
		}
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_success", atURI, msg.ATDID, "local_reprocess", nil)
		return nil
	case msg != nil && msg.MessageState == db.MessageStateFailed:
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_skip", atURI, msg.ATDID, "local_failed", nil)
		return nil
	}

	if err = noteDependencyFetch(childCtx); err != nil {
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", atURI, "", "guardrail", err)
		return err
	}

	logDependencyEvent(r.logger, childCtx, "dependency_fetch_start", atURI, "", "remote_fetch", nil)
	fetched, err := r.fetcher.FetchRecord(childCtx, atURI)
	if err != nil {
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", atURI, "", "remote_fetch", err)
		return err
	}
	if !isSupportedCollection(fetched.Collection) {
		err = fmt.Errorf("unsupported dependency collection: %s", fetched.Collection)
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", fetched.ATURI, fetched.ATDID, "remote_fetch", err)
		return err
	}
	if r.process == nil {
		err = fmt.Errorf("dependency processor callback is nil")
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", fetched.ATURI, fetched.ATDID, "remote_fetch", err)
		return err
	}

	err = r.process(childCtx, fetched.ATDID, fetched.ATURI, fetched.ATCID, fetched.Collection, fetched.RecordJSON)
	if err != nil {
		logDependencyEvent(r.logger, childCtx, "dependency_fetch_error", fetched.ATURI, fetched.ATDID, "remote_fetch", err)
		return err
	}

	logDependencyEvent(r.logger, childCtx, "dependency_fetch_success", fetched.ATURI, fetched.ATDID, "remote_fetch", nil)
	return nil
}

func (r *ATProtoDependencyResolver) acquire(atURI string) (*recordDependencyCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if call, ok := r.inflight[atURI]; ok {
		return call, true
	}

	call := &recordDependencyCall{done: make(chan struct{})}
	r.inflight[atURI] = call
	return call, false
}

func (r *ATProtoDependencyResolver) finish(atURI string, call *recordDependencyCall, errp *error) {
	r.mu.Lock()
	if errp != nil && *errp != nil {
		call.err = *errp
	}
	delete(r.inflight, atURI)
	close(call.done)
	r.mu.Unlock()
}

type dependencyFrame struct {
	scope *dependencyScope
	depth int
}

type dependencyScope struct {
	rootDID        string
	rootURI        string
	visitedRecords map[string]struct{}
	fetchedRecords int
	mu             sync.Mutex
}

type dependencyFrameKey struct{}
type dependencyReasonKey struct{}

func ensureDependencyContext(ctx context.Context, rootDID, rootURI string) context.Context {
	if _, ok := dependencyFrameFromContext(ctx); ok {
		return ctx
	}

	scope := &dependencyScope{
		rootDID:        strings.TrimSpace(rootDID),
		rootURI:        strings.TrimSpace(rootURI),
		visitedRecords: make(map[string]struct{}),
	}
	if scope.rootURI != "" {
		scope.visitedRecords[scope.rootURI] = struct{}{}
	}

	return context.WithValue(ctx, dependencyFrameKey{}, &dependencyFrame{
		scope: scope,
		depth: 0,
	})
}

func withDependencyReason(ctx context.Context, reasonKey string) context.Context {
	reasonKey = strings.TrimSpace(reasonKey)
	if reasonKey == "" {
		return ctx
	}
	return context.WithValue(ctx, dependencyReasonKey{}, reasonKey)
}

func beginDependencyRecord(ctx context.Context, atURI string) (context.Context, *dependencyFrame, bool, error) {
	frame, ok := dependencyFrameFromContext(ctx)
	if !ok {
		ctx = ensureDependencyContext(ctx, "", "")
		frame, _ = dependencyFrameFromContext(ctx)
	}
	if frame == nil || frame.scope == nil {
		return ctx, nil, false, fmt.Errorf("dependency resolution context is missing")
	}
	if frame.depth+1 > maxDependencyDepth {
		return ctx, frame, false, fmt.Errorf("dependency depth exceeded for %s (max=%d)", atURI, maxDependencyDepth)
	}

	frame.scope.mu.Lock()
	if _, exists := frame.scope.visitedRecords[atURI]; exists {
		frame.scope.mu.Unlock()
		return ctx, frame, true, nil
	}
	frame.scope.visitedRecords[atURI] = struct{}{}
	frame.scope.mu.Unlock()

	child := &dependencyFrame{
		scope: frame.scope,
		depth: frame.depth + 1,
	}
	return context.WithValue(ctx, dependencyFrameKey{}, child), child, false, nil
}

func noteDependencyFetch(ctx context.Context) error {
	frame, ok := dependencyFrameFromContext(ctx)
	if !ok || frame == nil || frame.scope == nil {
		return nil
	}

	frame.scope.mu.Lock()
	defer frame.scope.mu.Unlock()

	if frame.scope.fetchedRecords >= maxDependencyRecords {
		return fmt.Errorf("dependency fetch limit exceeded for %s (max=%d)", frame.scope.rootURI, maxDependencyRecords)
	}
	frame.scope.fetchedRecords++
	return nil
}

func dependencyFrameFromContext(ctx context.Context) (*dependencyFrame, bool) {
	frame, ok := ctx.Value(dependencyFrameKey{}).(*dependencyFrame)
	return frame, ok
}

func dependencyReasonFromContext(ctx context.Context) string {
	reason, _ := ctx.Value(dependencyReasonKey{}).(string)
	return strings.TrimSpace(reason)
}

func logDependencyEvent(logger *log.Logger, ctx context.Context, event, dependencyURI, dependencyDID, note string, err error) {
	logger = logutil.Ensure(logger)

	rootDID := ""
	rootURI := ""
	depth := 0
	if frame, ok := dependencyFrameFromContext(ctx); ok && frame != nil && frame.scope != nil {
		rootDID = frame.scope.rootDID
		rootURI = frame.scope.rootURI
		depth = frame.depth
	}
	reasonKey := dependencyReasonFromContext(ctx)

	if err != nil {
		logger.Printf(
			"event=%s root_did=%s root_at_uri=%s dependency_at_uri=%s dependency_did=%s depth=%d reason_key=%s note=%s err=%v",
			event,
			rootDID,
			rootURI,
			strings.TrimSpace(dependencyURI),
			strings.TrimSpace(dependencyDID),
			depth,
			reasonKey,
			strings.TrimSpace(note),
			err,
		)
		return
	}

	logger.Printf(
		"event=%s root_did=%s root_at_uri=%s dependency_at_uri=%s dependency_did=%s depth=%d reason_key=%s note=%s",
		event,
		rootDID,
		rootURI,
		strings.TrimSpace(dependencyURI),
		strings.TrimSpace(dependencyDID),
		depth,
		reasonKey,
		strings.TrimSpace(note),
	)
}
