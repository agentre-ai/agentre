import * as React from "react";
import {
  ArrowDownUp,
  ChevronDown,
  Circle,
  CircleAlert,
  CircleCheck,
  CircleDot,
  Columns3,
  List,
  MessageSquare,
  Plus,
  Send,
  SlidersHorizontal,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { AgentAvatar, StatusPill } from "./primitives";
import type { AgentColor, AgentStatus } from "./types";

type IssueView = "list" | "board";
type IssueLabelTone =
  | "auth"
  | "bug"
  | "critical"
  | "docs"
  | "feature"
  | "hook"
  | "ops"
  | "perf"
  | "refactor"
  | "ui";

type IssueLabel = {
  name: string;
  tone: IssueLabelTone;
};

type IssueAgent = {
  color: AgentColor;
  initials: string;
  name: string;
};

type IssueItem = {
  id: string;
  title: string;
  labels: IssueLabel[];
  status: AgentStatus;
  meta: string;
  comments: number;
  agents: IssueAgent[];
  closed?: boolean;
  dispatchable?: boolean;
  highlighted?: boolean;
};

type BoardColumn = {
  count: number;
  id: string;
  issues: IssueItem[];
  tone: AgentStatus;
  title: string;
};

const labelToneClassNames: Record<IssueLabelTone, string> = {
  auth: "bg-agent-1/10 text-agent-1",
  bug: "bg-destructive-soft text-destructive",
  critical: "bg-destructive text-destructive-foreground",
  docs: "bg-secondary text-muted-foreground",
  feature: "bg-status-running-bg text-status-running",
  hook: "bg-primary-soft text-primary-text",
  ops: "bg-secondary text-muted-foreground",
  perf: "bg-status-waiting-bg text-status-waiting",
  refactor: "bg-primary-soft text-primary-text",
  ui: "bg-agent-2/10 text-agent-2",
};

const statusIconMeta: Record<
  AgentStatus,
  { className: string; icon: LucideIcon }
> = {
  error: { icon: CircleAlert, className: "text-status-error" },
  idle: { icon: Circle, className: "text-muted-foreground" },
  running: { icon: CircleDot, className: "text-status-running" },
  waiting: { icon: CircleDot, className: "text-status-waiting" },
};

const boardColumnToneClassNames: Record<
  AgentStatus,
  { badge: string; dot: string }
> = {
  error: {
    badge: "bg-destructive-soft text-status-error",
    dot: "bg-status-error",
  },
  idle: {
    badge: "bg-secondary text-muted-foreground",
    dot: "bg-muted-foreground",
  },
  running: {
    badge: "bg-status-running-bg text-status-running",
    dot: "bg-status-running",
  },
  waiting: {
    badge: "bg-status-waiting-bg text-status-waiting",
    dot: "bg-status-waiting",
  },
};

function createIssueRows(t: TFunction): IssueItem[] {
  return [
    {
      id: "#142",
      title: t("issues.samples.142.title"),
      labels: [
        { name: "bug", tone: "bug" },
        { name: "auth", tone: "auth" },
      ],
      status: "running",
      meta: t("issues.samples.142.meta"),
      comments: 4,
      agents: [
        { name: "Kai", initials: "K", color: "agent-2" },
        { name: "Parker", initials: "P", color: "agent-1" },
      ],
    },
    {
      id: "#141",
      title: t("issues.samples.141.title"),
      labels: [
        { name: "feature", tone: "feature" },
        { name: "ui", tone: "ui" },
      ],
      status: "running",
      meta: t("issues.samples.141.meta"),
      comments: 12,
      agents: [{ name: "Eva", initials: "E", color: "agent-3" }],
    },
    {
      id: "#140",
      title: t("issues.samples.140.title"),
      labels: [{ name: "perf", tone: "perf" }],
      status: "waiting",
      meta: t("issues.samples.140.meta"),
      comments: 7,
      agents: [{ name: "Dora", initials: "D", color: "agent-4" }],
    },
    {
      id: "#139",
      title: t("issues.samples.139.title"),
      labels: [
        { name: "hook", tone: "hook" },
        { name: "ops", tone: "ops" },
      ],
      status: "waiting",
      meta: t("issues.samples.139.meta"),
      comments: 3,
      agents: [{ name: "Bea", initials: "B", color: "agent-5" }],
    },
    {
      id: "#138",
      title: t("issues.samples.138.title"),
      labels: [
        { name: "bug", tone: "bug" },
        { name: "critical", tone: "critical" },
      ],
      status: "error",
      meta: t("issues.samples.138.meta"),
      comments: 9,
      agents: [{ name: "Kai", initials: "K", color: "agent-2" }],
      highlighted: true,
    },
    {
      id: "#137",
      title: t("issues.samples.137.title"),
      labels: [{ name: "docs", tone: "docs" }],
      status: "idle",
      meta: t("issues.samples.137.meta"),
      comments: 0,
      agents: [],
      dispatchable: true,
    },
    {
      id: "#136",
      title: t("issues.samples.136.title"),
      labels: [
        { name: "refactor", tone: "refactor" },
        { name: "ui", tone: "ui" },
      ],
      status: "running",
      meta: t("issues.samples.136.meta"),
      comments: 18,
      agents: [
        { name: "Eva", initials: "E", color: "agent-3" },
        { name: "Parker", initials: "P", color: "agent-1" },
      ],
    },
  ];
}

function createBoardColumns(
  t: TFunction,
  issueRows: IssueItem[],
): BoardColumn[] {
  const backlogIssues: IssueItem[] = [
    issueRows[5],
    {
      id: "#133",
      title: t("issues.samples.133.title"),
      labels: [{ name: "refactor", tone: "refactor" }],
      status: "idle",
      meta: t("issues.samples.133.meta"),
      comments: 0,
      agents: [],
      dispatchable: true,
    },
  ];
  const closedIssues: IssueItem[] = [
    {
      id: "#132",
      title: t("issues.samples.132.title"),
      labels: [{ name: "bug", tone: "bug" }],
      status: "idle",
      meta: t("issues.samples.132.meta"),
      comments: 2,
      agents: [],
      closed: true,
    },
    {
      id: "#129",
      title: t("issues.samples.129.title"),
      labels: [],
      status: "running",
      meta: t("issues.samples.129.meta"),
      comments: 5,
      agents: [],
      closed: true,
    },
  ];

  return [
    {
      id: "backlog",
      title: t("issues.columns.backlog"),
      count: 2,
      tone: "idle",
      issues: backlogIssues,
    },
    {
      id: "running",
      title: t("issues.columns.running"),
      count: 4,
      tone: "running",
      issues: [issueRows[0], issueRows[1], issueRows[4], issueRows[6]],
    },
    {
      id: "waiting",
      title: t("issues.columns.waiting"),
      count: 2,
      tone: "waiting",
      issues: [issueRows[2], issueRows[3]],
    },
    {
      id: "closed",
      title: t("issues.columns.closed"),
      count: 47,
      tone: "running",
      issues: closedIssues,
    },
  ];
}

function IssuesPage() {
  const { t } = useTranslation();
  const [view, setView] = React.useState<IssueView>("list");
  const issueRows = React.useMemo(() => createIssueRows(t), [t]);
  const boardColumns = React.useMemo(
    () => createBoardColumns(t, issueRows),
    [t, issueRows],
  );
  const summary =
    view === "list" ? t("issues.summary.list") : t("issues.summary.board");

  return (
    <main
      className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
      data-slot="issues-page"
    >
      <IssuesHeader view={view} summary={summary} onViewChange={setView} />
      <IssueFilterBar />
      {view === "list" ? (
        <IssuesList issueRows={issueRows} />
      ) : (
        <IssuesBoard boardColumns={boardColumns} />
      )}
    </main>
  );
}

type IssuesHeaderProps = {
  onViewChange: (view: IssueView) => void;
  summary: string;
  view: IssueView;
};

function IssuesHeader({ onViewChange, summary, view }: IssuesHeaderProps) {
  const { t } = useTranslation();

  return (
    <header className="flex min-h-[60px] shrink-0 flex-wrap items-center gap-3 border-b border-border bg-background px-5 py-3 lg:h-[60px] lg:flex-nowrap lg:py-0">
      <div className="flex min-w-0 flex-col gap-0.5">
        <h1 className="truncate text-base font-semibold tracking-normal">
          {t("issues.title")}
        </h1>
        <p className="truncate text-2xs text-muted-foreground">{summary}</p>
      </div>
      <div className="min-w-0 flex-1" />
      <div
        className="flex h-[30px] shrink-0 items-center gap-0.5 rounded-md border border-border bg-secondary p-0.5"
        aria-label={t("issues.view.aria")}
      >
        <IssueViewButton
          active={view === "list"}
          icon={List}
          label={t("issues.view.list")}
          onClick={() => onViewChange("list")}
        />
        <IssueViewButton
          active={view === "board"}
          icon={Columns3}
          label={t("issues.view.board")}
          onClick={() => onViewChange("board")}
        />
      </div>
      <Button type="button" variant="outline" size="sm" className="h-[30px]">
        <SlidersHorizontal data-icon="inline-start" aria-hidden="true" />
        {t("issues.actions.filter")}
      </Button>
      <Button type="button" size="sm" className="h-[30px]">
        <Plus data-icon="inline-start" aria-hidden="true" />
        {t("issues.actions.newIssue")}
      </Button>
    </header>
  );
}

type IssueViewButtonProps = {
  active: boolean;
  icon: LucideIcon;
  label: string;
  onClick: () => void;
};

function IssueViewButton({
  active,
  icon: Icon,
  label,
  onClick,
}: IssueViewButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={cn(
        "inline-flex h-full cursor-pointer items-center justify-center gap-1.5 rounded-sm px-2.5 text-xs font-medium outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/40",
        active
          ? "border border-border bg-card text-foreground shadow-xs"
          : "text-muted-foreground hover:text-foreground",
      )}
      onClick={onClick}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
    </button>
  );
}

