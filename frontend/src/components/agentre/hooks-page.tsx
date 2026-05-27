import * as React from "react";
import {
  AlertCircle,
  Bell,
  CalendarClock,
  CheckCircle2,
  ChevronRight,
  Clock3,
  Copy,
  FileJson,
  Github,
  Inbox,
  Loader2,
  Mail,
  MessageSquare,
  MoreHorizontal,
  Plus,
  Power,
  PowerOff,
  RefreshCw,
  Route,
  Save,
  Search,
  Send,
  ShieldCheck,
  Trash2,
  Webhook,
  XCircle,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

type SourceKind =
  | "email"
  | "github"
  | "slack"
  | "schedule"
  | "webhook"
  | "system";
type ConnectionStatus = "connected" | "pending" | "disabled" | "error";
type EventStatus = "dispatched" | "unmatched" | "failed";

type SourceConfig = {
  webhookUrl: string;
  secret: string;
  verifySignature: boolean;
  events: string[];
  imapServer: string;
  imapPort: number;
  imapMailbox: string;
  useTls: boolean;
  emailAddress: string;
  appPassword: string;
  pollingInterval: string;
  lastUid: number;
  uidValidity: number;
  botToken: string;
  channel: string;
  cronExpr: string;
  timezone: string;
  systemPermission: string;
};

type HookSourceItem = {
  id: number;
  kind: SourceKind;
  name: string;
  description: string;
  identifier: string;
  config: SourceConfig;
  enabled: boolean;
  connectionStatus: ConnectionStatus;
  lastSyncTime: number;
  totalCount: number;
  createtime: number;
  updatetime: number;
};

type AgentOption = {
  id: number;
  name: string;
  avatarColor: string;
  agentStatus: string;
  systemBadge: string;
  departmentId: number;
};

type HookRuleItem = {
  id: number;
  sourceId: number;
  name: string;
  conditionExpr: string;
  targetAgentId: number;
  targetAgentName: string;
  enabled: boolean;
  isFallback: boolean;
  sortOrder: number;
  createtime: number;
  updatetime: number;
};

type RuleMatchResult = {
  ruleId: number;
  ruleName: string;
  matched: boolean;
  reason: string;
  agentId: number;
  agentName: string;
};

type HookDispatchItem = {
  agentId: number;
  agentName: string;
  sessionId: string;
  status: string;
  message: string;
};

type HookEventItem = {
  id: number;
  sourceId: number;
  sourceName: string;
  title: string;
  sourceRef: string;
  sender: string;
  eventType: string;
  eventStatus: EventStatus;
  payloadJson: string;
  matchedRules: RuleMatchResult[];
  dispatches: HookDispatchItem[];
  matchedRuleNames: string[];
  targetAgentNames: string[];
  receivedAt: number;
  createtime: number;
  updatetime: number;
};

type LoadHooksResponse = {
  sources: HookSourceItem[];
  rules: HookRuleItem[];
  events: HookEventItem[];
  agents: AgentOption[];
};

type HookBridge = {
  LoadHooks: (req: {
    sourceId?: number;
    limit?: number;
  }) => Promise<LoadHooksResponse>;
  CreateHookSource: (req: {
    kind: SourceKind;
    name: string;
    description: string;
    identifier: string;
    config: SourceConfig;
    enabled: boolean;
  }) => Promise<{ item: HookSourceItem }>;
  UpdateHookSource: (req: {
    id: number;
    kind: SourceKind;
    name: string;
    description: string;
    identifier: string;
    config: SourceConfig;
    enabled: boolean;
  }) => Promise<{ item: HookSourceItem }>;
  DeleteHookSource: (req: { id: number }) => Promise<Record<string, never>>;
  CreateHookRule: (req: {
    sourceId: number;
    name: string;
    conditionExpr: string;
    targetAgentId: number;
    enabled: boolean;
  }) => Promise<{ item: HookRuleItem }>;
  UpdateHookRule: (req: {
    id: number;
    name: string;
    conditionExpr: string;
    targetAgentId: number;
    enabled: boolean;
  }) => Promise<{ item: HookRuleItem }>;
  DeleteHookRule: (req: { id: number }) => Promise<Record<string, never>>;
  TestHookSource: (req: { id: number }) => Promise<{
    item: HookSourceItem;
    event: HookEventItem;
  }>;
  SyncHookEmailSource: (req: { id: number; limit?: number }) => Promise<{
    item: HookSourceItem;
    events: HookEventItem[];
    created: number;
    skipped: number;
  }>;
  RedeliverHookEvent: (req: {
    id: number;
    targetAgentId: number;
  }) => Promise<{ item: HookEventItem }>;
};

type HooksData = LoadHooksResponse;
type HookTab = "config" | "log";
type StatusFilter = "all" | EventStatus;
type FlashState = { kind: "ok" | "err"; text: string } | null;

type SourceDraft = {
  kind: SourceKind;
  name: string;
  description: string;
  identifier: string;
  enabled: boolean;
  config: SourceConfig;
};

type RuleDraft = {
  name: string;
  conditionExpr: string;
  targetAgentId: number;
  enabled: boolean;
};

const emptyConfig: SourceConfig = {
  webhookUrl: "",
  secret: "",
  verifySignature: true,
  events: [],
  imapServer: "",
  imapPort: 993,
  imapMailbox: "INBOX",
  useTls: true,
  emailAddress: "",
  appPassword: "",
  pollingInterval: "5m",
  lastUid: 0,
  uidValidity: 0,
  botToken: "",
  channel: "",
  cronExpr: "0 9 * * 1-5",
  timezone: "Asia/Shanghai",
  systemPermission: "",
};
const maskedSecret = "********";

const sourceKindMeta: Record<
  SourceKind,
  { icon: LucideIcon; label: string; shortLabel: string }
> = {
  email: { icon: Mail, label: "邮箱", shortLabel: "邮箱" },
  github: { icon: Github, label: "GitHub Webhook", shortLabel: "GitHub" },
  slack: { icon: MessageSquare, label: "Slack DM", shortLabel: "Slack" },
  schedule: { icon: CalendarClock, label: "定时器", shortLabel: "定时器" },
  webhook: { icon: Webhook, label: "自定义 Webhook", shortLabel: "Webhook" },
  system: { icon: Bell, label: "系统通知", shortLabel: "系统" },
};

const sourceKindOptions = Object.entries(sourceKindMeta).map(
  ([value, meta]) => ({
    value: value as SourceKind,
    ...meta,
  }),
);

const connectionStatusMeta: Record<
  ConnectionStatus,
  {
    label: string;
    className: string;
    icon: LucideIcon;
  }
> = {
  connected: {
    label: "已连接",
    className: "bg-status-running-bg text-status-running",
    icon: CheckCircle2,
  },
  pending: {
    label: "待验证",
    className: "bg-status-waiting-bg text-status-waiting",
    icon: Clock3,
  },
  disabled: {
    label: "已停用",
    className: "bg-secondary text-muted-foreground",
    icon: XCircle,
  },
  error: {
    label: "连接失败",
    className: "bg-destructive-soft text-status-error",
    icon: AlertCircle,
  },
};

const eventStatusMeta: Record<
  EventStatus,
  { label: string; dot: string; pill: string }
> = {
  dispatched: {
    label: "已派发",
    dot: "bg-status-running",
    pill: "bg-status-running-bg text-status-running",
  },
  unmatched: {
    label: "未匹配",
    dot: "bg-status-waiting",
    pill: "bg-status-waiting-bg text-status-waiting",
  },
  failed: {
    label: "失败",
    dot: "bg-status-error",
    pill: "bg-destructive-soft text-status-error",
  },
};

function isSourceKind(value: unknown): value is SourceKind {
  return typeof value === "string" && value in sourceKindMeta;
}

function isConnectionStatus(value: unknown): value is ConnectionStatus {
  return (
    value === "connected" ||
    value === "pending" ||
    value === "disabled" ||
    value === "error"
  );
}

function isEventStatus(value: unknown): value is EventStatus {
  return typeof value === "string" && value in eventStatusMeta;
}

function normalizeConfig(config?: Partial<SourceConfig> | null): SourceConfig {
  const raw = config ?? {};

  return {
    ...emptyConfig,
    ...raw,
    events: Array.isArray(raw.events) ? raw.events : [],
    imapPort:
      typeof raw.imapPort === "number" && Number.isFinite(raw.imapPort)
        ? raw.imapPort
        : emptyConfig.imapPort,
    imapMailbox:
      typeof raw.imapMailbox === "string" && raw.imapMailbox.trim()
        ? raw.imapMailbox
        : emptyConfig.imapMailbox,
    useTls: typeof raw.useTls === "boolean" ? raw.useTls : emptyConfig.useTls,
    lastUid:
      typeof raw.lastUid === "number" && Number.isFinite(raw.lastUid)
        ? raw.lastUid
        : emptyConfig.lastUid,
    uidValidity:
      typeof raw.uidValidity === "number" && Number.isFinite(raw.uidValidity)
        ? raw.uidValidity
        : emptyConfig.uidValidity,
    verifySignature:
      typeof raw.verifySignature === "boolean"
        ? raw.verifySignature
        : emptyConfig.verifySignature,
  };
}

function normalizeSource(source: HookSourceItem): HookSourceItem {
  return {
    ...source,
    kind: isSourceKind(source.kind) ? source.kind : "webhook",
    config: normalizeConfig(source.config),
    connectionStatus: isConnectionStatus(source.connectionStatus)
      ? source.connectionStatus
      : "pending",
  };
}

function normalizeEvent(event: HookEventItem): HookEventItem {
  return {
    ...event,
    eventStatus: isEventStatus(event.eventStatus)
      ? event.eventStatus
      : "failed",
    matchedRules: Array.isArray(event.matchedRules) ? event.matchedRules : [],
    dispatches: Array.isArray(event.dispatches) ? event.dispatches : [],
    matchedRuleNames: Array.isArray(event.matchedRuleNames)
      ? event.matchedRuleNames
      : [],
    targetAgentNames: Array.isArray(event.targetAgentNames)
      ? event.targetAgentNames
      : [],
    payloadJson: event.payloadJson || "{}",
  };
}

function normalizeHooksData(resp: LoadHooksResponse): HooksData {
  return {
    sources: (resp.sources ?? []).map(normalizeSource),
    rules: resp.rules ?? [],
    events: (resp.events ?? []).map(normalizeEvent),
    agents: resp.agents ?? [],
  };
}

function getBridge() {
  return (
    window as unknown as {
      go?: { app?: { App?: HookBridge } };
    }
  ).go?.app?.App;
}

function getBridgeMethod<K extends keyof HookBridge>(name: K): HookBridge[K] {
  const bridge = getBridge();
  const method = bridge?.[name];
  if (typeof method !== "function") {
    throw new Error(`Wails method ${String(name)} is unavailable`);
  }
  return method.bind(bridge) as HookBridge[K];
}

function sourceToDraft(source?: HookSourceItem | null): SourceDraft {
  if (!source) {
    return {
      kind: "github",
      name: "",
      description: "",
      identifier: "",
      enabled: true,
      config: { ...emptyConfig },
    };
  }
  return {
    kind: source.kind,
    name: source.name,
    description: source.description,
    identifier: source.identifier,
    enabled: source.enabled,
    config: normalizeConfig(source.config),
  };
}

function ruleToDraft(rule?: HookRuleItem | null): RuleDraft {
  if (!rule) {
    return {
      name: "",
      conditionExpr: "",
      targetAgentId: 0,
      enabled: true,
    };
  }
  return {
    name: rule.name,
    conditionExpr: rule.conditionExpr,
    targetAgentId: rule.targetAgentId,
    enabled: rule.enabled,
  };
}

function normaliseQuery(value: string) {
  return value.trim().toLowerCase();
}

function sourceSubtitle(source: HookSourceItem) {
  if (source.kind === "email")
    return source.config.emailAddress || source.identifier;
  if (source.kind === "schedule") {
    return [source.config.cronExpr, source.config.timezone]
      .filter(Boolean)
      .join(" · ");
  }
  if (source.kind === "slack")
    return source.config.channel || source.identifier;
  if (source.kind === "system") {
    return source.enabled ? "macOS 通知中心" : "macOS 通知中心 · 已禁用";
  }
  return source.identifier || sourceKindMeta[source.kind].label;
}

function eventMatchesQuery(event: HookEventItem, query: string) {
  const q = normaliseQuery(query);
  if (!q) return true;
  return [
    event.title,
    event.sender,
    event.eventType,
    event.sourceName,
    event.sourceRef,
    event.payloadJson,
  ]
    .join(" ")
    .toLowerCase()
    .includes(q);
}

function formatRelativeTime(seconds: number) {
  if (!seconds) return "从未";
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - seconds);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function formatDateTime(seconds: number) {
  if (!seconds) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(seconds * 1000));
}

