export type AgentColor =
  | "agent-1"
  | "agent-2"
  | "agent-3"
  | "agent-4"
  | "agent-5"
  | "agent-6"
  | "agent-7"
  | "agent-8"
  | "agent-9"
  | "agent-10"
  | "neutral";

import type { AgentStatus } from "@/stores/types";
export type { AgentStatus };

export const agentColorClassNames: Record<AgentColor, string> = {
  "agent-1": "bg-agent-1",
  "agent-2": "bg-agent-2",
  "agent-3": "bg-agent-3",
  "agent-4": "bg-agent-4",
  "agent-5": "bg-agent-5",
  "agent-6": "bg-agent-6",
  "agent-7": "bg-agent-7",
  "agent-8": "bg-agent-8",
  "agent-9": "bg-agent-9",
  "agent-10": "bg-agent-10",
  neutral: "bg-neutral-600",
};

export const agentColorOrder: AgentColor[] = [
  "agent-1",
  "agent-2",
  "agent-3",
  "agent-4",
  "agent-5",
  "agent-6",
  "agent-7",
  "agent-8",
  "agent-9",
  "agent-10",
];

export const statusConfig: Record<
  AgentStatus,
  {
    label: string;
    dotClassName: string;
    textClassName: string;
    pillClassName: string;
  }
> = {
  running: {
    label: "RUNNING",
    dotClassName: "bg-status-running",
    textClassName: "text-status-running",
    pillClassName: "bg-status-running-bg text-status-running",
  },
  waiting: {
    label: "WAITING",
    dotClassName: "bg-status-waiting",
    textClassName: "text-status-waiting",
    pillClassName: "bg-status-waiting-bg text-status-waiting",
  },
  idle: {
    label: "IDLE",
    dotClassName: "bg-status-idle",
    textClassName: "text-muted-foreground",
    pillClassName: "bg-secondary text-muted-foreground",
  },
  error: {
    label: "ERROR",
    dotClassName: "bg-status-error",
    textClassName: "text-status-error",
    pillClassName: "bg-destructive-soft text-status-error",
  },
};
