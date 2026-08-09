export type Scope =
  | "llm-providers"
  | "agent-backends"
  | "organization"
  | "remote-devices";

export type ItemAction = "create" | "overwrite" | "skip" | "duplicate";
