# Indigo Test Fixture Migration

## Goal
Remove Indigo dependency from test fixtures by adding local repo builder API.

## Current State
- Production code uses local `pkg/atproto` and `internal/atindex`
- Tests use Indigo imports for CAR fixture generation:
  - `indigorepo.NewRepo(ctx, did, blockstore)` 
  - `rr.CreateRecord(ctx, collection, record)`
  - `rr.Commit(ctx, signFunc)`
  - CAR serialization with `car.WriteHeader`

## Required Local API
Need to add to `pkg/atproto/repo`:
- `NewRepo(did string, blockstore Blockstore) *Repo` - create new repo instance
- `CreateRecord(ctx, collection, record) (cid.Cid, string, error)` - add record to repo
- `Commit(ctx, signFunc) (cid.Cid, string, error)` - commit repo and get root

## Blockstore Interface
Tests use a simple map-based blockstore:
```go
type testBlockstore struct {
    blocks map[string]blockformat.Block
}

func (bs *testBlockstore) Put(_ context.Context, blk blockformat.Block) error
func (bs *testBlockstore) Get(_ context.Context, c cid.Cid) (blockformat.Block, error)
```

## Test Files to Migrate
1. `internal/backfill/pds_test.go` - uses indigorepo, appbsky types
2. `internal/firehose/client_test.go` - same pattern
3. `internal/firehose/integration_test.go` - same pattern  
4. `internal/bridge/integration_test.go` - same pattern
5. `internal/firehose/docker_integration_test.go` - same pattern
6. `internal/livee2e/live_test.go` - uses xrpc, session types

## Imports to Replace
- `indigorepo "github.com/bluesky-social/indigo/repo"` → local `atrepo` 
- `indigoatproto "github.com/bluesky-social/indigo/api/atproto"` → local `atproto`
- `appbsky "github.com/bluesky-social/indigo/api/bsky"` → local `appbsky`
- `lexutil "github.com/bluesky-social/indigo/lex/util"` → local `lexutil`
- `xrpc` from indigo → local `xrpc`

## Notes
- Local appbsky types already exist in `pkg/atproto/appbsky`
- Local repo package exists in `pkg/atproto/repo` with `ReadRepoFromCar`
- Need to add write capability to local repo package
- CAR serialization uses standard `ipld/go-car` library (already used)

## Update 2026-04-01

- The repo writer/test-fixture migration is now complete enough that there are no remaining Indigo imports under `internal`, `cmd`, or `pkg`.
- Final cleanup removed `github.com/bluesky-social/indigo` from `go.mod` and `go.sum`.
- `go mod vendor` removed the vendored Indigo tree and regenerated `vendor/modules.txt` without Indigo entries.
- `go mod tidy` also promoted `modernc.org/sqlite` to an explicit dependency, which reflects current direct use rather than Indigo fallout.

## Validation 2026-04-01

- Targeted suite remains green after module cleanup:
  - `go test ./pkg/atproto/... ./internal/db ./internal/atindex ./internal/backfill ./internal/firehose ./internal/bridge ./internal/web/handlers ./internal/blobbridge ./internal/mapper ./cmd/bridge-cli ./cmd/atproto-seed`
- Full repo suite is green after updating the smoke test to the new dashboard cursor semantics:
  - `go test ./...`

## Update 2026-04-01 (Worktree Hygiene)

- Added `.gitignore` rules for local ATProto migration leftovers that should stay out of commits:
  - generated binary/log output
  - local demo repo data
  - regenerated local `vendor/` output
- Left untracked source-looking files visible instead of ignoring them globally.
