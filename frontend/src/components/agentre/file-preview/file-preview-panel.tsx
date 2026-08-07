import * as React from "react";
import { useTranslation } from "react-i18next";

import {
  FileCode,
  FileImage,
  FileText,
  X,
  type LucideIcon,
} from "lucide-react";

import {
  WorkspaceFsGitFileContent,
  WorkspaceFsReadFile,
} from "@/../wailsjs/go/app/App";
import type { workspace_fs_svc } from "@/../wailsjs/go/models";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  useChatSidebarStore,
  type FilePreviewSegment,
} from "@/stores/chat-sidebar-store";
import { useSessionStatus } from "@/stores/session-status-store";

import type { PreviewKind } from "../chat-context-sidebar/previewable";
import { previewKind } from "../chat-context-sidebar/previewable";
import {
  errorText,
  PanelNotice,
  PanelSkeleton,
} from "../chat-context-sidebar/views/panel-feedback";
import { MarkdownText } from "../markdown-text";
import { ResizableSidebar } from "../resizable-sidebar";

import { CodePreview } from "./code-view";
import { DiffPreview } from "./diff-view";
import { MarkdownSourceView } from "./markdown-source-view";

type ReadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "loaded"; view: workspace_fs_svc.ReadFileView };

type GitState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "loaded"; view: workspace_fs_svc.GitFileContentView };

type Props = {
  sessionId: number;
};

function basename(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] ?? path;
}

function dirname(path: string): string {
  const i = path.lastIndexOf("/");
  return i > 0 ? path.slice(0, i + 1) : "";
}

const KIND_ICON: Record<PreviewKind, LucideIcon> = {
  markdown: FileText,
  code: FileCode,
  image: FileImage,
};

/**
 * FilePreviewPanel 是会话「文件」面板的最右一栏预览面板（spec「状态与布局」）：
 * 仅在选中了可预览文件时渲染，可拖拽调宽（ResizableSidebar edge="left"，独立
 * persistenceKey），按文件类型分档（markdown 渲染/文本/双栏、代码文本/对比、
 * 图片无档）。读取走 WorkspaceFsReadFile / WorkspaceFsGitFileContent（会话级
 * relPath，本机 / 远端同一绑定）；本会话轮次结束（doneTick）自动重读刷新。
 */
