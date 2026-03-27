# 017 Next-Cycle Scope Plan

## Objective
Define the first milestone after shipping the strict room-tunnel verifier cycle.

## Chosen Option
Prioritize record-type expansion: add `app.bsky.actor.profile` and `app.bsky.graph.block` bridge support in the active pipeline.

## Rejected Option
Prioritize onboarding automation first (auto-discovery/auto-add flows) before expanding bridged record types.

## Why This Choice
- It extends protocol completeness directly in the data plane that is already stable (`post/like/repost/follow`).
- It keeps current manual onboarding unchanged while increasing bridged semantic coverage.
- It minimizes operational risk versus introducing account lifecycle automation in the same cycle.

## Proposed Milestone Shape
1. Add profile/block support in firehose commit handling and backfill.
2. Define mapper payload shapes for profile update and block semantics.
3. Add delete/tombstone behavior where applicable.
4. Extend smoke and live E2E fixtures to include profile/block assertions.
5. Keep one-way direction and manual onboarding unchanged.

## Acceptance Criteria
- Deterministic tests cover map/ingest/publish for profile and block.
- Local strict live E2E still passes with expanded record set.
- No regression for existing `post/like/repost/follow` behavior.

## Deferred to Following Cycle
- Onboarding automation improvements (auto-discovery, account lifecycle workflows, operator UX around enrollment).
