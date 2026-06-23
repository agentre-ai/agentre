import { Plus, Route } from "lucide-react";
import { useTranslation } from "react-i18next";

import i18n from "@/i18n";
import { cn } from "@/lib/utils";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { scoreItem } from "../score";
import type { CommandSource, OnSelectCtx } from "../types";

type WorkflowActionKey = "workflow-open-library" | "workflow-new";

export type WorkflowActionItem = {
  key: WorkflowActionKey;
  titleKey: string;
  hintKey: string;
  icon: "route" | "plus";
};

const ACTIONS: WorkflowActionItem[] = [
  {
    key: "workflow-open-library",
    titleKey: "workflows.actions.openLibrary",
    hintKey: "workflows.actions.openLibraryHint",
    icon: "route",
  },
  {
    key: "workflow-new",
    titleKey: "workflows.actions.newWorkflow",
    hintKey: "workflows.actions.newWorkflowHint",
    icon: "plus",
  },
];

function useItems(): { items: WorkflowActionItem[]; loading: boolean } {
  return { items: ACTIONS, loading: false };
}

function getScore(query: string, item: WorkflowActionItem): number {
  return scoreItem({
    query,
    title: i18n.t(item.titleKey),
    subtitle: i18n.t(item.hintKey),
  });
}

function ActionRow({ item }: { item: WorkflowActionItem }) {
  const { t } = useTranslation();
  const Icon = item.icon === "plus" ? Plus : Route;
  return (
    <div className="flex w-full items-center gap-3">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
        <Icon className="size-4 text-primary-text" aria-hidden="true" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-foreground">
          {t(item.titleKey)}
        </span>
        <span className="truncate text-2xs text-muted-foreground">
          {t(item.hintKey)}
        </span>
      </div>
      <kbd
        className={cn(
          "rounded-sm border border-border bg-card px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground",
          "opacity-0 group-data-[selected=true]/cmditem:opacity-100",
        )}
        aria-hidden="true"
      >
        ↵
      </kbd>
    </div>
  );
}

function onSelect(item: WorkflowActionItem, ctx: OnSelectCtx): void {
  ctx.close();
  const store = useWorkflowManagerStore.getState();
  if (item.key === "workflow-new") store.openCreate();
  else store.openBrowse();
}

export const workflowActionsSource: CommandSource<WorkflowActionItem> = {
  id: "workflow-actions",
  heading: i18n.t("commandPalette.workflows.heading"),
  modes: ["command"],
  useItems,
  getScore,
  renderItem: (item) => <ActionRow item={item} />,
  onSelect,
};
