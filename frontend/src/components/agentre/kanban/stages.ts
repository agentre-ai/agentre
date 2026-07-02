export type StageId = "todo" | "doing" | "review" | "done";

export const STAGES: {
  id: StageId;
  labelKey: string;
  icon: string;
  accent: string;
}[] = [
  {
    id: "todo",
    labelKey: "issues.stages.todo",
    icon: "circle",
    accent: "text-status-idle",
  },
  {
    id: "doing",
    labelKey: "issues.stages.doing",
    icon: "circle-dot",
    accent: "text-primary-text",
  },
  {
    id: "review",
    labelKey: "issues.stages.review",
    icon: "circle-dashed",
    accent: "text-status-waiting",
  },
  {
    id: "done",
    labelKey: "issues.stages.done",
    icon: "circle-check-big",
    accent: "text-status-running",
  },
];
