import * as React from "react";
import { FileText, Image as ImageIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  previewKind,
  toRelPath,
} from "@/components/agentre/chat-context-sidebar/previewable";
import { OpenPath, WorkspaceFsReadFile } from "@/../wailsjs/go/app/App";

const ABS_POSIX = /^\//;
const ABS_WINDOWS = /^[A-Za-z]:[\\/]/;
const FILE_PROTOCOL = /^file:\/\//i;
const SCHEME = /^[A-Za-z][A-Za-z0-9+.-]*:/;
const HTTP_PROTOCOL = /^https?:/i;
const WWW_PREFIX = /^www\./i;

function decodeLocalPath(path: string): string {
  try {
    return decodeURIComponent(path);
  } catch {
    return path;
  }
}

function fileURLToPath(href: string): string {
  // file:///Users/x/a.png → /Users/x/a.png
  // file:///C:/Users/x/a.png → C:/Users/x/a.png
  let path = href.slice("file://".length);
  if (path.startsWith("/") && /^[A-Za-z]:/.test(path.slice(1))) {
    path = path.slice(1);
  }
  return decodeLocalPath(path);
}

function basenameOf(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const idx = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

function toSlash(path: string): string {
  return path.replace(/\\/g, "/");
}

// 相对路径是否仍落在 cwd 内(不因 ".." 越界)。后端 workspacefs.ReadFile 是权威
// 边界,这里只做 UX 预判:越界的路径既不该读(fetch/fallback 分档),也不可点。
function isRelPathInside(relPath: string): boolean {
  const parts = relPath.split("/");
  let depth = 0;
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      depth -= 1;
      if (depth < 0) return false;
    } else {
      depth += 1;
    }
  }
  return true;
}

export type MarkdownImageClass =
  | { kind: "remote"; src: string }
  | { kind: "plain"; src: string | undefined }
  | {
      kind: "fetch";
      relPath: string;
      absolutePath: string | null;
      basename: string;
    }
  | { kind: "fallback"; absolutePath: string | null; basename: string };

/**
 * 把 markdown 图片的 src 分成四档:remote(原样 <img>)/ plain(维持现状的 <img>,
 * 相对本地路径剥空 src)/ fetch(本地路径 + sessionId + cwd 内 + 图片扩展名,读文件
 * 渲染 data URL)/ fallback(本地路径但不可预览,渲染兜底 chip)。
 */
export function classifyMarkdownImage(
  src: string,
  opts: { cwd?: string; sessionId?: number },
): MarkdownImageClass {
  if (src === "") return { kind: "plain", src: undefined };

  if (HTTP_PROTOCOL.test(src) || WWW_PREFIX.test(src)) {
    return { kind: "remote", src };
  }

  let localPath: string | null = null;
  if (FILE_PROTOCOL.test(src)) {
    localPath = fileURLToPath(src);
  } else if (ABS_POSIX.test(src) || ABS_WINDOWS.test(src)) {
    localPath = decodeLocalPath(src);
  } else if (!SCHEME.test(src)) {
    localPath = decodeLocalPath(src);
  }

  if (localPath !== null) {
    const basename = basenameOf(localPath);
    // 无会话上下文时本地图片维持现状:相对路径剥空(今天被 urlTransform 剥掉);
    // 绝对/file:// 路径原样保留 src(今天的破图行为不变)。
    if (opts.sessionId === undefined) {
      const relative =
        !ABS_POSIX.test(src) &&
        !ABS_WINDOWS.test(src) &&
        !FILE_PROTOCOL.test(src);
      return { kind: "plain", src: relative ? undefined : src };
    }
    const resolved = resolveLocalPath(localPath, opts.cwd);
    if (
      resolved.relPath !== null &&
      resolved.absolutePath !== null &&
      previewKind(localPath) === "image"
    ) {
      return {
        kind: "fetch",
        relPath: resolved.relPath,
        absolutePath: resolved.absolutePath,
        basename,
      };
    }
    return { kind: "fallback", absolutePath: resolved.absolutePath, basename };
  }

  if (SCHEME.test(src)) {
    if (/^(data|javascript):/i.test(src)) {
      return { kind: "plain", src: undefined };
    }
    return { kind: "plain", src };
  }

  return { kind: "plain", src };
}

