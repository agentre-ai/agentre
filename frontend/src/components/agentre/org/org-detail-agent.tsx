import * as React from "react";
import {
  AlertTriangle,
  Ban,
  Boxes,
  CornerDownRight,
  Info,
  Network,
  Trash2,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { useAutoSave } from "./use-auto-save";
import { AutoSaveStatus } from "./auto-save-status";
import { ExecTargetList, type ExecTargetRow } from "./exec-target-list";
import { useExecTargetAvailability } from "./use-exec-target-availability";
import {
  ExecTargetSkillsBlock,
  type CopySource,
} from "./exec-target-skills-block";

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

// ExecTargetEdit 是编辑态执行目标行（R15/R15e）：agentBackendId 定位这一档，
// skills 是它自己的技能授权（下沉到执行目标行,不再是 Agent 级一份）。
type ExecTargetEdit = {
  agentBackendId: number;
  skills: department_svc.AgentSkillDTO[];
};

function execTargetsFromAgent(agent: OrgAgent): ExecTargetEdit[] {
  return (agent.execTargets ?? []).map((t) => ({
    agentBackendId: t.agentBackendId,
    skills: (t.skills ?? []).map((s) => ({ ...s })),
  }));
}

export function OrgDetailAgent(props: Props) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isCEO = props.agent.systemBadge === "DEFAULT";

  const { values, patch, flush, wrap, status, pendingInvalid, retry } =
    useAutoSave({
      initial: {
        name: props.agent.name,
        description: props.agent.description,
        avatarColor: safeAgentColor(props.agent.avatarColor),
        avatarIcon: props.agent.avatarIcon || "",
        execTargets: execTargetsFromAgent(props.agent),
        prompt: (props.agent.prompt ?? []).join("\n"),
        tools: ((): department_svc.AgentToolDTO[] => {
          const cur = new Map(
            (props.agent.tools ?? []).map((tl) => [tl.key, tl.enabled]),
          );
          return (props.availableTools ?? []).map((key) => ({
            key,
            enabled: cur.get(key) ?? false,
          }));
        })(),
      },
      // R15：列表为空的 Agent 不能起会话——界面在保存时就要求至少一项。
      isValid: (v) => v.name.trim() !== "" && v.execTargets.length > 0,
      save: (v) =>
        props.onUpdate(
          agent_svc.UpdateAgentRequest.createFrom({
            id: props.agent.id,
            name: v.name,
            description: v.description,
            avatarColor: v.avatarColor,
            avatarIcon: v.avatarIcon,
            prompt: v.prompt.split("\n").filter((s) => s.trim() !== ""),
            execTargets: v.execTargets,
            tools: v.tools,
          }),
        ),
    });

  const {
    name,
    description,
    avatarColor,
    avatarIcon,
    execTargets,
    prompt,
    tools,
  } = values;

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

  // props.agent.backend 是这个 Agent 的主档 backend 摘要（department_svc.Load 已
  // join 好，见 AgentItem.backend 字段注释）：props.backends 全量列表还没加载完/
  // 该 backend 已被删但摘要还留着时的兜底,不让界面在这段时间内空白。
  const backendById = React.useMemo(() => {
    const m = new Map<number, agent_backend_svc.BackendItem>();
    for (const b of props.backends) m.set(b.id, b);
    if (props.agent.backend && !m.has(props.agent.backend.id)) {
      m.set(
        props.agent.backend.id,
        backendItemFromSummary(props.agent.backend),
      );
    }
    return m;
  }, [props.backends, props.agent.backend]);
  const backendsForList = React.useMemo(
    () => [...backendById.values()],
    [backendById],
  );

  const singleTarget = execTargets.length === 1;
  const primaryTarget = execTargets[0];
  const selectedBackend = primaryTarget
    ? (backendById.get(primaryTarget.agentBackendId) ??
      (props.agent.backend?.id === primaryTarget.agentBackendId
        ? props.agent.backend
        : undefined))
    : undefined;

  // 任务 8：不可对话状态内联提示（继续只覆盖单档场景——多档时"哪一档缺什么"已经在
  // 执行目标列表的逐行徽标 + 全部不可用横幅里说明，不需要在这里重复一份笼统提示）。
  const hasUsableProvider =
    selectedBackend?.llmProviderActive === true &&
    Boolean(selectedBackend?.llmProviderName?.trim());
  const noBackendBound = execTargets.length === 0;
  const builtinMissingProvider =
    singleTarget &&
    !noBackendBound &&
    selectedBackend?.type === "builtin" &&
    !hasUsableProvider;

  // patchExecTargetSet 把列表上的一次增删/更换落到**账号级集合**上：成员按 next
  // 收敛，顺序取账号自己那一份（execTargets）——列表展示的是本端解析后的顺序，
  // 照搬它会把本端排序回写成账号顺序，污染其它端看到的顺序。
  const patchExecTargetSet = (next: ExecTargetRow[]) => {
    const wanted = next.map((r) => r.agentBackendId);
    const wantedIds = new Set(wanted);
    const kept = execTargets.filter((t) => wantedIds.has(t.agentBackendId));
    const keptIds = new Set(kept.map((t) => t.agentBackendId));
    const added: ExecTargetEdit[] = wanted
      .filter((id) => !keptIds.has(id))
      .map((id) => ({ agentBackendId: id, skills: [] }));
    patch({ execTargets: [...kept, ...added] }, { immediate: true });
  };

  // 本端生效顺序（R14 解析后的顺序，含覆盖 / 无覆盖时本机自己提前）来自后端
  // ListExecTargetAvailability 的返回数组顺序，它就是执行目标区唯一那份列表。
  const deviceTargetsKey = React.useMemo(
    () =>
      execTargets
        .map((t) => t.agentBackendId)
        .slice()
        .sort((a, b) => a - b)
        .join(","),
    [execTargets],
  );
  const availability = useExecTargetAvailability(
    props.agent.id,
    deviceTargetsKey,
  );
  // null = 这一轮读还没落定（顺序还在路上）；数组 = 已落定的本端顺序，读失败时是空
  // 数组。两者必须分开，否则「读完了但没拿到」会被当成「还在路上」，列表永远停在骨架。
  const [deviceTargets, setDeviceTargets] = React.useState<
    ExecTargetRow[] | null
  >(null);
  React.useEffect(() => {
    if (availability.orderedTargets.length > 0) {
      setDeviceTargets(availability.orderedTargets);
    } else if (availability.settled) {
      setDeviceTargets([]);
    }
  }, [availability.orderedTargets, availability.settled]);

  // 顺序数据还没到达：渲染骨架而不是空态卡片（真正的空态是「这个 Agent 没有任何
  // 执行目标」，此时 execTargets 也是空的，下面这个判断自然为假）。
  const orderPending = execTargets.length > 0 && deviceTargets === null;

  // 顺序读完了却没拿到（读失败）时列表回落到账号级集合自己的顺序：增删/更换与顺序
  // 无关，任何状态下都得可用（规格「增删恒可用」），把列表钉死在骨架上等于把它们
  // 一起锁掉。用户少看到的只是「本机档自动提前」那一下重排，能做的事一件不少。
  const listTargets: ExecTargetRow[] =
    deviceTargets && deviceTargets.length > 0
      ? deviceTargets
      : execTargets.map((t) => ({ agentBackendId: t.agentBackendId }));

  // 重排只写本端顺序覆盖（orderOverride），不碰账号级集合、不同步。
  const writeDeviceOverride = React.useCallback(
    (order: number[]) => {
      void wrap(() =>
        props.onUpdate(
          agent_svc.UpdateAgentRequest.createFrom({
            id: props.agent.id,
            name,
            description,
            avatarColor,
            avatarIcon,
            prompt: prompt.split("\n").filter((s) => s.trim() !== ""),
            execTargets,
            tools,
            orderOverride: order,
          }),
        ),
      ).then(() => availability.reload());
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      props.agent.id,
      name,
      description,
      avatarColor,
      avatarIcon,
      prompt,
      execTargets,
      tools,
    ],
  );

  const handleExecTargetReorder = (next: ExecTargetRow[]) => {
    setDeviceTargets(next);
    void writeDeviceOverride(next.map((t) => t.agentBackendId));
  };

  // 增删/更换：列表当场跟手，账号级集合随后由 patchExecTargetSet 落库；这一批
  // 写入不带 orderOverride，本端已有的顺序覆盖原样保留（收敛规则会补上新档）。
  const handleExecTargetSetChange = (next: ExecTargetRow[]) => {
    setDeviceTargets(next);
    patchExecTargetSet(next);
  };

  const patchTargetSkills = (
    targetIndex: number,
    skills: department_svc.AgentSkillDTO[],
  ) => {
    const next = execTargets.map((t, i) =>
      i === targetIndex ? { ...t, skills } : t,
    );
    patch({ execTargets: next }, { immediate: true });
  };

  const execTargetLabel = (index: number) => {
    const b = backendById.get(execTargets[index]?.agentBackendId ?? 0);
    const machine = b?.deviceId
      ? b.deviceName || b.deviceId
      : t("org.agent.execTargets.localMachine");
    return `${index + 1} · ${machine}`;
  };

  const copySourcesFor = (targetIndex: number): CopySource[] => {
    const targetBackend = backendById.get(
      execTargets[targetIndex]?.agentBackendId ?? 0,
    );
    return execTargets
      .map((t, i) => ({ t, i }))
      .filter(({ i }) => i !== targetIndex)
      .map(({ t, i }) => {
        const srcBackend = backendById.get(t.agentBackendId);
        return {
          index: i,
          label: execTargetLabel(i),
          skills: t.skills,
          sameType: Boolean(
            srcBackend &&
            targetBackend &&
            srcBackend.type === targetBackend.type,
          ),
        };
      });
  };

  // 单档场景（R20：与今天逐像素一致）继续用既有的 GrantedChips + CapabilityPicker
  // 单份渲染,数据源从 execTargets[0] 取。
  const { caps } = useBackendCapabilities(selectedBackend?.type);
  const skillsCapOn = caps?.has("skills") ?? false;
  const primarySkills = primaryTarget?.skills ?? [];
  const skillCatalog = useSkillCatalog(
    props.agent.id,
    primaryTarget?.agentBackendId ?? 0,
    skillsCapOn,
  );
  const [skillPickerOpen, setSkillPickerOpen] = React.useState(false);

  const handleUploadFile = async (file: File) => {
    const dataUrl = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result ?? ""));
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
    await wrap(() => props.onUploadAvatar({ id: props.agent.id, dataUrl }));
  };

  const handleDeleteAvatar = async () => {
    await wrap(() => props.onDeleteAvatar({ id: props.agent.id }));
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
    patch(
      {
        tools: tools.map((tl) =>
          tl.key === key ? { ...tl, enabled: !tl.enabled } : tl,
        ),
      },
      { immediate: true },
    );
  const removeTool = (key: string) =>
    patch(
      {
        tools: tools.map((tl) =>
          tl.key === key ? { ...tl, enabled: false } : tl,
        ),
      },
      { immediate: true },
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
    const s = primarySkills.find((x) => x.id === id);
    if (!s) return "inherit";
    return s.enabled ? "on" : "off";
  };
  const setSkillState = (id: string, next: TriState) => {
    const rest = primarySkills.filter((s) => s.id !== id);
    const nextSkills =
      next === "inherit"
        ? rest
        : [
            ...rest,
            department_svc.AgentSkillDTO.createFrom({
              id,
              enabled: next === "on",
            }),
          ];
    patchTargetSkills(0, nextSkills);
  };

  const triLabels: Record<TriState, string> = {
    inherit: t("capability.triState.inherit"),
    on: t("capability.triState.on"),
    off: t("capability.triState.off"),
  };

  const onSkills = primarySkills.filter((s) => s.enabled);
  const offSkills = primarySkills.filter((s) => !s.enabled);
  const overriddenIds = new Set(primarySkills.map((s) => s.id));
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
    patchTargetSkills(
      0,
      primarySkills.filter((s) => s.id !== id),
    );

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
            onChangeIcon={(v) => patch({ avatarIcon: v }, { immediate: true })}
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
              onChange={(e) => patch({ name: e.target.value })}
              onBlur={flush}
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
              onChange={(e) => patch({ description: e.target.value })}
              onBlur={flush}
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
                onChangeIcon={(v) =>
                  patch({ avatarIcon: v }, { immediate: true })
                }
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
                  onClick={() =>
                    patch({ avatarColor: c as AgentColor }, { immediate: true })
                  }
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

        <section data-slot="agent-section-backend">
          {noBackendBound ? (
            <Alert
              className="mb-2.5 border-status-waiting/40 bg-status-waiting-bg text-xs"
              data-testid="org-agent-no-backend"
            >
              <AlertTriangle className="size-4" aria-hidden="true" />
              <AlertTitle className="text-xs">
                {t("org.agent.backend.noBackendTitle")}
              </AlertTitle>
              <AlertDescription className="text-2xs">
                {t("org.agent.backend.noBackendDescription")}
              </AlertDescription>
            </Alert>
          ) : null}
          {builtinMissingProvider ? (
            <Alert
              className="mb-2.5 border-status-waiting/40 bg-status-waiting-bg text-xs"
              data-testid="org-agent-provider-gap"
            >
              <AlertTriangle className="size-4" aria-hidden="true" />
              <AlertTitle className="text-xs">
                {t("org.agent.backend.providerGapTitle")}
              </AlertTitle>
              <AlertDescription className="text-2xs">
                {t("org.agent.backend.providerGapDescription")}
                <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1">
                  <Button
                    type="button"
                    size="sm"
                    className="h-7 px-2.5 text-2xs"
                    onClick={() =>
                      navigate("/settings", {
                        state: { settingsPage: "llm-providers" },
                      })
                    }
                  >
                    {t("org.agent.backend.configureProvider")}
                  </Button>
                  <Button
                    type="button"
                    variant="link"
                    size="sm"
                    className="h-auto px-0 text-2xs"
                    onClick={() =>
                      navigate("/settings", {
                        state: { settingsPage: "agent-backend" },
                      })
                    }
                  >
                    {t("org.agent.backend.goAgentBackendSettings")}
                  </Button>
                </div>
              </AlertDescription>
            </Alert>
          ) : null}
          {/* 执行目标区只有这一个列表：它恒等于这台电脑当前实际的派发顺序。 */}
          <ExecTargetList
            agentName={props.agent.name}
            availability={availability.byBackendId}
            targets={listTargets}
            backends={backendsForList}
            onChange={handleExecTargetSetChange}
            onReorder={handleExecTargetReorder}
            loading={orderPending}
            saveRejected={pendingInvalid && execTargets.length === 0}
          />
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
            onChange={(e) => patch({ prompt: e.target.value })}
            onBlur={flush}
            aria-label={t("org.agent.systemPrompt")}
            className="min-h-[160px] font-mono text-xs"
          />
          <div className="flex items-center gap-1.5 text-2xs text-muted-foreground">
            <Info className="size-3" aria-hidden="true" />
            <span>{t("org.agent.systemPromptHint")}</span>
          </div>
        </section>

        {execTargets.length > 0 && (
          <section className="space-y-2.5" data-slot="agent-section-skills">
            {singleTarget ? (
              caps?.has("skills") ? (
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
                    removeLabel={(name) =>
                      t("capability.picker.remove", { name })
                    }
                    onRemove={removeSkillOverride}
                    onAdd={openSkillPicker}
                    emptyLabel={t("org.agent.skills.empty")}
                    footerNote={t("org.agent.skills.inheritNote")}
                  />
                  <CapabilityPicker
                    open={skillPickerOpen}
                    title={t("org.agent.skillPicker.title")}
                    subtitle={t("org.agent.skillPicker.subtitle")}
                    searchPlaceholder={t(
                      "org.agent.skillPicker.searchPlaceholder",
                    )}
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
              )
            ) : (
              <>
                <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {t("org.agent.skills.sectionTitle")}
                </h3>
                {execTargets.map((target, index) => (
                  <ExecTargetSkillsBlock
                    key={target.agentBackendId}
                    agentId={props.agent.id}
                    index={index}
                    total={execTargets.length}
                    backend={backendById.get(target.agentBackendId)}
                    skills={target.skills}
                    onSkillsChange={(next) => patchTargetSkills(index, next)}
                    copySources={copySourcesFor(index)}
                  />
                ))}
                <p className="font-mono text-2xs text-muted-foreground">
                  {t("org.agent.skills.inheritNote")}
                </p>
              </>
            )}
          </section>
        )}

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

      <AutoSaveStatus
        status={status}
        pendingInvalid={pendingInvalid}
        onRetry={retry}
      />

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

// backendItemFromSummary 把 AgentItem.backend（department_svc.BackendSummary，只有
// 少数字段的只读摘要）撑成 ExecTargetList/ExecTargetSkillsBlock 期望的
// agent_backend_svc.BackendItem 形状——只在 props.backends 全量列表还没覆盖这个
// backend 时才用得到这份兜底，撑出来的字段（deviceId 等）留空/假值即可：这个兜底
// 场景下这个 backend 本来就只知道这几个摘要字段。
function backendItemFromSummary(
  b: department_svc.BackendSummary,
): agent_backend_svc.BackendItem {
  return {
    id: b.id,
    type: b.type,
    name: b.name,
    llmProviderKey: "",
    llmProviderName: b.llmProviderName,
    llmProviderType: "",
    llmProviderModel: b.llmProviderModel,
    llmProviderActive: b.llmProviderActive,
    llmModelKey: "",
    cliPath: "",
    modelRoutes: {},
    sandbox: "",
    approval: "",
    envJson: "",
    reasoningEffort: "",
    defaultPermissionMode: "",
    defaultModel: "",
    openClawGatewayUrl: "",
    openClawAgentId: "",
    openClawDefaultModel: "",
    openClawSessionMode: "",
    hasToken: false,
    deviceId: "",
    deviceName: "",
    online: false,
    agentCount: 0,
    createtime: 0,
    updatetime: 0,
  } as unknown as agent_backend_svc.BackendItem;
}
