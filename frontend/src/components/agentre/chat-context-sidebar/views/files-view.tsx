import { ChevronDown, ChevronRight, Folder } from "lucide-react";
import * as React from "react";
import { useTranslation } from "react-i18next";

import { deriveFileTree, type FileEntry, type FileTreeNode } from "../derive";

import { FileTypeIcon, SidebarRow } from "./sidebar-row";

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
        <SidebarRow
          sessionId={sessionId}
          cwd={cwd}
          remote={remote}
          sourceMode="changes"
          kind="dir"
          path={dirPath}
          name={node.name}
          depth={depth}
          expanded={!isCollapsed}
          onToggle={() => toggleCollapse(dirPath)}
          ariaLabel={
            isCollapsed
              ? t("chatContext.files.expandFolder", { name: node.name })
              : t("chatContext.files.collapseFolder", { name: node.name })
          }
          lead={
            <>
              {isCollapsed ? (
                <ChevronRight
                  className="size-3.5 shrink-0"
                  aria-hidden="true"
                />
              ) : (
                <ChevronDown className="size-3.5 shrink-0" aria-hidden="true" />
              )}
              <Folder className="size-3.5 shrink-0" aria-hidden="true" />
            </>
          }
          testId="changes-row"
          withMenu={false}
        />
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

  const renderFile = (entry: FileEntry, depth: number) => (
    <SidebarRow
      key={entry.path}
      sessionId={sessionId}
      cwd={cwd}
      remote={remote}
      sourceMode="changes"
      kind="file"
      path={entry.path}
      name={basename(entry.path)}
      depth={depth}
      title={entry.path}
      onJumpToTurn={() => onJumpToTurn(entry.lastTurn)}
      lead={
        <>
          {/* 预留与目录 chevron 等宽的槽位，让同级目录名/文件名、图标列对齐 */}
          <span className="size-3.5 shrink-0" aria-hidden="true" />
          <FileTypeIcon path={entry.path} />
        </>
      }
      trailing={
        <span className="ml-auto flex shrink-0 items-center">
          <DiffBadge plus={entry.plus} minus={entry.minus} />
        </span>
      }
      testId="changes-row"
      rowData={{ "data-path": entry.path }}
    />
  );

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
