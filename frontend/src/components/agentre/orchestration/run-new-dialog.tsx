import * as React from "react";
import {
  LibraryBig,
  Loader2,
  type LucideIcon,
  Play,
  SquarePen,
  Waypoints,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { useOrchRunListStore } from "@/stores/orch-run-list-store";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { firstLetter, tokenToCssColor } from "../session-avatar";
import {
  ListChatAgents,
  LoadOrg,
  ProjectListTree,
  RunCreate,
  WorkflowList,
} from "../../../../wailsjs/go/app/App";
import { app } from "../../../../wailsjs/go/models";
import { FlowGraphView } from "./flow-graph-view";
import { TeamDepartmentPicker } from "./team-department-picker";
import {
  groupAgentsByDepartment,
  type OrgAgentLite,
  type OrgDeptLite,
} from "./team-picker-data";

// 流程模式: 从流程库 | 临时写
type FlowMode = "library" | "adhoc";

// 上次选择的流程 id 持久化 key(sticky 预选;localStorage 是本仓前端持久化套路)
const LAST_FLOW_KEY = "agentre.orchestration.lastFlowId";

// 起始流程分段控件配置(顺序即渲染顺序)
const FLOW_SEGMENTS: { mode: FlowMode; icon: LucideIcon; labelKey: string }[] =
  [
    {
      mode: "library",
      icon: LibraryBig,
      labelKey: "orchestration.new.flowLibrary",
    },
    { mode: "adhoc", icon: SquarePen, labelKey: "orchestration.new.flowAdhoc" },
  ];

type AgentItem = {
  id: number;
  name: string;
  avatarColor: string;
  backendType: string;
  defaultPermissionMode: string;
};

type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
  graph: string;
};

type FlatProject = { id: number; name: string; depth: number };

// flattenTree 把项目树拍平成 [{id,name,depth}] 供下拉用(与 project-new-dialog 同款，
// depth 决定缩进)。就地复刻以避免改动无关文件。
function flattenTree(nodes: app.ProjectTreeNode[], depth = 0): FlatProject[] {
  const out: FlatProject[] = [];
  for (const n of nodes) {
    if (!n.project) continue;
    out.push({ id: n.project.id, name: n.project.name, depth });
    if (n.children) out.push(...flattenTree(n.children, depth + 1));
  }
  return out;
}

