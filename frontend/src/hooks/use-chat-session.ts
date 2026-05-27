import { useCallback, useEffect, useMemo, useState } from "react";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import type { chat_svc } from "../../wailsjs/go/models";
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
      setMessages(resp.messages ?? []);
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
      useSessionStatusStore.getState().upsert(sessionId, {
        // Wails boundary: backend sends agentStatus as string; cast to AgentStatus.
        agentStatus: resp.session.agentStatus as AgentStatus,
        needsAttention: resp.session.needsAttention,
        permissionMode: resp.session.permissionMode,
      });
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
