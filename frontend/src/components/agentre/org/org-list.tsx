import * as React from "react";
import { ArrowDown, ArrowUp, Puzzle, Search, Wrench, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

import type { agent_backend_svc } from "../../../../wailsjs/go/models";
import { AgentAvatar } from "../primitives";
import { buildReportToMap } from "./reporting";
import {
  safeAgentColor,
  type OrgAgent,
  type OrgDepartment,
  type OrgSelection,
} from "./types";

type BackendItem = agent_backend_svc.BackendItem;

export type OrgListProps = {
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: BackendItem[];
  selected: OrgSelection;
  onSelect: (sel: OrgSelection) => void;
};

type SortKey = "hierarchy" | "name";
type SortDir = "asc" | "desc";

type RowModel = {
  agent: OrgAgent;
  depth: number;
};

// 用 effectiveParent 串成树后做先序遍历，得到含 depth 的行序列。
function buildHierarchyRows(
  agents: OrgAgent[],
  effectiveParent: Map<number, number>,
): RowModel[] {
  const childrenOf = new Map<number, OrgAgent[]>();
  for (const a of agents) {
    const pid = effectiveParent.get(a.id) ?? 0;
    if (!childrenOf.has(pid)) childrenOf.set(pid, []);
    childrenOf.get(pid)!.push(a);
  }
  for (const arr of childrenOf.values()) {
    arr.sort((x, y) => (x.sortOrder ?? 0) - (y.sortOrder ?? 0));
  }

  const out: RowModel[] = [];
  const visited = new Set<number>();
  const visit = (agent: OrgAgent, depth: number) => {
    if (visited.has(agent.id)) return;
    visited.add(agent.id);
    out.push({ agent, depth });
    const kids = childrenOf.get(agent.id);
    if (kids) for (const k of kids) visit(k, depth + 1);
  };

  // 根：effectiveParent === 0 的 Agent（通常是 CEO，没有 CEO 时是各孤立顶点）。
  const roots = (childrenOf.get(0) ?? []).slice();
  for (const r of roots) visit(r, 0);
  // 兜底：环里的、或挂在不存在父级上的，单独放出来。
  for (const a of agents) if (!visited.has(a.id)) visit(a, 0);
  return out;
}

function flatRows(agents: OrgAgent[]): RowModel[] {
  return agents.map((a) => ({ agent: a, depth: 0 }));
}

// 把命中过滤的 Agent 集合扩展为：命中项 + 全部祖先（保证层级展示完整）。
function expandLineage(
  hits: Set<number>,
  agents: OrgAgent[],
  effectiveParent: Map<number, number>,
): Set<number> {
  const byId = new Map(agents.map((a) => [a.id, a]));
  const out = new Set(hits);
  for (const id of hits) {
    let cur = byId.get(id);
    const guard = new Set<number>();
    while (cur) {
      const pid = effectiveParent.get(cur.id) ?? 0;
      if (pid === 0 || guard.has(pid)) break;
      guard.add(pid);
      out.add(pid);
      cur = byId.get(pid);
    }
  }
  return out;
}

export function OrgList(props: OrgListProps) {
  const { t } = useTranslation();
  const { departments, agents, backends, selected, onSelect } = props;

  const [search, setSearch] = React.useState("");
  const [backendFilter, setBackendFilter] = React.useState<number>(0);
  const [reportsToFilter, setReportsToFilter] = React.useState<number>(0);
  const [sortKey, setSortKey] = React.useState<SortKey>("hierarchy");
  const [sortDir, setSortDir] = React.useState<SortDir>("asc");

  const agentById = React.useMemo(
    () => new Map(agents.map((a) => [a.id, a])),
    [agents],
  );

  // 有效汇报关系：单一事实源在 ./reporting.ts —— 显式 parent_agent_id ▸
  // 沿部门链找首个非自身 leader ▸ CEO 兜底。
  const effectiveParent = React.useMemo(
    () => buildReportToMap(agents, departments),
    [agents, departments],
  );

  // 候选 parent agent 列表（用于「汇报给」过滤）：以有效父级出现过的 Agent 为候选。
  const parentOptions = React.useMemo(() => {
    const ids = new Set<number>();
    for (const pid of effectiveParent.values()) if (pid !== 0) ids.add(pid);
    return [...ids]
      .map((id) => agentById.get(id))
      .filter((x): x is OrgAgent => Boolean(x))
      .sort((a, b) => a.name.localeCompare(b.name, "zh-Hans"));
  }, [effectiveParent, agentById]);

  const rows = React.useMemo<RowModel[]>(() => {
    const kw = search.trim().toLowerCase();

    // 先算出每个 Agent 是否被过滤条件命中。
    const hits = new Set<number>();
    for (const a of agents) {
      if (backendFilter !== 0 && (a.agentBackendId ?? 0) !== backendFilter)
        continue;
      if (
        reportsToFilter !== 0 &&
        (effectiveParent.get(a.id) ?? 0) !== reportsToFilter
      )
        continue;
      if (kw) {
        const hay = `${a.name} ${a.description ?? ""}`.toLowerCase();
        if (!hay.includes(kw)) continue;
      }
      hits.add(a.id);
    }

    // hierarchy 模式下补齐祖先链，确保缩进可读。
    const visible =
      sortKey === "hierarchy"
        ? expandLineage(hits, agents, effectiveParent)
        : hits;
    const filtered = agents.filter((a) => visible.has(a.id));

    if (sortKey === "hierarchy") {
      const all = buildHierarchyRows(filtered, effectiveParent);
      return sortDir === "asc" ? all : [...all].reverse();
    }

    const sorted = [...filtered].sort((a, b) =>
      a.name.localeCompare(b.name, "zh-Hans"),
    );
    if (sortDir === "desc") sorted.reverse();
    return flatRows(sorted);
  }, [
    agents,
    effectiveParent,
    search,
    backendFilter,
    reportsToFilter,
    sortKey,
    sortDir,
  ]);

  return (
    <div
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-card"
      data-slot="org-list"
    >
      <FilterBar
        search={search}
        onSearch={setSearch}
        backends={backends}
        backendFilter={backendFilter}
        onBackendFilter={setBackendFilter}
        parentOptions={parentOptions}
        reportsToFilter={reportsToFilter}
        onReportsToFilter={setReportsToFilter}
        sortKey={sortKey}
        sortDir={sortDir}
        onSortKey={setSortKey}
        onSortDir={setSortDir}
      />

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <TableHead
          sortKey={sortKey}
          sortDir={sortDir}
          onToggleNameSort={() => {
            if (sortKey === "name") {
              setSortDir((d) => (d === "asc" ? "desc" : "asc"));
            } else {
              setSortKey("name");
              setSortDir("asc");
            }
          }}
        />

        <div className="flex-1 overflow-y-auto" data-slot="org-list-body">
          {rows.length === 0 ? (
            <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">
              {t("org.list.empty")}
            </div>
          ) : (
            rows.map((row) => {
              const pid = effectiveParent.get(row.agent.id) ?? 0;
              const parentAgent =
                pid !== 0 ? (agentById.get(pid) ?? null) : null;
              return (
                <ListRow
                  key={row.agent.id}
                  row={row}
                  isSelected={
                    selected?.kind === "agent" && selected.id === row.agent.id
                  }
                  parentAgent={parentAgent}
                  showIndentConnector={sortKey === "hierarchy" && row.depth > 0}
                  onClick={() => onSelect({ kind: "agent", id: row.agent.id })}
                />
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}

type FilterBarProps = {
  search: string;
  onSearch: (v: string) => void;
  backends: BackendItem[];
  backendFilter: number;
  onBackendFilter: (id: number) => void;
  parentOptions: OrgAgent[];
  reportsToFilter: number;
  onReportsToFilter: (id: number) => void;
  sortKey: SortKey;
  sortDir: SortDir;
  onSortKey: (k: SortKey) => void;
  onSortDir: (d: SortDir) => void;
};

function FilterBar(props: FilterBarProps) {
  const { t } = useTranslation();
  const backendLabel =
    props.backendFilter === 0
      ? t("common.all")
      : (props.backends.find((b) => b.id === props.backendFilter)?.name ??
        t("common.all"));
  const reportsLabel =
    props.reportsToFilter === 0
      ? t("common.all")
      : (props.parentOptions.find((a) => a.id === props.reportsToFilter)
          ?.name ?? t("common.all"));

  return (
    <div
      className="flex shrink-0 items-center gap-2 border-b bg-background px-5 py-2.5"
      data-slot="org-list-filter"
    >
      <div className="relative">
        <Search
          aria-hidden="true"
          className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          aria-label={t("org.list.search.aria")}
          placeholder={t("org.list.search.placeholder")}
          value={props.search}
          onChange={(e) => props.onSearch(e.target.value)}
          className="h-8 w-60 pl-7 text-xs"
        />
      </div>

      <Select
        value={String(props.backendFilter)}
        onValueChange={(v) => props.onBackendFilter(Number(v))}
      >
        <SelectTrigger
          aria-label={t("org.list.filters.backendAria")}
          size="sm"
          className={cn(
            "h-8 text-xs",
            props.backendFilter !== 0 &&
              "border-primary bg-primary-soft text-primary-text",
          )}
        >
          <span>
            <span className="text-muted-foreground">
              {t("org.list.filters.backendPrefix")}
            </span>
            <SelectValue placeholder={backendLabel}>{backendLabel}</SelectValue>
          </span>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="0">{t("common.all")}</SelectItem>
          {props.backends.map((b) => (
            <SelectItem key={b.id} value={String(b.id)}>
              {b.name}
              {b.type ? ` · ${b.type}` : ""}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={String(props.reportsToFilter)}
        onValueChange={(v) => props.onReportsToFilter(Number(v))}
      >
        <SelectTrigger
          aria-label={t("org.list.filters.reportsToAria")}
          size="sm"
          className={cn(
            "h-8 text-xs",
            props.reportsToFilter !== 0 &&
              "border-primary bg-primary-soft text-primary-text",
          )}
        >
          <span>
            <span className="text-muted-foreground">
              {t("org.list.filters.reportsToPrefix")}
            </span>
            <SelectValue placeholder={reportsLabel}>{reportsLabel}</SelectValue>
          </span>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="0">{t("common.all")}</SelectItem>
          {props.parentOptions.map((a) => (
            <SelectItem key={a.id} value={String(a.id)}>
              {a.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {props.reportsToFilter !== 0 && (
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("org.list.filters.clearReportsTo")}
          className="size-7 text-primary-text"
          onClick={() => props.onReportsToFilter(0)}
        >
          <X className="size-3.5" />
        </Button>
      )}

      <div className="flex-1" />

      <div className="flex items-center gap-1.5 text-2xs text-muted-foreground">
        <span>{t("org.list.sort.label")}</span>
        <Select
          value={props.sortKey}
          onValueChange={(v) => props.onSortKey(v as SortKey)}
        >
          <SelectTrigger
            aria-label={t("org.list.sort.aria")}
            size="sm"
            className="h-8 text-xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="hierarchy">
              {t("org.list.sort.hierarchy")}
            </SelectItem>
            <SelectItem value="name">{t("org.department.name")}</SelectItem>
            <SelectItem value="status">{t("org.list.sort.status")}</SelectItem>
          </SelectContent>
        </Select>
        <Button
          variant="ghost"
          size="icon"
          aria-label={
            props.sortDir === "asc"
              ? t("org.list.sort.asc")
              : t("org.list.sort.desc")
          }
          className="size-7"
          onClick={() =>
            props.onSortDir(props.sortDir === "asc" ? "desc" : "asc")
          }
        >
          {props.sortDir === "asc" ? (
            <ArrowDown className="size-3.5" />
          ) : (
            <ArrowUp className="size-3.5" />
          )}
        </Button>
      </div>
    </div>
  );
}

type TableHeadProps = {
  sortKey: SortKey;
  sortDir: SortDir;
  onToggleNameSort: () => void;
};

function TableHead(props: TableHeadProps) {
  const { t } = useTranslation();
  return (
    <div
      className="sticky top-0 z-10 flex h-9 shrink-0 items-center gap-3 border-b bg-secondary px-5 font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground"
      data-slot="org-list-head"
    >
      <button
        type="button"
        aria-label={t("org.list.table.sortByAgent")}
        onClick={props.onToggleNameSort}
        className="flex flex-1 items-center gap-1.5 text-left hover:text-foreground"
      >
        <span>{t("org.list.table.agent")}</span>
        {props.sortKey === "name" &&
          (props.sortDir === "asc" ? (
            <ArrowDown className="size-3" />
          ) : (
            <ArrowUp className="size-3" />
          ))}
      </button>
      <span className="w-[160px] shrink-0">
        {t("org.chart.newAgent.backend")}
      </span>
      <span className="w-[160px] shrink-0">{t("org.agent.reportsTo")}</span>
      <span className="w-[120px] shrink-0">{t("org.agent.skills.title")}</span>
      <span className="w-[100px] shrink-0">{t("org.list.table.active")}</span>
      <span className="w-12 shrink-0" aria-hidden="true" />
    </div>
  );
}

type ListRowProps = {
  row: RowModel;
  isSelected: boolean;
  parentAgent: OrgAgent | null;
  showIndentConnector: boolean;
  onClick: () => void;
};

function ListRow({
  row,
  isSelected,
  parentAgent,
  showIndentConnector,
  onClick,
}: ListRowProps) {
  const { t } = useTranslation();
  const { agent, depth } = row;
  const color = safeAgentColor(agent.avatarColor);

  const enabledSkills = (agent.skills ?? []).filter((s) => s.enabled).length;
  const totalSkills = (agent.skills ?? []).length;
  const disabledSkills = totalSkills - enabledSkills;
  const skillsLabel =
    totalSkills === 0
      ? "—"
      : disabledSkills === 0
        ? t("org.list.skills.enabledOnly", { count: enabledSkills })
        : t("org.agent.skills.summary", {
            disabled: disabledSkills,
            enabled: enabledSkills,
          });

  const backendName = agent.backend?.name ?? "—";
  const BackendIcon = agent.backend?.type === "claude-code" ? Wrench : Puzzle;

  return (
    <button
      type="button"
      role="option"
      aria-selected={isSelected}
      onClick={onClick}
      data-slot="org-list-row"
      data-agent-id={agent.id}
      className={cn(
        "flex w-full items-center gap-3 border-b border-l-[3px] border-border/60 py-2.5 pl-[17px] pr-5 text-left transition-colors",
        "hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
        isSelected
          ? "border-l-primary bg-primary-soft"
          : "border-l-transparent",
      )}
    >
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {depth > 0 && (
          <div
            aria-hidden="true"
            className="shrink-0"
            style={{ width: depth * 24 }}
          />
        )}
        {showIndentConnector && (
          <span
            aria-hidden="true"
            className={cn(
              "shrink-0 font-mono text-xs leading-none",
              isSelected ? "text-primary-text" : "text-muted-foreground",
            )}
          >
            └
          </span>
        )}
        <AgentAvatar
          name={agent.name}
          color={color}
          size="md"
          avatarDataUrl={agent.avatarDataUrl}
          avatarIcon={agent.avatarIcon}
          className="size-9 shrink-0 rounded-lg"
        />
        <div className="flex min-w-0 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span
              className={cn(
                "truncate text-sm font-semibold",
                isSelected && "text-primary-text",
              )}
            >
              {agent.name}
            </span>
          </span>
          {agent.description && (
            <span
              className={cn(
                "truncate text-2xs",
                isSelected ? "text-primary-text" : "text-muted-foreground",
              )}
            >
              {agent.description}
            </span>
          )}
        </div>
      </div>

      <div className="w-[160px] shrink-0">
        <span
          className={cn(
            "inline-flex items-center gap-1.5 rounded border px-2 py-0.5 font-mono text-2xs",
            isSelected
              ? "border-primary bg-card text-primary-text"
              : "border-transparent bg-secondary text-foreground",
          )}
        >
          <BackendIcon
            aria-hidden="true"
            className={cn(
              "size-3",
              isSelected ? "text-primary" : "text-muted-foreground",
            )}
          />
          <span className="truncate">{backendName}</span>
        </span>
      </div>

      <div className="w-[160px] shrink-0">
        {parentAgent ? (
          <span
            className={cn(
              "inline-flex items-center gap-1.5 rounded border px-1.5 py-0.5 font-mono text-2xs",
              isSelected
                ? "border-border bg-card"
                : "border-transparent bg-secondary",
            )}
          >
            <AgentAvatar
              name={parentAgent.name}
              color={safeAgentColor(parentAgent.avatarColor)}
              size="sm"
              avatarDataUrl={parentAgent.avatarDataUrl}
              avatarIcon={parentAgent.avatarIcon}
              className="rounded"
            />
            <span className="truncate">{parentAgent.name}</span>
          </span>
        ) : (
          <span className="font-mono text-xs text-muted-foreground">—</span>
        )}
      </div>

      <div
        className={cn(
          "w-[120px] shrink-0 font-mono text-2xs",
          isSelected ? "font-medium text-primary-text" : "text-foreground",
        )}
      >
        {skillsLabel}
      </div>
    </button>
  );
}
