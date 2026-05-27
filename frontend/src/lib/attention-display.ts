// frontend/src/lib/attention-display.ts
//
// attention reason → UI 投影。所有"红还是橙"、"显示什么 pill 文案"决策集中在这里；
// attention-store 不知道这些。
import type { AttentionReason } from "@/stores/attention-store";
import type { AgentStatus } from "@/stores/types";

export function reasonToDisplayStatus(
  reason: AttentionReason | null,
  fallback: AgentStatus,
): AgentStatus {
  if (reason === "needs_attention" || reason === "unread") return "waiting";
  if (reason === "running") return "running";
  if (reason === "error") return "error";
  return fallback;
}

export function reasonToPillText(
  reason: AttentionReason | null,
): string | null {
  if (reason === "needs_attention") return "审批";
  if (reason === "error") return "出错";
  if (reason === "unread") return "未读";
  return null;
}
