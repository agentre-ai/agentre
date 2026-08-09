import { Eye, EyeOff, Search } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Toggle } from "@/components/ui/toggle";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import type { FileEntry } from "../derive";
import { FilesModeSwitcher } from "../files-mode-switcher";
import type { Change } from "../git-rows";

import { DirectoryView } from "./directory-view";
import { FilesView } from "./files-view";
import { useDirectorySearch } from "./use-directory-search";

type Props = {
  sessionId: number;
  files: FileEntry[];
  cwd: string;
  remote: boolean;
  /**
   * 目录模式的 git 状态叠加数据（served requirement「目录模式的 git 状态叠加」）：
   * 非 git 仓库、读取失败或尚未加载时为 null——目录树照常渲染，只是不带叠加。
   */
  gitChanges?: Change[] | null;
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
  gitChanges = null,
  onJumpToTurn,
}: Props) {
  const { t } = useTranslation();
  const mode = useChatSidebarStore((s) => s.filesMode);
  const setMode = useChatSidebarStore((s) => s.setFilesMode);
  const showIgnored = useChatSidebarStore((s) => s.showIgnored);
  const setShowIgnored = useChatSidebarStore((s) => s.setShowIgnored);
  // 搜索过滤态与查询串是本次查看会话的临时交互状态，不进持久化 store（决策见
  // use-directory-search.ts 顶部注释）；持有在这一层是因为搜索按钮与「显示
  // 忽略项」同一行在这里，而结果区在 DirectoryView——两者需要共享同一份状态。
  const search = useDirectorySearch(sessionId, showIgnored);

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
        {mode === "directory" && cwd !== "" ? (
          <Toggle
            size="sm"
            className="h-6 w-6 shrink-0 p-0 data-[state=on]:bg-transparent data-[state=on]:text-primary"
            pressed={search.active}
            onPressedChange={search.toggle}
            aria-label={t("chatContext.directory.search")}
            title={
              search.active
                ? t("chatContext.directory.searchOn")
                : t("chatContext.directory.searchOff")
            }
          >
            <Search className="size-3" aria-hidden="true" />
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
            gitChanges={gitChanges}
            search={search}
          />
        ) : null}
      </div>
    </div>
  );
}
