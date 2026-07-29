import type { chat_svc } from "../../../../wailsjs/go/models";

// flipSubagentStatusInMessages 在已持久化的 messages 里就地翻转 tool_use block 的
// subagent.status(+ summary)。后台任务的完成是跨轮的(autonomous 轮携带
// completedTask):完成事件到达时,发起它的主轮早已结束,那条 tool_use block 已从
// liveBlocks 落进 messages —— 所以只改 liveBlocks(mergeSubagentMeta)翻不到它,面板
// 与行内 pill 会永远 spin。这里按 toolUseId 命中 messages 里的块并翻状态,免整会话
// 重载即可停转。
//
// 不可变更新:命中才返回新数组/新消息/新块引用,未命中(或空参)原样返回同一引用,
// 让 React setState 跳过无谓 re-render。
export function flipSubagentStatusInMessages(
  messages: chat_svc.ChatMessage[],
  toolUseId: string,
  status: string,
  summary?: string,
): chat_svc.ChatMessage[] {
  if (!status) return messages;
  return mergeSubagentMetaInMessages(messages, toolUseId, {
    status,
    ...(summary ? { summary } : {}),
  } as chat_svc.ChatBlockSubagent);
}

// mergeSubagentMetaInMessages 把一份 subagent meta 合并进 messages 里命中 toolUseId 的
// tool_use block。后台 subagent 在会话空闲态跑的那段时间,CLI 一直吐 task_progress,而它
// 的派遣卡早已从 liveBlocks 落进 messages —— store 的 mergeSubagentMeta 只翻 liveBlocks,
// 合并必然落空,卡片上的工具数 / token 会一直停在派遣那一刻。
//
// 合并语义与 store 侧一致:后端 meta 字段全是 omitempty,这一帧没带的字段在 JSON 里直接
// 缺席 → 展开时不会把已有值抹成 0/空。
//
// 不可变更新:命中才返回新数组/新消息/新块引用,未命中(或空参)原样返回同一引用,
// 让 React setState 跳过无谓 re-render。
export function mergeSubagentMetaInMessages(
  messages: chat_svc.ChatMessage[],
  toolUseId: string,
  meta: chat_svc.ChatBlockSubagent,
): chat_svc.ChatMessage[] {
  if (!toolUseId || !meta) return messages;

  let hit = false;
  const next = messages.map((m) => {
    let blockHit = false;
    const blocks = (m.blocks ?? []).map((b) => {
      if (b.type !== "tool_use" || b.toolUseId !== toolUseId || !b.subagent) {
        return b;
      }
      blockHit = true;
      return {
        ...b,
        subagent: { ...b.subagent, ...meta },
      } as chat_svc.ChatBlock;
    });
    if (!blockHit) return m;
    hit = true;
    return { ...m, blocks } as chat_svc.ChatMessage;
  });

  return hit ? next : messages;
}
