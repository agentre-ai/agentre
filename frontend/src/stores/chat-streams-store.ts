import { create } from "zustand";

import type { ChatStreamEvent, ChatStreamUsage } from "@/hooks/use-chat-stream";

import type { chat_svc, view } from "../../wailsjs/go/models";
import { useSessionStatusStore, type DoneEvent } from "./session-status-store";
import { useQueuedMessagesStore } from "./queued-messages-store";

// ChatBlockData 是 Wails 生成的 chat_svc.ChatBlock 的「纯数据形态」——去掉自动注入的
// convertValues 方法，方便前端用对象字面量构造 / 在 store 内拼装。Wails 实际下行的
// ChatBlock 实例（含 convertValues）也结构性满足这个类型，因此渲染路径同时接受两者。
export type ChatBlockData = Omit<chat_svc.ChatBlock, "convertValues">;

export type RetryNotice = {
  attempt: number;
  maxAttempts: number;
  message: string;
  details: string;
  at: number;
};

// LiveStream 是「该 session 当前正在跑的一轮 turn 的全部前端可见状态」。
// 把它放到全局 store(而不是 ChatPanel 内部 state)的原因:
//   - 用户切到 /projects 时 ChatPage 整棵 unmount,自管 state 会被销毁、
//     <StreamSubscriber> 一并 EventsOff,后端继续推但前端再收不到。
//   - 流式期间到达的 tool_use / tool_result 必须有个地方落,否则切回来时即使重新订阅也丢历史。
//
// 字段含义:
//   - name: Wails 事件流名(后端 SendResponse.Stream),供 ChatStreamsHost 挂 EventsOn。
//   - liveDelta: 尾部还没冻结成 TextBlock 的文字。遇到 tool_use 时整段冻进 liveBlocks,清空。
//   - liveBlocks: 已按真实顺序冻结的文字 / tool_use / tool_result。渲染时摆在 persisted blocks
//     之后、liveDelta 之前。
//   - liveThinking: 单独累计的思考链。Anthropic 协议要求 thinking 一轮一个,在 turn 开头,
//     所以前端也不穿插,统一让 renderer 摆到 liveBlocks 前面。
export type LiveStream = {
  name: string;
  sessionId: number;
  assistantMessageId: number;
  streamStartedAt: number;
  liveDelta: string;
  liveThinking: string;
  liveBlocks: ChatBlockData[];
  liveRetry: RetryNotice | null;
  // liveUsage 由后端 usage 事件推上来：turn 内每次模型 API call 边界一条，
  // 携带 当前 assistant 消息的 per-call token 快照。Composer 进度条优先用它
  // 覆盖 messages 扫描结果，实现 turn 内随工具循环阶梯式刷新「已用上下文」。
  // stream 结束销毁 entry 后回落到 messages-based 计算（持久化 token 列已是
  // 最终值）。
  liveUsage: ChatStreamUsage | null;
  // liveContextWindow 是 Codex runtime 从 token usage notification 里探到的
  // modelContextWindow。首轮 CLI login / provider 未配置时，LoadSession 初始
  // contextWindow 可能为 0；这里让 Composer 在 turn 内立即显示真实窗口。
  liveContextWindow: number;
  // liveCompacting 由后端 runtime_status 事件驱动:claudecode CLI 在 /compact 启动
  // (manual 或 auto) 时推 status:"compacting",chat_svc 翻译成 RuntimeStatus
  // {compacting:true}。前端据此把 typing indicator 替换为"正在压缩上下文…" chip。
  // 在 appendLiveCompactBoundary / finishStream / consumeSteer 自动清回 false,
  // 不依赖 CLI 再推一帧 status:"" 来清旗。
  liveCompacting: boolean;
};

type State = {
  streams: Map<number, LiveStream>;
};

