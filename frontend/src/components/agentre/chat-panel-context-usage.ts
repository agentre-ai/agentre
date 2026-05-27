import type { ChatStreamUsage } from "@/hooks/use-chat-stream";

import type { chat_svc } from "../../../wailsjs/go/models";

type SvcChatMessage = chat_svc.ChatMessage;

// computeComposerContextUsage 计算 Composer 底栏「上下文用量」展示值。
//
// 前端不做 provider/backend family-specific 聚合 —— runtime translator 在每条
// StreamUsage / ChatMessage 上自报 TotalInputTokens 列(spec §"Token &
// ContextWindow"),按 family 算好的总输入。前端直接读即可。
//
//   - contextWindow <= 0 时返回 undefined(前端整块隐藏)。
//   - liveUsage.totalInputTokens > 0 优先用(turn 进行中阶梯式刷新)。
//   - 否则从尾部往前找首条 totalInputTokens > 0 的 assistant message。
//   - 都没有时返回 { used: 0, max }(渲染 0/max 进度条占位)。
export function computeComposerContextUsage(
  messages: SvcChatMessage[],
  contextWindow: number,
  liveUsage?: ChatStreamUsage | null,
): { used: number; max: number } | undefined {
  if (contextWindow <= 0) return undefined;
  if (liveUsage?.totalInputTokens && liveUsage.totalInputTokens > 0) {
    return { used: liveUsage.totalInputTokens, max: contextWindow };
  }
  let used = 0;
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "assistant") continue;
    if (m.totalInputTokens > 0) {
      used = m.totalInputTokens;
      break;
    }
  }
  return { used, max: contextWindow };
}
