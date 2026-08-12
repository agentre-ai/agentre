// 共享 ModelTargetPicker（spec「UI, accessibility and recent targets」）。
//
// 三处复用同一主体，只替换顶部特殊项（scenario）：
//   - backend：顶部特殊项 = CLI 自身登录态（native）；
//   - chat：顶部特殊项 = 跟随 Agent 绑定（inherit-agent）；
//   - route：顶部特殊项 = 继承主绑定（inherit-main）。
//
// 主体交互：搜索、最近使用（localStorage，按执行位置指纹隔离）、Provider 分组、
// provider-default 首项、fixed-model 列表、兼容性过滤（effective backend type）、
// loading/empty/error/invalid/remote 状态、键盘导航（方向键 / Enter / Esc / focus ring）。
// 只通过 onChange 发射 providerKey/modelKey，绝不发射名称 / ModelID / 凭据。
import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  Check,
  ChevronDown,
  History,
  Loader2,
  Search,
  ShieldAlert,
  X,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { readRecentTargets } from "./recents";
import {
  isNativeTarget,
  providerCompatibleForBackend,
  sameTarget,
  type ModelTarget,
  type PickerProvider,
  type PickerScenario,
} from "./types";

// specialItemKey 顶部特殊项（native / inherit）在选项列表里的唯一 key。
const SPECIAL_ITEM_KEY = "__special__";

export type ModelTargetPickerProps = {
  scenario: PickerScenario;
  // backendType 是 effective backend 类型，用于 provider.type 兼容性过滤。
  backendType: string;
  // executionLocation 空串 = 本机；非空 = 远端设备 id（最近使用指纹隔离）。
  executionLocation?: string;
  selected: ModelTarget | null;
  onChange: (target: ModelTarget) => void;
  catalog: PickerProvider[];
  loading?: boolean;
  // error：模型目录拉取失败 → 弹层错误行。errorText 覆盖默认错误文案（chat 场景把
  // 持久化切换失败的真实信息透出来）。
  error?: boolean;
  errorText?: string;
  disabled?: boolean;
  // invalid：当前选中的 target 在目录里解析不出来（Provider/Model 缺失/停用/被删）。
  invalid?: boolean;
  // remoteMissing：目标执行设备上缺少所选 Provider（远端场景提示，task 6 深化）。
  remoteMissing?: boolean;
  // supportsFixedModel：目标执行设备是否公布 llm-model-target-v1 能力位（task 6 决策 11）。
  // 远端且为 false 时禁用所有 fixed-model 选项，避免旧 daemon 静默降级为默认模型；
  // 本机 / 未传 → 默认 true（不限制）。
  supportsFixedModel?: boolean;
  // remoteCatalog：目标执行设备的 daemon 目录（task 6 决策 12，远端可运行事实源）。
  // 未传 = 本机（不启用远端门控）。传了以后，desktop 目录里在 daemon 上不存在的
  // Provider/Model 被禁用并标注「需同步」，绝不保存一个未经验证的远端目标。
  remoteCatalog?: PickerProvider[];
  // footer：弹层底部常显说明（chat 场景的「自下一轮生效」等），随弹层一起出现。
  footer?: React.ReactNode;
  // compact：表单内嵌（claude tier 路由）用小号触发按钮。
  compact?: boolean;
  align?: "start" | "end";
  className?: string;
  title?: string;
  // triggerLabel：覆盖触发按钮文案。chat 场景「未选但 agent 已绑 provider」时显示
  // 绑定供应商名，而不是顶部特殊项「跟随 Agent 绑定」；undefined 时按目录解析。
  triggerLabel?: string;
  "data-testid"?: string;
  "aria-label"?: string;
};

type Option = {
  key: string;
  kind: "special" | "recent" | "provider-default" | "fixed";
  label: string;
  sublabel?: string;
  target: ModelTarget;
  disabled: boolean;
  group?: string;
  // disabledHint 远端门控的禁用原因（task 6）：桌面目录里 daemon 上没有的
  // Provider/Model，或旧 daemon 不支持的 fixed-model。
  disabledHint?: string;
};

