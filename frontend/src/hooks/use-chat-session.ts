import { useCallback, useEffect, useMemo, useState } from "react";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import type { chat_svc } from "../../wailsjs/go/models";
import { clientLog } from "@/lib/client-log";
import {
  hasSessionStream,
  primaryStream,
  streamForMessage,
  useChatStreamsStore,
} from "@/stores/chat-streams-store";
import { useSessionConnStore } from "@/stores/session-conn-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import {
  normalizeSessionSnapshot,
  useSessionStatusStore,
} from "@/stores/session-status-store";
import { useSessionWithOverlays } from "./use-session-with-overlays";
import type { AgentStatus, SessionConnectionState } from "@/stores/types";

export type ChatSessionDetail = chat_svc.ChatSessionDetail & {
  deviceID?: string;
  deviceName?: string;
  online?: boolean;
  cwd?: string;
};
export type ChatMessage = chat_svc.ChatMessage;

export function useChatSession(sessionId: number) {
  const [session, setSession] = useState<ChatSessionDetail | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // useSessionWithOverlays 合并 meta + status + read-overlay，作为详情页
  // 运行时态的唯一来源。sessionWithLiveStatus 从此通过 overlay 读取，而不是
  // 直接订阅 session-status-store。
  const overlay = useSessionWithOverlays(sessionId);

  const reload = useCallback(async () => {
    if (!sessionId) {
      setSession(null);
      setMessages([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const startedDoneTick =
        useSessionStatusStore.getState().statuses.get(sessionId)?.doneTick ?? 0;
      let resp = await LoadChatSession({ sessionId });
      const returnedDoneTick =
        useSessionStatusStore.getState().statuses.get(sessionId)?.doneTick ?? 0;
      if (returnedDoneTick > startedDoneTick) {
        clientLog.warn(
          "use-chat-session",
          "reloading session because a turn finished during LoadChatSession",
          {
            sessionId,
            startedDoneTick,
            returnedDoneTick,
          },
        );
        resp = await LoadChatSession({ sessionId });
      }
      setSession(resp.session);
      // loadedMessages 可能在下方 activeStream 分支被替换(剥离 overlay pending
      // tool_approval 块),setMessages 统一挪到该分支之后执行。
      let loadedMessages = resp.messages ?? [];
      // Cache session 的静态字段 (agentColor / agentName / projectId / title /
      // lastMessageAt / lastReadAt) 到 session-meta-store, 让 TabStrip 在不主动
      // LoadSession 的前提下能拿到这些 detail 字段渲染 avatar 色 / 项目色下划线 /
      // tooltip 项目链 + attention 判断。
      //
      // setMeta 是 replace 语义,所以 lastReadAt 必须显式带上 ——
      // 否则会把 chat-agents-store.bulkUpsert 之前写入的服务端值擦掉,attention
      // 判断在客户端 override 缺席时会误判成未读。
      useSessionMetaStore.getState().setMeta(sessionId, {
        agentId: resp.session.agentId,
        agentName: resp.session.agentName,
        agentColor: resp.session.agentColor,
        projectId: resp.session.projectId ?? 0,
        title: resp.session.title,
        lastMessageAt: resp.session.lastMessageAt ?? 0,
        lastReadAt: resp.session.lastReadAt ?? 0,
        permissionModeAtLaunch: resp.session.permissionModeAtLaunch ?? "",
      });
      // 把详情快照里的 agentStatus / needsAttention / permissionMode 灌进
      // session-status-store, 让其它读路径(tab / sidebar / use-tabs-view)拿到
      // 最新值, 不依赖独立 reload。
      //
      // 诊断: LoadChatSession 是异步 DB 快照。若本 sid 仍有活跃 LiveStream 而
      // 详情说 agentStatus="error"/"idle", 大概率是 reload 在 turn 起手前发起、
      // 响应到达时 Send 已经把 DB 翻 "running"。normalizeSessionSnapshot 会忽略
      // 这次旧状态覆盖；这里保留诊断证据。
      const live = primaryStream(useChatStreamsStore.getState(), sessionId);
      if (
        live &&
        resp.session.agentStatus !== "running" &&
        resp.session.agentStatus !== "waiting"
      ) {
        const prev = useSessionStatusStore.getState().statuses.get(sessionId);
        clientLog.warn(
          "use-chat-session",
          "ignored stale LoadChatSession agentStatus while LiveStream is active",
          {
            sessionId,
            prevAgentStatus: prev?.agentStatus,
            loadedAgentStatus: resp.session.agentStatus,
            streamAgeMs: Date.now() - live.streamStartedAt,
          },
        );
      }
      const snapshot = normalizeSessionSnapshot(
        sessionId,
        {
          // Wails boundary: backend sends agentStatus as string; cast to AgentStatus.
          agentStatus: resp.session.agentStatus as AgentStatus,
          needsAttention: resp.session.needsAttention,
          permissionMode: resp.session.permissionMode,
          bgRunning: resp.session.bgRunning ?? false,
        },
        !!live,
      );
      useSessionStatusStore.getState().upsert(sessionId, snapshot);
      // 重挂活跃 turn 的实时流。自主轮 / subagent 子轮等"非前端发起"的 turn 没有 Send
      // 响应入口给出 per-turn 流名,中途打开会话就看不到"生成中"和流式内容 ——
      // LoadSession 在有活跃 turn 时回传 activeStream,这里据此 openStream 续看。
      // 已有活跃 LiveStream 时不覆盖(避免打断正常 Send 已开的流);流名指向在跑的
      // (末条)assistant 消息,StreamDone 经既有路径收口并 reload 回填最终内容。
      if (resp.session.activeStream) {
        const streamsStore = useChatStreamsStore.getState();
        let lastAssistantIdx = -1;
        for (let i = loadedMessages.length - 1; i >= 0; i--) {
          if (loadedMessages[i].role === "assistant") {
            lastAssistantIdx = i;
            break;
          }
        }
        if (lastAssistantIdx >= 0) {
          const lastAssistant = loadedMessages[lastAssistantIdx];
          // overlay pending tool_approval 块搬进 liveBlocks(单一真相源):
          // 后端把内存里悬而未决的审批 overlay 进末条 assistant 消息投影。若留在
          // persisted messages 路径,之后的 resolved 流事件只反扫 liveBlocks →
          // no-op → 卡片永远 pending。这里从消息副本剥离 + 注入 live store,
          // resolved 自然命中;同时避免与流事件已写入的同 requestId live 块双卡
          // (transcript 两路 push 同 identity 行不会自动去重)。注入按 requestId
          // 去重,已有活跃流且 liveBlocks 已含该卡时只剥不注。
          const isPendingToolApproval = (b: chat_svc.ChatBlock) =>
            b.type === "tool_approval" && b.toolApproval?.status === "pending";
          const pendingApprovals = (lastAssistant.blocks ?? []).filter(
            isPendingToolApproval,
          );
          const isPendingExecApproval = (b: chat_svc.ChatBlock) =>
            b.type === "exec_approval" && b.execApproval?.status === "pending";
          const pendingExecApprovals = (lastAssistant.blocks ?? []).filter(
            isPendingExecApproval,
          );
          if (pendingApprovals.length > 0 || pendingExecApprovals.length > 0) {
            loadedMessages = loadedMessages.slice();
            loadedMessages[lastAssistantIdx] = {
              ...lastAssistant,
              blocks: (lastAssistant.blocks ?? []).filter(
                (b) => !isPendingToolApproval(b) && !isPendingExecApproval(b),
              ),
            } as ChatMessage;
          }
          // 已有活跃 LiveStream 时不覆盖(避免打断正常 Send 已开的流)。
          if (
            !hasSessionStream(streamsStore, sessionId) &&
            lastAssistant.id > 0
          ) {
            streamsStore.openStream({
              name: resp.session.activeStream,
              sessionId,
              assistantMessageId: lastAssistant.id,
              streamStartedAt: Date.now(),
            });
          }
          for (const block of pendingApprovals) {
            const approval = block.toolApproval;
            if (!approval?.requestId) continue;
            const liveNow = streamForMessage(
              useChatStreamsStore.getState(),
              sessionId,
              lastAssistant.id,
            );
            const exists = liveNow?.liveBlocks.some(
              (b) =>
                b.type === "tool_approval" &&
                b.toolApproval?.requestId === approval.requestId,
            );
            if (!exists) {
              useChatStreamsStore
                .getState()
                .appendLiveToolApproval(sessionId, lastAssistant.id, approval);
            }
          }
          for (const block of pendingExecApprovals) {
            const approval = block.execApproval;
            if (!approval?.id) continue;
            const liveNow = streamForMessage(
              useChatStreamsStore.getState(),
              sessionId,
              lastAssistant.id,
            );
            const exists = liveNow?.liveBlocks.some(
              (b) =>
                b.type === "exec_approval" &&
                b.execApproval?.id === approval.id,
            );
            if (!exists) {
              useChatStreamsStore
                .getState()
                .appendLiveExecApproval(sessionId, lastAssistant.id, approval);
            }
          }
        }
      }
      // 连接态播种。整页重载会清空 session-conn-store,而这条会话可能正卡在退避
      // 窗口中间(断连不再终结会话,上面的 activeStream 分支照旧把流重挂起来):
      // 不播种,用户在整个窗口里看到的都是普通打字指示器,分不清 agent 在想还是网断了。
      // 只在会话确有活跃流时落笔 —— 断连形态是活信号的一种形态,没有活信号就没有
      // 可修饰的对象;更要紧的是清条目的责任在 chat:conn:<sid> 的订阅者手上,
      // 而它只跟着活跃流挂载,给没有流的会话写条目就是写一条永远清不掉的记录。
      if (
        resp.session.connectionState &&
        hasSessionStream(useChatStreamsStore.getState(), sessionId)
      ) {
        useSessionConnStore
          .getState()
          .seed(
            sessionId,
            resp.session.connectionState as SessionConnectionState,
          );
      }
      setMessages(loadedMessages);
      // 注:不在这里 MarkRead。语义上"用户已读到 lastMessageAt"只能由
      // ChatPanel 根据 active prop 判断 —— 隐藏 tab 也会 mount useChatSession,
      // 在这里无条件 MarkRead 会把用户没看过的 session 标记成已读。
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // sessionWithLiveStatus 把 LoadSession 拿到的 detail 与 useSessionWithOverlays
  // 当前态合并:运行时翻转(turn 起手乐观 running / waiting 翻转 / 详情 reload 回填)
  // 都从 overlay 读, 详情对象本身的 agentStatus / needsAttention / permissionMode
  // 被 overlay 覆盖。这样所有写路径只对 store 一次写, 详情页 toolbar 跟侧栏 / tab
  // 拿到同一份事实。
  const sessionWithLiveStatus = useMemo(() => {
    if (!session) return null;
    if (!overlay) return session;
    return {
      ...session,
      agentStatus: overlay.agentStatus,
      needsAttention: overlay.needsAttention,
      permissionMode: overlay.permissionMode ?? session.permissionMode,
    };
  }, [session, overlay]);

  return {
    session: sessionWithLiveStatus,
    messages,
    loading,
    error,
    reload,
    setMessages,
  };
}
