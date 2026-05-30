import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronRight, LoaderCircle, Pencil } from "lucide-react";

import { cn } from "@/lib/utils";

import { statusConfig } from "../../types";
import type { CanonicalCardProps } from "../props";
import type { CanonicalDTO } from "../types";

import { FileBlock } from "./hunk-renderer";

// FileEditCard 渲染 canonical.file.edit —— 局部修改 + 删除 + 多文件 diff。
// 来源:claudecode Edit/MultiEdit / codex fileChange{Kind in {modified,deleted}}。
export const FileEditCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
}) => {
  const { t } = useTranslation();
  const canonical = (toolBlock as { canonical?: CanonicalDTO }).canonical;
  const [expanded, setExpanded] = React.useState(false);

  if (!canonical || canonical.kind !== "file.edit") return null;
  const files = canonical.fileEdit.files;
  if (files.length === 0) return null;

  const isMulti = files.length > 1;
  const totalPlus = files.reduce((a, f) => a + (f.plus ?? 0), 0);
  const totalMinus = files.reduce((a, f) => a + (f.minus ?? 0), 0);
  const headerPath = isMulti
    ? t("canonical.fileEdit.fileCount", { count: files.length })
    : relativize(files[0].path, cwd);

  const hasResult = !!resultBlock;
  const isError = !!resultBlock?.isError;
  const status = isError ? "error" : "running";
  const statusLabel = isError
    ? t("canonical.status.error")
    : hasResult
      ? t("canonical.status.done")
      : t("canonical.status.running");
  const pillConfig = statusConfig[status];
  const StatusIcon = hasResult || isError ? Check : LoaderCircle;

  return (
    <section
      data-testid="file-edit-card"
      aria-label={t("canonical.fileEdit.aria", { tool: toolBlock.toolName })}
      className={cn(
        "w-full max-w-[720px] overflow-hidden rounded-md border bg-card font-mono text-xs",
        isError ? "border-status-error/40" : "border-border",
      )}
    >
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/40"
      >
        <ChevronRight
          className={cn(
            "size-3 shrink-0 text-muted-foreground transition-transform",
            expanded && "rotate-90",
          )}
          aria-hidden="true"
        />
        <Pencil
          className="size-3.5 shrink-0 text-primary-text"
          aria-hidden="true"
        />
        <span className="shrink-0 font-semibold text-primary-text">
          {toolBlock.toolName}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="min-w-0 truncate text-muted-foreground">
          {headerPath}
        </span>
        {totalPlus > 0 && (
          <span className="ml-1 font-semibold text-status-running">
            +{totalPlus}
          </span>
        )}
        {totalMinus > 0 && (
          <span className="font-semibold text-destructive">−{totalMinus}</span>
        )}
        {files.some((f) => f.replaceAll) && (
          <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em] text-muted-foreground">
            replace_all
          </span>
        )}
        <span className="min-w-0 flex-1" />
        <span
          className={cn(
            "inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em]",
            pillConfig.pillClassName,
          )}
        >
          <StatusIcon
            className={cn("size-2.5", !hasResult && !isError && "animate-spin")}
            aria-hidden="true"
          />
          {statusLabel}
        </span>
      </button>
      {expanded && (
        <div className="border-t border-border">
          {files.map((file, fi) => (
            <FileBlock key={fi} file={file} showHeader={isMulti} />
          ))}
        </div>
      )}
    </section>
  );
};

function relativize(path: string, cwd?: string): string {
  if (!cwd) return path;
  const trimmed = cwd.replace(/\/+$/, "");
  if (path === trimmed) return "./";
  if (path.startsWith(trimmed + "/"))
    return "./" + path.slice(trimmed.length + 1);
  return path;
}
