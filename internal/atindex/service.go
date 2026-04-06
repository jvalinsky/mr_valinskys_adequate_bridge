package atindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
)

const (
	StatePending        = "pending"
	StateBackfilling    = "backfilling"
	StateSynced         = "synced"
	StateDesynchronized = "desynchronized"
	StateDeleted        = "deleted"
	StateDeactivated    = "deactivated"
	StateTakendown      = "takendown"
	StateSuspended      = "suspended"
	StateError          = "error"
)

type EventKind string

const (
	EventKindRecord   EventKind = "record"
	EventKindIdentity EventKind = "identity"
	EventKindAccount  EventKind = "account"
)

type Notification struct {
	Kind     EventKind
	Cursor   int64
	Record   *db.ATProtoRecordEvent
	Identity *IdentityNotification
	Account  *AccountNotification
}

type IdentityNotification struct {
	DID    string
	Handle string
	Seq    int64
	Time   string
}

type AccountNotification struct {
	DID    string
	Active bool
	Status string
	Seq    int64
	Time   string
}

type Service struct {
	db        *db.DB
	resolver  backfill.HostResolver
	fetcher   backfill.RepoFetcher
	logger    *log.Logger
	sourceKey string
	relayURL  string

	queueMu sync.Mutex
	queued  map[string]struct{}
	queue   chan string

	subMu       sync.Mutex
	nextSubID   int
	subscribers map[int]chan Notification
}

func New(database *db.DB, resolver backfill.HostResolver, fetcher backfill.RepoFetcher, relayURL string, logger *log.Logger) *Service {
	return &Service{
		db:          database,
		resolver:    resolver,
		fetcher:     fetcher,
		logger:      logutil.Ensure(logger),
		sourceKey:   "default-relay",
		relayURL:    strings.TrimSpace(relayURL),
		queued:      make(map[string]struct{}),
		queue:       make(chan string, 1024),
		subscribers: make(map[int]chan Notification),
	}
}

func (s *Service) QueueDepth() int {
	return len(s.queue)
}

func (s *Service) Start(ctx context.Context) {
	go s.runWorker(ctx)
}

func (s *Service) TrackRepo(ctx context.Context, did, reason string) error {
	did = strings.TrimSpace(did)
	if did == "" {
		return fmt.Errorf("track repo requires did")
	}

	repoInfo, err := s.db.GetATProtoRepo(ctx, did)
	if err != nil {
		return err
	}

	if repoInfo == nil {
		repoInfo = &db.ATProtoRepo{
			DID:        did,
			Tracking:   true,
			Reason:     strings.TrimSpace(reason),
			SyncState:  StatePending,
			Generation: 1,
		}
		if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
			return err
		}
		s.enqueue(did)
		return nil
	}

	repoInfo.Tracking = true
	if strings.TrimSpace(reason) != "" {
		repoInfo.Reason = strings.TrimSpace(reason)
	}
	if repoInfo.Generation <= 0 {
		repoInfo.Generation = 1
	}
	if repoInfo.SyncState == "" || !isKnownSyncState(repoInfo.SyncState) {
		repoInfo.SyncState = StatePending
	}
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}
	if shouldQueueTrack(repoInfo.SyncState) {
		s.enqueue(did)
	}
	return nil
}

func (s *Service) RequestResync(ctx context.Context, did, reason string) error {
	did = strings.TrimSpace(did)
	if did == "" {
		return fmt.Errorf("resync repo requires did")
	}

	repoInfo, err := s.db.GetATProtoRepo(ctx, did)
	if err != nil {
		return err
	}

	if repoInfo == nil {
		repoInfo = &db.ATProtoRepo{
			DID:        did,
			Tracking:   true,
			Reason:     strings.TrimSpace(reason),
			SyncState:  StatePending,
			Generation: 1,
		}
	} else {
		repoInfo.Tracking = true
		if strings.TrimSpace(reason) != "" {
			repoInfo.Reason = strings.TrimSpace(reason)
		}
		if repoInfo.Generation <= 0 {
			repoInfo.Generation = 1
		} else {
			repoInfo.Generation++
		}
		repoInfo.SyncState = StatePending
		repoInfo.LastError = ""
	}

	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}
	s.enqueue(did)
	return nil
}

