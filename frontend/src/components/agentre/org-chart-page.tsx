import * as React from "react";
import type { TFunction } from "i18next";
import {
  FolderPlus,
  List,
  Network,
  Plus,
  RotateCcw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

import { agent_svc, department_svc } from "../../../wailsjs/go/models";
import {
  agentColorClassNames,
  agentColorOrder,
  type AgentColor,
} from "./types";
import { AgentreDialog } from "./app-dialog";
import { AgentAvatarPicker, IconPicker } from "./icon-picker";
import { type OrgAgent, type OrgDepartment } from "./org/types";
import { OrgDetailAgent } from "./org/org-detail-agent";
import { OrgDetailDepartment } from "./org/org-detail-department";
import { OrgList } from "./org/org-list";
import { OrgTree } from "./org/org-tree";
import { useOrgData } from "./org/use-org-data";
import { useOrgTreeView } from "./org/use-org-tree-view";

export function OrgChartPage() {
  const { t } = useTranslation();
  const {
    loading,
    error,
    departments,
    agents,
    backends,
    moveAgent,
    moveDepartment,
    updateDepartment,
    deleteDepartment,
    updateAgent,
    deleteAgent,
    uploadAgentAvatar,
    deleteAgentAvatar,
    createDepartment,
    createAgent,
  } = useOrgData();
  const view = useOrgTreeView();

  const [newDeptOpen, setNewDeptOpen] = React.useState(false);
  const [newAgentOpen, setNewAgentOpen] = React.useState(false);
  // 当从 DeptEditDrawer 的 “+ 添加 Agent 或子部门” 触发时，预选目标部门。
  const [newAgentParentDeptId, setNewAgentParentDeptId] =
    React.useState<number>(0);
  const [newSubDeptParentId, setNewSubDeptParentId] = React.useState<number>(0);

  const agentById = React.useMemo(
    () =>
      Object.fromEntries(agents.map((a) => [a.id, a])) as Record<
        number,
        OrgAgent
      >,
    [agents],
  );
  const departmentById = React.useMemo(
    () =>
      Object.fromEntries(departments.map((d) => [d.id, d])) as Record<
        number,
        OrgDepartment
      >,
    [departments],
  );

  const summaryText = React.useMemo(() => {
    const totalAgents = agents.length;
    if (view.viewMode === "list") {
      return t("org.chart.summary.list", { count: totalAgents });
    }
    const top = departments.filter((d) => d.parentId === 0).length;
    const sub = departments.length - top;
    return t("org.chart.summary.tree", {
      agents: totalAgents,
      departments: top,
      subDepartments: sub,
    });
  }, [departments, agents, t, view.viewMode]);

  const zoomPct = Math.round(view.zoom * 100);

  if (loading) {
    return (
      <div
        className="flex min-h-0 min-w-0 flex-1 items-center justify-center text-muted-foreground"
        data-slot="org-chart-loading"
      >
        {t("org.chart.loading")}
      </div>
    );
  }

  if (error) {
    return (
      <div
        className="flex min-h-0 min-w-0 flex-1 items-center justify-center text-destructive"
        data-slot="org-chart-error"
      >
        {error}
      </div>
    );
  }

  const detailContent = renderDetail({
    selected: view.selected,
    agentById,
    departmentById,
    departments,
    agents,
    backends,
    onSelect: view.setSelected,
    onClose: () => view.setSelected(null),
    updateDepartment,
    moveDepartment,
    deleteDepartment,
    updateAgent,
    deleteAgent,
    uploadAgentAvatar,
    deleteAgentAvatar,
    t,
    onAddAgent: (deptId) => {
      setNewAgentParentDeptId(deptId);
      setNewAgentOpen(true);
    },
    onAddSubDepartment: (deptId) => {
      setNewSubDeptParentId(deptId);
      setNewDeptOpen(true);
    },
  });

  return (
    <main
      className="flex min-h-0 min-w-0 flex-1 flex-col"
      data-slot="org-chart-page"
    >
      <header
        className="flex h-[60px] shrink-0 items-center gap-3 border-b bg-background px-5"
        data-slot="org-header"
      >
        <div className="flex flex-col">
          <span className="text-base font-semibold">
            {t("org.chart.title")}
          </span>
          <span className="font-mono text-2xs text-muted-foreground">
            {summaryText}
          </span>
        </div>
        <div className="flex-1" />
        {view.viewMode === "tree" && (
          <div className="flex items-center gap-1 rounded-md border bg-card px-1 py-0.5">
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label={t("org.chart.zoom.out")}
              onClick={view.zoomOut}
            >
              <ZoomOut className="size-3.5" />
            </Button>
            <span className="inline-flex h-6 w-12 items-center justify-center rounded bg-muted font-mono text-2xs">
              {zoomPct}%
            </span>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label={t("org.chart.zoom.in")}
              onClick={view.zoomIn}
            >
              <ZoomIn className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label={t("org.chart.zoom.reset")}
              onClick={view.zoomReset}
            >
              <RotateCcw className="size-3.5" />
            </Button>
          </div>
        )}
        <div className="flex rounded-md bg-secondary p-0.5">
          <button
            type="button"
            aria-pressed={view.viewMode === "tree"}
            aria-label={t("org.chart.view.treeAria")}
            onClick={() => view.setViewMode("tree")}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-2xs font-medium transition-colors",
              view.viewMode === "tree"
                ? "bg-card shadow-xs"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Network className="size-3" />
            {t("org.chart.view.tree")}
          </button>
          <button
            type="button"
            aria-pressed={view.viewMode === "list"}
            aria-label={t("org.chart.view.listAria")}
            onClick={() => view.setViewMode("list")}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-2xs font-medium transition-colors",
              view.viewMode === "list"
                ? "bg-card shadow-xs"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <List className="size-3" />
            {t("org.chart.view.list")}
          </button>
        </div>
        {view.viewMode === "tree" && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setNewSubDeptParentId(0);
              setNewDeptOpen(true);
            }}
          >
            <FolderPlus className="size-3.5 mr-1" />
            {t("org.chart.actions.newDepartment")}
          </Button>
        )}
        <Button
          size="sm"
          disabled={departments.length === 0 && agents.length === 0}
          title={
            departments.length === 0 && agents.length === 0
              ? t("org.chart.empty.noMountNodes")
              : undefined
          }
          onClick={() => {
            setNewAgentParentDeptId(0);
            setNewAgentOpen(true);
          }}
        >
          <Plus className="size-3.5 mr-1" />
          {t("org.chart.actions.newAgent")}
        </Button>
      </header>

      <div className="relative flex min-h-0 min-w-0 flex-1 overflow-hidden">
        <div className="flex min-w-0 flex-1 overflow-hidden">
          {view.viewMode === "tree" ? (
            <OrgTree
              departments={departments}
              agents={agents}
              selected={view.selected}
              collapse={view.collapse}
              zoom={view.zoom}
              pan={view.pan}
              onSelect={view.setSelected}
              onToggleCollapse={view.toggleCollapse}
              onPanChange={view.setPan}
              onZoomChange={view.setZoom}
              onMoveAgent={(id, placement) => {
                void moveAgent({
                  id,
                  newDepartmentId: placement.departmentId,
                  newParentAgentId: placement.parentAgentId,
                  newSortOrder: 0,
                });
              }}
              onMoveDepartment={(id, parentId) => {
                void moveDepartment({
                  id,
                  newParentId: parentId,
                  newSortOrder: 0,
                });
              }}
            />
          ) : (
            <OrgList
              departments={departments}
              agents={agents}
              backends={backends}
              selected={view.selected}
              onSelect={view.setSelected}
            />
          )}
        </div>

        <aside
          className="relative w-[380px] shrink-0 overflow-y-auto border-l bg-sidebar"
          data-slot="org-detail-panel"
        >
          {detailContent}
        </aside>
      </div>

      <NewDepartmentDialog
        open={newDeptOpen}
        departments={departments}
        agents={agents}
        defaultParentId={newSubDeptParentId}
        onSubmit={async (req) => {
          await createDepartment(
            department_svc.CreateDepartmentRequest.createFrom(req),
          );
          setNewDeptOpen(false);
        }}
        onClose={() => setNewDeptOpen(false)}
      />
      <NewAgentDialog
        open={newAgentOpen}
        departments={departments}
        agents={agents}
        backends={backends}
        defaultDepartmentId={newAgentParentDeptId}
        onSubmit={async (req) => {
          await createAgent(agent_svc.CreateAgentRequest.createFrom(req));
          setNewAgentOpen(false);
        }}
        onClose={() => setNewAgentOpen(false)}
      />
    </main>
  );
}

