import * as React from "react";

import { clientLog } from "@/lib/client-log";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { StreamSubscriber } from "./stream-subscriber";

import type { ChatStreamEvent } from "@/hooks/use-chat-stream";
import type { AgentStatus } from "@/stores/types";

function bumpSessionTabToAfterPinned(sessionId: number): void {
  const tabsState = useChatTabsStore.getState();
  const tab = tabsState.tabs.find(
    (t) => t.meta.kind === "session" && t.meta.sessionId === sessionId,
  );
  if (!tab) return;
  tabsState.bumpToAfterPinned(tab.id);
}

// ChatStreamsHost 是「无 DOM 的全局订阅器」。挂在 App 顶层、Routes 同级,
// 跨路由 不会 unmount —— 即使 /chat 被切走,这里的 <StreamSubscriber> 依然
// 持续从 Wails EventsOn 收事件、写到 zustand store。ChatPanel 切回来时
// 直接从 store 读 liveBlocks/liveDelta 还原完整流式视图。
//
// 这里不该有任何业务判断,只做「Wails event → store action」一次翻译。
export function ChatStreamsHost(): React.ReactElement | null {
  const streams = useChatStreamsStore((s) => s.streams);
  const appendLiveText = useChatStreamsStore((s) => s.appendLiveText);
  const appendLiveThinking = useChatStreamsStore((s) => s.appendLiveThinking);
  const appendLiveToolUse = useChatStreamsStore((s) => s.appendLiveToolUse);
  const appendLiveToolResult = useChatStreamsStore(
    (s) => s.appendLiveToolResult,
  );
  const appendLivePlanUpdate = useChatStreamsStore(
    (s) => s.appendLivePlanUpdate,
  );
  const mergeSubagentMeta = useChatStreamsStore((s) => s.mergeSubagentMeta);
  const setLiveRetry = useChatStreamsStore((s) => s.setLiveRetry);
  const clearLiveRetry = useChatStreamsStore((s) => s.clearLiveRetry);
  const finishStream = useChatStreamsStore((s) => s.finishStream);
  const consumeSteer = useChatStreamsStore((s) => s.consumeSteer);
  const appendLiveAskUserQuestion = useChatStreamsStore(
    (s) => s.appendLiveAskUserQuestion,
  );
  const markAskUserQuestionAnswered = useChatStreamsStore(
    (s) => s.markAskUserQuestionAnswered,
  );
  const appendLiveToolPermissionRequest = useChatStreamsStore(
    (s) => s.appendLiveToolPermissionRequest,
  );
  const markToolPermissionResolved = useChatStreamsStore(
    (s) => s.markToolPermissionResolved,
  );
  const patchLiveUsage = useChatStreamsStore((s) => s.patchLiveUsage);
  const patchLiveContextWindow = useChatStreamsStore(
    (s) => s.patchLiveContextWindow,
  );
  const appendLiveCompactBoundary = useChatStreamsStore(
    (s) => s.appendLiveCompactBoundary,
  );
  const setLiveCompacting = useChatStreamsStore((s) => s.setLiveCompacting);

  const handleEvent = React.useCallback(
    (sessionId: number, ev: ChatStreamEvent) => {
      // retry 是「正在等下次尝试」的瞬时态;下面这些进展事件到达就意味着上一次重试已经成功
      // 拿到了新内容,RetryNoticeCard 该撤掉。store 内部 null→null 是 referential no-op,
      // 这里只要在事件入口顺手调一次就够了。done/error/closed/aborted/steer_consumed
      // 走各自的 finish/consume 路径已经隐式清空 liveRetry,无需重复调用。
      switch (ev.kind) {
        case "chunk":
          if (ev.delta) {
            clearLiveRetry(sessionId);
            appendLiveText(sessionId, ev.delta);
          }
          return;
        case "thinking":
          if (ev.delta) {
            clearLiveRetry(sessionId);
            appendLiveThinking(sessionId, ev.delta);
          }
          return;
        case "tool_use":
          // toolUseId / toolName 任一存在才算有效 —— 与旧版 applyLiveToolUse 行为一致。
          if (!ev.toolUseId && !ev.toolName) return;
          clearLiveRetry(sessionId);
          appendLiveToolUse(sessionId, {
            toolUseId: ev.toolUseId,
            toolName: ev.toolName,
            toolInput: ev.toolInput,
            canonical: ev.canonical,
            parentToolUseId: ev.parentToolUseId,
            subagent: ev.subagent,
          });
          return;
        case "tool_result":
          if (!ev.toolUseId && typeof ev.toolResult === "undefined") return;
          clearLiveRetry(sessionId);
          // toolResultMeta 必传 —— claudecode TaskCreate 的真实 task id 走这里
          // 透出(meta.task.id),backend 的 task_aggregator 消费后合成 canonical.
          // PlanUpdate 推回前端。漏掉 → backend 拿不到 id → task-progress 列表里
          // 该任务永远停在 pending 且 TaskUpdate 找不到 id 落空。
          appendLiveToolResult(sessionId, {
            toolUseId: ev.toolUseId,
            text: ev.toolResult ?? "",
            isError: !!ev.isError,
            parentToolUseId: ev.parentToolUseId,
            toolResultMeta: ev.toolResultMeta,
          });
          return;
        case "subagent_started":
        case "subagent_progress":
        case "subagent_done":
          if (ev.toolUseId && ev.subagent) {
            clearLiveRetry(sessionId);
            mergeSubagentMeta(sessionId, ev.toolUseId, ev.subagent);
          }
          return;
        case "retry":
          setLiveRetry(sessionId, {
            attempt: ev.retryAttempt ?? 0,
            maxAttempts: ev.retryMaxAttempts ?? 0,
            message: ev.retryMessage ?? "",
            details: ev.retryDetails ?? "",
            at: ev.retryAt ?? Date.now(),
          });
          return;
        case "ask_user_question":
          if (!ev.askUserQuestion) return;
          clearLiveRetry(sessionId);
          if (ev.askUserQuestion.answered || ev.askUserQuestion.skipped) {
            markAskUserQuestionAnswered(
              sessionId,
              ev.askUserQuestion,
              ev.canonical,
            );
          } else {
            bumpSessionTabToAfterPinned(sessionId);
            appendLiveAskUserQuestion(
              sessionId,
              ev.askUserQuestion,
              ev.canonical,
            );
          }
          return;
        case "plan_update":
          clearLiveRetry(sessionId);
          appendLivePlanUpdate(sessionId, ev.delta ?? "", ev.canonical);
          return;
        case "tool_permission_request":
          if (!ev.toolPermission) return;
          clearLiveRetry(sessionId);
          // ev.canonical 必须随事件落到 store —— 后端 dispatcher_emitter 已经为
          // tool.permission / plan.approve_request 计算好 canonical, 漏传会让
          // CanonicalToolRouter fallback 到 RawToolCard (空标题 + 简化 overlay)。
          if (ev.toolPermission.resolved) {
            markToolPermissionResolved(
              sessionId,
              ev.toolPermission,
              ev.canonical,
            );
          } else {
            bumpSessionTabToAfterPinned(sessionId);
            appendLiveToolPermissionRequest(
              sessionId,
              ev.toolPermission,
              ev.canonical,
            );
          }
          return;
        case "session_status": {
          if (!ev.sessionStatus) return;
          // session_status patch 一般只带 agentStatus + needsAttention,
          // permissionMode / contextWindow 可能单独到达。contextWindow 写 live stream,
          // 其它事件保留 store 里之前的 permissionMode/status,避免被未携带字段清空。
          if ((ev.sessionStatus.contextWindow ?? 0) > 0) {
            patchLiveContextWindow(
              sessionId,
              ev.sessionStatus.contextWindow ?? 0,
            );
          }
          const nextStatus = ev.sessionStatus.agentStatus;
          const hasStatus = !!nextStatus;
          const hasMode = !!ev.sessionStatus.permissionMode;
          if (!hasStatus && !hasMode) return;
          const prev = useSessionStatusStore.getState().statuses.get(sessionId);
          // 诊断: 收到 agentStatus="error" 但本 sid 仍有活跃 LiveStream entry,
          // 说明后端在 events channel 关闭前就推了 error 帧 (理论上不该发生 ——
          // 末端 emit 走在 StreamError 之前但 StreamClosed 之后流就应当结束)。
          // 命中即埋根因证据, 一并打 prev/next/streamActive 让排查不用回放事件。
          if (hasStatus && nextStatus === "error") {
            const live = useChatStreamsStore.getState().streams.get(sessionId);
            if (live) {
              clientLog.warn(
                "chat-streams-host",
                "session_status agentStatus=error received while LiveStream is still active",
                {
                  sessionId,
                  prevAgentStatus: prev?.agentStatus,
                  nextAgentStatus: nextStatus,
                  needsAttention: ev.sessionStatus.needsAttention,
                  streamAgeMs: Date.now() - live.streamStartedAt,
                },
              );
            }
          }
          if (
            hasStatus &&
            (ev.sessionStatus.needsAttention ||
              nextStatus === "running" ||
              nextStatus === "waiting" ||
              nextStatus === "error")
          ) {
            bumpSessionTabToAfterPinned(sessionId);
          }
          useSessionStatusStore.getState().upsert(sessionId, {
            // Wails boundary: backend sends agentStatus as string; cast to AgentStatus.
            agentStatus: (hasStatus
              ? nextStatus
              : (prev?.agentStatus ?? "idle")) as AgentStatus,
            needsAttention: hasStatus
              ? ev.sessionStatus.needsAttention
              : (prev?.needsAttention ?? false),
            permissionMode:
              ev.sessionStatus.permissionMode || prev?.permissionMode,
          });
          return;
        }
        case "usage":
          // turn 内每次模型 API call 边界后端推一条 per-call usage 快照；写到
          // LiveStream.liveUsage 上让 Composer 进度条实时刷新。不动 liveRetry：
          // usage 帧本身不算「正在重试」的成功信号（chunk/tool_use 才算）。
          if (!ev.usage) return;
          patchLiveUsage(sessionId, ev.usage);
          return;
        case "compact_boundary":
          // claudecode CLI 通报上下文已压缩 (manual /compact 或 auto 阈值触发)。
          // 落一个 type=compact_boundary 的 live block,transcript 据此渲染分隔卡
          // + 折叠之前的旧消息。trigger / preTokens 不一定有,UI 退化即可。
          // store 内部同步把 liveCompacting 清回 false,不必再单独发 status:""。
          if (!ev.compact) return;
          clearLiveRetry(sessionId);
          appendLiveCompactBoundary(sessionId, {
            preTokens: ev.compact.preTokens,
            trigger: ev.compact.trigger,
            at: ev.compact.at,
          });
          return;
        case "runtime_status":
          // claudecode CLI 的会话级运行状态过渡 (manual /compact 或 auto 阈值开始
          // 时一帧 compacting:true,压缩结束由 compact_boundary 自动清旗)。
          if (!ev.runtimeStatus) return;
          setLiveCompacting(sessionId, !!ev.runtimeStatus.compacting);
          return;
        case "steer_consumed":
          consumeSteer(sessionId, ev);
          return;
        case "done":
        case "error":
        case "closed":
        case "aborted":
          if (ev.kind !== "closed") {
            bumpSessionTabToAfterPinned(sessionId);
          }
          finishStream(sessionId, ev);
          return;
      }
    },
    [
      appendLiveText,
      appendLiveThinking,
      appendLiveToolUse,
      appendLiveToolResult,
      appendLivePlanUpdate,
      mergeSubagentMeta,
      setLiveRetry,
      clearLiveRetry,
      finishStream,
      consumeSteer,
      appendLiveAskUserQuestion,
      markAskUserQuestionAnswered,
      appendLiveToolPermissionRequest,
      markToolPermissionResolved,
      patchLiveUsage,
      patchLiveContextWindow,
      appendLiveCompactBoundary,
    ],
  );

  return (
    <>
      {Array.from(streams.values()).map((s) => (
        <StreamSubscriber
          key={s.sessionId}
          streamName={s.name}
          onEvent={(ev) => handleEvent(s.sessionId, ev)}
        />
      ))}
    </>
  );
}
