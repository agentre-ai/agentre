import { ChevronDown, ChevronRight, Folder } from "lucide-react";
import * as React from "react";
import { useTranslation } from "react-i18next";

import { WorkspaceFsListDir } from "@/../wailsjs/go/app/App";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useSessionStatus } from "@/stores/session-status-store";

import type { workspace_fs_svc } from "@/../wailsjs/go/models";

import { errorText, PanelNotice, PanelSkeleton } from "./panel-feedback";
import { FileTypeIcon, SidebarRow } from "./sidebar-row";
import { indentStyle } from "./tree-indent";

type Entry = workspace_fs_svc.EntryView;

/** 一层目录的取数状态。已加载的层按「相对 cwd 的路径」缓存，根是空串。 */
type Level =
  | { status: "loading" }
  | { status: "loaded"; entries: Entry[]; truncated: boolean }
  | { status: "error"; message: string };

const ROOT = "";

type Props = {
  sessionId: number;
  /** 会话工作目录；空串表示这个会话没有工作目录，直接出空态、不打后端。 */
  cwd: string;
  remote: boolean;
  showIgnored: boolean;
};

/**
 * DirectoryView 是「文件」页的「目录」模式：会话工作目录的完整文件树。
 *
 * 树按目录懒加载，每次只列一层（设计决策 6）：大仓一次性递归会遍历几十万文件。
 * 已加载的层与展开集合只存在组件内、不持久化，切换会话时清空（决策 12）；数据是
 * 快照，在「本模式可见且无缓存」与「当前会话轮次结束」两个时机自动重拉，没有手动
 * 刷新按钮（决策 13）。
 *
 * 路径解析、`.git` 恒隐藏与忽略判定全在后端（`WorkspaceFsListDir` 的入参是
 * sessionID 而不是路径，决策 2），本地会话与远端 agentred 会话走同一个绑定。
 */
