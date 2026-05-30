import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronRight, Copy, LoaderCircle, Pencil } from "lucide-react";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

import { statusConfig } from "../../types";
import type { CanonicalCardProps } from "../props";
import type { CanonicalDTO } from "../types";

// FileWriteCard 渲染 canonical.file.write —— 全量写入文件。
// 来源:claudecode Write{file_path, content} / codex fileChange{Changes[].Kind=created}。
export const FileWriteCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
}) => {
  const { t } = useTranslation();
  const canonical = (toolBlock as { canonical?: CanonicalDTO }).canonical;
  const [expanded, setExpanded] = React.useState(false);

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
    <section
      data-testid="file-write-card"
      aria-label={t("canonical.fileWrite.aria")}
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
          {t("canonical.fileWrite.title")}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="min-w-0 truncate text-muted-foreground">{path}</span>
        <span className="rounded-sm bg-status-running-bg px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em] text-status-running">
          {t("canonical.fileWrite.newBadge")}
        </span>
        {w.lines > 0 && (
          <span className="font-semibold text-status-running">+{w.lines}</span>
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
        <div className="border-t border-border py-2">
          {w.content === "" ? (
            <div className="px-3 py-1 text-muted-foreground">
              {t("canonical.fileWrite.empty")}
            </div>
          ) : (
            w.content.split("\n").map((text, i) => (
              <div key={i} className="flex items-center px-3 py-0.5">
                <span className="w-8 text-right text-[11px] text-subtle-foreground">
                  {i + 1}
                </span>
                <span
                  className="w-5 text-center text-[11px] text-subtle-foreground"
                  aria-hidden="true"
                >
                  {" "}
                </span>
                <span className="whitespace-pre text-foreground">{text}</span>
              </div>
            ))
          )}
          {w.truncated && <TruncatedBar content={w.content} lines={w.lines} />}
        </div>
      )}
    </section>
  );
};

function TruncatedBar({ content, lines }: { content: string; lines: number }) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = React.useState<
    "copied" | "failed" | "idle"
  >("idle");
  const resetTimerRef = React.useRef<number | null>(null);

  React.useEffect(() => {
    return () => {
      if (resetTimerRef.current !== null) {
        window.clearTimeout(resetTimerRef.current);
      }
    };
  }, []);

  async function handleCopy() {
    if (resetTimerRef.current !== null) {
      window.clearTimeout(resetTimerRef.current);
      resetTimerRef.current = null;
    }
    try {
      const copied = await copyTextWithToast(content, {
        errorTitle: t("canonical.fileWrite.copyFailed"),
        successTitle: t("canonical.fileWrite.copyDone"),
      });
      setCopyState(copied ? "copied" : "failed");
    } catch {
      setCopyState("failed");
    }
    resetTimerRef.current = window.setTimeout(() => {
      setCopyState("idle");
      resetTimerRef.current = null;
    }, 1400);
  }

  return (
    <div className="mt-1 flex items-center gap-2 border-t border-border px-3 py-1">
      <span className="text-[11px] text-muted-foreground">
        {t("canonical.fileWrite.truncated", { lines })}
      </span>
      <span className="ml-auto" />
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
        onClick={() => void handleCopy()}
      >
        <Copy data-icon="inline-start" aria-hidden="true" />
        {copyState === "copied"
          ? t("common.copied")
          : copyState === "failed"
            ? t("common.copyFailed")
            : t("canonical.fileWrite.copyFull")}
      </Button>
    </div>
  );
}

function relativize(path: string, cwd?: string): string {
  if (!cwd) return path;
  const trimmed = cwd.replace(/\/+$/, "");
  if (path === trimmed) return "./";
  if (path.startsWith(trimmed + "/"))
    return "./" + path.slice(trimmed.length + 1);
  return path;
}
