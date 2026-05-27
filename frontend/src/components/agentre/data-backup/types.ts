export type Scope =
  | "llm-providers"
  | "agent-backends"
  | "organization"
  | "remote-devices";

export const SCOPE_LABELS: Record<Scope, string> = {
  "llm-providers": "LLM 供应商",
  "agent-backends": "Agent 后端",
  organization: "组织架构",
  "remote-devices": "远端设备",
};

export type ItemAction = "create" | "overwrite" | "skip" | "duplicate";
