# ATProto Firehose Client

- Created `internal/firehose/client.go` to connect to `wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos`
- Used `github.com/bluesky-social/indigo/events` and `sequential.NewScheduler` to process events
- Created a flexible `EventHandler` interface for the bridge core to process commits
- Integration test `client_test.go` verifies the websocket connection and correctly parses incoming streams (received ~2000 commits in 5s during testing)