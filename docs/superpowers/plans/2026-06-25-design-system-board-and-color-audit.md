# Design System board + color-token audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reusable Design System board to the Pencil file, eliminate hardcoded colors in the frontend so every color flows through a theme-adaptive token, and bring the design docs back in lockstep.

**Architecture:** Four threads — (1) new `code-surface` token family for console/output surfaces; (2) mechanical palette-class → semantic-token swaps across ~15 files; (3) three new Pencil boards (tokens, type/radius/spacing, components) in both themes; (4) DESIGN.md updates that document the new token + the sanctioned exceptions + a pointer to the Pencil board. Source of truth for all color values is `frontend/src/styles/globals.css`.

**Tech Stack:** React 19 + Tailwind CSS v4 (no config, `@theme inline` in `globals.css`) + shadcn/ui; Pencil MCP for the `.pen` design file; Vitest + ESLint for verification.

## Global Constraints

- **Single source of truth for color values:** `frontend/src/styles/globals.css`. Tokens live under `:root` (light) + `.dark` (dark) and are exposed as `--color-*` in `@theme inline`. Copy hex verbatim. (DESIGN.md Constraint 1.)
- **No literal colors in app code:** never a hex, `rgb()`, or palette class (`text-blue-500`). Use a semantic token (`bg-card`, `text-status-running`, …).
- **Diff stays inside `agentre/`** and touches only color literals + the Pencil file + docs. **No** drive-by refactor / rename / formatter pass / import reorder in the audited files (repo Constraint 4).
- **Pre-existing dirty working tree:** the branch `develop/wyz` already has unrelated uncommitted changes (e.g. `canonical-tool/user-ask/card.tsx` is already `M`). When staging, stage **only** the files listed for the task, and for files that are already dirty, stage just your color hunks (`git add -p`). Flag any overlap.
- **Commits are gated on user request.** The harness rule is "commit only when the user asks." Treat each task's Commit step as conditional — do it only if the user has opted in; otherwise leave changes staged-but-uncommitted and report.
- **i18n:** no new visible copy is introduced by this plan; if any appears, it must go through `t(...)` + zh-CN/en. (Not expected here.)
- **Pencil hygiene:** every new/modified root frame keeps `placeholder: true` until verified; use `FindEmptySpace` so new boards never overlap the 60+ existing screens; `clip: true` on screen frames.

---

## Task 1: `code-surface` token family + retokenize console boxes

**Files:**
- Modify: `frontend/src/styles/globals.css` (`:root`, `.dark`, `@theme inline` blocks)
- Modify: `frontend/src/components/agentre/hooks-page.tsx:386,396,590`
- Modify: `frontend/src/components/agentre/local-command/card.tsx:93-94`

**Interfaces:**
- Produces: Tailwind classes `bg-code-surface`, `text-code-foreground`, `text-code-muted-foreground` (theme-adaptive). DESIGN.md (Task 7) documents these.

- [ ] **Step 1: Add the token values to `globals.css`.**

In `:root` (after the Destructive block, before the Agent palette comment) add:

```css
  /* Code / console surfaces — monospace output (hook logs, command output).
     Light-adaptive: a light code surface in light theme, deep console near-black in dark. */
  --code-surface: #f4f4f5;
  --code-foreground: #3f3f46;
  --code-muted-foreground: #71717a;
```

In `.dark` (in the matching position) add:

```css
  /* Code / console surfaces */
  --code-surface: #121418;
  --code-foreground: #e6e8eb;
  --code-muted-foreground: #9aa0ab;
```

In `@theme inline` (next to the other `--color-*` mappings) add:

```css
  --color-code-surface: var(--code-surface);
  --color-code-foreground: var(--code-foreground);
  --color-code-muted-foreground: var(--code-muted-foreground);
```

- [ ] **Step 2: Retokenize `hooks-page.tsx`.**

Line 386 (stdout `pre`): replace `bg-[#121418]` → `bg-code-surface` and `text-[#9aa0ab]` → `text-code-muted-foreground`.
Line 396 (stderr `pre`): replace `bg-[#121418]` → `bg-code-surface` (keep `text-status-error`).
Line 590 (script `Textarea`): replace `bg-[#121418]` → `bg-code-surface` and `text-[#e6e6e6]` → `text-code-foreground`.

- [ ] **Step 3: Retokenize `local-command/card.tsx`.**