type RenderDetailArgs = {
  selected: ReturnType<typeof useOrgTreeView>["selected"];
  agentById: Record<number, OrgAgent>;
  departmentById: Record<number, OrgDepartment>;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: ReturnType<typeof useOrgData>["backends"];
  onSelect: (sel: ReturnType<typeof useOrgTreeView>["selected"]) => void;
  onClose: () => void;
  updateDepartment: ReturnType<typeof useOrgData>["updateDepartment"];
  moveDepartment: ReturnType<typeof useOrgData>["moveDepartment"];
  deleteDepartment: ReturnType<typeof useOrgData>["deleteDepartment"];
  updateAgent: ReturnType<typeof useOrgData>["updateAgent"];
  deleteAgent: ReturnType<typeof useOrgData>["deleteAgent"];
  uploadAgentAvatar: ReturnType<typeof useOrgData>["uploadAgentAvatar"];
  deleteAgentAvatar: ReturnType<typeof useOrgData>["deleteAgentAvatar"];
  t: TFunction;
  onAddAgent: (deptId: number) => void;
  onAddSubDepartment: (deptId: number) => void;
};

function renderDetail(args: RenderDetailArgs): React.ReactNode {
  const { selected } = args;
  if (selected?.kind === "department" && args.departmentById[selected.id]) {
    return (
      <OrgDetailDepartment
        key={`dept-${selected.id}`}
        department={args.departmentById[selected.id]}
        allDepartments={args.departments}
        allAgents={args.agents}
        leadCandidates={args.agents.filter(
          (a) => a.departmentId === selected.id && (a.parentAgentId ?? 0) === 0,
        )}
        onUpdate={(req) => args.updateDepartment(req)}
        onMove={(req) => args.moveDepartment(req)}
        onDelete={(req) => args.deleteDepartment(req)}
        onSelect={(sel) => args.onSelect(sel)}
        onClose={args.onClose}
        onAddAgent={() => args.onAddAgent(selected.id)}
        onAddSubDepartment={() => args.onAddSubDepartment(selected.id)}
      />
    );
  }
  if (selected?.kind === "agent" && args.agentById[selected.id]) {
    return (
      <OrgDetailAgent
        key={`agent-${selected.id}`}
        agent={args.agentById[selected.id]}
        departments={args.departments}
        agents={args.agents}
        backends={args.backends}
        isLeadOf={
          args.departments.find((d) => d.leadAgentId === selected.id) ?? null
        }
        onUpdate={(req) => args.updateAgent(req)}
        onDelete={(req) => args.deleteAgent(req)}
        onUploadAvatar={(req) => args.uploadAgentAvatar(req)}
        onDeleteAvatar={(req) => args.deleteAgentAvatar(req)}
        onClose={args.onClose}
      />
    );
  }
  return (
    <div
      className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground"
      data-slot="org-detail-empty"
    >
      {args.t("org.chart.detail.empty")}
    </div>
  );
}