type Actions = {
  openStream: (
    s: Omit<
      LiveStream,
      | "liveDelta"
      | "liveThinking"
      | "liveBlocks"
      | "liveRetry"
      | "liveUsage"
      | "liveContextWindow"
      | "liveCompacting"
    >,
  ) => void;
  closeStream: (sessionId: number) => void;
  appendLiveText: (sessionId: number, delta: string) => void;
  appendLiveThinking: (sessionId: number, delta: string) => void;
  // 接受不带 type 的 partial:action 会统一 stamp 成 "tool_use" / "tool_result",
  // 避免每个 caller 都重复填同样的字段。
  appendLiveToolUse: (
    sessionId: number,
    block: Omit<ChatBlockData, "type">,
  ) => void;
  appendLiveToolResult: (
    sessionId: number,
    block: Omit<ChatBlockData, "type">,
  ) => void;
  // appendLivePlanUpdate 在 stream 上插入或更新 Codex/Cli runtime 上报的计划。
  // 只保留本轮 turn 的最新一张 plan block,避免 update_plan/plan delta 多次到达
  // 时刷出一串重复计划卡。
  appendLivePlanUpdate: (
    sessionId: number,
    text: string,
    canonical?: view.CanonicalDTO,
  ) => void;
  // mergeSubagentMeta 把 subagent_started/progress/done 事件携带的元数据合并到
  // 对应外层 Agent tool_use block 上（按 toolUseId 匹配 liveBlocks 里最近一个）。
  // 字段做浅 merge：新事件未带的字段保留旧值（task_progress 不带 prompt 不会清掉它）。
  mergeSubagentMeta: (
    sessionId: number,
    toolUseId: string,
    meta: chat_svc.ChatBlockSubagent,
  ) => void;
  // appendLiveAskUserQuestion 在 stream 上插入 AskUserQuestion 卡片：与 tool_use
  // 类似先 flush liveDelta 把文字定型，再追加一个 type:"ask_user_question" block。
  // canonical 由后端 dispatcher_emitter 同步附上(canonical.UserAsk),让
  // CanonicalToolRouter 在 live 路径与 replay 路径走同一份卡片。
  appendLiveAskUserQuestion: (
    sessionId: number,
    payload: chat_svc.ChatBlockAskUserQuestion,
    canonical?: view.CanonicalDTO,
  ) => void;
  // markAskUserQuestionAnswered 按 requestId 找到对应 block（live 或重渲染历史），
  // 更新 Answered/Answers/Skipped 字段。用户提交答案时调用，做乐观更新。
  markAskUserQuestionAnswered: (
    sessionId: number,
    payload: chat_svc.ChatBlockAskUserQuestion,
    canonical?: view.CanonicalDTO,
  ) => void;
  // appendLiveToolPermissionRequest 在 stream 上插入工具审批卡片，与
  // appendLiveAskUserQuestion 同款 flushText 流程。
  // canonical 必须随 payload 一起落到 block 上,否则 CanonicalToolRouter 不认识
  // kind=tool.permission 会 fallback 到 RawToolCard (空标题"tool" + 简化 overlay)。
  appendLiveToolPermissionRequest: (
    sessionId: number,
    payload: chat_svc.ChatBlockToolPermission,
    canonical?: view.CanonicalDTO,
  ) => void;
  // markToolPermissionResolved 按 requestId 找到对应 block 更新决策态。
  // 用户点 Allow/Deny 时调用做乐观更新；后端确认到来时再次调以兜底。
  // canonical 可选:乐观更新路径没有完整 canonical 也能跑(store 内部按 existing
  // canonical 合成 resolved/allowed 标志);后端 echo 路径直接传整份新 canonical。
  markToolPermissionResolved: (
    sessionId: number,
    payload: chat_svc.ChatBlockToolPermission,
    canonical?: view.CanonicalDTO,
  ) => void;
  // patchLiveUsage 把后端推来的 per-call usage 快照写到 LiveStream.liveUsage 上。
  // Composer 进度条用它在 turn 内随工具循环实时刷新「已用上下文」，turn 结束
  // entry 被销毁后回落到 messages 扫描。无 stream entry 时静默丢弃（极端 race：
  // usage 帧先于 openStream 到达 —— 下一帧或者 reload 都能兜回来）。
  patchLiveUsage: (sessionId: number, usage: ChatStreamUsage) => void;
  // appendLiveCompactBoundary 把后端 compact_boundary 事件落成一个 live block。
  // 先 flush liveDelta(让边界前已经流出的文本固化为 text block),再插入
  // type=compact_boundary 块,之后流出的内容会落在边界之后。
  appendLiveCompactBoundary: (
    sessionId: number,
    compact: { preTokens?: number; trigger?: "auto" | "manual"; at: number },
  ) => void;
  // patchLiveContextWindow 写入 runtime mid-turn 探到的模型窗口大小。
  patchLiveContextWindow: (sessionId: number, contextWindow: number) => void;
  // setLiveCompacting 设置/清空 liveCompacting 旗。chat-streams-host 在收到
  // runtime_status 事件时按 ev.runtimeStatus.compacting 调用。无 stream entry 时
  // 静默丢弃,避免 race (status 帧先于 openStream)。
  setLiveCompacting: (sessionId: number, compacting: boolean) => void;
  setLiveRetry: (sessionId: number, retry: RetryNotice) => void;
  // clearLiveRetry 把 liveRetry 置空。retry 是「正在等下次尝试」的瞬时态,下一个非 retry
  // 的进展事件(chunk/thinking/tool_use 等)到达 = 重试已成功 = 状态过期。由 ChatStreamsHost
  // 在那些事件入口顺手调一次,避免回复内容已经流出来了 RetryNoticeCard 还挂着。
  // 无 stream 或 liveRetry 本就是 null 时是 referential no-op,不触发 zustand 重渲染。
  clearLiveRetry: (sessionId: number) => void;
  // finishStream 统一处理 done/error/closed:写入 lastDoneEvent 缓存、bump tick、删 stream entry、
  // 清空该会话排队。给 host 一处调用,避免散落多分支。
  finishStream: (sessionId: number, event: ChatStreamEvent) => void;
  // consumeSteer 处理后端确认已消费的排队消息：清掉对应 queue chip，
  // 把 live stream 切到新的 assistant 占位，并 bump tick 让 ChatPanel reload 消息段。
  consumeSteer: (sessionId: number, event: ChatStreamEvent) => void;
};