Line 93: replace `bg-[#1a1a1a]` → `bg-code-surface`.
Line 94: replace `text-[#d4d4d4]` → `text-code-foreground`.

- [ ] **Step 4: Verify no hardcoded hex remains in those two files.**

Run:
```bash
cd frontend/src && grep -nE "\[#[0-9a-fA-F]{3,8}\]" components/agentre/hooks-page.tsx components/agentre/local-command/card.tsx
```
Expected: no output (exit 1).

- [ ] **Step 5: Lint + relevant tests green.**

Run:
```bash
cd frontend && pnpm exec eslint src/components/agentre/hooks-page.tsx src/components/agentre/local-command/card.tsx
pnpm test -- local-command
```
Expected: ESLint clean; tests pass (or "no tests found" for hooks-page).

- [ ] **Step 6 (gated): Commit.**

```bash
git add frontend/src/styles/globals.css frontend/src/components/agentre/hooks-page.tsx frontend/src/components/agentre/local-command/card.tsx
git commit -m "🎨 加 code-surface token,控制台输出框改走主题变量"
```

---

## Task 2: Palette classes → semantic tokens (status-semantic files)

The clear status-color cases. Mapping: `emerald-* → status-running(-bg)/(-foreground)`, `amber-* → status-waiting(-bg)`, `red-* → destructive/destructive-soft/status-error`. Each is value-faithful in light (`emerald-500 #10b981 == status-running`, `amber-500 #f59e0b == status-waiting`, `emerald-50 #ecfdf5 == status-running-bg`); `emerald-600`/`amber-600`/`green-500` unify to the running/waiting token (a minor, intentional hue tightening). Fold any existing `dark:` palette variant into the single token (the token carries its own dark value).

**Files (exact occurrences to change):**
- `frontend/src/components/agentre/background-tasks/background-tasks-chip.tsx:53` — `bg-green-500` → `bg-status-running`
- `frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx:55` — `bg-emerald-50 … text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400` → `bg-status-running-bg … text-status-running`
- `…/background-tasks-popover.tsx:57` — `bg-emerald-500` → `bg-status-running`
- `…/background-tasks-popover.tsx:158` — `bg-emerald-50 … text-green-700 dark:bg-emerald-500/15 dark:text-green-400` → `bg-status-running-bg … text-status-running`
- `…/background-tasks-popover.tsx:160` — `bg-green-500` → `bg-status-running`
- `frontend/src/components/agentre/tool-approval/card.tsx:64,72,74,92` — `border-amber-500/40`→`border-status-waiting/40`; `text-amber-500`→`text-status-waiting`; `text-emerald-500`→`text-status-running`; `bg-emerald-500/10 text-emerald-600`→`bg-status-running-bg text-status-running`
- `frontend/src/components/agentre/canonical-tool/tool-permission/card.tsx:127,143,145,166` — same amber→status-waiting / emerald→status-running mapping as tool-approval
- `frontend/src/components/agentre/canonical-tool/user-ask/card.tsx:342,349,350,389` — `bg-emerald-500/15 … text-emerald-600 dark:text-emerald-400`→`bg-status-running-bg … text-status-running`; `bg-amber-500/15 … text-amber-600 dark:text-amber-400`→`bg-status-waiting-bg … text-status-waiting`; `bg-amber-500`→`bg-status-waiting`; `text-emerald-500/70`→`text-status-running/70` **(file already dirty — stage only these hunks)**
- `frontend/src/components/agentre/canonical-tool/raw/card.tsx:163` — `bg-emerald-500/15 … text-emerald-600 dark:text-emerald-400` → `bg-status-running-bg … text-status-running`
- `frontend/src/components/agentre/local-command/card.tsx:17,18,22,23` — `dot: "bg-amber-500"`→`"bg-status-waiting"`; `pill: "bg-amber-500/15 text-amber-600 dark:text-amber-400"`→`"bg-status-waiting-bg text-status-waiting"`; emerald equivalents → status-running
- `frontend/src/components/agentre/update-section.tsx:424,729-732,789` — `text-emerald-500`→`text-status-running`; `border-emerald-500/30 bg-emerald-500/5`→`border-status-running/30 bg-status-running-bg`; `border-emerald-500/20`→`border-status-running/20`; `text-emerald-600`→`text-status-running`; `text-amber-500`→`text-status-waiting`
- `frontend/src/components/agentre/remote-devices/device-row.tsx:48` — `bg-emerald-500`→`bg-status-running`
- `frontend/src/components/agentre/remote-devices/device-providers-sync.tsx:204,251` — `text-emerald-500`→`text-status-running`
- `frontend/src/components/agentre/agent-backends.tsx:2194` — `border-amber-500/40 bg-amber-500/10 … text-amber-700 dark:text-amber-300` → `border-status-waiting/40 bg-status-waiting-bg … text-status-waiting`
- `frontend/src/components/agentre/project-settings-drawer.tsx:682` — `text-amber-600 dark:text-amber-500` → `text-status-waiting`

