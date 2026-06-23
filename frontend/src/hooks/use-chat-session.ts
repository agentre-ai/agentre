import { useCallback, useEffect, useMemo, useState } from "react";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import type { chat_svc } from "../../wailsjs/go/models";
import { clientLog } from "@/lib/client-log";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";
import { useSessionWithOverlays } from "./use-session-with-overlays";
import type { AgentStatus } from "@/stores/types";

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
      const resp = await LoadChatSession({ sessionId });
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
        groupId: resp.session.groupId ?? 0,
        groupTitle: resp.session.groupTitle ?? "",
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
      // 响应到达时 Send 已经把 DB 翻 "running" —— 旧快照覆盖乐观值会让 tab
      // 翻红/翻灰而内容仍在流。命中即埋根因证据。
      const live = useChatStreamsStore.getState().streams.get(sessionId);
      if (
        live &&
        resp.session.agentStatus !== "running" &&
        resp.session.agentStatus !== "waiting"
      ) {
        const prev = useSessionStatusStore.getState().statuses.get(sessionId);
        clientLog.warn(
          "use-chat-session",
          "LoadChatSession upsert about to override agentStatus while LiveStream is active",
          {
            sessionId,
            prevAgentStatus: prev?.agentStatus,
            loadedAgentStatus: resp.session.agentStatus,
            streamAgeMs: Date.now() - live.streamStartedAt,
          },
        );
      }
      useSessionStatusStore.getState().upsert(sessionId, {
        // Wails boundary: backend sends agentStatus as string; cast to AgentStatus.
        agentStatus: resp.session.agentStatus as AgentStatus,
        needsAttention: resp.session.needsAttention,
        permissionMode: resp.session.permissionMode,
      });
      // 重挂活跃 turn 的实时流。群聊成员轮 / 自主轮等"非前端发起"的 turn 没有 Send
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
          if (pendingApprovals.length > 0) {
            loadedMessages = loadedMessages.slice();
            loadedMessages[lastAssistantIdx] = {
              ...lastAssistant,
              blocks: (lastAssistant.blocks ?? []).filter(
                (b) => !isPendingToolApproval(b),
              ),
            } as ChatMessage;
          }
          // 已有活跃 LiveStream 时不覆盖(避免打断正常 Send 已开的流)。
          if (!streamsStore.streams.get(sessionId) && lastAssistant.id > 0) {
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
            const liveNow = useChatStreamsStore
              .getState()
              .streams.get(sessionId);
            const exists = liveNow?.liveBlocks.some(
              (b) =>
                b.type === "tool_approval" &&
                b.toolApproval?.requestId === approval.requestId,
            );
            if (!exists) {
              useChatStreamsStore
                .getState()
                .appendLiveToolApproval(sessionId, approval);
            }
          }
        }
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
