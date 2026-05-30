import * as React from "react";
import type { TFunction } from "i18next";
import {
  AlertTriangle,
  CornerDownRight,
  History,
  Info,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import { AgentAvatarPicker, AgentAvatarUploadActions } from "../icon-picker";
import { AgentAvatar } from "../primitives";
import {
  agentColorClassNames,
  type AgentColor,
  agentColorOrder,
} from "../types";
import {
  agent_svc,
  type agent_backend_svc,
  type department_svc,
} from "../../../../wailsjs/go/models";

import { resolveReportTo } from "./reporting";
import { safeAgentColor, type OrgAgent, type OrgDepartment } from "./types";

type Props = {
  agent: OrgAgent;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: agent_backend_svc.BackendItem[];
  isLeadOf: OrgDepartment | null;
  onUpdate: (req: agent_svc.UpdateAgentRequest) => Promise<unknown>;
  onDelete: (req: agent_svc.DeleteAgentRequest) => Promise<unknown>;
  onUploadAvatar: (req: agent_svc.UploadAvatarRequest) => Promise<unknown>;
  onDeleteAvatar: (req: agent_svc.DeleteAvatarRequest) => Promise<unknown>;
  onClose: () => void;
};

const backendHintKeys: Record<string, string> = {
  claudecode: "org.agent.backendHints.claudeCode",
  "claude-code": "org.agent.backendHints.claudeCode",
  codex: "org.agent.backendHints.codex",
  builtin: "org.agent.backendHints.builtin",
};

type BackendSummaryLike = Pick<
  agent_backend_svc.BackendItem,
  | "id"
  | "type"
  | "name"
  | "llmProviderName"
  | "llmProviderModel"
  | "llmProviderActive"
>;

export function OrgDetailAgent(props: Props) {
  const { t } = useTranslation();
  const isCEO = props.agent.systemBadge === "DEFAULT";
  const [name, setName] = React.useState(props.agent.name);
  const [description, setDescription] = React.useState(props.agent.description);
  const [avatarColor, setAvatarColor] = React.useState<AgentColor>(
    safeAgentColor(props.agent.avatarColor),
  );
  const [avatarIcon, setAvatarIcon] = React.useState<string>(
    props.agent.avatarIcon || "",
  );
  const [backendId, setBackendId] = React.useState<number>(
    props.agent.agentBackendId,
  );
  const [prompt, setPrompt] = React.useState(
    (props.agent.prompt ?? []).join("\n"),
  );
  const [skills, setSkills] = React.useState<department_svc.AgentSkillDTO[]>(
    () => (props.agent.skills ?? []).map((s) => ({ ...s })),
  );
  const [deletePromptOpen, setDeletePromptOpen] = React.useState(false);

  // 汇报对象走统一解析：显式上级 ▸ 部门 leader（沿父部门链递归） ▸ CEO 兜底。
  const reportToId = resolveReportTo(
    props.agent,
    props.agents,
    props.departments,
  );
  const reportTarget =
    reportToId !== 0
      ? (props.agents.find((a) => a.id === reportToId) ?? null)
      : null;
  const selectedBackend =
    props.backends.find((b) => b.id === backendId) ??
    (props.agent.backend?.id === backendId ? props.agent.backend : undefined);

  const handleSave = async () => {
    await props.onUpdate(
      agent_svc.UpdateAgentRequest.createFrom({
        id: props.agent.id,
        name,
        description,
        avatarColor,
        avatarIcon,
        agentBackendId: backendId,
        prompt: prompt.split("\n").filter((s) => s.trim() !== ""),
        skills,
      }),
    );
  };

  const handleUploadFile = async (file: File) => {
    const dataUrl = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result ?? ""));
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
    await props.onUploadAvatar({ id: props.agent.id, dataUrl });
  };

  const handleDeleteAvatar = async () => {
    await props.onDeleteAvatar({ id: props.agent.id });
  };

  const handleConfirmDeleteAgent = async () => {
    await props.onDelete({ id: props.agent.id });
    setDeletePromptOpen(false);
  };

  const toggleSkill = (label: string) => {
    setSkills((prev) =>
      prev.map((s) => (s.label === label ? { ...s, enabled: !s.enabled } : s)),
    );
  };

  const enabledCount = skills.filter((s) => s.enabled).length;
  const disabledCount = skills.length - enabledCount;
  const promptCharCount = prompt.replace(/\s/g, "").length;

  return (
    <div data-slot="org-detail-agent" className="flex h-full flex-col bg-card">
      <header className="space-y-3 border-b border-border bg-card px-5 py-4">
        <div className="flex items-start gap-3">
          <AgentAvatarPicker
            name={name || props.agent.name}
            avatarColor={avatarColor}
            avatarIcon={avatarIcon}
            avatarDataUrl={props.agent.avatarDataUrl}
            onChangeIcon={setAvatarIcon}
            showImageMode={false}
            triggerSize="lg"
          />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-base font-semibold">
                {props.agent.name}
              </span>
            </div>
            <div className="mt-1 flex items-center gap-1.5 font-mono text-2xs text-muted-foreground">
              {props.isLeadOf && (
                <>
                  <span>·</span>
                  <span className="text-primary-text">
                    {t("org.agent.departmentLead", {
                      name: props.isLeadOf.name,
                    })}
                  </span>
                </>
              )}
            </div>
          </div>
          <div className="flex shrink-0 gap-1">
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              disabled={isCEO}
              aria-label={t("org.agent.actions.deleteAgent")}
              onClick={() => setDeletePromptOpen(true)}
            >
              <Trash2 className="size-4 text-destructive" />
            </Button>
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              aria-label={t("common.close")}
              onClick={props.onClose}
            >
              <X className="size-4" />
            </Button>
          </div>
        </div>
        {reportTarget && (
          <div className="flex items-center gap-1.5 text-2xs text-muted-foreground">
            <CornerDownRight className="size-3" aria-hidden="true" />
            <span>{t("org.agent.reportsTo")}</span>
            <span className="inline-flex items-center gap-1.5 rounded-sm bg-secondary px-1.5 py-0.5 font-mono text-foreground">
              <AgentAvatar
                name={reportTarget.name}
                color={safeAgentColor(reportTarget.avatarColor)}
                avatarDataUrl={reportTarget.avatarDataUrl}
                avatarIcon={reportTarget.avatarIcon}
                className="size-3.5 rounded-sm text-2xs"
              />
              <span>{reportTarget.name}</span>
            </span>
            <span className="opacity-60">
              {t("org.agent.dragHierarchyHint")}
            </span>
          </div>
        )}
      </header>

      <div className="flex-1 space-y-6 overflow-y-auto px-5 py-5">
        <section className="space-y-4" data-slot="agent-section-basic">
          <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("org.agent.basicInfo")}
          </h3>
          <div className="space-y-1.5">
            <label className="block text-2xs text-muted-foreground">
              {t("org.department.name")}
            </label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              aria-label={t("org.department.name")}
            />
          </div>
          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <label className="text-2xs text-muted-foreground">
                {t("org.department.description")}
              </label>
              <span className="font-mono text-2xs text-muted-foreground">
                {t("org.agent.descriptionHint")}
              </span>
            </div>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              aria-label={t("org.department.description")}
            />
          </div>
          <div className="space-y-2">
            <label className="block text-2xs text-muted-foreground">
              {t("org.chart.newAgent.avatar")}
            </label>
            <div className="flex items-center gap-3">
              <AgentAvatarPicker
                name={name || props.agent.name}
                avatarColor={avatarColor}
                avatarIcon={avatarIcon}
                avatarDataUrl={props.agent.avatarDataUrl}
                onChangeIcon={setAvatarIcon}
                showImageMode={false}
                triggerSize="lg"
                triggerClassName="size-12 rounded-lg"
              />
              <AgentAvatarUploadActions
                avatarDataUrl={props.agent.avatarDataUrl}
                onUpload={handleUploadFile}
                onDelete={handleDeleteAvatar}
                uploadLabel={
                  props.agent.avatarDataUrl
                    ? t("org.agent.avatar.replaceImage")
                    : t("org.agent.avatar.uploadImage")
                }
              />
            </div>
          </div>
          <div className="space-y-2">
            <label className="block text-2xs text-muted-foreground">
              {t("org.chart.newAgent.avatarColor")}
            </label>
            <div
              className="grid grid-cols-5 gap-2"
              role="radiogroup"
              aria-label={t("org.chart.newAgent.avatarColor")}
            >
              {agentColorOrder.map((c) => (
                <button
                  key={c}
                  type="button"
                  role="radio"
                  aria-checked={avatarColor === c}
                  aria-label={t("org.chart.newAgent.avatarColorNamed", {
                    color: c,
                  })}
                  onClick={() => setAvatarColor(c)}
                  className={cn(
                    "size-7 rounded-full ring-offset-2 transition-all",
                    agentColorClassNames[c],
                    avatarColor === c && "ring-2 ring-primary",
                  )}
                />
              ))}
            </div>
          </div>
        </section>

        <section className="space-y-2.5" data-slot="agent-section-backend">
          <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("org.agent.backend.title")}
          </h3>
          <Select
            value={backendId > 0 ? String(backendId) : ""}
            onValueChange={(v) => setBackendId(Number(v))}
          >
            <SelectTrigger
              aria-label={t("org.agent.backend.title")}
              className="h-auto bg-input-bg px-3 py-2.5"
            >
              <div className="flex min-w-0 items-center gap-2.5">
                <span className="inline-flex size-7 shrink-0 items-center justify-center rounded-sm bg-secondary text-foreground">
                  <Wrench className="size-3.5" aria-hidden="true" />
                </span>
                {selectedBackend ? (
                  <div className="flex min-w-0 flex-col items-start">
                    <span className="max-w-full truncate text-sm font-semibold text-foreground">
                      {selectedBackend.name}
                    </span>
                    <span className="max-w-full truncate font-mono text-2xs font-normal text-muted-foreground">
                      {backendProviderSummary(selectedBackend, t)}
                    </span>
                  </div>
                ) : (
                  <SelectValue placeholder={t("common.unassigned")} />
                )}
              </div>
            </SelectTrigger>
            <SelectContent>
              {props.backends.map((b) => (
                <SelectItem key={b.id} value={String(b.id)}>
                  <div className="flex min-w-0 flex-col items-start">
                    <span className="max-w-full truncate text-sm font-semibold">
                      {b.name}
                    </span>
                    <span className="max-w-full truncate font-mono text-2xs text-muted-foreground">
                      {backendProviderSummary(b, t)}
                    </span>
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-2xs text-muted-foreground">
            {t(
              backendHintKeys[selectedBackend?.type ?? ""] ??
                "org.agent.backendHints.default",
            )}
          </p>
        </section>

        <section className="space-y-2" data-slot="agent-section-prompt">
          <div className="flex items-center justify-between">
            <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              {t("org.agent.systemPrompt")}
            </h3>
            <span className="font-mono text-2xs text-muted-foreground">
              {t("org.agent.charCount", { count: promptCharCount })}
            </span>
          </div>
          <Textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            aria-label={t("org.agent.systemPrompt")}
            className="min-h-[160px] font-mono text-xs"
          />
          <div className="flex items-center gap-1.5 text-2xs text-muted-foreground">
            <Info className="size-3" aria-hidden="true" />
            <span>{t("org.agent.systemPromptHint")}</span>
          </div>
        </section>

        <section className="space-y-2.5" data-slot="agent-section-skills">
          <div className="flex items-center justify-between">
            <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              {t("org.agent.skills.title")}
            </h3>
            <span className="font-mono text-2xs text-muted-foreground">
              {t("org.agent.skills.summary", {
                disabled: disabledCount,
                enabled: enabledCount,
              })}
            </span>
          </div>
          {skills.length === 0 ? (
            <p className="text-2xs text-muted-foreground">
              {t("org.agent.skills.empty")}
            </p>
          ) : (
            <div
              className="flex flex-wrap gap-1.5"
              role="group"
              aria-label={t("org.agent.skills.list")}
            >
              {skills.map((s) => (
                <button
                  key={s.label}
                  type="button"
                  role="switch"
                  aria-checked={s.enabled}
                  aria-label={t("org.agent.skills.item", { label: s.label })}
                  onClick={() => toggleSkill(s.label)}
                  className={cn(
                    "rounded-sm border px-2 py-1 font-mono text-2xs transition-colors",
                    s.enabled
                      ? "border-status-running bg-status-running-bg text-status-running"
                      : "border-destructive bg-destructive-soft text-destructive line-through",
                  )}
                >
                  {s.label}
                </button>
              ))}
            </div>
          )}
        </section>
      </div>

      <footer className="flex items-center gap-2 border-t border-border bg-secondary/40 px-5 py-3">
        <span className="flex flex-1 items-center gap-1.5 font-mono text-2xs text-muted-foreground">
          <History className="size-3" aria-hidden="true" />
          {props.agent.updatetime > 0
            ? t("org.agent.savedAt", {
                time: formatRelativeTime(props.agent.updatetime, t),
              })
            : t("org.agent.unsaved")}
        </span>
        <Button variant="outline" size="sm" onClick={props.onClose}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" onClick={handleSave}>
          {t("common.save")}
        </Button>
      </footer>

      <Dialog
        open={deletePromptOpen}
        onOpenChange={(o) => !o && setDeletePromptOpen(false)}
      >
        {deletePromptOpen && (
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <AlertTriangle
                  className="size-[18px] text-destructive"
                  aria-hidden="true"
                />
                <span>
                  {t("org.agent.deleteDialog.title", {
                    name: props.agent.name,
                  })}
                </span>
              </DialogTitle>
              <DialogDescription>
                {t("org.agent.deleteDialog.description")}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <span className="mr-auto font-mono text-2xs text-muted-foreground">
                {t("org.department.deleteDialog.irreversible")}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setDeletePromptOpen(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => void handleConfirmDeleteAgent()}
              >
                <Trash2 className="size-3.5" />
                {t("org.department.deleteDialog.confirm")}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </div>
  );
}

function backendProviderSummary(
  backend: BackendSummaryLike,
  t: TFunction,
): string {
  const providerName = backend.llmProviderName?.trim();
  const model = backend.llmProviderModel?.trim();
  const inactiveSuffix =
    backend.llmProviderActive === false && providerName
      ? t("org.agent.backend.inactiveSuffix")
      : "";

  if (providerName && model) {
    return `${providerName} · ${model}${inactiveSuffix}`;
  }
  if (providerName) {
    return `${providerName}${inactiveSuffix}`;
  }
  if (model) {
    return model;
  }
  return t("org.agent.backend.unlinkedProvider");
}

function formatRelativeTime(unixSeconds: number, t: TFunction): string {
  const now = Math.floor(Date.now() / 1000);
  const diff = now - unixSeconds;
  if (diff < 60) return t("org.agent.relativeTime.justNow");
  if (diff < 3600) {
    return t("org.agent.relativeTime.minutesAgo", {
      count: Math.floor(diff / 60),
    });
  }
  if (diff < 86400) {
    return t("org.agent.relativeTime.hoursAgo", {
      count: Math.floor(diff / 3600),
    });
  }
  return t("org.agent.relativeTime.daysAgo", {
    count: Math.floor(diff / 86400),
  });
}
