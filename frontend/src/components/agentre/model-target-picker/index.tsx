// 共享 ModelTargetPicker（spec「UI, accessibility and recent targets」）。
//
// 三处复用同一主体，只替换顶部特殊项（scenario）：
//   - backend：顶部特殊项 = CLI 自身登录态（native）；
//   - chat：顶部特殊项 = 跟随 Agent 绑定（inherit-agent）；
//   - route：顶部特殊项 = 继承主绑定（inherit-main）。
//
// 主体交互：搜索、最近使用（localStorage，按执行位置指纹隔离，单行横向可移除 chip）、
// Provider 分组（sticky 组头承载品牌标识 + 供应商名）、provider-default 首项（强调底色 +
// 动态图标 + 当前解析模型）、fixed-model 列表（display name + 上下文/最大输出）、
// 兼容性过滤（effective backend type）、loading/empty/error/invalid/remote 状态、
// 键盘导航（方向键 / Enter / Esc / focus ring）。
// 只通过 onChange 发射 providerKey/modelKey，绝不发射名称 / ModelID / 凭据。
import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  Check,
  ChevronDown,
  GitBranch,
  Loader2,
  Monitor,
  RefreshCw,
  Search,
  Upload,
  X,
} from "lucide-react";

import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { LlmModelLogo, LlmProviderLogo } from "../ai-brand-logo";
import { formatTokens } from "../llm-provider-models";
import { readRecentTargets, removeRecentTarget } from "./recents";
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
  // openOnMount：直达更换绑定等入口打开消费方时，同时展开选择器。
  openOnMount?: boolean;
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
  // deviceLabel：目标执行设备的人读名字。传了以后弹层顶部说明「在该设备上执行、以该设备
  // 的配置为准」，并给设备上已有的供应商组打标记；未传（本机 / 消费方还没解析出名字）
  // 时整块不渲染。
  deviceLabel?: string;
  // specialSublabel：顶部特殊项的解析结果副行（chat/route 由消费方解析后传入，
  // 写出「供应商 · 模型」或「跟随该 Agent 绑定的供应商」）。backend 场景未传时
  // 回落「由 CLI 自身的登录账号决定」。
  specialSublabel?: string;
  // 远端目录缺少本机 Provider 时的显式同步入口；消费方负责确认与执行凭证复制。
  onSyncProvider?: (provider: PickerProvider) => void;
  // footer：弹层底部常显说明（chat 场景的「自下一轮生效」等），随弹层一起出现。
  footer?: React.ReactNode;
  // compact：表单内嵌（claude tier 路由）用小号触发按钮。
  compact?: boolean;
  align?: "start" | "end";
  className?: string;
  title?: string;
  // triggerLabel：覆盖触发按钮主行。chat 场景「未选但 agent 已绑 provider」时显示
  // 绑定供应商名，而不是顶部特殊项「跟随 Agent 绑定」；undefined 时按目录解析。
  // 可以是节点（backend 编辑器的主行 = 品牌标识 + 供应商名 + 跟随/固定徽标）；此时
  // 按钮的无障碍名改由 aria-label 决定，节点内容不参与名字计算（见 triggerAriaLabel）。
  triggerLabel?: React.ReactNode;
  // triggerSub：可选触发按钮副行。后端编辑器用它展示当前解析出的生效模型与模式后果；
  // 未传时保持 model-pill / chat 等既有消费方的单行形态。
  triggerSub?: React.ReactNode;
  "data-testid"?: string;
  "aria-label"?: string;
};

type Option = {
  key: string;
  kind: "special" | "invalid" | "provider-default" | "fixed";
  label: string;
  sublabel?: string;
  target: ModelTarget;
  disabled: boolean;
  group?: string;
  groupType?: string;
  // disabledHint 不可选项的行内原因：模型停用 / 供应商停用 / 远端需同步 / 不支持固定模型。
  disabledHint?: string;
  // fixed 行专属：modelId 供 LlmModelLogo 判定品牌，contextWindow/maxOutput 供右侧展示。
  modelId?: string;
  contextWindow?: number;
  maxOutput?: number;
};

