import * as React from "react";
import { useTranslation } from "react-i18next";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import type { chat_svc } from "../../../../wailsjs/go/models";

import { ResizableSidebar } from "../resizable-sidebar";

import { deriveFiles, deriveOutline } from "./derive";
import { TabBar } from "./tab-bar";
import { FilesPanel } from "./views/files-panel";
import { OutlineView } from "./views/outline-view";

type Msg = chat_svc.ChatMessage;

type Props = {
  sessionId: number;
  messages: Msg[];
  activeMessageId: number | null;
  onJumpToMessage: (messageId: number) => void;
  cwd?: string;
  remote?: boolean;
  /** R10：cwd 为空时的结构化原因，透给「目录」模式渲染专用空态。 */
  cwdUnavailableReason?: string;
  /** 会话绑定的项目 id，R10 空态的"指定本机路径"入口据此调用 ProjectSetLocalPath。 */
  projectId?: number;
  /** 指定路径成功后的回调——调用方据此重新 LoadSession。 */
  onCwdSpecified?: () => void;
};

export function ChatContextSidebar({
  sessionId,
  messages,
  activeMessageId,
  onJumpToMessage,
  cwd = "",
  remote = false,
  cwdUnavailableReason,
  projectId,
  onCwdSpecified,
}: Props) {
  const { t } = useTranslation();
  const activeTab = useChatSidebarStore((s) => s.activeTab);
  const setActiveTab = useChatSidebarStore((s) => s.setActiveTab);

  const outline = React.useMemo(() => deriveOutline(messages), [messages]);
  const files = React.useMemo(() => deriveFiles(messages), [messages]);

  const turnToMessageId = React.useMemo(() => {
    const m = new Map<number, number>();
    let turn = 0;
    for (const msg of messages) {
      if (msg.role === "user") {
        turn += 1;
        m.set(turn, msg.id);
      }
    }
    return m;
  }, [messages]);

  // messageIdToTurnUserId 把任意 message id 映射回它所在 turn 的 user 消息 id，
  // 让 outline 高亮在「问–答」整段区间内都锚定在同一行，直到下一个 user 消息出现。
  const messageIdToTurnUserId = React.useMemo(() => {
    const m = new Map<number, number>();
    let anchor: number | null = null;
    for (const msg of messages) {
      if (msg.role === "user") anchor = msg.id;
      if (anchor != null) m.set(msg.id, anchor);
    }
    return m;
  }, [messages]);

  const resolvedActiveId =
    activeMessageId != null
      ? (messageIdToTurnUserId.get(activeMessageId) ?? null)
      : null;

  const scrollRef = React.useRef<HTMLDivElement>(null);

  // resolvedActiveId 变化时把对应 outline 行推到滚动区域底部，让右侧进度跟随 transcript。
  React.useEffect(() => {
    if (resolvedActiveId == null) return;
    const container = scrollRef.current;
    if (!container) return;
    const row = container.querySelector<HTMLElement>(
      `[data-outline-message-id="${resolvedActiveId}"]`,
    );
    if (row) row.scrollIntoView({ block: "end", inline: "nearest" });
  }, [resolvedActiveId, activeTab]);

  return (
    <ResizableSidebar
      persistenceKey="chat-context"
      ariaLabel={t("chatContext.sidebar")}
      edge="left"
      defaultWidth={240}
      className="h-full"
    >
      <TabBar
        active={activeTab}
        onChange={setActiveTab}
        outlineCount={outline.length}
        filesCount={files.length}
      />
      {/*
        「文件」页自带模式控件与上下文条，滚动容器在 FilesPanel 内部；「大纲」页
        仍用这里的滚动容器（scrollRef 要拿它做 scrollIntoView）。
      */}
      {activeTab === "outline" ? (
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
          <OutlineView
            items={outline}
            activeMessageId={resolvedActiveId}
            onSelect={onJumpToMessage}
          />
        </div>
      ) : (
        <FilesPanel
          sessionId={sessionId}
          files={files}
          cwd={cwd}
          remote={remote}
          onJumpToTurn={(turn) => {
            const mid = turnToMessageId.get(turn);
            if (mid != null) onJumpToMessage(mid);
          }}
          cwdUnavailableReason={cwdUnavailableReason}
          projectId={projectId}
          onCwdSpecified={onCwdSpecified}
        />
      )}
    </ResizableSidebar>
  );
}
