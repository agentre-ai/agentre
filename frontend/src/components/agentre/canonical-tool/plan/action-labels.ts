import {
  CheckCircle2,
  FastForward,
  PencilLine,
  Send,
  ShieldCheck,
  ShieldOff,
  type LucideIcon,
} from "lucide-react";

import type { PlanActionDTO } from "../types";

// PlanActionMeta 把 backend 装配的 provider-neutral actionId 映射到前端展示属性。
export type PlanActionMeta = {
  label: string;
  variant: "default" | "outline" | "ghost";
  icon: LucideIcon;
};

const META: Record<string, PlanActionMeta> = {
  "plan.approve.bypass_permissions": {
    label: "批准并跳过权限确认",
    variant: "default",
    icon: ShieldOff,
  },
  "plan.approve.accept_edits": {
    label: "批准并切换自动模式",
    variant: "default",
    icon: FastForward,
  },
  "plan.approve.manual": {
    label: "批准,手动确认编辑",
    variant: "outline",
    icon: ShieldCheck,
  },
  "plan.execute": {
    label: "执行计划",
    variant: "default",
    icon: CheckCircle2,
  },
};

type MetaContext = {
  requestId?: string;
};

// metaFor 兜底未知 actionId → kind 级通用文案,避免 backend 加新 id 时前端崩溃。
export function metaFor(
  action: PlanActionDTO,
  context?: MetaContext,
): PlanActionMeta {
  if (action.id === "plan.refine" || action.kind === "refine") {
    return {
      label: context?.requestId ? "继续规划" : "继续完善",
      variant: context?.requestId ? "ghost" : "outline",
      icon: PencilLine,
    };
  }
  const exact = META[action.id];
  if (exact) return exact;
  if (action.kind === "approve") {
    return {
      label: context?.requestId ? "批准计划" : "执行计划",
      variant: "default",
      icon: CheckCircle2,
    };
  }
  return {
    label: "继续",
    variant: "outline",
    icon: Send,
  };
}

// splitActions 把"主要按钮"(非 refine)与"反馈按钮"(refine)拆开,前者放
// 工具栏主区,后者打开 feedback textarea。
export function splitActions(actions: PlanActionDTO[] | undefined): {
  primary: PlanActionDTO[];
  refine?: PlanActionDTO;
} {
  if (!actions || actions.length === 0) return { primary: [] };
  const refine = actions.find((a) => a.kind === "refine");
  const primary = actions.filter((a) => a.kind !== "refine");
  return { primary, refine };
}
