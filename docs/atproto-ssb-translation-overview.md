# ATProto to SSB Translation Overview

See also: [docs index](./README.md), [identity mapping](./atproto-ssb-identity-mapping.md), [record translation](./atproto-ssb-record-translation.md).

This bridge does not do one single "ATProto to SSB" conversion. The codebase keeps three distinct mappings:

| Source value | Target value | Where it is resolved | Why it exists |
| --- | --- | --- | --- |
| ATProto DID (`did:plc:...`) | SSB feed ref (`@...`) | [`internal/bots/manager.go`](../internal/bots/manager.go), [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go) | Every bridged ATProto account publishes as a deterministic SSB bot identity. |
| AT URI (`at://did/.../collection/rkey`) | SSB message ref (`%...`) | [`internal/bridge/processor.go`](../internal/bridge/processor.go), [`internal/db/schema.sql`](../internal/db/schema.sql) | Replies, likes, quotes, and similar edges must point at already-published SSB messages. |
| AT blob CID | SSB blob ref (`&...`) | [`internal/blobbridge/bridge.go`](../internal/blobbridge/bridge.go) | Media is mirrored separately from account and message identity. |

## The important split

The bridge treats "who is this account?" and "what SSB object does this record point to?" as different problems:

- Account identity is deterministic. Given the same master seed and the same DID, the bridge can derive the same SSB feed every time.
- Message identity is not deterministic. A message ref only exists after the bridge has actually published the mapped record into SSB.

That split explains most of the surrounding design:

- [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go) stores DID-to-feed mappings in `bridged_accounts`, but the feed can still be recomputed from the DID when needed.
- [`internal/mapper/mapper.go`](../internal/mapper/mapper.go) first emits placeholder `_atproto_*` fields instead of guessing SSB refs too early.
- [`internal/bridge/processor.go`](../internal/bridge/processor.go) resolves placeholders, defers unresolved records, and retries after dependencies exist.

## End-to-end flow

1. Operators register a DID with `account add`; the CLI derives that DID's SSB feed and stores it in `bridged_accounts`.
2. Firehose processing keys off the DID in `evt.Repo`; inactive or unknown DIDs are ignored.
3. The mapper converts one ATProto record into an SSB-shaped payload, but leaves ATProto identifiers in `_atproto_*` placeholder fields.
4. The processor resolves placeholders:
   - AT URIs are looked up in `messages` to find published SSB message refs.
   - DIDs are looked up in `bridged_accounts` or derived on demand through the SSB runtime.
5. If a needed target does not exist yet, the record is stored as `deferred` with a machine-readable reason.
6. Once the missing dependency is bridged, deferred records are retried and published with real SSB refs.

## Why DIDs, not handles

The codebase consistently keys on DIDs:

- the firehose commit repo identifier is a DID in [`internal/bridge/processor.go`](../internal/bridge/processor.go)
- the CLI `account add` command accepts a DID in [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go)
- the database schema stores `at_did` in [`internal/db/schema.sql`](../internal/db/schema.sql)

Handles do not participate in bridge identity resolution. The only handle-oriented text in this repo is operator-facing live-test/runbook material such as [`docs/runbook.md`](./runbook.md), not the core translation path.

---

## See Also

- [Documentation Index](./README.md)
- [ATProto to SSB Identity Mapping](./atproto-ssb-identity-mapping.md)
- [Bridge Operator Runbook](./runbook.md)
