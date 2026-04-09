# ATProto to SSB Record Translation

See also: [overview](./atproto-ssb-translation-overview.md) and [identity mapping](./atproto-ssb-identity-mapping.md).

This page describes the forward bridge's supported collection set: post, like, follow, block, profile, list, listitem, and threadgate. Standalone reposts are not part of the processor's supported collections today.

## The bridge translates records in two passes

The code deliberately does not try to emit final SSB refs in a single mapper step.

Pass 1 in [`internal/mapper/mapper.go`](../internal/mapper/mapper.go):

- convert the ATProto record into an SSB-shaped payload
- keep unresolved ATProto references in `_atproto_*` placeholder fields

Pass 2 in [`internal/bridge/processor.go`](../internal/bridge/processor.go):

- replace placeholders with real SSB refs when they already exist
- fetch missing ATProto record dependencies when possible
- defer the record if any required placeholder is still unresolved

This split is necessary because SSB message refs only exist after publish time, while ATProto records can arrive out of order.

## Placeholder fields and what they mean

| Placeholder | Produced from | Replaced with | Used for |
| --- | --- | --- | --- |
| `_atproto_reply_root` | post reply root URI | `root` message ref | reply threading |
| `_atproto_reply_parent` | post reply parent URI | `branch` message ref | reply threading |
| `_atproto_subject` | like subject URI | `vote.link` message ref | likes |
| `_atproto_quote_subject` | quoted post URI | mention + markdown link to message ref | quotes |
| `_atproto_list` | listitem parent list URI | `list` message ref | list membership |
| `_atproto_contact` | follow/block subject DID | `contact` feed ref | social graph edges |
| `_atproto_about_did` | author DID on profile record | `about` feed ref | profile/about messages |

The mapper also rewrites rich text and collects `mentions`, but DID mentions remain DIDs until the replacement step.

## How URI references are resolved

AT URI lookups go through the message table:

1. [`internal/bridge/processor.go`](../internal/bridge/processor.go) calls `resolveMessageReference(ctx, uri)`.
2. That reads [`db.GetMessage`](../internal/db/db.go).
3. If the target row already has `ssb_msg_ref`, the placeholder can be replaced.

This is why the `messages` table stores both `at_uri` and `ssb_msg_ref` in [`internal/db/schema.sql`](../internal/db/schema.sql).

Without that lookup table, a bridged like or reply would have no stable way to point at the SSB message produced for the original ATProto record.

## How DID references are resolved

DID lookups go through `resolveFeedReference(ctx, did)` in [`internal/bridge/processor.go`](../internal/bridge/processor.go):

1. Check `bridged_accounts` for an active local mapping.
2. If not present, ask the configured `FeedResolver`.
3. In the main runtime, that resolver is [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go), which derives the feed deterministically from the DID.

This path is used for:

- follow and block `contact`
- profile `about`
- rich-text mention links that point at DIDs

## What happens to DID mentions

`ReplaceATProtoRefs` in [`internal/mapper/mapper.go`](../internal/mapper/mapper.go) treats mentions conservatively:

- if a mention link is already not a DID, it stays as-is
- if a mention link is a DID and the feed can be resolved, the DID is replaced with the SSB feed ref
- if a mention link is a DID and cannot be resolved, that mention is dropped

The intended behavior is captured directly in [`internal/mapper/mapper_test.go`](../internal/mapper/mapper_test.go).

The reason is straightforward: publishing an SSB mention that still contains an unresolved DID would leave a bridge-specific half-translated payload in the signed message.

## Why the processor hydrates some dependencies but not others

`hydrateRecordDependencies` in [`internal/bridge/processor.go`](../internal/bridge/processor.go) auto-fetches unresolved AT URI dependencies for:

- `_atproto_subject`
- `_atproto_quote_subject`
- `_atproto_reply_root`
- `_atproto_reply_parent`

Those can be fetched as records via [`internal/bridge/dependencies.go`](../internal/bridge/dependencies.go) using `com.atproto.repo.getRecord`.

By contrast, DID placeholders such as `_atproto_contact` and `_atproto_about_did` are not fetched as records. They do not need a record fetch; they need a feed resolution step.

That difference mirrors the actual target:

- AT URI placeholder -> needs a published SSB message ref
- DID placeholder -> needs an SSB feed ref