func (s *Service) UntrackRepo(ctx context.Context, did string) error {
	repoInfo, err := s.db.GetATProtoRepo(ctx, did)
	if err != nil {
		return err
	}
	if repoInfo == nil {
		return nil
	}
	repoInfo.Tracking = false
	return s.db.UpsertATProtoRepo(ctx, *repoInfo)
}

func (s *Service) GetRepoInfo(ctx context.Context, did string) (*db.ATProtoRepo, error) {
	return s.db.GetATProtoRepo(ctx, did)
}

func (s *Service) GetRecord(ctx context.Context, atURI string) (*db.ATProtoRecord, error) {
	return s.db.GetATProtoRecord(ctx, atURI)
}

func (s *Service) ListRecords(ctx context.Context, did, collection, cursor string, limit int) ([]db.ATProtoRecord, error) {
	return s.db.ListATProtoRecords(ctx, did, collection, cursor, limit)
}

func (s *Service) Subscribe(ctx context.Context, cursor int64) (<-chan Notification, error) {
	live := make(chan Notification, 256)

	s.subMu.Lock()
	id := s.nextSubID
	s.nextSubID++
	s.subscribers[id] = live
	s.subMu.Unlock()

	out := make(chan Notification, 256)
	go func() {
		defer func() {
			s.subMu.Lock()
			delete(s.subscribers, id)
			s.subMu.Unlock()
			close(out)
		}()

		nextCursor := cursor
		for {
			events, err := s.db.ListATProtoEventsAfter(ctx, nextCursor, 256)
			if err != nil {
				s.logger.Printf("event=atindex_subscribe_replay_error cursor=%d err=%v", nextCursor, err)
				return
			}
			if len(events) == 0 {
				break
			}
			for _, event := range events {
				nextCursor = event.Cursor
				select {
				case <-ctx.Done():
					return
				case out <- Notification{Kind: EventKindRecord, Cursor: event.Cursor, Record: &event}:
				default:
					s.logger.Printf("event=atindex_subscribe_consumer_overflow stage=replay cursor=%d", event.Cursor)
					return
				}
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case note, ok := <-live:
				if !ok {
					return
				}
				if note.Kind == EventKindRecord {
					if note.Cursor <= nextCursor {
						continue
					}
					nextCursor = note.Cursor
				}
				select {
				case <-ctx.Done():
					return
				case out <- note:
				default:
					s.logger.Printf("event=atindex_subscribe_consumer_overflow stage=live kind=%s cursor=%d", note.Kind, note.Cursor)
					return
				}
			}
		}
	}()

	return out, nil
}

func (s *Service) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	if evt == nil || strings.TrimSpace(evt.Repo) == "" {
		return nil
	}

	repoInfo, err := s.ensureTracked(ctx, evt.Repo)
	if err != nil {
		return err
	}
	if repoInfo == nil {
		return s.updateSourceCursor(ctx, evt.Seq)
	}

	if repoInfo.Generation <= 0 {
		repoInfo.Generation = 1
	}

	if repoInfo.SyncState != StateSynced {
		if err := s.bufferCommit(ctx, evt, repoInfo.Generation); err != nil {
			return err
		}
		return s.updateSourceCursor(ctx, evt.Seq)
	}

	if repoInfo.CurrentRev != "" {
		if repoInfo.CurrentRev == evt.Rev && strings.TrimSpace(repoInfo.CurrentCommitCID) == evt.Commit.String() {
			return s.updateSourceCursor(ctx, evt.Seq)
		}
		if evt.Since == nil || strings.TrimSpace(*evt.Since) != repoInfo.CurrentRev {
			if err := s.markDesynchronized(ctx, repoInfo, evt, fmt.Sprintf("commit continuity break current=%s since=%v", repoInfo.CurrentRev, evt.Since)); err != nil {
				return err
			}
			return s.updateSourceCursor(ctx, evt.Seq)
		}
	}

	if err := s.applyCommit(ctx, repoInfo, evt, true); err != nil {
		return err
	}
	return s.updateSourceCursor(ctx, evt.Seq)
}

