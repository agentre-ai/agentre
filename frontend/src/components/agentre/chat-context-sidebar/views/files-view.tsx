import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
  Eye,
  FileCode,
  Folder,
} from "lucide-react";
import * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import { deriveFileTree, type FileEntry, type FileTreeNode } from "../derive";
import { resolvePreviewRelPath } from "../previewable";

import { indentStyle } from "./tree-indent";
import { useOpenFile } from "./use-open-file";

type Props = {
  sessionId: number;
  files: FileEntry[];
  cwd: string;
  remote: boolean;
  onJumpToTurn: (turn: number) => void;
};

function DiffBadge({ plus, minus }: { plus: number; minus: number }) {
  if (plus <= 0 && minus <= 0) return null;
  return (
    <span
      aria-hidden="true"
      className="inline-flex shrink-0 items-center gap-1 font-mono text-[10px] font-medium"
    >
      {plus > 0 ? <span className="text-status-running">+{plus}</span> : null}
      {minus > 0 ? <span className="text-destructive">−{minus}</span> : null}
    </span>
  );
}

function basename(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] ?? path;
}

export function FilesView({
  sessionId,
  files,
  cwd,
  remote,
  onJumpToTurn,
}: Props) {
  const { t } = useTranslation();
  const tree = React.useMemo(() => deriveFileTree(files), [files]);
  const filePathsKey = files.map((file) => file.path).join("\u0000");

  // 当前被预览文件的 relPath(按会话);与某行的预览按钮同路径时高亮它。
  const previewPath = useChatSidebarStore(
    (s) => s.previewBySession[sessionId]?.path,
  );
  const openPreview = useChatSidebarStore((s) => s.openPreview);

  // 展开状态仅存组件内、不持久化；文件集合变化时重置为全部展开。
  const [collapsed, setCollapsed] = React.useState<Set<string>>(new Set());
  React.useEffect(() => {
    setCollapsed(new Set());
  }, [filePathsKey]);

  const toggleCollapse = (dirPath: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(dirPath)) next.delete(dirPath);
      else next.add(dirPath);
      return next;
    });
  };

  const canOpen = cwd !== "" && !remote;
  const openFile = useOpenFile(cwd);

  if (files.length === 0) {
    return (
      <div className="px-3 py-6 text-center text-xs text-muted-foreground">
        {t("chatContext.files.empty")}
      </div>
    );
  }

  const renderDir = (
    node: Extract<FileTreeNode, { kind: "dir" }>,
    dirPath: string,
    depth: number,
  ) => {
    const isCollapsed = collapsed.has(dirPath);
    return (
      <div key={dirPath} className="flex flex-col">
        <button
          type="button"
          onClick={() => toggleCollapse(dirPath)}
          aria-expanded={!isCollapsed}
          aria-label={
            isCollapsed
              ? t("chatContext.files.expandFolder", { name: node.name })
              : t("chatContext.files.collapseFolder", { name: node.name })
          }
          className="flex items-center gap-1.5 rounded-md py-1.5 pr-2.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/50"
          style={indentStyle(depth)}
        >
          {isCollapsed ? (
            <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />
          ) : (
            <ChevronDown className="size-3.5 shrink-0" aria-hidden="true" />
          )}
          <Folder
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="flex-1 truncate font-mono">{node.name}</span>
        </button>
        {!isCollapsed ? (
          <div className="flex flex-col">
            {node.children.map((child) =>
              child.kind === "dir"
                ? renderDir(child, `${dirPath}/${child.name}`, depth + 1)
                : renderFile(child.entry, depth + 1),
            )}
          </div>
        ) : null}
      </div>
    );
  };

  const renderFile = (entry: FileEntry, depth: number) => {
    const previewRelPath = resolvePreviewRelPath(entry.path, cwd);
    return (
      <div
        key={entry.path}
        className="flex items-center"
        style={indentStyle(depth)}
      >
        <button
          type="button"
          onClick={() => onJumpToTurn(entry.lastTurn)}
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md py-1.5 pr-2.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/50"
          title={entry.path}
        >
          {/* 预留与目录 chevron 等宽的槽位，让同级目录名/文件名、图标列对齐 */}
          <span className="size-3.5 shrink-0" aria-hidden="true" />
          <FileCode
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="min-w-0 flex-1 truncate font-mono">
            {basename(entry.path)}
          </span>
          <DiffBadge plus={entry.plus} minus={entry.minus} />
        </button>
        {previewRelPath !== null ? (
          <button
            type="button"
            aria-label={t("chatContext.filePreview.open")}
            title={t("chatContext.filePreview.open")}
            onClick={() => openPreview(sessionId, previewRelPath)}
            className={cn(
              "ml-1 shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:text-foreground",
              previewPath === previewRelPath && "text-primary",
            )}
          >
            <Eye className="size-3" aria-hidden="true" />
          </button>
        ) : null}
        {canOpen ? (
          <button
            type="button"
            aria-label={t("chatContext.files.openFile")}
            title={t("chatContext.files.openFile")}
            onClick={() => openFile(entry.path)}
            className="ml-1 shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="size-3" aria-hidden="true" />
          </button>
        ) : null}
      </div>
    );
  };

  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {tree.map((node) =>
        node.kind === "dir"
          ? renderDir(node, node.name, 0)
          : renderFile(node.entry, 0),
      )}
    </div>
  );
}