type RecentChip = {
  key: string;
  label: string;
  target: ModelTarget;
  disabled: boolean;
  title?: string;
};

// resolveTargetLabel 把 target 解析成人读摘要（供应商 · 模型）；目录里解析不出来时
// 回落原始 providerKey/modelKey。供触发按钮、失效警示与失效保留项共用。
function resolveTargetLabel(
  target: ModelTarget,
  catalog: PickerProvider[],
): string {
  const p = catalog.find((x) => x.providerKey === target.providerKey);
  const providerLabel = p?.name ?? target.providerKey;
  if (!target.modelKey) {
    return p?.defaultModel
      ? `${providerLabel} · ${p.defaultModel.modelId}`
      : providerLabel;
  }
  const m = p?.models.find((x) => x.modelKey === target.modelKey);
  const modelLabel = m ? m.name || m.modelId : target.modelKey;
  return `${providerLabel} · ${modelLabel}`;
}

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
  openOnMount = false,
  invalid = false,
  remoteMissing = false,
  supportsFixedModel = true,
  remoteCatalog,
  deviceLabel,
  specialSublabel,
  onSyncProvider,
  footer,
  compact = false,
  align = "start",
  className,
  title,
  triggerLabel,
  triggerSub,
  "data-testid": testId,
  "aria-label": ariaLabel,
}: ModelTargetPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(openOnMount);
  const [search, setSearch] = React.useState("");
  const [activeIndex, setActiveIndex] = React.useState(0);
  const [recentTick, setRecentTick] = React.useState(0);
  const searchRef = React.useRef<HTMLInputElement>(null);
  const listRef = React.useRef<HTMLUListElement>(null);

  const specialLabel = t(`modelTargetPicker.special.${scenario}`);
  const specialResolution =
    specialSublabel ??
    (scenario === "backend"
      ? t("modelTargetPicker.special.backendSublabel")
      : undefined);

  const selectedLabel = React.useMemo(() => {
    if (!selected || isNativeTarget(selected)) return specialLabel;
    return resolveTargetLabel(selected, catalog);
  }, [catalog, selected, specialLabel]);

  // 兼容目录：按 effective backend type 过滤。
  const compatible = React.useMemo(
    () =>
      catalog.filter((p) => providerCompatibleForBackend(backendType, p.type)),
    [catalog, backendType],
  );

  // 行内禁用原因文案（t 派生）。
  const remoteSyncHint = t("modelTargetPicker.remoteSyncNeeded");
  const remoteFixedHint = t("modelTargetPicker.fixedModelUnsupported");
  const disabledModelHint = t("modelTargetPicker.disabledModel");
  const disabledProviderHint = t("modelTargetPicker.disabledProvider");
  const followDefaultLabel = t("modelTargetPicker.followDefault");
  const noDefaultModelLabel = t("modelTargetPicker.noDefaultModel");

  // remoteByKey：daemon 目录的 Provider/Model 存在性索引（task 6 决策 12）。
  // providerKey → provider（含其 models 的 modelKey 集合）。
  const remoteByKey = React.useMemo(() => {
    if (!remoteCatalog) return null;
    const m = new Map<string, PickerProvider>();
    for (const p of remoteCatalog) m.set(p.providerKey, p);
    return m;
  }, [remoteCatalog]);

  // remoteGated：远端执行 + 已知 daemon 目录 → 组头标注「设备上已有」、行内同步入口生效。
  const remoteGated = remoteByKey != null && executionLocation !== "";

  // 最近使用（按执行位置指纹隔离）。只展示当前 backend 兼容的项；失效项禁用并给原因。
  const recents = React.useMemo(() => {
    // recentTick 仅在移除 chip 后递增，强制重新读取 localStorage；读取仍走 localStorage
    // 而不是内存态，保证多实例/刷新后一致。
    void recentTick;
    const all = readRecentTargets(scenario, executionLocation);
    const seen = new Set<string>();
    const out: RecentChip[] = [];
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
        modelKey: r.modelKey,
      };
      const remoteProvider =
        executionLocation !== "" ? remoteByKey?.get(p.providerKey) : undefined;
      const remoteTargetOk =
        remoteByKey == null || executionLocation === ""
          ? true
          : target.modelKey === ""
            ? remoteProvider?.defaultModel?.modelKey ===
              p.defaultModel?.modelKey
            : supportsFixedModel &&
              remoteProvider?.models.some(
                (model) => model.modelKey === target.modelKey && model.enabled,
              ) === true;
      const dedupeKey = `${target.providerKey}\u0000${target.modelKey}`;
      if (seen.has(dedupeKey)) continue;
      seen.add(dedupeKey);
      const label = target.modelKey
        ? (p.models.find((m) => m.modelKey === target.modelKey)?.modelId ??
          target.modelKey)
        : p.name;
      out.push({
        key: `recent-${dedupeKey}`,
        label,
        target,
        disabled: !p.enabled || !modelOk || !remoteTargetOk,
        title: !p.enabled
          ? disabledProviderHint
          : !modelOk
            ? disabledModelHint
            : !remoteTargetOk
              ? !supportsFixedModel && target.modelKey
                ? remoteFixedHint
                : remoteSyncHint
              : target.modelKey
                ? undefined
                : t("modelTargetPicker.defaultLabel"),
      });
    }
    return out.slice(0, 5);
  }, [
    compatible,
    executionLocation,
    scenario,
    recentTick,
    disabledProviderHint,
    disabledModelHint,
    remoteByKey,
    supportsFixedModel,
    remoteFixedHint,
    remoteSyncHint,
    t,
  ]);

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
      // provider-default 的运行语义由 Provider 当前 defaultModelKey 决定；远端目录里的
      // 默认 key 不一致时，即使同名 Provider 已存在也必须重新同步后才能保存。
      const defaultModel = p.defaultModel;
      const defaultSyncNeeded =
        remoteByKey != null &&
        executionLocation !== "" &&
        remoteProvider != null &&
        remoteProvider.defaultModel?.modelKey !== defaultModel?.modelKey;
      out.push({
        key: `default-${p.providerKey}`,
        kind: "provider-default",
        group: groupLabel,
        groupType: p.type,
        label: followDefaultLabel,
        sublabel: defaultModel
          ? t("modelTargetPicker.defaultCurrent", {
              model: defaultModel.modelId,
            })
          : noDefaultModelLabel,
        target: { providerKey: p.providerKey, modelKey: "" },
        disabled: !p.enabled || providerSyncNeeded || defaultSyncNeeded,
        disabledHint:
          providerSyncNeeded || defaultSyncNeeded
            ? remoteSyncHint
            : !p.enabled
              ? disabledProviderHint
              : undefined,
      });
      // fixed-model 列表。
      for (const m of p.models) {
        // 当前默认模型仍需作为 fixed-model 候选保留：provider-default 表达动态跟随，
        // 而选择这里的同一 ModelKey 表达锁定当前模型、以后不随 Provider 默认变化。
        // 远端门控：模型在 daemon 上不存在 / 停用 → 需同步；daemon 不支持
        // fixed-model（旧协议）→ 一律禁用，绝不静默降级。
        const remoteModelOk =
          !remoteProvider ||
          remoteProvider.models.some(
            (rm) => rm.modelKey === m.modelKey && rm.enabled,
          );
        const fixedUnsupported =
          remoteByKey != null &&
          executionLocation !== "" &&
          !supportsFixedModel;
        const fixedSyncNeeded =
          remoteByKey != null &&
          executionLocation !== "" &&
          remoteProvider != null &&
          !remoteModelOk;
        out.push({
          key: `fixed-${p.providerKey}-${m.modelKey}`,
          kind: "fixed",
          group: groupLabel,
          groupType: p.type,
          label: m.name || m.modelId,
          sublabel: m.modelId,
          modelId: m.modelId,
          contextWindow: m.contextWindow,
          maxOutput: m.maxOutput,
          target: { providerKey: p.providerKey, modelKey: m.modelKey },
          disabled:
            !p.enabled ||
            !m.enabled ||
            providerSyncNeeded ||
            fixedUnsupported ||
            fixedSyncNeeded,
          disabledHint: fixedUnsupported
            ? remoteFixedHint
            : providerSyncNeeded || fixedSyncNeeded
              ? remoteSyncHint
              : !p.enabled
                ? disabledProviderHint
                : !m.enabled
                  ? disabledModelHint
                  : undefined,
        });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    compatible,
    executionLocation,
    remoteByKey,
    supportsFixedModel,
    followDefaultLabel,
    noDefaultModelLabel,
    remoteSyncHint,
    remoteFixedHint,
    disabledProviderHint,
    disabledModelHint,
  ]);

  // 失效目标：以禁用项保留在列表顶部（不被清除），目标本身即当前 selected。
  const invalidOption = React.useMemo((): Option | null => {
    if (!invalid || !selected || isNativeTarget(selected)) return null;
    return {
      key: "invalid-selected",
      kind: "invalid",
      label: resolveTargetLabel(selected, catalog),
      target: selected,
      disabled: true,
    };
  }, [invalid, selected, catalog]);

  // 搜索过滤：匹配 provider 名 / model id / 特殊项。chips（最近使用）只在未搜索时显示。
  const flatOptions = React.useMemo(() => {
    // 特殊项（native / inherit）内联到 memo 内，避免每渲染新对象身份成为依赖。
    const specialOption: Option = {
      key: SPECIAL_ITEM_KEY,
      kind: "special",
      label: specialLabel,
      sublabel: specialResolution,
      target: { providerKey: "", modelKey: "" },
      disabled: false,
    };
    const q = search.trim().toLowerCase();
    const base: Option[] = [];
    if (invalidOption) base.push(invalidOption);
    if (q === "") {
      base.push(specialOption);
    } else if (specialLabel.toLowerCase().includes(q)) {
      base.push(specialOption);
    }
    base.push(...catalogOptions);
    if (q === "") return base;
    const match = (o: Option) =>
      (o.label + " " + (o.sublabel ?? "")).toLowerCase().includes(q);
    return base.filter(match);
  }, [search, specialLabel, specialResolution, invalidOption, catalogOptions]);

  // 同步入口落点：每个「本机独有 / 目录过期」的供应商，只在它第一条待修复的行内挂一个
  // 「同步过去」，而不是在列表底部聚合。没有真实同步路由（未传 onSyncProvider）时一个都不挂。
  const syncAnchorByProvider = React.useMemo(() => {
    const anchors = new Map<string, string>();
    if (!onSyncProvider) return anchors;
    for (const o of flatOptions) {
      if (o.disabledHint !== remoteSyncHint) continue;
      if (anchors.has(o.target.providerKey)) continue;
      anchors.set(o.target.providerKey, o.key);
    }
    return anchors;
  }, [flatOptions, onSyncProvider, remoteSyncHint]);

  const showChips = search.trim() === "" && recents.length > 0;

  const handleRemoveRecent = React.useCallback(
    (target: ModelTarget) => {
      removeRecentTarget(scenario, executionLocation, target);
      setRecentTick((x) => x + 1);
    },
    [scenario, executionLocation],
  );

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

  const triggerText = triggerLabel ?? selectedLabel;
  // 主行是纯文本时按钮可以继续靠内容拿无障碍名（既有消费方形态不变）；主行是节点时
  // 内容里有品牌标识（role=img，自带品牌名）和模式徽标，让它们参与名字计算会念出
  // 重复且不稳定的名字 —— 所以名字只认显式字符串：aria-label 优先，其次目录解析结果。
  const isTextTriggerLabel =
    typeof triggerText === "string" || typeof triggerText === "number";
  const triggerAriaLabel =
    ariaLabel ?? (isTextTriggerLabel ? undefined : selectedLabel);

  // 最近使用横条：紧跟在顶部特殊项之后、供应商分组之前（mockup 的列表次序），
  // 自带「最近使用」标签，单行横向可移除 chip，不占竖列表位。
  const recentChipsRow = showChips ? (
    <li role="none" className="px-1 pt-1">
      <div
        data-testid="recent-chips"
        className="flex flex-nowrap items-center gap-1 overflow-x-auto py-0.5"
      >
        <span className="shrink-0 pr-0.5 text-2xs uppercase tracking-wide text-subtle-foreground">
          {t("modelTargetPicker.recentLabel")}
        </span>
        {recents.map((r) => (
          <span
            key={r.key}
            className="flex shrink-0 items-center gap-0.5 rounded-full border border-border bg-secondary/40 px-1.5 py-0.5 text-2xs"
          >
            <button
              type="button"
              disabled={r.disabled}
              title={r.title}
              className="max-w-[10rem] truncate text-foreground disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => {
                if (r.disabled) return;
                onChange(r.target);
                setOpen(false);
              }}
            >
              {r.label}
            </button>
            <button
              type="button"
              aria-label={t("modelTargetPicker.removeRecent", {
                label: r.label,
              })}
              className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={() => handleRemoveRecent(r.target)}
            >
              <X className="size-3" aria-hidden="true" />
            </button>
          </span>
        ))}
      </div>
    </li>
  ) : null;

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          data-testid={testId}
          title={title}
          aria-label={triggerAriaLabel}
          aria-expanded={open}
          aria-haspopup="listbox"
          className={cn(
            "inline-flex w-full items-center justify-between gap-2 rounded-md border border-border bg-background text-xs transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
            "disabled:cursor-not-allowed disabled:opacity-60",
            triggerSub
              ? compact
                ? "min-h-9 px-2 py-1"
                : "min-h-11 px-2.5 py-1.5"
              : compact
                ? "h-7 px-2"
                : "h-9 px-2.5",
            invalid
              ? "border-status-waiting/60 bg-status-waiting-bg"
              : "hover:bg-secondary/60",
            className,
          )}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            {invalid ? (
              <AlertTriangle
                className={cn("size-3.5 shrink-0 text-status-waiting")}
                aria-hidden="true"
              />
            ) : null}
            <span className="flex min-w-0 flex-col items-start gap-0.5">
              <span
                className={cn(
                  "max-w-full",
                  // 节点主行自己是一排图标 + 文字 + 徽标，得撑成 flex 行；纯文本主行
                  // 保持既有的单行截断。
                  isTextTriggerLabel
                    ? "truncate"
                    : "flex min-w-0 items-center gap-1.5",
                  invalid ? "text-status-waiting" : "text-foreground",
                )}
              >
                {triggerText}
              </span>
              {triggerSub ? (
                <span
                  data-testid="model-target-trigger-sub"
                  className="max-w-full truncate text-2xs text-muted-foreground"
                >
                  {triggerSub}
                </span>
              ) : null}
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
        {/* 失效目标顶部警示（弹层内上方，不是底部 footer）。 */}
        {invalid ? (
          <div
            data-testid="invalid-banner"
            className="flex items-start gap-2 border-b border-border bg-status-waiting-bg px-3 py-2 text-2xs text-status-waiting"
          >
            <AlertTriangle
              className="mt-px size-3.5 shrink-0"
              aria-hidden="true"
            />
            <span>
              {t("modelTargetPicker.invalidTarget", {
                target: selected ? resolveTargetLabel(selected, catalog) : "",
              })}
            </span>
          </div>
        ) : null}

        {/* 远端场景：说明以目标设备的配置为准（未传设备名 = 本机，不渲染）。 */}
        {deviceLabel ? (
          <div
            data-testid="remote-device-header"
            className="flex items-center gap-1.5 border-b border-border px-3 py-2 text-2xs text-muted-foreground"
          >
            <Monitor className="size-3.5 shrink-0" aria-hidden="true" />
            <span className="truncate">
              {t("modelTargetPicker.remoteDeviceHeader", {
                device: deviceLabel,
              })}
            </span>
          </div>
        ) : null}

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
          ) : catalog.length === 0 && isNativeTarget(selected) ? (
            <div className="px-2.5 py-3 text-xs text-muted-foreground">
              {t("modelTargetPicker.empty")}
            </div>
          ) : catalog.length > 0 && compatible.length === 0 ? (
            <div className="px-2.5 py-3 text-xs text-muted-foreground">
              {t("modelTargetPicker.noCompatibleProviders")}
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
                const showFixedSection =
                  opt.kind === "fixed" &&
                  (i === 0 ||
                    flatOptions[i - 1].kind !== "fixed" ||
                    flatOptions[i - 1].group !== opt.group);
                // 远端：组头标注该供应商在目标设备上已存在（不代表模型齐全，行内另有原因）。
                const groupOnDevice =
                  remoteGated && remoteByKey.has(opt.target.providerKey);
                // 行内同步入口：只挂在该供应商第一条待同步的行上。
                const syncProvider =
                  syncAnchorByProvider.get(opt.target.providerKey) === opt.key
                    ? compatible.find(
                        (p) => p.providerKey === opt.target.providerKey,
                      )
                    : undefined;
                return (
                  <React.Fragment key={opt.key}>
                    <li role="none">
                      {showGroup ? (
                        <div
                          data-testid="picker-group"
                          data-provider-key={opt.target.providerKey}
                          className="sticky top-0 z-10 flex items-center gap-1.5 bg-background px-2.5 pb-0.5 pt-2 text-2xs font-medium uppercase tracking-wide text-subtle-foreground"
                        >
                          {opt.groupType ? (
                            <LlmProviderLogo
                              providerType={opt.groupType}
                              providerName={opt.group}
                              className="size-3.5"
                            />
                          ) : null}
                          <span>{opt.group}</span>
                          {groupOnDevice ? (
                            <span className="ml-auto rounded-full bg-status-running-bg px-1.5 py-0.5 text-2xs font-medium normal-case tracking-normal text-status-running">
                              {t("modelTargetPicker.providerOnDevice")}
                            </span>
                          ) : null}
                        </div>
                      ) : null}
                      {showFixedSection ? (
                        <div className="px-2.5 pb-0.5 pt-1 text-2xs font-medium text-subtle-foreground">
                          {t("modelTargetPicker.fixedSection")}
                        </div>
                      ) : null}
                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          role="option"
                          aria-selected={selectedNow}
                          aria-disabled={opt.disabled}
                          data-option-index={i}
                          data-kind={opt.kind}
                          disabled={opt.disabled}
                          title={
                            opt.kind === "invalid"
                              ? t("modelTargetPicker.invalidHint")
                              : opt.disabledHint
                          }
                          onMouseEnter={() => setActiveIndex(i)}
                          onClick={() => {
                            if (opt.disabled) return;
                            onChange(opt.target);
                            setOpen(false);
                          }}
                          className={cn(
                            "flex min-w-0 flex-1 items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs transition-colors",
                            "focus-visible:outline-none",
                            // 核心语义分歧：跟随默认（动态）用 primary-soft 强调底，
                            // 固定到具体模型保持中性底，两态一眼可分。
                            opt.kind === "provider-default"
                              ? cn(
                                  "bg-primary-soft",
                                  active && "ring-1 ring-ring/40",
                                )
                              : opt.kind === "invalid"
                                ? "border border-dashed border-status-waiting/60"
                                : active
                                  ? "bg-accent"
                                  : "hover:bg-accent/60",
                            "disabled:cursor-not-allowed disabled:opacity-50",
                          )}
                        >
                          <span className="flex min-w-0 flex-1 items-center gap-2">
                            {opt.kind === "special" ? (
                              <GitBranch
                                className="size-3.5 shrink-0 text-muted-foreground"
                                aria-hidden="true"
                              />
                            ) : opt.kind === "invalid" ? (
                              <AlertTriangle
                                className="size-3.5 shrink-0 text-status-waiting"
                                aria-hidden="true"
                              />
                            ) : opt.kind === "provider-default" ? (
                              <RefreshCw
                                data-testid="provider-default-icon"
                                className="size-3.5 shrink-0 text-primary-text"
                                aria-hidden="true"
                              />
                            ) : opt.kind === "fixed" && opt.modelId ? (
                              <LlmModelLogo
                                model={opt.modelId}
                                providerType={opt.groupType ?? ""}
                                providerName={opt.group}
                                className="size-4"
                              />
                            ) : null}
                            <span className="flex min-w-0 flex-1 flex-col">
                              <span
                                className={cn(
                                  "truncate",
                                  opt.kind === "special"
                                    ? "font-medium text-foreground"
                                    : opt.kind === "provider-default"
                                      ? "font-medium text-primary-text"
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
                              {opt.kind === "invalid" ? (
                                <span className="truncate text-2xs text-status-waiting">
                                  {t("modelTargetPicker.invalidCurrent")}
                                </span>
                              ) : null}
                              {opt.disabled &&
                              opt.disabledHint &&
                              opt.kind !== "invalid" ? (
                                <span className="truncate text-2xs text-status-waiting">
                                  {opt.disabledHint}
                                </span>
                              ) : null}
                            </span>
                            {opt.kind === "invalid" ? (
                              <span className="shrink-0 rounded-full bg-status-waiting-bg px-1.5 py-0.5 text-2xs font-medium text-status-waiting">
                                {t("modelTargetPicker.invalidChip")}
                              </span>
                            ) : null}
                            {opt.kind === "fixed" &&
                            (opt.contextWindow || opt.maxOutput) ? (
                              <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                                {t("modelTargetPicker.contextOutput", {
                                  ctx: formatTokens(opt.contextWindow ?? 0),
                                  out: formatTokens(opt.maxOutput ?? 0),
                                })}
                              </span>
                            ) : null}
                          </span>
                          {selectedNow ? (
                            <Check
                              className="size-3.5 shrink-0 text-primary-text"
                              aria-hidden="true"
                            />
                          ) : null}
                        </button>
                        {/* 同步入口就放在它会修复的那一行内（消费方负责确认与凭证复制）。 */}
                        {syncProvider && onSyncProvider ? (
                          <button
                            type="button"
                            className="inline-flex shrink-0 items-center gap-1 rounded-md border border-border px-1.5 py-1 text-2xs text-primary-text transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                            aria-label={t(
                              "modelTargetPicker.syncProviderNamed",
                              {
                                provider: syncProvider.name,
                              },
                            )}
                            onClick={() => onSyncProvider(syncProvider)}
                          >
                            <Upload className="size-3" aria-hidden="true" />
                            {t("modelTargetPicker.syncInline")}
                          </button>
                        ) : null}
                      </div>
                    </li>
                    {opt.kind === "special" ? recentChipsRow : null}
                  </React.Fragment>
                );
              })}
            </ul>
          ) : null}
        </div>

        {remoteGated && catalogOptions.some((o) => o.disabledHint) ? (
          <div className="flex flex-col gap-1 border-t border-border bg-secondary px-3 py-2 text-2xs text-muted-foreground">
            <span>{t("modelTargetPicker.remoteGateHint")}</span>
            {/* 同步按钮在行内；这里只承诺「同步会复制 API Key、需要明确确认」。 */}
            {onSyncProvider ? (
              <span>{t("modelTargetPicker.syncFootnote")}</span>
            ) : null}
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

export {
  readRecentTargets,
  recordRecentTarget,
  removeRecentTarget,
} from "./recents";
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
