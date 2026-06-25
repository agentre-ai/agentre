# Design System board + color-token audit — Design

> Date: 2026-06-25 · Repo: `agentre/` (desktop app)
>
> Goal: produce a reusable **Design System board** in the Pencil file, eliminate hardcoded
> colors in the frontend so every color flows through a theme-adaptive token, and bring the
> design docs back in lockstep with the code.

## Why

`docs/DESIGN.md` and `frontend/src/styles/globals.css` already define a complete, well-documented
token system (surfaces, brand, status, the 16-color agent palette, sidebar, destructive, charts).
Two gaps remain:

1. **The Pencil file (`/Users/codfrm/Code/agentre/agentre.pen`) has ~60 app-screen frames in
   light/dark but no design-system reference board** — no color-token swatch sheet and no
   base-component catalog you can copy from.
2. **The code violates "tokens, not literal colors" (DESIGN.md Constraint 1) in ~15 files** —
   hardcoded hex in classNames and Tailwind palette classes (`emerald-/amber-/red-/sky-/cyan-`)
   that bypass the existing `status-*` / `destructive` tokens, so they do not adapt cleanly
   across themes and fragment the palette.

## Scope (approved)

In scope, four deliverables:

- **A.** Pencil Design System board (color tokens + typography/radius/spacing + base components),
  both themes.
- **B.** New `code-surface` token family + retokenize the console/output boxes.
- **C.** Map palette classes → semantic tokens across the audited files.
- **D.** Doc updates (DESIGN.md primarily) + sanction the legitimate exceptions.

Out of scope: redrawing full app screens, adding an ESLint rule to enforce token usage (note it
as a future option), backend changes, anything outside `agentre/`.

---

## A. Pencil Design System board

New top-level frames placed in empty canvas space via `FindEmptySpace` (must not overlap existing
screens). Each new/modified root frame carries `placeholder: true` until done. Use `clip: true`.

Pull values verbatim from `globals.css` / DESIGN.md §3–5 (single source of truth).

1. **`设计系统 — 颜色 Tokens`** — each token family is a section; each token is a row with
   `name` + a **light chip** + a **dark chip** + hex labels (mirrors DESIGN.md §3 tables). Families:
   base surfaces & text · brand primary · secondary/muted/accent · border/input · status (4) ·
   **agent palette (16)** · sidebar · destructive · macOS traffic lights · **new code-surface** ·
   charts (note: `oklch`). Light chips on a light card, dark chips on a dark card, so contrast reads.
2. **`设计系统 — 排版·圆角·间距`** — type scale (`text-2xs` 11px / `text-xs` 12px / `text-sm` 14px),
   `font-sans` + `font-mono` samples, radius `sm`(4)/`md`(6)/`lg`(8)/`xl`(12) chips, chrome
   dimensions (title bar 44 / rail 56 / tab strip 38 / status bar 28) + spacing rhythm.
3. **`设计系统 — 基础组件 · Light`** and **`设计系统 — 基础组件 · Dark`** — Button (variants ×
   representative sizes), Badge (variants), Input/Textarea, Select, Checkbox/Switch/Radio,
   StatusDot + StatusPill (running/waiting/idle/error), AgentAvatar (sizes + a few colors) +
   RunAvatar, inline Card (`rounded-lg border bg-card`), Alert (default/destructive), Toast
   (success/error/warning/info), Dialog skeleton. Rendered with the matching theme's token values.

Verify after each board: layout not collapsed, nothing clipped, contrast sufficient, swatches
match the hex in `globals.css`.

## B. `code-surface` token family

The console/output boxes (`hooks-page.tsx` stdout & stderr `pre`, `local-command/card.tsx` output
`pre`) use hardcoded `bg-[#121418]` / `bg-[#1a1a1a]` + `text-[#9aa0ab]` / `text-[#e6e6e6]` /
`text-[#d4d4d4]`. These are monospace console surfaces, not hljs-highlighted code (per
`code-highlight.css`, only token colors are defined; the container is component-owned).

Add to `globals.css`:

- `--code-surface` — light: a light code surface aligned to the `<CodeBlock>` container; dark:
  near-black `#121418`-class. (Exact values set during implementation by reading CodeBlock's
  container classes so console output and code blocks read uniformly.)
