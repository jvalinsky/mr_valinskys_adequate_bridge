# SSBC Compliance Merge

Date: 2026-04-02

Prompt:
- `review codex/ssbc-compliance worktree branch and make a detailed plan to merge`
- `PLEASE IMPLEMENT THIS PLAN: Merge Plan For codex/ssbc-compliance`

Scope:
- replay `codex/ssbc-compliance` onto current `main`
- preserve current ATProto/indexer/Indigo-exit behavior while porting SSBC compliance fixes
- keep the merge in logical commit groups

Working notes:
- `codex/ssbc-compliance` has two commits: one code commit (`d2b61bd`) and one migration-note commit (`802a633`).
- Current `main` is authoritative for ATProto, cursor semantics, and Indigo removal.
- The only real textual merge conflict identified ahead of time is in `internal/web/handlers/ui.go`.
- `cmd/bridge-cli/helpers.go` auto-merges but needs manual review because it combines ATIndex scheduler work from `main` with the compliance branch's room tunnel bootstrap rewrite.
- The compliance worktree has unrelated untracked workflow files that must stay out of this integration.
- Root-package test coverage on this machine is constrained by low disk space, so targeted package runs are safer than a full root `go test ./...`.
- Root `go` commands in this worktree must use `GOFLAGS=-mod=mod` because a stale local `vendor/` tree would otherwise shadow the live `internal/ssb` module.

Execution log:
- Created integration branch `codex/ssbc-compliance-merge` from current `main`.
- Replayed `d2b61bd` with `git cherry-pick -n` and resolved the expected `internal/web/handlers/ui.go` conflict by keeping the `main`-side `appbsky.LexBlob` type and ATProto wiring.
- Verified the merged `cmd/bridge-cli/helpers.go` keeps both the mainline ATIndex/cursor logic and the compliance branch's real `tunnel.announce` bootstrap path.
- Fixed sandbox-sensitive tests in `internal/room` and `internal/web/handlers` so targeted verification can run without IPv6/real-socket assumptions.
- Verified:
  - `go test ./...` from `internal/ssb`
  - `go test -vet=off ./cmd/room-tunnel-feed-verify`
  - `go test -vet=off ./internal/room`
  - `go test -vet=off ./internal/web/handlers`
  - `go test -vet=off ./cmd/bridge-cli`
- Confirmed the replayed code delta does not touch `go.mod` or `go.sum`.
