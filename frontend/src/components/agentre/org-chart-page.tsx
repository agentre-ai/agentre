import * as React from "react";
import {
  FolderPlus,
  List,
  Network,
  Plus,
  RotateCcw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";

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
      return `${totalAgents} 个 Agent · 列表视图`;
    }
    const top = departments.filter((d) => d.parentId === 0).length;
    const sub = departments.length - top;
    return `${top} 部门 · ${sub} 子部门 · ${totalAgents} Agent`;
  }, [departments, agents, view.viewMode]);

  const zoomPct = Math.round(view.zoom * 100);

  if (loading) {
    return (
      <div
        className="flex min-h-0 min-w-0 flex-1 items-center justify-center text-muted-foreground"
        data-slot="org-chart-loading"
      >
        正在加载组织架构…
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
          <span className="text-base font-semibold">组织架构</span>
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
              aria-label="缩小"
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
              aria-label="放大"
              onClick={view.zoomIn}
            >
              <ZoomIn className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label="重置缩放"
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
            aria-label="树状视图"
            onClick={() => view.setViewMode("tree")}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-2xs font-medium transition-colors",
              view.viewMode === "tree"
                ? "bg-card shadow-xs"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Network className="size-3" />
            树状
          </button>
          <button
            type="button"
            aria-pressed={view.viewMode === "list"}
            aria-label="列表视图"
            onClick={() => view.setViewMode("list")}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-2xs font-medium transition-colors",
              view.viewMode === "list"
                ? "bg-card shadow-xs"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <List className="size-3" />
            列表
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
            新建部门
          </Button>
        )}
        <Button
          size="sm"
          disabled={departments.length === 0 && agents.length === 0}
          title={
            departments.length === 0 && agents.length === 0
              ? "暂无可挂载节点"
              : undefined
          }
          onClick={() => {
            setNewAgentParentDeptId(0);
            setNewAgentOpen(true);
          }}
        >
          <Plus className="size-3.5 mr-1" />
          新建 Agent
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
      选择一个部门或 Agent 查看详情
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
      title="新建部门"
      description="选择父级、图标和主题色，创建后会出现在组织架构中。"
      bodyClassName="flex flex-col gap-4"
      onSubmit={handleSubmit}
      footer={
        <>
          <Button type="button" variant="outline" onClick={props.onClose}>
            取消
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            创建
          </Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">名称</span>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          aria-label="new-dept-name"
        />
      </label>
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">父部门</span>
        <Select
          value={String(parentId)}
          onValueChange={(v) => setParentId(Number(v))}
        >
          <SelectTrigger aria-label="new-dept-parent">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">公司顶层</SelectItem>
            {props.departments.map((d) => (
              <SelectItem key={d.id} value={String(d.id)}>
                {d.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </label>
      <div className="flex flex-col gap-2">
        <span className="text-2xs text-muted-foreground">图标</span>
        <IconPicker
          value={icon}
          onChange={setIcon}
          accentColor={accentColor}
          ariaLabel="新部门图标"
        />
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-2xs text-muted-foreground">主题色</span>
        <div
          className="grid grid-cols-5 gap-2"
          role="radiogroup"
          aria-label="主题色"
        >
          {agentColorOrder.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={accentColor === c}
              aria-label={`主题色 ${c}`}
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
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [avatarColor, setAvatarColor] = React.useState<AgentColor>("agent-1");
  const [avatarIcon, setAvatarIcon] = React.useState<string>("");
  const placementOptions = React.useMemo(
    () => [
      ...props.departments.map((d) => ({
        value: `department:${d.id}`,
        label: `部门 · ${d.name}`,
      })),
      ...props.agents.map((a) => ({
        value: `agent:${a.id}`,
        label: `Agent · ${a.name}`,
      })),
    ],
    [props.departments, props.agents],
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
    (parsedPlacement.departmentId > 0 || parsedPlacement.parentAgentId > 0) &&
    backendId > 0;

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
      title="新建 Agent"
      description="设置 Agent 的名称、挂载位置和执行后端。"
      bodyClassName="flex flex-col gap-4"
      onSubmit={handleSubmit}
      footer={
        <>
          <Button type="button" variant="outline" onClick={props.onClose}>
            取消
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            创建
          </Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-2xs text-muted-foreground">名称</span>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          aria-label="new-agent-name"
        />
      </label>
      <label className="flex flex-col gap-1.5 text-xs">
        <span className="text-2xs text-muted-foreground">
          简介 <span className="opacity-60">（可选）</span>
        </span>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          aria-label="new-agent-description"
        />
      </label>
      <div className="flex flex-col gap-1.5">
        <span className="text-2xs text-muted-foreground">头像</span>
        <div className="flex items-center gap-3">
          <AgentAvatarPicker
            name={name || "Agent"}
            avatarColor={avatarColor}
            avatarIcon={avatarIcon}
            avatarDataUrl=""
            onChangeIcon={setAvatarIcon}
            allowUpload={false}
            triggerSize="lg"
          />
          <span className="font-mono text-2xs text-muted-foreground">
            点击头像选图标，创建后可在详情页上传图片
          </span>
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-2xs text-muted-foreground">头像配色</span>
        <div
          className="grid grid-cols-5 gap-2"
          role="radiogroup"
          aria-label="头像配色"
        >
          {agentColorOrder.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={avatarColor === c}
              aria-label={`头像配色 ${c}`}
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
        <span className="text-2xs text-muted-foreground">挂载到</span>
        <Select value={placement} onValueChange={setPlacement}>
          <SelectTrigger aria-label="new-agent-placement">
            <SelectValue placeholder="选择部门或上级 Agent" />
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
        <span className="text-2xs text-muted-foreground">后端</span>
        <Select
          value={backendId > 0 ? String(backendId) : ""}
          onValueChange={(v) => setBackendId(Number(v))}
        >
          <SelectTrigger aria-label="new-agent-backend">
            <SelectValue placeholder="选择后端" />
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
