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
  /** R10：cwd 为空时的结构化原因，透给「目录」模式渲染专用空态。 */
  cwdUnavailableReason?: string;
  /** 会话绑定的项目 id，R10 空态的"指定本机路径"入口据此调用 ProjectSetLocalPath。 */
  projectId?: number;
  /** 指定路径成功后的回调——调用方据此重新 LoadSession。 */
  onCwdSpecified?: () => void;
};

/**
 * FilesPanel 是「文件」页的外壳：一行「变动 / 目录」两档胶囊 + 右端的「显示
 * 忽略项」与「搜索」两个图标按钮（都只在目录档出现）+ 内容区。Git 已提升为顶层
 * tab（设计决策 1），不再是这里的第三档；这一行连同顶层 TabBar 共两行常驻
 * chrome，搜索输入框只在过滤态激活期间占临时的第三行。
 *
 * 「变动」模式的内容区仍是 FilesView —— 纯前端从 messages 派生、零后端调用；行的
 * 单击语义与另外两个模式一样是「开预览标签」（决策 5 取代了上一轮「变动模式整行
 * 点击跳回对应轮次」的硬不变量，跳轮次降级成右键菜单的一项）。「目录」模式才走后端。
 */
export function FilesPanel({
  sessionId,
  files,
  cwd,
  remote,
  gitChanges = null,
  onJumpToTurn,
  cwdUnavailableReason,
  projectId,
  onCwdSpecified,
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
            cwdUnavailableReason={cwdUnavailableReason}
            projectId={projectId}
            onCwdSpecified={onCwdSpecified}
            gitChanges={gitChanges}
            search={search}
          />
        ) : null}
      </div>
    </div>
  );
}
