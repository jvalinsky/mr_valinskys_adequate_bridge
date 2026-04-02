# ATProto Module Design

## Scope

- DID, handle, AT URI, NSID, TID, and CID parsing
- XRPC client, auth, and error handling
- identity resolution and handle verification
- firehose framing for `com.atproto.sync.subscribeRepos`
- CAR/MST traversal and generic record decoding
- minimal hand-written `app.bsky` models needed by the bridge

## Notes

- Keep this package reusable inside the repo and avoid bridge-specific types.
- Prefer standard library and existing non-Indigo dependencies where practical.
- Keep public APIs small and explicit so call sites stop importing protocol details ad hoc.
- The package tree now exists as:
  - `pkg/atproto/syntax`
  - `pkg/atproto/xrpc`
  - `pkg/atproto/identity`
  - `pkg/atproto/firehose`
  - `pkg/atproto/repo`
  - `pkg/atproto/lexutil`
  - `pkg/atproto/appbsky`
- `pkg/atproto/appbsky` uses hand-written minimal models plus custom JSON unmarshalers for union-heavy embed and facet shapes needed by mapper/blob/UI code.
- `pkg/atproto/lexutil.CborDecodeValue` normalizes CBOR values into JSON-friendly data, including CID and byte handling.

## Open Questions

- Firehose envelope parsing as local event structs is working well enough for current runtime usage; no generic envelope abstraction is needed right now.
- The remaining question is fixture ergonomics: production reads are local, but tests still need a lightweight local repo/CAR writer so Indigo can leave the test graph.

## Implementation Notes

- `pkg/atproto/repo` had to move to lazy MST loading because live firehose commit CARs are diffs, not complete snapshots.
- Eager tree expansion worked for full `sync.getRepo` snapshots but broke on commit diff CARs, so nodes now load children on demand.
- `pkg/atproto/identity` currently resolves `did:plc` and `did:web`, verifies handle linkage, and supports `.well-known` plus DNS TXT resolution.
- `pkg/atproto/syntax.ParseATURI` now parses `at://did:...` authorities directly instead of relying on `net/url`, which misparsed DIDs containing colons as host+port.
- Added syntax tests for DID-authority AT URIs to lock down dependency-fetch behavior.
