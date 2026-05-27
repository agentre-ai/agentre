import * as React from "react";
import {
  AlertTriangle,
  ArrowUpRight,
  ChevronRight,
  CornerDownRight,
  Crown,
  FolderPlus,
  History,
  Plus,
  Trash2,
  UserPlus,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
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
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

import {
  agentColorClassNames,
  agentColorOrder,
  type AgentColor,
} from "../types";
import type { department_svc } from "../../../../wailsjs/go/models";

import { IconPicker } from "../icon-picker";
import { AgentAvatar } from "../primitives";

import {
  iconForKey,
  safeAgentColor,
  type OrgAgent,
  type OrgDepartment,
} from "./types";

type Props = {
  department: OrgDepartment;
  allDepartments: OrgDepartment[];
  allAgents: OrgAgent[];
  leadCandidates: OrgAgent[];
  onUpdate: (req: department_svc.UpdateDepartmentRequest) => Promise<unknown>;
  onMove: (req: department_svc.MoveDepartmentRequest) => Promise<unknown>;
  onDelete: (req: department_svc.DeleteDepartmentRequest) => Promise<unknown>;
  onSelect: (
    sel: { kind: "agent"; id: number } | { kind: "department"; id: number },
  ) => void;
  onClose: () => void;
  onAddAgent?: () => void;
  onAddSubDepartment?: () => void;
};

const leadTextColorClassNames: Record<AgentColor, string> = {
  "agent-1": "text-agent-1",
  "agent-2": "text-agent-2",
  "agent-3": "text-agent-3",
  "agent-4": "text-agent-4",
  "agent-5": "text-agent-5",
  "agent-6": "text-agent-6",
  "agent-7": "text-agent-7",
  "agent-8": "text-agent-8",
  "agent-9": "text-agent-9",
  "agent-10": "text-agent-10",
  neutral: "text-foreground",
};

export function OrgDetailDepartment(props: Props) {
  const [name, setName] = React.useState(props.department.name);
  const [description, setDescription] = React.useState(
    props.department.description,
  );
  const [icon, setIcon] = React.useState(props.department.icon || "puzzle");
  const [accentColor, setAccentColor] = React.useState<AgentColor>(
    safeAgentColor(props.department.accentColor),
  );
  const [leadAgentId, setLeadAgentId] = React.useState<number>(
    props.department.leadAgentId,
  );
  const [parentId, setParentId] = React.useState<number>(
    props.department.parentId,
  );
  const [deletePromptOpen, setDeletePromptOpen] = React.useState(false);
  const [strategy, setStrategy] = React.useState<"reparent" | "cascade">(
    "reparent",
  );

  const dirty =
    name !== props.department.name ||
    description !== props.department.description ||
    icon !== (props.department.icon || "puzzle") ||
    accentColor !== safeAgentColor(props.department.accentColor) ||
    leadAgentId !== props.department.leadAgentId ||
    parentId !== props.department.parentId;

  const handleSave = React.useCallback(async () => {
    await props.onUpdate({
      id: props.department.id,
      name,
      description,
      icon,
      accentColor,
      leadAgentId,
    });
    if (parentId !== props.department.parentId) {
      await props.onMove({
        id: props.department.id,
        newParentId: parentId,
        newSortOrder: 0,
      });
    }
  }, [name, description, icon, accentColor, leadAgentId, parentId, props]);

  const handleConfirmDelete = React.useCallback(async () => {
    await props.onDelete({ id: props.department.id, strategy });
    props.onClose();
  }, [strategy, props]);

  const path = buildPath(props.department, props.allDepartments);
  const parentOptions = props.allDepartments.filter(
    (d) =>
      d.id !== props.department.id &&
      !isDescendant(d.id, props.department.id, props.allDepartments),
  );
  const directAgents = props.allAgents.filter(
    (a) => a.departmentId === props.department.id,
  );
  const directDepts = props.allDepartments.filter(
    (d) => d.parentId === props.department.id,
  );
  const selectedParent =
    parentId > 0
      ? (props.allDepartments.find((d) => d.id === parentId) ?? null)
      : null;
  const selectedLead =
    props.leadCandidates.find((a) => a.id === leadAgentId) ?? null;
  const iconNode = React.createElement(iconForKey(icon), {
    className: "size-5 text-white",
    "aria-hidden": true,
  });

  return (
    <div
      data-slot="org-detail-department"
      className="flex h-full flex-col bg-card"
    >
      <header className="space-y-3 border-b border-border bg-card px-5 py-4">
        <div className="flex items-start gap-3">
          <span
            className={cn(
              "inline-flex size-10 shrink-0 items-center justify-center rounded-lg",
              agentColorClassNames[accentColor],
            )}
          >
            {iconNode}
          </span>
          <div className="flex-1 min-w-0">
            <div className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              编辑部门
            </div>
            <div className="truncate text-base font-semibold">
              {props.department.name}
            </div>
          </div>
          <div className="flex shrink-0 gap-1">
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              aria-label="删除部门"
              onClick={() => setDeletePromptOpen(true)}
            >
              <Trash2 className="size-4 text-destructive" />
            </Button>
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              aria-label="关闭"
              onClick={props.onClose}
            >
              <X className="size-4" />
            </Button>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-1.5 text-2xs text-muted-foreground">
          <CornerDownRight className="size-3" aria-hidden="true" />
          <span>路径</span>
          {path.map((node, i) => (
            <React.Fragment key={node.id}>
              {i > 0 && (
                <ChevronRight
                  className="size-3 text-muted-foreground"
                  aria-hidden="true"
                />
              )}
              <span
                className={cn(
                  "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 font-mono text-2xs",
                  i === path.length - 1
                    ? "border border-primary bg-primary-soft text-primary-text"
                    : "bg-secondary text-foreground",
                )}
              >
                {node.name}
              </span>
            </React.Fragment>
          ))}
        </div>
      </header>

      <div className="flex-1 space-y-6 overflow-y-auto px-5 py-5">
        <section className="space-y-4" data-slot="dept-section-basic">
          <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            基本信息
          </h3>
          <div className="space-y-1.5">
            <label className="block text-2xs text-muted-foreground">名称</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              aria-label="dept-name"
            />
          </div>
          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <label className="text-2xs text-muted-foreground">简介</label>
              <span className="font-mono text-2xs text-muted-foreground">
                可选
              </span>
            </div>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              aria-label="dept-description"
            />
          </div>
          <div
            className="flex items-start gap-2.5"
            data-slot="dept-icon-theme-row"
          >
            <div className="flex w-[150px] shrink-0 flex-col gap-1.5">
              <label className="block text-2xs text-muted-foreground">
                图标
              </label>
              <IconPicker
                value={icon}
                onChange={setIcon}
                accentColor={accentColor}
                ariaLabel="部门图标"
                className="h-[38px] px-2.5 py-1.5"
              />
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="block text-2xs text-muted-foreground">
                主题色
              </label>
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
          </div>
        </section>

        <section
          className="flex flex-col gap-2"
          data-slot="dept-section-parent"
        >
          <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            归属
          </h3>
          <Select
            value={String(parentId)}
            onValueChange={(v) => setParentId(Number(v))}
          >
            <SelectTrigger aria-label="dept-parent" className="h-auto py-2">
              {selectedParent ? (
                <DepartmentSelectPreview department={selectedParent} />
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <span
                    className="inline-flex size-[22px] shrink-0 items-center justify-center rounded bg-primary text-primary-foreground"
                    aria-hidden="true"
                  >
                    <Crown className="size-3" />
                  </span>
                  <span className="truncate text-sm font-medium">
                    公司顶层（CEO 之下）
                  </span>
                </div>
              )}
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0">
                <span
                  className="inline-flex size-[22px] shrink-0 items-center justify-center rounded bg-primary text-primary-foreground"
                  aria-hidden="true"
                >
                  <Crown className="size-3" />
                </span>
                <span>公司顶层（CEO 之下）</span>
              </SelectItem>
              {parentOptions.map((d) => (
                <SelectItem key={d.id} value={String(d.id)}>
                  <DepartmentIconBadge
                    icon={d.icon}
                    accentColor={d.accentColor}
                    className="size-[22px] rounded"
                    iconClassName="size-3"
                  />
                  <span>{d.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </section>

        <section className="flex flex-col gap-2" data-slot="dept-section-lead">
          <div className="flex items-center justify-between">
            <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              Leader
            </h3>
            <span className="font-mono text-2xs text-muted-foreground">
              从直属 Agent 中选
            </span>
          </div>
          <Select
            value={String(leadAgentId)}
            onValueChange={(v) => setLeadAgentId(Number(v))}
          >
            <SelectTrigger aria-label="dept-lead" className="h-auto py-2">
              {selectedLead ? (
                <LeaderSelectPreview agent={selectedLead} />
              ) : (
                <span className="text-xs text-muted-foreground">未指定</span>
              )}
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0">未指定</SelectItem>
              {props.leadCandidates.map((a) => (
                <SelectItem key={a.id} value={String(a.id)}>
                  {a.name}
                  {a.description && ` · ${a.description}`}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </section>

        <section
          className="flex flex-col gap-2"
          data-slot="dept-section-members"
        >
          <div className="flex items-center justify-between">
            <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              成员
            </h3>
            <span className="font-mono text-2xs text-muted-foreground">
              {directAgents.length} 直属 Agent · {directDepts.length} 子部门
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            {directAgents.map((a) => {
              const agentColor = safeAgentColor(a.avatarColor);
              const isLead = a.id === leadAgentId;
              return (
                <button
                  key={`a-${a.id}`}
                  type="button"
                  onClick={() => props.onSelect({ kind: "agent", id: a.id })}
                  className="flex w-full items-center gap-2.5 rounded-md border border-border bg-card px-3 py-2 text-left text-sm hover:bg-accent"
                  aria-label={`查看 Agent ${a.name}`}
                >
                  <AgentAvatar
                    name={a.name}
                    color={agentColor}
                    avatarDataUrl={a.avatarDataUrl}
                    avatarIcon={a.avatarIcon}
                    className="size-7 rounded-md text-xs"
                  />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className="truncate text-xs font-semibold text-foreground">
                        {a.name}
                      </span>
                      {isLead && <LeadBadge color={agentColor} compact />}
                    </span>
                    <span className="truncate font-mono text-2xs text-muted-foreground">
                      {agentMemberDescription(a)}
                    </span>
                  </span>
                  <ArrowUpRight
                    className="size-3 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                </button>
              );
            })}
            {directDepts.map((d) => (
              <button
                key={`d-${d.id}`}
                type="button"
                onClick={() => props.onSelect({ kind: "department", id: d.id })}
                className="flex w-full items-center gap-2.5 rounded-md border border-border bg-card px-3 py-2 text-left text-sm hover:bg-accent"
                aria-label={`查看部门 ${d.name}`}
              >
                <DepartmentIconBadge
                  icon={d.icon}
                  accentColor={d.accentColor}
                  className="size-7 rounded-md"
                  iconClassName="size-3.5"
                />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="truncate text-xs font-semibold text-foreground">
                    {d.name}
                  </span>
                  <span className="truncate font-mono text-2xs text-muted-foreground">
                    {departmentMemberDescription(d)}
                  </span>
                </span>
                <ArrowUpRight
                  className="size-3 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
              </button>
            ))}
            {directAgents.length === 0 && directDepts.length === 0 && (
              <div className="rounded-md border border-dashed border-border px-3 py-2 text-center text-2xs text-muted-foreground">
                暂无直属成员
              </div>
            )}
            {(props.onAddAgent || props.onAddSubDepartment) && (
              <div
                role="group"
                aria-label="添加 Agent 或子部门"
                className="flex min-h-[38px] items-center gap-1.5 rounded-md border border-dashed border-border bg-background/30 px-3 py-2"
              >
                <Plus
                  className="size-3 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <span className="flex-1 text-center text-xs text-muted-foreground">
                  添加 Agent 或子部门
                </span>
                {props.onAddAgent && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 gap-1 px-2 text-2xs"
                    onClick={props.onAddAgent}
                  >
                    <UserPlus className="size-3" aria-hidden="true" />
                    Agent
                  </Button>
                )}
                {props.onAddSubDepartment && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 gap-1 px-2 text-2xs"
                    onClick={props.onAddSubDepartment}
                  >
                    <FolderPlus className="size-3" aria-hidden="true" />
                    子部门
                  </Button>
                )}
              </div>
            )}
          </div>
        </section>
      </div>

      <footer className="flex items-center gap-2 border-t border-border bg-secondary/40 px-5 py-3">
        <span className="flex flex-1 items-center gap-1.5 font-mono text-2xs text-muted-foreground">
          <History className="size-3" aria-hidden="true" />
          {dirty ? "未保存的修改" : "已保存"}
        </span>
        <Button variant="outline" size="sm" onClick={props.onClose}>
          取消
        </Button>
        <Button size="sm" disabled={!dirty} onClick={handleSave}>
          保存
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
                <span>确认删除「{props.department.name}」？</span>
              </DialogTitle>
              <DialogDescription>
                将按所选策略递归处理，该操作不可撤销。
              </DialogDescription>
            </DialogHeader>
            <DialogBody className="space-y-2.5">
              <h4 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                选择如何处理子项
              </h4>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-2.5 rounded-md border bg-card px-3 py-2.5 transition-colors",
                  strategy === "reparent"
                    ? "border-primary ring-1 ring-primary"
                    : "border-border",
                )}
              >
                <input
                  type="radio"
                  name="strategy"
                  value="reparent"
                  checked={strategy === "reparent"}
                  onChange={() => setStrategy("reparent")}
                  className="mt-0.5"
                />
                <div className="flex-1">
                  <div className="text-sm font-semibold text-foreground">
                    成员上挂到父部门
                  </div>
                  <div className="text-2xs text-muted-foreground">
                    推荐 / 安全 · 直属子项移动到当前部门的父级
                  </div>
                </div>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-2.5 rounded-md border bg-card px-3 py-2.5 transition-colors",
                  strategy === "cascade"
                    ? "border-primary ring-1 ring-primary"
                    : "border-border",
                )}
              >
                <input
                  type="radio"
                  name="strategy"
                  value="cascade"
                  checked={strategy === "cascade"}
                  onChange={() => setStrategy("cascade")}
                  aria-label="递归删除整棵子树"
                  className="mt-0.5"
                />
                <div className="flex-1">
                  <div className="text-sm font-semibold text-foreground">
                    递归删除整棵子树
                  </div>
                  <div className="text-2xs text-muted-foreground">
                    连同所有子部门 / Agent 一并软删
                  </div>
                </div>
              </label>
            </DialogBody>
            <DialogFooter>
              <span className="mr-auto font-mono text-2xs text-muted-foreground">
                该操作不可撤销
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setDeletePromptOpen(false)}
              >
                取消
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleConfirmDelete}
              >
                <Trash2 className="size-3.5" />
                确认删除
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </div>
  );
}

