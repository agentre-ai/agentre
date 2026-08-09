import * as React from "react";
import { useTranslation } from "react-i18next";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import type { chat_svc } from "../../../../wailsjs/go/models";

import { ResizableSidebar } from "../resizable-sidebar";

import { deriveFiles, deriveOutline } from "./derive";
import { TabBar } from "./tab-bar";
import { FilesPanel } from "./views/files-panel";
import { GitContextBar } from "./views/git-context-bar";
import { GitView } from "./views/git-view";
import { OutlineView } from "./views/outline-view";
import { useGitChanges } from "./views/use-git-changes";

type Msg = chat_svc.ChatMessage;

type Props = {
  sessionId: number;
  messages: Msg[];
  activeMessageId: number | null;
  onJumpToMessage: (messageId: number) => void;
  cwd?: string;
  remote?: boolean;
};

export function ChatContextSidebar({
  sessionId,
  messages,
  activeMessageId,
  onJumpToMessage,
  cwd = "",
  remote = false,
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

  // Git 页的取数挂在这一层：顶层 tab 的计数角标与 Git 页内容共用同一份状态；
  // enabled 保证只有 Git 顶层 tab 可见时才打后端（决策 13），跟随 Git 从
  // 「文件」页内一档提升为顶层 tab 而改挂在这里（此前挂在 FilesPanel 上）。
  const git = useGitChanges({ sessionId, cwd, enabled: activeTab === "git" });

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
        gitCount={git.count}
      />
      {/*
        「文件」页自带两档胶囊与上下文条，滚动容器在 FilesPanel 内部；「大纲」页
        仍用这里的滚动容器（scrollRef 要拿它做 scrollIntoView）；「Git」页的上下文
        条与现状一致（两档 + 基线），非 git 仓库时整条收起（mockup 板 C2）。
      */}
      {activeTab === "outline" ? (
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
          <OutlineView
            items={outline}
            activeMessageId={resolvedActiveId}
            onSelect={onJumpToMessage}
          />
        </div>
      ) : activeTab === "files" ? (
        <FilesPanel
          sessionId={sessionId}
          files={files}
          cwd={cwd}
          remote={remote}
          onJumpToTurn={(turn) => {
            const mid = turnToMessageId.get(turn);
            if (mid != null) onJumpToMessage(mid);
          }}
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col">
          {cwd !== "" && !git.notARepo ? (
            <GitContextBar
              scope={git.scope}
              onScopeChange={git.setScope}
              baseRef={git.baseRef}
              branches={git.branches}
              onSelectBaseline={git.selectBaseline}
            />
          ) : null}
          <div className="min-h-0 flex-1 overflow-auto">
            <GitView
              sessionId={sessionId}
              cwd={cwd}
              remote={remote}
              scope={git.scope}
              baseRef={git.baseRef}
              state={git.state}
              onRetry={git.reload}
            />
          </div>
        </div>
      )}
    </ResizableSidebar>
  );
}
