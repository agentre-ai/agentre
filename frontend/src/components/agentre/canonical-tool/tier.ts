// tier.ts — 一步工具调用落在哪一档视觉权重的纯判据。按顺序取第一个命中:
// ① canonical.kind(后端已算好)② input shape(与 raw/summary.ts 同源探测)
// ③ 都不匹配落中性层。**不依赖工具名硬集合** —— 那是 backend-specific 知识泄漏
// (raw/summary.ts 开头的规矩,本文件沿用)。
//
// 中性层是关键分支:认不出来的工具不假装它是只读,不能给它读层的轻量级视觉权重。

import type { ChatBlockData } from "@/stores/chat-streams-store";

import type { CanonicalDTO, CanonicalKind } from "./types";

export type Tier = "out" | "write" | "read" | "neutral";

const OUT_KINDS = new Set<CanonicalKind>([
  "agent.spawn",
  "user.ask",
  "plan.approve_request",
  "tool.permission",
]);

const WRITE_KINDS = new Set<CanonicalKind>(["file.write", "file.edit"]);

const COMMAND_KEYS = ["command", "cmd"];
const WRITE_SHAPE_KEYS = ["content", "edits", "old_string"];
const READ_SHAPE_KEYS = ["path", "pattern", "query", "url"];

export function tier(block: ChatBlockData): Tier {
  // block.canonical 的生成类型(wailsjs view.CanonicalDTO)把 kind 宽化成 string;
  // 这里借道本地 CanonicalDTO 拿回字面量联合类型,与其余 canonical-tool 卡片同一手法
  // (如 file-write/card.tsx:28、agent-spawn/card.tsx:51)。
  const kind = (block as { canonical?: CanonicalDTO }).canonical?.kind;
  if (kind) {
    if (OUT_KINDS.has(kind)) return "out";
    if (WRITE_KINDS.has(kind)) return "write";
  }

  const input = block.toolInput as Record<string, unknown> | undefined;
  if (input) {
    if (hasAnyKey(input, COMMAND_KEYS)) return "write";
    if (hasAnyKey(input, WRITE_SHAPE_KEYS)) return "write";
    if (hasAnyKey(input, READ_SHAPE_KEYS)) return "read";
  }

  return "neutral";
}

function hasAnyKey(input: Record<string, unknown>, keys: string[]): boolean {
  return keys.some((k) => input[k] != null);
}

// displayName 把 `mcp__<server>__<tool>` 形态拆成 "server · tool";其余原样返回。
export function displayName(toolName: string): string {
  const prefix = "mcp__";
  if (!toolName.startsWith(prefix)) return toolName;
  const rest = toolName.slice(prefix.length);
  const sep = rest.indexOf("__");
  if (sep === -1) return toolName;
  const server = rest.slice(0, sep);
  const tool = rest.slice(sep + 2);
  if (!server || !tool) return toolName;
  return `${server} · ${tool}`;
}