function DepartmentSelectPreview({
  department,
}: {
  department: OrgDepartment;
}) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <DepartmentIconBadge
        icon={department.icon}
        accentColor={department.accentColor}
        className="size-[22px] rounded"
        iconClassName="size-3"
      />
      <span className="truncate text-sm font-medium">{department.name}</span>
    </div>
  );
}

function DepartmentIconBadge({
  accentColor,
  className,
  icon,
  iconClassName,
}: {
  accentColor: string;
  className?: string;
  icon: string;
  iconClassName?: string;
}) {
  const Icon = iconForKey(icon);
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center text-white",
        agentColorClassNames[safeAgentColor(accentColor)],
        className,
      )}
      aria-hidden="true"
    >
      {React.createElement(Icon, {
        className: cn("size-3.5", iconClassName),
      })}
    </span>
  );
}

function LeaderSelectPreview({ agent }: { agent: OrgAgent }) {
  const color = safeAgentColor(agent.avatarColor);
  return (
    <div className="flex w-full items-center gap-2.5">
      <AgentAvatar
        name={agent.name}
        color={color}
        avatarDataUrl={agent.avatarDataUrl}
        avatarIcon={agent.avatarIcon}
        className="size-6 rounded text-2xs"
      />
      <span className="flex min-w-0 flex-1 flex-col items-start gap-0 text-left">
        <span className="truncate text-sm font-semibold text-foreground">
          {agent.name}
        </span>
        {agent.description && (
          <span className="truncate font-mono text-2xs text-muted-foreground">
            {agent.description}
          </span>
        )}
      </span>
      <LeadBadge color={color} />
    </div>
  );
}

