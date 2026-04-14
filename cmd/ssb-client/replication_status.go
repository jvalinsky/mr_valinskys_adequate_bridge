package main

import (
	"sort"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
)

type replicationStatusRow struct {
	FeedID        string `json:"feedId"`
	LocalSeq      int64  `json:"localSeq"`
	FrontierSeq   int64  `json:"frontierSeq"`
	Lag           int64  `json:"lag"`
	Replicate     bool   `json:"replicate"`
	Receive       bool   `json:"receive"`
	LastTimestamp int64  `json:"lastTimestamp"`
	LastUpdate    string `json:"lastUpdate"`
	Status        string `json:"status"`
}

type replicationSnapshot struct {
	Enabled     bool                        `json:"enabled"`
	SelfFeed    string                      `json:"selfFeed"`
	Matrix      map[string]map[string]int64 `json:"matrix"`
	MatrixPeers int                         `json:"matrixPeers"`
	Rows        []replicationStatusRow      `json:"rows"`
}

func collectReplicationSnapshot(node *sbot.Sbot) (replicationSnapshot, error) {
	snapshot := replicationSnapshot{
		Enabled: false,
		Matrix:  map[string]map[string]int64{},
		Rows:    []replicationStatusRow{},
	}

	if node == nil || node.Store() == nil {
		return snapshot, nil
	}

	whoami, _ := node.Whoami()
	snapshot.SelfFeed = whoami

	localSeq := map[string]int64{}
	lastTS := map[string]int64{}
	if logs := node.Store().Logs(); logs != nil {
		feedIDs, _ := logs.List()
		for _, feedID := range feedIDs {
			log, err := logs.Get(feedID)
			if err != nil {
				continue
			}
			seq, err := log.Seq()
			if err != nil {
				continue
			}
			localSeq[feedID] = seq
			if seq > 0 {
				if msg, err := log.Get(seq); err == nil && msg != nil && msg.Metadata != nil {
					lastTS[feedID] = msg.Metadata.Timestamp
				}
			}
		}
	}

	type frontierNote struct {
		seq       int64
		replicate bool
		receive   bool
	}
	frontier := map[string]frontierNote{}
	if sm := node.StateMatrix(); sm != nil {
		snapshot.Enabled = true
		snapshot.Matrix = sm.Export()
		snapshot.MatrixPeers = len(snapshot.Matrix)

		if selfRef, err := refs.ParseFeedRef(whoami); err == nil {
			if selfFrontier, inspectErr := sm.Inspect(selfRef); inspectErr == nil {
				for feedID, note := range selfFrontier {
					frontier[feedID] = frontierNote{
						seq:       note.Seq,
						replicate: note.Replicate,
						receive:   note.Receive,
					}
				}
			}
		}
	}

	feedSet := map[string]struct{}{}
	for feedID := range localSeq {
		feedSet[feedID] = struct{}{}
	}
	for feedID := range frontier {
		feedSet[feedID] = struct{}{}
	}
	if whoami != "" {
		feedSet[whoami] = struct{}{}
	}

	rows := make([]replicationStatusRow, 0, len(feedSet))
	for feedID := range feedSet {
		row := replicationStatusRow{
			FeedID:      feedID,
			LocalSeq:    localSeq[feedID],
			FrontierSeq: localSeq[feedID],
			Replicate:   false,
			Receive:     false,
			Status:      "not-tracked",
		}

		if note, ok := frontier[feedID]; ok {
			row.FrontierSeq = note.seq
			row.Replicate = note.replicate
			row.Receive = note.receive
		}
		if feedID == whoami {
			row.Replicate = true
			row.Receive = true
			if row.FrontierSeq < row.LocalSeq {
				row.FrontierSeq = row.LocalSeq
			}
		}

		switch {
		case row.FrontierSeq == -1:
			row.Status = "unfollowed"
			row.Lag = 0
		case !row.Replicate:
			row.Status = "not-tracked"
			row.Lag = 0
		default:
			if row.FrontierSeq > row.LocalSeq {
				row.Lag = row.FrontierSeq - row.LocalSeq
				row.Status = "behind"
			} else if row.LocalSeq == 0 && row.FrontierSeq == 0 {
				row.Status = "pending"
			} else {
				row.Status = "in-sync"
			}
		}

		row.LastTimestamp = lastTS[feedID]
		if row.LastTimestamp > 0 {
			row.LastUpdate = time.UnixMilli(row.LastTimestamp).UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Lag == rows[j].Lag {
			return rows[i].FeedID < rows[j].FeedID
		}
		return rows[i].Lag > rows[j].Lag
	})
	snapshot.Rows = rows

	return snapshot, nil
}