- [ ] **Step 1: Apply every replacement above** (exact substring swaps; do not touch surrounding markup).

- [ ] **Step 2: Verify the status palette classes are gone.**

Run:
```bash
cd frontend/src && grep -rnE "(text|bg|border|ring)-(emerald|amber|green)-[0-9]" \
  components/agentre/background-tasks components/agentre/tool-approval \
  components/agentre/canonical-tool components/agentre/local-command \
  components/agentre/update-section.tsx components/agentre/remote-devices \
  components/agentre/agent-backends.tsx components/agentre/project-settings-drawer.tsx | grep -v "__tests__"
```
Expected: no output (exit 1). (`bg-neutral-600` in `types.ts` is intentionally kept and not matched here.)

- [ ] **Step 3: Lint + tests for touched components.**

Run:
```bash
cd frontend && pnpm test -- user-ask tool-permission tool-approval local-command
pnpm exec eslint src/components/agentre/background-tasks src/components/agentre/canonical-tool src/components/agentre/update-section.tsx src/components/agentre/agent-backends.tsx
```
Expected: tests pass; ESLint clean. If a test snapshots a palette class, update the assertion to the token (color intent unchanged).

- [ ] **Step 4 (gated): Commit** (stage only the listed files; use `git add -p` for `user-ask/card.tsx`).

```bash
git commit -m "🎨 状态色统一走 status-* token,移除 emerald/amber/green 调色板类"
```

---

## Task 3: Palette classes → semantic tokens (decorative + terminal judgment cases)