type NewDeptProps = {
  open: boolean;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  defaultParentId?: number;
  onSubmit: (req: {
    name: string;
    description: string;
    icon: string;
    accentColor: string;
    parentId: number;
  }) => Promise<void>;
  onClose: () => void;
};

function NewDepartmentDialog(props: NewDeptProps) {
  if (!props.open) return null;
  return <NewDepartmentDialogBody {...props} />;
}

function NewDepartmentDialogBody(props: NewDeptProps) {
  const { t } = useTranslation();
  const [name, setName] = React.useState("");
  const [icon, setIcon] = React.useState("hammer");
  const [accentColor, setAccentColor] = React.useState<AgentColor>("agent-2");
  const [parentId, setParentId] = React.useState<number>(
    props.defaultParentId ?? 0,
  );

  const canSubmit = name.trim().length > 0;

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!canSubmit) return;
    await props.onSubmit({
      name: name.trim(),
      description: "",
      icon,
      accentColor,
      parentId,
    });
  }

  return (
    <AgentreDialog
      open={props.open}
      onOpenChange={(o) => !o && props.onClose()}
      title={t("org.chart.actions.newDepartment")}
      description={t("org.chart.newDepartment.description")}
      bodyClassName="flex flex-col gap-4"
      onSubmit={handleSubmit}
      footer={
        <>
          <Button type="button" variant="outline" onClick={props.onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            {t("common.create")}
          </Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.name")}
        </span>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          aria-label={t("org.department.name")}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.parent")}
        </span>
        <Select
          value={String(parentId)}
          onValueChange={(v) => setParentId(Number(v))}
        >
          <SelectTrigger aria-label={t("org.department.parent")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">{t("org.department.topLevel")}</SelectItem>
            {props.departments.map((d) => (
              <SelectItem key={d.id} value={String(d.id)}>
                {d.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </label>
      <div className="flex flex-col gap-2">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.icon")}
        </span>
        <IconPicker
          value={icon}
          onChange={setIcon}
          accentColor={accentColor}
          ariaLabel={t("org.chart.newDepartment.iconAria")}
        />
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.themeColor")}
        </span>
        <div
          className="grid grid-cols-5 gap-2"
          role="radiogroup"
          aria-label={t("org.department.themeColor")}
        >
          {agentColorOrder.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={accentColor === c}
              aria-label={t("org.department.themeColorNamed", { color: c })}
              onClick={() => setAccentColor(c)}
              className={cn(
                "size-6 rounded-full ring-offset-2 transition-all",
                agentColorClassNames[c],
                accentColor === c && "size-7 ring-2 ring-primary",
              )}
            />
          ))}
        </div>
      </div>
    </AgentreDialog>
  );
}

type NewAgentProps = {
  open: boolean;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: ReturnType<typeof useOrgData>["backends"];
  defaultDepartmentId?: number;
  onSubmit: (req: {
    name: string;
    description: string;
    avatarColor: string;
    avatarIcon: string;
    departmentId: number;
    parentAgentId: number;
    agentBackendId: number;
    prompt: string[];
    skills: { label: string; enabled: boolean }[];
  }) => Promise<void>;
  onClose: () => void;
};

function NewAgentDialog(props: NewAgentProps) {
  if (!props.open) return null;
  return <NewAgentDialogBody {...props} />;
}

function NewAgentDialogBody(props: NewAgentProps) {
  const { t } = useTranslation();
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [avatarColor, setAvatarColor] = React.useState<AgentColor>("agent-1");
  const [avatarIcon, setAvatarIcon] = React.useState<string>("");
  const placementOptions = React.useMemo(
    () => [
      ...props.departments.map((d) => ({
        value: `department:${d.id}`,
        label: t("org.chart.newAgent.departmentOption", { name: d.name }),
      })),
      ...props.agents.map((a) => ({
        value: `agent:${a.id}`,
        label: t("org.chart.newAgent.agentOption", { name: a.name }),
      })),
    ],
    [props.departments, props.agents, t],
  );
  const initialPlacement = React.useMemo(() => {
    const preset = props.defaultDepartmentId
      ? `department:${props.defaultDepartmentId}`
      : null;
    if (preset && placementOptions.some((opt) => opt.value === preset)) {
      return preset;
    }
    return placementOptions[0]?.value ?? "";
  }, [props.defaultDepartmentId, placementOptions]);
  const [placement, setPlacement] = React.useState<string>(initialPlacement);
  const [backendId, setBackendId] = React.useState<number>(
    props.backends[0]?.id ?? 0,
  );
  const parsedPlacement = parsePlacement(placement);

  const canSubmit =
    name.trim().length > 0 &&
    (parsedPlacement.departmentId > 0 || parsedPlacement.parentAgentId > 0);

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!canSubmit) return;
    await props.onSubmit({
      name: name.trim(),
      description: description.trim(),
      avatarColor,
      avatarIcon,
      departmentId: parsedPlacement.departmentId,
      parentAgentId: parsedPlacement.parentAgentId,
      agentBackendId: backendId,
      prompt: [],
      skills: [],
    });
  }

  return (
    <AgentreDialog
      open={props.open}
      onOpenChange={(o) => !o && props.onClose()}
      title={t("org.chart.actions.newAgent")}
      description={t("org.chart.newAgent.description")}
      bodyClassName="flex flex-col gap-4"
      onSubmit={handleSubmit}
      footer={
        <>
          <Button type="button" variant="outline" onClick={props.onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            {t("common.create")}
          </Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.name")}
        </span>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          aria-label={t("org.department.name")}
        />
      </label>
      <label className="flex flex-col gap-1.5 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.department.description")}{" "}
          <span className="opacity-60">
            {t("org.chart.newAgent.optionalSuffix")}
          </span>
        </span>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          aria-label={t("org.department.description")}
        />
      </label>
      <div className="flex flex-col gap-1.5">
        <span className="text-2xs text-muted-foreground">
          {t("org.chart.newAgent.avatar")}
        </span>
        <div className="flex items-center gap-3">
          <AgentAvatarPicker
            name={name || t("org.agent.fallbackName")}
            avatarColor={avatarColor}
            avatarIcon={avatarIcon}
            avatarDataUrl=""
            onChangeIcon={setAvatarIcon}
            allowUpload={false}
            triggerSize="lg"
          />
          <span className="font-mono text-2xs text-muted-foreground">
            {t("org.chart.newAgent.avatarHint")}
          </span>
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-2xs text-muted-foreground">
          {t("org.chart.newAgent.avatarColor")}
        </span>
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
                "size-6 rounded-full ring-offset-2 transition-all",
                agentColorClassNames[c],
                avatarColor === c && "size-7 ring-2 ring-primary",
              )}
            />
          ))}
        </div>
      </div>
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.chart.newAgent.placement")}
        </span>
        <Select value={placement} onValueChange={setPlacement}>
          <SelectTrigger aria-label={t("org.chart.newAgent.placement")}>
            <SelectValue placeholder={t("org.chart.newAgent.placement")} />
          </SelectTrigger>
          <SelectContent>
            {placementOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </label>
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">
          {t("org.chart.newAgent.backend")}
        </span>
        <Select
          value={backendId > 0 ? String(backendId) : ""}
          onValueChange={(v) => setBackendId(Number(v))}
        >
          <SelectTrigger aria-label={t("org.chart.newAgent.backend")}>
            <SelectValue placeholder={t("org.chart.newAgent.backend")} />
          </SelectTrigger>
          <SelectContent>
            {props.backends.map((b) => (
              <SelectItem key={b.id} value={String(b.id)}>
                {b.name} · {b.type}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </label>
    </AgentreDialog>
  );
}

function parsePlacement(value: string): {
  departmentId: number;
  parentAgentId: number;
} {
  const [kind, rawId] = value.split(":");
  const id = Number(rawId);
  if (!Number.isFinite(id) || id <= 0) {
    return { departmentId: 0, parentAgentId: 0 };
  }
  if (kind === "department") {
    return { departmentId: id, parentAgentId: 0 };
  }
  if (kind === "agent") {
    return { departmentId: 0, parentAgentId: id };
  }
  return { departmentId: 0, parentAgentId: 0 };
}
