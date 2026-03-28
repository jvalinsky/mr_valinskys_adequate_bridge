---
description: Update project documentation based on current codebase state
allowed-tools: Read, Glob, Grep, Bash(go doc:*, go build:*, ls:*, head:*, diff:*, deciduous:*, git diff:*, git log:*), Write, Edit
argument-hint: [section-or-file] (e.g., "readme", "runbook", "packages", "scripts", "all")
---

# Update Documentation

**Audit project docs against the codebase and fix drift.**

This skill compares each documentation artifact against its source of truth in the code, drafts corrections in scratchpad files, then applies them. It follows a scratchpad-first workflow to avoid context issues when editing large docs.

---

## Step 1: Create Goal Node

```bash
deciduous add goal "Update documentation: $ARGUMENTS" -c 90 --prompt-stdin << 'EOF'
[INSERT VERBATIM USER REQUEST]
EOF
```

Store the goal ID for linking later.

---

## Step 2: Determine Scope

Parse `$ARGUMENTS` to decide what to audit:

| Argument | Scope |
|----------|-------|
| `readme` | README.md only |
| `runbook` | docs/runbook.md only |
| `packages` | Package doc comments (doc.go / `// Package` lines) only |
| `scripts` | Scripts table in README only |
| `all` or empty | Full audit across all docs |
| A file path | That specific doc file |

---

## Step 3: Audit Docs Against Sources of Truth

For each doc in scope, compare against its canonical source:

### README.md — CLI Commands
- **Source:** `cmd/bridge-cli/main.go` — grep for `Name:` and `Usage:` fields
- **Check:** Every command in main.go appears in the CLI Commands table. Flag descriptions match.

### README.md — Supported Record Types
- **Source:** `internal/mapper/mapper.go` — `RecordType*` constants and `MapRecord` switch cases
- **Check:** Every record type constant has a row in the Supported Record Types table.

### README.md — Internal Packages Table
- **Source:** Run `go doc ./internal/<pkg>` for each package under `internal/`
- **Check:** Every package is listed. Descriptions match `// Package` comments.
- **Discovery:** `ls internal/` to find all packages, including any new ones not yet in the table.

### README.md — Scripts Table
- **Source:** `ls scripts/*.sh` + first 5 lines of each script for header comments
- **Check:** Every script has a row. Descriptions are accurate.

### docs/runbook.md — CLI Flags
- **Source:** `cmd/bridge-cli/main.go` — flag Name, Usage, Value fields for `start`, `backfill`, `retry-failures`, `serve-ui` commands
- **Check:** Flag names in runbook examples match code. Default values match. No removed/renamed flags referenced.

### reference/README.md — Directory Index
- **Source:** `ls reference/`
- **Check:** Every subdirectory has a row in the table. (Note: this file is gitignored but maintained locally.)

### Package Doc Comments
- **Source:** `go doc ./internal/<pkg>` for each package
- **Check:** Every package has a `// Package` comment (via doc.go or main source file). Comment is accurate.

---

## Step 4: Report Findings

List each discrepancy found. Group by doc file:

```
README.md:
- CLI Commands: missing `new-command` (added in main.go:XXX)
- Packages: `internal/newpkg` not in table
- Scripts: `new_script.sh` not listed

docs/runbook.md:
- Flag `--old-flag` renamed to `--new-flag` in code

Packages:
- internal/newpkg/ has no doc.go or package comment
```

If no discrepancies found, report that docs are up to date and skip to Step 7.

---

## Step 5: Draft Changes in Scratchpad

For each doc that needs updates, create a numbered scratchpad draft:

1. Find the next available number: `ls docs/scratchpad/ | tail -1`
2. Write draft to `docs/scratchpad/NNN-docs-update-<section>.md`
3. Each draft contains ONLY the changed section(s), not the entire doc

**One draft per doc file.** Don't try to edit multiple docs in a single draft.

---

## Step 6: Apply Drafts

For each scratchpad draft:
1. Read the draft
2. Read the target doc
3. Apply the changes using Edit (prefer surgical edits over full rewrites)
4. For new doc.go files, use Write

After applying all changes, verify:
- `go build ./cmd/bridge-cli` — project still builds
- `go doc ./internal/<pkg>` — for any packages whose docs changed
- Spot-check tables against source commands

---

## Step 7: Decision Graph Outcome

```bash
deciduous add action "Audited and updated docs: <summary>" -c 90 -f "<files-changed>"
deciduous link <goal_id> <action_id> -r "Documentation update"

# After commit:
deciduous add outcome "Docs updated: <summary>" -c 95 --commit HEAD
deciduous link <action_id> <outcome_id> -r "Update complete"
```

---

## Source of Truth Reference

Quick reference for where each doc artifact's truth lives:

| Doc Artifact | Source File(s) |
|-------------|----------------|
| CLI commands & flags | `cmd/bridge-cli/main.go` |
| Record types | `internal/mapper/mapper.go` |
| Package descriptions | `// Package` comment in each package's main .go file |
| Script inventory | `scripts/*.sh` |
| Go version | `go.mod` line 3 |
| Runbook flag accuracy | `cmd/bridge-cli/main.go` flag definitions |
| Reference dirs | `reference/` directory listing |

---

## Important Rules

- **Never edit a doc directly without drafting first** — use scratchpad for any non-trivial change
- **One doc at a time** — don't try to update README and runbook in the same edit pass
- **Verify after each doc** — catch errors before moving to the next
- **Preserve existing prose** — only change what's actually drifted. Don't rewrite sections that are still accurate
- **Don't add content beyond what the code supports** — docs should reflect what exists, not what's planned

---

**Now audit and update: $ARGUMENTS**
