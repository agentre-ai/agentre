import type { TFunction } from "i18next";

import type { CatalogItem } from "../capability/catalog";
import type { department_svc } from "../../../../wailsjs/go/models";

// 已知需审批的工具(后端写操作走审批门)。新增工具时在此登记，
// 真正的 per-tool 审批元数据未来应由后端提供(本期前端已知集合)。
export const APPROVAL_TOOLS = new Set(["org", "workflow", "group_create"]);

export function toolKeysToCatalog(
  keys: string[],
  agentTools: department_svc.AgentToolDTO[],
  t: TFunction,
): CatalogItem[] {
  const enabledByKey = new Map(agentTools.map((tl) => [tl.key, tl.enabled]));
  return keys.map((key) => ({
    id: key,
    name: t(`org.agent.tools.names.${key}`),
    description: t(`org.agent.tools.descriptions.${key}`),
    group: "",
    badges: APPROVAL_TOOLS.has(key)
      ? [{ label: t("org.agent.tools.approval"), tone: "approval" as const }]
      : undefined,
    enabled: enabledByKey.get(key) ?? false,
  }));
}
