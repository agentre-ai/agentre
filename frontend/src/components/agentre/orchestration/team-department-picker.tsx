import * as React from "react";
import { Check, CircleDashed, Minus, Users } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

import { iconForKey } from "../icon-registry";
import { firstLetter, tokenToCssColor } from "../session-avatar";
import type { PickerAgent, PickerModel } from "./team-picker-data";

export type TeamDepartmentPickerProps = {
  model: PickerModel;
  value: number[];
  onChange: (ids: number[]) => void;
};

type Scope = "all" | "ungrouped" | number;

// 后端类型 → 展示标签(品牌拉丁串, 不进 i18n)。未知类型原样显示。
const BACKEND_LABEL: Record<string, string> = {
  claudecode: "Claude Code",
  codex: "Codex",
  builtin: "Built-in",
};
function backendLabel(t: string): string {
  return BACKEND_LABEL[t] ?? t;
}

export function TeamDepartmentPicker({ model, value, onChange }: TeamDepartmentPickerProps) {
  const { t } = useTranslation();
  const [scope, setScope] = React.useState<Scope>("all");
  const [query, setQuery] = React.useState("");
  const selected = React.useMemo(() => new Set(value), [value]);

  const q = query.trim().toLowerCase();
  const searching = q.length > 0;

  const scopeAgents: PickerAgent[] =
    scope === "all"
      ? model.all
      : scope === "ungrouped"
        ? model.ungrouped
        : (model.departments.find((d) => d.id === scope)?.agents ?? []);
  const shown = searching
    ? model.all.filter((a) => a.name.toLowerCase().includes(q))
    : scopeAgents;

  const toggle = (id: number) => {
    const next = new Set(value);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next]);
  };

  const shownIds = shown.map((a) => a.id);
  const allShownSelected = shownIds.length > 0 && shownIds.every((id) => selected.has(id));
  const someShownSelected = shownIds.some((id) => selected.has(id));
  const selectAllState: CbState = allShownSelected ? "checked" : someShownSelected ? "partial" : "empty";
  const toggleSelectAll = () => {
    const next = new Set(value);
    if (allShownSelected) shownIds.forEach((id) => next.delete(id));
    else shownIds.forEach((id) => next.add(id));
    onChange([...next]);
  };

  const title = searching
    ? t("orchestration.new.teamSearchResults")
    : scope === "all"
      ? t("orchestration.new.teamAllAgents")
      : scope === "ungrouped"
        ? t("orchestration.new.teamUngrouped")
        : (model.departments.find((d) => d.id === scope)?.name ?? "");

  const selectedAgents = value
    .map((id) => model.all.find((a) => a.id === id))
    .filter((a): a is PickerAgent => Boolean(a));

  return (
    <div className="flex flex-col gap-2 text-xs">
      <div className="flex items-center gap-2">
        <span className="font-medium text-foreground">{t("orchestration.new.team")}</span>
        <span
          data-testid="run-team-count"
          className="ml-auto rounded-full bg-primary-soft px-2 py-0.5 text-primary-text"
        >
          {t("orchestration.new.teamSelected", { count: value.length })}
        </span>
      </div>

      <div className="overflow-hidden rounded-md border border-border bg-card">
        <div className="border-b border-border">
          <Input
            data-testid="run-team-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("orchestration.new.teamSearchPlaceholder")}
            className="h-9 border-0 bg-transparent text-xs shadow-none focus-visible:ring-0"
          />
        </div>

        <div className="flex">
          {!searching ? (
            <div className="flex w-[168px] shrink-0 flex-col gap-0.5 border-r border-border bg-sidebar py-1">
              <NavItem
                testid="run-team-scope-all"
                active={scope === "all"}
                onClick={() => setScope("all")}
                icon={<Users className="size-3.5 text-muted-foreground" aria-hidden="true" />}
                name={t("orchestration.new.teamAllAgents")}
                count={model.all.length}
              />
              {model.departments.map((d) => {
                const Icon = iconForKey(d.icon);
                return (
                  <NavItem
                    key={d.id}
                    testid={`run-team-scope-${d.id}`}
                    active={scope === d.id}
                    onClick={() => setScope(d.id)}
                    icon={
                      <span
                        className="flex size-4 items-center justify-center rounded-sm"
                        style={{ backgroundColor: tokenToCssColor(d.accentColor) ?? "#94a3b8" }}
                      >
                        <Icon className="size-2.5 text-white" aria-hidden="true" />
                      </span>
                    }
                    name={d.name}
                    count={d.agents.length}
                  />
                );
              })}
              {model.ungrouped.length > 0 ? (
                <NavItem
                  testid="run-team-scope-ungrouped"
                  active={scope === "ungrouped"}
                  onClick={() => setScope("ungrouped")}
                  icon={<CircleDashed className="size-3.5 text-muted-foreground" aria-hidden="true" />}
                  name={t("orchestration.new.teamUngrouped")}
                  count={model.ungrouped.length}
                />
              ) : null}
            </div>
          ) : null}

          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex items-center gap-2 border-b border-border px-3 py-2">
              <span className="font-semibold text-foreground">{title}</span>
              {shown.length > 0 ? (
                <span className="text-subtle-foreground">
                  {t("orchestration.new.teamDeptMembers", { count: shown.length })}
                </span>
              ) : null}
              {shown.length > 0 ? (
                <button
                  type="button"
                  data-testid="run-team-select-all"
                  onClick={toggleSelectAll}
                  className="ml-auto flex items-center gap-1.5 text-primary-text"
                >
                  <span>{t("orchestration.new.teamSelectAll")}</span>
                  <CheckBox state={selectAllState} />
                </button>
              ) : null}
            </div>

            {shown.length === 0 ? (
              <div
                data-testid="run-team-search-empty"
                className="px-3 py-6 text-center text-muted-foreground"
              >
                {t("orchestration.new.teamSearchEmpty")}
              </div>
            ) : (
              <div className="flex flex-col">
                {shown.map((a) => {
                  const on = selected.has(a.id);
                  return (
                    <button
                      key={a.id}
                      type="button"
                      data-testid={`run-team-agent-${a.id}`}
                      aria-pressed={on}
                      onClick={() => toggle(a.id)}
                      className={cn(
                        "flex items-center gap-2.5 border-t border-border px-3 py-2 text-left first:border-t-0",
                        on ? "bg-primary-soft" : "bg-card",
                      )}
                    >
                      <CheckBox state={on ? "checked" : "empty"} />
                      <AgentDot color={a.avatarColor} name={a.name} />
                      <span className={cn("text-foreground", on ? "font-medium" : "")}>{a.name}</span>
                      {a.backendType ? (
                        <span className="ml-auto rounded-sm bg-secondary px-1.5 py-0.5 font-mono text-2xs text-muted-foreground">
                          {backendLabel(a.backendType)}
                        </span>
                      ) : null}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {selectedAgents.length > 0 ? (
          <div
            data-testid="run-team-summary"
            className="flex items-center gap-2 border-t border-border bg-muted px-3 py-2"
          >
            <span className="font-medium text-muted-foreground">{t("orchestration.new.teamSummary")}</span>
            <span className="flex items-center gap-1">
              {selectedAgents.slice(0, 8).map((a) => (
                <AgentDot key={a.id} color={a.avatarColor} name={a.name} small />
              ))}
              {selectedAgents.length > 8 ? (
                <span className="text-2xs text-muted-foreground">+{selectedAgents.length - 8}</span>
              ) : null}
            </span>
            <span className="ml-auto font-medium text-foreground">
              {t("orchestration.new.teamTotal", { count: selectedAgents.length })}
            </span>
          </div>
        ) : null}
      </div>

      <span className="text-2xs text-muted-foreground">{t("orchestration.new.teamHint")}</span>
    </div>
  );
}

function NavItem({
  testid,
  active,
  onClick,
  icon,
  name,
  count,
}: {
  testid: string;
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  name: string;
  count: number;
}) {
  return (
    <button
      type="button"
      data-testid={testid}
      onClick={onClick}
      className={cn(
        "mx-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-left",
        active ? "bg-primary-soft font-medium text-primary-text" : "text-foreground hover:bg-accent",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{name}</span>
      <span className="font-mono text-2xs text-subtle-foreground">{count}</span>
    </button>
  );
}

type CbState = "checked" | "partial" | "empty";
function CheckBox({ state }: { state: CbState }) {
  if (state === "empty") {
    return <span className="size-4 shrink-0 rounded-sm border-[1.5px] border-border-strong" aria-hidden="true" />;
  }
  return (
    <span
      className="flex size-4 shrink-0 items-center justify-center rounded-sm bg-primary text-white"
      aria-hidden="true"
    >
      {state === "partial" ? <Minus className="size-3" /> : <Check className="size-3" />}
    </span>
  );
}

function AgentDot({ color, name, small = false }: { color: string; name: string; small?: boolean }) {
  const bg = tokenToCssColor(color) ?? "#94a3b8";
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full font-semibold text-white",
        small ? "size-[18px] text-[9px]" : "size-5 text-2xs",
      )}
      style={{ backgroundColor: bg }}
    >
      {firstLetter(name)}
    </span>
  );
}