export type RunNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function RunNewDialog({ open, onOpenChange }: RunNewDialogProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [agents, setAgents] = React.useState<AgentItem[]>([]);
  const [workflows, setWorkflows] = React.useState<WorkflowOption[]>([]);
  const [projects, setProjects] = React.useState<FlatProject[]>([]);
  const [projectId, setProjectId] = React.useState(0);
  const [orgDepartments, setOrgDepartments] = React.useState<OrgDeptLite[]>([]);
  const [orgAgents, setOrgAgents] = React.useState<OrgAgentLite[]>([]);

  const [goal, setGoal] = React.useState("");
  const [leaderId, setLeaderId] = React.useState(0);
  const [flowMode, setFlowMode] = React.useState<FlowMode>("library");
  const [flowId, setFlowId] = React.useState(0);
  const [flowContent, setFlowContent] = React.useState("");
  const [allowedAgentIds, setAllowedAgentIds] = React.useState<number[]>([]);

  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // 每次打开时重置表单并加载数据
  React.useEffect(() => {
    if (!open) return;

    setGoal("");
    setLeaderId(0);
    setFlowMode("library");
    setFlowId(0);
    setFlowContent("");
    setAllowedAgentIds([]);
    setProjectId(0);
    setOrgDepartments([]);
    setOrgAgents([]);
    setError(null);

    // 并发加载 agents 和 workflows
    ListChatAgents()
      .then((resp) => {
        setAgents(
          (resp?.agents ?? []).map(
            (a: {
              id: number;
              name: string;
              avatarColor: string;
              backendType: string;
              defaultPermissionMode: string;
            }) => ({
              id: a.id,
              name: a.name,
              avatarColor: a.avatarColor,
              backendType: a.backendType,
              defaultPermissionMode: a.defaultPermissionMode,
            }),
          ),
        );
      })
      .catch(() => setAgents([]));

    LoadOrg()
      .then((resp) => {
        setOrgDepartments(
          (resp?.departments ?? []).map((d) => ({
            id: d.id,
            name: d.name,
            icon: d.icon,
            accentColor: d.accentColor,
            sortOrder: d.sortOrder,
          })),
        );
        setOrgAgents(
          (resp?.agents ?? []).map((a) => ({
            id: a.id,
            departmentId: a.departmentId,
          })),
        );
      })
      .catch(() => {
        setOrgDepartments([]);
        setOrgAgents([]);
      });

    WorkflowList()
      .then((resp) => {
        const items = (resp?.items ?? []).map((w) => ({
          id: w.id,
          name: w.name,
          tags: w.tags ?? [],
          outline: w.outline ?? [],
          graph: w.graph ?? "",
        }));
        setWorkflows(items);
        // sticky: 上次选择(若仍在列表)否则首个
        const lastId = Number(localStorage.getItem(LAST_FLOW_KEY) || 0);
        const picked = items.find((w) => w.id === lastId) ?? items[0];
        if (picked) setFlowId(picked.id);
      })
      .catch(() => setWorkflows([]));

    ProjectListTree()
      .then((tree) => setProjects(flattenTree(tree ?? [])))
      .catch(() => setProjects([]));
  }, [open]);

  // 是否可以提交: 目标非空 + 已选 Leader(Leader 是必选的编排枢纽,leaderAgentId=0 无效)
  const canSubmit = goal.trim().length > 0 && leaderId > 0 && !submitting;
  const selectedWorkflow = workflows.find((w) => w.id === flowId);
  const pickerModel = React.useMemo(
    () => groupAgentsByDepartment(agents, orgDepartments, orgAgents),
    [agents, orgDepartments, orgAgents],
  );

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      const d = await RunCreate({
        goal: goal.trim(),
        leaderAgentId: leaderId,
        // 流程库模式传 flowId, 临时写传 flowContent, 留空两者为 0/""
        flowId: flowMode === "library" ? flowId : 0,
        flowContent: flowMode === "adhoc" ? flowContent : "",
        projectId,
        allowedAgentIds,
      });
      // 记录本次实际使用的流程,供下次打开预选
      if (flowMode === "library" && flowId > 0) {
        localStorage.setItem(LAST_FLOW_KEY, String(flowId));
      }
      // run 可能为 undefined, 用可选链保护
      if (d.run?.id) {
        // 乐观写进左侧列表 store: 后端 RunCreate 不发 orch:run:* 事件, 而 RunList
        // 常驻不随 /orchestration/:id 重挂 → 不主动 upsert 就得切页才刷新。列表按
        // updatetime DESC 排序, 新 Run 最新, upsert 前插正好对齐。
        useOrchRunListStore.getState().upsert(d.run);
        navigate(`/orchestration/${d.run.id}`);
      }
      onOpenChange(false);
    } catch (err) {
      setError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[560px]">
        <DialogHeader>
          <div className="flex items-center gap-2.5">
            <span className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary-soft text-primary-text">
              <Waypoints className="size-4" aria-hidden="true" />
            </span>
            <DialogTitle>{t("orchestration.new.title")}</DialogTitle>
          </div>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {/* 目标 */}
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="flex items-center gap-2">
              <span className="font-medium text-foreground">
                {t("orchestration.new.goal")}
              </span>
              <span className="ml-auto font-normal text-subtle-foreground">
                {t("orchestration.new.goalHint")}
              </span>
            </span>
            <Textarea
              data-testid="run-goal"
              aria-label={t("orchestration.new.goal")}
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              placeholder={t("orchestration.new.goalPlaceholder")}
              className="min-h-[72px] resize-none text-xs"
            />
          </label>

          {/* Leader 选择: trigger 与选项均显示身份色头像 + 名字 */}
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.leader")}
            </span>
            <Select
              value={leaderId ? String(leaderId) : ""}
              onValueChange={(v) => setLeaderId(Number(v))}
            >
              <SelectTrigger
                data-testid="run-leader"
                aria-label={t("orchestration.new.leader")}
                className="h-9 text-xs"
              >
                <SelectValue
                  placeholder={t("orchestration.new.leaderPlaceholder")}
                />
              </SelectTrigger>
              <SelectContent>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={String(a.id)}>
                    <AgentDot color={a.avatarColor} name={a.name} />
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>

          {/* 起始流程: 分段控件(从零开始 / 从流程库 / 临时) */}
          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.flow")}
            </span>
            <div className="flex gap-0.5 rounded-lg bg-secondary p-0.5">
              {FLOW_SEGMENTS.map(({ mode, icon: Icon, labelKey }) => {
                const active = flowMode === mode;
                return (
                  <button
                    key={mode}
                    type="button"
                    data-testid={`run-flow-mode-${mode}`}
                    onClick={() => setFlowMode(mode)}
                    className={cn(
                      "flex min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md px-2 py-1.5 text-center text-xs leading-tight transition-colors",
                      active
                        ? "border border-border bg-card font-medium text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon
                      className={cn(
                        "size-3.5 shrink-0",
                        active ? "text-primary-text" : "",
                      )}
                      aria-hidden="true"
                    />
                    <span>{t(labelKey)}</span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* 流程库选择: 下拉框,显示名称 + 标签 chip;选中后展示步骤面包屑 */}
          {flowMode === "library" ? (
            <div className="flex flex-col gap-1.5 text-xs">
              <span className="flex items-center gap-2">
                <span className="font-medium text-foreground">
                  {t("orchestration.new.flowSelect")}
                </span>
                <button
                  type="button"
                  data-testid="run-flow-manage"
                  onClick={() =>
                    useWorkflowManagerStore.getState().openBrowse()
                  }
                  className="ml-auto text-2xs text-primary-text hover:underline"
                >
                  {t("orchestration.new.flowManage")} →
                </button>
              </span>
              <Select
                value={flowId ? String(flowId) : ""}
                onValueChange={(v) => setFlowId(Number(v))}
              >
                <SelectTrigger
                  data-testid="run-flow-select"
                  aria-label={t("orchestration.new.flowSelect")}
                  className="h-9 text-xs"
                >
                  <SelectValue
                    placeholder={t("orchestration.new.flowSelectPlaceholder")}
                  />
                </SelectTrigger>
                <SelectContent>
                  {workflows.map((w) => (
                    <SelectItem key={w.id} value={String(w.id)}>
                      <span className="flex items-center gap-2">
                        <span>{w.name}</span>
                        {w.tags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded bg-accent px-1 py-0.5 text-2xs text-muted-foreground"
                          >
                            {tag}
                          </span>
                        ))}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {selectedWorkflow && selectedWorkflow.graph ? (
                <div
                  data-testid="run-flow-preview"
                  className="flex flex-col gap-1.5"
                >
                  <span className="text-2xs text-subtle-foreground">
                    {t("orchestration.new.flowPreview")}
                  </span>
                  <FlowGraphView graph={selectedWorkflow.graph} />
                </div>
              ) : selectedWorkflow && selectedWorkflow.outline.length > 0 ? (
                <span
                  data-testid="run-flow-outline"
                  className="flex flex-wrap items-center gap-1"
                >
                  {selectedWorkflow.outline.map((step, i) => (
                    <React.Fragment key={`${step}-${i}`}>
                      {i > 0 ? (
                        <span className="text-2xs text-subtle-foreground">
                          ›
                        </span>
                      ) : null}
                      <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-muted-foreground">
                        {step}
                      </span>
                    </React.Fragment>
                  ))}
                </span>
              ) : null}
            </div>
          ) : null}

          {/* 临时流程内容 */}
          {flowMode === "adhoc" ? (
            <label className="flex flex-col gap-1.5 text-xs">
              <span className="font-medium text-foreground">
                {t("orchestration.new.flowContent")}
              </span>
              <Textarea
                data-testid="run-flow-content"
                aria-label={t("orchestration.new.flowContent")}
                value={flowContent}
                onChange={(e) => setFlowContent(e.target.value)}
                placeholder={t("orchestration.new.flowContentPlaceholder")}
                className="min-h-[80px] resize-none text-xs"
              />
            </label>
          ) : null}

          {/* 项目: 可选; 选中后该 Run 全部 agent 以项目目录为工作目录 */}
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.project")}
            </span>
            <Select
              value={String(projectId)}
              onValueChange={(v) => setProjectId(Number(v))}
            >
              <SelectTrigger
                data-testid="run-project"
                aria-label={t("orchestration.new.project")}
                className="h-9 text-xs"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">
                  {t("orchestration.new.projectNone")}
                </SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    <span style={{ paddingInlineStart: `${p.depth * 12}px` }}>
                      {p.name}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>

          {/* 可参与团队: 双栏部门选择器 */}
          <TeamDepartmentPicker
            model={pickerModel}
            value={allowedAgentIds}
            onChange={setAllowedAgentIds}
          />

          {error ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
              {error}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            data-testid="run-create"
            disabled={!canSubmit}
            onClick={() => void submit()}
          >
            {submitting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <Play className="size-3.5" aria-hidden="true" />
            )}
            {t("orchestration.new.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// AgentDot: 身份色圆点/头像; showLetter=true 时显示名字首字(用于 Leader 下拉),
// 否则只是纯色小圆点(用于可参与 agent chips)。始终 aria-hidden, 名字由旁边文本承载。
function AgentDot({
  color,
  name,
  showLetter = true,
}: {
  color: string;
  name: string;
  showLetter?: boolean;
}) {
  const bg = tokenToCssColor(color) ?? "#94a3b8";
  if (!showLetter) {
    return (
      <span
        aria-hidden="true"
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: bg }}
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className="flex size-5 shrink-0 items-center justify-center rounded-full text-2xs font-semibold text-white"
      style={{ backgroundColor: bg }}
    >
      {firstLetter(name)}
    </span>
  );
}