function IssueFilterBar() {
  const { t } = useTranslation();

  return (
    <div className="flex min-h-12 shrink-0 items-center gap-2 overflow-x-auto border-b border-border bg-sidebar px-5 py-2">
      <div className="flex shrink-0 items-center gap-0.5">
        <FilterTab
          active
          icon={CircleDot}
          label={t("issues.filters.open")}
          count={12}
        />
        <FilterTab
          icon={CircleCheck}
          label={t("issues.filters.closed")}
          count={47}
        />
      </div>
      <div className="min-w-0 flex-1" />
      <FilterChip label={t("issues.filters.author")} />
      <FilterChip label={t("issues.filters.label")} />
      <FilterChip label={t("issues.filters.assignedAgent")} />
      <FilterChip icon={ArrowDownUp} label={t("issues.filters.latestUpdate")} />
    </div>
  );
}

type FilterTabProps = {
  active?: boolean;
  count: number;
  icon: LucideIcon;
  label: string;
};

function FilterTab({
  active = false,
  count,
  icon: Icon,
  label,
}: FilterTabProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={cn(
        "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm font-medium outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/40",
        active
          ? "border border-primary bg-primary-soft text-primary-text"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
      {active ? (
        <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-primary px-1.5 py-px font-mono text-2xs font-semibold text-primary-foreground">
          {count}
        </span>
      ) : (
        <span className="font-mono text-2xs text-subtle-foreground">
          {count}
        </span>
      )}
    </button>
  );
}

type FilterChipProps = {
  icon?: LucideIcon;
  label: string;
};

function FilterChip({ icon: Icon, label }: FilterChipProps) {
  return (
    <button
      type="button"
      className="inline-flex h-7 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border border-border bg-card px-2.5 text-xs font-medium text-foreground outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/40"
    >
      {Icon ? <Icon className="size-3" aria-hidden="true" /> : null}
      {label}
      <ChevronDown className="size-3" aria-hidden="true" />
    </button>
  );
}

function IssuesList({ issueRows }: { issueRows: IssueItem[] }) {
  const { t } = useTranslation();

  return (
    <section
      aria-label={t("issues.list.aria")}
      className="min-h-0 flex-1 overflow-auto bg-background"
    >
      <div role="list" className="min-w-[760px]">
        {issueRows.map((issue) => (
          <IssueListRow issue={issue} key={issue.id} />
        ))}
      </div>
    </section>
  );
}

function IssueListRow({ issue }: { issue: IssueItem }) {
  const { t } = useTranslation();
  const StatusIcon = statusIconMeta[issue.status].icon;

  return (
    <article
      role="listitem"
      className={cn(
        "flex min-h-[68px] items-center gap-3.5 border-b border-border px-5 py-3.5 transition-colors hover:bg-accent/40",
        issue.highlighted && "bg-destructive-soft hover:bg-destructive-soft",
      )}
    >
      <StatusIcon
        className={cn(
          "size-4 shrink-0",
          statusIconMeta[issue.status].className,
        )}
        aria-hidden="true"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="truncate text-sm font-semibold">{issue.title}</span>
          <IssueLabels labels={issue.labels} />
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 font-mono text-2xs">
          <span className="font-medium text-primary-text">{issue.id}</span>
          <span
            className={cn(
              "truncate text-muted-foreground",
              issue.status === "error" && "text-destructive",
            )}
          >
            · {issue.meta}
          </span>
        </div>
      </div>
      <div className="hidden shrink-0 items-center gap-3 md:flex">
        {issue.dispatchable ? (
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="h-6 border-primary bg-primary-soft font-mono text-2xs font-semibold text-primary-text hover:bg-primary-soft hover:text-primary-text"
          >
            <Send data-icon="inline-start" aria-hidden="true" />
            {t("issues.actions.dispatchToAgent")}
          </Button>
        ) : (
          <StatusPill status={issue.status} />
        )}
        <IssueCommentCount count={issue.comments} />
        <IssueAssignees agents={issue.agents} />
      </div>
    </article>
  );
}

function IssueLabels({ labels }: { labels: IssueLabel[] }) {
  if (labels.length === 0) {
    return null;
  }

  return (
    <span className="flex shrink-0 flex-wrap items-center gap-1.5">
      {labels.map((label) => (
        <Badge
          variant="secondary"
          className={cn(
            "rounded-full border-0 px-2 py-px font-mono text-2xs font-semibold",
            labelToneClassNames[label.tone],
          )}
          key={`${label.name}-${label.tone}`}
        >
          {label.name}
        </Badge>
      ))}
    </span>
  );
}

function IssueCommentCount({ count }: { count: number }) {
  return (
    <span className="inline-flex items-center gap-1 font-mono text-2xs font-medium text-muted-foreground">
      <MessageSquare className="size-3" aria-hidden="true" />
      {count}
    </span>
  );
}

function IssueAssignees({ agents }: { agents: IssueAgent[] }) {
  if (agents.length === 0) {
    return (
      <span className="inline-flex size-6 items-center justify-center rounded-lg border border-border text-muted-foreground">
        <Plus className="size-3" aria-hidden="true" />
      </span>
    );
  }

  return (
    <span className="flex items-center">
      {agents.map((agent, index) => (
        <AgentAvatar
          className={cn(
            "border-2 border-background shadow-xs",
            index > 0 && "-ml-1.5",
          )}
          color={agent.color}
          initials={agent.initials}
          key={agent.name}
          name={agent.name}
          size="sm"
        />
      ))}
    </span>
  );
}

function IssuesBoard({ boardColumns }: { boardColumns: BoardColumn[] }) {
  const { t } = useTranslation();

  return (
    <section
      aria-label={t("issues.board.aria")}
      className="min-h-0 flex-1 overflow-auto bg-sidebar px-5 py-4"
    >
      <div className="flex min-w-max items-start gap-4">
        {boardColumns.map((column) => (
          <IssueBoardColumn column={column} key={column.id} />
        ))}
      </div>
    </section>
  );
}

function IssueBoardColumn({ column }: { column: BoardColumn }) {
  const { t } = useTranslation();
  const tone = boardColumnToneClassNames[column.tone];

  return (
    <section className="flex w-80 shrink-0 flex-col gap-2 rounded-lg border border-border bg-card p-2.5">
      <div className="flex items-center gap-2 border-b border-border px-1.5 pb-2">
        <span
          className={cn("size-2 rounded-full", tone.dot)}
          aria-hidden="true"
        />
        <h2 className="text-xs font-semibold">{column.title}</h2>
        <span
          className={cn(
            "inline-flex min-w-6 items-center justify-center rounded-full px-1.5 py-px font-mono text-2xs font-semibold",
            tone.badge,
          )}
        >
          {column.count}
        </span>
        <div className="min-w-0 flex-1" />
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={t("issues.board.addToColumn", { column: column.title })}
          className="text-muted-foreground"
        >
          <Plus data-icon="only" aria-hidden="true" />
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        {column.issues.map((issue) => (
          <IssueBoardCard issue={issue} key={`${column.id}-${issue.id}`} />
        ))}
        {column.id === "backlog" ? (
          <button
            type="button"
            className="inline-flex cursor-pointer items-center justify-center gap-1.5 rounded-md border border-border bg-transparent px-2 py-2 text-2xs font-medium text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/40"
          >
            <Plus className="size-3" aria-hidden="true" />
            {t("issues.board.addCard")}
          </button>
        ) : null}
        {column.id === "closed" ? (
          <button
            type="button"
            className="inline-flex cursor-pointer items-center justify-center rounded-md px-2 py-1.5 font-mono text-2xs font-semibold text-primary-text outline-none transition-colors hover:bg-primary-soft focus-visible:ring-[3px] focus-visible:ring-ring/40"
          >
            {t("issues.board.viewAllClosed", { count: 47 })}
          </button>
        ) : null}
      </div>
    </section>
  );
}

function IssueBoardCard({ issue }: { issue: IssueItem }) {
  const { t } = useTranslation();
  const StatusIcon = statusIconMeta[issue.status].icon;
  const isError = issue.status === "error";

  return (
    <article
      className={cn(
        "flex flex-col gap-2 rounded-md border border-border bg-background px-3 py-2.5 shadow-xs",
        issue.id === "#142" && "border-primary",
        isError && "border-destructive bg-destructive-soft",
      )}
    >
      <div className="flex items-center gap-1.5">
        <StatusIcon
          className={cn(
            "size-3 shrink-0",
            statusIconMeta[issue.status].className,
          )}
          aria-hidden="true"
        />
        <span
          className={cn(
            "font-mono text-2xs font-semibold",
            isError ? "text-destructive" : "text-primary-text",
          )}
        >
          {issue.id}
        </span>
        <div className="min-w-0 flex-1" />
        {issue.status === "running" || issue.status === "error" ? (
          <StatusPill
            className={cn(
              isError && "bg-destructive text-destructive-foreground",
            )}
            status={issue.status}
          />
        ) : (
          <span className="font-mono text-2xs text-muted-foreground">
            {issue.meta.split("·").at(-1)?.trim()}
          </span>
        )}
      </div>
      <h3
        className={cn(
          "line-clamp-2 text-xs font-semibold leading-normal",
          columnClosedIssue(issue) && "text-muted-foreground",
        )}
      >
        {issue.title}
      </h3>
      {isError ? (
        <p className="font-mono text-2xs leading-normal text-destructive">
          {t("issues.board.lastFailure", { code: 124, minutes: 12 })}
        </p>
      ) : null}
      <IssueLabels labels={issue.labels} />
      <div className="flex items-center gap-2">
        <IssueAssignees agents={issue.agents} />
        {issue.dispatchable && issue.agents.length === 0 ? (
          <span className="font-mono text-2xs font-medium text-muted-foreground">
            {t("issues.status.unassigned")}
          </span>
        ) : null}
        <div className="min-w-0 flex-1" />
        <IssueCommentCount count={issue.comments} />
      </div>
      {isError ? (
        <Button
          type="button"
          size="xs"
          variant="destructive"
          className="h-6 self-start font-mono text-2xs font-semibold"
        >
          {t("issues.actions.redispatch")}
        </Button>
      ) : null}
    </article>
  );
}

function columnClosedIssue(issue: IssueItem) {
  return issue.closed === true;
}

export { IssuesPage };
