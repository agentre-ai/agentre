import { useTranslation } from "react-i18next";
import { SquareTerminal } from "lucide-react";

import { Button } from "@/components/ui/button";

import { TerminalClose } from "../../../../wailsjs/go/app/App";
import { useLocalCommandsStore } from "../../../stores/local-commands-store";
import type { LocalCommandStatus } from "../../../stores/local-commands-store";
import { stripAnsi } from "./ansi";

// Status → visual style map (DRY — one place for all status styles).
const STATUS_CONFIG: Record<
  LocalCommandStatus,
  { dot: string; pill: string; labelKey: string }
> = {
  running: {
    dot: "bg-amber-500",
    pill: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    labelKey: "localCommand.status.running",
  },
  done: {
    dot: "bg-emerald-500",
    pill: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
    labelKey: "localCommand.status.done",
  },
  failed: {
    dot: "bg-destructive",
    pill: "bg-destructive/15 text-destructive",
    labelKey: "localCommand.status.failed",
  },
  stopped: {
    dot: "bg-muted-foreground",
    pill: "bg-muted text-muted-foreground",
    labelKey: "localCommand.status.stopped",
  },
};

export function LocalCommandCard({
  entryId,
  onOpenInTerminal,
}: {
  entryId: string;
  onOpenInTerminal: (id: string) => void;
}) {
  const { t } = useTranslation();
  const entry = useLocalCommandsStore((s) => s.entries[entryId]);

  if (!entry) return null;

  const cfg = STATUS_CONFIG[entry.status];
  const isRunning = entry.status === "running";
  const showExitCode = entry.status !== "running" && entry.exitCode !== undefined;

  return (
    <div className="rounded-lg border border-border bg-card text-foreground shadow-sm">
      {/* Header */}
      <div className="flex items-center gap-2 border-b border-border px-3.5 py-2.5">
        <SquareTerminal className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />

        {/* "本地命令" chip */}
        <span className="rounded-sm border border-border bg-muted px-1.5 py-0.5 text-2xs font-semibold text-muted-foreground">
          {t("localCommand.localChip")}
        </span>

        {/* Command */}
        <span className="font-mono text-xs font-semibold text-foreground">
          {entry.command}
        </span>

        <div className="flex-1" />

        {/* Not shared with AI marker */}
        <span className="text-2xs text-muted-foreground/70">
          {t("localCommand.notSharedWithAI")}
        </span>

        {/* Status pill */}
        <span
          className={`flex items-center gap-1.5 rounded-sm px-1.5 py-0.5 text-2xs font-semibold tracking-wider ${cfg.pill}`}
        >
          <span className={`h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
          {t(cfg.labelKey)}
          {showExitCode && (
            <>
              <span className="opacity-50">·</span>
              {t("localCommand.exitCode", { code: entry.exitCode })}
            </>
          )}
        </span>
      </div>

      {/* Output area */}
      <div className="max-h-48 overflow-y-auto bg-code-surface px-3.5 py-2.5">
        <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-code-foreground">
          {stripAnsi(entry.output)}
        </pre>
      </div>

      {/* Actions — only while running */}
      {isRunning && (
        <div className="flex items-center gap-2 border-t border-border px-3.5 py-2.5">
          <div className="flex-1" />
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => void TerminalClose(entryId)}
          >
            {t("localCommand.stop")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => onOpenInTerminal(entryId)}
          >
            {t("localCommand.openInTerminal")}
          </Button>
        </div>
      )}
    </div>
  );
}
