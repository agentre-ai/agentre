import * as React from "react";
import { ChevronDown, ChevronUp, GripVertical, Plus, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import type { agent_backend_svc } from "../../../../wailsjs/go/models";

import { execTargetReasonLabel } from "./exec-target-reasons";
import { moveItem } from "./exec-target-reorder";
import { useExecTargetAvailability } from "./use-exec-target-availability";

// ExecTargetRow 是 ExecTargetList 编辑态的一项：这里只关心 agentBackendId——
// 技能授权（ExecTargetInputDTO.skills）由 org-detail-agent.tsx 在同一个数组元素上
// 一并持有，ExecTargetList 不读也不写那部分，靠 agentBackendId 在 targets 数组内
// 唯一这条不变量对齐（前端"+ 添加"面板与后端 agent_svc.buildExecTargets 都靠它
// 判重）。
export type ExecTargetRow = { agentBackendId: number };

type Props = {
  agentId: number;
  agentName: string;
  targets: ExecTargetRow[];
  backends: agent_backend_svc.BackendItem[];
  onChange: (next: ExecTargetRow[]) => void;
  saveRejected?: boolean;
};

const BACKEND_TYPE_LABEL: Record<string, string> = {
  claudecode: "Claude Code",
  "claude-code": "Claude Code",
  codex: "Codex",
  builtin: "Built-in",
  openclaw: "OpenClaw",
  piagent: "Pi Agent",
};

function backendTypeLabel(type: string): string {
  return BACKEND_TYPE_LABEL[type] ?? type;
}

function machineLabel(
  b: agent_backend_svc.BackendItem | undefined,
  localLabel: string,
): string {
  if (!b || !b.deviceId) return localLabel;
  return b.deviceName || b.deviceId;
}

// 不可用原因里哪些是"供应商/配置类"问题（红色强调），哪些是"连通性类"问题
// （中性提示）——与 R15 的判据分组一致（决策 37 的既有 BlockReason vs R15b 新增的
// exec-target-* 三类）。
const DESTRUCTIVE_REASONS = new Set([
  "no-backend",
  "backend-requires-provider",
  "provider-inactive",
  "remote-provider-missing",
  "gateway-not-running",
  "remote-openclaw-unavailable",
  "unknown-backend",
]);

export function ExecTargetList(props: Props) {
  const { t } = useTranslation();
  const [addOpen, setAddOpen] = React.useState(false);
  const [announcement, setAnnouncement] = React.useState("");

  const targetsKey = React.useMemo(
    () =>
      props.targets
        .map((t) => t.agentBackendId)
        .slice()
        .sort((a, b) => a - b)
        .join(","),
    [props.targets],
  );
  const { byBackendId } = useExecTargetAvailability(props.agentId, targetsKey);

  const backendById = React.useMemo(() => {
    const m = new Map<number, agent_backend_svc.BackendItem>();
    for (const b of props.backends) m.set(b.id, b);
    return m;
  }, [props.backends]);

  const firstAvailableIndex = props.targets.findIndex(
    (t) => byBackendId.get(t.agentBackendId)?.available === true,
  );
  const allEvaluated =
    props.targets.length > 0 &&
    props.targets.every((t) => byBackendId.has(t.agentBackendId));
  const allUnavailable = allEvaluated && firstAvailableIndex === -1;

  const move = (from: number, to: number) => {
    const next = moveItem(props.targets, from, to);
    if (next === props.targets) return;
    props.onChange(next);
    setAnnouncement(
      t("org.agent.execTargets.moved", {
        position: to + 1,
        total: next.length,
      }),
    );
  };

  const removeAt = (idx: number) => {
    props.onChange(props.targets.filter((_, i) => i !== idx));
  };

  const appendBackend = (backendId: number) => {
    props.onChange([...props.targets, { agentBackendId: backendId }]);
    setAddOpen(false);
  };

  const replaceSole = (backendId: number) => {
    props.onChange([{ agentBackendId: backendId }]);
  };

  const usedIds = new Set(props.targets.map((t) => t.agentBackendId));
  const single = props.targets.length === 1;

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-center gap-2">
        <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
          {t("org.agent.execTargets.title")}
        </h3>
        <div className="flex-1" />
        {props.targets.length > 0 && (
          <Popover open={addOpen} onOpenChange={setAddOpen}>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-7 px-2 text-2xs"
              >
                <Plus className="size-3" aria-hidden="true" />
                {t("org.agent.execTargets.add")}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-72 p-0">
              <AddTargetPanel
                backends={props.backends}
                usedIds={usedIds}
                onPick={appendBackend}
              />
            </PopoverContent>
          </Popover>
        )}
      </div>

      {props.targets.length === 0 ? (
        <div className="rounded-md border border-border bg-input-bg px-3 py-5 text-center">
          <p className="text-sm text-muted-foreground">
            {t("org.agent.execTargets.emptyTitle")}
          </p>
          <p className="mt-1 text-2xs text-muted-foreground">
            {t("org.agent.execTargets.emptyHint")}
          </p>
          <Popover open={addOpen} onOpenChange={setAddOpen}>
            <PopoverTrigger asChild>
              <Button
                type="button"
                size="sm"
                className="mt-2 h-7 px-2 text-2xs"
              >
                <Plus className="size-3" aria-hidden="true" />
                {t("org.agent.execTargets.addFull")}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-72 p-0">
              <AddTargetPanel
                backends={props.backends}
                usedIds={usedIds}
                onPick={appendBackend}
              />
            </PopoverContent>
          </Popover>
          {props.saveRejected && (
            <p className="mt-1.5 text-2xs text-destructive">
              {t("org.agent.execTargets.saveRejected")}
            </p>
          )}
        </div>
      ) : (
        <div className="flex flex-col overflow-hidden rounded-md border border-border bg-input-bg">
          {props.targets.map((target, index) => {
            const b = backendById.get(target.agentBackendId);
            const status = byBackendId.get(target.agentBackendId);
            return (
              <ExecTargetRowView
                key={target.agentBackendId}
                index={index}
                total={props.targets.length}
                backend={b}
                status={status}
                // R20：只有一项时不显示「当前生效」徽标——一档时它零信息量，
                // 与序号/拖拽柄/排序说明同批隐去。
                isFirstAvailable={!single && index === firstAvailableIndex}
                onMoveUp={index > 0 ? () => move(index, index - 1) : undefined}
                onMoveDown={
                  index < props.targets.length - 1
                    ? () => move(index, index + 1)
                    : undefined
                }
                onRemove={!single ? () => removeAt(index) : undefined}
                replaceBackends={single ? props.backends : undefined}
                onReplacePick={single ? replaceSole : undefined}
              />
            );
          })}
        </div>
      )}

      {props.targets.length > 1 && (
        <p className="text-2xs text-muted-foreground">
          {t("org.agent.execTargets.hint")}
        </p>
      )}

      {allUnavailable && (
        <div className="flex gap-2 rounded-md border border-destructive bg-destructive-soft p-3">
          <div className="flex min-w-0 flex-col gap-1">
            <span className="text-xs font-semibold">
              {t("org.agent.execTargets.allUnavailableTitle", {
                name: props.agentName,
              })}
            </span>
            <span className="text-2xs leading-relaxed text-muted-foreground">
              {props.targets.map((target, i) => {
                const status = byBackendId.get(target.agentBackendId);
                const b = backendById.get(target.agentBackendId);
                return (
                  <React.Fragment key={target.agentBackendId}>
                    {i + 1} ·{" "}
                    {machineLabel(b, t("org.agent.execTargets.localMachine"))}
                    {" —— "}
                    {status ? execTargetReasonLabel(status.reason, t) : ""}
                    <br />
                  </React.Fragment>
                );
              })}
            </span>
          </div>
        </div>
      )}

      <p role="status" className="sr-only">
        {announcement}
      </p>
    </div>
  );
}

