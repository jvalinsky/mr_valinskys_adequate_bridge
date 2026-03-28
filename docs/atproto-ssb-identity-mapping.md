# ATProto to SSB Identity Mapping

See also: [overview](./atproto-ssb-translation-overview.md) and [record translation](./atproto-ssb-record-translation.md).

## What the bridge treats as an "account"

In the runtime code, an ATProto account is identified by its DID, not by its handle.

- `account add` requires a DID in [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go).
- the primary key in `bridged_accounts` is `at_did` in [`internal/db/schema.sql`](../internal/db/schema.sql).
- firehose commits are gated by `evt.Repo` and looked up directly as a DID in [`internal/bridge/processor.go`](../internal/bridge/processor.go).

That means the bridge's durable account identity is:

`ATProto DID -> deterministic SSB feed ref`

Not:

`handle -> feed ref`

## Where the DID-to-feed mapping comes from

The actual derivation lives in [`internal/bots/manager.go`](../internal/bots/manager.go):

1. `deriveKeyPair(atDID)` computes `HMAC-SHA256(masterSeed, atDID)`.
2. The first 32 bytes are used as an Ed25519 seed.
3. The resulting Ed25519 public key is wrapped as an SSB1 feed ref with `refs.NewFeedRefFromBytes(..., refs.RefAlgoFeedSSB1)`.

That gives the bridge a stable SSB identity for every DID as long as the master seed stays the same.

## Why the mapping is deterministic

The codebase makes deterministic derivation the default for a few concrete reasons:

- Restart stability. The same DID must resolve to the same SSB feed after every restart; otherwise later likes/follows/about messages would point at a different author identity.
- No extra secret store. The bridge only needs the master seed, not a per-account private-key database.
- On-demand resolution. [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go) can answer `ResolveFeed(ctx, did)` even if there is no existing DB row for that DID.

This is also why the runbook says the bot seed must remain stable across restarts in [`docs/runbook.md`](./runbook.md).

## Why `bridged_accounts` still exists if derivation is deterministic

The database row is a policy and operations layer, not the source of truth for key generation.

`bridged_accounts` in [`internal/db/schema.sql`](../internal/db/schema.sql) stores:

- `at_did`
- `ssb_feed_id`
- `active`
- `created_at`

The bridge uses that row for three things:

1. Activation gating. [`internal/bridge/processor.go`](../internal/bridge/processor.go) ignores commits for DIDs that are absent or inactive.
2. Fast local resolution. [`internal/bridge/processor.go`](../internal/bridge/processor.go) first checks `bridged_accounts` before falling back to the runtime resolver.
3. Operator visibility. The CLI, stats, and admin UI read account rows directly through [`internal/db/db.go`](../internal/db/db.go).

So the model is:

- deterministic feed derivation answers "what SSB feed belongs to this DID?"
- `bridged_accounts` answers "is this DID actively bridged here, and what mapping should the operator see?"

## The account registration path

The account-add flow in [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go) is:

1. Parse the DID argument.
2. Construct a [`bots.Manager`](../internal/bots/manager.go).
3. Call `GetFeedID(did)`.
4. Persist `{ATDID, SSBFeedID, Active:true}` via [`db.AddBridgedAccount`](../internal/db/db.go).

No network lookup is required for that step. The feed ref is derived locally from the DID and the configured seed.

## Runtime publishing path

When the bridge later publishes a mapped record:

1. [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go) calls `manager.GetPublisher(atDID)`.
2. The manager derives or reuses the DID-specific keypair.
3. The message is published through a feed-specific SSB publisher.
4. `ResolveFeed` uses the same derivation path without needing to publish a message first.

This is what makes feed resolution consistent across:

- explicit account registration
- first publish for a DID
- reference replacement for contact/about/mention fields

## How cross-account DID references are resolved

The bridge distinguishes between "ingest this account's repo" and "resolve this DID to an SSB feed ref."

For a DID found inside a record:

1. [`internal/bridge/processor.go`](../internal/bridge/processor.go) checks `bridged_accounts`.
2. If an active row exists, it uses that `ssb_feed_id`.
3. Otherwise, if a `FeedResolver` is configured, it calls `ResolveFeed(ctx, did)`.

In the main runtime, the feed resolver is the SSB runtime itself, wired in [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go).

That means:

- the bridge only ingests commits for active accounts
- but it can still resolve another DID to a stable SSB feed when a follow/about/mention needs one

## Why handles are intentionally absent from this mapping

This repo's core mapping code never stores or resolves handles as account identity. That matches the implementation constraints:

- firehose repos arrive keyed by DID
- follow/block records point at DIDs
- the bridge database schema is DID-first

A handle could change without changing the underlying account DID. Using the DID avoids coupling bridge identity to mutable profile state.

## Code and tests to read next

- [`internal/bots/manager.go`](../internal/bots/manager.go)
- [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go)
- [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go)
- [`internal/db/schema.sql`](../internal/db/schema.sql)
- [`internal/bridge/processor_test.go`](../internal/bridge/processor_test.go)
- [`internal/ssbruntime/runtime_test.go`](../internal/ssbruntime/runtime_test.go)