func (s *Service) HandleIdentity(ctx context.Context, evt *atproto.SyncSubscribeRepos_Identity) error {
	if evt == nil || strings.TrimSpace(evt.Did) == "" {
		return nil
	}
	repoInfo, err := s.ensureTracked(ctx, evt.Did)
	if err != nil {
		return err
	}
	if repoInfo == nil {
		return s.updateSourceCursor(ctx, evt.Seq)
	}

	if evt.Handle != nil {
		repoInfo.Handle = strings.TrimSpace(*evt.Handle)
	}
	now := time.Now().UTC()
	repoInfo.LastIdentityAt = &now
	if repoInfo.PDSURL == "" && s.resolver != nil {
		if endpoint, err := s.resolver.ResolvePDSEndpoint(ctx, evt.Did); err == nil {
			repoInfo.PDSURL = endpoint
		}
	}
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}
	if err := s.updateSourceCursor(ctx, evt.Seq); err != nil {
		return err
	}

	s.broadcast(Notification{
		Kind: EventKindIdentity,
		Identity: &IdentityNotification{
			DID:    evt.Did,
			Handle: repoInfo.Handle,
			Seq:    evt.Seq,
			Time:   evt.Time,
		},
	})
	return nil
}

func (s *Service) HandleAccount(ctx context.Context, evt *atproto.SyncSubscribeRepos_Account) error {
	if evt == nil || strings.TrimSpace(evt.Did) == "" {
		return nil
	}
	repoInfo, err := s.ensureTracked(ctx, evt.Did)
	if err != nil {
		return err
	}
	if repoInfo == nil {
		return s.updateSourceCursor(ctx, evt.Seq)
	}

	now := time.Now().UTC()
	repoInfo.AccountActive = &evt.Active
	repoInfo.LastAccountAt = &now
	repoInfo.LastFirehoseSeq = &evt.Seq
	if evt.Status != nil {
		repoInfo.AccountStatus = strings.TrimSpace(*evt.Status)
		switch repoInfo.AccountStatus {
		case StateDeleted:
			repoInfo.SyncState = StateDeleted
		case StateDeactivated:
			repoInfo.SyncState = StateDeactivated
		case StateTakendown:
			repoInfo.SyncState = StateTakendown
		case StateSuspended:
			repoInfo.SyncState = StateSuspended
		}
	}
	if evt.Active && repoInfo.SyncState == "" {
		repoInfo.SyncState = StatePending
	}
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}
	if err := s.updateSourceCursor(ctx, evt.Seq); err != nil {
		return err
	}

	s.broadcast(Notification{
		Kind: EventKindAccount,
		Account: &AccountNotification{
			DID:    evt.Did,
			Active: evt.Active,
			Status: repoInfo.AccountStatus,
			Seq:    evt.Seq,
			Time:   evt.Time,
		},
	})
	return nil
}

func (s *Service) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case did := <-s.queue:
			s.dequeue(did)
			if err := s.runBackfill(ctx, did); err != nil {
				s.logger.Printf("event=atindex_backfill_error did=%s err=%v", did, err)
			}
		}
	}
}