The non-status cases that need a deliberate mapping (spec §C: keep semantic intent, don't invent a color).

**Files:**
- `frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx:95` — the subagent/task icon wrapper `bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300` → `bg-primary-soft text-primary-text` (brand-tinted identity, theme-adaptive).
- `frontend/src/components/agentre/remote-fs-picker.tsx:363,367` — file-tree icons. `text-cyan-500` (expand/chevron) → `text-muted-foreground`; `text-amber-500` (FolderIcon) → `text-primary-text` (keeps folders visually distinct from files via the brand tint, fully tokenized).
- `frontend/src/components/agentre/terminal/terminal-panel.tsx:249,255` — error banner. `border-red-700 bg-red-950/60 … text-red-100` → `border-status-error/40 bg-destructive-soft … text-destructive`; `border-red-700 … hover:bg-red-900` → `border-status-error/40 … hover:bg-destructive/10`. (Deliberate: the banner becomes theme-adaptive — soft red in light, instead of always near-black.)

- [ ] **Step 1: Apply the three replacements above.**

- [ ] **Step 2: Verify the decorative palette classes are gone.**

Run:
```bash
cd frontend/src && grep -rnE "(text|bg|border)-(sky|cyan|red)-[0-9]" \
  components/agentre/background-tasks/background-tasks-popover.tsx \
  components/agentre/remote-fs-picker.tsx components/agentre/terminal/terminal-panel.tsx
```
Expected: no output (exit 1). (`ui/dialog.tsx` scrim `bg-slate-900/25 dark:bg-black/70` is a sanctioned neutral — out of scope here.)

- [ ] **Step 3: Lint the touched files.**

Run:
```bash
cd frontend && pnpm exec eslint src/components/agentre/background-tasks/background-tasks-popover.tsx src/components/agentre/remote-fs-picker.tsx src/components/agentre/terminal/terminal-panel.tsx
```
Expected: ESLint clean.

- [ ] **Step 4 (gated): Commit.**

```bash
git add frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx frontend/src/components/agentre/remote-fs-picker.tsx frontend/src/components/agentre/terminal/terminal-panel.tsx
git commit -m "🎨 装饰/终端色改走 primary/destructive token(文件树·错误条主题自适应)"
```

---

## Task 4: Pencil — color tokens board

Build in `/Users/codfrm/Code/agentre/agentre.pen`. Use the Pencil MCP `batch_design` tool iteratively (one section per call). Read `get_editor_state(include_schema:true)` first if schema isn't in context.

**Approach:** `FindEmptySpace({width:1280,height:1600})` for a new root frame `设计系统 — 颜色 Tokens` (`placeholder:true`, `clip:true`, vertical layout, padding 40, gap 32). For each token family, a section frame with a heading + a grid of swatch rows. Each swatch row: token name (mono) + a **light chip** + a **dark chip** + the two hex labels. Chips are 56×40 rounded-md rectangles; light chips sit on a `#ffffff` card, dark chips on a `#17191c` card so both read.

**Token families & values (copy verbatim from `globals.css`):**
- Base surfaces & text: `background #fafafa/#17191c`, `foreground #18181b/#e6e8eb`, `card #ffffff/#1d2025`, `popover #ffffff/#262931`, `rail #e4e4e7/#0a0b0d`, `muted-foreground #71717a/#8a8d94`, `subtle-foreground #a1a1aa/#5a5d64`.
- Brand primary: `primary #3b6896/#5b8dbf`, `primary-foreground #ffffff/#0a1420`, `primary-soft #eef4fa/#1a2738`, `primary-text #3b6896/#8eb6dc`, `ring #3b6896/#5b8dbf`.
- Secondary/muted/accent: `secondary #f4f4f5/#262931`, `secondary-foreground #3f3f46/#c4c7cd`, `muted #f4f4f5/#1d2025`, `accent #f4f4f5/#383d47`, `accent-foreground #18181b/#e6e8eb`.
- Border/input: `border #e4e4e7/#2a2d34`, `border-strong #d4d4d8/#3a3e47`, `input #e4e4e7/#2a2d34`, `input-bg #ffffff/#17191c`.
- Status: `status-running #10b981/#34d399` + `-bg #ecfdf5/#0f2218`, `status-waiting #f59e0b/#fbbf24` + `-bg #fffbeb/#261d0d`, `status-idle #a1a1aa/#6a6d74`, `status-error #dc2626/#f87171`.
- Destructive: `destructive #dc2626/#f87171`, `destructive-foreground #ffffff/#fafafa`, `destructive-soft #fef2f2/#2a1414`.
- **Code surface (new):** `code-surface #f4f4f5/#121418`, `code-foreground #3f3f46/#e6e8eb`, `code-muted-foreground #71717a/#9aa0ab`.
- Agent palette (16): `agent-1 #2563eb/#60a5fa` … `agent-16 #9333ea/#c084fc` (full list in DESIGN.md §3.6 / `globals.css:70-85,161-176`). Render as a 4×4 grid, each cell name + light + dark chip.
- Sidebar: `sidebar #f4f4f5/#111316`, `sidebar-active-bg #ffffff/#262931`, `sidebar-icon #71717a/#8a8d94`, `sidebar-icon-active #3b6896/#5b8dbf` (+ the rest from §3.7).
- macOS traffic lights: `traffic-close #ff5f57`, `traffic-minimize #febc2e`, `traffic-zoom #28c840` (both themes equal).
- Charts: note `chart-1…5` are `oklch` (label them; a representative swatch is fine).

- [ ] **Step 1:** Create the root frame + the first two sections (base surfaces, brand). `batch_design` call #1.
- [ ] **Step 2:** Add the remaining scalar families (secondary/accent, border/input, status, destructive, code-surface, sidebar, traffic, charts). One or two `batch_design` calls.
- [ ] **Step 3:** Add the agent-palette 4×4 grid. One `batch_design` call (loop over the 16 tokens).
- [ ] **Step 4: Verify.** `snapshot_layout(parentId: <root>, problemsOnly:true)` → no clipping/overlap. `get_screenshot(nodeId: <root>)` → swatches match the hex; contrast readable. Fix in place (never delete-and-redo). Then `Update(root,{placeholder:false})`.

---

## Task 5: Pencil — typography · radius · spacing board

**Approach:** `FindEmptySpace` anchored to the Task 4 board (`direction:"right"`). Root frame `设计系统 — 排版·圆角·间距` (`placeholder:true`, `clip:true`, vertical, padding 40, gap 32).

**Content (values from DESIGN.md §5 / `globals.css`):**
- **Type scale:** `text-2xs` 0.6875rem/11px, `text-xs` 12px, `text-sm` 14px — each a sample line "The quick brown fox 敏捷狐狸 0123" at that size, labeled.
- **Fonts:** one `font-sans` sample + one `font-mono` sample (mono shows code: `claude-opus-4-8  /usr/local`), with the stack noted in a caption.
- **Radius:** four chips `rounded-sm` 4px / `rounded-md` 6px / `rounded-lg` 8px / `rounded-xl` 12px (rectangles with that corner radius, labeled).
- **Chrome dimensions:** a labeled bar list — title bar 44 · icon rail 56 · tab strip 38 · status bar 28 · sidebar default 320 (min 220 / max 640) · window min 860×640.
- **Spacing rhythm:** gap-2 / gap-3 swatches; card padding p-4–p-6 note.

- [ ] **Step 1:** Create root frame + type-scale + fonts sections. `batch_design` call.
- [ ] **Step 2:** Add radius chips + chrome-dimensions + spacing sections. `batch_design` call.
- [ ] **Step 3: Verify** via `snapshot_layout(problemsOnly:true)` + a `get_screenshot`; fix in place; `placeholder:false`.

---

## Task 6: Pencil — base components board (Light + Dark)

**Approach:** two sibling root frames `设计系统 — 基础组件 · Light` and `设计系统 — 基础组件 · Dark` via `FindEmptySpace` (anchored right of Task 5). Each `placeholder:true`, `clip:true`, vertical, padding 40, gap 28. Light frame fill `#fafafa`; Dark frame fill `#17191c`. Build the Light frame fully, verify, then `Copy` it to the Dark frame and override the section fills/text to the dark token values (faster + guarantees parity).

**Components to render (match DESIGN.md §6 + `components/ui`):**
- **Button** — variants `default`(bg `primary`, fg `primary-foreground`), `outline`, `secondary`, `ghost`, `destructive`, `link`; sizes `sm`(h-8)/`default`(h-9)/`lg`(h-10) + an `icon` button. Rounded-md.
- **Badge** — `default`/`secondary`/`destructive`/`outline`, `rounded-full`.
- **Input** + **Textarea** — `input-bg` fill, `input` border, rounded-md; one focused (ring `ring/50`).
- **Select** — closed trigger + a small open menu (`popover` surface, `shadow-md`).
- **Checkbox / Switch / Radio** — checked + unchecked.
- **StatusDot + StatusPill** — all four states: RUNNING (green), WAITING (amber), IDLE (gray), ERROR (red); pill is `font-mono text-2xs rounded-sm` on the status' tinted bg.
- **AgentAvatar** (sizes sm/md/lg, two agent colors e.g. `agent-7`/`agent-3`) + **RunAvatar** (`primary-soft` fill + `primary-text` glyph).
- **Card** — inline `rounded-lg border bg-card p-4` with a title + body.
- **Alert** — `default` + `destructive`.
- **Toast** — four rows: success (`status-running-bg`), error (`destructive-soft`), warning (`status-waiting-bg`), info (`primary-soft`); neutral `foreground` text + saturated icon (matches `globals.css` Sonner binding).
- **Dialog** — a small modal skeleton (`popover`/`card` surface, `rounded-xl`, header/body/footer).

- [ ] **Step 1:** Build the Light frame: Button + Badge + Input/Textarea/Select sections. `batch_design` call(s).
- [ ] **Step 2:** Light frame: Checkbox/Switch/Radio + Status (Dot/Pill) + Avatars sections.
- [ ] **Step 3:** Light frame: Card + Alert + Toast + Dialog sections. Verify Light frame (`snapshot_layout` + screenshot); fix in place; `placeholder:false` on Light.
- [ ] **Step 4:** `Copy` the Light root to the Dark root; override section/background fills + text colors to the dark token values; set Dark fill `#17191c`. Verify Dark frame screenshot for contrast; `placeholder:false` on Dark.

---

## Task 7: DESIGN.md + doc updates

**Files:**
- Modify: `frontend ../docs/DESIGN.md` → actual path `docs/DESIGN.md`

- [ ] **Step 1: Add the `code-surface` token subsection to §3.** After §3.11 Destructive (before §3.12 Elevation) insert a "§3.11a Code / console surface" table:

```markdown
### 3.11a Code / console surface

Monospace **console output** surfaces (hook stdout/stderr, local-command output) — theme-adaptive, distinct from the `secondary`-based `CodeBlock` container.

| Token / class | Light | Dark | Use |
| --- | --- | --- | --- |
| `code-surface` | `#f4f4f5` | `#121418` | Console/output box fill (`bg-code-surface`) |
| `code-foreground` | `#3f3f46` | `#e6e8eb` | Primary monospace text on `code-surface` |
| `code-muted-foreground` | `#71717a` | `#9aa0ab` | De-emphasized monospace text (stdout) |
```

- [ ] **Step 2: Add a sanctioned-exceptions note.** In §1 (after the "Use tokens, not literal colors" bullet) or a short new note near §12, list the *only* allowed literal-color sites and why:

```markdown
> **Sanctioned literal-color exceptions** (everything else must be a token): the xterm ANSI
> palette in [`terminal/terminal-theme.ts`](../frontend/src/components/agentre/terminal/terminal-theme.ts)
> (xterm.js can't consume CSS variables); the `#94a3b8` slate **avatar fallback** when agent meta is
> missing (§3.6); neutral black-alpha **shadows/scrim** (`box-shadow rgba(0,0,0,…)`, the `Dialog`
> backdrop) — there are no `--shadow-*` tokens by design (§3.12); and `bg-neutral-600` as the
> **`"neutral"` agent** identity fill (§3.6).
```

- [ ] **Step 3: Add a pointer to the Pencil Design System board.** In §0 ("What this doc owns") or §12 (Sources), add: the visual companion to this doc is the **设计系统** boards in `agentre.pen` (颜色 Tokens / 排版·圆角·间距 / 基础组件 · Light·Dark).

- [ ] **Step 4: Re-verify token tables match `globals.css`.**

Run:
```bash
cd frontend/src/styles && grep -E "^\s*--(code-surface|code-foreground|code-muted-foreground|status-running|status-waiting|primary-text):" globals.css
```
Expected: prints the values used in DESIGN.md. Spot-check the new §3.11a row against this output.

- [ ] **Step 5 (gated): Commit.**

```bash
git add docs/DESIGN.md docs/superpowers/specs/2026-06-25-design-system-board-and-color-audit-design.md docs/superpowers/plans/2026-06-25-design-system-board-and-color-audit.md
git commit -m "📝 DESIGN.md 记 code-surface token + 例外清单 + Pencil 设计系统板指针"
```

---

## Final verification (after all tasks)

- [ ] **Full audit re-scan** — no unsanctioned literals remain:

```bash
cd frontend/src && grep -rnE "(\[#[0-9a-fA-F]{3,8}\]|(text|bg|border|ring)-(emerald|amber|green|sky|cyan|red|rose|violet|purple|indigo|teal)-[0-9])" --include="*.tsx" --include="*.ts" components | grep -vE "__tests__|\.test\.|terminal-theme\.ts|types\.ts:.*neutral|dialog\.tsx.*slate-900"
```
Expected: only sanctioned hits (or none).

- [ ] **Lint + frontend tests green:** `cd frontend && pnpm test` and `make lint` (from `agentre/`).
- [ ] **Both-themes visual pass** of: hooks run logs, local-command output, background-tasks popover, tool-approval/permission cards, user-ask card, update-section, remote-fs-picker, terminal error banner.
- [ ] **Pencil:** the three boards render correctly in the canvas, swatches match `globals.css`, nothing clipped.

---

## Self-review notes

- **Spec coverage:** A (Pencil board) → Tasks 4–6; B (code-surface) → Task 1; C (palette→token) → Tasks 2–3; D (docs) → Task 7. All covered.
- **No new behavior to red-test** (color-literal swaps + CSS tokens + Pencil + docs) — verification is value-fidelity + lint + existing tests + visual/screenshot, as flagged in the spec and approved.
- **Type/name consistency:** the three new classes (`bg-code-surface`, `text-code-foreground`, `text-code-muted-foreground`) and the status tokens (`status-running(-bg)`, `status-waiting(-bg)`, `status-error`, `destructive(-soft)`) are used consistently across Tasks 1–3, 6, 7.
