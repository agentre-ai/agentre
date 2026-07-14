# Chat Input `@` Mention References — Design

**Date:** 2026-07-14
**Branch:** `develop/wyz` (pathspec commits; concurrent sessions active — see Concurrency notes)
**Status:** Approved (design), pending implementation plan

## Goal

Add an `@`-triggered mention feature to the chat composer so a user can pick a
**project** or an **agent** from an autocomplete menu and insert it as a styled
chip. On send, each chip serializes to an XML tag embedded in the message text
(`<agent id="…">Name</agent>` / `<project id="…" path="…">Name</project>`). The
XML serves three purposes chosen by the maintainer:

1. **Structured context** — the tag lets the receiving agent unambiguously know
   which project/agent the user means (resolves free-text ambiguity).
2. **Project file/path attachment** — the project's absolute path rides in the
   `path` attribute so the running agent can read that project's files with its
   own file tools. **Path-only marker; no backend file loading.**
3. **Visual linking** — in the transcript the tag renders as a clickable chip
   that navigates to the referenced project/agent.

Explicitly **not** a routing/dispatch mechanism: `@`-mentioning an agent does
**not** hand the task to it. It is a prompt-enrichment marker only.

## Scoping decisions (locked)

1. **One `@` trigger for both types.** Typing `@` opens a single menu listing
   both agents and projects, grouped into **Agents** / **Projects** sections
   with type icons. Mirrors the single existing `/` slash menu.
2. **Styled chip display.** An inserted reference renders as a compact atom
   (icon + colored name) that deletes as one unit and serializes to the full XML
   tag only at send time. Raw XML is never shown in the editor.
3. **Enabled everywhere `AIChatInput` is used.** Wired into the shared composer
   (`chat-input/index.tsx`) so main chat, and any other composer mounting
   `AIChatInput`, all get it. Opt-in via one new prop (`mentionSources`); enabling
   broadly is trivial because the data sources are global.
4. **Path-only for `@project`.** XML carries the project's absolute path; the
   agent reads files itself. No backend change, no file-selection policy.
5. **Draft round-trip in scope.** When a message containing mentions is loaded
   for editing, its XML is parsed back into chips (not shown as raw XML),
   preserving the styled-chip display in the edit flow.
6. **Mirror the slash-command architecture** (rejected alternatives: the official
   `@tiptap/extension-mention` Suggestion plugin — adds a dependency and a second
   trigger paradigm; and literal-XML-text insertion — contradicts the chip
   decision). A dedicated `mentions/` module parallels `slash-commands/`.

## Architecture / data flow

```
useChatAgents()  ─┐
                  ├─► mentionSources { agents[], projects[] }  ─► AIChatInput
useProjectList()  ┘    (projects extended to keep path/icon/color)
       │
       ▼  user types "@", filters, picks an item
TipTap `mention` atom node  ──extractPlainText──►  "<agent id=\"12\">Reviewer</agent>"
       │                                                       │
       ▼  onSubmit(text)                                       ▼  rides inside SendRequest.text
SendChatMessage({ text })  (NO backend change)          transcript: <MarkdownText> + mention
                                                        decorator ─► clickable chip (navigate)
```

## Components

### A. Insert side — new `frontend/src/components/agentre/chat-input/mentions/` module

Mirrors `slash-commands/` one-for-one so the codebase stays cohesive:

- **`trigger.ts`** — `detectAtTrigger(textBeforeCursor)`: fires on `@` at
  line-start or after whitespace; the query ends at the first whitespace; a `@`
  embedded in a word (`foo@bar`) does not trigger. The line-scan logic is
  identical to `detectSlashTrigger`; extract the shared scan into one helper
  parameterized by trigger char, and have both slash and `@` call it (removes
  duplication rather than copy-pasting).
- **`mention-node.ts`** — a TipTap **inline atom** node `mention`
  (`group: "inline"`, `inline: true`, `atom: true`, `selectable: true`,
  `draggable: false`) with attrs `{ kind: "agent" | "project", refId: number,
  label: string, path?: string }`. A React nodeview renders the chip: type icon
  + name, colored by the entity's avatar color; the whole chip is one deletable
  unit.
- **`use-mention-menu.ts`** — mirrors `useSlashMenu`. Watches editor
  `update` / `selectionUpdate`, computes anchor rect via
  `editor.view.coordsAtPos`, keeps `{ open, anchorRect, items, selectedIndex,
  query }`, filters the combined agent+project list by query, exposes
  `onKeyDown` (↑ ↓ / Enter / Tab / Esc) and a unified `pick`. On pick it
  `deleteRange`s the `@query` span and `insertContent`s a `mention` node
  (not text).
- **`mention-popover.tsx`** — mirrors `SlashPopover` (fixed-position listbox
  anchored to cursor coords), but renders **grouped** sections (Agents /
  Projects) each with type icon + avatar color.

### B. `chat-input/index.tsx` wiring

- Register the `mention` node in the `useEditor` extensions list.
- Run `useMentionMenu` and render `<MentionPopover>`, gated by a new optional
  prop `mentionSources?: { agents: MentionAgent[]; projects: MentionProject[] }`.
  When absent, the `@` menu is disabled (exactly how the slash menu is gated by
  `backendType && onSlashSelect`).