The mapper does have a `_atproto_repost_subject` placeholder for `app.bsky.feed.repost`, but the forward processor does not currently ingest standalone repost records. In practice the forward path handles quote-post embeds instead.

## Deferred state is part of normal operation

After replacement, [`internal/mapper/mapper.go`](../internal/mapper/mapper.go) reports leftover placeholders through `UnresolvedATProtoRefs`.

If any remain, [`internal/bridge/processor.go`](../internal/bridge/processor.go) stores the row as:

- `message_state = deferred`
- `defer_reason = "_atproto_key=value;..."`

The admin UI turns those raw reasons into operator-friendly summaries in [`internal/web/handlers/ui.go`](../internal/web/handlers/ui.go), for example:

- waiting on reply target bridge
- waiting on contact bridge
- waiting on subject bridge
- waiting on quoted post bridge
- waiting on author feed bridge

This is not just an error path. It is how the bridge absorbs:

- out-of-order firehose delivery
- backfills where parent records arrive later
- cross-account references whose target has not been bridged yet

## Deferred resolution is cascading

`ResolveDeferredMessages` in [`internal/bridge/processor.go`](../internal/bridge/processor.go) does not just retry a flat list. It builds a dependency index so that when one deferred message is published, dependent messages in the same batch are retried immediately.

[`internal/bridge/processor_test.go`](../internal/bridge/processor_test.go) includes a reply-chain test where A publishes, then B resolves because A now exists, then C resolves because B now exists.

That behavior is the main reason the bridge stores explicit defer reasons instead of only logging transient failures.

## Why the placeholder approach exists at all

The codebase's answer is:

- mapping and publishing are separate concerns
- SSB refs are only knowable after publish
- ATProto references must survive long enough to be retried correctly

So the bridge preserves ATProto identifiers first, resolves them only when the corresponding SSB object exists, and persists enough state to finish the translation later.

## Code and tests to read next

- [`internal/mapper/mapper.go`](../internal/mapper/mapper.go)
- [`internal/bridge/processor.go`](../internal/bridge/processor.go)
- [`internal/bridge/dependencies.go`](../internal/bridge/dependencies.go)
- [`internal/db/schema.sql`](../internal/db/schema.sql)
- [`internal/mapper/mapper_test.go`](../internal/mapper/mapper_test.go)
- [`internal/bridge/processor_test.go`](../internal/bridge/processor_test.go)

## Reverse sync v2: SSB to ATProto media and facets

Reverse sync now has a second content-fidelity pass for SSB `post` and `reply` messages in [`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go).

### Canonical reverse media source

Reverse media only looks at structured SSB `mentions` entries shaped like:

- `{"link":"&...sha256","name":"alt text","type":"image/png"}`

Rules:

- only blob refs in `mentions[].link` are eligible for reverse media
- only `image/*` is supported
- duplicate blob refs are deduplicated in mention order
- more than 4 unique image refs defers the whole reverse event
- missing blob bytes, unsupported MIME, MIME mismatches, and upload failures defer the whole reverse event

A bare `&blob...` token in text without a matching structured `mentions` entry stays plain text and does not create an ATProto embed.

### Reverse text shaping

When a structured image blob mention is embedded as `app.bsky.embed.images`, the reverse path removes markdown segments that point at that blob ref from the ATProto text body.

Examples:

- `![Preview](&blob...)`
- `[Preview](&blob...)`

Other markdown and prose remain unchanged. After stripping those segments, the reverse path trims immediate whitespace artifacts before computing facet byte ranges.

### Reverse facet generation

Facet byte ranges are computed against the final UTF-8 bytes of the shaped ATProto text.

The reverse processor builds facets from:

- structured SSB feed mentions, if the feed resolves to an AT DID
- structured `#tag` mentions
- structured `http(s)` link mentions
- bare `http(s)` URLs still present in text after structured facets are placed

Placement is conservative:

- use `mentions[].name` as the visible token when present
- otherwise fall back to the link literal (`@feed`, `#tag`, or URL)
- claim the first non-overlapping exact match in the final text
- if a structured token cannot be placed, skip that facet instead of deferring the whole event
- if an SSB feed mention cannot be mapped to an AT DID, publish the rest of the post and leave that token as plain text