func (s *Service) runBackfill(ctx context.Context, did string) error {
	repoInfo, err := s.db.GetATProtoRepo(ctx, did)
	if err != nil || repoInfo == nil {
		return err
	}
	if !repoInfo.Tracking {
		return nil
	}

	repoInfo.SyncState = StateBackfilling
	repoInfo.LastError = ""
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}

	if s.resolver == nil || s.fetcher == nil {
		return s.failRepo(ctx, repoInfo, fmt.Errorf("backfill resolver/fetcher not configured"))
	}

	endpoint, err := s.resolver.ResolvePDSEndpoint(ctx, did)
	if err != nil {
		return s.failRepo(ctx, repoInfo, err)
	}
	repoInfo.PDSURL = endpoint

	carBytes, err := s.fetcher.FetchRepo(ctx, endpoint, did)
	if err != nil {
		return s.failRepo(ctx, repoInfo, err)
	}

	repoSnapshot, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(carBytes))
	if err != nil {
		return s.failRepo(ctx, repoInfo, err)
	}

	var lastCursor int64
	err = repoSnapshot.ForEach(ctx, "", func(path string, _ cid.Cid) error {
		record, event, err := s.recordFromRepoPath(ctx, repoSnapshot, repoInfo.DID, path, repoSnapshot.SignedCommit().Rev, nil, false)
		if err != nil {
			return err
		}
		if record == nil || event == nil {
			return nil
		}
		if err := s.db.UpsertATProtoRecord(ctx, *record); err != nil {
			return err
		}
		cursor, err := s.db.AppendATProtoEvent(ctx, *event)
		if err != nil {
			return err
		}
		event.Cursor = cursor
		lastCursor = cursor
		s.broadcast(Notification{Kind: EventKindRecord, Cursor: cursor, Record: event})
		return nil
	})
	if err != nil {
		return s.failRepo(ctx, repoInfo, err)
	}

	if err := s.drainBufferedCommits(ctx, repoInfo); err != nil {
		return s.failRepo(ctx, repoInfo, err)
	}

	now := time.Now().UTC()
	repoInfo.SyncState = StateSynced
	repoInfo.CurrentRev = repoSnapshot.SignedCommit().Rev
	repoInfo.CurrentDataCID = repoSnapshot.SignedCommit().Data.String()
	repoInfo.LastBackfillAt = &now
	if lastCursor > 0 {
		repoInfo.LastEventCursor = &lastCursor
	}
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}

	s.logger.Printf("event=atindex_backfill_complete did=%s generation=%d", did, repoInfo.Generation)
	return nil
}

func (s *Service) drainBufferedCommits(ctx context.Context, repoInfo *db.ATProtoRepo) error {
	items, err := s.db.ListATProtoCommitBufferItems(ctx, repoInfo.DID, repoInfo.Generation)
	if err != nil {
		return err
	}
	for _, item := range items {
		var commit atproto.SyncSubscribeRepos_Commit
		if err := json.Unmarshal([]byte(item.RawEventJSON), &commit); err != nil {
			return fmt.Errorf("decode buffered commit %s/%s: %w", item.DID, item.Rev, err)
		}
		if err := s.applyCommit(ctx, repoInfo, &commit, true); err != nil {
			return err
		}
	}
	return s.db.DeleteATProtoCommitBufferItems(ctx, repoInfo.DID, repoInfo.Generation)
}