function LeadBadge({
  color,
  compact = false,
}: {
  color: AgentColor;
  compact?: boolean;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center rounded-sm bg-secondary font-mono font-semibold",
        leadTextColorClassNames[color],
        compact ? "gap-1 px-1 py-0.5 text-2xs" : "gap-1 px-1.5 py-0.5 text-2xs",
      )}
    >
      <Crown className={compact ? "size-2" : "size-2.5"} aria-hidden="true" />
      LEAD
    </span>
  );
}

function agentMemberDescription(agent: OrgAgent): string {
  return agent.description || "";
}

function departmentMemberDescription(department: OrgDepartment): string {
  return `${department.memberCount} 名成员`;
}

function buildPath(dept: OrgDepartment, all: OrgDepartment[]): OrgDepartment[] {
  const byId = new Map(all.map((d) => [d.id, d]));
  const out: OrgDepartment[] = [dept];
  let cur: OrgDepartment | undefined = byId.get(dept.parentId);
  while (cur) {
    out.unshift(cur);
    cur = byId.get(cur.parentId);
  }
  return out;
}

function isDescendant(
  candidateId: number,
  ancestorId: number,
  all: OrgDepartment[],
): boolean {
  const byId = new Map(all.map((d) => [d.id, d]));
  let cur: number | undefined = candidateId;
  while (cur && cur > 0) {
    if (cur === ancestorId) return true;
    cur = byId.get(cur)?.parentId;
  }
  return false;
}