- Keyboard interception: the mention menu's `onKeyDown` is checked alongside the
  existing slash `onKeyDown` in `handleKeyDown`, so Up/Down/Enter/Tab/Esc are
  consumed when either popover is open. Only one popover can be open at a time
  (`@` and `/` triggers are mutually exclusive by construction).

### C. `chat-input/content.ts` — serialization

- Extend `extractPlainText` with a `mention` case that emits the XML string
  (currently it only handles `text` / `hardBreak` / `paragraph`).
- Extend `buildEditorDocFromMessage` (draft round-trip): parse `<agent …>` /
  `<project …>` tags in the incoming text into `mention` nodes interleaved with
  text nodes, so an edited message shows chips, not raw XML. Parsing is a small
  tokenizer over the two known tag shapes (not a general XML/HTML parser).

### D. XML schema (rides inside `SendRequest.text`, no backend change)

- Agent:   `<agent id="12">Reviewer</agent>`
- Project: `<project id="3" path="/Users/me/proj">proj</project>`

The element text is the human label (readable to the agent). `id` is the stable
reference. `path` (projects only) gives the agent enough to read the project's
files. Attribute values are XML-escaped when serialized and unescaped when
parsed back.

### E. Data sources

- **Agents:** `useChatAgents()` → `AgentSlim` (has `id, name, avatarColor,
  avatarIcon, backendType`). Map to `MentionAgent { refId, label, icon, color }`.
- **Projects:** `useProjectList()` currently flattens to `{ id, name }`; extend
  `ProjectFlat` (and `flatten`) to also keep `path`, `icon`, `color` from
  `app.ProjectItem` (which already exposes all of them — confirmed). Map to
  `MentionProject { refId, label, path, icon, color }`.
- The parent composer (e.g. `chat-panel.tsx` / `ChatComposer`) composes
  `mentionSources` from these two hooks and passes it to `AIChatInput`.

### F. Transcript render side

- Plug a `MarkdownInlineDecorator` (existing seam in `markdown-text.tsx`) into
  the user-message `<MarkdownText>` (transcript-row-view.tsx:477). Its
  `tokenize` splits the two known tag shapes out of the text into `token`
  segments; `render` produces a clickable chip that navigates to the
  agent/project.
- **Known risk (resolve in planning):** `react-markdown` without `rehype-raw`
  may drop or not preserve raw `<agent>` / `<project>` tags as hast text nodes,
  so the decorator (which operates on text nodes) might never see them. Mitigation
  options, decided during planning after a quick check: (a) pre-tokenize the raw
  text before markdown and render chips + `MarkdownText` fragments, or (b) use a
  tag form that survives (e.g. wrap the tokenizer earlier in the pipeline). This
  is the one uncertain seam; the insert side and XML schema are not affected by
  how it resolves.

## Testing (TDD — red first, per repo invariants)

- **`detectAtTrigger`** pure-function tests (mirror `trigger.test.ts`):
  line-start, after-whitespace, in-word non-trigger, whitespace ends query.
- **`extractPlainText`** mention-node → XML serialization (incl. XML-escaping of
  labels/paths).
- **`buildEditorDocFromMessage`** round-trip: XML text → `mention` nodes; and a
  serialize↔parse round-trip stays stable.
- **`useMentionMenu`** behavior (mirror `use-slash-menu` tests): open on `@`,
  filter by query, keyboard nav, pick inserts a node + clears the trigger span,
  Esc/blur closes.
- **Transcript decorator `tokenize`**: XML → segments (mirror
  `markdown-text.test.tsx`), including the code-fence skip already provided by
  the seam.
- **i18n:** menu section headers ("Agents" / "Projects"), empty-state, and any
  chip a11y label added to both `zh-CN/common.json` and `en/common.json`; static
  keys validated by `frontend/src/__tests__/i18n.test.ts`.

## Out of scope (YAGNI)

- No dispatch/routing — mentioning an agent never hands it the task.
- No backend file loading — path-only marker; the agent reads files itself.
- No remote device-specific project path resolution — uses the default
  `ProjectItem.path`. (Projects can have per-device paths via
  `project_location_entity`; resolving those for remote agents is a possible
  follow-up, explicitly deferred.)
- No new backend, Wails binding, or DB migration. The feature is entirely
  frontend; the XML rides inside the existing `SendRequest.text`.

## Concurrency / branch notes

- Work stays on `develop/wyz`; commit with **pathspec** (`git commit <files>`),
  never bare `git commit` — concurrent sessions share the index.
- Both `frontend/src/i18n/locales/{zh-CN,en}/common.json` currently have
  **uncommitted edits from another session**. When adding i18n keys, add only the
  new keys and commit those two files by pathspec, being careful not to revert the
  other session's in-flight edits.
- A concurrent session is **removing the orchestration subsystem**
  (`2026-07-14-remove-orchestration-design.md`). This feature only touches
  **agents** and **projects**, both of which survive that removal, so there is no
  functional overlap — but expect churn in shared files (`chat-panel.tsx`,
  locale JSON); rebase/merge carefully.
