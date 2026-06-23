import { useEffect, useRef } from "react";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { chat_svc, view } from "../../wailsjs/go/models";

// ChatSessionStatusPatch mirrors backend chat_svc.ChatSessionStatusPatch.
// Type definition unified into @/stores/types (ChatSessionStatusEvent); import + re-export here.
import type { ChatSessionStatusEvent } from "@/stores/types";
export type ChatSessionStatusPatch = ChatSessionStatusEvent;

// ChatStreamUsage mirrors backend chat_svc.ChatStreamUsage. Carried on the
// "usage" kind to live-update the Composer's context-usage progress bar
// in the middle of a turn (each model API call boundary fires one).
export type ChatStreamUsage = {
  messageId?: number;
  promptTokens?: number;
  completionTokens?: number;
  cachedTokens?: number;
  cacheCreationTokens?: number;
  reasoningTokens?: number;
  // totalInputTokens runtime translator 按 family 聚合好的本次 API call 输入总量;
  // 前端按它读「已用上下文」,不做 family-specific 加法。
  totalInputTokens?: number;
};

// ChatStreamEvent mirrors backend chat_svc.ChatStreamEvent. Fields are optional
// because only the ones relevant to a given `kind` are populated.
export type ChatStreamEvent = {
  kind:
    | "chunk"
    | "thinking"
    | "tool_use"
    | "tool_result"
    | "steer_consumed"
    | "subagent_started"
    | "subagent_progress"
    | "subagent_done"
    | "retry"
    | "message_end"
    | "done"
    | "error"
    | "closed"
    | "aborted"
    | "ask_user_question"
    | "plan_update"
    | "tool_permission_request"
    | "tool_approval"
    | "session_status"
    | "usage"
    | "compact_boundary"
    | "runtime_status"
    | "autonomous_started";
  delta?: string;
  message?: chat_svc.ChatMessage;
  error?: string;

  // steer_consumed:
  queuedIds?: string[];
  previousAssistantMessage?: chat_svc.ChatMessage;
  userMessages?: chat_svc.ChatMessage[];
  assistantMessage?: chat_svc.ChatMessage;

  // tool_use:
  toolUseId?: string;
  toolName?: string;
  toolInput?: Record<string, unknown>;
  // canonical — runtime translator 算出的统一工具识别投影。前端 CanonicalToolRouter
  // 按 canonical.kind 分发到 canonical-tool/<kind>/card.tsx;不识别走 RawToolCard。
  // tool_use 之外,tool_permission_request 也会携带 canonical (kind=tool.permission
  // 或 plan.approve_request);ChatStreamsHost 需要把它一并落到 store。
  canonical?: view.CanonicalDTO;

  // tool_result:
  toolResult?: string;
  isError?: boolean;
  // tool_result 元数据 (claudecode CLI 顶层 tool_use_result 原样透传):
  // 典型场景 TaskCreate 在这里返回 {"task":{"id":"1"}}, 前端 task-progress 派生层
  // 据此把 TaskCreate ↔ TaskUpdate 关联起来。普通工具帧无该字段时为 undefined。
  toolResultMeta?: Record<string, unknown>;

  // subagent: tool_use / tool_result 携带 parentToolUseId 表示这是 subagent 内部的子调用；
  // subagent_* 事件携带 toolUseId（指向外层 Agent）+ subagent meta，前端按 toolUseId 把
  // meta merge 到对应 ChatBlock 的 subagent 字段。
  parentToolUseId?: string;
  subagent?: chat_svc.ChatBlockSubagent;

  // ask_user_question: 携带交互问题载荷（初次到达）或答完后的状态切换
  // （Answered=true，前端按 requestId 找到既有 block 更新）。
  askUserQuestion?: chat_svc.ChatBlockAskUserQuestion;

  // tool_permission_request: 携带工具审批载荷（初次到达）或审批后的状态切换
  // （Resolved=true，前端按 requestId 找到既有 block 更新）。
  toolPermission?: chat_svc.ChatBlockToolPermission;

  // tool_approval: agent 内置写工具审批。status="pending" 为新卡(appendLiveToolApproval),
  // "approved"|"denied"|"expired" 为决议更新(markToolApprovalResolved,同 requestId)。
  // 这些字段平铺在事件上(不像 toolPermission 走一个嵌套对象),ChatStreamsHost 据此
  // 合成 ToolApprovalData。toolKey 标识来源工具(org / group_create / ...);requestId
  // 同时被 tool_approval 与未来其它按 id 关联的事件共用。
  toolKey?: string;
  requestId?: string;
  status?: string;
  result?: string;

  // session_status: 后端推上来的 session 级 status patch
  // （agentStatus + needsAttention）。ChatStreamsHost 把它写到 LiveStream
  // 上，useChatSession 订阅后覆盖到 ChatSessionDetail，让 toolbar 实时变色。
  sessionStatus?: ChatSessionStatusPatch;

  // retry: 后端/上游的非终态重试通知；本轮 stream 仍继续运行。
  retryAttempt?: number;
  retryMaxAttempts?: number;
  retryMessage?: string;
  retryDetails?: string;
  retryAt?: number;

  // usage: 当前 assistant 消息的 per-call usage 快照。每次模型内部 API call
  // 边界（claudecode 的主 agent assistant 帧 / codex 的 token_count notification）
  // 都推一条，前端 store 写到 LiveStream.liveUsage，Composer 进度条读它实时
  // 刷新「已用上下文」，不必等 done 事件 reload 才看到变化。
  usage?: ChatStreamUsage;

  // compact_boundary: 后端识别到 claudecode CLI 的 system.compact_boundary 帧
  // (manual /compact 或 auto 阈值触发)。带 messageId(boundary 落在哪条 assistant
  // 消息) + seq + pre_tokens + trigger + at。前端 ChatStreamsHost 把它落到当前
  // assistant message 的 blocks 末尾 (Type=compact_boundary block);ChatTranscript
  // 按"最后一个 compact_boundary"切分,折叠之前的旧消息。
  compact?: {
    messageId: number;
    seq: number;
    preTokens?: number;
    trigger?: "auto" | "manual";
    at: number;
  };

  // runtime_status: claudecode CLI 的 system{subtype:"status",status:<非空>} 帧
  // 透传 (compacting 等过渡态)。Compacting 是 chat_svc 已经判定的方便位 —— 前端
  // 直接读 compacting 即可,不必再判字符串。compact 结束信号 = compact_boundary /
  // done / error / aborted / closed,store 在 finishStream + appendLiveCompactBoundary
  // 自动清旗,不依赖 CLI 显式 status:"" 帧。
  runtimeStatus?: {
    status?: string;
    compacting?: boolean;
  };

  // autonomous_started: 经会话级旁路事件 "chat:autonomous:<sessionId>" 推上来 ——
  // CLI 在 run_in_background 任务完成后**自主**跑的一轮(无用户输入)被后端捕获。
  // assistantMessage 是要插入 transcript 的新 assistant 行;stream 是该自主轮的
  // per-turn 事件名(前端 openStream 订阅它接后续 chunk/done);trigger="background_task"。
  // completedTask: 触发本自主轮的后台任务身份;前端据此把对应 tool_use.subagent.status
  // 即时翻成 completed/failed,刷新后台任务面板的状态胶囊。summary 为退出码摘要文本。
  stream?: string;
  trigger?: string;
  completedTask?: { toolUseId: string; status: string; summary?: string };
};

