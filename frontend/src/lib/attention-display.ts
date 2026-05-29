// frontend/src/lib/attention-display.ts
//
// attention reason → UI 投影。所有"红还是橙"、"显示什么 pill 文案"决策集中在这里；
// attention-store 不知道这些。
import i18n from "@/i18n";
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
  if (reason === "needs_attention") return i18n.t("attention.needsAttention");
  if (reason === "error") return i18n.t("attention.error");
  if (reason === "unread") return i18n.t("attention.unread");
  return null;
}