export function FilePreviewPanel({ sessionId }: Props) {
  const { t } = useTranslation();
  const path = useChatSidebarStore((s) => s.previewBySession[sessionId]?.path);
  const storedSegment = useChatSidebarStore(
    (s) => s.previewBySession[sessionId]?.segment ?? null,
  );
  const setPreviewSegment = useChatSidebarStore((s) => s.setPreviewSegment);
  const clearPreview = useChatSidebarStore((s) => s.clearPreview);

  // 切换会话：关闭面板、清空上一个会话的选中、档位回默认（spec 决策 12）。
  const prevSessionRef = React.useRef(sessionId);
  React.useEffect(() => {
    const prev = prevSessionRef.current;
    if (prev !== sessionId) {
      clearPreview(prev);
      prevSessionRef.current = sessionId;
    }
  }, [sessionId, clearPreview]);

  // 关闭按钮的滑出动画：先本地置 closing 播放 200ms 出场，再清空选中卸载面板。
  const [closing, setClosing] = React.useState(false);
  const closeTimerRef = React.useRef<number | null>(null);
  React.useEffect(
    () => () => {
      if (closeTimerRef.current != null)
        window.clearTimeout(closeTimerRef.current);
    },
    [],
  );
  const handleClose = React.useCallback(() => {
    if (closing) return;
    setClosing(true);
    closeTimerRef.current = window.setTimeout(() => {
      clearPreview(sessionId);
      setClosing(false);
    }, 200);
  }, [closing, clearPreview, sessionId]);

  // 档位是面板状态:切文件保留,关面板 / 切会话回默认。这里把存储的档位按文件
  // 类型钳到合法集合(markdown render/text/split,代码 text/diff,图片无)。
  const kind: PreviewKind | null = path ? previewKind(path) : null;
  const effectiveSegment = React.useMemo(() => {
    if (kind === "image") return null;
    if (kind === "markdown") {
      return storedSegment === "text" || storedSegment === "split"
        ? storedSegment
        : "render";
    }
    return storedSegment === "diff" ? "diff" : "text";
  }, [kind, storedSegment]);

  // doneTick 每次本会话轮次结束自增一次;轮次结束是「文件可能变了」的唯一强信号,
  // 面板开着时据此重读(不读缓存,spec 决策 11)。
  const doneTick = useSessionStatus(sessionId)?.doneTick ?? 0;

  const [readState, setReadState] = React.useState<ReadState>({
    status: "loading",
  });
  const [gitState, setGitState] = React.useState<GitState>({ status: "idle" });
  const [reloadKey, setReloadKey] = React.useState(0);
  const readGenRef = React.useRef(0);
  const gitGenRef = React.useRef(0);

  React.useEffect(() => {
    if (!path) return;
    readGenRef.current += 1;
    const gen = readGenRef.current;
    setReadState({ status: "loading" });
    WorkspaceFsReadFile(sessionId, path).then(
      (view) => {
        if (readGenRef.current !== gen) return;
        setReadState({ status: "loaded", view });
      },
      (err: unknown) => {
        if (readGenRef.current !== gen) return;
        setReadState({ status: "error", message: errorText(err) });
      },
    );
  }, [sessionId, path, doneTick, reloadKey]);

  React.useEffect(() => {
    if (effectiveSegment !== "diff" || !path) {
      setGitState({ status: "idle" });
      return;
    }
    gitGenRef.current += 1;
    const gen = gitGenRef.current;
    setGitState({ status: "loading" });
    WorkspaceFsGitFileContent(sessionId, path).then(
      (view) => {
        if (gitGenRef.current !== gen) return;
        setGitState({ status: "loaded", view });
      },
      (err: unknown) => {
        if (gitGenRef.current !== gen) return;
        setGitState({ status: "error", message: errorText(err) });
      },
    );
  }, [sessionId, path, effectiveSegment, doneTick, reloadKey]);

  if (!path) return null;

  const dir = dirname(path);
  const Icon = KIND_ICON[kind ?? "code"];
  const segments =
    kind === "markdown"
      ? (["render", "text", "split"] as const)
      : kind === "code"
        ? (["text", "diff"] as const)
        : [];
  const segmentLabel = (seg: FilePreviewSegment): string => {
    switch (seg) {
      case "render":
        return t("chatContext.filePreview.segmentRender");
      case "text":
        return t("chatContext.filePreview.segmentText");
      case "split":
        return t("chatContext.filePreview.segmentSplit");
      default:
        return t("chatContext.filePreview.segmentDiff");
    }
  };

  const isLoading =
    readState.status === "loading" ||
    (effectiveSegment === "diff" &&
      (gitState.status === "idle" || gitState.status === "loading"));

  // 正文容器按内容变化重挂载 → 150ms 淡入(motion-reduce 停用);骨架屏不参与。
  const contentKey = `${path}|${effectiveSegment}|${readState.status}|${gitState.status}`;

  return (
    <ResizableSidebar
      persistenceKey="file-preview"
      ariaLabel={t("chatContext.filePreview.panelAria")}
      edge="left"
      defaultWidth={440}
      className={cn(
        "overflow-hidden",
        closing
          ? "animate-out slide-out-to-right-6 duration-200 ease-out motion-reduce:animate-none"
          : "animate-in slide-in-from-right-6 duration-200 ease-out motion-reduce:animate-none",
      )}
    >
      <header
        className="flex h-10 shrink-0 items-center gap-1.5 border-b border-border pl-3 pr-2"
        data-testid="file-preview-header"
      >
        <Icon
          className="size-4 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span
          className="shrink truncate font-mono text-xs font-semibold"
          title={path}
        >
          {basename(path)}
        </span>
        {dir !== "" ? (
          <span
            className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-foreground"
            title={dir}
          >
            {dir}
          </span>
        ) : null}
        {segments.length > 1 && effectiveSegment !== null ? (
          <div
            role="group"
            aria-label={t("chatContext.filePreview.segmentGroup")}
            className="flex shrink-0 items-center rounded-md border border-border p-0.5"
          >
            {segments.map((seg) => (
              <button
                key={seg}
                type="button"
                aria-pressed={effectiveSegment === seg}
                onClick={() => setPreviewSegment(sessionId, seg)}
                className={cn(
                  "rounded px-1.5 py-0.5 text-[10px] transition-colors duration-150",
                  effectiveSegment === seg
                    ? "bg-accent font-semibold text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {segmentLabel(seg)}
              </button>
            ))}
          </div>
        ) : null}
        <button
          type="button"
          aria-label={t("chatContext.filePreview.close")}
          title={t("chatContext.filePreview.close")}
          onClick={handleClose}
          className="ml-0.5 shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:text-foreground"
        >
          <X className="size-4" aria-hidden="true" />
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col">
        {isLoading ? (
          <PanelSkeleton label={t("chatContext.filePreview.loading")} />
        ) : (
          <div
            key={contentKey}
            className="flex min-h-0 flex-1 flex-col animate-in fade-in duration-150 motion-reduce:animate-none"
          >
            <PanelBody
              kind={kind}
              segment={effectiveSegment}
              readState={readState}
              gitState={gitState}
              path={path}
              onRetry={() => setReloadKey((k) => k + 1)}
            />
          </div>
        )}
      </div>
    </ResizableSidebar>
  );
}

function PanelBody({
  kind,
  segment,
  readState,
  gitState,
  path,
  onRetry,
}: {
  kind: PreviewKind | null;
  segment: FilePreviewSegment | null;
  readState: ReadState;
  gitState: GitState;
  path: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation();

  if (readState.status === "error") {
    return (
      <PreviewMessage
        text={readState.message || t("chatContext.filePreview.readFailed")}
        onRetry={onRetry}
      />
    );
  }
  if (readState.status === "loading") return null;

  const view = readState.view;
  if (view.binary) {
    return (
      <PreviewMessage
        text={t("chatContext.filePreview.binary")}
        hint={t("chatContext.filePreview.binaryHint")}
        onRetry={onRetry}
      />
    );
  }
  if (view.tooLarge) {
    return (
      <PreviewMessage
        text={t("chatContext.filePreview.tooLarge")}
        hint={
          kind === "image"
            ? t("chatContext.filePreview.tooLargeImageHint")
            : t("chatContext.filePreview.tooLargeTextHint")
        }
        onRetry={onRetry}
      />
    );
  }

  if (kind === "image") {
    return (
      <div
        className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-4"
        style={{
          // 棋盘格底:衬托透明图(png)。
          backgroundImage:
            "repeating-conic-gradient(var(--muted) 0% 25%, transparent 0% 50%)",
          backgroundSize: "16px 16px",
        }}
      >
        <img
          src={`data:${view.contentType};base64,${view.content}`}
          alt={basename(path)}
          className="max-h-full max-w-full rounded-md object-contain shadow-sm"
        />
      </div>
    );
  }

  if (kind === "markdown" && segment === "split") {
    return (
      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto border-r border-border">
          <MarkdownSourceView
            value={view.content}
            path={path}
            ariaLabel={t("chatContext.filePreview.sourceAria", {
              name: basename(path),
            })}
            className="h-full"
          />
        </div>
        <div className="min-w-0 flex-1 overflow-auto px-4 py-3">
          <MarkdownText text={view.content} />
        </div>
      </div>
    );
  }

  if (segment === "diff") {
    if (gitState.status === "error") {
      return (
        <PreviewMessage
          text={gitState.message || t("chatContext.filePreview.readFailed")}
          onRetry={onRetry}
        />
      );
    }
    if (gitState.status !== "loaded") return null;
    if (gitState.view.notARepo) {
      return (
        <PreviewMessage
          text={t("chatContext.filePreview.noGitBaseline")}
          hint={t("chatContext.filePreview.noGitBaselineHint")}
          onRetry={onRetry}
        />
      );
    }
    return (
      <>
        <div className="flex h-8 shrink-0 items-center gap-2 border-b border-border bg-muted px-3 text-[10px] text-muted-foreground">
          <span className="font-mono">
            {t("chatContext.filePreview.diffHeader")}
          </span>
          <span className="ml-auto inline-flex items-center gap-3">
            <span className="inline-flex items-center gap-1">
              <span
                aria-hidden="true"
                className="size-2 rounded-[2px] bg-status-running/30"
              />
              {t("chatContext.filePreview.diffLegendAdded")}
            </span>
            <span className="inline-flex items-center gap-1">
              <span
                aria-hidden="true"
                className="size-2 rounded-[2px] bg-destructive/30"
              />
              {t("chatContext.filePreview.diffLegendDeleted")}
            </span>
          </span>
        </div>
        <DiffPreview
          original={gitState.view.content}
          modified={view.content}
          path={path}
          ariaLabel={t("chatContext.filePreview.diffAria", {
            name: basename(path),
          })}
          className="min-h-0 flex-1"
        />
      </>
    );
  }

  if (kind === "markdown" && segment === "render") {
    return (
      <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
        <MarkdownText text={view.content} />
      </div>
    );
  }

  if (kind === "markdown") {
    // markdown 文本档:原始源码,Monaco 只读(markdown 语言)。
    return (
      <MarkdownSourceView
        value={view.content}
        path={path}
        ariaLabel={t("chatContext.filePreview.sourceAria", {
          name: basename(path),
        })}
        className="min-h-0 flex-1"
      />
    );
  }

  // 代码 / 文本 文本档:Monaco 只读,按扩展名语言高亮。
  return (
    <CodePreview
      value={view.content}
      path={path}
      ariaLabel={t("chatContext.filePreview.codeAria", {
        name: basename(path),
      })}
      className="min-h-0 flex-1"
    />
  );
}

/** 面板内错误 / 说明态:复用 panel-feedback 的 PanelNotice + 一个重试按钮。 */
function PreviewMessage({
  text,
  hint,
  onRetry,
}: {
  text: string;
  hint?: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center px-4 pb-4">
      <PanelNotice text={text} hint={hint} />
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 text-[11px]"
        onClick={onRetry}
      >
        {t("chatContext.filePreview.retry")}
      </Button>
    </div>
  );
}
