import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronRight, LoaderCircle, Pencil } from "lucide-react";

import { cn } from "@/lib/utils";

import {
  TranscriptCard,
  TranscriptCardHeader,
  TranscriptPill,
} from "../../transcript-card";
import { statusConfig } from "../../types";
import { useTranscriptBooleanState } from "../../transcript-ui-state";
import type { CanonicalCardProps } from "../props";
import type { CanonicalDTO } from "../types";

import { FileBlock } from "./hunk-renderer";

// FileEditCard 渲染 canonical.file.edit —— 局部修改 + 删除 + 多文件 diff。
// 来源:claudecode Edit/MultiEdit / codex fileChange{Kind in {modified,deleted}}。
export const FileEditCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
  uiStateKey,
}) => {
  const { t } = useTranslation();
  const canonical = (toolBlock as { canonical?: CanonicalDTO }).canonical;
  const [expanded, setExpanded] = useTranscriptBooleanState(uiStateKey, false);

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
    <TranscriptCard
      data-testid="file-edit-card"
      aria-label={t("canonical.fileEdit.aria", { tool: toolBlock.toolName })}
      tone={isError ? "error" : "default"}
      className="font-mono text-aux"
    >
      <TranscriptCardHeader
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
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
          <TranscriptPill>replace_all</TranscriptPill>
        )}
        <span className="min-w-0 flex-1" />
        <TranscriptPill className={pillConfig.pillClassName}>
          <StatusIcon
            className={cn("size-2.5", !hasResult && !isError && "animate-spin")}
            aria-hidden="true"
          />
          {statusLabel}
        </TranscriptPill>
      </TranscriptCardHeader>
      {expanded && (
        <div className="border-t border-border">
          {files.map((file, fi) => (
            <FileBlock key={fi} file={file} showHeader={isMulti} />
          ))}
        </div>
      )}
    </TranscriptCard>
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
