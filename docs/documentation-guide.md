# Documentation Guide for AI Assistants

> This guide explains how to update documentation in this project. For AI agent setup instructions, see [CLAUDE.md](../CLAUDE.md) and [Agent Setup Profiles](./agents.md).

---

## Overview

This project uses a structured documentation system with:

1. **Primary docs** — `README.md`, `docs/runbook.md`, `docs/README.md`
2. **Reference docs** — `docs/atproto-ssb-*.md`, `docs/ssb-*.md`, `docs/rate-limiting.md`
3. **Scratchpads** — `docs/scratchpad/` — development notes and drafts

---

## Key Principles

1. **Source of truth is the code** — Documentation must match what the code actually does
2. **Cross-link explicitly** — Docs should reference each other using relative links
3. **Keep scratchpads archival** — Don't delete old scratchpads, mark them as "Historical"
4. **One doc at a time** — Don't update multiple docs in a single edit pass

---

## When to Update Docs

Update docs when you:

| Action | Doc Impact |
|--------|-----------|
| Add/remove/change CLI flag | Update README CLI table, runbook flags section |
| Add new record type | Update README "Supported Record Types" table |
| Add new package | Update README "Internal Packages" table |
| Add new script | Update README "Scripts" table |
| Add new feature | Create new doc in `docs/` + link from docs/README.md |
| Change constants | Update docs/README.md code references |
| Update infra | Update `infra/*/README.md` + link to runbook |

---

## Doc Update Workflow

### Step 1: Audit Against Code

Find the source of truth for what you're updating:

| What to Update | Source File(s) |
|---------------|----------------|
| CLI commands & flags | `cmd/bridge-cli/main.go` — look for `Name:`, `Usage:`, `Value:` fields |
| Record types | `internal/mapper/mapper.go` — `RecordType*` constants |
| Package descriptions | `go doc ./internal/<pkg>` or `// Package` comment |
| Script inventory | `ls scripts/*.sh` + header comments |
| Constants | `internal/config/constants.go` |
| Runbook flags | `cmd/bridge-cli/main.go` flag definitions |

### Step 2: Identify Changes Needed

- Compare doc table/description against source
- Note any discrepancies: missing entries, outdated descriptions, renamed items

### Step 3: Make the Change

For small changes (typos, link fixes):
- Edit directly

For substantive changes (new flags, new packages):
- Create a scratchpad draft first: `docs/scratchpad/NNN-doc-update-<topic>.md`
- Apply the draft, then verify

### Step 4: Add Cross-Links

When adding new docs or updating major sections, ensure:

1. **Index link** — Add to `docs/README.md` under appropriate section
2. **See Also footer** — Add "## See Also" section linking to related docs
3. **From README** — If it's a feature, link from relevant README section

### Step 5: Verify

- Check that all relative links resolve (file exists)
- For CLI changes: `go run ./cmd/bridge-cli --help` matches docs
- For packages: `go doc ./internal/<pkg>` works

---

## Link Standards

### Good Links

```markdown
[Rate Limiting](./rate-limiting.md)
[Bridge Operator Runbook](./runbook.md)
[CLAUDE.md](../CLAUDE.md)
[Agent Setup Profiles](./agents.md)
```

### Avoid

- Absolute paths like `/Users/jack/...`
- Links to files that don't exist
- Orphaned docs with no incoming links

---

## Required Link Patterns

### New Feature Doc (`docs/rate-limiting.md` example)

```markdown
# Per-DID Rate Limiting

See also: [docs index](./README.md), [runbook](./runbook.md)

## Overview
...

---

## See Also

- [Bridge Operator Runbook](./runbook.md)
- [Documentation Index](./README.md)
```

### Index Doc (`docs/README.md`)

Add new doc to appropriate section:

```markdown
## ATProto to SSB Bridge

- [New Doc](./new-doc.md)
- [Existing Doc](./existing-doc.md)
```

### Infra Doc (`infra/*/README.md`)

Add "See Also" footer:

```markdown
## See Also

- [Bridge Operator Runbook](../docs/runbook.md)
```

---

## Scratchpad Management

Scratchpads are in `docs/scratchpad/`:

| Status | Meaning |
|--------|---------|
| Historical | Completed work, implementation in source |
| Pending | In progress or not fully implemented |
| Reference | Draft content incorporated elsewhere |

**Never delete scratchpads** — they're part of the project's decision history.

---

## Quick Reference

```bash
# Audit specific doc
/deciduous update-docs readme

# Audit all docs
/deciduous update-docs all

# Check links manually
ls docs/*.md

# Verify CLI matches docs
go run ./cmd/bridge-cli start --help
```

---

## Related Files

- [`.claude/commands/update-docs.md`](../.claude/commands/update-docs.md) — Full audit command
- [`docs/README.md`](./README.md) — Documentation index
- [`docs/runbook.md`](./runbook.md) — Operational procedures
- [`CLAUDE.md`](../CLAUDE.md) — AI agent instructions
- [`docs/agents.md`](./agents.md) — Setup profile reference