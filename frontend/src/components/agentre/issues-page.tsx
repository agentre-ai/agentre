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

const issueRows: IssueItem[] = [
  {
    id: "#142",
    title: "修复 OAuth 回调在 Safari 下丢失 state 参数",
    labels: [
      { name: "bug", tone: "bug" },
      { name: "auth", tone: "auth" },
    ],
    status: "running",
    meta: "由 邮件 Hook 创建 · 14:32 · 最近更新 3 分钟前",
    comments: 4,
    agents: [
      { name: "Kai", initials: "K", color: "agent-2" },
      { name: "Parker", initials: "P", color: "agent-1" },
    ],
  },
  {
    id: "#141",
    title: "实现深色模式与 Slack 风格消息流",
    labels: [
      { name: "feature", tone: "feature" },
      { name: "ui", tone: "ui" },
    ],
    status: "running",
    meta: "由 手动创建 · 昨天 · 最近更新 12 分钟前",
    comments: 12,
    agents: [{ name: "Eva", initials: "E", color: "agent-3" }],
  },
  {
    id: "#140",
    title: "部门嵌套缩放下卡顿（≥ 4 层）",
    labels: [{ name: "perf", tone: "perf" }],
    status: "waiting",
    meta: "由 手动创建 · 2 天前 · 等待用户审批",
    comments: 7,
    agents: [{ name: "Dora", initials: "D", color: "agent-4" }],
  },
  {
    id: "#139",
    title: "邮箱 Hook 重连重试间隔过短，频繁触发限流",
    labels: [
      { name: "hook", tone: "hook" },
      { name: "ops", tone: "ops" },
    ],
    status: "waiting",
    meta: "由 GitHub Webhook 创建 · 3 天前 · 等待 CEO 助手批准",
    comments: 3,
    agents: [{ name: "Bea", initials: "B", color: "agent-5" }],
  },
  {
    id: "#138",
    title: "[紧急] Codex CLI 调用偶发超时导致会话中断",
    labels: [
      { name: "bug", tone: "bug" },
      { name: "critical", tone: "critical" },
    ],
    status: "error",
    meta: "由 Sentry Webhook 创建 · 4 天前 · 调用失败 exit=124",
    comments: 9,
    agents: [{ name: "Kai", initials: "K", color: "agent-2" }],
    highlighted: true,
  },
  {
    id: "#137",
    title: "文档：Agent 后端配置示例与常见错误",
    labels: [{ name: "docs", tone: "docs" }],
    status: "idle",
    meta: "由 手动创建 · 5 天前 · 未派发",
    comments: 0,
    agents: [],
    dispatchable: true,
  },
  {
    id: "#136",
    title: "重构 ChatArea 消息列表性能（虚拟滚动）",
    labels: [
      { name: "refactor", tone: "refactor" },
      { name: "ui", tone: "ui" },
    ],
    status: "running",
    meta: "由 手动创建 · 6 天前 · 最近更新 1 小时前",
    comments: 18,
    agents: [
      { name: "Eva", initials: "E", color: "agent-3" },
      { name: "Parker", initials: "P", color: "agent-1" },
    ],
  },
];

const backlogIssues: IssueItem[] = [
  issueRows[5],
  {
    id: "#133",
    title: "研究 LLM cost dashboard 方案",
    labels: [{ name: "refactor", tone: "refactor" }],
    status: "idle",
    meta: "由 手动创建 · 7 天前 · 未派发",
    comments: 0,
    agents: [],
    dispatchable: true,
  },
];

const closedIssues: IssueItem[] = [
  {
    id: "#132",
    title: "修复部门 banner 折叠状态丢失",
    labels: [{ name: "bug", tone: "bug" }],
    status: "idle",
    meta: "已关闭 · 昨天",
    comments: 2,
    agents: [],
  },
  {
    id: "#129",
    title: "修复 Hook 路由匹配优先级",
    labels: [],
    status: "running",
    meta: "已关闭 · 3 天前",
    comments: 5,
    agents: [],
  },
];

const boardColumns: BoardColumn[] = [
  {
    id: "backlog",
    title: "待派发",
    count: 2,
    tone: "idle",
    issues: backlogIssues,
  },
  {
    id: "running",
    title: "进行中",
    count: 4,
    tone: "running",
    issues: [issueRows[0], issueRows[1], issueRows[4], issueRows[6]],
  },
  {
    id: "waiting",
    title: "待审批",
    count: 2,
    tone: "waiting",
    issues: [issueRows[2], issueRows[3]],
  },
  {
    id: "closed",
    title: "已关闭",
    count: 47,
    tone: "running",
    issues: closedIssues,
  },
];

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

