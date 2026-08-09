import { Eye, EyeOff } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Toggle } from "@/components/ui/toggle";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import type { FileEntry } from "../derive";
import { FilesModeSwitcher } from "../files-mode-switcher";

import { DirectoryView } from "./directory-view";
import { FilesView } from "./files-view";

type Props = {
  sessionId: number;
  files: FileEntry[];
  cwd: string;
  remote: boolean;
  onJumpToTurn: (turn: number) => void;
};

/**
 * FilesPanel 是「文件」页的外壳：一行「变动 / 目录」两档胶囊 + 右端的「显示
 * 忽略项」图标按钮（仅目录档出现）+ 内容区。Git 已提升为顶层 tab（设计决策
 * 1），不再是这里的第三档；这一行连同顶层 TabBar 共两行常驻 chrome。
 *
 * 「变动」模式的内容区仍是原样的 FilesView —— 纯前端从 messages 派生、零后端调用、
 * 行点击跳回对应轮次（硬不变量 1）。「目录」模式才走后端。
 */
export function FilesPanel({
  sessionId,
  files,
  cwd,
  remote,
  onJumpToTurn,
}: Props) {
  const { t } = useTranslation();
  const mode = useChatSidebarStore((s) => s.filesMode);
  const setMode = useChatSidebarStore((s) => s.setFilesMode);
  const showIgnored = useChatSidebarStore((s) => s.showIgnored);
  const setShowIgnored = useChatSidebarStore((s) => s.setShowIgnored);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-b border-border px-2">
        <FilesModeSwitcher
          active={mode}
          onChange={setMode}
          changesCount={files.length}
        />
        {mode === "directory" ? (
          <Toggle
            size="sm"
            className="ml-auto h-6 w-6 shrink-0 p-0 data-[state=on]:bg-transparent data-[state=on]:text-primary"
            pressed={showIgnored}
            onPressedChange={setShowIgnored}
            aria-label={t("chatContext.directory.showIgnored")}
            title={
              showIgnored
                ? t("chatContext.directory.showIgnoredOn")
                : t("chatContext.directory.showIgnoredOff")
            }
          >
            {showIgnored ? (
              <Eye className="size-3" aria-hidden="true" />
            ) : (
              <EyeOff className="size-3" aria-hidden="true" />
            )}
          </Toggle>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {mode === "changes" ? (
          <FilesView
            sessionId={sessionId}
            files={files}
            cwd={cwd}
            remote={remote}
            onJumpToTurn={onJumpToTurn}
          />
        ) : null}
        {mode === "directory" ? (
          <DirectoryView
            sessionId={sessionId}
            cwd={cwd}
            remote={remote}
            showIgnored={showIgnored}
          />
        ) : null}
      </div>
    </div>
  );
}
