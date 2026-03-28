// Package backfill replays supported records from ATProto repositories via sync.getRepo.
package backfill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	indigorepo "github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
	"github.com/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
)

// RecordProcessor handles a decoded record during backfill traversal.
type RecordProcessor interface {
	ProcessRecord(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error
}

// Stats summarizes backfill processing results.
type Stats struct {
	Processed int
	Skipped   int
	Errors    int
}

// SinceFilter controls optional timestamp or sequence-based backfill filtering.
type SinceFilter struct {
	Raw            string
	Timestamp      *time.Time
	Sequence       *int64
	SequenceNotice string
}

// ParseSince parses a --since value as either a sequence number or timestamp.
func ParseSince(raw string) (SinceFilter, error) {
	filter := SinceFilter{Raw: strings.TrimSpace(raw)}
	if filter.Raw == "" {
		return filter, nil
	}

	if seq, err := strconv.ParseInt(filter.Raw, 10, 64); err == nil {
		filter.Sequence = &seq
		filter.SequenceNotice = "sequence-based filtering is not available for sync.getRepo snapshots; processing all supported records"
		return filter, nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, filter.Raw); err == nil {
			utc := ts.UTC()
			filter.Timestamp = &utc
			return filter, nil
		}
	}

	return filter, fmt.Errorf("invalid --since value %q (expected unix-seq integer or timestamp)", raw)
}

// Include reports whether recordJSON passes the filter.
func (f SinceFilter) Include(recordJSON []byte) bool {
	if f.Timestamp == nil {
		return true
	}

	var payload struct {
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(recordJSON, &payload); err != nil {
		return true
	}
	if payload.CreatedAt == "" {
		return true
	}

	created, err := time.Parse(time.RFC3339, payload.CreatedAt)
	if err != nil {
		return true
	}
	return !created.UTC().Before(*f.Timestamp)
}

// RunForDID backfills supported records for a single DID using a resolved per-DID PDS host.
func RunForDID(ctx context.Context, did string, since SinceFilter, processor RecordProcessor, logger *log.Logger, resolver HostResolver, fetcher RepoFetcher) DIDResult {
	result := DIDResult{
		DID:    did,
		Status: StatusSuccess,
	}
	logger = logutil.Ensure(logger)

	if since.SequenceNotice != "" {
		logger.Printf("event=backfill_since_notice did=%s notice=%q", did, since.SequenceNotice)
	}

	if resolver == nil {
		result.Status = StatusTransportError
		result.Err = fmt.Errorf("backfill host resolver is nil")
		return result
	}
	if fetcher == nil {
		result.Status = StatusTransportError
		result.Err = fmt.Errorf("backfill repo fetcher is nil")
		return result
	}

	host, err := resolver.ResolvePDSEndpoint(ctx, did)
	if err != nil {
		result.Status = classifyDIDResult(err)
		result.Err = err
		return result
	}
	result.PDSHost = host

	carBytes, err := fetcher.FetchRepo(ctx, host, did)
	if err != nil {
		result.Status = classifyDIDResult(err)
		result.Err = fmt.Errorf("sync.getRepo did=%s pds_host=%s: %w", did, host, err)
		return result
	}

	stats, err := processRepoCAR(ctx, carBytes, did, since, processor, logger)
	if err != nil {
		result.Status = classifyDIDResult(err)
		result.Err = err
		return result
	}
	result.Stats = stats
	return result
}

func processRepoCAR(ctx context.Context, carBytes []byte, did string, since SinceFilter, processor RecordProcessor, logger *log.Logger) (Stats, error) {
	rr, err := indigorepo.ReadRepoFromCar(ctx, bytes.NewReader(carBytes))
	if err != nil {
		return Stats{}, fmt.Errorf("read repo car did=%s: %w", did, err)
	}

	stats := Stats{}
	err = rr.ForEach(ctx, "", func(path string, _ cid.Cid) error {
		collection, ok := collectionFromPath(path)
		if !ok || !isSupportedCollection(collection) {
			stats.Skipped++
			return nil
		}

		cc, recordCBOR, err := rr.GetRecordBytes(ctx, path)
		if err != nil {
			stats.Errors++
			logger.Printf("event=backfill_record_error did=%s path=%s err=%v", did, path, err)
			return nil
		}
		if recordCBOR == nil {
			stats.Skipped++
			return nil
		}

		recordJSON, err := cborToJSON(*recordCBOR)
		if err != nil {
			stats.Errors++
			logger.Printf("event=backfill_decode_error did=%s path=%s err=%v", did, path, err)
			return nil
		}

		if !since.Include(recordJSON) {
			stats.Skipped++
			return nil
		}

		atURI := fmt.Sprintf("at://%s/%s", did, path)
		if err := processor.ProcessRecord(ctx, did, atURI, cc.String(), collection, recordJSON); err != nil {
			stats.Errors++
			logger.Printf("event=backfill_process_error did=%s at_uri=%s record_type=%s err=%v", did, atURI, collection, err)
			return nil
		}

		stats.Processed++
		return nil
	})
	if err != nil {
		return stats, fmt.Errorf("iterate repo did=%s: %w", did, err)
	}

	logger.Printf("event=backfill_complete did=%s processed=%d skipped=%d errors=%d", did, stats.Processed, stats.Skipped, stats.Errors)
	return stats, nil
}

func collectionFromPath(path string) (string, bool) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func isSupportedCollection(collection string) bool {
	switch collection {
	case mapper.RecordTypePost, mapper.RecordTypeLike, mapper.RecordTypeFollow, mapper.RecordTypeBlock, mapper.RecordTypeProfile:
		return true
	default:
		return false
	}
}

func cborToJSON(rawCBOR []byte) ([]byte, error) {
	decoded, err := lexutil.CborDecodeValue(rawCBOR)
	if err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}
