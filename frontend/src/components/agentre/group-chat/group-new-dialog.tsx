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

import { AgentMultiPicker, type PickableAgent } from "./agent-multi-picker";
import { GroupCreate } from "../../../../wailsjs/go/app/App";

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
  const [coordinatorID, setCoordinatorID] = React.useState(0);
  const [projectID, setProjectID] = React.useState(0);
  const [memberIDs, setMemberIDs] = React.useState<number[]>([]);
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // 每次打开重置 + 预填当前项目上下文。
  React.useEffect(() => {
    if (open) {
      setTitle("");
      setCoordinatorID(0);
      setProjectID(projectContext?.projectID ?? 0);
      setMemberIDs([]);
      setError(null);
    }
  }, [open, projectContext]);

  const canSubmit = title.trim().length > 0 && coordinatorID > 0 && !submitting;

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      const detail = await GroupCreate({
        title: title.trim(),
        coordinatorAgentID: coordinatorID,
        departmentID: 0,
        projectID,
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
              aria-label={t("group.new.groupTitle")}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("group.new.groupTitlePlaceholder")}
              className="h-9 text-xs"
            />
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.coordinator")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Select
              value={coordinatorID ? String(coordinatorID) : ""}
              onValueChange={(v) => setCoordinatorID(Number(v))}
            >
              <SelectTrigger
                aria-label={t("group.new.coordinator")}
                className="h-9 text-xs"
              >
                <SelectValue
                  placeholder={t("group.new.coordinatorPlaceholder")}
                />
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
              {t("group.new.coordinatorHint")}
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

          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.members")}
            </span>
            <AgentMultiPicker
              agents={eligible}
              value={memberIDs}
              onChange={setMemberIDs}
              exclude={coordinatorID ? [coordinatorID] : []}
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