function prettyJSON(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw || "{}";
  }
}

function replaceById<T extends { id: number }>(items: T[], item: T) {
  return items.map((current) => (current.id === item.id ? item : current));
}

function prependUniqueEvents(
  current: HookEventItem[],
  incoming: HookEventItem[],
) {
  const incomingIds = new Set(incoming.map((event) => event.id));
  return [
    ...incoming,
    ...current.filter((event) => !incomingIds.has(event.id)),
  ];
}

function StatusPill({ status }: { status: ConnectionStatus | EventStatus }) {
  if (status in connectionStatusMeta) {
    const meta = connectionStatusMeta[status as ConnectionStatus];
    const Icon = meta.icon;
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center gap-1 rounded-sm px-1.5 py-0.5 font-mono text-2xs font-semibold",
          meta.className,
        )}
      >
        <Icon aria-hidden="true" />
        {meta.label}
      </span>
    );
  }
  const meta = eventStatusMeta[status as EventStatus];
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-sm px-1.5 py-0.5 font-mono text-2xs font-semibold",
        meta.pill,
      )}
    >
      <span className={cn("size-1.5 rounded-full", meta.dot)} />
      {meta.label}
    </span>
  );
}

function TextLabel({
  children,
  htmlFor,
}: {
  children: React.ReactNode;
  htmlFor?: string;
}) {
  return (
    <label htmlFor={htmlFor} className="text-xs font-semibold text-foreground">
      {children}
    </label>
  );
}

