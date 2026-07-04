import * as React from "react";

import type { ChatImageAttachment } from "@/components/agentre/chat";
import { useChatSession } from "@/hooks/use-chat-session";
import { useChatStream, type ChatStreamEvent } from "@/hooks/use-chat-stream";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import {
  markSessionRunning,
  useSessionStatusStore,
} from "@/stores/session-status-store";

import type { chat_svc } from "../../wailsjs/go/models";
import { useComposerSend } from "./use-composer-send";

type SvcChatMessage = chat_svc.ChatMessage;

const EMPTY_LIVE = {
  liveDelta: "",
  liveThinking: "",
  liveBlocks: [] as unknown[],
  liveRetry: null as unknown,
  liveStreamStartedAt: 0,
  streaming: false,
  liveCompacting: false,
};

// optimisticUser / optimisticAssistantPlaceholder 与 chat-panel.tsx 里的同名函数
// 逐字同源(乐观占位消息形状),但 chat-panel.tsx 里那两个是模块私有函数、未导出,
// 而 chat-panel.tsx 本身在本任务范围外不可修改(并发编辑中)。所以这里保留一份
// 本地等价实现,而不是从 chat-panel.tsx import。
function optimisticUser(
  id: number,
  sid: number,
  text: string,
  images: ChatImageAttachment[] = [],
): SvcChatMessage {
  const blocks: Array<Record<string, unknown>> = [];
  if (text) blocks.push({ type: "text", text });
  for (const image of images) {
    blocks.push({
      type: "image",
      image: {
        dataUrl: image.dataUrl,
        mediaType: image.mediaType,
        name: image.name,
      },
    });
  }
  return {
    id,
    sessionId: sid,
    role: "user",
    blocks,
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: Date.now(),
  } as unknown as SvcChatMessage;
}

function optimisticAssistantPlaceholder(
  id: number,
  sid: number,
): SvcChatMessage {
  return {
    id,
    sessionId: sid,
    role: "assistant",
    blocks: [],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: Date.now(),
  } as unknown as SvcChatMessage;
}

// useLiveConversation 是编排会话视图的对话原语:聚合持久消息(useChatSession)+
// 实时流覆盖层(chat-streams-store)+ 自主续轮旁路(chat:autonomous:<sessionId>)+
// turn 结束后的对账 reload,再组合 Task 1 的 useComposerSend 提供发送能力。
// 与 chat-panel.tsx 的关系:行为上是其"自主轮 + live overlay + reload"三段逻辑
// 的最小复用集,但两者不共享代码(chat-panel.tsx 不可修改),后续若要收敛重复
// 需要专门的重构任务把这三段下沉成共享模块。
export function useLiveConversation(sessionId: number, agentId: number) {
  const { session, messages, setMessages, reload } = useChatSession(sessionId);
  const openStream = useChatStreamsStore((s) => s.openStream);

  // ── live overlay ──
  const stream = useChatStreamsStore((s) =>
    sessionId ? (s.streams.get(sessionId) ?? null) : null,
  );
  const live = stream
    ? {
        liveDelta: stream.liveDelta,
        liveThinking: stream.liveThinking,
        liveBlocks: stream.liveBlocks,
        liveRetry: stream.liveRetry,
        liveStreamStartedAt: stream.streamStartedAt,
        streaming: true,
        liveCompacting: stream.liveCompacting,
      }
    : EMPTY_LIVE;

  // ── 自发轮捕获(后台任务完成后 CLI 自主续轮 / 后台 subagent 内部活动) ──
  const onAutonomous = React.useCallback(
    (ev: ChatStreamEvent) => {
      if (ev.kind === "subagent_activity_started") {
        if (!ev.stream || !ev.launchMessageId) return;
        openStream({
          name: ev.stream,
          sessionId,
          assistantMessageId: ev.launchMessageId,
          streamStartedAt: Date.now(),
        });
        return;
      }
      if (
        ev.kind !== "autonomous_started" ||
        !ev.assistantMessage ||
        !ev.stream
      ) {
        return;
      }
      const amsg = ev.assistantMessage;
      markSessionRunning(sessionId);
      openStream({
        name: ev.stream,
        sessionId,
        assistantMessageId: amsg.id,
        streamStartedAt: Date.now(),
      });
      setMessages((prev) =>
        prev.some((m) => m.id === amsg.id) ? prev : [...prev, amsg],
      );
    },
    [sessionId, openStream, setMessages],
  );
  useChatStream(
    sessionId ? `chat:autonomous:${sessionId}` : null,
    onAutonomous,
  );

  // ── turn 结束 → reload 持久消息对账 ──
  const doneTick = useSessionStatusStore((s) =>
    sessionId ? (s.statuses.get(sessionId)?.doneTick ?? 0) : 0,
  );
  const lastSeenDoneTick = React.useRef(doneTick);
  React.useEffect(() => {
    if (doneTick !== lastSeenDoneTick.current) {
      lastSeenDoneTick.current = doneTick;
      reload();
    }
  }, [doneTick, reload]);

  // ── 发送(带 optimistic 插入) ──
  const onOptimistic = React.useCallback(
    (
      r: {
        sessionId: number;
        userMessageId: number;
        assistantMessageId: number;
      },
      text: string,
      images: ChatImageAttachment[],
    ) => {
      setMessages((prev) => [
        ...prev,
        optimisticUser(r.userMessageId, r.sessionId, text, images),
        optimisticAssistantPlaceholder(r.assistantMessageId, r.sessionId),
      ]);
    },
    [setMessages],
  );
  const sender = useComposerSend({
    sessionId,
    agentId,
    backendType: session?.backendType ?? "",
    isRunning: live.streaming,
    onOptimistic,
    initialMode: session?.permissionMode,
    initialModeAtLaunch: session?.permissionModeAtLaunch,
    hasActiveSession: messages.length > 0,
  });

  const contextUsage = {
    used: 0,
    max: session?.contextWindow ?? 0,
  };

  return {
    messages,
    live,
    submit: sender.submit,
    sending: sender.sending,
    isModeSwitchable: sender.isModeSwitchable,
    supportsImageInput: sender.supportsImageInput,
    permissionMode: sender.permissionMode,
    permissionModeMeta: sender.permissionModeMeta,
    backendType: sender.backendType,
    contextUsage,
  };
}