func (s *Service) applyCommit(ctx context.Context, repoInfo *db.ATProtoRepo, evt *atproto.SyncSubscribeRepos_Commit, live bool) error {
	repoSnapshot, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return err
	}

	var lastCursor int64
	for _, op := range evt.Ops {
		collection, rkey, ok := collectionAndRKey(op.Path)
		if !ok {
			continue
		}

		atURI := fmt.Sprintf("at://%s/%s", evt.Repo, op.Path)
		switch op.Action {
		case "create", "update":
			record, event, err := s.recordFromRepoPath(ctx, repoSnapshot, evt.Repo, op.Path, evt.Rev, &evt.Seq, live)
			if err != nil {
				return err
			}
			if record == nil || event == nil {
				continue
			}
			if err := s.db.UpsertATProtoRecord(ctx, *record); err != nil {
				return err
			}
			cursor, err := s.db.AppendATProtoEvent(ctx, *event)
			if err != nil {
				return err
			}
			event.Cursor = cursor
			lastCursor = cursor
			s.broadcast(Notification{Kind: EventKindRecord, Cursor: cursor, Record: event})
		case "delete":
			existing, err := s.db.GetATProtoRecord(ctx, atURI)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			record := db.ATProtoRecord{
				DID:        evt.Repo,
				Collection: collection,
				RKey:       rkey,
				ATURI:      atURI,
				ATCID:      existingATCID(existing, op),
				RecordJSON: existingJSON(existing),
				LastRev:    evt.Rev,
				LastSeq:    &evt.Seq,
				Deleted:    true,
				DeletedAt:  &now,
			}
			if existing != nil {
				record.CreatedAt = existing.CreatedAt
			}
			if err := s.db.UpsertATProtoRecord(ctx, record); err != nil {
				return err
			}

			event := db.ATProtoRecordEvent{
				DID:        evt.Repo,
				Collection: collection,
				RKey:       rkey,
				ATURI:      atURI,
				ATCID:      record.ATCID,
				Action:     "delete",
				Live:       live,
				Rev:        evt.Rev,
				Seq:        &evt.Seq,
				RecordJSON: record.RecordJSON,
			}
			cursor, err := s.db.AppendATProtoEvent(ctx, event)
			if err != nil {
				return err
			}
			event.Cursor = cursor
			lastCursor = cursor
			s.broadcast(Notification{Kind: EventKindRecord, Cursor: cursor, Record: &event})
		}
	}

	repoInfo.CurrentRev = evt.Rev
	repoInfo.CurrentCommitCID = evt.Commit.String()
	repoInfo.LastFirehoseSeq = &evt.Seq
	if lastCursor > 0 {
		repoInfo.LastEventCursor = &lastCursor
	}
	return s.db.UpsertATProtoRepo(ctx, *repoInfo)
}

func (s *Service) recordFromRepoPath(ctx context.Context, repoSnapshot *atrepo.Repo, did, path, rev string, seq *int64, live bool) (*db.ATProtoRecord, *db.ATProtoRecordEvent, error) {
	collection, rkey, ok := collectionAndRKey(path)
	if !ok {
		return nil, nil, nil
	}

	recordCID, rawCBOR, err := repoSnapshot.GetRecordBytes(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if rawCBOR == nil {
		return nil, nil, nil
	}

	decoded, err := lexutil.CborDecodeValue(*rawCBOR)
	if err != nil {
		return nil, nil, err
	}
	recordJSON, err := json.Marshal(decoded)
	if err != nil {
		return nil, nil, err
	}

	atURI := fmt.Sprintf("at://%s/%s", did, path)
	record := &db.ATProtoRecord{
		DID:        did,
		Collection: collection,
		RKey:       rkey,
		ATURI:      atURI,
		ATCID:      recordCID.String(),
		RecordJSON: string(recordJSON),
		LastRev:    rev,
		LastSeq:    seq,
		Deleted:    false,
	}
	event := &db.ATProtoRecordEvent{
		DID:        did,
		Collection: collection,
		RKey:       rkey,
		ATURI:      atURI,
		ATCID:      recordCID.String(),
		Action:     "upsert",
		Live:       live,
		Rev:        rev,
		Seq:        seq,
		RecordJSON: string(recordJSON),
	}
	return record, event, nil
}

func (s *Service) updateSourceCursor(ctx context.Context, seq int64) error {
	if s.db == nil {
		return nil
	}
	source := db.ATProtoSource{
		SourceKey: s.sourceKey,
		RelayURL:  s.relayURL,
		LastSeq:   seq,
	}
	now := time.Now().UTC()
	source.ConnectedAt = &now
	return s.db.UpsertATProtoSource(ctx, source)
}

func (s *Service) ensureTracked(ctx context.Context, did string) (*db.ATProtoRepo, error) {
	repoInfo, err := s.db.GetATProtoRepo(ctx, did)
	if err != nil {
		return nil, err
	}
	if repoInfo != nil && repoInfo.Tracking {
		return repoInfo, nil
	}

	account, err := s.db.GetBridgedAccount(ctx, did)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.Active {
		return nil, nil
	}
	if err := s.TrackRepo(ctx, did, "active_bridged_account"); err != nil {
		return nil, err
	}
	return s.db.GetATProtoRepo(ctx, did)
}

func (s *Service) markDesynchronized(ctx context.Context, repoInfo *db.ATProtoRepo, evt *atproto.SyncSubscribeRepos_Commit, reason string) error {
	repoInfo.Generation++
	repoInfo.SyncState = StateDesynchronized
	repoInfo.LastError = reason
	repoInfo.LastFirehoseSeq = &evt.Seq
	if err := s.db.UpsertATProtoRepo(ctx, *repoInfo); err != nil {
		return err
	}
	if err := s.bufferCommit(ctx, evt, repoInfo.Generation); err != nil {
		return err
	}
	s.enqueue(repoInfo.DID)
	return nil
}

func (s *Service) bufferCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit, generation int64) error {
	raw, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	if err := s.db.AddATProtoCommitBufferItem(ctx, db.ATProtoCommitBufferItem{
		DID:          evt.Repo,
		Generation:   generation,
		Rev:          evt.Rev,
		Seq:          evt.Seq,
		RawEventJSON: string(raw),
	}); err != nil {
		return err
	}
	s.enqueue(evt.Repo)
	return nil
}

