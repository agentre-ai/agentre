import type { LucideIcon } from "lucide-react";

import type { department_svc } from "../../../../wailsjs/go/models";

import { ICON_LIST, iconForKey as iconForKeyShared } from "../icon-registry";

export type OrgAgentColor =
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

export type OrgAgentStatus = "running" | "waiting" | "idle";

export type OrgSelection =
  | { kind: "agent"; id: number }
  | { kind: "department"; id: number }
  | null;

// 向后兼容：旧版本 `ICON_REGISTRY` 是 key → LucideIcon 的 Record。
// 新代码请直接使用 `../icon-registry` 里的 `ICON_LIST` / `iconForKey` / `searchIcons`。
export const ICON_REGISTRY: Record<string, LucideIcon> = Object.fromEntries(
  ICON_LIST.map((m) => [m.key, m.icon]),
);

export const iconForKey = iconForKeyShared;

export type OrgDepartment = department_svc.DepartmentItem;
export type OrgAgent = department_svc.AgentItem;

export function isOrgAgentColor(value: string): value is OrgAgentColor {
  return (
    value === "agent-1" ||
    value === "agent-2" ||
    value === "agent-3" ||
    value === "agent-4" ||
    value === "agent-5" ||
    value === "agent-6" ||
    value === "agent-7" ||
    value === "agent-8" ||
    value === "agent-9" ||
    value === "agent-10" ||
    value === "neutral"
  );
}

export function safeAgentColor(value: string): OrgAgentColor {
  return isOrgAgentColor(value) ? value : "neutral";
}