export function DirectoryView({ sessionId, cwd, remote, showIgnored }: Props) {
  const { t } = useTranslation();
  const [levels, setLevels] = React.useState<Record<string, Level>>({});
  const [expanded, setExpanded] = React.useState<ReadonlySet<string>>(
    () => new Set(),
  );

  // gen 是取数代际：会话 / 忽略开关变化后，先前在途的响应必须丢弃，否则慢的那
  // 一次会把新快照覆盖回旧数据。
  const genRef = React.useRef(0);
  const sessionRef = React.useRef(sessionId);

  // 重取那一步需要「当前展开了哪些层」，但它不能把 expanded 放进依赖里（那样每次
  // 展开都会重取整棵树），所以在这里把最新值镜像到 ref。
  const expandedRef = React.useRef(expanded);
  React.useEffect(() => {
    expandedRef.current = expanded;
  }, [expanded]);

  // doneTick 每次本会话轮次结束自增一次；别的会话结束不会动它。轮次结束是「文件
  // 可能变了」的唯一强信号，快照据此重拉（决策 13）。
  const doneTick = useSessionStatus(sessionId)?.doneTick ?? 0;

  const load = React.useCallback(
    (relPath: string) => {
      const gen = genRef.current;
      setLevels((prev) => ({ ...prev, [relPath]: { status: "loading" } }));
      WorkspaceFsListDir(sessionId, relPath, showIgnored).then(
        (res) => {
          if (genRef.current !== gen) return;
          setLevels((prev) => ({
            ...prev,
            [relPath]: {
              status: "loaded",
              entries: res.entries ?? [],
              truncated: res.truncated,
            },
          }));
        },
        (err: unknown) => {
          if (genRef.current !== gen) return;
          setLevels((prev) => ({
            ...prev,
            [relPath]: { status: "error", message: errorText(err) },
          }));
        },
      );
    },
    [sessionId, showIgnored],
  );

  // 快照失效并重取：换会话时连展开态一起清空；「显示忽略项」变化或本会话轮次结束
  // 时保留展开态，把根与每个已展开的层各重拉一遍（开关会改变后端返回的条目集合，
  // 轮次结束则可能改变任意一层的内容）。
  React.useEffect(() => {
    if (cwd === "") return;
    genRef.current += 1;
    const sameSession = sessionRef.current === sessionId;
    sessionRef.current = sessionId;
    const keep = sameSession ? expandedRef.current : new Set<string>();
    if (!sameSession) setExpanded(keep);
    setLevels({});
    for (const relPath of [ROOT, ...keep]) load(relPath);
  }, [sessionId, showIgnored, cwd, load, doneTick]);

  const toggleDir = (relPath: string) => {
    const isOpen = expanded.has(relPath);
    setExpanded((prev) => {
      const next = new Set(prev);
      if (isOpen) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
    // 首次展开才取数；收起再展开读缓存。读失败的那一层没有缓存可读，收起再展开
    // 就是它的重试手势（这一层不单独放重试按钮，240px 的行里塞不下）。
    const cached = levels[relPath];
    if (!isOpen && (cached === undefined || cached.status === "error")) {
      load(relPath);
    }
  };

  if (cwd === "") {
    return <PanelNotice text={t("chatContext.directory.noCwd")} />;
  }

  const root = levels[ROOT];
  if (root === undefined || root.status === "loading") {
    return <PanelSkeleton label={t("chatContext.directory.loading")} />;
  }
  if (root.status === "error") {
    return (
      <div className="px-3 py-6 text-center text-xs leading-relaxed text-muted-foreground">
        <p>{root.message || t("chatContext.directory.readFailed")}</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2.5 h-7 text-[11px]"
          onClick={() => load(ROOT)}
        >
          {t("chatContext.directory.retry")}
        </Button>
      </div>
    );
  }

  const renderFile = (entry: Entry, relPath: string, depth: number) => (
    <SidebarRow
      key={relPath}
      sessionId={sessionId}
      cwd={cwd}
      remote={remote}
      sourceMode="directory"
      kind="file"
      path={relPath}
      name={entry.name}
      depth={depth}
      title={relPath}
      lead={
        <>
          {/* 与目录 chevron 等宽的槽位，让同级目录名 / 文件名对齐。 */}
          <span className="size-3.5 shrink-0" aria-hidden="true" />
          <FileTypeIcon path={relPath} />
        </>
      }
      testId="directory-row"
      className={entry.gitIgnored ? "opacity-50" : undefined}
      rowData={{ "data-git-ignored": entry.gitIgnored ? "true" : undefined }}
    />
  );

  const renderDir = (entry: Entry, relPath: string, depth: number) => {
    const isOpen = expanded.has(relPath);
    const child = levels[relPath];
    return (
      <div key={relPath} className="flex flex-col">
        <SidebarRow
          sessionId={sessionId}
          cwd={cwd}
          remote={remote}
          sourceMode="directory"
          kind="dir"
          path={relPath}
          name={entry.name}
          depth={depth}
          title={relPath}
          expanded={isOpen}
          onToggle={() => toggleDir(relPath)}
          ariaLabel={
            isOpen
              ? t("chatContext.files.collapseFolder", { name: entry.name })
              : t("chatContext.files.expandFolder", { name: entry.name })
          }
          lead={
            <>
              {child?.status === "loading" ? (
                <Spinner
                  className="size-3.5 shrink-0"
                  aria-label={t("chatContext.directory.loading")}
                />
              ) : isOpen ? (
                <ChevronDown className="size-3.5 shrink-0" aria-hidden="true" />
              ) : (
                <ChevronRight
                  className="size-3.5 shrink-0"
                  aria-hidden="true"
                />
              )}
              <Folder className="size-3.5 shrink-0" aria-hidden="true" />
            </>
          }
          testId="directory-row"
          className={entry.gitIgnored ? "opacity-50" : undefined}
          rowData={{
            "data-git-ignored": entry.gitIgnored ? "true" : undefined,
          }}
        />
        {isOpen ? (
          <div className="flex flex-col">{renderLevel(relPath, depth + 1)}</div>
        ) : null}
      </div>
    );
  };

  // renderLevel 渲染一层已加载的条目。单层读取失败只让这一个节点出错误行，树的
  // 其余部分照常可用。
  const renderLevel = (parentPath: string, depth: number): React.ReactNode => {
    const level = levels[parentPath];
    if (level === undefined || level.status === "loading") return null;
    if (level.status === "error") {
      return (
        <div
          role="alert"
          className="py-1.5 pr-2.5 text-xs text-destructive"
          style={indentStyle(depth)}
        >
          {level.message || t("chatContext.directory.readFailed")}
        </div>
      );
    }
    if (level.entries.length === 0) {
      return (
        <div
          className="py-1.5 pr-2.5 text-xs text-muted-foreground"
          style={indentStyle(depth)}
        >
          {t("chatContext.directory.empty")}
        </div>
      );
    }
    return (
      <>
        {sortEntries(level.entries).map((entry) => {
          const relPath = parentPath
            ? `${parentPath}/${entry.name}`
            : entry.name;
          return entry.isDir
            ? renderDir(entry, relPath, depth)
            : renderFile(entry, relPath, depth);
        })}
        {level.truncated ? (
          // 截断不静默：条目数就是后端这一层的实际上限（后端先过滤忽略项再截断）。
          <div
            className="py-1.5 pr-2.5 text-[11px] text-muted-foreground"
            style={indentStyle(depth)}
          >
            {t("chatContext.directory.truncated", {
              limit: level.entries.length,
            })}
          </div>
        ) : null}
      </>
    );
  };

  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {renderLevel(ROOT, 0)}
    </div>
  );
}

/** 目录在前、各自名称字母序（后端按 os.ReadDir 的文件名序返回，不分目录/文件）。 */
function sortEntries(entries: Entry[]): Entry[] {
  return [...entries].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
}
