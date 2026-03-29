package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
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

	count, err := countFeedMessages(*repoPath, *feedRefText)
	if err != nil {
		fatalf("count feed messages: %v", err)
	}

	if *minCount >= 0 && count < *minCount {
		fatalf("feed count %d is below required minimum %d", count, *minCount)
	}

	fmt.Printf("%d\n", count)
}

func countFeedMessages(repoPath, feedRefStr string) (int, error) {
	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:   repoPath + "/flume.sqlite",
		RepoPath: repoPath,
	})
	if err != nil {
		return 0, fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	logs := store.Logs()

	seq, err := logs.Get(feedRefStr)
	if err == feedlog.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get feed log: %w", err)
	}

	seqNum, err := seq.Seq()
	if err != nil {
		return 0, fmt.Errorf("get seq: %w", err)
	}
	if seqNum < 0 {
		return 0, nil
	}
	return int(seqNum + 1), nil
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