function resolveLocalPath(
  path: string,
  cwd: string | undefined,
): { relPath: string | null; absolutePath: string | null } {
  if (!cwd) return { relPath: null, absolutePath: null };
  const relPath = toRelPath(path, cwd);
  if (ABS_POSIX.test(path) || ABS_WINDOWS.test(path)) {
    // 绝对路径:落在 cwd 内(toRelPath 成功)且剥出的 relPath 不含越界 ".."
    // 才可点;越界 ".." 与相对路径同一套判定,不读也不可点。
    if (relPath !== null && isRelPathInside(relPath)) {
      return { relPath, absolutePath: path };
    }
    return { relPath: null, absolutePath: null };
  }
  const slash = toSlash(path);
  if (!isRelPathInside(slash)) return { relPath: null, absolutePath: null };
  const sep = cwd.includes("\\") ? "\\" : "/";
  const base = cwd.endsWith("/") || cwd.endsWith("\\") ? cwd : `${cwd}${sep}`;
  return { relPath, absolutePath: base + slash };
}

type FetchState =
  | { status: "loading" }
  | { status: "loaded"; dataUrl: string }
  | { status: "failed"; reason: "cannot-preview" | "too-large" };

function FetchImage({
  relPath,
  absolutePath,
  basename,
  alt,
  sessionId,
}: {
  relPath: string;
  absolutePath: string | null;
  basename: string;
  alt?: string;
  sessionId?: number;
}) {
  const [state, setState] = React.useState<FetchState>({ status: "loading" });

  React.useEffect(() => {
    if (sessionId === undefined) {
      setState({ status: "failed", reason: "cannot-preview" });
      return;
    }
    let cancelled = false;
    WorkspaceFsReadFile(sessionId, relPath)
      .then((view) => {
        if (cancelled) return;
        if (view.tooLarge) {
          setState({ status: "failed", reason: "too-large" });
          return;
        }
        if (view.binary || !view.contentType) {
          setState({ status: "failed", reason: "cannot-preview" });
          return;
        }
        setState({
          status: "loaded",
          dataUrl: `data:${view.contentType};base64,${view.content}`,
        });
      })
      .catch(() => {
        if (!cancelled)
          setState({ status: "failed", reason: "cannot-preview" });
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, relPath]);

  if (state.status === "loading") {
    return (
      <span
        className="inline-flex size-8 items-center justify-center rounded bg-muted align-middle"
        aria-hidden
      >
        <ImageIcon className="size-4 text-muted-foreground" aria-hidden />
      </span>
    );
  }
  if (state.status === "loaded") {
    return (
      <img src={state.dataUrl} alt={alt} className="max-w-full rounded-lg" />
    );
  }
  return (
    <FallbackChip
      absolutePath={absolutePath}
      basename={basename}
      reason={state.reason}
    />
  );
}

function FallbackChip({
  absolutePath,
  basename,
  reason,
}: {
  absolutePath: string | null;
  basename: string;
  reason: "cannot-preview" | "too-large";
}) {
  const { t } = useTranslation();
  const label =
    reason === "too-large"
      ? t("markdownImage.tooLarge")
      : t("markdownImage.cannotPreview");

  const content = (
    <>
      <FileText className="size-3 shrink-0" aria-hidden />
      <span className="min-w-0 truncate">{basename}</span>
      <span aria-hidden>·</span>
      <span className="shrink-0">{label}</span>
    </>
  );

  if (absolutePath !== null) {
    return (
      <button
        type="button"
        className="inline-flex max-w-full items-center gap-1 rounded border border-border bg-muted px-1.5 py-0.5 text-meta text-muted-foreground outline-none transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50"
        onClick={() => OpenPath(absolutePath).catch(() => {})}
      >
        {content}
      </button>
    );
  }
  return (
    <span className="inline-flex max-w-full items-center gap-1 rounded border border-border bg-muted px-1.5 py-0.5 text-meta text-muted-foreground">
      {content}
    </span>
  );
}

export function MarkdownImage({
  src,
  alt,
  cwd,
  sessionId,
}: {
  src?: string;
  alt?: string;
  cwd?: string;
  sessionId?: number;
}) {
  const cls = classifyMarkdownImage(src ?? "", { cwd, sessionId });

  if (cls.kind === "remote") {
    return <img src={cls.src} alt={alt} />;
  }
  if (cls.kind === "plain") {
    return <img src={cls.src} alt={alt} />;
  }
  if (cls.kind === "fetch") {
    return (
      <FetchImage
        relPath={cls.relPath}
        absolutePath={cls.absolutePath}
        basename={cls.basename}
        alt={alt}
        sessionId={sessionId}
      />
    );
  }
  return (
    <FallbackChip
      absolutePath={cls.absolutePath}
      basename={cls.basename}
      reason="cannot-preview"
    />
  );
}
