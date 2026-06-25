import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { useChatTabsStore } from "../../../stores/chat-tabs-store";

import {
  ListChatAgents,
  RunCreate,
  WorkflowList,
} from "../../../../wailsjs/go/app/App";

// 流程模式: 流程库 | 临时写 | 不使用
type FlowMode = "library" | "adhoc" | "none";

type AgentItem = {
  id: number;
  name: string;
  defaultPermissionMode: string;
};

type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
};

export type RunNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function RunNewDialog({ open, onOpenChange }: RunNewDialogProps) {
  const { t } = useTranslation();

  const [agents, setAgents] = React.useState<AgentItem[]>([]);
  const [workflows, setWorkflows] = React.useState<WorkflowOption[]>([]);

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
    setError(null);

    // 并发加载 agents 和 workflows
    ListChatAgents()
      .then((resp) => {
        setAgents(
          (resp?.agents ?? []).map(
            (a: {
              id: number;
              name: string;
              defaultPermissionMode: string;
            }) => ({
              id: a.id,
              name: a.name,
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
            (w: { id: number; name: string; tags?: string[]; outline?: string[] }) => ({
              id: w.id,
              name: w.name,
              tags: w.tags ?? [],
              outline: w.outline ?? [],
            }),
          ),
        );
      })
      .catch(() => setWorkflows([]));
  }, [open]);

  // 是否可以提交: 目标非空 + 已选 Leader(Leader 是必选的编排枢纽,leaderAgentId=0 无效)
  const canSubmit = goal.trim().length > 0 && leaderId > 0 && !submitting;

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
        projectId: 0,
        allowedAgentIds,
      });
      // run 可能为 undefined, 用可选链保护
      if (d.run?.id) {
        useChatTabsStore.getState().openRun(d.run.id, goal.trim());
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
      <DialogContent className="max-w-[540px]">
        <DialogHeader>
          <DialogTitle>{t("orchestration.new.title")}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3.5">
          {/* 目标 */}
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.goal")}
              <span className="ml-0.5 text-destructive">*</span>
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

          {/* Leader 选择 */}
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
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>

          {/* 流程模式: 三态按钮组 */}
          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.flow")}
            </span>
            <div className="flex gap-1.5">
              {(["none", "library", "adhoc"] as FlowMode[]).map((m) => (
                <Button
                  key={m}
                  type="button"
                  size="sm"
                  variant={flowMode === m ? "default" : "outline"}
                  data-testid={`run-flow-mode-${m}`}
                  onClick={() => setFlowMode(m)}
                >
                  {t(
                    m === "none"
                      ? "orchestration.new.flowNone"
                      : m === "library"
                        ? "orchestration.new.flowLibrary"
                        : "orchestration.new.flowAdhoc",
                  )}
                </Button>
              ))}
            </div>
          </div>

          {/* 流程库选择: 单选行列表,显示名称 + 标签 chip + 步骤面包屑 */}
          {flowMode === "library" ? (
            <div className="flex flex-col gap-1.5 text-xs">
              <span className="font-medium text-foreground">
                {t("orchestration.new.flowSelect")}
              </span>
              <div className="flex flex-col gap-1.5">
                {workflows.map((w) => (
                  <button
                    key={w.id}
                    type="button"
                    data-testid={`run-flow-pick-${w.id}`}
                    onClick={() => setFlowId(w.id)}
                    className={
                      flowId === w.id
                        ? "flex flex-col gap-1 rounded-md border border-primary bg-primary-soft px-3 py-2 text-left"
                        : "flex flex-col gap-1 rounded-md border border-border px-3 py-2 text-left hover:bg-accent/50"
                    }
                  >
                    <span className="flex items-center gap-2">
                      <span className="font-medium text-foreground">
                        {w.name}
                      </span>
                      {w.tags.map((tag) => (
                        <span
                          key={tag}
                          className="rounded bg-accent px-1 py-0.5 text-2xs text-muted-foreground"
                        >
                          {tag}
                        </span>
                      ))}
                    </span>
                    {w.outline.length > 0 ? (
                      <span className="flex flex-wrap items-center gap-1">
                        {w.outline.map((step, i) => (
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
                  </button>
                ))}
              </div>
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

          {/* 限定团队成员（可选），每个 agent 旁显示危险操作姿态徽标 */}
          {agents.length > 0 ? (
            <div className="flex flex-col gap-1.5 text-xs">
              <span className="font-medium text-foreground">
                {t("orchestration.new.team")}
              </span>
              <div className="flex flex-col gap-2">
                {agents.map((a) => (
                  <div key={a.id} className="flex items-center gap-2">
                    <Checkbox
                      id={`agent-allowed-${a.id}`}
                      checked={allowedAgentIds.includes(a.id)}
                      onCheckedChange={(checked) =>
                        toggleAllowed(a.id, checked === true)
                      }
                    />
                    <label
                      htmlFor={`agent-allowed-${a.id}`}
                      className="flex flex-1 cursor-pointer items-center gap-2"
                    >
                      <span>{a.name}</span>
                      {/* 危险操作姿态徽标:
                          bypassPermissions = 工具调用自动放行（危险）
                          其他模式(default/approve等) = 需要用户审批（安全）*/}
                      <span
                        className={
                          a.defaultPermissionMode === "bypassPermissions"
                            ? "rounded px-1 py-0.5 text-2xs font-medium bg-destructive/10 text-destructive"
                            : "rounded px-1 py-0.5 text-2xs font-medium bg-muted text-muted-foreground"
                        }
                      >
                        {a.defaultPermissionMode === "bypassPermissions"
                          ? t("orchestration.new.dangerAuto")
                          : t("orchestration.new.dangerApproval")}
                      </span>
                    </label>
                  </div>
                ))}
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
            variant="ghost"
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
            ) : null}
            {t("orchestration.new.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
