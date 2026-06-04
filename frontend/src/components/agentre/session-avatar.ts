// session-avatar 把 session 的 agent 元信息（颜色 token + 名字）推导成头像所需的
// { letter, color }，供 tab 条与通知 toast 等处复用，避免各自再实现一遍。
import type { AgentColor } from "./types";
import { agentColorOrder } from "./types";

const AGENT_COLOR_SET = new Set<string>(agentColorOrder);

// tokenToCssColor 把 agent / project 颜色 token 映射成 css 变量；非法 token → null。
export function tokenToCssColor(
  token: string | null | undefined,
): string | null {
  if (!token) return null;
  if (!AGENT_COLOR_SET.has(token as AgentColor)) return null;
  return `var(--${token})`;
}

// firstLetter 取名字首字符作头像字母；空名回落 "?"。
export function firstLetter(name: string | null | undefined): string {
  if (!name) return "?";
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return Array.from(trimmed)[0] ?? "?";
}

export type AvatarMeta =
  | { agentColor?: string | null; agentName?: string | null }
  | null
  | undefined;

// avatarFromMeta 从 session meta 推导头像；meta 缺失时回落灰底问号。
export function avatarFromMeta(meta: AvatarMeta): {
  letter: string;
  color: string;
} {
  return {
    letter: firstLetter(meta?.agentName),
    color: tokenToCssColor(meta?.agentColor) ?? "#94a3b8",
  };
}