func (s *Service) enqueue(did string) {
	s.queueMu.Lock()
	if _, ok := s.queued[did]; ok {
		s.queueMu.Unlock()
		return
	}
	s.queued[did] = struct{}{}
	s.queueMu.Unlock()

	select {
	case s.queue <- did:
	default:
		go func() { s.queue <- did }()
	}
}

func (s *Service) dequeue(did string) {
	s.queueMu.Lock()
	delete(s.queued, did)
	s.queueMu.Unlock()
}

func (s *Service) failRepo(ctx context.Context, repoInfo *db.ATProtoRepo, err error) error {
	repoInfo.SyncState = StateError
	repoInfo.LastError = err.Error()
	if upsertErr := s.db.UpsertATProtoRepo(ctx, *repoInfo); upsertErr != nil {
		return fmt.Errorf("%v (also failed to update repo state: %w)", err, upsertErr)
	}
	return err
}

func (s *Service) broadcast(note Notification) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for id, ch := range s.subscribers {
		select {
		case ch <- note:
		default:
			s.logger.Printf("event=atindex_subscriber_overflow subscriber=%d kind=%s action=evict", id, note.Kind)
			close(ch)
			delete(s.subscribers, id)
		}
	}
}

func collectionAndRKey(path string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(path), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	collection, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return "", "", false
	}
	rkey, err := syntax.ParseRecordKey(parts[1])
	if err != nil {
		return "", "", false
	}
	return collection.String(), rkey.String(), true
}

func existingATCID(existing *db.ATProtoRecord, op *atproto.SyncSubscribeRepos_RepoOp) string {
	if existing != nil && strings.TrimSpace(existing.ATCID) != "" {
		return existing.ATCID
	}
	if op != nil && op.Prev != nil {
		return op.Prev.String()
	}
	return ""
}

func existingJSON(existing *db.ATProtoRecord) string {
	if existing == nil {
		return ""
	}
	return existing.RecordJSON
}

func shouldQueueTrack(state string) bool {
	switch state {
	case "", StatePending, StateBackfilling, StateDesynchronized, StateError:
		return true
	default:
		return false
	}
}

func isKnownSyncState(state string) bool {
	switch state {
	case StatePending, StateBackfilling, StateSynced, StateDesynchronized, StateDeleted, StateDeactivated, StateTakendown, StateSuspended, StateError:
		return true
	default:
		return false
	}
}
