# Chat Input `@` Mention References Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `@`-triggered menu to the chat composer that inserts a project or agent as a styled chip which serializes to an XML tag inside the sent message text, and renders those tags back as clickable chips in the transcript.

**Architecture:** Mirror the existing slash-command system with a new self-contained `chat-input/mentions/` module: a pure XML serialize/parse core, a pure `@` trigger detector, a plain-DOM TipTap `mention` atom node (no React node-view — keeps it testable and matches the repo's decoration-only editor style), a `useMentionMenu` hook + grouped `MentionPopover`, and a sentinel-based transcript decorator plugged into the existing `MarkdownText` `decorator` seam. Entirely frontend; the XML rides inside the existing `SendRequest.text`.

**Tech Stack:** React 19, TypeScript, TipTap/ProseMirror v3 (`@tiptap/core`, `@tiptap/react`), react-i18next, react-markdown (rehype decorator seam), Vitest + Testing Library, Tailwind v4.

## Global Constraints

- **TDD, red first.** Every task writes a failing test, runs it to confirm it fails for the right reason, then implements. Repo invariant.
- **i18n both locales.** Every new visible string uses `t(...)` and is added to BOTH `frontend/src/i18n/locales/zh-CN/common.json` and `frontend/src/i18n/locales/en/common.json`. `frontend/src/__tests__/i18n.test.ts` validates coverage. No hardcoded Chinese in JSX.
- **shadcn only** for form controls (`@/components/ui/*`); no native `<select>`. (This feature adds no shadcn controls — the popover is a bespoke listbox like `SlashPopover`.)
- **No backend / Wails / DB / migration changes.** The XML is carried inside the existing `SendRequest.text`; do not touch Go code.
- **Branch `develop/wyz`, pathspec commits.** Concurrent sessions share the index — always `git commit <explicit files>`, never bare `git commit`. Both `common.json` locale files have uncommitted edits from another session; when adding i18n keys, add ONLY the new keys and commit those two files by pathspec without reverting others' edits.
- **Frontend test command:** `cd frontend && pnpm test -- <path>` for a focused file. Do not run backend gates.
- **XML schema (verbatim):** agent → `<agent id="12">Reviewer</agent>`; project → `<project id="3" path="/Users/me/proj">proj</project>`. Element text is the human label; attribute values are XML-escaped.

---

### Task 1: Shared mention XML core (serialize + parse)

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/xml.ts`
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/xml.test.ts`

**Interfaces:**
- Produces:
  - `type MentionKind = "agent" | "project"`
  - `type MentionRef = { kind: MentionKind; refId: number; label: string; path?: string }`
  - `type MentionSegment = { type: "text"; value: string } | { type: "mention"; ref: MentionRef }`
  - `serializeMentionXml(ref: MentionRef): string`
  - `parseMentionXml(text: string): MentionSegment[]`

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/mentions/__tests__/xml.test.ts
import { describe, expect, it } from "vitest";

import { parseMentionXml, serializeMentionXml } from "../xml";

describe("serializeMentionXml", () => {
  it("agent → id + label element text", () => {
    expect(
      serializeMentionXml({ kind: "agent", refId: 12, label: "Reviewer" }),
    ).toBe('<agent id="12">Reviewer</agent>');
  });

  it("project → id + path + label", () => {
    expect(
      serializeMentionXml({
        kind: "project",
        refId: 3,
        label: "proj",
        path: "/Users/me/proj",
      }),
    ).toBe('<project id="3" path="/Users/me/proj">proj</project>');
  });

  it("escapes XML-special chars in label and path", () => {
    expect(
      serializeMentionXml({
        kind: "project",
        refId: 4,
        label: "a & b <x>",
        path: '/p/"q"',
      }),
    ).toBe(
      '<project id="4" path="/p/&quot;q&quot;">a &amp; b &lt;x&gt;</project>',
    );
  });
});

describe("parseMentionXml", () => {
  it("splits text around an agent tag", () => {
    expect(parseMentionXml('hi <agent id="12">Reviewer</agent> there')).toEqual(
      [
        { type: "text", value: "hi " },
        {
          type: "mention",
          ref: { kind: "agent", refId: 12, label: "Reviewer" },
        },
        { type: "text", value: " there" },
      ],
    );
  });

  it("parses a project tag with path and unescapes entities", () => {
    expect(
      parseMentionXml('<project id="3" path="/p/&quot;q&quot;">a &amp; b</project>'),
    ).toEqual([
      {
        type: "mention",
        ref: { kind: "project", refId: 3, label: "a & b", path: '/p/"q"' },
      },
    ]);
  });

  it("plain text with no tags → single text segment", () => {
    expect(parseMentionXml("just text")).toEqual([
      { type: "text", value: "just text" },
    ]);
  });

  it("serialize → parse round-trips", () => {
    const ref = {
      kind: "project" as const,
      refId: 9,
      label: "My <Proj>",
      path: "/a/b c",
    };
    expect(parseMentionXml(serializeMentionXml(ref))).toEqual([
      { type: "mention", ref },
    ]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/xml.test.ts`
Expected: FAIL — cannot resolve `../xml`.

- [ ] **Step 3: Write minimal implementation**

```ts
// frontend/src/components/agentre/chat-input/mentions/xml.ts
// Mention 的 XML 序列化/解析核心 —— 纯函数,单测覆盖。
// 序列化产出进入 SendRequest.text;解析用于草稿回填与 transcript chip 渲染。

export type MentionKind = "agent" | "project";

export type MentionRef = {
  kind: MentionKind;
  refId: number;
  label: string;
  // 仅 project 有;agent 省略。
  path?: string;
};

export type MentionSegment =
  | { type: "text"; value: string }
  | { type: "mention"; ref: MentionRef };

function xmlEscape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function xmlUnescape(s: string): string {
  return s
    .replace(/&quot;/g, '"')
    .replace(/&gt;/g, ">")
    .replace(/&lt;/g, "<")
    .replace(/&amp;/g, "&");
}

export function serializeMentionXml(ref: MentionRef): string {
  const label = xmlEscape(ref.label);
  if (ref.kind === "project") {
    const path = xmlEscape(ref.path ?? "");
    return `<project id="${ref.refId}" path="${path}">${label}</project>`;
  }
  return `<agent id="${ref.refId}">${label}</agent>`;
}

// 同时匹配 <agent …>…</agent> 与 <project …>…</project>。属性顺序固定
// (id,先; project 再 path),但解析用独立属性正则,不依赖顺序。
const TAG_RE = /<(agent|project)\b([^>]*)>([\s\S]*?)<\/\1>/g;
const ID_RE = /\bid="(\d+)"/;
const PATH_RE = /\bpath="([^"]*)"/;

export function parseMentionXml(text: string): MentionSegment[] {
  const out: MentionSegment[] = [];
  let last = 0;
  TAG_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = TAG_RE.exec(text)) !== null) {
    if (m.index > last) {
      out.push({ type: "text", value: text.slice(last, m.index) });
    }
    const kind = m[1] as MentionKind;
    const attrs = m[2];
    const id = Number(ID_RE.exec(attrs)?.[1] ?? "0");
    const label = xmlUnescape(m[3]);
    const ref: MentionRef = { kind, refId: id, label };
    if (kind === "project") {
      ref.path = xmlUnescape(PATH_RE.exec(attrs)?.[1] ?? "");
    }
    out.push({ type: "mention", ref });
    last = m.index + m[0].length;
  }
  if (last < text.length) {
    out.push({ type: "text", value: text.slice(last) });
  }
  if (out.length === 0) out.push({ type: "text", value: text });
  return out;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/xml.test.ts`
Expected: PASS (all 7 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/xml.ts frontend/src/components/agentre/chat-input/mentions/__tests__/xml.test.ts
git commit frontend/src/components/agentre/chat-input/mentions/xml.ts frontend/src/components/agentre/chat-input/mentions/__tests__/xml.test.ts -m "✨ chat mentions: XML serialize/parse core"
```

---

### Task 2: `@` trigger detector

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/trigger.ts`
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts`

**Interfaces:**
- Produces: `detectAtTrigger(textBeforeCursor: string): { startOffset: number; query: string } | null`

> Note: this deliberately duplicates the ~15-line scan from `slash-commands/trigger.ts` rather than refactoring a shared helper, to avoid an out-of-scope change to the slash module (repo invariant: no drive-by refactor). The two are independent.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts
import { describe, expect, it } from "vitest";

import { detectAtTrigger } from "../trigger";

describe("detectAtTrigger", () => {
  it("line-start @ triggers, empty query", () => {
    expect(detectAtTrigger("@")).toEqual({ startOffset: 0, query: "" });
  });
  it("line-start @rev triggers", () => {
    expect(detectAtTrigger("@rev")).toEqual({ startOffset: 0, query: "rev" });
  });
  it("after whitespace triggers with correct offset", () => {
    expect(detectAtTrigger("hi @rev")).toEqual({ startOffset: 3, query: "rev" });
  });
  it("email-like foo@bar does not trigger", () => {
    expect(detectAtTrigger("foo@bar")).toBeNull();
  });
  it("query with space ends the trigger", () => {
    expect(detectAtTrigger("@rev iewer")).toBeNull();
  });
  it("no @ → null", () => {
    expect(detectAtTrigger("hello")).toBeNull();
  });
  it("nearest @ to cursor wins", () => {
    expect(detectAtTrigger("@a @co")).toEqual({ startOffset: 3, query: "co" });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts`
Expected: FAIL — cannot resolve `../trigger`.

- [ ] **Step 3: Write minimal implementation**

```ts
// frontend/src/components/agentre/chat-input/mentions/trigger.ts
// @ mention 触发检测 —— 纯函数。触发条件:输入 "@" 且其左侧紧邻字符是行首或
// 空白 (foo@bar 邮箱形式不触发);query 是 @ 与光标之间的文本,含空白视为已结束。

export type AtTriggerHit = { startOffset: number; query: string };

export function detectAtTrigger(textBeforeCursor: string): AtTriggerHit | null {
  for (let i = textBeforeCursor.length - 1; i >= 0; i--) {
    const ch = textBeforeCursor[i];
    if (ch === "@") {
      if (i === 0 || /\s/.test(textBeforeCursor[i - 1] ?? "")) {
        const query = textBeforeCursor.slice(i + 1);
        if (/\s/.test(query)) return null;
        return { startOffset: i, query };
      }
      return null;
    }
    if (/\s/.test(ch)) return null;
  }
  return null;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/trigger.ts frontend/src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts
git commit frontend/src/components/agentre/chat-input/mentions/trigger.ts frontend/src/components/agentre/chat-input/mentions/__tests__/trigger.test.ts -m "✨ chat mentions: @ trigger detector"
```

---

### Task 3: TipTap `mention` atom node

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/mention-node.ts`
- Modify: `frontend/src/styles/globals.css` (append one `.agentre-mention` rule — see step 3b)
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts`

**Interfaces:**
- Consumes: `MentionKind` (Task 1), `tokenToCssColor` from `../../session-avatar`.
- Produces:
  - `MENTION_NODE_NAME = "mention"`
  - `Mention` (a TipTap `Node`) with attrs `{ kind: MentionKind; refId: number; label: string; path: string; color: string }`. `color` is a display-only agent/project color token (e.g. `"agent-3"`) — NOT serialized to XML.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts
import Document from "@tiptap/extension-document";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { Editor } from "@tiptap/core";
import { describe, expect, it } from "vitest";

import { Mention } from "../mention-node";

function makeEditor() {
  return new Editor({ extensions: [Document, Paragraph, Text, Mention] });
}

describe("Mention node", () => {
  it("renderHTML emits data-* attributes and @label text", () => {
    const editor = makeEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            {
              type: "mention",
              attrs: {
                kind: "agent",
                refId: 12,
                label: "Reviewer",
                path: "",
                color: "agent-3",
              },
            },
          ],
        },
      ],
    });
    const html = editor.getHTML();
    expect(html).toContain('data-mention-kind="agent"');
    expect(html).toContain('data-ref-id="12"');
    expect(html).toContain('data-label="Reviewer"');
    expect(html).toContain("@Reviewer");
    editor.destroy();
  });

  it("parseHTML round-trips the attributes", () => {
    const editor = makeEditor();
    editor.commands.setContent(
      '<p><span data-mention-kind="project" data-ref-id="3" data-label="Web" data-path="/w">@Web</span></p>',
    );
    const json = editor.getJSON();
    const node = json.content?.[0]?.content?.[0];
    expect(node?.type).toBe("mention");
    expect(node?.attrs).toMatchObject({
      kind: "project",
      refId: 3,
      label: "Web",
      path: "/w",
    });
    editor.destroy();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts`
Expected: FAIL — cannot resolve `../mention-node`.

- [ ] **Step 3a: Write the node implementation**

```ts
// frontend/src/components/agentre/chat-input/mentions/mention-node.ts
// mention 是 inline atom 节点:承载 @ 引用的结构化数据(kind/refId/label/path),
// 以纯 DOM (renderHTML/parseHTML) 渲染成一个 pill,不使用 React node-view ——
// 保持可测 + 与仓库现有「只用 decoration 插件」的编辑器风格一致。
// color 仅用于显示着色 (从选中项带入),不参与 XML 序列化。
import { Node, mergeAttributes } from "@tiptap/core";

import { tokenToCssColor } from "../../session-avatar";
import type { MentionKind } from "./xml";

export const MENTION_NODE_NAME = "mention";

export const Mention = Node.create({
  name: MENTION_NODE_NAME,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      kind: {
        default: "agent" as MentionKind,
        parseHTML: (el) =>
          (el.getAttribute("data-mention-kind") as MentionKind) || "agent",
        renderHTML: (attrs) => ({ "data-mention-kind": attrs.kind }),
      },
      refId: {
        default: 0,
        parseHTML: (el) => Number(el.getAttribute("data-ref-id") ?? "0"),
        renderHTML: (attrs) => ({ "data-ref-id": String(attrs.refId) }),
      },
      label: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-label") ?? "",
        renderHTML: (attrs) => ({ "data-label": attrs.label }),
      },
      path: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-path") ?? "",
        renderHTML: (attrs) => ({ "data-path": attrs.path }),
      },
      color: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-color") ?? "",
        renderHTML: (attrs) => ({ "data-color": attrs.color }),
      },
    };
  },

  parseHTML() {
    return [{ tag: "span[data-mention-kind]" }];
  },

  renderHTML({ node, HTMLAttributes }) {
    const css = tokenToCssColor(node.attrs.color as string);
    return [
      "span",
      mergeAttributes(HTMLAttributes, {
        class: "agentre-mention",
        ...(css ? { style: `--mention-color:${css}` } : {}),
      }),
      `@${node.attrs.label as string}`,
    ];
  },
});
```

- [ ] **Step 3b: Append the chip stylesheet rule**

Append to `frontend/src/styles/globals.css` (the app's Tailwind entry CSS):

```css
/* @ mention chip in the composer (rendered by the TipTap mention node). */
.agentre-mention {
  display: inline-flex;
  align-items: center;
  border-radius: 0.375rem;
  padding: 0 0.25rem;
  font-weight: 500;
  white-space: nowrap;
  color: var(--mention-color, var(--foreground));
  background-color: color-mix(in oklab, var(--mention-color, var(--muted)) 15%, transparent);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/mention-node.ts frontend/src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts frontend/src/styles/globals.css
git commit frontend/src/components/agentre/chat-input/mentions/mention-node.ts frontend/src/components/agentre/chat-input/mentions/__tests__/mention-node.test.ts frontend/src/styles/globals.css -m "✨ chat mentions: TipTap mention atom node + chip style"
```

---

### Task 4: Serialize mention nodes in `extractPlainText`

**Files:**
- Modify: `frontend/src/components/agentre/chat-input/content.ts` (extend `extractPlainText`, ~line 11-24)
- Modify: `frontend/src/components/agentre/chat-input/types.ts` (add `TipTapMentionNode`, widen paragraph content)
- Test: `frontend/src/components/agentre/chat-input/__tests__/extract-mentions.test.ts`

**Interfaces:**
- Consumes: `serializeMentionXml`, `MentionKind` (Task 1).
- Produces: `extractPlainText` now emits `serializeMentionXml(...)` for `mention` nodes. New type `TipTapMentionNode = { type: "mention"; attrs: { kind: MentionKind; refId: number; label: string; path: string } }`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/__tests__/extract-mentions.test.ts
import { describe, expect, it } from "vitest";

import { extractPlainText } from "../content";
import type { ProseMirrorLikeNode } from "../types";

// 构造一个最小 ProseMirrorLikeNode:doc → paragraph → [text, mention, text]。
function docWith(children: ProseMirrorLikeNode[]): ProseMirrorLikeNode {
  const para: ProseMirrorLikeNode = {
    type: { name: "paragraph" },
    attrs: {},
    descendants(fn) {
      fn(para);
      for (const c of children) fn(c);
    },
  };
  return {
    type: { name: "doc" },
    attrs: {},
    descendants(fn) {
      para.descendants(fn);
    },
  };
}

function text(v: string): ProseMirrorLikeNode {
  return { type: { name: "text" }, text: v, attrs: {}, descendants() {} };
}

function mention(attrs: Record<string, unknown>): ProseMirrorLikeNode {
  return { type: { name: "mention" }, attrs, descendants() {} };
}

describe("extractPlainText with mention nodes", () => {
  it("serializes an agent mention to XML inline", () => {
    const doc = docWith([
      text("ping "),
      mention({ kind: "agent", refId: 12, label: "Reviewer", path: "" }),
      text(" now"),
    ]);
    expect(extractPlainText(doc)).toBe(
      'ping <agent id="12">Reviewer</agent> now',
    );
  });

  it("serializes a project mention with path", () => {
    const doc = docWith([
      mention({ kind: "project", refId: 3, label: "Web", path: "/w" }),
    ]);
    expect(extractPlainText(doc)).toBe(
      '<project id="3" path="/w">Web</project>',
    );
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/__tests__/extract-mentions.test.ts`
Expected: FAIL — `extractPlainText` ignores the `mention` node, so output lacks the XML.

- [ ] **Step 3a: Add the types**

In `frontend/src/components/agentre/chat-input/types.ts`, add after `TipTapTextNode` (line 26) and widen the paragraph content type:

```ts
import type { MentionKind } from "./mentions/xml";

export interface TipTapMentionNode {
  type: "mention";
  attrs: {
    kind: MentionKind;
    refId: number;
    label: string;
    path: string;
    color?: string;
  };
}
```

Change `TipTapParagraphNode` (line 28-31) `content` to allow mentions:

```ts
export interface TipTapParagraphNode {
  type: "paragraph";
  content?: (TipTapTextNode | TipTapMentionNode)[];
}
```

- [ ] **Step 3b: Extend `extractPlainText`**

In `frontend/src/components/agentre/chat-input/content.ts`, add the import and a `mention` branch inside the `descendants` callback (after the `hardBreak` branch, ~line 16):

```ts
import { serializeMentionXml, type MentionKind } from "./mentions/xml";
// ...
    } else if (node.type.name === "hardBreak") {
      out += "\n";
    } else if (node.type.name === "mention") {
      out += serializeMentionXml({
        kind: (node.attrs.kind as MentionKind) ?? "agent",
        refId: Number(node.attrs.refId ?? 0),
        label: String(node.attrs.label ?? ""),
        path: node.attrs.path ? String(node.attrs.path) : undefined,
      });
    } else if (node.type.name === "paragraph" && out.length > 0) {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/__tests__/extract-mentions.test.ts`
Expected: PASS (2 tests). Also run the existing `content`/`insert-text` tests to confirm no regression: `cd frontend && pnpm test -- src/components/agentre/chat-input/__tests__/insert-text.test.tsx`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/content.ts frontend/src/components/agentre/chat-input/types.ts frontend/src/components/agentre/chat-input/__tests__/extract-mentions.test.ts
git commit frontend/src/components/agentre/chat-input/content.ts frontend/src/components/agentre/chat-input/types.ts frontend/src/components/agentre/chat-input/__tests__/extract-mentions.test.ts -m "✨ chat mentions: serialize mention nodes to XML on send"
```

---

### Task 5: Parse XML back into mention nodes for drafts

**Files:**
- Modify: `frontend/src/components/agentre/chat-input/content.ts` (`buildEditorDocFromMessage`, ~line 35-54)
- Test: `frontend/src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts`

**Interfaces:**
- Consumes: `parseMentionXml` (Task 1), `TipTapMentionNode` / `TipTapTextNode` (Task 4).
- Produces: `buildEditorDocFromMessage` now interleaves `mention` nodes with text nodes within each line, so an edited message containing mention XML loads as chips, not raw XML.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts
import { describe, expect, it } from "vitest";

import { buildEditorDocFromMessage } from "../content";

describe("buildEditorDocFromMessage with mention XML", () => {
  it("parses an inline agent tag into a mention node between text nodes", () => {
    const doc = buildEditorDocFromMessage(
      'ping <agent id="12">Reviewer</agent> now',
    );
    expect(doc.content[0].content).toEqual([
      { type: "text", text: "ping " },
      {
        type: "mention",
        attrs: { kind: "agent", refId: 12, label: "Reviewer", path: "" },
      },
      { type: "text", text: " now" },
    ]);
  });

  it("keeps plain lines as plain text (no mention nodes)", () => {
    const doc = buildEditorDocFromMessage("just text");
    expect(doc.content[0].content).toEqual([{ type: "text", text: "just text" }]);
  });

  it("handles multiple lines, mention on the second", () => {
    const doc = buildEditorDocFromMessage(
      'a\n<project id="3" path="/w">Web</project>',
    );
    expect(doc.content).toHaveLength(2);
    expect(doc.content[1].content).toEqual([
      {
        type: "mention",
        attrs: { kind: "project", refId: 3, label: "Web", path: "/w" },
      },
    ]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts`
Expected: FAIL — current builder emits one text node per line (raw XML text), not mention nodes.

- [ ] **Step 3: Rewrite the per-segment builder**

In `frontend/src/components/agentre/chat-input/content.ts`, replace the body of the `for (const seg of segments)` loop (lines 41-48) so each line is tokenized through `parseMentionXml`:

```ts
import { parseMentionXml } from "./mentions/xml";
import type { TipTapMentionNode, TipTapTextNode } from "./types";
// ...
  for (const seg of segments) {
    const nodes: (TipTapTextNode | TipTapMentionNode)[] = [];
    for (const part of parseMentionXml(seg)) {
      if (part.type === "text") {
        if (part.value.length > 0) nodes.push({ type: "text", text: part.value });
      } else {
        nodes.push({
          type: "mention",
          attrs: {
            kind: part.ref.kind,
            refId: part.ref.refId,
            label: part.ref.label,
            path: part.ref.path ?? "",
          },
        });
      }
    }
    paragraphs.push(
      nodes.length > 0
        ? { type: "paragraph", content: nodes }
        : { type: "paragraph" },
    );
  }
```

Note: `parseMentionXml` returns a single `{type:"text"}` segment for a plain line, so plain lines still yield one text node. Empty lines yield a bare paragraph (unchanged behavior).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts`
Expected: PASS (3 tests). Re-run `insert-text.test.tsx` and `undo-redo.test.tsx` to confirm no regression in the plain-text path.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/content.ts frontend/src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts
git commit frontend/src/components/agentre/chat-input/content.ts frontend/src/components/agentre/chat-input/__tests__/build-doc-mentions.test.ts -m "✨ chat mentions: parse mention XML into chips on draft load"
```

---

### Task 6: `MentionPopover` (grouped listbox) + i18n keys

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/types.ts`
- Create: `frontend/src/components/agentre/chat-input/mentions/mention-popover.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`, `frontend/src/i18n/locales/en/common.json` (add `mentions.*` keys — pathspec-commit only these two files, do not revert other sessions' edits)
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx`

**Interfaces:**
- Consumes: `MentionKind` (Task 1), `tokenToCssColor` from `../../session-avatar`.
- Produces:
  - `type MentionItem = { kind: MentionKind; refId: number; label: string; path?: string; color?: string }`
  - `type MentionSources = { agents: MentionItem[]; projects: MentionItem[] }`
  - `type MentionMenuState = { open: boolean; anchorRect: { left: number; top: number; bottom: number } | null; items: MentionItem[]; selectedIndex: number; query: string }`
  - `MentionPopover({ state, onPick, onHover })` — grouped listbox with `role="listbox"` aria-label `t("mentions.aria")`, section headers `t("mentions.group.agents")` / `t("mentions.group.projects")`.

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MentionPopover } from "../mention-popover";
import type { MentionItem, MentionMenuState } from "../types";

const items: MentionItem[] = [
  { kind: "agent", refId: 12, label: "Reviewer", color: "agent-3" },
  { kind: "project", refId: 3, label: "Web", path: "/w", color: "agent-5" },
];

const state: MentionMenuState = {
  open: true,
  anchorRect: { left: 10, top: 40, bottom: 60 },
  items,
  selectedIndex: 0,
  query: "",
};

describe("MentionPopover", () => {
  it("renders grouped agent + project items", () => {
    render(<MentionPopover state={state} onPick={vi.fn()} onHover={vi.fn()} />);
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Web")).toBeInTheDocument();
    // section headers present
    expect(screen.getByText("Agents")).toBeInTheDocument();
    expect(screen.getByText("Projects")).toBeInTheDocument();
  });

  it("calls onPick on mousedown", () => {
    const onPick = vi.fn();
    render(<MentionPopover state={state} onPick={onPick} onHover={vi.fn()} />);
    screen.getByText("Reviewer").closest("button")!.dispatchEvent(
      new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
    );
    expect(onPick).toHaveBeenCalledWith(items[0]);
  });

  it("renders nothing when closed", () => {
    const { container } = render(
      <MentionPopover
        state={{ ...state, open: false }}
        onPick={vi.fn()}
        onHover={vi.fn()}
      />,
    );
    expect(container.firstChild).toBeNull();
  });
});
```

> If existing popover tests need an i18n provider, follow the pattern in `slash-commands/__tests__/` — the repo's test setup initializes `i18n` globally (English), so `t("mentions.group.agents")` resolves to "Agents". Confirm by grepping an existing `*popover*`/`integration` test for how it asserts translated text (the slash integration test asserts the English aria-label "Slash commands" directly).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx`
Expected: FAIL — cannot resolve `../mention-popover` / `../types`.

- [ ] **Step 3a: Add the i18n keys**

Add to `frontend/src/i18n/locales/en/common.json` (place under a new top-level `"mentions"` object; if the file is alphabetized near other keys, insert to match surrounding style):

```json
"mentions": {
  "aria": "Mention a project or agent",
  "group": { "agents": "Agents", "projects": "Projects" },
  "empty": "No matches",
  "chip": {
    "agentTitle": "Agent: {{name}}",
    "projectTitle": "Project: {{name}}"
  }
}
```

Add the mirror to `frontend/src/i18n/locales/zh-CN/common.json`:

```json
"mentions": {
  "aria": "提及项目或 agent",
  "group": { "agents": "Agent", "projects": "项目" },
  "empty": "无匹配项",
  "chip": {
    "agentTitle": "Agent：{{name}}",
    "projectTitle": "项目：{{name}}"
  }
}
```

- [ ] **Step 3b: Add the types**

```ts
// frontend/src/components/agentre/chat-input/mentions/types.ts
import type { MentionKind } from "./xml";

export type MentionItem = {
  kind: MentionKind;
  refId: number;
  label: string;
  path?: string; // project only
  color?: string; // agent/project color token, e.g. "agent-3"
};

export type MentionSources = {
  agents: MentionItem[];
  projects: MentionItem[];
};

export type MentionMenuState = {
  open: boolean;
  anchorRect: { left: number; top: number; bottom: number } | null;
  items: MentionItem[];
  selectedIndex: number;
  query: string;
};
```

- [ ] **Step 3c: Write the popover component**

```tsx
// frontend/src/components/agentre/chat-input/mentions/mention-popover.tsx
import * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

import { tokenToCssColor } from "../../session-avatar";
import type { MentionItem, MentionMenuState } from "./types";

// MentionPopover 视觉层:光标上方 fixed 弹层,agent / project 分组渲染。
// 键盘选中在 useMentionMenu 里处理;本组件只渲染 + 鼠标 hover/点击。
// items 已按 agents-first / projects-last 排好序,selectedIndex 是扁平下标。
export function MentionPopover({
  state,
  onPick,
  onHover,
}: {
  state: MentionMenuState;
  onPick: (item: MentionItem) => void;
  onHover: (idx: number) => void;
}): React.ReactElement | null {
  const { t } = useTranslation();

  if (!state.open || !state.anchorRect || state.items.length === 0) return null;

  const style: React.CSSProperties = {
    position: "fixed",
    left: state.anchorRect.left,
    bottom: window.innerHeight - state.anchorRect.top + 4,
    zIndex: 50,
  };

  return (
    <div
      role="listbox"
      aria-label={t("mentions.aria")}
      style={style}
      className="min-w-[14rem] max-w-[20rem] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {state.items.map((item, idx) => {
        const active = idx === state.selectedIndex;
        const prevKind = idx > 0 ? state.items[idx - 1].kind : null;
        const showHeader = item.kind !== prevKind;
        const css = tokenToCssColor(item.color) ?? "var(--muted-foreground)";
        return (
          <React.Fragment key={`${item.kind}-${item.refId}`}>
            {showHeader ? (
              <div className="px-2 pt-1.5 pb-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                {item.kind === "agent"
                  ? t("mentions.group.agents")
                  : t("mentions.group.projects")}
              </div>
            ) : null}
            <button
              type="button"
              role="option"
              aria-selected={active}
              onMouseEnter={() => onHover(idx)}
              onMouseDown={(e) => {
                e.preventDefault();
                onPick(item);
              }}
              className={cn(
                "flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
                active
                  ? "bg-accent text-accent-foreground"
                  : "text-foreground hover:bg-accent/60",
              )}
            >
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: css }}
              />
              <span className="truncate font-medium">{item.label}</span>
              {item.kind === "project" && item.path ? (
                <span className="ml-auto truncate text-muted-foreground">
                  {item.path}
                </span>
              ) : null}
            </button>
          </React.Fragment>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx`
Expected: PASS (3 tests). Also run the i18n coverage test: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/types.ts frontend/src/components/agentre/chat-input/mentions/mention-popover.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx
git commit frontend/src/components/agentre/chat-input/mentions/types.ts frontend/src/components/agentre/chat-input/mentions/mention-popover.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx frontend/src/i18n/locales/en/common.json frontend/src/i18n/locales/zh-CN/common.json -m "✨ chat mentions: grouped MentionPopover + i18n"
```

---

### Task 7: `useMentionMenu` hook + wire into `AIChatInput`

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/use-mention-menu.ts`
- Create: `frontend/src/components/agentre/chat-input/mentions/index.ts` (barrel re-exports)
- Modify: `frontend/src/components/agentre/chat-input/index.tsx` (register `Mention` node, run hook, render popover, add keydown, add `mentionSources` prop)
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx`

**Interfaces:**
- Consumes: `detectAtTrigger` (Task 2), `Mention`/`MENTION_NODE_NAME` (Task 3), `MentionItem`/`MentionSources`/`MentionMenuState` (Task 6), `MentionPopover` (Task 6).
- Produces:
  - `useMentionMenu({ editor, sources, onPick }): { state: MentionMenuState; onKeyDown: (e: KeyboardEvent) => boolean; pick: (item: MentionItem) => void; setSelectedIndex: (i: number) => void; close: () => void }`
  - `AIChatInput` gains prop `mentionSources?: MentionSources`. When present with ≥1 item, `@` opens the menu; picking inserts a `mention` node + trailing space.
  - Barrel `mentions/index.ts` exports: `useMentionMenu`, `MentionPopover`, `Mention`, `MENTION_NODE_NAME`, and all types from `./types` and `./xml`.

- [ ] **Step 1: Write the failing integration test**

```tsx
// frontend/src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx
import { act, render, screen, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it, vi } from "vitest";

import type { Editor } from "@tiptap/react";

import { AIChatInput, type AIChatInputHandle } from "../../index";
import type { MentionSources } from "../types";

const sources: MentionSources = {
  agents: [{ kind: "agent", refId: 12, label: "Reviewer", color: "agent-3" }],
  projects: [
    { kind: "project", refId: 3, label: "Web", path: "/w", color: "agent-5" },
  ],
};

function Harness({ onSubmit }: { onSubmit: (t: string) => void }) {
  const editorRef = useRef<Editor | null>(null);
  const handleRef = useRef<AIChatInputHandle>(null);
  return (
    <>
      <button
        type="button"
        data-testid="ins"
        onClick={() => editorRef.current?.commands.insertContent("@")}
      >
        @
      </button>
      <button
        type="button"
        data-testid="ins-rev"
        onClick={() => editorRef.current?.commands.insertContent("Rev")}
      >
        Rev
      </button>
      <button
        type="button"
        data-testid="submit"
        onClick={() => handleRef.current?.submit()}
      >
        submit
      </button>
      <AIChatInput
        ref={handleRef}
        onSubmit={onSubmit}
        editorRef={editorRef}
        mentionSources={sources}
        autoFocus
      />
    </>
  );
}

describe("AIChatInput @ mention integration", () => {
  it("@ opens grouped popover with agent + project", async () => {
    render(<Harness onSubmit={vi.fn()} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() =>
      expect(screen.getByRole("listbox")).toBeInTheDocument(),
    );
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Web")).toBeInTheDocument();
  });

  it("typing Rev filters to the agent only", async () => {
    render(<Harness onSubmit={vi.fn()} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() => screen.getByText("Reviewer"));
    act(() => screen.getByTestId("ins-rev").click());
    await waitFor(() => expect(screen.queryByText("Web")).toBeNull());
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
  });

  it("picking an agent inserts a chip that serializes to XML on submit", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() => screen.getByText("Reviewer"));
    act(() =>
      screen
        .getByText("Reviewer")
        .closest("button")!
        .dispatchEvent(
          new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
        ),
    );
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    act(() => screen.getByTestId("submit").click());
    expect(onSubmit).toHaveBeenCalledWith(
      expect.stringContaining('<agent id="12">Reviewer</agent>'),
    );
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx`
Expected: FAIL — `AIChatInput` has no `mentionSources` prop / `../index` barrel missing exports.

- [ ] **Step 3a: Write the hook** (modeled on `use-slash-menu.ts`)

```ts
// frontend/src/components/agentre/chat-input/mentions/use-mention-menu.ts
import { useCallback, useEffect, useMemo, useState } from "react";

import type { Editor } from "@tiptap/react";

import { MENTION_NODE_NAME } from "./mention-node";
import { detectAtTrigger } from "./trigger";
import type { MentionItem, MentionMenuState, MentionSources } from "./types";

function filterItems(all: MentionItem[], query: string): MentionItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return all;
  return all.filter((i) => i.label.toLowerCase().includes(q));
}

export function useMentionMenu({
  editor,
  sources,
  onPick,
}: {
  editor: Editor | null;
  sources: MentionSources;
  onPick?: (item: MentionItem) => void;
}): {
  state: MentionMenuState;
  onKeyDown: (event: KeyboardEvent) => boolean;
  pick: (item: MentionItem) => void;
  setSelectedIndex: (idx: number) => void;
  close: () => void;
} {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [anchorRect, setAnchorRect] = useState<
    MentionMenuState["anchorRect"] | null
  >(null);
  const [selectedIndex, setSelectedIndex] = useState(0);

  // agents 在前、projects 在后 —— MentionPopover 按此顺序分组。
  const available = useMemo(
    () => [...sources.agents, ...sources.projects],
    [sources.agents, sources.projects],
  );
  const items = useMemo(
    () => filterItems(available, query),
    [available, query],
  );

  useEffect(() => {
    if (selectedIndex >= items.length) {
      setSelectedIndex(items.length > 0 ? items.length - 1 : 0);
    }
  }, [items.length, selectedIndex]);

  const close = useCallback(() => {
    setOpen(false);
    setAnchorRect(null);
    setQuery("");
    setSelectedIndex(0);
  }, []);

  useEffect(() => {
    if (!editor) return;
    const recompute = () => {
      if (available.length === 0) {
        if (open) close();
        return;
      }
      const { $from, empty } = editor.state.selection;
      if (!empty) {
        if (open) close();
        return;
      }
      const before = $from.parent.textBetween(0, $from.parentOffset);
      const hit = detectAtTrigger(before);
      if (!hit) {
        if (open) close();
        return;
      }
      const triggerPos = $from.start() + hit.startOffset;
      let rect: MentionMenuState["anchorRect"];
      try {
        const c = editor.view.coordsAtPos(triggerPos);
        rect = { left: c.left, top: c.top, bottom: c.bottom };
      } catch {
        rect = null;
      }
      setQuery(hit.query);
      setAnchorRect(rect);
      setOpen(true);
    };
    editor.on("update", recompute);
    editor.on("selectionUpdate", recompute);
    return () => {
      editor.off("update", recompute);
      editor.off("selectionUpdate", recompute);
    };
  }, [editor, available, open, close]);

  const confirm = useCallback(
    (item: MentionItem) => {
      if (editor) {
        const { $from } = editor.state.selection;
        const before = $from.parent.textBetween(0, $from.parentOffset);
        const hit = detectAtTrigger(before);
        if (hit) {
          const from = $from.start() + hit.startOffset;
          const to = $from.pos;
          editor
            .chain()
            .focus()
            .deleteRange({ from, to })
            .insertContent({
              type: MENTION_NODE_NAME,
              attrs: {
                kind: item.kind,
                refId: item.refId,
                label: item.label,
                path: item.path ?? "",
                color: item.color ?? "",
              },
            })
            .insertContent(" ")
            .run();
        }
      }
      close();
      onPick?.(item);
    },
    [close, editor, onPick],
  );

  const onKeyDown = useCallback(
    (event: KeyboardEvent): boolean => {
      if (!open || items.length === 0) return false;
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSelectedIndex((i) => (i + 1) % items.length);
          return true;
        case "ArrowUp":
          event.preventDefault();
          setSelectedIndex((i) => (i - 1 + items.length) % items.length);
          return true;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const item = items[selectedIndex] ?? items[0];
          if (item) confirm(item);
          return true;
        }
        case "Escape":
          event.preventDefault();
          close();
          return true;
        default:
          return false;
      }
    },
    [open, items, selectedIndex, confirm, close],
  );

  const state: MentionMenuState = useMemo(
    () => ({ open, anchorRect, items, selectedIndex, query }),
    [open, anchorRect, items, selectedIndex, query],
  );

  return { state, onKeyDown, pick: confirm, setSelectedIndex, close };
}
```

- [ ] **Step 3b: Add the barrel**

```ts
// frontend/src/components/agentre/chat-input/mentions/index.ts
export { Mention, MENTION_NODE_NAME } from "./mention-node";
export { MentionPopover } from "./mention-popover";
export { useMentionMenu } from "./use-mention-menu";
export * from "./types";
export * from "./xml";
```

- [ ] **Step 3c: Wire into `AIChatInput`** (`frontend/src/components/agentre/chat-input/index.tsx`)

1. Add imports near the slash-commands import (line 21-28):

```tsx
import {
  Mention,
  MentionPopover,
  useMentionMenu,
  type MentionItem,
  type MentionSources,
} from "./mentions";
```

2. Add the prop to `AIChatInputProps` (after `onSlashSelect`, ~line 66):

```tsx
  /** 项目 / agent 提及数据源。提供且非空时启用 @ 菜单;省略则不启用。 */
  mentionSources?: MentionSources;
```

3. Destructure it in the component params (with `onSlashSelect`, ~line 84): add `mentionSources,`.

4. Register the node in the `extensions` array (after `SlashHighlight.configure(...)`, ~line 154):

```tsx
        Mention,
```

5. Add a keydown ref beside `slashKeyDownRef` (~line 96):

```tsx
    const mentionKeyDownRef = useRef<(e: KeyboardEvent) => boolean>(
      () => false,
    );
```

6. In `handleKeyDown`, extend the existing slash interception (line 176-177) to also consult the mention menu:

```tsx
          if (
            !commandModeRef.current &&
            (mentionKeyDownRef.current(event) || slashKeyDownRef.current(event))
          )
            return true;
```

7. After the slash menu setup (`const slashMenu = useSlashMenu(...)`, ~line 343-350), add the mention menu wiring:

```tsx
    const mentionEnabled = !!(
      mentionSources &&
      mentionSources.agents.length + mentionSources.projects.length > 0
    );
    const emptySources = useMemo<MentionSources>(
      () => ({ agents: [], projects: [] }),
      [],
    );
    const mentionMenu = useMentionMenu({
      editor: mentionEnabled ? (editor ?? null) : null,
      sources: mentionSources ?? emptySources,
      // 插入由 hook 内部完成(insert mention node);父组件无需处理。
      onPick: (_item: MentionItem) => {},
    });
    useEffect(() => {
      mentionKeyDownRef.current = mentionMenu.onKeyDown;
    }, [mentionMenu.onKeyDown]);
```

8. Render the popover next to `SlashPopover` (line 355-361):

```tsx
        {mentionEnabled ? (
          <MentionPopover
            state={mentionMenu.state}
            onPick={mentionMenu.pick}
            onHover={mentionMenu.setSelectedIndex}
          />
        ) : null}
```

9. Ensure `useMemo` is imported (it already is, line 7).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx`
Expected: PASS (3 tests). Also run the slash integration test to confirm the two menus coexist: `cd frontend && pnpm test -- src/components/agentre/slash-commands/__tests__/integration.test.tsx`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/use-mention-menu.ts frontend/src/components/agentre/chat-input/mentions/index.ts frontend/src/components/agentre/chat-input/index.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx
git commit frontend/src/components/agentre/chat-input/mentions/use-mention-menu.ts frontend/src/components/agentre/chat-input/mentions/index.ts frontend/src/components/agentre/chat-input/index.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx -m "✨ chat mentions: useMentionMenu hook + AIChatInput wiring"
```

---

### Task 8: Feed sources into `ChatComposer` (agents + projects)

**Files:**
- Modify: `frontend/src/hooks/use-project-list.ts` (extend `ProjectFlat` with `path`/`color`)
- Create: `frontend/src/components/agentre/chat-input/mentions/build-sources.ts`
- Modify: `frontend/src/components/agentre/chat.tsx` (`ChatComposer`: call hooks, pass `mentionSources`)
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts`

**Interfaces:**
- Consumes: `AgentSlim`/`ChatAgentItem` (from `stores/chat-agents-store`), extended `ProjectFlat`, `MentionItem`/`MentionSources` (Task 6).
- Produces: `buildMentionSources(agents, projects): MentionSources` — a pure mapper. `ChatComposer` now passes `mentionSources` to `AIChatInput`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts
import { describe, expect, it } from "vitest";

import { buildMentionSources } from "../build-sources";

describe("buildMentionSources", () => {
  it("maps agents and projects into mention items", () => {
    const out = buildMentionSources(
      [{ id: 12, name: "Reviewer", avatarColor: "agent-3" }],
      [{ id: 3, name: "Web", path: "/w", color: "agent-5" }],
    );
    expect(out).toEqual({
      agents: [{ kind: "agent", refId: 12, label: "Reviewer", color: "agent-3" }],
      projects: [
        {
          kind: "project",
          refId: 3,
          label: "Web",
          path: "/w",
          color: "agent-5",
        },
      ],
    });
  });

  it("tolerates missing color/path", () => {
    const out = buildMentionSources(
      [{ id: 1, name: "A" }],
      [{ id: 2, name: "B" }],
    );
    expect(out.agents[0]).toMatchObject({ kind: "agent", refId: 1, label: "A" });
    expect(out.projects[0]).toMatchObject({
      kind: "project",
      refId: 2,
      label: "B",
      path: "",
    });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts`
Expected: FAIL — cannot resolve `../build-sources`.

- [ ] **Step 3a: Extend `ProjectFlat`** (`frontend/src/hooks/use-project-list.ts`)

Change the type (line 8-11) and the `flatten` push (line 17-21):

```ts
export type ProjectFlat = {
  id: number;
  name: string;
  path: string;
  color: string;
};
```

```ts
      if (n.project) {
        out.push({
          id: n.project.id,
          name: n.project.name,
          path: n.project.path,
          color: n.project.color,
        });
      }
```

(These fields already exist on `app.ProjectItem`; the command-palette consumer only reads `id`/`name`, so this is additive and safe.)

- [ ] **Step 3b: Write the mapper**

```ts
// frontend/src/components/agentre/chat-input/mentions/build-sources.ts
import type { MentionSources } from "./types";

// 只依赖字段子集(结构化类型),避免耦合完整 AgentSlim / ProjectFlat 形状。
type AgentLike = { id: number; name: string; avatarColor?: string | null };
type ProjectLike = {
  id: number;
  name: string;
  path?: string | null;
  color?: string | null;
};

export function buildMentionSources(
  agents: AgentLike[],
  projects: ProjectLike[],
): MentionSources {
  return {
    agents: agents.map((a) => ({
      kind: "agent",
      refId: a.id,
      label: a.name,
      color: a.avatarColor ?? "",
    })),
    projects: projects.map((p) => ({
      kind: "project",
      refId: p.id,
      label: p.name,
      path: p.path ?? "",
      color: p.color ?? "",
    })),
  };
}
```

- [ ] **Step 3c: Wire `ChatComposer`** (`frontend/src/components/agentre/chat.tsx`)

Add imports at the top of the file (with the other hook/component imports):

```tsx
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useProjectList } from "@/hooks/use-project-list";
import { buildMentionSources } from "./chat-input/mentions/build-sources";
```

Inside `ChatComposer` (after `const { t } = useTranslation();`, ~line 456), derive the sources:

```tsx
  const { agents } = useChatAgents();
  const { projects } = useProjectList();
  const mentionSources = React.useMemo(
    () => buildMentionSources(agents, projects),
    [agents, projects],
  );
```

Pass it to `AIChatInput` (add to the props at ~line 729-741):

```tsx
            mentionSources={mentionSources}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts`
Expected: PASS (2 tests). Then run the existing composer tests to confirm no regression from the new hooks (they call Wails bindings — ensure the test files mock them per `reference_frontend_wails_runtime_test_mock`): `cd frontend && pnpm test -- src/components/agentre/__tests__/composer-command-mode.test.tsx`

> If `composer-command-mode.test.tsx` (or other `ChatComposer` renderers) now fail because `useChatAgents`/`useProjectList` call unmocked Wails bindings, add a per-file `vi.mock("@/hooks/use-chat-agents", ...)` / `vi.mock("@/hooks/use-project-list", ...)` returning empty arrays — do NOT add a global alias. This is the documented mock rule for components that transitively import the Wails runtime.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/use-project-list.ts frontend/src/components/agentre/chat-input/mentions/build-sources.ts frontend/src/components/agentre/chat.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts
git commit frontend/src/hooks/use-project-list.ts frontend/src/components/agentre/chat-input/mentions/build-sources.ts frontend/src/components/agentre/chat.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts -m "✨ chat mentions: feed agent+project sources into composer"
```

---

### Task 9: Render mention chips in the transcript

**Files:**
- Create: `frontend/src/components/agentre/chat-input/mentions/transcript.tsx` (`prepareMentionText`, `MentionChip`, `makeMentionDecorator`)
- Modify: `frontend/src/components/agentre/transcript-row-view.tsx` (line 477 — pre-process `item.text`, pass a decorator)
- Test: `frontend/src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx`

**Interfaces:**
- Consumes: `parseMentionXml`/`MentionRef` (Task 1), `MarkdownInlineDecorator` from `../../markdown-text`, `tokenToCssColor`, `useNavigate` (react-router-dom).
- Produces:
  - `prepareMentionText(raw: string): { text: string; refs: MentionRef[] }` — replaces each mention tag with a private-use sentinel `{index}` (survives markdown untouched), returning the parallel refs table.
  - `makeMentionDecorator(refs: MentionRef[]): MarkdownInlineDecorator<MentionRef>` — `tokenize` matches the sentinel and maps to `refs[index]`; `render` returns `<MentionChip refData={ref} />`.
  - `MentionChip({ refData })` — a colored, clickable chip; click navigates to `/org` (agent) or `/projects` (project); `title` = `t("mentions.chip.agentTitle"|"projectTitle", { name })`.

**Rationale:** react-markdown has no `rehype-raw`, so raw `<agent>` tags are stripped before the decorator's hast-text `tokenize` runs. The sentinel is plain text that survives to a hast text node, so the existing `MarkdownText` decorator seam can tokenize it. Only pass a decorator when `refs.length > 0` (zero overhead for normal messages).

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { MarkdownText } from "../../../markdown-text";
import { makeMentionDecorator, prepareMentionText } from "../transcript";

describe("prepareMentionText", () => {
  it("replaces tags with sentinels and collects refs", () => {
    const { text, refs } = prepareMentionText(
      'hi <agent id="12">Reviewer</agent> and <project id="3" path="/w">Web</project>',
    );
    expect(text).toBe("hi 0 and 1");
    expect(refs).toEqual([
      { kind: "agent", refId: 12, label: "Reviewer" },
      { kind: "project", refId: 3, label: "Web", path: "/w" },
    ]);
  });

  it("leaves plain text untouched with empty refs", () => {
    expect(prepareMentionText("just text")).toEqual({
      text: "just text",
      refs: [],
    });
  });
});

describe("MarkdownText + mention decorator", () => {
  it("renders a chip for the mention sentinel", () => {
    const { text, refs } = prepareMentionText('yo <agent id="1">Bob</agent>');
    render(
      <MemoryRouter>
        <MarkdownText text={text} decorator={makeMentionDecorator(refs)} />
      </MemoryRouter>,
    );
    expect(screen.getByText("@Bob")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx`
Expected: FAIL — cannot resolve `../transcript`.

- [ ] **Step 3a: Write the transcript module**

```tsx
// frontend/src/components/agentre/chat-input/mentions/transcript.tsx
import * as React from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import type { MarkdownInlineDecorator } from "../../markdown-text";
import { tokenToCssColor } from "../../session-avatar";
import { parseMentionXml, type MentionRef } from "./xml";

// 私有区哨兵:markdown 无 rehype-raw 会吃掉 <agent>/<project> 原始标签,所以
// 先把标签替换成 {idx} 这种「对 markdown 无意义」的纯文本哨兵,
// 让它安全穿过 markdown 解析,再由 decorator 在 hast 文本节点层面还原成 chip。
const S = "";
const E = "";

export function prepareMentionText(raw: string): {
  text: string;
  refs: MentionRef[];
} {
  const refs: MentionRef[] = [];
  let text = "";
  for (const seg of parseMentionXml(raw)) {
    if (seg.type === "text") {
      text += seg.value;
    } else {
      text += `${S}${refs.length}${E}`;
      refs.push(seg.ref);
    }
  }
  return { text, refs };
}

export function MentionChip({
  refData,
}: {
  refData: MentionRef;
}): React.ReactElement {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const title =
    refData.kind === "agent"
      ? t("mentions.chip.agentTitle", { name: refData.label })
      : t("mentions.chip.projectTitle", { name: refData.label });
  return (
    <button
      type="button"
      title={title}
      onClick={() => navigate(refData.kind === "agent" ? "/org" : "/projects")}
      className="agentre-mention cursor-pointer"
    >
      @{refData.label}
    </button>
  );
}

const SENTINEL_RE = /(\d+)/g;

export function makeMentionDecorator(
  refs: MentionRef[],
): MarkdownInlineDecorator<MentionRef> {
  return {
    tokenize(text) {
      const out: Array<
        | { type: "text"; value: string }
        | { type: "token"; data: MentionRef }
      > = [];
      let last = 0;
      SENTINEL_RE.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = SENTINEL_RE.exec(text)) !== null) {
        if (m.index > last) {
          out.push({ type: "text", value: text.slice(last, m.index) });
        }
        const ref = refs[Number(m[1])];
        if (ref) out.push({ type: "token", data: ref });
        last = m.index + m[0].length;
      }
      if (last < text.length) out.push({ type: "text", value: text.slice(last) });
      return out;
    },
    render(data) {
      return <MentionChip refData={data} />;
    },
  };
}
```

> Note: `tokenToCssColor` is imported for parity with the composer chip, but transcript refs carry no color (XML omits it), so `MentionChip` uses the neutral `.agentre-mention` style. Coloring transcript chips by looking up the live agent/project is a possible follow-up; leave it out (YAGNI). Remove the unused import if the linter flags it.

- [ ] **Step 3b: Wire the transcript row** (`frontend/src/components/agentre/transcript-row-view.tsx`)

Add imports near the `MarkdownText` import (line 30):

```tsx
import {
  makeMentionDecorator,
  prepareMentionText,
} from "./chat-input/mentions/transcript";
```

Replace the message-body render at line 477. Find the component that owns line 477 and, just above its `return`, compute the prepared text + decorator; then use them in the `<MarkdownText>` call:

```tsx
  const { text: mentionText, refs: mentionRefs } = React.useMemo(
    () => prepareMentionText(item.text),
    [item.text],
  );
  const mentionDecorator = React.useMemo(
    () => (mentionRefs.length > 0 ? makeMentionDecorator(mentionRefs) : undefined),
    [mentionRefs],
  );
```

```tsx
        <MarkdownText
          cwd={ctx?.cwd}
          text={mentionText}
          decorator={mentionDecorator}
        />
```

> If line 477 sits inside an inline arrow/`.map` rather than a component body with hook scope, hoist the two `useMemo`s to the nearest enclosing component that renders this row (the one holding `item`), or extract a tiny `MessageBody({ item, cwd })` component that does the `useMemo` + `MarkdownText`. Do not call hooks inside a `.map` callback.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx`
Expected: PASS (3 tests). Run the existing transcript/message-row tests to confirm no regression: `cd frontend && pnpm test -- src/components/agentre/message-row.test.tsx`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-input/mentions/transcript.tsx frontend/src/components/agentre/transcript-row-view.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx
git commit frontend/src/components/agentre/chat-input/mentions/transcript.tsx frontend/src/components/agentre/transcript-row-view.tsx frontend/src/components/agentre/chat-input/mentions/__tests__/transcript.test.tsx -m "✨ chat mentions: render mention XML as chips in the transcript"
```

---

### Task 10: Full-gate verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full frontend test suite**

Run: `cd frontend && pnpm test`
Expected: PASS. Watch specifically for regressions in `chat-input/`, `slash-commands/`, `markdown-text`, `transcript`, and `i18n.test.ts`.

- [ ] **Step 2: Type + lint gate**

Run: `cd frontend && pnpm exec tsc --noEmit && pnpm lint` (or `make lint` from repo root per AGENTS.md; note `make lint` runs `generate` first).
Expected: no type errors, no ESLint errors (especially `i18next/no-literal-string` — all visible copy must be `t(...)`).

- [ ] **Step 3: Manual smoke (real app)**

Run `make dev`, open a chat, type `@`, confirm the grouped menu lists agents + projects; pick one → chip appears; send → the sent user message shows a chip in the transcript; click the chip → navigates to `/org` or `/projects`. Edit the sent message → the composer reloads with a chip (not raw XML).

- [ ] **Step 4: Commit (only if a fix was needed)**

Commit any gate fixes by pathspec with a `🐛`/`✅` message.

---

## Self-Review

**Spec coverage:**
- Structured-context XML in `text` → Tasks 1, 4 (serialize), Task 7 (insert). ✓
- Project path attach (path-only) → Task 1 (`path` attr in XML), Task 8 (`ProjectFlat.path`). ✓
- Visual linking chip in transcript → Task 9. ✓
- One `@` menu, grouped → Tasks 6, 7. ✓
- Styled chip, serialize-on-send → Tasks 3, 4, 7. ✓
- Enabled everywhere `AIChatInput` used → Task 8 (wired in the shared `ChatComposer`). ✓
- Draft round-trip → Task 5. ✓
- Mirror slash architecture, no official mention ext → module layout matches `slash-commands/`. ✓
- i18n both locales → Task 6 (`mentions.*`). ✓
- No backend change → confirmed; all tasks are frontend-only. ✓
- Out-of-scope items (dispatch, backend file load, remote per-device path) → not implemented. ✓

**Placeholder scan:** No TBD/TODO/"add error handling"; every code step has full code. ✓

**Type consistency:** `MentionRef`/`MentionKind`/`MentionSegment` (Task 1) reused verbatim in Tasks 4, 5, 9. `MentionItem`/`MentionSources`/`MentionMenuState` (Task 6) reused in Tasks 7, 8. `MENTION_NODE_NAME` (Task 3) reused in Task 7. `mentionSources` prop name consistent Tasks 7↔8. `buildMentionSources` signature consistent Task 8. ✓

**Known residual risk:** Task 9's line-477 hook placement depends on the exact component structure at that site — the step includes a fallback (extract a `MessageBody` component) if hooks can't be added inline. This is the one spot the implementer must read surrounding code before editing.