function IssuesPage() {
  const [view, setView] = React.useState<IssueView>("list");
  const summary =
    view === "list"
      ? "12 个 Open · 47 个 Closed · 3 个 Agent 在跟进"
      : "按状态分列 · 拖卡片可在列间流转";

  return (
    <main
      className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
      data-slot="issues-page"
    >
      <IssuesHeader view={view} summary={summary} onViewChange={setView} />
      <IssueFilterBar />
      {view === "list" ? <IssuesList /> : <IssuesBoard />}
    </main>
  );
}

type IssuesHeaderProps = {
  onViewChange: (view: IssueView) => void;
  summary: string;
  view: IssueView;
};

function IssuesHeader({ onViewChange, summary, view }: IssuesHeaderProps) {
  return (
    <header className="flex min-h-[60px] shrink-0 flex-wrap items-center gap-3 border-b border-border bg-background px-5 py-3 lg:h-[60px] lg:flex-nowrap lg:py-0">
      <div className="flex min-w-0 flex-col gap-0.5">
        <h1 className="truncate text-base font-semibold tracking-normal">
          看板
        </h1>
        <p className="truncate text-2xs text-muted-foreground">{summary}</p>
      </div>
      <div className="min-w-0 flex-1" />
      <div
        className="flex h-[30px] shrink-0 items-center gap-0.5 rounded-md border border-border bg-secondary p-0.5"
        aria-label="Issue 视图"
      >
        <IssueViewButton
          active={view === "list"}
          icon={List}
          label="List"
          onClick={() => onViewChange("list")}
        />
        <IssueViewButton
          active={view === "board"}
          icon={Columns3}
          label="Board"
          onClick={() => onViewChange("board")}
        />
      </div>
      <Button type="button" variant="outline" size="sm" className="h-[30px]">
        <SlidersHorizontal data-icon="inline-start" aria-hidden="true" />
        筛选
      </Button>
      <Button type="button" size="sm" className="h-[30px]">
        <Plus data-icon="inline-start" aria-hidden="true" />
        新建 Issue
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
  return (
    <div className="flex min-h-12 shrink-0 items-center gap-2 overflow-x-auto border-b border-border bg-sidebar px-5 py-2">
      <div className="flex shrink-0 items-center gap-0.5">
        <FilterTab active icon={CircleDot} label="Open" count={12} />
        <FilterTab icon={CircleCheck} label="Closed" count={47} />
      </div>
      <div className="min-w-0 flex-1" />
      <FilterChip label="作者" />
      <FilterChip label="标签" />
      <FilterChip label="分派 Agent" />
      <FilterChip icon={ArrowDownUp} label="最新更新" />
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

function IssuesList() {
  return (
    <section
      aria-label="Issue 列表"
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
            派发给 Agent
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

function IssuesBoard() {
  return (
    <section
      aria-label="Issue 看板"
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
          aria-label={`添加到${column.title}`}
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
            添加卡片
          </button>
        ) : null}
        {column.id === "closed" ? (
          <button
            type="button"
            className="inline-flex cursor-pointer items-center justify-center rounded-md px-2 py-1.5 font-mono text-2xs font-semibold text-primary-text outline-none transition-colors hover:bg-primary-soft focus-visible:ring-[3px] focus-visible:ring-ring/40"
          >
            查看全部 47 个 →
          </button>
        ) : null}
      </div>
    </section>
  );
}

function IssueBoardCard({ issue }: { issue: IssueItem }) {
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
          exit=124 · last fail 12 分钟前
        </p>
      ) : null}
      <IssueLabels labels={issue.labels} />
      <div className="flex items-center gap-2">
        <IssueAssignees agents={issue.agents} />
        {issue.dispatchable && issue.agents.length === 0 ? (
          <span className="font-mono text-2xs font-medium text-muted-foreground">
            未派发
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
          重新派发
        </Button>
      ) : null}
    </article>
  );
}

function columnClosedIssue(issue: IssueItem) {
  return issue.meta.startsWith("已关闭");
}

export { IssuesPage };
