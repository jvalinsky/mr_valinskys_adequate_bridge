# SSB to ATProto Reverse Sync

This document covers the optional reverse path from SSB back into ATProto. It is separate from the forward ATProto-to-SSB bridge and is disabled by default.

## What It Does

When reverse sync is enabled, the bridge scans the local SSB receive log and can publish a small allowlisted set of actions into ATProto:

| SSB input | ATProto result | Notes |
| --- | --- | --- |
| `post` | `app.bsky.feed.post` | Plain post |
| `post` with reply refs | `app.bsky.feed.post` reply | `root` and `branch` must already map to AT URIs/CIDs |
| `contact` with `following=true` | `app.bsky.graph.follow` | Target feed must resolve to an AT DID |
| `contact` with `following=false` and `blocking=false` | Delete previously created follow record | Requires an earlier reverse-synced follow |

Behavior outside that set:
- `contact` with `blocking=true` is skipped.
- Messages from feeds without an active reverse mapping are ignored.
- Failures and deferred work are persisted in `reverse_events`.

## Preconditions

Reverse sync needs three pieces of state:

1. An active reverse mapping from SSB feed ID to AT DID in `reverse_identity_mappings`.
2. Credentials for that AT DID in the reverse credentials JSON file.
3. Any referenced targets already resolvable from local bridge state.

Target resolution rules:
- Reply targets are resolved from the `messages` table by SSB message ref.
- Follow targets are resolved to AT DIDs from `reverse_identity_mappings` first, then `bridged_accounts`.

## Reverse Mapping Allowlist

Reverse sync is opt-in per feed. Each mapping stores:
- the source `ssb_feed_id`
- the destination `at_did`
- whether the mapping is active
- per-action allow flags for posts, replies, and follows

The admin UI exposes this under `/reverse`. There is no dedicated CLI for maintaining these mappings today.

## Credentials File

The credentials file is a JSON object keyed by AT DID:

```json
{
  "did:plc:example123": {
    "identifier": "user@example.com",
    "pds_host": "https://bsky.social",
    "password_env": "BRIDGE_REVERSE_SOURCE_PASSWORD"
  }
}
```

Field rules:
- `identifier` is required.
- `password_env` is required and must name an environment variable present in the bridge process.
- `pds_host` is optional. If it is omitted, the bridge resolves the PDS host through PLC.

## Enabling Reverse Sync

Example runtime command:

```bash
export BRIDGE_REVERSE_SOURCE_PASSWORD="app-password"

./bridge-cli \
  --db bridge.sqlite \
  --relay-url wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  start \
  --repo-path .ssb-bridge \
  --reverse-sync-enable \
  --reverse-credentials-file /run/secrets/reverse-credentials.json \
  --reverse-sync-scan-interval 5s \
  --reverse-sync-batch-size 100
```

Relevant flags:
- `--reverse-sync-enable`
- `--reverse-credentials-file`
- `--reverse-sync-scan-interval` (default `5s`)
- `--reverse-sync-batch-size` (default `100`)
- `--plc-url` if you rely on PLC-based PDS resolution
- `--atproto-insecure` only for local or test stacks

The standalone UI can also expose reverse-sync status and retry controls:

```bash
./bridge-cli \
  --db bridge.sqlite \
  serve-ui \
  --repo-path .ssb-bridge \
  --reverse-sync-enable \
  --reverse-credentials-file /run/secrets/reverse-credentials.json
```

## Event States

Reverse work is recorded in `reverse_events` with one of these states:

| State | Meaning |
| --- | --- |
| `pending` | Event has been discovered but not completed yet |
| `published` | ATProto write or delete succeeded |
| `deferred` | The bridge needs more local state, credentials, or correlation data |
| `failed` | The bridge attempted the ATProto action and got an error |
| `skipped` | The action was intentionally ignored by policy or unsupported input |

The `/reverse` UI page lets operators filter the queue and retry `failed` or `deferred` events.

## Common Reasons for Deferred Work

- `credentials_missing_credentials_entry`
- `credentials_password_env_unset`
- `reply_root_unmapped=%...`
- `reply_parent_unmapped=%...`
- `target_did_unmapped=@...`
- `follow_record_not_found=did:plc:...`
- `action_disabled=post|reply|follow`

Those strings come from the code path in [`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go) and are stored directly in the database.

## Limits and Operational Notes

- Reverse sync is conservative. It only writes actions the bridge can map without guessing.
- Reply and unfollow handling depend on forward correlation data already existing in SQLite.
- Credentials should stay in environment variables or secret files, not in the JSON file itself.
- Keep `--atproto-insecure` off outside local and test stacks.

## See Also

- [Bridge Operator Runbook](./runbook.md)
- [ATProto to SSB Translation Overview](./atproto-ssb-translation-overview.md)
- [`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go)
- [`internal/db/schema.sql`](../internal/db/schema.sql)
