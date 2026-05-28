import * as React from "react";
import { FileCode, List } from "lucide-react";

import { cn } from "@/lib/utils";

import type { ChatSidebarTab } from "@/stores/chat-sidebar-store";

type Props = {
  active: ChatSidebarTab;
  onChange: (tab: ChatSidebarTab) => void;
  outlineCount: number;
  filesCount: number;
};

export function TabBar({ active, onChange, outlineCount, filesCount }: Props) {
  return (
    <div
      className="flex h-9 shrink-0 items-center gap-1 border-b border-border px-3"
      role="tablist"
    >
      <Tab
        icon={<List className="size-3" aria-hidden="true" />}
        label="Outline"
        count={outlineCount}
        active={active === "outline"}
        onClick={() => onChange("outline")}
      />
      <Tab
        icon={<FileCode className="size-3" aria-hidden="true" />}
        label="Files"
        count={filesCount}
        active={active === "files"}
        onClick={() => onChange("files")}
      />
    </div>
  );
}

function Tab({
  icon,
  label,
  count,
  active,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium",
        active
          ? "bg-primary/10 text-primary"
          : "text-muted-foreground hover:bg-muted/50",
      )}
    >
      {icon}
      <span>{label}</span>
      <span className="font-mono text-[10px] opacity-80">{count}</span>
    </button>
  );
}