type RowProps = {
  index: number;
  total: number;
  backend: agent_backend_svc.BackendItem | undefined;
  status: { available: boolean; reason: string } | undefined;
  isFirstAvailable: boolean;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  onRemove?: () => void;
  // 单档场景专用:"更换"按钮打开一份 AddTargetPanel,选中的一项整份替换掉
  // 唯一那一档(而不是追加)。只在 total===1 时给出。
  replaceBackends?: agent_backend_svc.BackendItem[];
  onReplacePick?: (backendId: number) => void;
};

function ExecTargetRowView(props: RowProps) {
  const { t } = useTranslation();
  const [replaceOpen, setReplaceOpen] = React.useState(false);
  const single = props.total === 1;
  const b = props.backend;
  const local = machineLabel(b, t("org.agent.execTargets.localMachine"));
  const sub = b
    ? `${backendTypeLabel(b.type)} · ${b.name}`
    : t("org.agent.execTargets.reasons.unknownBackend");

  return (
    <div
      className={cn(
        "flex items-start gap-2 border-border px-2.5 py-2",
        props.index < props.total - 1 && "border-b",
      )}
      data-testid={`exec-target-row-${props.index}`}
    >
      {!single && (
        <span
          className="select-none pt-0.5 text-sm text-subtle-foreground"
          aria-hidden="true"
        >
          <GripVertical className="size-3.5" />
        </span>
      )}
      {!single && (
        <span className="mt-px inline-flex size-5 shrink-0 items-center justify-center rounded-sm bg-secondary font-mono text-2xs text-muted-foreground">
          {props.index + 1}
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="text-xs font-semibold">{local}</span>
        <span className="font-mono text-2xs text-muted-foreground">{sub}</span>
      </div>
      {!single && (
        <div className="flex shrink-0 flex-col">
          <button
            type="button"
            aria-label={t("org.agent.execTargets.moveUp")}
            disabled={!props.onMoveUp}
            onClick={props.onMoveUp}
            className="text-muted-foreground hover:text-foreground disabled:opacity-30"
          >
            <ChevronUp className="size-3.5" />
          </button>
          <button
            type="button"
            aria-label={t("org.agent.execTargets.moveDown")}
            disabled={!props.onMoveDown}
            onClick={props.onMoveDown}
            className="text-muted-foreground hover:text-foreground disabled:opacity-30"
          >
            <ChevronDown className="size-3.5" />
          </button>
        </div>
      )}
      <StatusBadge
        status={props.status}
        isFirstAvailable={props.isFirstAvailable}
        isRemote={Boolean(b?.deviceId)}
      />
      {props.onRemove && (
        <button
          type="button"
          aria-label={t("org.agent.execTargets.remove")}
          onClick={props.onRemove}
          className="shrink-0 text-muted-foreground hover:text-destructive"
        >
          <X className="size-3.5" />
        </button>
      )}
      {props.replaceBackends && props.onReplacePick && (
        <Popover open={replaceOpen} onOpenChange={setReplaceOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              className="shrink-0 text-2xs text-muted-foreground hover:underline"
            >
              {t("org.agent.execTargets.replace")}
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-72 p-0">
            <AddTargetPanel
              backends={props.replaceBackends}
              usedIds={new Set()}
              onPick={(id) => {
                props.onReplacePick?.(id);
                setReplaceOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

function StatusBadge(props: {
  status: { available: boolean; reason: string } | undefined;
  isFirstAvailable: boolean;
  isRemote: boolean;
}) {
  const { t } = useTranslation();
  if (!props.status) return null;
  if (!props.status.available) {
    const destructive = DESTRUCTIVE_REASONS.has(props.status.reason);
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center rounded-sm px-1.5 py-0.5 text-2xs font-medium",
          destructive
            ? "bg-destructive-soft text-destructive"
            : "border border-border text-muted-foreground",
        )}
      >
        {execTargetReasonLabel(props.status.reason, t)}
      </span>
    );
  }
  if (props.isFirstAvailable) {
    return (
      <span className="inline-flex shrink-0 items-center rounded-sm bg-status-running-bg px-1.5 py-0.5 text-2xs font-medium text-status-running">
        {t("org.agent.execTargets.currentlyActive")}
      </span>
    );
  }
  if (props.isRemote) {
    return (
      <span className="inline-flex shrink-0 items-center rounded-sm bg-secondary px-1.5 py-0.5 text-2xs font-medium text-secondary-foreground">
        {t("org.agent.execTargets.online")}
      </span>
    );
  }
  return null;
}

function AddTargetPanel(props: {
  backends: agent_backend_svc.BackendItem[];
  usedIds: Set<number>;
  onPick: (backendId: number) => void;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = React.useState("");

  const groups = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = props.backends.filter(
      (b) =>
        !q ||
        b.name.toLowerCase().includes(q) ||
        (b.deviceName ?? "").toLowerCase().includes(q),
    );
    const byGroup = new Map<string, agent_backend_svc.BackendItem[]>();
    for (const b of filtered) {
      const key = b.deviceId || "";
      const list = byGroup.get(key) ?? [];
      list.push(b);
      byGroup.set(key, list);
    }
    return byGroup;
  }, [props.backends, query]);

  return (
    <div>
      <div className="border-b border-border bg-input-bg px-3 py-2">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("org.agent.execTargets.addPanel.searchPlaceholder")}
          aria-label={t("org.agent.execTargets.addPanel.searchPlaceholder")}
          className="w-full bg-transparent text-xs text-foreground outline-none placeholder:text-muted-foreground"
        />
      </div>
      <div className="max-h-72 overflow-y-auto py-1">
        {[...groups.entries()].map(([deviceId, backends]) => (
          <div key={deviceId || "local"}>
            <div className="px-3 pb-1 pt-2 text-2xs font-semibold text-subtle-foreground">
              {deviceId
                ? backends[0].deviceName || deviceId
                : t("org.agent.execTargets.localMachine")}
            </div>
            {backends.map((b) => {
              const used = props.usedIds.has(b.id);
              const unpaired = Boolean(b.deviceId) && !b.deviceName;
              return (
                <button
                  type="button"
                  key={b.id}
                  disabled={used}
                  onClick={() => props.onPick(b.id)}
                  className={cn(
                    "flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent",
                    used && "cursor-not-allowed opacity-60",
                  )}
                >
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="text-xs font-semibold">
                      {backendTypeLabel(b.type)} · {b.name}
                    </span>
                  </div>
                  {used && (
                    <span className="inline-flex shrink-0 items-center rounded-sm border border-border px-1.5 py-0.5 text-2xs text-muted-foreground">
                      {t("org.agent.execTargets.addPanel.alreadyInList")}
                    </span>
                  )}
                  {!used && unpaired && (
                    <span className="inline-flex shrink-0 items-center rounded-sm border border-border px-1.5 py-0.5 text-2xs text-muted-foreground">
                      {t("org.agent.execTargets.addPanel.unpaired")}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}
