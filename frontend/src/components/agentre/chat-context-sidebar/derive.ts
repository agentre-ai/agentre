import type { chat_svc } from "../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

export type OutlineItem = {
  messageId: number;
  turn: number;
  text: string;
  time: number;
  edits: number;
  err: boolean;
};

export type FileEntry = {
  path: string;
  edits: number;
  reads: number;
  lastTurn: number;
};

const EDIT_TOOLS = new Set(["Edit", "Write", "MultiEdit", "apply_patch"]);
const READ_TOOLS = new Set(["Read", "read"]);

function textOf(m: Msg): string {
  for (const b of m.blocks ?? []) {
    if ((b as { type?: string }).type === "text") {
      return (b as { text?: string }).text ?? "";
    }
  }
  return "";
}

function extractToolPaths(
  block: unknown,
): { name: string; paths: string[] } | null {
  const b = block as {
    type?: string;
    name?: string;
    input?: Record<string, unknown>;
  };
  if (b.type !== "tool_use" || !b.name) return null;
  const input = b.input ?? {};
  const paths: string[] = [];
  if (typeof input.file_path === "string") paths.push(input.file_path);
  if (typeof input.path === "string") paths.push(input.path);
  const changes = (input as { changes?: Array<{ path?: string }> }).changes;
  if (Array.isArray(changes)) {
    for (const c of changes) {
      if (typeof c?.path === "string") paths.push(c.path);
    }
  }
  return paths.length > 0 ? { name: b.name, paths } : null;
}

export function deriveOutline(messages: Msg[]): OutlineItem[] {
  const out: OutlineItem[] = [];
  let turn = 0;
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    if (m.role !== "user") continue;
    turn += 1;
    let edits = 0;
    let err = false;
    for (
      let j = i + 1;
      j < messages.length && messages[j].role !== "user";
      j++
    ) {
      const peer = messages[j];
      if (peer.errorText) err = true;
      for (const block of peer.blocks ?? []) {
        const ext = extractToolPaths(block);
        if (ext && EDIT_TOOLS.has(ext.name)) edits += 1;
      }
    }
    out.push({
      messageId: m.id,
      turn,
      text: textOf(m).slice(0, 200),
      time: m.createtime ?? 0,
      edits,
      err,
    });
  }
  return out;
}

export function deriveFiles(messages: Msg[]): FileEntry[] {
  const map = new Map<string, FileEntry>();
  let turn = 0;
  for (const m of messages) {
    if (m.role === "user") {
      turn += 1;
      continue;
    }
    for (const block of m.blocks ?? []) {
      const ext = extractToolPaths(block);
      if (!ext) continue;
      const isEdit = EDIT_TOOLS.has(ext.name);
      const isRead = READ_TOOLS.has(ext.name);
      if (!isEdit && !isRead) continue;
      for (const p of ext.paths) {
        const cur = map.get(p) ?? { path: p, edits: 0, reads: 0, lastTurn: 0 };
        if (isEdit) cur.edits += 1;
        if (isRead) cur.reads += 1;
        cur.lastTurn = Math.max(cur.lastTurn, turn);
        map.set(p, cur);
      }
    }
  }
  return [...map.values()].sort((a, b) => {
    if (b.edits !== a.edits) return b.edits - a.edits;
    return b.lastTurn - a.lastTurn;
  });
}
