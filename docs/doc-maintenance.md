# Documentation Maintenance and Fact-Checking Guide

> **Read this before adding, editing, reordering, or reviewing any contributor doc (`AGENTS.md`, `CLAUDE.md`, `docs/*`).** It has two jobs:
> keep the doc set **orderly** (links resolve, the index is current, nothing is duplicated), and keep every assertion **true for the code on the current branch**.

## Why This Doc Exists

Contributor docs describe a living code base, so two kinds of failure keep recurring:

- **Stale facts** — a package gets renamed, a count changes, a file moves, but the doc keeps the old value. A real example from this repo: `docs/architecture.md`'s
  `internal/pkg/` list was missing `ccoauth` / `clienv` / `pty`, and its `pkg/` list was missing `piagent` / `agentred` (fixed casually in this pass).
- **Branch leakage** — work that only lives on some feature branch gets written into the docs as if it were already the state of `main`. Agentre runs many feature
  branches in parallel over long periods (the current branch while writing this doc is `feat/issue-tracker-v1`); with unmerged code sitting in the working tree, it is all too easy to slip it into `main`'s docs.

### Agentre's Handling Principle: Stale Means Fix or Delete, Don't Leave Deprecated Content

**Agentre is not yet released and carries no compatibility burden** — migrations / refactors can hard delete old data, with no compatibility layer and no release notes needed.
**Same for docs: when you find a stale / invalid fact, fix it or delete it outright; do not leave the invalid content in the doc behind a "(deprecated)" or "the old version was…" note.**
Keeping it around only makes readers unsure which line is current. The only exception is "planned, not yet landed" content — that either goes into the docs of its corresponding branch, or is **explicitly marked** as planned;
it must never be written as if already released.

## Key Rule: If `git grep` Can't Find It, Don't Write It

**If you can't `git grep` it in the committed code on the current branch, don't claim in the docs that it exists.** Always verify with git-aware commands
(`git grep` / `git ls-files` / `git ls-tree`); **do not** use bare `rg` / `ls` — those will count **uncommitted / unmerged** files in the working tree too, so the feature-branch code in
your checkout will masquerade as released (the "branch leakage" above is exactly how this happens). Planned / feature-branch-specific content either stays in that branch's docs, or is clearly marked as planned.

> Cross-repo reminder: the workspace root (`/Users/codfrm/Code/agentre`) wraps `agentre/` and `agentre-server/`, **two mutually independent git
> repositories**. This guide only covers `agentre/`. Don't verify facts about `agentre-server` with `agentre`'s commands, or vice versa; neither module path
> (`agentre` / `agentre-server`) has a VCS prefix, so don't invent `github.com/...`.

## Doc Set and Responsibilities (Don't Duplicate — Cross-Link)

| Doc | What it owns |
| --- | --- |
| [`../../CLAUDE.md`](../../CLAUDE.md) | **Workspace root**: facts and invariants spanning both repos (go.work, the two repos commit independently, the cago framework). |
| [`../CLAUDE.md`](../CLAUDE.md) | Just `@import`s `AGENTS.md`; holds no content of its own. |
| [`../AGENTS.md`](../AGENTS.md) | **Single source of truth for the agent guide**: engineering principles, high-priority constraints, high cohesion / low coupling, key constraints, common commands; also indexes the `docs/*` below. |
| [`architecture.md`](./architecture.md) | Project layout, cago layering conventions, remote execution architecture, `AppDataDir` storage paths, database and migration flow, list of generated files. |
| [`development.md`](./development.md) | The concrete "how to": TDD/BDD, SOLID, high cohesion / low coupling, Fix Discipline, the test stack, commit style, logging conventions. |
| [`frontend.md`](./frontend.md) | shadcn `@/components/ui/*` conventions, i18n, frontend structure, pnpm, formatting / lint, module path. |
| [`debugging.md`](./debugging.md) | Diagnosing runtime issues: SQLite / log commands, table → feature mapping, reproduction commands, common pitfalls. |
| [`agent-backend.md`](./agent-backend.md) | The full path to wiring in a new AI Agent backend (entity / migration / runtime / translator / capability / daemon import / frontend gating). |
| [`doc-maintenance.md`](./doc-maintenance.md) | This guide: doc organization rules + fact-checking / anti-drift discipline. |
| [`README_zh.md`](./README_zh.md) / [`../README.md`](../README.md) | The user-facing Chinese / English project README — **not** a docs index; don't stuff contributor conventions into it. |
| `superpowers/{plans,specs}/*` | Date-archived historical plan / spec snapshots, **not updated alongside the code**; when referencing one, note that it is the archived snapshot of some design, not a living doc. |

