// frontend/src/components/agentre/rich-link.tsx
import {
  Copy as CopyIcon,
  ExternalLink,
  FileText,
  Folder,
  Link as LinkIcon,
  MousePointerClick,
} from "lucide-react";
import * as React from "react";
import { toast } from "sonner";

import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { cn } from "@/lib/utils";
import { classifyLink, type LinkClass } from "@/lib/link-classify";

import { BrowserOpenURL } from "@/../wailsjs/runtime/runtime";
import { OpenPath } from "@/../wailsjs/go/app/App";

const HOVER_OPEN_DELAY_MS = 200;
const HOVER_CLOSE_DELAY_MS = 200;

type RichLinkProps = {
  href?: string;
  className?: string;
  cwd?: string;
  children: React.ReactNode;
};

function lineColSuffix(c: { line?: number; col?: number }): string {
  if (c.line === undefined) return "";
  if (c.col === undefined) return `:${c.line}`;
  return `:${c.line}:${c.col}`;
}

function fullTarget(kind: LinkClass): string {
  switch (kind.kind) {
    case "url":
      return kind.url;
    case "local-internal":
    case "local-external":
      return kind.fullPath + lineColSuffix(kind);
    case "unknown":
      return kind.href;
  }
}

function PathKindIcon({
  pathKind,
}: {
  pathKind: Extract<LinkClass, { kind: "local-internal" }>["pathKind"];
}) {
  const props = {
    "data-testid": "rich-link-path-icon",
    "data-path-kind": pathKind,
    className: "inline-block size-3 shrink-0 align-text-bottom",
    "aria-hidden": true,
  } as const;
  return pathKind === "folder" ? (
    <Folder {...props} />
  ) : (
    <FileText {...props} />
  );
}

function OpenLinkIcon({
  kind,
}: {
  kind: Exclude<LinkClass["kind"], "unknown">;
}) {
  return (
    <ExternalLink
      data-testid="rich-link-open-icon"
      data-link-kind={kind}
      className="inline-block size-3 shrink-0 align-text-bottom opacity-70"
      aria-hidden
    />
  );
}

async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success("已复制", { duration: 2000, position: "bottom-right" });
  } catch {
    toast.error("复制失败");
  }
}

function dispatchClick(kind: LinkClass) {
  switch (kind.kind) {
    case "url":
      BrowserOpenURL(kind.url);
      return;
    case "local-internal":
    case "local-external":
      OpenPath(fullTarget(kind)).catch((err: unknown) => {
        toast.error(
          `打开失败: ${err instanceof Error ? err.message : String(err)}`,
        );
      });
      return;
    case "unknown":
      // 不拦截，让浏览器走默认行为（target=_blank fallback）。
      return;
  }
}

function URLPopover({ kind }: { kind: Extract<LinkClass, { kind: "url" }> }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-[10px] font-semibold text-primary-foreground">
          <LinkIcon className="size-3" aria-hidden /> URL
        </span>
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(kind.url)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <code className="break-all font-mono text-xs text-foreground">
        {kind.url}
      </code>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击在系统浏览器中打开
      </div>
    </div>
  );
}

function LineChip({ line, col }: { line?: number; col?: number }) {
  if (line === undefined) return null;
  return (
    <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 font-mono text-[10px]">
      L{line}
      {col !== undefined ? `:${col}` : ""}
    </span>
  );
}

function LocalInternalPopover({
  kind,
  cwd,
}: {
  kind: Extract<LinkClass, { kind: "local-internal" }>;
  cwd: string;
}) {
  const full = fullTarget(kind);
  const PathIcon = kind.pathKind === "folder" ? Folder : FileText;
  const label = kind.pathKind === "folder" ? "本地文件夹" : "本地文件";
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-agent-2 px-2 py-0.5 text-[10px] font-semibold text-primary-foreground">
          <PathIcon className="size-3" aria-hidden /> {label}
        </span>
        <LineChip line={kind.line} col={kind.col} />
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(full)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <div className="flex flex-col gap-0.5 rounded-md bg-secondary px-2.5 py-1.5">
        <div className="flex items-baseline gap-2">
          <span className="w-12 shrink-0 text-[10px] font-semibold text-muted-foreground">
            项目根
          </span>
          <code className="min-w-0 flex-1 break-all whitespace-normal font-mono text-xs text-muted-foreground">
            {cwd}
          </code>
        </div>
        <div className="flex items-baseline gap-2">
          <span className="w-12 shrink-0 text-[10px] font-semibold text-muted-foreground">
            相对
          </span>
          <code className="min-w-0 flex-1 break-all whitespace-normal font-mono text-xs font-semibold text-foreground">
            {kind.relPath}
          </code>
        </div>
      </div>
      <code className="break-all font-mono text-[11px] text-muted-foreground">
        {full}
      </code>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击用系统默认应用打开
      </div>
    </div>
  );
}

function LocalExternalPopover({
  kind,
}: {
  kind: Extract<LinkClass, { kind: "local-external" }>;
}) {
  const full = fullTarget(kind);
  const PathIcon = kind.pathKind === "folder" ? Folder : FileText;
  const label =
    kind.pathKind === "folder" ? "本地文件夹 · 项目外" : "本地文件 · 项目外";
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-muted-foreground px-2 py-0.5 text-[10px] font-semibold text-background">
          <PathIcon className="size-3" aria-hidden /> {label}
        </span>
        <LineChip line={kind.line} col={kind.col} />
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(full)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <code className="break-all font-mono text-xs font-semibold text-foreground">
        {full}
      </code>
      <div className="text-[11px] text-muted-foreground">
        路径不在当前 cwd 之下，不展示项目根分段。
      </div>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击用系统默认应用打开
      </div>
    </div>
  );
}

export function RichLink({ href, className, cwd, children }: RichLinkProps) {
  const kind = React.useMemo(() => classifyLink(href, cwd), [href, cwd]);

  if (kind.kind === "unknown") {
    // unknown 一律走原有 anchor 行为（target=_blank 兜底，不挂 popover）
    return (
      <a
        href={kind.href || undefined}
        className={cn(
          "text-primary-text underline underline-offset-2 hover:opacity-80",
          className,
        )}
        target="_blank"
        rel="noreferrer noopener"
      >
        {children}
      </a>
    );
  }

  const onClick = (e: React.MouseEvent) => {
    e.preventDefault();
    dispatchClick(kind);
  };

  return (
    <HoverCard
      openDelay={HOVER_OPEN_DELAY_MS}
      closeDelay={HOVER_CLOSE_DELAY_MS}
    >
      <HoverCardTrigger asChild>
        <a
          href={fullTarget(kind)}
          className={cn(
            "inline-flex items-baseline gap-1 text-primary-text underline underline-offset-2 hover:opacity-80",
            className,
          )}
          onClick={onClick}
          rel="noreferrer noopener"
        >
          {kind.kind === "local-internal" || kind.kind === "local-external" ? (
            <PathKindIcon pathKind={kind.pathKind} />
          ) : null}
          {children}
          <OpenLinkIcon kind={kind.kind} />
        </a>
      </HoverCardTrigger>
      <HoverCardContent className="w-[min(28rem,calc(100vw-2rem))]">
        {kind.kind === "url" ? (
          <URLPopover kind={kind} />
        ) : kind.kind === "local-internal" ? (
          <LocalInternalPopover kind={kind} cwd={cwd ?? ""} />
        ) : (
          <LocalExternalPopover kind={kind} />
        )}
      </HoverCardContent>
    </HoverCard>
  );
}
