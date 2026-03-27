package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.cryptoscope.co/luigi"
	"go.cryptoscope.co/margaret"
	librarian "go.cryptoscope.co/margaret/indexes"
	"go.cryptoscope.co/margaret/multilog"
	ssbmultilogs "go.cryptoscope.co/ssb/multilogs"
	ssbrepo "go.cryptoscope.co/ssb/repo"
	refs "go.mindeco.de/ssb-refs"
	"go.mindeco.de/ssb-refs/tfk"
)

func main() {
	repoPath := flag.String("repo", "", "path to SSB repo")
	feedRefText := flag.String("feed", "", "SSB feed ref to count (e.g. @...=.ed25519)")
	minCount := flag.Int("min-count", -1, "optional minimum expected count")
	flag.Parse()

	if *repoPath == "" {
		fatalf("--repo is required")
	}
	if *feedRefText == "" {
		fatalf("--feed is required")
	}

	feedRef, err := refs.ParseFeedRef(*feedRefText)
	if err != nil {
		fatalf("parse --feed: %v", err)
	}

	count, err := countFeedMessages(*repoPath, feedRef)
	if err != nil {
		fatalf("count feed messages: %v", err)
	}

	if *minCount >= 0 && count < *minCount {
		fatalf("feed count %d is below required minimum %d", count, *minCount)
	}

	fmt.Printf("%d\n", count)
}

func countFeedMessages(repoPath string, feed refs.FeedRef) (int, error) {
	repo := ssbrepo.New(repoPath)

	rxLog, err := ssbrepo.OpenLog(repo)
	if err != nil {
		return 0, fmt.Errorf("open receive log: %w", err)
	}
	defer closeIfPossible(rxLog)

	userFeeds, userFeedIndex, err := ssbrepo.OpenStandaloneMultiLog(repo, "userFeeds", ssbmultilogs.UserFeedsUpdate)
	if err != nil {
		return 0, fmt.Errorf("open user feeds index: %w", err)
	}
	defer closeIfPossible(userFeedIndex)
	defer closeIfPossible(userFeeds)

	if err := refreshUserFeeds(context.Background(), rxLog, userFeedIndex); err != nil {
		return 0, err
	}

	addr, err := feedAddr(feed)
	if err != nil {
		return 0, err
	}

	sublog, err := userFeeds.Get(addr)
	if errors.Is(err, multilog.ErrSublogNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open feed sublog: %w", err)
	}

	seq := sublog.Seq()
	if seq < 0 {
		return 0, nil
	}
	return int(seq + 1), nil
}

func refreshUserFeeds(ctx context.Context, rxLog margaret.Log, userFeedIndex librarian.SinkIndex) error {
	src, err := rxLog.Query(userFeedIndex.QuerySpec())
	if err != nil {
		return fmt.Errorf("query user feed index source: %w", err)
	}
	if err := luigi.Pump(ctx, userFeedIndex, src); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("pump user feed index: %w", err)
	}
	return nil
}

func feedAddr(feed refs.FeedRef) (librarian.Addr, error) {
	encoded, err := tfk.FeedFromRef(feed)
	if err != nil {
		return "", fmt.Errorf("encode feed tfk: %w", err)
	}
	binary, err := encoded.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("marshal feed tfk: %w", err)
	}
	return librarian.Addr(binary), nil
}

func closeIfPossible(value interface{}) {
	if closer, ok := value.(io.Closer); ok {
		_ = closer.Close()
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
