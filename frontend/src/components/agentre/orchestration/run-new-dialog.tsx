import * as React from "react";
import {
  LibraryBig,
  Loader2,
  type LucideIcon,
  Play,
  Sparkles,
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
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { firstLetter, tokenToCssColor } from "../session-avatar";
import {
  ListChatAgents,
  ProjectListTree,
  RunCreate,
  WorkflowList,
} from "../../../../wailsjs/go/app/App";
import { app } from "../../../../wailsjs/go/models";

// 流程模式: 从零开始(AI 自拆) | 从流程库 | 临时写
type FlowMode = "none" | "library" | "adhoc";

// 起始流程分段控件配置(顺序即渲染顺序)
const FLOW_SEGMENTS: { mode: FlowMode; icon: LucideIcon; labelKey: string }[] =
  [
    { mode: "none", icon: Sparkles, labelKey: "orchestration.new.flowNone" },
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
  defaultPermissionMode: string;
};

type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
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

  const [goal, setGoal] = React.useState("");
  const [leaderId, setLeaderId] = React.useState(0);
  const [flowMode, setFlowMode] = React.useState<FlowMode>("none");
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
    setFlowMode("none");
    setFlowId(0);
    setFlowContent("");
    setAllowedAgentIds([]);
    setProjectId(0);
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
              defaultPermissionMode: string;
            }) => ({
              id: a.id,
              name: a.name,
              avatarColor: a.avatarColor,
              defaultPermissionMode: a.defaultPermissionMode,
            }),
          ),
        );
      })
      .catch(() => setAgents([]));

    WorkflowList()
      .then((resp) => {
        setWorkflows(
          (resp?.items ?? []).map(
            (w: {
              id: number;
              name: string;
              tags?: string[];
              outline?: string[];
            }) => ({
              id: w.id,
              name: w.name,
              tags: w.tags ?? [],
              outline: w.outline ?? [],
            }),
          ),
        );
      })
      .catch(() => setWorkflows([]));

    ProjectListTree()
      .then((tree) => setProjects(flattenTree(tree ?? [])))
      .catch(() => setProjects([]));
  }, [open]);

  // 是否可以提交: 目标非空 + 已选 Leader(Leader 是必选的编排枢纽,leaderAgentId=0 无效)
  const canSubmit = goal.trim().length > 0 && leaderId > 0 && !submitting;
  const selectedWorkflow = workflows.find((w) => w.id === flowId);

  // 切换团队成员勾选
  const toggleAllowed = (agentId: number, checked: boolean) => {
    setAllowedAgentIds((prev) =>
      checked ? [...prev, agentId] : prev.filter((id) => id !== agentId),
    );
  };

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
      // run 可能为 undefined, 用可选链保护
      if (d.run?.id) {
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
                  onClick={() => useWorkflowManagerStore.getState().openBrowse()}
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
                  <SelectValue placeholder={t("orchestration.new.flowSelectPlaceholder")} />
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
              {selectedWorkflow && selectedWorkflow.outline.length > 0 ? (
                <span
                  data-testid="run-flow-outline"
                  className="flex flex-wrap items-center gap-1"
                >
                  {selectedWorkflow.outline.map((step, i) => (
                    <React.Fragment key={`${step}-${i}`}>
                      {i > 0 ? (
                        <span className="text-2xs text-subtle-foreground">›</span>
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

          {/* 可参与 agent: 身份色药丸 chips 多选; 留空则不限制 */}
          {agents.length > 0 ? (
            <div className="flex flex-col gap-1.5 text-xs">
              <span className="flex items-center gap-2">
                <span className="font-medium text-foreground">
                  {t("orchestration.new.team")}
                </span>
                <span
                  className="ml-auto text-subtle-foreground"
                  data-testid="run-team-count"
                >
                  {t("orchestration.new.teamSelected", {
                    count: allowedAgentIds.length,
                  })}
                </span>
              </span>
              <div className="flex flex-wrap gap-1.5">
                {agents.map((a) => {
                  const selected = allowedAgentIds.includes(a.id);
                  return (
                    <button
                      key={a.id}
                      type="button"
                      data-testid={`run-team-${a.id}`}
                      aria-pressed={selected}
                      onClick={() => toggleAllowed(a.id, !selected)}
                      className={cn(
                        "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                        selected
                          ? "border-primary bg-primary-soft font-medium text-primary-text"
                          : "border-border bg-card text-muted-foreground hover:text-foreground",
                      )}
                    >
                      <AgentDot
                        color={a.avatarColor}
                        name={a.name}
                        showLetter={false}
                      />
                      <span>{a.name}</span>
                    </button>
                  );
                })}
              </div>
              <span className="text-2xs text-muted-foreground">
                {t("orchestration.new.teamHint")}
              </span>
            </div>
          ) : null}

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
