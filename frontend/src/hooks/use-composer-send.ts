import * as React from "react";

import type { ChatImageAttachment } from "@/components/agentre/chat";
import { useBackendCapabilities } from "@/components/agentre/capability/use-backend-capabilities";
import { usePermissionMode } from "@/components/agentre/permission-mode";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { markSessionRunning } from "@/stores/session-status-store";

import { EnqueueChatMessage, SendChatMessage } from "../../wailsjs/go/app/App";
import { chat_svc } from "../../wailsjs/go/models";

export type ComposerSendResult = {
  sessionId: number;
  userMessageId: number;
  assistantMessageId: number;
};

const EMPTY_META = {
  allowedModes: [] as string[],
  defaultMode: "",
  switchableDuringTurn: false,
  order: [] as string[],
};

// useComposerSend 是 chat-panel 之外(编排会话尾栏等)复用的发送原语:封装
// SendChatMessage(新起 turn)/ EnqueueChatMessage(turn 进行中排队)分支 +
// openStream 接流 + markSessionRunning 乐观翻态 + permission mode 只读透出。
// 不含 optimistic transcript 插入(交给调用方 onOptimistic),不含 queued-messages
// store 落盘(enqueue 成功后的排队 chip 展示留给未来任务)。
export function useComposerSend(args: {
  sessionId: number;
  agentId: number;
  backendType: string;
  /** turn 进行中(有 live stream)时 submit 走 enqueue 而非新起 turn */
  isRunning: boolean;
  /** optimistic 插入回调;footer 无 transcript 时省略 */
  onOptimistic?: (
    r: ComposerSendResult,
    text: string,
    images: ChatImageAttachment[],
  ) => void;
  /** 权限模式初值(来自 session detail;footer 可省) */
  initialMode?: string;
  initialModeAtLaunch?: string;
  hasActiveSession?: boolean;
}) {
  const {
    sessionId,
    agentId,
    backendType,
    isRunning,
    onOptimistic,
    initialMode,
    initialModeAtLaunch,
    hasActiveSession,
  } = args;
  const openStream = useChatStreamsStore((s) => s.openStream);
  const { caps } = useBackendCapabilities(
    sessionId > 0 ? undefined : backendType,
  );
  const isModeSwitchable = !!caps?.has("set_permission_mode");
  const supportsImageInput = !!caps?.has("image_input");
  const permissionModeMeta = caps?.permissionModeMeta ?? EMPTY_META;
  const permissionMode = usePermissionMode({
    sessionId: isModeSwitchable && sessionId > 0 ? sessionId : undefined,
    permissionModeMeta,
    runtimeKey: backendType,
    initialMode,
    initialModeAtLaunch,
    hasActiveSession: hasActiveSession ?? false,
  });

  const [sending, setSending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const submit = React.useCallback(
    async (msg: {
      text: string;
      images?: ChatImageAttachment[];
    }): Promise<ComposerSendResult | null> => {
      const text = msg.text.trim();
      const images = msg.images ?? [];
      if (!text || sending) return null;
      setSending(true);
      setError(null);
      try {
        if (isRunning) {
          await EnqueueChatMessage({ sessionId, text });
          return null;
        }
        const payload: Record<string, unknown> = {
          sessionId,
          agentId,
          text,
          projectId: 0,
          permissionMode: isModeSwitchable ? permissionMode.mode : "",
        };
        if (images.length > 0) {
          payload.images = images.map((i) => ({
            name: i.name,
            dataUrl: i.dataUrl,
          }));
        }
        const resp = await SendChatMessage(
          chat_svc.SendRequest.createFrom(payload),
        );
        const r: ComposerSendResult = {
          sessionId: resp.sessionId,
          userMessageId: resp.userMessageId,
          assistantMessageId: resp.assistantMessageId,
        };
        markSessionRunning(resp.sessionId);
        openStream({
          name: resp.stream,
          sessionId: resp.sessionId,
          assistantMessageId: resp.assistantMessageId,
          streamStartedAt: Date.now(),
        });
        onOptimistic?.(r, text, images);
        return r;
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : String(e));
        return null;
      } finally {
        setSending(false);
      }
    },
    [
      sessionId,
      agentId,
      isRunning,
      isModeSwitchable,
      permissionMode,
      openStream,
      onOptimistic,
      sending,
    ],
  );

  return {
    submit,
    sending,
    error,
    backendType,
    isModeSwitchable,
    supportsImageInput,
    permissionMode,
    permissionModeMeta,
  };
}