function flushLiveDelta(s: LiveStream): LiveStream {
  if (s.liveDelta.length === 0) return s;
  return {
    ...s,
    liveBlocks: [...s.liveBlocks, { type: "text", text: s.liveDelta }],
    liveDelta: "",
  };
}

export const useChatStreamsStore = create<State & Actions>((set) => ({
  streams: new Map(),

  openStream: (s) =>
    set((state) => {
      const next = new Map(state.streams);
      next.set(s.sessionId, {
        ...s,
        liveDelta: "",
        liveThinking: "",
        liveBlocks: [],
        liveRetry: null,
        liveUsage: null,
        liveContextWindow: 0,
        liveCompacting: false,
      });
      return { streams: next };
    }),

  closeStream: (sessionId) =>
    set((state) => {
      if (!state.streams.has(sessionId)) return state;
      const next = new Map(state.streams);
      next.delete(sessionId);
      return { streams: next };
    }),

  appendLiveText: (sessionId, delta) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !delta) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveDelta: cur.liveDelta + delta });
      return { streams: next };
    }),

  appendLiveThinking: (sessionId, delta) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !delta) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveThinking: cur.liveThinking + delta });
      return { streams: next };
    }),

  patchLiveUsage: (sessionId, usage) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      // 同值短路：所有 token 字段一致就不重建 Map，避免 zustand 触发多余重渲染。
      // 消息 id 也比一下 —— turn 内换 assistant 段（steer_consumed）时它会变。
      const prev = cur.liveUsage;
      if (
        prev &&
        prev.messageId === usage.messageId &&
        prev.promptTokens === usage.promptTokens &&
        prev.completionTokens === usage.completionTokens &&
        prev.cachedTokens === usage.cachedTokens &&
        prev.cacheCreationTokens === usage.cacheCreationTokens &&
        prev.reasoningTokens === usage.reasoningTokens
      ) {
        return state;
      }
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveUsage: usage });
      return { streams: next };
    }),

  patchLiveContextWindow: (sessionId, contextWindow) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || contextWindow <= 0) return state;
      if (cur.liveContextWindow === contextWindow) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveContextWindow: contextWindow });
      return { streams: next };
    }),

  setLiveCompacting: (sessionId, compacting) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      if (cur.liveCompacting === compacting) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveCompacting: compacting });
      return { streams: next };
    }),

  setLiveRetry: (sessionId, retry) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveRetry: retry });
      return { streams: next };
    }),

  clearLiveRetry: (sessionId) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || cur.liveRetry === null) return state;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveRetry: null });
      return { streams: next };
    }),

  appendLiveToolUse: (sessionId, block) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      const flushed = flushLiveDelta(cur);
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...flushed,
        liveBlocks: [...flushed.liveBlocks, { ...block, type: "tool_use" }],
      });
      return { streams: next };
    }),

  appendLiveToolResult: (sessionId, block) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      // 故意不 flush liveDelta:tool_use→tool_result 之间通常没有用户可见的文字,
      // 把累积的 liveDelta 留给"下一段文字 + 下次 tool_use"那个 flush 时机。
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...cur,
        liveBlocks: [...cur.liveBlocks, { ...block, type: "tool_result" }],
      });
      return { streams: next };
    }),

  appendLiveCompactBoundary: (sessionId, compact) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      const flushed = flushLiveDelta(cur);
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...flushed,
        liveBlocks: [
          ...flushed.liveBlocks,
          {
            type: "compact_boundary",
            compact: {
              preTokens: compact.preTokens,
              trigger: compact.trigger,
              at: compact.at,
            },
          },
        ],
        // compact_boundary 到达 = 压缩流程结束 → 自动清 liveCompacting,
        // 不依赖 CLI 显式再推一帧 status:"" 清旗。
        liveCompacting: false,
      });
      return { streams: next };
    }),

  appendLivePlanUpdate: (sessionId, text, canonical) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur) return state;
      const hasPlanPayload = text || canonical?.kind === "plan.update";
      const planText = hasPlanPayload
        ? (canonical?.planUpdate?.text ?? text)
        : text;
      if (!planText && canonical?.kind !== "plan.update") return state;
      const flushed = flushLiveDelta(cur);
      const nextBlock: ChatBlockData = {
        type: "plan",
        text: planText,
        canonical,
      };
      const targetIdx = flushed.liveBlocks.findIndex(
        (b) => b.type === "plan" && b.canonical?.kind === "plan.update",
      );
      const nextBlocks =
        targetIdx >= 0
          ? flushed.liveBlocks.map((b, i) => (i === targetIdx ? nextBlock : b))
          : [...flushed.liveBlocks, nextBlock];
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...flushed,
        liveBlocks: nextBlocks,
      });
      return { streams: next };
    }),

  appendLiveAskUserQuestion: (sessionId, payload, canonical) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !payload || !payload.requestId) return state;
      const flushed = flushLiveDelta(cur);
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...flushed,
        liveBlocks: [
          ...flushed.liveBlocks,
          {
            type: "ask_user_question",
            askUserQuestion: payload,
            canonical,
          },
        ],
      });
      return { streams: next };
    }),

  markAskUserQuestionAnswered: (sessionId, payload, canonical) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !payload || !payload.requestId) return state;
      const target = payload.requestId;
      // 倒序找最近一个匹配的 ask_user_question block。乐观更新 + 后端回填都走这里。
      let targetIdx = -1;
      for (let i = cur.liveBlocks.length - 1; i >= 0; i--) {
        const b = cur.liveBlocks[i];
        if (
          b.type === "ask_user_question" &&
          b.askUserQuestion?.requestId === target
        ) {
          targetIdx = i;
          break;
        }
      }
      if (targetIdx < 0) return state;
      const existing = cur.liveBlocks[targetIdx];
      const mergedQuestion = {
        ...(existing.askUserQuestion ?? payload),
        ...payload,
      } as chat_svc.ChatBlockAskUserQuestion;
      const merged: ChatBlockData = {
        ...existing,
        askUserQuestion: mergedQuestion,
        canonical: canonical ?? existing.canonical,
      };
      const nextBlocks = [...cur.liveBlocks];
      nextBlocks[targetIdx] = merged;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveBlocks: nextBlocks });
      return { streams: next };
    }),

  appendLiveToolPermissionRequest: (sessionId, payload, canonical) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !payload || !payload.requestId) return state;
      const flushed = flushLiveDelta(cur);
      const next = new Map(state.streams);
      next.set(sessionId, {
        ...flushed,
        liveBlocks: [
          ...flushed.liveBlocks,
          {
            type: "tool_permission_request",
            toolPermission: payload,
            canonical,
          },
        ],
      });
      return { streams: next };
    }),

  markToolPermissionResolved: (sessionId, payload, canonical) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !payload || !payload.requestId) return state;
      const target = payload.requestId;
      let targetIdx = -1;
      for (let i = cur.liveBlocks.length - 1; i >= 0; i--) {
        const b = cur.liveBlocks[i];
        if (
          b.type === "tool_permission_request" &&
          b.toolPermission?.requestId === target
        ) {
          targetIdx = i;
          break;
        }
      }
      if (targetIdx < 0) return state;
      const existing = cur.liveBlocks[targetIdx];
      const mergedSidecar = {
        ...(existing.toolPermission ?? payload),
        ...payload,
      } as chat_svc.ChatBlockToolPermission;
      // canonical 同步更新,避免乐观更新 race —— 新卡读 canonical 为 truth。
      // 调用方传了完整 canonical(后端 echo 路径)直接覆盖;否则按 existing canonical
      // 拷出新对象,只推进 resolved/allowed/alwaysAllow 三个标志位(乐观更新路径)。
      // toolPermission/planApprove 在对应 kind 下必填(后端 dispatcher_emitter 保证),
      // 但 wails 生成 TS 类型为 optional + class 含 convertValues,所以用 cast
      // 构造纯数据对象(运行时只用字段,不调 convertValues)。
      let mergedCanonical = canonical ?? existing.canonical;
      if (
        !canonical &&
        mergedCanonical?.kind === "tool.permission" &&
        mergedCanonical.toolPermission
      ) {
        mergedCanonical = {
          ...mergedCanonical,
          toolPermission: {
            ...mergedCanonical.toolPermission,
            resolved: !!payload.resolved,
            allowed: !!payload.allowed,
            alwaysAllow: !!payload.alwaysAllow,
          },
        } as view.CanonicalDTO;
      } else if (
        !canonical &&
        mergedCanonical?.kind === "plan.approve_request" &&
        mergedCanonical.planApprove
      ) {
        mergedCanonical = {
          ...mergedCanonical,
          planApprove: {
            ...mergedCanonical.planApprove,
            resolved: !!payload.resolved,
            allowed: !!payload.allowed,
          },
        } as view.CanonicalDTO;
      }
      const merged: ChatBlockData = {
        ...existing,
        toolPermission: mergedSidecar,
        canonical: mergedCanonical,
      };
      const nextBlocks = [...cur.liveBlocks];
      nextBlocks[targetIdx] = merged;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveBlocks: nextBlocks });
      return { streams: next };
    }),

  mergeSubagentMeta: (sessionId, toolUseId, meta) =>
    set((state) => {
      const cur = state.streams.get(sessionId);
      if (!cur || !toolUseId) return state;
      // 倒序找最近一个匹配的 tool_use block（同一 toolUseId 在一轮里只会出现一次，
      // 但倒序更稳，能正确处理流式过程中尚未匹配到的边界态）。
      let targetIdx = -1;
      for (let i = cur.liveBlocks.length - 1; i >= 0; i--) {
        const b = cur.liveBlocks[i];
        if (b.type === "tool_use" && b.toolUseId === toolUseId) {
          targetIdx = i;
          break;
        }
      }
      if (targetIdx < 0) return state;
      const target = cur.liveBlocks[targetIdx];
      const merged: ChatBlockData = {
        ...target,
        subagent: { ...(target.subagent ?? {}), ...meta },
      };
      const nextBlocks = [...cur.liveBlocks];
      nextBlocks[targetIdx] = merged;
      const next = new Map(state.streams);
      next.set(sessionId, { ...cur, liveBlocks: nextBlocks });
      return { streams: next };
    }),

  finishStream: (sessionId, event) =>
    set((state) => {
      const streams = new Map(state.streams);
      streams.delete(sessionId);
      useQueuedMessagesStore.getState().clear(sessionId);
      useSessionStatusStore.getState().bumpDone(sessionId, event as DoneEvent);
      return { streams };
    }),

  consumeSteer: (sessionId, event) =>
    set((state) => {
      const streams = new Map(state.streams);
      const cur = streams.get(sessionId);
      if (cur) {
        streams.set(sessionId, {
          ...cur,
          assistantMessageId:
            event.assistantMessage?.id ?? cur.assistantMessageId,
          streamStartedAt: Date.now(),
          liveDelta: "",
          liveThinking: "",
          liveBlocks: [],
          liveRetry: null,
          // 新 assistant 段开始 → 清掉上一段的 compacting chip。
          liveCompacting: false,
        });
      }

      const ids = event.queuedIds ?? [];
      if (ids.length > 0) {
        useQueuedMessagesStore.getState().consume(sessionId, ids);
      }

      useSessionStatusStore.getState().bumpDone(sessionId, event as DoneEvent);
      return { streams };
    }),
}));
