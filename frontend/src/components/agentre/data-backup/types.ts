import i18n from "@/i18n";

export type Scope =
  | "llm-providers"
  | "agent-backends"
  | "organization"
  | "remote-devices";

export const SCOPE_LABELS: Record<Scope, string> = {
  "llm-providers": i18n.t("dataBackup.scopes.llm-providers"),
  "agent-backends": i18n.t("dataBackup.scopes.agent-backends"),
  organization: i18n.t("dataBackup.scopes.organization"),
  "remote-devices": i18n.t("dataBackup.scopes.remote-devices"),
};

export type ItemAction = "create" | "overwrite" | "skip" | "duplicate";
