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

import { FileWriteContent } from "./content-renderer";

// FileWriteCard 渲染 canonical.file.write —— 全量写入文件。
// 来源:claudecode Write{file_path, content} / codex fileChange{Changes[].Kind=created}。
export const FileWriteCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
  uiStateKey,
}) => {
  const { t } = useTranslation();
  const canonical = (toolBlock as { canonical?: CanonicalDTO }).canonical;
  const [expanded, setExpanded] = useTranscriptBooleanState(uiStateKey, false);

  if (!canonical || canonical.kind !== "file.write") return null;
  const w = canonical.fileWrite;

  const path = relativize(w.path, cwd);
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
      data-testid="file-write-card"
      aria-label={t("canonical.fileWrite.aria")}
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
          {t("canonical.fileWrite.title")}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="min-w-0 truncate text-muted-foreground">{path}</span>
        <TranscriptPill tone="done">
          {t("canonical.fileWrite.newBadge")}
        </TranscriptPill>
        {w.lines > 0 && (
          <span className="font-semibold text-status-running">+{w.lines}</span>
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
        <div className="border-t border-border py-2">
          <FileWriteContent write={w} />
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
