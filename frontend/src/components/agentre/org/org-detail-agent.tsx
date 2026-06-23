import * as React from "react";
import type { TFunction } from "i18next";
import {
  AlertTriangle,
  Ban,
  Boxes,
  CornerDownRight,
  History,
  Info,
  Network,
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
  department_svc,
  type agent_backend_svc,
} from "../../../../wailsjs/go/models";

import type { TriState } from "../capability/catalog";
import { useBackendCapabilities } from "../capability/use-backend-capabilities";
import { CapabilityPicker } from "../capability/capability-picker";
import { GrantedChips, type GrantedChip } from "../capability/granted-chips";
import { resolveReportTo } from "./reporting";
import { toolKeysToCatalog, APPROVAL_TOOLS } from "./tool-catalog";
import { useSkillCatalog } from "./use-skill-catalog";
import { safeAgentColor, type OrgAgent, type OrgDepartment } from "./types";

type Props = {
  agent: OrgAgent;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: agent_backend_svc.BackendItem[];
  isLeadOf: OrgDepartment | null;
  availableTools?: string[];
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
  const [tools, setTools] = React.useState<department_svc.AgentToolDTO[]>(
    () => {
      const cur = new Map(
        (props.agent.tools ?? []).map((t) => [t.key, t.enabled]),
      );
      return (props.availableTools ?? []).map((key) => ({
        key,
        enabled: cur.get(key) ?? false,
      }));
    },
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

  const { caps } = useBackendCapabilities(selectedBackend?.type);
  const skillsCapOn = caps?.has("skills") ?? false;
  const skillCatalog = useSkillCatalog(props.agent.id, skillsCapOn);
  const [skillPickerOpen, setSkillPickerOpen] = React.useState(false);

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
        tools,
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

  const [toolPickerOpen, setToolPickerOpen] = React.useState(false);
  const toolChips: GrantedChip[] = tools
    .filter((tl) => tl.enabled)
    .map((tl) => ({
      id: tl.key,
      label: t(`org.agent.tools.names.${tl.key}`),
      badge: APPROVAL_TOOLS.has(tl.key)
        ? t("org.agent.tools.approval")
        : undefined,
    }));
  const toolItems = toolKeysToCatalog(props.availableTools ?? [], tools, t);
  const toggleToolGrant = (key: string) =>
    setTools((prev) =>
      prev.map((tl) => (tl.key === key ? { ...tl, enabled: !tl.enabled } : tl)),
    );
  const removeTool = (key: string) =>
    setTools((prev) =>
      prev.map((tl) => (tl.key === key ? { ...tl, enabled: false } : tl)),
    );

  // 已授予芯片：label 由 id 派生(strip @marketplace)，count 来自已拉过的目录缓存。
  const idToName = (id: string) => id.split("@")[0] || id;
  const skillCountById = React.useMemo(() => {
    const m = new Map<string, number>();
    for (const it of skillCatalog.items) m.set(it.id, it.contents?.length ?? 0);
    return m;
  }, [skillCatalog.items]);
  const globallyOn = React.useMemo(
    () =>
      new Set(
        skillCatalog.items.filter((i) => i.globallyEnabled).map((i) => i.id),
      ),
    [skillCatalog.items],
  );

  const skillStateOf = (id: string): TriState => {
    const s = skills.find((x) => x.id === id);
    if (!s) return "inherit";
    return s.enabled ? "on" : "off";
  };
  const setSkillState = (id: string, next: TriState) =>
    setSkills((prev) => {
      const rest = prev.filter((s) => s.id !== id);
      if (next === "inherit") return rest;
      return [
        ...rest,
        department_svc.AgentSkillDTO.createFrom({ id, enabled: next === "on" }),
      ];
    });

  const triLabels: Record<TriState, string> = {
    inherit: t("capability.triState.inherit"),
    on: t("capability.triState.on"),
    off: t("capability.triState.off"),
  };

  const onSkills = skills.filter((s) => s.enabled);
  const offSkills = skills.filter((s) => !s.enabled);
  const overriddenIds = new Set(skills.map((s) => s.id));
  const inheritedIds = [...globallyOn].filter((id) => !overriddenIds.has(id));
  const skillChips: GrantedChip[] = [
    ...inheritedIds.map((id) => ({
      id,
      label: idToName(id),
      count: skillCountById.get(id),
      tone: "inherit" as const,
      locked: true,
    })),
    ...onSkills.map((s) => ({
      id: s.id,
      label: idToName(s.id),
      count: skillCountById.get(s.id),
      tone: "on" as const,
    })),
    ...offSkills.map((s) => ({
      id: s.id,
      label: idToName(s.id),
      count: skillCountById.get(s.id),
      tone: "off" as const,
    })),
  ];

  const pickerItems = skillCatalog.items.map((it) => ({
    ...it,
    state: it.state ? skillStateOf(it.id) : undefined,
  }));

  const openSkillPicker = () => {
    setSkillPickerOpen(true);
    if (!skillCatalog.fetched) void skillCatalog.load(false);
  };
  const removeSkillOverride = (id: string) =>
    setSkills((prev) => prev.filter((s) => s.id !== id));

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
                className="size-5 rounded-sm text-2xs"
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
          {caps?.has("skills") ? (
            <>
              <GrantedChips
                title={t("org.agent.skills.sectionTitle")}
                countLabel={t("org.agent.skills.count", {
                  inherit: inheritedIds.length,
                  on: onSkills.length,
                  off: offSkills.length,
                })}
                chipIcon={Boxes}
                chips={skillChips}
                addLabel={t("org.agent.skills.manage")}
                removeLabel={(name) => t("capability.picker.remove", { name })}
                onRemove={removeSkillOverride}
                onAdd={openSkillPicker}
                emptyLabel={t("org.agent.skills.empty")}
                footerNote={t("org.agent.skills.inheritNote")}
              />
              <CapabilityPicker
                open={skillPickerOpen}
                title={t("org.agent.skillPicker.title")}
                subtitle={t("org.agent.skillPicker.subtitle")}
                searchPlaceholder={t("org.agent.skillPicker.searchPlaceholder")}
                items={pickerItems}
                loading={skillCatalog.loading}
                triLabels={triLabels}
                footerSummary={t("org.agent.skills.count", {
                  inherit: inheritedIds.length,
                  on: onSkills.length,
                  off: offSkills.length,
                })}
                footerNote={t("org.agent.skillPicker.personalNote")}
                onToggle={() => {}}
                onSetState={setSkillState}
                onConfirm={() => setSkillPickerOpen(false)}
                onCancel={() => setSkillPickerOpen(false)}
                onRescan={() => void skillCatalog.load(true)}
              />
            </>
          ) : (
            <div className="space-y-2">
              <div className="flex items-center gap-1.5">
                <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {t("org.agent.skills.title")}
                </h3>
                <div className="flex-1" />
                <span className="rounded bg-secondary px-1.5 py-0.5 font-mono text-2xs text-muted-foreground">
                  {t("org.agent.skillsGate.pill")}
                </span>
              </div>
              <div className="flex items-start gap-2.5 rounded-md border border-border bg-secondary/30 px-3 py-2.5">
                <Ban
                  className="mt-0.5 size-3.5 text-muted-foreground"
                  aria-hidden="true"
                />
                <div className="space-y-0.5">
                  <p className="text-2xs font-semibold text-foreground">
                    {t("org.agent.skillsGate.title")}
                  </p>
                  <p className="font-mono text-2xs text-muted-foreground">
                    {t("org.agent.skillsGate.description")}
                  </p>
                </div>
              </div>
            </div>
          )}
        </section>

        {caps?.has("mcp_tools") && (
          <section className="space-y-2.5" data-slot="agent-section-tools">
            <GrantedChips
              title={t("org.agent.tools.sectionTitle")}
              countLabel={t("org.agent.tools.enabledCount", {
                count: toolChips.length,
              })}
              chipIcon={Network}
              chips={toolChips}
              addLabel={t("org.agent.tools.add")}
              removeLabel={(name) => t("capability.picker.remove", { name })}
              onRemove={removeTool}
              onAdd={() => setToolPickerOpen(true)}
              emptyLabel={t("org.agent.tools.empty")}
            />
            <CapabilityPicker
              open={toolPickerOpen}
              title={t("org.agent.toolPicker.title")}
              subtitle={t("org.agent.toolPicker.subtitle")}
              searchPlaceholder={t("org.agent.toolPicker.searchPlaceholder")}
              items={toolItems}
              onToggle={toggleToolGrant}
              onConfirm={() => setToolPickerOpen(false)}
              onCancel={() => setToolPickerOpen(false)}
            />
          </section>
        )}
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
