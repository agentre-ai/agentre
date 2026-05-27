// frontend/src/components/agentre/chat-tabs/tab-tooltip.tsx
import * as React from "react";
import { Folder } from "lucide-react";

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { relativeTime } from "@/lib/relative-time";

type Props = {
  title: string;
  projectChain: string[] | null;
  projectColor: string | null;
  status: "idle" | "running" | "waiting" | "error";
  sessionId: number;
  worktreeBranch: string | null;
  keyboardIndex: number | null;
  /** 最近一次消息时间, ms epoch; 0/未知时跳过 "X ago" 段。*/
  lastMessageAt?: number;
  /** hover 延迟,默认 600ms;测试可传 0 跳过等待。 */
  delayDuration?: number;
  children: React.ReactNode;
};

const statusLabel: Record<Props["status"], { color: string; text: string }> = {
  idle: { color: "bg-muted-foreground", text: "idle" },
  running: { color: "bg-status-running", text: "running" },
  waiting: { color: "bg-status-waiting", text: "waiting" },
  error: { color: "bg-destructive", text: "error" },
};

export function TabTooltip({
  title,
  projectChain,
  projectColor,
  status,
  sessionId,
  worktreeBranch,
  keyboardIndex,
  lastMessageAt,
  delayDuration = 600,
  children,
}: Props) {
  const sLabel = statusLabel[status];
  const ago =
    lastMessageAt && lastMessageAt > 0 ? relativeTime(lastMessageAt) : "";
  const meta = [
    sLabel.text,
    ago ? `${ago} ago` : null,
    `sess-${sessionId}`,
    worktreeBranch ? `worktree ${worktreeBranch}` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <TooltipProvider delayDuration={delayDuration} skipDelayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>{children}</TooltipTrigger>
        <TooltipContent
          side="bottom"
          sideOffset={6}
          align="start"
          className="w-72 p-3"
        >
          {projectChain && projectChain.length > 0 ? (
            <div className="mb-2 flex items-center gap-1.5 text-xs">
              <Folder
                data-testid="tooltip-folder-icon"
                className="size-3"
                style={projectColor ? { color: projectColor } : undefined}
                aria-hidden="true"
              />
              <span className="truncate font-semibold text-foreground">
                {projectChain.join(" / ")}
              </span>
            </div>
          ) : null}
          <div className="mb-1 truncate text-xs font-medium text-foreground">
            {title}
          </div>
          <div className="flex items-center gap-1.5 truncate font-mono text-2xs text-muted-foreground">
            <span
              className={`size-1.5 shrink-0 rounded-full ${sLabel.color}`}
            />
            <span className="truncate">{meta}</span>
          </div>
          <div className="mt-2 border-t border-border pt-2 text-2xs text-muted-foreground">
            {keyboardIndex !== null ? (
              <kbd className="mr-1 rounded-sm border border-border bg-secondary px-1 font-mono">
                ⌘{keyboardIndex}
              </kbd>
            ) : null}
            激活 · 点 ✕ 关闭
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