export function useChatStream(
  stream: string | null,
  onEvent: (e: ChatStreamEvent) => void,
): void {
  // onEvent 通常被父组件以 `(ev) => handleEvent(sessionId, ev)` 形式包了一层
  // 内联 arrow，每次 render 引用都变。如果直接进 effect 依赖数组，每次父组件
  // 重渲染都会 EventsOff + EventsOn 抖动；在两次订阅之间到达的 Wails 事件
  // 直接被丢（EventsOff 已清掉 listener，新的 EventsOn 还没绑回来）。
  //
  // ChatStreamsHost 监听 streams Map,而每条 chunk/thinking delta 都会替换
  // streams 引用，turn 中每秒几十次重渲染——steer_consumed 撞进抖动窗口的
  // 概率非常高,表现就是 chip 不消除、user 消息也没插入。
  //
  // 用 ref 持有最新 callback,订阅 effect 只在 stream 变化时跑一次 EventsOn,
  // 内部闭包始终读 cbRef.current,既稳定又不丢事件。ref 的更新放进单独的
  // useEffect 而非 render 阶段:concurrent rendering 下 render 可能被丢弃,
  // commit 后再写 ref 才能保证持有的真是已生效的那次 onEvent。
  const cbRef = useRef(onEvent);
  useEffect(() => {
    cbRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!stream) return;
    const handler = (payload: ChatStreamEvent) => cbRef.current(payload);
    // EventsOn 返回精确卸载函数（按 callback 引用解绑），比 EventsOff(name)
    // 更安全:后者会清掉同名所有 listener,未来若有第二个订阅者会被误伤。
    const off = EventsOn(stream, handler);
    return () => {
      if (typeof off === "function") off();
    };
  }, [stream]);
}
