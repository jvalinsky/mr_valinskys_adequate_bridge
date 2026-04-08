# Documentation Guide

This guide applies to the living docs in this repository: the root [README](../README.md), the files in [`docs/`](./README.md), and the infra READMEs under `infra/`.

## Audience and Scope

- `README.md` is the front door. It should give a new contributor or operator a correct picture of what the repository does today.
- `docs/runbook.md` is for operators and on-call work.
- Feature docs in `docs/*.md` explain current behavior and design constraints for developers.
- `infra/*/README.md` explains how a specific local or test stack works.
- Dated reports in `docs/investigations/`, `docs/ssbc-*.md`, and `docs/scratchpad/` are historical records. Keep the date and scope clear instead of rewriting them into timeless docs.

## Style

- Write from the code, tests, and command help. Do not document planned behavior as if it already exists.
- Keep the tone direct. Avoid sales language, filler transitions, and vague claims.
- Prefer short sections, tables, and explicit notes over long introductions.
- State when something is partial, optional, disabled by default, deprecated, test-only, or advisory.
- Use examples that are runnable in this repository. If an example needs secrets or external services, say so.
- Prefer file references over pasted implementation blocks when the source file is the real point of reference.

## When Docs Need Updates

| Change in code | Docs to check |
| --- | --- |
| CLI command, flag, or default changes | `README.md`, `docs/runbook.md`, related feature docs |
| Supported record types change | `README.md`, `docs/atproto-ssb-translation-overview.md`, `docs/atproto-ssb-record-translation.md` |
| Reverse-sync behavior changes | `README.md`, `docs/reverse-sync.md`, `docs/runbook.md` |
| New local/test stack or compose service | matching `infra/*/README.md`, `README.md`, `docs/README.md` |
| New package or major subsystem | `README.md`, `docs/README.md`, possibly a new focused doc |
| Broken or moved file references | every doc that links to the old path |

## Update Workflow

1. Start from the code or command help.
2. Identify every user-facing place that makes a claim about that surface area.
3. Update the primary doc first.
4. Update secondary docs and indexes in the same change when they would otherwise become stale.
5. Verify examples, flags, and links before finishing.

For command surfaces, prefer:

```bash
GOFLAGS=-mod=mod go run ./cmd/bridge-cli --help
GOFLAGS=-mod=mod go run ./cmd/bridge-cli start --help
GOFLAGS=-mod=mod go run ./cmd/ssb-client --help
```

## Links and References

- Use relative links that resolve from the current file.
- Link to the concrete file that backs the claim when possible.
- Avoid links to generated or machine-local paths.
- Keep "See also" sections small and relevant.

Examples:

```markdown
[Bridge Operator Runbook](./runbook.md)
[Local ATProto Stack](../infra/local-atproto/README.md)
[`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go)
```

## Historical Docs

- Do not delete scratchpads or dated investigations.
- Do not silently rewrite dated reports to match the current codebase.
- If a dated report needs context, add a short note that it is historical and point readers to the current doc.

## Verification

Before finishing a docs change:

1. Check command help for every CLI example you touched.
2. Check that every relative link you added resolves.
3. Re-read the changed prose for claims that are stronger than the code supports.

Useful commands:

```bash
rg -n "]\\([^)]*\\)" README.md docs/*.md infra/*/README.md
GOFLAGS=-mod=mod go run ./cmd/bridge-cli --help
GOFLAGS=-mod=mod go run ./cmd/ssb-client --help
```

## Related Docs

- [Documentation Index](./README.md)
- [Bridge Operator Runbook](./runbook.md)
- [Contributor Setup Profiles](./agents.md)