**Agentre has no `docs/README.md` index file** — the docs index role is played by the **"Development Conventions (required reading)" section of `AGENTS.md`**.
When you add / move / delete `docs/*`, keep that section and the "Doc Set and Responsibilities" table above in sync.

When you move a fact, move it to **the doc that owns it** and cross-link — never copy the same fact into two places, or they will eventually drift.

## Checklist 1 — Organization (Run Every Time You Change a Doc)

- [ ] Added / renamed / deleted a doc → update the "Development Conventions (required reading)" list in [`AGENTS.md`](../AGENTS.md), the "Doc Set and Responsibilities" table here, **and** everywhere that references it.
- [ ] All relative links resolve (run the link check in *One-Shot Verification* below).
- [ ] Nothing that only exists on a feature branch is written as the state of `main` — either delete it, or explicitly mark it "planned (branch `X`)".
- [ ] No fact is duplicated across multiple docs; the doc that owns it holds it, the rest link to it.
- [ ] **Stale / invalid content has been fixed or deleted outright, with no "deprecated" / "old version" note left behind** (see Agentre's handling principle above).

## Checklist 2 — Fact-Checking (When a Doc States Concrete Content)

Verify **every** concrete assertion against the code. Common assertion types and how to check them:

| Assertion in the doc | What to verify it with |
| --- | --- |
| The two binaries exist (`agentre` / `agentred`) | `git ls-files main.go cmd/agentred/main.go` |
| service domain package list | `git ls-tree --name-only -d HEAD internal/service/` |
| repository / entity package list | `git ls-tree --name-only -d HEAD internal/repository/ internal/model/entity/` |
| `internal/pkg/` cross-cutting package list (**#1 drift source**) | `git ls-tree --name-only -d HEAD internal/pkg/` |
| external `pkg/` list | `git ls-tree --name-only -d HEAD pkg/` |
| `internal/daemon/` subpackages | `git ls-tree --name-only -d HEAD internal/daemon/` |
| Wails binding "one file per domain" | `git ls-files internal/app/` (exclude `*_test.go`) |
| Some interface / identifier exists **under this exact name** (renames are the #1 source of drift) | `git grep "type Xxx interface" -- internal` / `git grep "func RegisterXxx" -- internal/repository` |
| repository uses the `Register` / accessor pattern | `git grep -n "^func Register" -- internal/repository` |
| migration count / naming prefix (`YYYYMMDDNNNN`) | `git ls-files 'migrations/*.go'` + `git grep -oE "migration[0-9]{12}" -- migrations/migrations.go` |
| Counts ("N migrations", "N languages", "N tables") | Enumerate from the canonical list — don't trust prose, don't trust memory |
| i18n locale language count | `git ls-files 'frontend/src/i18n/locales/*/common.json'` (should be `zh-CN` + `en`) |
| frontend path alias | the `aliases` block in `frontend/components.json` |
| localStorage keys (`agentre.theme` / `windowSize` / `lastPath`) | `git grep -nE "agentre\.(theme\|windowSize\|lastPath)" -- frontend/src` |
| `AppDataDir` paths / database table names | Cross-check `migrations/` + the entity's GORM tags; for table structure use the live DB `.schema` (see [debugging.md](./debugging.md)), don't go from memory |
| `.golangci.yml` exceptions / `//nolint` | `.golangci.yml` + `git grep -n "nolint:" -- internal` |
| cago framework import path | `git grep "github.com/cago-frame/cago" -- go.mod` |
| Signatures / constructors / switch branches | Open the file and compare parameter by parameter; don't guess |

Four pitfalls hit over and over:

- **Working tree ≠ committed.** Bare `rg` / `ls` match **uncommitted** files in the working tree, so the feature-branch code checked out locally but not yet merged into
  `main` reads as if already released — this is exactly the branch leakage failure above. Always use `git grep` / `git ls-files` /
  `git ls-tree`, so only committed code counts.
- **Don't mix up the repos.** `agentre/` and `agentre-server/` are two independent repos; don't use one repo's commands to verify the other's facts.
- **Counts drift silently.** For every number the docs state, enumerate it live from the canonical list; don't trust prose, don't trust memory.
- **Generated files are not a source of truth.** `frontend/wailsjs/` (Wails-generated, **gitignored**, invisible to `git ls-files`) and
  `internal/**/mock_*/` (mockgen output) are both derived files. Don't treat them as a fact source, and don't `git ls-files` them and then report "MISSING" —
  they simply aren't under version control (for the list, see "Generated / self-managed files" in [architecture.md](./architecture.md)).

## One-Shot Verification

Deliberately all git-aware commands: each check reads **committed** code only, so uncommitted feature-branch files in the checkout won't be reported as "existing".
Run from the `agentre/` repo root and check the output against the docs line by line:

```bash
echo "== two binaries =="
for f in main.go cmd/agentred/main.go; do
  git ls-files --error-unmatch "$f" >/dev/null 2>&1 && echo "ok   $f" || echo "missing/uncommitted $f"
done
echo "== service domain packages =="; git ls-tree --name-only -d HEAD internal/service/
echo "== repository packages =="; git ls-tree --name-only -d HEAD internal/repository/
echo "== entity packages =="; git ls-tree --name-only -d HEAD internal/model/entity/
echo "== internal/pkg cross-cutting packages (#1 drift source) =="; git ls-tree --name-only -d HEAD internal/pkg/
echo "== external pkg/ =="; git ls-tree --name-only -d HEAD pkg/
echo "== internal/daemon subpackages =="; git ls-tree --name-only -d HEAD internal/daemon/
echo "== Wails bindings (one file per domain, exclude _test) =="; git ls-files internal/app/ | grep -vE '_test\.go$'
echo "== repository Register/accessor =="; git grep -n "^func Register" -- internal/repository
echo "== migration count + registered identifiers =="
git ls-files 'migrations/*.go' | grep -vE '_test\.go$|/migrations\.go$' | wc -l
git grep -hoE "migration[0-9]{12}" -- migrations/migrations.go | sort -u
echo "== i18n locale languages =="; git ls-files 'frontend/src/i18n/locales/*/common.json'
echo "== frontend path aliases =="; git show HEAD:frontend/components.json | grep -A6 '"aliases"'
echo "== localStorage keys =="; git grep -nE "agentre\.(theme|windowSize|lastPath)" -- frontend/src
echo "== golangci nilerr exception =="; git grep -n "nolint:nilerr" -- internal/service
echo "== cago import =="; git grep -n "github.com/cago-frame/cago" -- go.mod
```

Link integrity — confirm every relative markdown link in the core docs resolves:

```bash
for doc in AGENTS.md CLAUDE.md docs/architecture.md docs/development.md docs/frontend.md \
           docs/debugging.md docs/agent-backend.md docs/doc-maintenance.md; do
  grep -oE '\]\(([^)]+)\)' "$doc" | sed -E 's/^\]\(|\)$//g' | grep -vE '^https?:|^#|^mailto:' | while read -r link; do
    target="$(dirname "$doc")/${link%%#*}"
    [ -e "$target" ] && echo "ok     $doc → $link" || echo "BROKEN $doc → $link"
  done
done
```

## What to Do When You Find an Inconsistency

Change the **docs** to match the code — the code on the current branch is the source of truth. Exception: if it is **the code itself that's wrong** (a real bug), follow
[development.md](./development.md)'s Fix Discipline — write a failing regression test first, then fix the code, and explain it in the PR. Either way,
**never** silently skip a check you didn't satisfy — call it out in the PR description / conversation so the reviewer can confirm.

When fixing a stale fact, remember Agentre's handling principle: **fix it or delete it outright, don't leave a deprecated note.** And don't casually make unrelated drive-by
changes (rename sweeps / formatter passes / import reordering) — those bury the real doc fix and break review.