function FormRow({
  children,
  description,
  label,
}: {
  children: React.ReactNode;
  description?: string;
  label: string;
}) {
  return (
    <div className="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-[160px_minmax(0,1fr)]">
      <div className="flex flex-col gap-1 pt-1">
        <TextLabel>{label}</TextLabel>
        {description ? (
          <p className="text-2xs leading-relaxed text-muted-foreground">
            {description}
          </p>
        ) : null}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function SourceIcon({ kind }: { kind: SourceKind }) {
  const Icon = sourceKindMeta[kind].icon;
  return (
    <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-card text-primary-text">
      <Icon aria-hidden="true" />
    </span>
  );
}

function SourceList({
  activeId,
  query,
  sources,
  onNew,
  onQueryChange,
  onSelect,
}: {
  activeId: number | null;
  query: string;
  sources: HookSourceItem[];
  onNew: () => void;
  onQueryChange: (query: string) => void;
  onSelect: (sourceId: number) => void;
}) {
  const filtered = sources.filter((source) => {
    const q = normaliseQuery(query);
    if (!q) return true;
    return [source.name, source.identifier, sourceSubtitle(source), source.kind]
      .join(" ")
      .toLowerCase()
      .includes(q);
  });
  const messageSources = filtered.filter((source) =>
    ["email", "github", "slack"].includes(source.kind),
  );
  const systemSources = filtered.filter((source) =>
    ["schedule", "webhook", "system"].includes(source.kind),
  );

  return (
    <aside
      aria-label="信号源列表"
      className="flex w-full shrink-0 flex-col border-b border-border bg-sidebar lg:w-[260px] lg:border-b-0 lg:border-r"
    >
      <div className="flex flex-col gap-3 border-b border-border px-3.5 py-3">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <h2 className="text-sm font-semibold">信号源</h2>
            <span className="font-mono text-2xs text-muted-foreground">
              {sources.length}
            </span>
          </div>
          <Button size="sm" className="h-7 gap-1.5 px-2.5" onClick={onNew}>
            <Plus data-icon="inline-start" aria-hidden="true" />
            新建
          </Button>
        </div>
        <div className="flex h-8 min-w-0 items-center gap-2 rounded-md border border-input bg-input-bg px-2.5">
          <Search aria-hidden="true" className="text-muted-foreground" />
          <input
            aria-label="搜索信号源"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder="搜索信号源"
            className="min-w-0 flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground"
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-1.5 py-2">
        <SourceGroup
          label="消息源"
          sources={messageSources}
          activeId={activeId}
          onSelect={onSelect}
        />
        <SourceGroup
          label="事件源"
          sources={systemSources}
          activeId={activeId}
          onSelect={onSelect}
        />
      </div>
    </aside>
  );
}

function SourceGroup({
  activeId,
  label,
  onSelect,
  sources,
}: {
  activeId: number | null;
  label: string;
  onSelect: (sourceId: number) => void;
  sources: HookSourceItem[];
}) {
  if (sources.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col gap-1 pb-2">
      <div className="px-2 py-1 font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
        {label}
      </div>
      {sources.map((source) => {
        const active = source.id === activeId;
        const meta = sourceKindMeta[source.kind];
        const Icon = meta.icon;
        return (
          <button
            key={source.id}
            type="button"
            aria-current={active ? "page" : undefined}
            onClick={() => onSelect(source.id)}
            className={cn(
              "flex min-w-0 items-center gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-accent",
              active &&
                "border-l-2 border-primary bg-primary-soft text-primary-text hover:bg-primary-soft",
            )}
          >
            <span
              className={cn(
                "inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground",
                active && "border-primary/30 text-primary-text",
              )}
            >
              <Icon aria-hidden="true" />
            </span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span
                data-selectable-text="true"
                className={cn(
                  "truncate text-xs font-semibold text-foreground",
                  active && "text-primary-text",
                )}
              >
                {source.name}
              </span>
              <span className="truncate font-mono text-2xs text-muted-foreground">
                {meta.shortLabel} · {source.totalCount || 0} 触发
              </span>
            </span>
            {source.enabled ? (
              <span className="size-1.5 rounded-full bg-status-running" />
            ) : (
              <span className="font-mono text-2xs font-semibold text-muted-foreground">
                OFF
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

function HooksPageHeader({
  activeTab,
  busy,
  eventCount,
  onDelete,
  onSyncEmail,
  onTabChange,
  onTest,
  onToggleEnabled,
  source,
}: {
  activeTab: HookTab;
  busy: boolean;
  eventCount: number;
  onDelete: () => void;
  onSyncEmail: () => void;
  onTabChange: (tab: HookTab) => void;
  onTest: () => void;
  onToggleEnabled: () => void;
  source: HookSourceItem | null;
}) {
  const [actionsOpen, setActionsOpen] = React.useState(false);

  if (!source) {
    return (
      <div className="flex h-[120px] shrink-0 items-center justify-between border-b border-border px-6">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="text-lg font-semibold">Hook</h1>
          <p className="text-xs text-muted-foreground">
            新建一个信号源后配置连接和路由规则。
          </p>
        </div>
      </div>
    );
  }

  const meta = sourceKindMeta[source.kind];

  return (
    <div className="flex shrink-0 flex-col border-b border-border bg-background">
      <div className="flex min-h-[76px] flex-wrap items-center justify-between gap-3 px-6 py-4">
        <div className="flex min-w-0 items-start gap-3">
          <SourceIcon kind={source.kind} />
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <h1
                data-selectable-text="true"
                className="truncate text-lg font-semibold"
              >
                {source.name}
              </h1>
              <span className="text-muted-foreground">·</span>
              <span className="text-sm font-medium text-muted-foreground">
                {meta.label}
              </span>
              <StatusPill status={source.connectionStatus} />
            </div>
            <div className="flex flex-wrap items-center gap-2 font-mono text-2xs text-muted-foreground">
              <span>{sourceSubtitle(source) || "未配置标识"}</span>
              <span>·</span>
              <span>总计 {source.totalCount}</span>
              <span>·</span>
              <span>同步 {formatRelativeTime(source.lastSyncTime)}</span>
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" onClick={onTest} disabled={busy}>
            {busy ? (
              <Loader2
                data-icon="inline-start"
                className="animate-spin"
                aria-hidden="true"
              />
            ) : (
              <RefreshCw data-icon="inline-start" aria-hidden="true" />
            )}
            测试连接
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={onToggleEnabled}
            disabled={busy}
          >
            {source.enabled ? (
              <PowerOff data-icon="inline-start" aria-hidden="true" />
            ) : (
              <Power data-icon="inline-start" aria-hidden="true" />
            )}
            {source.enabled ? "停用" : "启用"}
          </Button>
          <Popover open={actionsOpen} onOpenChange={setActionsOpen}>
            <PopoverTrigger asChild>
              <Button
                variant="outline"
                size="icon-sm"
                aria-label="更多操作"
                title="更多操作"
                disabled={busy}
              >
                <MoreHorizontal data-icon="only" aria-hidden="true" />
              </Button>
            </PopoverTrigger>
            <PopoverContent
              align="end"
              className="flex w-44 flex-col gap-1 p-1"
            >
              {source.kind === "email" ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="justify-start"
                  onClick={() => {
                    setActionsOpen(false);
                    onSyncEmail();
                  }}
                  disabled={busy || !source.enabled}
                >
                  <Mail data-icon="inline-start" aria-hidden="true" />
                  同步邮箱
                </Button>
              ) : null}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="justify-start text-status-error hover:text-status-error"
                onClick={() => {
                  setActionsOpen(false);
                  onDelete();
                }}
              >
                <Trash2 data-icon="inline-start" aria-hidden="true" />
                删除
              </Button>
            </PopoverContent>
          </Popover>
        </div>
      </div>
      <div className="flex h-11 items-end gap-1 px-6">
        <TabButton
          active={activeTab === "config"}
          onClick={() => onTabChange("config")}
        >
          配置
        </TabButton>
        <TabButton
          active={activeTab === "log"}
          onClick={() => onTabChange("log")}
        >
          事件日志
          <span
            className={cn(
              "ml-1 rounded-sm px-1.5 py-0.5 font-mono text-2xs",
              activeTab === "log"
                ? "bg-primary text-primary-foreground"
                : "bg-secondary text-muted-foreground",
            )}
          >
            {eventCount}
          </span>
        </TabButton>
      </div>
    </div>
  );
}

function TabButton({
  active,
  children,
  onClick,
}: {
  active: boolean;
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-current={active ? "page" : undefined}
      onClick={onClick}
      className={cn(
        "flex h-11 items-center border-b-2 px-3 text-sm font-medium text-muted-foreground",
        active
          ? "border-primary text-primary-text"
          : "border-transparent hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function SourceConfigPanel({
  agents,
  busy,
  rules,
  source,
  onCreateRule,
  onDeleteRule,
  onRuleDialog,
  onSaveSource,
  onUpdateRule,
}: {
  agents: AgentOption[];
  busy: boolean;
  rules: HookRuleItem[];
  source: HookSourceItem;
  onCreateRule: () => void;
  onDeleteRule: (rule: HookRuleItem) => void;
  onRuleDialog: (rule: HookRuleItem) => void;
  onSaveSource: (draft: SourceDraft) => void;
  onUpdateRule: (rule: HookRuleItem, patch: Partial<RuleDraft>) => void;
}) {
  const [draft, setDraft] = React.useState<SourceDraft>(() =>
    sourceToDraft(source),
  );

  const setConfig = <K extends keyof SourceConfig>(
    key: K,
    value: SourceConfig[K],
  ) => {
    setDraft((current) => ({
      ...current,
      config: { ...current.config, [key]: value },
    }));
  };

  const selectedEvents = draft.config.events.join(", ");
  const enabledTitle =
    draft.kind === "email"
      ? draft.enabled
        ? "自动轮询"
        : "暂停轮询"
      : draft.enabled
        ? "接收事件"
        : "暂停接收";
  const enabledDescription =
    draft.kind === "email"
      ? "启用后按轮询间隔自动拉取未读邮件；停用后保留配置和历史日志。"
      : "停用后保留配置和历史日志，不再触发路由。";

  return (
    <div className="min-h-0 flex-1 overflow-y-auto bg-background px-6 py-5">
      <div className="mx-auto flex max-w-5xl flex-col gap-5">
        <section
          aria-label="连接配置"
          className="flex min-w-0 flex-col gap-4 rounded-lg border border-border bg-card p-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex min-w-0 items-start gap-3">
              <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg bg-secondary text-primary-text">
                <ShieldCheck aria-hidden="true" />
              </span>
              <div className="flex min-w-0 flex-col gap-1">
                <h2 className="text-sm font-semibold">连接配置</h2>
                <p className="text-2xs text-muted-foreground">
                  {sourceKindMeta[draft.kind].label} 的入口参数和连接状态。
                </p>
              </div>
            </div>
            <StatusPill status={source.connectionStatus} />
          </div>

          <div className="flex flex-col gap-4">
            <FormRow label="基础信息">
              <div className="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-2">
                <Input
                  aria-label="信号源名称"
                  value={draft.name}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      name: event.target.value,
                    }))
                  }
                  placeholder="agentre-bot"
                />
                <Select
                  value={draft.kind}
                  onValueChange={(value) =>
                    setDraft((current) => ({
                      ...current,
                      kind: value as SourceKind,
                    }))
                  }
                >
                  <SelectTrigger aria-label="信号源类型">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {sourceKindOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            </FormRow>

            <FormRow label="显示标识" description="用于列表和事件来源摘要。">
              <Input
                aria-label="信号源标识"
                value={draft.identifier}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    identifier: event.target.value,
                  }))
                }
                placeholder="agentre-frame"
              />
            </FormRow>

            <KindSpecificFields
              draft={draft}
              selectedEvents={selectedEvents}
              setConfig={setConfig}
            />

            <FormRow label="启用状态">
              <div className="flex items-center gap-3 rounded-md border border-border bg-secondary/40 px-3 py-2">
                <Switch
                  aria-label="启用信号源"
                  checked={draft.enabled}
                  onCheckedChange={(checked) =>
                    setDraft((current) => ({ ...current, enabled: checked }))
                  }
                />
                <div className="flex flex-col gap-0.5">
                  <span className="text-xs font-medium">{enabledTitle}</span>
                  <span className="text-2xs text-muted-foreground">
                    {enabledDescription}
                  </span>
                </div>
              </div>
            </FormRow>
          </div>

          <div className="flex justify-end border-t border-border pt-4">
            <Button onClick={() => onSaveSource(draft)} disabled={busy}>
              {busy ? (
                <Loader2
                  data-icon="inline-start"
                  className="animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <Save data-icon="inline-start" aria-hidden="true" />
              )}
              保存配置
            </Button>
          </div>
        </section>

        <section
          aria-label="路由规则"
          className="flex min-w-0 flex-col gap-4 rounded-lg border border-border bg-card p-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex min-w-0 items-start gap-3">
              <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg bg-secondary text-primary-text">
                <Route aria-hidden="true" />
              </span>
              <div className="flex min-w-0 flex-col gap-1">
                <div className="flex items-center gap-2">
                  <h2 className="text-sm font-semibold">路由规则</h2>
                  <span className="font-mono text-2xs text-muted-foreground">
                    {rules.length}
                  </span>
                </div>
                <p className="text-2xs text-muted-foreground">
                  所有匹配的规则都会执行；每个目标 Agent 生成独立派发记录。
                </p>
              </div>
            </div>
            <Button size="sm" onClick={onCreateRule}>
              <Plus data-icon="inline-start" aria-hidden="true" />
              新建规则
            </Button>
          </div>

          <div className="flex flex-col gap-2">
            {rules
              .filter((rule) => !rule.isFallback)
              .map((rule) => (
                <RuleRow
                  key={rule.id}
                  agents={agents}
                  rule={rule}
                  onDelete={() => onDeleteRule(rule)}
                  onEdit={() => onRuleDialog(rule)}
                  onUpdate={(patch) => onUpdateRule(rule, patch)}
                />
              ))}
            {rules.filter((rule) => !rule.isFallback).length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-3 py-6 text-center text-xs text-muted-foreground">
                暂无自定义规则。事件会落到兜底规则。
              </div>
            ) : null}
            {rules
              .filter((rule) => rule.isFallback)
              .map((rule) => (
                <div
                  key={rule.id}
                  className="mt-1 flex min-w-0 items-center gap-3 rounded-md border border-dashed border-status-waiting bg-status-waiting-bg px-3 py-3"
                >
                  <AlertCircle
                    className="shrink-0 text-status-waiting"
                    aria-hidden="true"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-semibold text-foreground">
                        {rule.name}
                      </span>
                      <Badge variant="secondary" className="font-mono text-2xs">
                        fallback
                      </Badge>
                    </div>
                    <p className="mt-1 text-2xs text-muted-foreground">
                      未命中任何规则时路由到{" "}
                      <span className="font-medium text-foreground">
                        {rule.targetAgentName || "未指定 Agent"}
                      </span>
                      。可编辑目标，但不可删除。
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onRuleDialog(rule)}
                  >
                    编辑
                  </Button>
                </div>
              ))}
          </div>
        </section>

        <Alert className="border-primary/30 bg-primary-soft py-3 text-primary-text">
          <ShieldCheck aria-hidden="true" />
          <AlertTitle className="text-xs font-semibold">
            事件转化由目标 Agent 自己决定
          </AlertTitle>
          <AlertDescription className="text-2xs leading-relaxed text-primary-text">
            一条事件可命中多条规则。建
            issue、回复邮件或发桌面通知等动作不在此页配置；当前版本只记录派发意图，不启动
            Agent runtime。
          </AlertDescription>
        </Alert>
      </div>
    </div>
  );
}

function KindSpecificFields({
  draft,
  selectedEvents,
  setConfig,
}: {
  draft: SourceDraft;
  selectedEvents: string;
  setConfig: <K extends keyof SourceConfig>(
    key: K,
    value: SourceConfig[K],
  ) => void;
}) {
  if (draft.kind === "email") {
    return (
      <>
        <FormRow label="IMAP 服务器">
          <Input
            aria-label="IMAP 服务器"
            value={draft.config.imapServer}
            onChange={(event) => setConfig("imapServer", event.target.value)}
            placeholder="imap.gmail.com"
          />
        </FormRow>
        <FormRow label="邮箱地址">
          <Input
            aria-label="邮箱地址"
            value={draft.config.emailAddress}
            onChange={(event) => setConfig("emailAddress", event.target.value)}
            placeholder="tooru@gmail.com"
          />
        </FormRow>
        <FormRow label="应用密码">
          <Input
            aria-label="应用密码"
            type="password"
            value={draft.config.appPassword}
            onChange={(event) => setConfig("appPassword", event.target.value)}
            onFocus={() => {
              if (draft.config.appPassword === maskedSecret) {
                setConfig("appPassword", "");
              }
            }}
            placeholder="留空保持当前密码"
          />
        </FormRow>
        <FormRow label="轮询间隔">
          <Input
            aria-label="轮询间隔"
            value={draft.config.pollingInterval}
            onChange={(event) =>
              setConfig("pollingInterval", event.target.value)
            }
            placeholder="5m"
          />
        </FormRow>
        <FormRow label="高级设置">
          <details className="rounded-md border border-border bg-secondary/40 px-3 py-2">
            <summary className="cursor-pointer text-xs font-medium text-foreground">
              端口、邮箱目录和 TLS
            </summary>
            <div className="mt-3 grid min-w-0 grid-cols-1 gap-3 md:grid-cols-[120px_minmax(0,1fr)]">
              <Input
                aria-label="IMAP 端口"
                type="number"
                min={1}
                max={65535}
                value={draft.config.imapPort}
                onChange={(event) =>
                  setConfig("imapPort", Number(event.target.value) || 0)
                }
                placeholder="993"
              />
              <Input
                aria-label="邮箱目录"
                value={draft.config.imapMailbox}
                onChange={(event) =>
                  setConfig("imapMailbox", event.target.value)
                }
                placeholder="INBOX"
              />
              <div className="flex items-center gap-3 md:col-span-2">
                <Switch
                  aria-label="使用 IMAP TLS"
                  checked={draft.config.useTls}
                  onCheckedChange={(checked) => setConfig("useTls", checked)}
                />
                <span className="text-xs text-foreground">
                  {draft.config.useTls ? "IMAPS / 993" : "明文 IMAP / 143"}
                </span>
              </div>
            </div>
          </details>
        </FormRow>
      </>
    );
  }

  if (draft.kind === "slack") {
    return (
      <>
        <FormRow label="Bot Token">
          <Input
            aria-label="Slack Bot Token"
            type="password"
            value={draft.config.botToken}
            onChange={(event) => setConfig("botToken", event.target.value)}
            placeholder="xoxb-..."
          />
        </FormRow>
        <FormRow label="监听频道">
          <Input
            aria-label="Slack 监听频道"
            value={draft.config.channel}
            onChange={(event) => setConfig("channel", event.target.value)}
            placeholder="#pm-bots"
          />
        </FormRow>
      </>
    );
  }

  if (draft.kind === "schedule") {
    return (
      <>
        <FormRow label="cron 表达式">
          <Input
            aria-label="cron 表达式"
            value={draft.config.cronExpr}
            onChange={(event) => setConfig("cronExpr", event.target.value)}
            placeholder="0 9 * * 1-5"
          />
        </FormRow>
        <FormRow label="时区">
          <Input
            aria-label="时区"
            value={draft.config.timezone}
            onChange={(event) => setConfig("timezone", event.target.value)}
            placeholder="Asia/Shanghai"
          />
        </FormRow>
      </>
    );
  }

  if (draft.kind === "system") {
    return (
      <FormRow label="系统权限">
        <Input
          aria-label="系统权限"
          value={draft.config.systemPermission}
          onChange={(event) =>
            setConfig("systemPermission", event.target.value)
          }
          placeholder="notification-center"
        />
      </FormRow>
    );
  }

  return (
    <>
      <FormRow
        label="Webhook URL"
        description="桌面本地模式下可以复制给外部系统；公网代理后续单独配置。"
      >
        <div className="flex min-w-0 gap-2">
          <Input
            aria-label="Webhook URL"
            value={draft.config.webhookUrl}
            onChange={(event) => setConfig("webhookUrl", event.target.value)}
            placeholder="https://agentre.local/hooks/abc"
          />
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="复制 Webhook URL"
            onClick={() =>
              void copyTextWithToast(draft.config.webhookUrl, {
                errorTitle: "复制 Webhook URL 失败",
                successTitle: "已复制 Webhook URL",
              })
            }
          >
            <Copy data-icon="only" aria-hidden="true" />
          </Button>
        </div>
      </FormRow>
      <FormRow label="Secret">
        <Input
          aria-label="Webhook Secret"
          type="password"
          value={draft.config.secret}
          onChange={(event) => setConfig("secret", event.target.value)}
          placeholder="••••••••"
        />
      </FormRow>
      <FormRow label="签名验证">
        <div className="flex items-center gap-3 rounded-md border border-border bg-secondary/40 px-3 py-2">
          <Switch
            aria-label="启用签名验证"
            checked={draft.config.verifySignature}
            onCheckedChange={(checked) => setConfig("verifySignature", checked)}
          />
          <span className="text-xs text-foreground">HMAC-SHA256</span>
        </div>
      </FormRow>
      <FormRow label="监听事件">
        <Input
          aria-label="监听事件"
          value={selectedEvents}
          onChange={(event) =>
            setConfig(
              "events",
              event.target.value
                .split(",")
                .map((item) => item.trim())
                .filter(Boolean),
            )
          }
          placeholder="pull_request, issues, push, release"
        />
      </FormRow>
    </>
  );
}

function RuleRow({
  agents,
  onDelete,
  onEdit,
  onUpdate,
  rule,
}: {
  agents: AgentOption[];
  onDelete: () => void;
  onEdit: () => void;
  onUpdate: (patch: Partial<RuleDraft>) => void;
  rule: HookRuleItem;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-background px-3 py-3 md:flex-row md:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <Switch
          aria-label={`启用规则 ${rule.name}`}
          checked={rule.enabled}
          onCheckedChange={(checked) => onUpdate({ enabled: checked })}
          size="sm"
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span
              data-selectable-text="true"
              className="truncate text-xs font-semibold"
            >
              {rule.name}
            </span>
            {rule.enabled ? (
              <Badge variant="secondary" className="font-mono text-2xs">
                enabled
              </Badge>
            ) : (
              <Badge variant="outline" className="font-mono text-2xs">
                paused
              </Badge>
            )}
          </div>
          <div
            data-selectable-text="true"
            className="mt-1 truncate font-mono text-2xs text-muted-foreground"
          >
            {rule.conditionExpr || "always"}
          </div>
        </div>
      </div>
      <div className="flex min-w-0 items-center gap-2 md:w-[260px]">
        <Select
          value={String(rule.targetAgentId || 0)}
          onValueChange={(value) => onUpdate({ targetAgentId: Number(value) })}
        >
          <SelectTrigger aria-label={`规则 ${rule.name} 目标 Agent`}>
            <SelectValue placeholder="目标 Agent" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="0">暂不派发</SelectItem>
              {agents.map((agent) => (
                <SelectItem key={agent.id} value={String(agent.id)}>
                  {agent.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" onClick={onEdit}>
          编辑
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label={`删除规则 ${rule.name}`}
          onClick={onDelete}
        >
          <Trash2 data-icon="only" aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
}

function EventLogPanel({
  agents,
  events,
  selectedEvent,
  source,
  statusFilter,
  query,
  busy,
  onQueryChange,
  onRedeliver,
  onSelectEvent,
  onStatusFilterChange,
}: {
  agents: AgentOption[];
  events: HookEventItem[];
  selectedEvent: HookEventItem | null;
  source: HookSourceItem;
  statusFilter: StatusFilter;
  query: string;
  busy: boolean;
  onQueryChange: (query: string) => void;
  onRedeliver: (event: HookEventItem, agentId: number) => void;
  onSelectEvent: (eventId: number) => void;
  onStatusFilterChange: (status: StatusFilter) => void;
}) {
  const sourceEvents = events.filter((event) => event.sourceId === source.id);
  const counts = {
    all: sourceEvents.length,
    dispatched: sourceEvents.filter(
      (event) => event.eventStatus === "dispatched",
    ).length,
    unmatched: sourceEvents.filter((event) => event.eventStatus === "unmatched")
      .length,
    failed: sourceEvents.filter((event) => event.eventStatus === "failed")
      .length,
  };
  const visibleEvents = sourceEvents.filter(
    (event) =>
      eventMatchesQuery(event, query) &&
      (statusFilter === "all" || event.eventStatus === statusFilter),
  );
  const active =
    selectedEvent && selectedEvent.sourceId === source.id
      ? selectedEvent
      : (visibleEvents[0] ?? null);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-6 py-3">
        <div className="flex h-9 min-w-[240px] flex-1 items-center gap-2 rounded-md border border-input bg-input-bg px-3">
          <Search className="text-muted-foreground" aria-hidden="true" />
          <input
            aria-label="搜索事件"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder="搜索事件标题 / 发件人 / payload…"
            className="min-w-0 flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground"
          />
        </div>
        <div className="flex flex-wrap items-center gap-1">
          {(["all", "dispatched", "unmatched", "failed"] as StatusFilter[]).map(
            (status) => (
              <button
                key={status}
                type="button"
                aria-current={statusFilter === status ? "true" : undefined}
                onClick={() => onStatusFilterChange(status)}
                className={cn(
                  "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-2xs font-medium",
                  statusFilter === status
                    ? "border-primary bg-primary-soft text-primary-text"
                    : "border-border bg-background text-foreground hover:bg-accent",
                )}
              >
                {status === "all" ? "全部" : eventStatusMeta[status].label}
                <span className="font-mono text-2xs">{counts[status]}</span>
              </button>
            ),
          )}
        </div>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[420px_minmax(0,1fr)]">
        <div
          role="list"
          aria-label="事件日志列表"
          className="min-h-0 overflow-y-auto border-b border-border lg:border-b-0 lg:border-r"
        >
          {visibleEvents.map((event) => (
            <button
              key={event.id}
              type="button"
              role="listitem"
              aria-current={active?.id === event.id ? "true" : undefined}
              onClick={() => onSelectEvent(event.id)}
              className={cn(
                "flex w-full min-w-0 gap-3 border-b border-border px-4 py-3 text-left hover:bg-accent",
                active?.id === event.id &&
                  "bg-primary-soft hover:bg-primary-soft",
              )}
            >
              <span
                className={cn(
                  "mt-1 size-2 shrink-0 rounded-full",
                  eventStatusMeta[event.eventStatus].dot,
                )}
              />
              <span className="min-w-0 flex-1">
                <span
                  data-selectable-text="true"
                  className="block truncate text-sm font-semibold"
                >
                  {event.title}
                </span>
                <span className="mt-1 block truncate font-mono text-2xs text-muted-foreground">
                  {event.sourceRef || source.name} · {event.eventType}
                </span>
                <span className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5 text-2xs text-muted-foreground">
                  <span>
                    {event.matchedRuleNames.join(", ") || "兜底 / 未匹配"}
                  </span>
                  {event.targetAgentNames.length > 0 ? (
                    <>
                      <ChevronRight aria-hidden="true" />
                      <span>{event.targetAgentNames.join(", ")}</span>
                    </>
                  ) : null}
                </span>
              </span>
              <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                {formatRelativeTime(event.receivedAt)}
              </span>
            </button>
          ))}
          {visibleEvents.length === 0 ? (
            <div className="flex min-h-[240px] flex-col items-center justify-center gap-2 px-6 text-center">
              <Inbox className="text-muted-foreground" aria-hidden="true" />
              <div className="text-sm font-medium">暂无事件</div>
              <p className="max-w-xs text-xs text-muted-foreground">
                试试测试连接，或调整搜索和状态筛选。
              </p>
            </div>
          ) : null}
        </div>

        <EventDetail
          key={active?.id ?? "empty"}
          agents={agents}
          busy={busy}
          event={active}
          onRedeliver={onRedeliver}
        />
      </div>
    </div>
  );
}

function EventDetail({
  agents,
  busy,
  event,
  onRedeliver,
}: {
  agents: AgentOption[];
  busy: boolean;
  event: HookEventItem | null;
  onRedeliver: (event: HookEventItem, agentId: number) => void;
}) {
  const [targetAgentId, setTargetAgentId] = React.useState("0");

  if (!event) {
    return (
      <div className="flex min-h-0 items-center justify-center px-6 text-center text-sm text-muted-foreground">
        选择一条事件查看 payload、规则匹配结果和派发记录。
      </div>
    );
  }

  return (
    <div className="min-h-0 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex max-w-3xl flex-col gap-4">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <StatusPill status={event.eventStatus} />
              <span className="font-mono text-2xs text-muted-foreground">
                {formatDateTime(event.receivedAt)}
              </span>
            </div>
            <h2 data-selectable-text="true" className="text-base font-semibold">
              {event.title}
            </h2>
            <div className="mt-1 flex flex-wrap items-center gap-2 font-mono text-2xs text-muted-foreground">
              <span>{event.sourceRef || event.sourceName}</span>
              <span>·</span>
              <span>{event.eventType}</span>
              {event.sender ? (
                <>
                  <span>·</span>
                  <span>作者：{event.sender}</span>
                </>
              ) : null}
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              void copyTextWithToast(event.payloadJson, {
                errorTitle: "复制 payload 失败",
                successTitle: "已复制 payload",
              })
            }
          >
            <Copy data-icon="inline-start" aria-hidden="true" />
            复制 payload
          </Button>
        </div>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Route className="text-primary-text" aria-hidden="true" />
            <h3 className="text-sm font-semibold">规则匹配结果</h3>
          </div>
          <div className="flex flex-col gap-2 p-3">
            {event.matchedRules.map((match) => (
              <div
                key={`${match.ruleId}-${match.ruleName}`}
                className={cn(
                  "flex min-w-0 items-start gap-3 rounded-md border px-3 py-2",
                  match.matched
                    ? "border-status-running/30 bg-status-running-bg"
                    : "border-border bg-background",
                )}
              >
                {match.matched ? (
                  <CheckCircle2
                    className="mt-0.5 shrink-0 text-status-running"
                    aria-hidden="true"
                  />
                ) : (
                  <XCircle
                    className="mt-0.5 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                )}
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-semibold">{match.ruleName}</div>
                  <div className="mt-1 font-mono text-2xs text-muted-foreground">
                    {match.reason || "—"}
                  </div>
                </div>
                {match.agentName ? (
                  <Badge variant="secondary" className="shrink-0">
                    {match.agentName}
                  </Badge>
                ) : null}
              </div>
            ))}
            {event.matchedRules.length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                没有规则匹配记录。
              </div>
            ) : null}
          </div>
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Send className="text-primary-text" aria-hidden="true" />
            <h3 className="text-sm font-semibold">派发结果</h3>
          </div>
          <div className="flex flex-col gap-2 p-3">
            {event.dispatches.map((dispatch, index) => (
              <div
                key={`${dispatch.agentId}-${dispatch.sessionId}-${index}`}
                className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border border-border bg-background px-3 py-2"
              >
                <div className="min-w-0">
                  <div className="text-xs font-semibold">
                    {dispatch.agentName || `Agent #${dispatch.agentId}`}
                  </div>
                  <div className="mt-1 font-mono text-2xs text-muted-foreground">
                    {dispatch.sessionId || "pending session"} ·{" "}
                    {dispatch.message || dispatch.status}
                  </div>
                </div>
                <Badge variant="secondary" className="font-mono">
                  {dispatch.status}
                </Badge>
              </div>
            ))}
            {event.dispatches.length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                尚未派发到 Agent。
              </div>
            ) : null}
            <div className="mt-2 flex flex-col gap-2 rounded-md bg-secondary/40 p-3 sm:flex-row sm:items-center">
              <Select value={targetAgentId} onValueChange={setTargetAgentId}>
                <SelectTrigger aria-label="重新派发目标 Agent">
                  <SelectValue placeholder="选择目标 Agent" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="0">默认 Agent</SelectItem>
                    {agents.map((agent) => (
                      <SelectItem key={agent.id} value={String(agent.id)}>
                        {agent.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                className="shrink-0"
                variant="outline"
                size="sm"
                onClick={() => onRedeliver(event, Number(targetAgentId))}
                disabled={busy}
              >
                {busy ? (
                  <Loader2
                    data-icon="inline-start"
                    className="animate-spin"
                    aria-hidden="true"
                  />
                ) : (
                  <RefreshCw data-icon="inline-start" aria-hidden="true" />
                )}
                重新派发
              </Button>
            </div>
          </div>
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <FileJson className="text-primary-text" aria-hidden="true" />
            <h3 className="text-sm font-semibold">原始 JSON payload</h3>
          </div>
          <pre
            data-selectable-text="true"
            className="max-h-[340px] overflow-auto p-4 font-mono text-2xs leading-relaxed text-foreground"
          >
            {prettyJSON(event.payloadJson)}
          </pre>
        </section>
      </div>
    </div>
  );
}

function SourceDialog({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (draft: SourceDraft) => void;
}) {
  const [draft, setDraft] = React.useState<SourceDraft>(() => sourceToDraft());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建信号源</DialogTitle>
          <DialogDescription>
            先创建基础入口，再在详情页补充连接参数和路由规则。
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          <form
            aria-label="新建信号源"
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              onSubmit(draft);
            }}
          >
            <div className="flex flex-col gap-2">
              <TextLabel htmlFor="hook-source-name">名称</TextLabel>
              <Input
                id="hook-source-name"
                value={draft.name}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                placeholder="agentre-bot"
              />
            </div>
            <div className="flex flex-col gap-2">
              <TextLabel>类型</TextLabel>
              <Select
                value={draft.kind}
                onValueChange={(value) =>
                  setDraft((current) => ({
                    ...current,
                    kind: value as SourceKind,
                  }))
                }
              >
                <SelectTrigger aria-label="新建信号源类型">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {sourceKindOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <TextLabel htmlFor="hook-source-identifier">标识</TextLabel>
              <Input
                id="hook-source-identifier"
                value={draft.identifier}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    identifier: event.target.value,
                  }))
                }
                placeholder="agentre-frame"
              />
            </div>
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => onSubmit(draft)}>创建</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RuleDialog({
  agents,
  open,
  onOpenChange,
  onSubmit,
  rule,
}: {
  agents: AgentOption[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (draft: RuleDraft) => void;
  rule: HookRuleItem | null;
}) {
  const [draft, setDraft] = React.useState<RuleDraft>(() => ruleToDraft(rule));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{rule ? "编辑路由规则" : "新建路由规则"}</DialogTitle>
          <DialogDescription>
            条件支持简单表达式，例如 subject contains "X" 或 event_type contains
            "release"。
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          <form
            aria-label={rule ? "编辑路由规则" : "新建路由规则"}
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              onSubmit(draft);
            }}
          >
            <div className="flex flex-col gap-2">
              <TextLabel htmlFor="hook-rule-name">名称</TextLabel>
              <Input
                id="hook-rule-name"
                value={draft.name}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                placeholder="PR opened / review"
              />
            </div>
            <div className="flex flex-col gap-2">
              <TextLabel htmlFor="hook-rule-condition">条件表达式</TextLabel>
              <Textarea
                id="hook-rule-condition"
                value={draft.conditionExpr}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    conditionExpr: event.target.value,
                  }))
                }
                placeholder='event_type contains "pr" OR "pull_request"'
                className="min-h-20 font-mono text-xs"
              />
            </div>
            <div className="flex flex-col gap-2">
              <TextLabel>目标 Agent</TextLabel>
              <Select
                value={String(draft.targetAgentId || 0)}
                onValueChange={(value) =>
                  setDraft((current) => ({
                    ...current,
                    targetAgentId: Number(value),
                  }))
                }
              >
                <SelectTrigger aria-label="路由目标 Agent">
                  <SelectValue placeholder="选择 Agent" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="0">暂不派发</SelectItem>
                    {agents.map((agent) => (
                      <SelectItem key={agent.id} value={String(agent.id)}>
                        {agent.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center gap-3 rounded-md border border-border bg-secondary/40 px-3 py-2">
              <Switch
                aria-label="启用规则"
                checked={draft.enabled}
                disabled={rule?.isFallback}
                onCheckedChange={(checked) =>
                  setDraft((current) => ({ ...current, enabled: checked }))
                }
              />
              <span className="text-xs">
                {rule?.isFallback ? "兜底规则始终启用" : "启用此规则"}
              </span>
            </div>
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => onSubmit(draft)}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function HooksPage() {
  const [data, setData] = React.useState<HooksData>({
    sources: [],
    rules: [],
    events: [],
    agents: [],
  });
  const [loading, setLoading] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [flash, setFlash] = React.useState<FlashState>(null);
  const [activeTab, setActiveTab] = React.useState<HookTab>("config");
  const [sourceQuery, setSourceQuery] = React.useState("");
  const [eventQuery, setEventQuery] = React.useState("");
  const [statusFilter, setStatusFilter] = React.useState<StatusFilter>("all");
  const [selectedSourceId, setSelectedSourceId] = React.useState<number | null>(
    null,
  );
  const [selectedEventId, setSelectedEventId] = React.useState<number | null>(
    null,
  );
  const [sourceDialogOpen, setSourceDialogOpen] = React.useState(false);
  const [ruleDialogOpen, setRuleDialogOpen] = React.useState(false);
  const [editingRule, setEditingRule] = React.useState<HookRuleItem | null>(
    null,
  );

  const selectedSource =
    data.sources.find((source) => source.id === selectedSourceId) ??
    data.sources[0] ??
    null;
  const sourceRules = selectedSource
    ? data.rules
        .filter((rule) => rule.sourceId === selectedSource.id)
        .sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
    : [];
  const sourceEvents = selectedSource
    ? data.events.filter((event) => event.sourceId === selectedSource.id)
    : [];
  const selectedEvent =
    data.events.find((event) => event.id === selectedEventId) ??
    sourceEvents[0] ??
    null;

  const reload = React.useCallback(async () => {
    try {
      setLoading(true);
      const LoadHooks = getBridgeMethod("LoadHooks");
      const resp = await LoadHooks({ limit: 100 });
      const nextData = normalizeHooksData(resp);
      setData(nextData);
      setSelectedSourceId((current) => {
        if (
          current &&
          nextData.sources.some((source) => source.id === current)
        ) {
          return current;
        }
        return nextData.sources[0]?.id ?? null;
      });
      setSelectedEventId((current) => {
        if (current && nextData.events.some((event) => event.id === current)) {
          return current;
        }
        return nextData.events[0]?.id ?? null;
      });
    } catch (err) {
      setFlash({
        kind: "err",
        text: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload();
  }, [reload]);

  const runBusy = async (action: () => Promise<void>) => {
    try {
      setBusy(true);
      setFlash(null);
      await action();
    } catch (err) {
      setFlash({
        kind: "err",
        text: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  };

  const handleCreateSource = (draft: SourceDraft) => {
    void runBusy(async () => {
      const CreateHookSource = getBridgeMethod("CreateHookSource");
      const resp = await CreateHookSource({
        kind: draft.kind,
        name: draft.name,
        description: draft.description,
        identifier: draft.identifier,
        config: draft.config,
        enabled: draft.enabled,
      });
      const item = normalizeSource(resp.item);
      setData((current) => ({
        ...current,
        sources: [...current.sources, item],
      }));
      setSelectedSourceId(item.id);
      setSourceDialogOpen(false);
      setFlash({ kind: "ok", text: `已创建信号源 ${item.name}` });
      await reload();
    });
  };

  const handleSaveSource = (draft: SourceDraft) => {
    if (!selectedSource) return;
    void runBusy(async () => {
      const UpdateHookSource = getBridgeMethod("UpdateHookSource");
      const resp = await UpdateHookSource({
        id: selectedSource.id,
        kind: draft.kind,
        name: draft.name,
        description: draft.description,
        identifier: draft.identifier,
        config: draft.config,
        enabled: draft.enabled,
      });
      const item = normalizeSource(resp.item);
      setData((current) => ({
        ...current,
        sources: replaceById(current.sources, item),
      }));
      setFlash({ kind: "ok", text: "连接配置已保存" });
    });
  };

  const handleToggleSourceEnabled = () => {
    if (!selectedSource) return;
    const nextEnabled = !selectedSource.enabled;
    void runBusy(async () => {
      const UpdateHookSource = getBridgeMethod("UpdateHookSource");
      const resp = await UpdateHookSource({
        id: selectedSource.id,
        kind: selectedSource.kind,
        name: selectedSource.name,
        description: selectedSource.description,
        identifier: selectedSource.identifier,
        config: selectedSource.config,
        enabled: nextEnabled,
      });
      const item = normalizeSource(resp.item);
      setData((current) => ({
        ...current,
        sources: replaceById(current.sources, item),
      }));
      setFlash({
        kind: "ok",
        text: nextEnabled ? "信号源已启用" : "信号源已停用",
      });
    });
  };

  const handleDeleteSource = () => {
    if (!selectedSource) return;
    void runBusy(async () => {
      const DeleteHookSource = getBridgeMethod("DeleteHookSource");
      await DeleteHookSource({ id: selectedSource.id });
      setFlash({ kind: "ok", text: `已删除信号源 ${selectedSource.name}` });
      await reload();
    });
  };

  const openCreateRule = () => {
    setEditingRule(null);
    setRuleDialogOpen(true);
  };

  const openEditRule = (rule: HookRuleItem) => {
    setEditingRule(rule);
    setRuleDialogOpen(true);
  };

  const handleSubmitRule = (draft: RuleDraft) => {
    if (!selectedSource) return;
    void runBusy(async () => {
      if (editingRule) {
        const UpdateHookRule = getBridgeMethod("UpdateHookRule");
        const resp = await UpdateHookRule({
          id: editingRule.id,
          name: draft.name,
          conditionExpr: draft.conditionExpr,
          targetAgentId: draft.targetAgentId,
          enabled: draft.enabled,
        });
        setData((current) => ({
          ...current,
          rules: replaceById(current.rules, resp.item),
        }));
      } else {
        const CreateHookRule = getBridgeMethod("CreateHookRule");
        const resp = await CreateHookRule({
          sourceId: selectedSource.id,
          name: draft.name,
          conditionExpr: draft.conditionExpr,
          targetAgentId: draft.targetAgentId,
          enabled: draft.enabled,
        });
        setData((current) => ({
          ...current,
          rules: [...current.rules, resp.item],
        }));
      }
      setRuleDialogOpen(false);
      setEditingRule(null);
      setFlash({ kind: "ok", text: "路由规则已保存" });
    });
  };

  const handleUpdateRule = (rule: HookRuleItem, patch: Partial<RuleDraft>) => {
    void runBusy(async () => {
      const UpdateHookRule = getBridgeMethod("UpdateHookRule");
      const resp = await UpdateHookRule({
        id: rule.id,
        name: patch.name ?? rule.name,
        conditionExpr: patch.conditionExpr ?? rule.conditionExpr,
        targetAgentId: patch.targetAgentId ?? rule.targetAgentId,
        enabled: patch.enabled ?? rule.enabled,
      });
      setData((current) => ({
        ...current,
        rules: replaceById(current.rules, resp.item),
      }));
    });
  };

  const handleDeleteRule = (rule: HookRuleItem) => {
    void runBusy(async () => {
      const DeleteHookRule = getBridgeMethod("DeleteHookRule");
      await DeleteHookRule({ id: rule.id });
      setData((current) => ({
        ...current,
        rules: current.rules.filter((item) => item.id !== rule.id),
      }));
      setFlash({ kind: "ok", text: `已删除规则 ${rule.name}` });
    });
  };

  const handleTestSource = () => {
    if (!selectedSource) return;
    void runBusy(async () => {
      const TestHookSource = getBridgeMethod("TestHookSource");
      const resp = await TestHookSource({ id: selectedSource.id });
      const item = normalizeSource(resp.item);
      const event = normalizeEvent(resp.event);
      setData((current) => ({
        ...current,
        sources: replaceById(current.sources, item),
        events: [
          event,
          ...current.events.filter(
            (currentEvent) => currentEvent.id !== event.id,
          ),
        ],
      }));
      setSelectedEventId(event.id);
      setActiveTab("log");
      setFlash({ kind: "ok", text: "测试事件已写入事件日志" });
    });
  };

  const handleSyncEmailSource = () => {
    if (!selectedSource || selectedSource.kind !== "email") return;
    void runBusy(async () => {
      const SyncHookEmailSource = getBridgeMethod("SyncHookEmailSource");
      const resp = await SyncHookEmailSource({
        id: selectedSource.id,
        limit: 20,
      });
      const item = normalizeSource(resp.item);
      const events = (resp.events ?? []).map(normalizeEvent);
      setData((current) => ({
        ...current,
        sources: replaceById(current.sources, item),
        events: prependUniqueEvents(current.events, events),
      }));
      if (events[0]) {
        setSelectedEventId(events[0].id);
      }
      setActiveTab("log");
      setFlash({
        kind: "ok",
        text: `邮箱同步完成：新增 ${resp.created ?? events.length} 封，跳过 ${
          resp.skipped ?? 0
        } 封`,
      });
    });
  };

  const handleRedeliver = (event: HookEventItem, targetAgentId: number) => {
    void runBusy(async () => {
      const RedeliverHookEvent = getBridgeMethod("RedeliverHookEvent");
      const resp = await RedeliverHookEvent({
        id: event.id,
        targetAgentId,
      });
      const item = normalizeEvent(resp.item);
      setData((current) => ({
        ...current,
        events: replaceById(current.events, item),
      }));
      setSelectedEventId(item.id);
      setFlash({ kind: "ok", text: "已记录重新派发请求" });
    });
  };

  return (
    <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-background lg:flex-row">
      <SourceList
        activeId={selectedSource?.id ?? null}
        query={sourceQuery}
        sources={data.sources}
        onNew={() => setSourceDialogOpen(true)}
        onQueryChange={setSourceQuery}
        onSelect={(id) => {
          setSelectedSourceId(id);
          setSelectedEventId(null);
        }}
      />

      <section className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <HooksPageHeader
          activeTab={activeTab}
          busy={busy}
          eventCount={sourceEvents.length}
          source={selectedSource}
          onDelete={handleDeleteSource}
          onSyncEmail={handleSyncEmailSource}
          onTabChange={setActiveTab}
          onTest={handleTestSource}
          onToggleEnabled={handleToggleSourceEnabled}
        />

        {flash ? (
          <div className="absolute right-4 top-4 z-20 max-w-md">
            <Alert
              className={cn(
                "shadow-lg",
                flash.kind === "err"
                  ? "border-destructive/40 bg-destructive-soft text-status-error"
                  : "border-status-running/40 bg-status-running-bg text-status-running",
              )}
            >
              {flash.kind === "err" ? (
                <AlertCircle aria-hidden="true" />
              ) : (
                <CheckCircle2 aria-hidden="true" />
              )}
              <AlertTitle className="text-xs font-semibold">
                {flash.kind === "err" ? "操作失败" : "操作完成"}
              </AlertTitle>
              <AlertDescription className="text-2xs">
                {flash.text}
              </AlertDescription>
            </Alert>
          </div>
        ) : null}

        {loading ? (
          <div className="flex min-h-0 flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="animate-spin" aria-hidden="true" />
            正在加载 Hooks…
          </div>
        ) : selectedSource ? (
          activeTab === "config" ? (
            <SourceConfigPanel
              key={selectedSource.id}
              agents={data.agents}
              busy={busy}
              rules={sourceRules}
              source={selectedSource}
              onCreateRule={openCreateRule}
              onDeleteRule={handleDeleteRule}
              onRuleDialog={openEditRule}
              onSaveSource={handleSaveSource}
              onUpdateRule={handleUpdateRule}
            />
          ) : (
            <EventLogPanel
              agents={data.agents}
              busy={busy}
              events={data.events}
              query={eventQuery}
              selectedEvent={selectedEvent}
              source={selectedSource}
              statusFilter={statusFilter}
              onQueryChange={setEventQuery}
              onRedeliver={handleRedeliver}
              onSelectEvent={setSelectedEventId}
              onStatusFilterChange={setStatusFilter}
            />
          )
        ) : (
          <div className="flex min-h-0 flex-1 items-center justify-center px-6 text-center">
            <div className="flex max-w-md flex-col items-center gap-3">
              <Webhook className="text-primary-text" aria-hidden="true" />
              <h2 className="text-base font-semibold">还没有信号源</h2>
              <p className="text-sm text-muted-foreground">
                创建邮箱、Webhook、Slack
                或定时器入口后，就能配置路由规则和查看事件日志。
              </p>
              <Button onClick={() => setSourceDialogOpen(true)}>
                <Plus data-icon="inline-start" aria-hidden="true" />
                新建信号源
              </Button>
            </div>
          </div>
        )}
      </section>

      <SourceDialog
        key={sourceDialogOpen ? "source-open" : "source-closed"}
        open={sourceDialogOpen}
        onOpenChange={setSourceDialogOpen}
        onSubmit={handleCreateSource}
      />
      <RuleDialog
        key={`${ruleDialogOpen ? "rule-open" : "rule-closed"}-${editingRule?.id ?? "new"}`}
        agents={data.agents}
        open={ruleDialogOpen}
        rule={editingRule}
        onOpenChange={(open) => {
          setRuleDialogOpen(open);
          if (!open) {
            setEditingRule(null);
          }
        }}
        onSubmit={handleSubmitRule}
      />
    </main>
  );
}
