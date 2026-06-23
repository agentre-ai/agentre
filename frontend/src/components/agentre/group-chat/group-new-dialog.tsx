import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
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
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useProjectList } from "@/hooks/use-project-list";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useGroupListStore } from "@/stores/group-list-store";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { AgentMultiPicker, type PickableAgent } from "./agent-multi-picker";
import { GroupCreate, WorkflowList } from "../../../../wailsjs/go/app/App";

export type GroupNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

function GroupNewDialog({ open, onOpenChange }: GroupNewDialogProps) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { projects } = useProjectList();
  const projectContext = useNewChatContextStore((s) => s.projectContext);

  const eligible: PickableAgent[] = React.useMemo(
    () =>
      agents
        .filter((a) => a.supportsGroup && a.chattable)
        .map((a) => ({
          id: a.id,
          name: a.name,
          avatarColor: a.avatarColor,
          avatarIcon: a.avatarIcon,
          avatarDataUrl: a.avatarDataUrl,
        })),
    [agents],
  );

  const [title, setTitle] = React.useState("");
  const [hostID, setHostID] = React.useState(0);
  const [projectID, setProjectID] = React.useState(0);
  const [memberIDs, setMemberIDs] = React.useState<number[]>([]);
  const [workflowID, setWorkflowID] = React.useState(0);
  const [workflowOptions, setWorkflowOptions] = React.useState<
    { id: number; name: string }[]
  >([]);
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const loadWorkflows = React.useCallback(() => {
    WorkflowList()
      .then((resp) =>
        setWorkflowOptions(
          (resp?.items ?? []).map((i: { id: number; name: string }) => ({
            id: i.id,
            name: i.name,
          })),
        ),
      )
      .catch(() => setWorkflowOptions([]));
  }, []);

  // 每次打开重置 + 预填当前项目上下文。
  React.useEffect(() => {
    if (open) {
      setTitle("");
      setHostID(0);
      setProjectID(projectContext?.projectID ?? 0);
      setMemberIDs([]);
      setWorkflowID(0);
      setError(null);
      loadWorkflows();
    }
  }, [open, projectContext, loadWorkflows]);

  // 管理弹窗关闭时刷新流程下拉（新建流程后可立即选中）。
  const wfManagerOpen = useWorkflowManagerStore((s) => s.open);
  const prevWfOpen = React.useRef(wfManagerOpen);
  React.useEffect(() => {
    if (prevWfOpen.current && !wfManagerOpen && open) loadWorkflows();
    prevWfOpen.current = wfManagerOpen;
  }, [wfManagerOpen, open, loadWorkflows]);

  const canSubmit = title.trim().length > 0 && hostID > 0 && !submitting;

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      const detail = await GroupCreate({
        title: title.trim(),
        hostAgentID: hostID,
        departmentID: 0,
        projectID,
        workflowID,
        memberAgentIDs: memberIDs,
      });
      await useGroupListStore.getState().reload();
      const g = detail.group;
      if (g) useChatTabsStore.getState().openGroup(g.id, g.title);
      onOpenChange(false);
    } catch (err) {
      setError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{t("group.new.title")}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3.5">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.groupTitle")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              data-testid="group-title-input"
              aria-label={t("group.new.groupTitle")}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("group.new.groupTitlePlaceholder")}
              className="h-9 text-xs"
            />
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.host")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Select
              value={hostID ? String(hostID) : ""}
              onValueChange={(v) => setHostID(Number(v))}
            >
              <SelectTrigger
                data-testid="group-host-select"
                aria-label={t("group.new.host")}
                className="h-9 text-xs"
              >
                <SelectValue placeholder={t("group.new.hostPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {eligible.map((a) => (
                  <SelectItem key={a.id} value={String(a.id)}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className="text-2xs text-muted-foreground">
              {t("group.new.hostHint")}
            </span>
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.project")}
            </span>
            <Select
              value={String(projectID)}
              onValueChange={(v) => setProjectID(Number(v))}
            >
              <SelectTrigger
                data-testid="group-project-select"
                aria-label={t("group.new.project")}
                className="h-9 text-xs"
              >
                <SelectValue placeholder={t("group.new.projectNone")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">{t("group.new.projectNone")}</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="flex items-center justify-between font-medium text-foreground">
              <span>{t("group.new.workflow")}</span>
              <Button
                type="button"
                variant="link"
                size="sm"
                data-testid="group-manage-workflows"
                className="h-auto p-0 text-2xs"
                onClick={() => useWorkflowManagerStore.getState().openBrowse()}
              >
                {t("group.new.manageWorkflows")}
              </Button>
            </span>
            <Select
              value={String(workflowID)}
              onValueChange={(v) => setWorkflowID(Number(v))}
            >
              <SelectTrigger
                data-testid="group-workflow-select"
                aria-label={t("group.new.workflow")}
                className="h-9 text-xs"
              >
                <SelectValue placeholder={t("group.new.workflowNone")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">{t("group.new.workflowNone")}</SelectItem>
                {workflowOptions.map((w) => (
                  <SelectItem key={w.id} value={String(w.id)}>
                    {w.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className="text-2xs text-muted-foreground">
              {t("group.new.workflowHint")}
            </span>
          </label>

          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.members")}
            </span>
            <AgentMultiPicker
              agents={eligible}
              value={memberIDs}
              onChange={setMemberIDs}
              exclude={hostID ? [hostID] : []}
            />
            <span className="text-2xs text-muted-foreground">
              {t("group.new.membersHint")}
            </span>
          </div>

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
            disabled={!canSubmit}
            data-testid="group-create-submit"
            onClick={() => void submit()}
          >
            {submitting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            {t("group.new.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { GroupNewDialog };
