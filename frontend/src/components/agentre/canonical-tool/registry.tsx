import * as React from "react";

import { FileWriteCard } from "./file-write/card";
import { FileEditCard } from "./file-edit/card";
import { UserAskCard } from "./user-ask/card";
import { PlanApproveCard } from "./plan-approve-request/card";
import { AgentSpawnCard } from "./agent-spawn/card";
import { ToolPermissionCard } from "./tool-permission/card";
import { RawToolCard } from "./raw/card";
import type { CanonicalCardProps } from "./props";
import type { CanonicalDTO, CanonicalKind } from "./types";

const REGISTRY: Partial<Record<CanonicalKind, React.FC<CanonicalCardProps>>> = {
  "file.write": FileWriteCard,
  "file.edit": FileEditCard,
  "user.ask": UserAskCard,
  "plan.approve_request": PlanApproveCard,
  "agent.spawn": AgentSpawnCard,
  "tool.permission": ToolPermissionCard,
};

// CanonicalToolRouter 根据 toolBlock.canonical.kind 路由到对应卡;
// 无 canonical 字段或 kind 未注册时回落 RawToolCard。plan.update 刻意不注册:
// tool_use 形态的 plan.update 仍显示普通工具卡;type="plan" 且带 actions 的
// plan.update 在 chat.tsx 里直接复用 PlanCard。
export function CanonicalToolRouter(props: CanonicalCardProps) {
  const canonical = (props.toolBlock as { canonical?: CanonicalDTO }).canonical;
  if (!canonical) return <RawToolCard {...props} />;
  const Card = REGISTRY[canonical.kind];
  if (!Card) return <RawToolCard {...props} />;
  return <Card {...props} />;
}