- `--code-foreground` — primary monospace text on `code-surface`, both themes.
- `--code-muted-foreground` — de-emphasized monospace text (the `#9aa0ab` stdout case), both themes.

Expose each as `--color-code-*` in the `@theme inline` block so `bg-code-surface` /
`text-code-foreground` / `text-code-muted-foreground` work and switch with the theme.

Retokenize the 5 boxes. `hooks-page` stderr already uses `text-status-error` — keep it; only swap
its `bg`.

**Deliberate visual change to call out:** in light theme these boxes become a light surface instead
of staying black. This is the point (theme adaptation) and matches how CodeBlock already renders.

## C. Palette classes → semantic tokens

Audited files (from grep, excluding tests): `background-tasks/{chip,popover}`,
`canonical-tool/{tool-permission,user-ask,raw}/card`, `tool-approval/card`, `local-command/card`,
`remote-fs-picker`, `terminal/terminal-panel`, `update-section`, `remote-devices/*`,
`agent-backends`, `project-settings-drawer`, `ui/dialog` (scrim).

Mapping rules (each **value-checked** against the palette it replaces for visual fidelity):

| Palette use (semantics) | Token |
| --- | --- |
| `emerald-*` success/done/online | `status-running`, `bg status-running-bg`, fg `status-running-foreground` |
| `amber-*` waiting/attention | `status-waiting`, `bg status-waiting-bg` |
| `red-*` error/danger | `destructive` / `destructive-soft` / `status-error` |
| decorative `sky/cyan/green` (file-type icons, dots) | case-by-case — keep semantic intent, map to `primary-text`/`muted-foreground`/an agent token; do **not** invent a new color |

Where a palette class already pairs `dark:` variants, fold both into the single token (the token
carries its own dark value). Verify `status-*` dark values match the existing `dark:` palette
(e.g. `status-running` dark `#34d399` == `emerald-400`).

**Judgment calls (kept as sanctioned neutrals unless told otherwise):**

- `types.ts` `neutral: "bg-neutral-600"` — the documented neutral-agent identity fallback; keep.
- `ui/dialog` scrim `bg-slate-900/25 dark:bg-black/70` and custom `box-shadow rgba(0,0,0,…)` —
  neutral, theme-independent scrim/shadow; DESIGN.md explicitly has **no `--shadow-*` tokens**.
  Keep, and document as a sanctioned exception rather than force-tokenize.

## D. Docs

- **DESIGN.md**: add the `code-surface` token rows (§3, new subsection); explicitly list the
  sanctioned exceptions — `terminal-theme.ts` xterm ANSI palette (xterm can't consume CSS vars),
  `#94a3b8` slate avatar fallback (already mentioned in §3.6), neutral black-alpha shadows/scrim;
  add a pointer to the new Pencil Design System board. Re-verify every token table still matches
  `globals.css` after the additions.
- Leave `frontend.md` rules as-is (they already state the no-literal-color constraint); optionally
  note the future option of an ESLint guard against palette classes.

---

## Verification & TDD note

These changes are mechanical className swaps + CSS token additions. There is **no meaningful
red-test** for "uses token X instead of literal Y" — flagged per the repo's strict-TDD rules.
Verification instead:

1. **Value fidelity** — every token replacing a palette class is value-checked against
   `globals.css` so the swap is visually faithful (or a deliberate, called-out change).
2. **`make lint`** (golangci-lint + ESLint) green; **frontend vitest** green
   (`cd frontend && pnpm test`).
3. **Both-themes visual pass** of the touched surfaces.
4. **Pencil**: per-board checklist (layout intact, no clipping, contrast, swatch == hex).

No backend tests are affected. The diff stays within `agentre/`: `globals.css`, the ~15 audited
frontend files, `DESIGN.md`, this spec, and the `.pen` design file.

## Risks

- **Palette→token drift**: a token's dark value not exactly matching the old `dark:` palette would
  shift a color. Mitigation: the value-fidelity check (1) above; call out any intentional shift.
- **Pencil overlap**: new boards must use `FindEmptySpace` and never overlap the 60+ existing
  frames; keep `placeholder: true` until each board is verified.
- **Scope creep in the audit**: only touch color literals; **no** drive-by refactor, rename, or
  formatter pass in the audited files (repo Constraint 4).