export function ModelTargetPicker({
  scenario,
  backendType,
  executionLocation = "",
  selected,
  onChange,
  catalog,
  loading = false,
  error = false,
  errorText,
  disabled = false,
  invalid = false,
  remoteMissing = false,
  supportsFixedModel = true,
  remoteCatalog,
  footer,
  compact = false,
  align = "start",
  className,
  title,
  triggerLabel,
  "data-testid": testId,
  "aria-label": ariaLabel,
}: ModelTargetPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");
  const [activeIndex, setActiveIndex] = React.useState(0);
  const searchRef = React.useRef<HTMLInputElement>(null);
  const listRef = React.useRef<HTMLUListElement>(null);

  const specialLabel = t(`modelTargetPicker.special.${scenario}`);
  const selectedLabel = React.useMemo(() => {
    if (isNativeTarget(selected)) return specialLabel;
    if (!selected) return specialLabel;
    const p = catalog.find((x) => x.providerKey === selected.providerKey);
    if (!p) return selected.providerKey;
    if (!selected.modelKey) {
      // provider-default：摘要必须显示 Provider 与实际模型（当前默认模型的解析结果），
      // 不得只显示 Provider 名称（spec「Backend and Route flow」）。默认模型缺失/停用
      // 时无实际模型可展示，回落 Provider 名。
      const dm = p.defaultModel;
      return dm ? `${p.name} · ${dm.modelId}` : p.name;
    }
    const m = p.models.find((x) => x.modelKey === selected.modelKey);
    return m ? `${p.name} · ${m.modelId}` : p.name;
  }, [catalog, selected, specialLabel]);

  // 兼容目录：按 effective backend type 过滤。
  const compatible = React.useMemo(
    () =>
      catalog.filter((p) => providerCompatibleForBackend(backendType, p.type)),
    [catalog, backendType],
  );

  // 最近使用（按执行位置指纹隔离）。只展示当前 backend 兼容的项；失效项禁用。
  const recents = React.useMemo(() => {
    const all = readRecentTargets(scenario, executionLocation);
    const seen = new Set<string>();
    const out: Option[] = [];
    for (const r of all) {
      const p = compatible.find((x) => x.providerKey === r.providerKey);
      if (!p) continue; // 当前 backend 不兼容 → 隐藏
      // 失效判定：仅当 recent 的固定模型已不存在/停用，或 provider 停用时才禁用。
      // 固定模型是不是 Provider 当前默认模型不影响其可选择性 —— 非默认的 fixed-model
      // recent 必须仍可一键重选（recent 存在的意义），不能因为不是默认就被禁用。
      const modelOk =
        r.modelKey === "" ||
        p.models.some((m) => m.modelKey === r.modelKey && m.enabled);
      const target: ModelTarget = {
        providerKey: r.providerKey,
        modelKey: r.modelKey === p.defaultModel?.modelKey ? "" : r.modelKey,
      };
      const dedupeKey = `${target.providerKey}\u0000${target.modelKey}`;
      if (seen.has(dedupeKey)) continue;
      seen.add(dedupeKey);
      out.push({
        key: `recent-${dedupeKey}`,
        kind: "recent",
        label: target.modelKey
          ? (p.models.find((m) => m.modelKey === target.modelKey)?.modelId ??
            target.modelKey)
          : p.name,
        sublabel: target.modelKey
          ? p.name
          : t("modelTargetPicker.defaultLabel"),
        target,
        disabled: !p.enabled || !modelOk,
      });
    }
    return out.slice(0, 5);
  }, [compatible, executionLocation, scenario, t]);

  // remoteByKey：daemon 目录的 Provider/Model 存在性索引（task 6 决策 12）。
  // providerKey → provider（含其 models 的 modelKey 集合）。
  const remoteByKey = React.useMemo(() => {
    if (!remoteCatalog) return null;
    const m = new Map<string, PickerProvider>();
    for (const p of remoteCatalog) m.set(p.providerKey, p);
    return m;
  }, [remoteCatalog]);

  const remoteSyncHint = t("modelTargetPicker.remoteSyncNeeded");
  const remoteFixedHint = t("modelTargetPicker.fixedModelUnsupported");

  // 目录选项（provider-default 首项，再 fixed-model 列表）。
  const catalogOptions = React.useMemo(() => {
    const out: Option[] = [];
    for (const p of compatible) {
      const groupLabel = p.name;
      // 远端门控：daemon 上没有该 Provider → 本机独有，需同步后才能选。
      const remoteProvider =
        remoteByKey && executionLocation
          ? remoteByKey.get(p.providerKey)
          : undefined;
      const providerSyncNeeded =
        remoteByKey != null && executionLocation !== "" && !remoteProvider;
      // provider-default 首项：当前默认模型解析结果（可能缺失 = 目标已失效，但仍可选，
      // 由后端/父组件按 kind 决定是否阻止保存）。
      const defaultModel = p.defaultModel;
      out.push({
        key: `default-${p.providerKey}`,
        kind: "provider-default",
        group: groupLabel,
        label: p.name,
        sublabel:
          defaultModel?.modelId ?? t("modelTargetPicker.noDefaultModel"),
        target: { providerKey: p.providerKey, modelKey: "" },
        disabled: !p.enabled || providerSyncNeeded,
        disabledHint: providerSyncNeeded ? remoteSyncHint : undefined,
      });
      // fixed-model 列表。
      for (const m of p.models) {
        if (m.modelKey === defaultModel?.modelKey) continue;
        // 远端门控：模型在 daemon 上不存在 / 停用 → 需同步；daemon 不支持
        // fixed-model（旧协议）→ 一律禁用，绝不静默降级。
        const remoteModelOk =
          !remoteProvider ||
          remoteProvider.models.some(
            (rm) => rm.modelKey === m.modelKey && rm.enabled,
          );
        const fixedUnsupported =
          remoteByKey != null && executionLocation !== "" && !supportsFixedModel;
        const fixedSyncNeeded =
          remoteByKey != null &&
          executionLocation !== "" &&
          remoteProvider != null &&
          !remoteModelOk;
        out.push({
          key: `fixed-${p.providerKey}-${m.modelKey}`,
          kind: "fixed",
          group: groupLabel,
          label: p.name,
          sublabel: m.modelId,
          target: { providerKey: p.providerKey, modelKey: m.modelKey },
          disabled:
            !p.enabled || !m.enabled || providerSyncNeeded ||
            fixedUnsupported ||
            fixedSyncNeeded,
          disabledHint: fixedUnsupported
            ? remoteFixedHint
            : providerSyncNeeded || fixedSyncNeeded
              ? remoteSyncHint
              : undefined,
        });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [compatible, executionLocation, remoteByKey, supportsFixedModel, t]);

  // 特殊项（native / inherit）。
  const specialOption: Option = {
    key: SPECIAL_ITEM_KEY,
    kind: "special",
    label: specialLabel,
    target: { providerKey: "", modelKey: "" },
    disabled: false,
  };

  // 搜索过滤：匹配 provider 名 / model id / 特殊项。
  const flatOptions = React.useMemo(() => {
    const q = search.trim().toLowerCase();
    const base: Option[] = [];
    if (q === "") {
      base.push(specialOption);
      base.push(...recents);
    } else if (specialLabel.toLowerCase().includes(q)) {
      base.push(specialOption);
    }
    base.push(...catalogOptions);
    if (q === "") return base;
    const match = (o: Option) =>
      (o.label + " " + (o.sublabel ?? "")).toLowerCase().includes(q);
    return base.filter(match);
  }, [search, specialOption, recents, catalogOptions, specialLabel]);

  // 打开时重置搜索与活动索引（优先定位当前选中项）。
  const handleOpenChange = React.useCallback(
    (next: boolean) => {
      setOpen(next);
      if (next) {
        setSearch("");
        const idx = flatOptions.findIndex(
          (o) => !o.disabled && sameTarget(o.target, selected),
        );
        setActiveIndex(idx >= 0 ? idx : 0);
        setTimeout(() => searchRef.current?.focus(), 0);
      }
    },
    [flatOptions, selected],
  );

  // 键盘导航：↑↓ 移动、Enter 选中、Esc 清搜索/关闭。
  const handleKeyDown = React.useCallback(
    (e: React.KeyboardEvent) => {
      if (flatOptions.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, flatOptions.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const opt = flatOptions[activeIndex];
        if (opt && !opt.disabled) {
          onChange(opt.target);
          setOpen(false);
        }
      } else if (e.key === "Escape") {
        if (search) {
          e.preventDefault();
          setSearch("");
        } else {
          setOpen(false);
        }
      }
    },
    [flatOptions, activeIndex, onChange, search],
  );

  // 活动项滚入可视区。
  React.useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(
      `[data-option-index="${activeIndex}"]`,
    );
    el?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  const hasCatalog = catalogOptions.length > 0;

  const triggerText = triggerLabel ?? selectedLabel;

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          data-testid={testId}
          title={title}
          aria-label={ariaLabel}
          aria-expanded={open}
          aria-haspopup="listbox"
          className={cn(
            "inline-flex w-full items-center justify-between gap-2 rounded-md border border-border bg-background text-xs transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
            "disabled:cursor-not-allowed disabled:opacity-60",
            compact ? "h-7 px-2" : "h-9 px-2.5",
            invalid
              ? "border-status-waiting/60 bg-status-waiting-bg"
              : "hover:bg-secondary/60",
            className,
          )}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            {invalid ? (
              <ShieldAlert
                className={cn("size-3.5 shrink-0 text-status-waiting")}
                aria-hidden="true"
              />
            ) : null}
            <span
              className={cn(
                "min-w-0 truncate",
                invalid ? "text-status-waiting" : "text-foreground",
              )}
            >
              {triggerText}
            </span>
          </span>
          <ChevronDown
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align={align}
        side="bottom"
        sideOffset={6}
        className="w-[340px] p-0"
        onKeyDown={handleKeyDown}
      >
        {/* 搜索框 */}
        <div className="flex items-center gap-1.5 border-b border-border px-2.5 py-2">
          <Search
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            ref={searchRef}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setActiveIndex(0);
            }}
            placeholder={t("modelTargetPicker.searchPlaceholder")}
            className="h-7 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0"
          />
          {search ? (
            <button
              type="button"
              aria-label={t("modelTargetPicker.clearSearch")}
              className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={() => {
                setSearch("");
                searchRef.current?.focus();
              }}
            >
              <X className="size-3.5" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        <div className="max-h-64 overflow-y-auto p-1.5">
          {loading ? (
            <div className="flex items-center gap-2 px-2.5 py-3 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
              {t("modelTargetPicker.loading")}
            </div>
          ) : error ? (
            <div className="px-2.5 py-3 text-xs text-status-waiting">
              {errorText ?? t("modelTargetPicker.error")}
            </div>
          ) : !hasCatalog && isNativeTarget(selected) ? (
            <div className="px-2.5 py-3 text-xs text-muted-foreground">
              {t("modelTargetPicker.empty")}
            </div>
          ) : null}

          {!loading && !error && flatOptions.length > 0 ? (
            <ul
              ref={listRef}
              role="listbox"
              aria-label={ariaLabel}
              className="flex flex-col gap-0.5"
            >
              {flatOptions.map((opt, i) => {
                const active = i === activeIndex;
                const selectedNow = sameTarget(opt.target, selected);
                const showGroup =
                  opt.group &&
                  (i === 0 || flatOptions[i - 1].group !== opt.group);
                return (
                  <li key={opt.key} role="none">
                    {showGroup ? (
                      <div className="px-2.5 pb-0.5 pt-2 text-2xs font-medium uppercase tracking-wide text-subtle-foreground">
                        {opt.group}
                      </div>
                    ) : null}
                    <button
                      type="button"
                      role="option"
                      aria-selected={selectedNow}
                      aria-disabled={opt.disabled}
                      data-option-index={i}
                      disabled={opt.disabled}
                      title={opt.disabledHint}
                      onMouseEnter={() => setActiveIndex(i)}
                      onClick={() => {
                        if (opt.disabled) return;
                        onChange(opt.target);
                        setOpen(false);
                      }}
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs transition-colors",
                        "focus-visible:outline-none",
                        active
                          ? "bg-accent"
                          : opt.kind === "recent"
                            ? "bg-secondary/40"
                            : "hover:bg-accent/60",
                        "disabled:cursor-not-allowed disabled:opacity-50",
                      )}
                    >
                      <span className="flex min-w-0 flex-1 items-center gap-2">
                        {opt.kind === "recent" ? (
                          <History
                            className="size-3 shrink-0 text-muted-foreground"
                            aria-hidden="true"
                          />
                        ) : opt.kind === "special" ? (
                          <span className="inline-flex size-3.5 shrink-0 items-center justify-center rounded-sm bg-secondary text-[9px] font-semibold text-muted-foreground">
                            A
                          </span>
                        ) : null}
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span
                            className={cn(
                              "truncate",
                              opt.kind === "special"
                                ? "font-medium text-foreground"
                                : "text-foreground",
                            )}
                          >
                            {opt.label}
                          </span>
                          {opt.sublabel ? (
                            <span className="truncate font-mono text-2xs text-muted-foreground">
                              {opt.sublabel}
                            </span>
                          ) : null}
                        </span>
                        {opt.kind === "provider-default" ? (
                          <Badge
                            variant="secondary"
                            className="shrink-0 rounded-sm px-1 py-0 font-mono text-2xs"
                          >
                            {t("modelTargetPicker.defaultBadge")}
                          </Badge>
                        ) : null}
                      </span>
                      {selectedNow ? (
                        <Check
                          className="size-3.5 shrink-0 text-primary-text"
                          aria-hidden="true"
                        />
                      ) : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          ) : null}
        </div>

        {invalid ? (
          <div className="border-t border-border bg-status-waiting-bg px-3 py-2 text-2xs text-status-waiting">
            {t("modelTargetPicker.invalidHint")}
          </div>
        ) : null}
        {remoteByKey != null && executionLocation !== "" &&
        catalogOptions.some((o) => o.disabledHint) ? (
          <div className="border-t border-border bg-secondary px-3 py-2 text-2xs text-muted-foreground">
            {t("modelTargetPicker.remoteGateHint")}
          </div>
        ) : null}
        {remoteMissing ? (
          <div className="border-t border-border bg-secondary px-3 py-2 text-2xs text-muted-foreground">
            {t("modelTargetPicker.remoteMissing")}
          </div>
        ) : null}
        {footer ? (
          <div className="border-t border-border px-3 py-2 text-2xs text-muted-foreground">
            {footer}
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

export { readRecentTargets, recordRecentTarget } from "./recents";
export type {
  ModelTarget,
  PickerModel,
  PickerProvider,
  PickerScenario,
} from "./types";
export { buildPickerCatalog, useModelTargetCatalog } from "./catalog";
export {
  isNativeTarget,
  providerCompatibleForBackend,
  sameTarget,
} from "./types";
