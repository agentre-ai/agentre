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
 * FilesPanel 是「文件」页的外壳：三段模式控件 + 随模式变化的上下文条 + 内容区。
 *
 * 「变动」模式的内容区仍是原样的 FilesView —— 纯前端从 messages 派生、零后端调用、
 * 行点击跳回对应轮次（硬不变量 1）。「目录」与「Git」两个模式才走后端。
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
      <FilesModeSwitcher
        active={mode}
        onChange={setMode}
        changesCount={files.length}
      />
      {mode === "directory" ? (
        <div className="flex h-7 shrink-0 items-center border-b border-border px-2">
          <Toggle
            size="sm"
            className="ml-auto h-5 gap-1 px-1.5 text-[10px] data-[state=on]:bg-transparent data-[state=on]:text-primary"
            pressed={showIgnored}
            onPressedChange={setShowIgnored}
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
            <span>{t("chatContext.directory.showIgnored")}</span>
          </Toggle>
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-auto">
        {mode === "changes" ? (
          <FilesView
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
        {/*
          「Git」模式（未提交 / 本分支两档 + 基线分支选择 + 扁平变动行）接在这里，
          上下文条的 git 分支同样接在上面的条件里。
        */}
      </div>
    </div>
  );
}
