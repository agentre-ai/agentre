import * as React from "react";
import type { TFunction } from "i18next";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  Check,
  Gauge,
  ImagePlus,
  LoaderCircle,
  Pencil,
  SendHorizontal,
  TriangleAlert,
  Wrench,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

import type { PlanActionStream } from "./canonical-tool/props";
import { AIChatInput, type AIChatInputHandle } from "./chat-input";
import { CodeBlock } from "./code-block";
import { CompactHistoryFold } from "./compact-history-fold";
import {
  ChatMessage,
  ErrorCard,
  MessageMeta,
  TranscriptRenderContext,
  TranscriptRowView,
  type TranscriptRenderContextValue,
} from "./transcript-row-view";
import {
  buildTranscriptRows,
  estimateRowSize,
  type TranscriptRow,
} from "./transcript-rows";
import { TranscriptUIStateProvider } from "./transcript-ui-state";
import type { AgentColor, AgentStatus } from "./types";
import { statusConfig } from "./types";
import type { ChatBlockData, RetryNotice } from "@/stores/chat-streams-store";
import { ChatReadDroppedImages } from "../../../wailsjs/go/app/App";
import { chat_svc } from "../../../wailsjs/go/models";
import { resolveDroppedPaths } from "./chat-input/drop";
import { useFileDropZone } from "./chat-input/use-file-drop";

type ToolCallProps = React.ComponentProps<"div"> & {
  path?: string;
  status?: AgentStatus;
  statusLabel: string;
  toolName: string;
};

function ToolCall({
  className,
  path,
  status = "running",
  statusLabel,
  toolName,
  ...props
}: ToolCallProps) {
  const config = statusConfig[status];
  const StatusIcon = status === "waiting" ? LoaderCircle : Check;

  return (
    <div
      data-selectable-text="true"
      className={cn(
        "flex w-full max-w-[720px] flex-col gap-1.5 rounded-md border border-border bg-card px-3 py-2.5",
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs">
        <Wrench className="size-3.5 shrink-0 text-primary-text" />
        <span className="font-semibold text-primary-text">{toolName}</span>
        {path ? (
          <>
            <span className="text-muted-foreground">·</span>
            <span className="min-w-0 truncate text-muted-foreground">
              {path}
            </span>
          </>
        ) : null}
      </div>
      <div className="flex items-center gap-1.5 font-mono text-[11px]">
        <StatusIcon className={cn("size-3", config.textClassName)} />
        <span className={status === "running" ? config.textClassName : ""}>
          {statusLabel}
        </span>
      </div>
    </div>
  );
}

type ApprovalGateProps = React.ComponentProps<"section"> & {
  description: string;
  onApprove?: () => void;
  onReject?: () => void;
  title: string;
};

function ApprovalGate({
  className,
  description,
  onApprove,
  onReject,
  title,
  ...props
}: ApprovalGateProps) {
  const { t } = useTranslation();
  return (
    <section
      className={cn(
        "flex w-full max-w-[720px] items-center gap-3 rounded-lg border border-status-waiting bg-status-waiting-bg px-4 py-3",
        className,
      )}
      {...props}
    >
      <TriangleAlert
        className="size-5 shrink-0 text-status-waiting"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="text-xs font-semibold text-status-waiting">{title}</div>
        <div className="mt-0.5 text-xs leading-snug">{description}</div>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-8"
        onClick={onReject}
      >
        {t("common.reject")}
      </Button>
      <Button
        type="button"
        size="sm"
        className="h-8 bg-status-running text-status-running-foreground hover:bg-status-running/90"
        onClick={onApprove}
      >
        {t("chat.actions.approve")}
      </Button>
    </section>
  );
}

type ChatComposerProps = Omit<React.ComponentProps<"form">, "onSubmit"> & {
  onSubmit?: (message: ChatComposerSubmit) => void;
  placeholder?: string;
  /** 历史消息文本，按时间倒序排列（最新在前），方向键 ↑↓ 浏览。 */
  userMessageHistory?: string[];
  /** 编辑模式：true 时输入框上方挂"正在编辑"提示条，发送按钮文案改为"保存"。 */
  editing?: boolean;
  /** 进入编辑模式时载入到输入框的初始草稿。每次切换编辑目标都会重新载入。 */
  editDraft?: string;
  /** 用户点击提示条上的"取消编辑"。父组件应清掉编辑状态并恢复正常输入。 */
  onCancelEdit?: () => void;
  /** 在输入框上方插入的内容（例：QueuedMessagesBar）。Composer 不关心内容，
   *  只负责把节点放进 card 顶部，跟 editing banner 同位。空节点正常渲染（组件自决可见性）。 */
  topSlot?: React.ReactNode;
  /** 上下文用量。max <= 0 时整块不渲染（未知模型 / 后端未配置）。 */
  contextUsage?: { used: number; max: number };
  /** Claude Code OAuth 5h/7d 配额。undefined / reason='no_credentials' 时整块不渲染。
   *  由 chat-panel 通过 useCCUsage(deviceKey) 拉到后传入。 */
  quotaUsage?: import("../../../wailsjs/go/models").cc_usage_svc.UsageState;
  /** 配额对应的 device 友好名(local 或远端设备名),供 QuotaMeter HoverCard 文案使用。 */
  quotaDeviceLabel?: string;
  /** Permission mode 控件，仅在 claudecode 后端时由 chat-panel 注入。null 时整块不渲染。 */
  permissionModeSlot?: React.ReactNode;
  /** 焦点在 composer 内时按下 Shift+Tab 的钩子（用于循环切换 permission mode）。 */
  onShiftTab?: () => void;
  /** 挂载时自动 focus 输入框。新建会话场景下由 chat-panel 传 true，让用户一打开
   *  就能直接打字、不用再点输入框一次。 */
  autoFocusOnMount?: boolean;
  /** 当前会话 backend 类型;让 AIChatInput 启用 slash menu 并按 backend 过滤候选命令。
   *  空串/省略 → 不启用 slash menu。 */
  backendType?: string;
  /** 当前 backend 是否支持图片输入。false 时不渲染图片附件入口。 */
  supportsImageInput?: boolean;
  /** slash menu rpc 类命令的回调(literal_text 类由 AIChatInput 内部直接填回编辑器,
   *  不自动发送,也不会冒泡到这里)。省略则 slash menu 不启用。 */
  onSlashRpc?: (
    cmd: import("./slash-commands").SlashCommand,
    exec: Extract<import("./slash-commands").SlashExec, { kind: "rpc" }>,
  ) => void;
};

export type ChatImageAttachment = {
  dataUrl: string;
  mediaType: string;
  name: string;
};

export type ChatComposerSubmit = {
  images?: ChatImageAttachment[];
  text: string;
};

const CHAT_IMAGE_ACCEPT = "image/png,image/jpeg,image/webp";
const MAX_CHAT_IMAGE_COUNT = 4;
const MAX_CHAT_IMAGE_BYTES = 5 * 1024 * 1024;

function readImageFile(file: File): Promise<ChatImageAttachment> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.onload = () => {
      if (typeof reader.result !== "string") {
        reject(new Error("invalid image data"));
        return;
      }
      resolve({
        dataUrl: reader.result,
        mediaType: file.type,
        name: file.name,
      });
    };
    reader.readAsDataURL(file);
  });
}

function imageFilesFromClipboard(data: DataTransfer): File[] {
  const itemFiles = Array.from(data.items ?? [])
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => !!file);
  if (itemFiles.length > 0) return itemFiles;
  return Array.from(data.files ?? []).filter((file) =>
    file.type.startsWith("image/"),
  );
}

// 把 token 数显示成 "42.3k / 200k" 这种紧凑形式，跟 inline 底栏的 10px 字号匹配。
// >= 1000 时按 k 缩写并保留 1 位小数；< 1000 时直接显示。
function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const v = n / 1000;
  return v >= 100 ? `${Math.round(v)}k` : `${v.toFixed(1)}k`;
}

// formatResetIn 把"距离 ISO 时间点还有多久"渲染成紧凑的 XdYh / Xh / Xm 形式
// (e.g. "4d21h", "3h", "40m"),用于 QuotaMeter tooltip。
//   - 空串 / 无法解析的输入 → 空串(调用方自己决定是否显示括号)
//   - 已过期(diff<=0)→ "0m"
//   - <1h → "Nm"(向上取整,避免 30s 显示 0m)
//   - <24h → "Nh"(向下取整)
//   - >=24h → "XdYh"(Yh=0 时省略,写 "Xd")
// nowMs 可选(测试注入固定 now);省略走 Date.now()。
export function formatResetIn(value: unknown, nowMs?: number): string {
  if (value == null || value === "") return "";
  const target =
    value instanceof Date ? value.getTime() : Date.parse(String(value));
  if (Number.isNaN(target)) return "";
  const diffMs = target - (nowMs ?? Date.now());
  if (diffMs <= 0) return "0m";
  if (diffMs < 3_600_000) {
    return `${Math.max(1, Math.ceil(diffMs / 60_000))}m`;
  }
  const totalHours = Math.floor(diffMs / 3_600_000);
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  if (days <= 0) return `${hours}h`;
  if (hours === 0) return `${days}d`;
  return `${days}d${hours}h`;
}

// QuotaMeter 展示 Claude Code 订阅的 5h / 7d 配额。数据由 chat-panel 通过 useCCUsage
// 拉取并传入(per-device, 不在这里订阅 store, 保证 Composer 可被纯 props 测试)。
//
// 渲染策略(与 cc_usage_svc.UsageState.reason 对齐):
//   - undefined / 空 reason / "no_credentials" → 整块不渲染(API key 用户、未首探)
//   - "ok" / "rate_limited"+stale / "network"+stale → 5h X% · 7d Y%(stale 不可见标记,只在 tooltip 文案里提示)
//   - "auth_expired" / "device_offline" / "network"无stale → 灰态占位 "5h —%"
function QuotaMeter({
  data,
  deviceLabel,
}: {
  data?: import("../../../wailsjs/go/models").cc_usage_svc.UsageState;
  deviceLabel?: string;
}) {
  const { t } = useTranslation();
  if (!data || !data.reason) return null;
  if (data.reason === "no_credentials") return null;

  const showNumbers = data.data && (data.reason === "ok" || !!data.stale);
  const fiveH = data.data ? Math.round(data.data.fiveHourPercent) : null;
  const sevenD = data.data ? Math.round(data.data.weeklyPercent) : null;

  // 阈值色:超 90% 红, 超 75% 黄, 其余正常。两个窗口取较高的那个驱动颜色。
  const peak =
    fiveH !== null && sevenD !== null ? Math.max(fiveH, sevenD) : (fiveH ?? 0);
  const tone =
    peak >= 90
      ? "text-status-error"
      : peak >= 75
        ? "text-status-waiting"
        : "text-muted-foreground";

  const offline =
    data.reason === "auth_expired" || data.reason === "device_offline";

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 font-mono text-[10px] tabular-nums",
        offline ? "text-subtle-foreground" : tone,
      )}
      aria-label={t("chat.quota.aria", {
        device: deviceLabel || "local",
        five: fiveH ?? "—",
        seven: sevenD ?? "—",
      })}
      title={describeQuotaTitle(data, deviceLabel, t)}
    >
      <Gauge className="size-2.5" aria-hidden="true" />
      <span>5h {showNumbers && fiveH !== null ? `${fiveH}%` : "—%"}</span>
      <span className="text-subtle-foreground">·</span>
      <span>7d {showNumbers && sevenD !== null ? `${sevenD}%` : "—%"}</span>
    </div>
  );
}

// describeQuotaTitle 给 HoverCard / native tooltip 提供"完整文案"。
// 不引入 HoverCard 组件以避免 Composer 引入复杂 Popover 状态;native title
// 已经够透露 reset 时间 + sonnet/opus 拆分这种次要信息。
function describeQuotaTitle(
  data: import("../../../wailsjs/go/models").cc_usage_svc.UsageState,
  deviceLabel: string | undefined,
  t: TFunction,
): string {
  const lines: string[] = [];
  const device = deviceLabel || "local";
  switch (data.reason) {
    case "ok":
      lines.push(t("chat.quota.title.ok", { device }));
      break;
    case "rate_limited":
      lines.push(t("chat.quota.title.rateLimited", { device }));
      break;
    case "network":
      lines.push(t("chat.quota.title.network", { device }));
      break;
    case "auth_expired":
      lines.push(t("chat.quota.title.authExpired", { device }));
      break;
    case "device_offline":
      lines.push(t("chat.quota.title.deviceOffline", { device }));
      break;
    default:
      lines.push(t("chat.quota.title.ok", { device }));
  }
  if (data.data) {
    const fiveIn = formatResetIn(data.data.fiveHourResetsAt);
    const sevenIn = formatResetIn(data.data.weeklyResetsAt);
    const five = fiveIn ? t("chat.quota.resetRemaining", { time: fiveIn }) : "";
    const seven = sevenIn
      ? t("chat.quota.resetRemaining", { time: sevenIn })
      : "";
    lines.push(`5h: ${Math.round(data.data.fiveHourPercent)}%${five}`);
    lines.push(`7d: ${Math.round(data.data.weeklyPercent)}%${seven}`);
    if (data.data.sonnetWeeklyPercent != null) {
      lines.push(`  Sonnet 7d: ${Math.round(data.data.sonnetWeeklyPercent)}%`);
    }
    if (data.data.opusWeeklyPercent != null) {
      lines.push(`  Opus 7d: ${Math.round(data.data.opusWeeklyPercent)}%`);
    }
  }
  return lines.join("\n");
}

function ContextMeter({ used, max }: { used: number; max: number }) {
  const { t } = useTranslation();
  const safeUsed = Math.max(0, used);
  const ratio = max > 0 ? Math.min(1, safeUsed / max) : 0;
  const pct = Math.round(ratio * 100);
  // 阈值色：>90% 触红，>75% 触黄，其余 primary。沿用 status-* / primary token，
  // 暗色模式下走 @theme 的 mapping，不用手 toggle。
  const warn = ratio >= 0.9 ? "error" : ratio >= 0.75 ? "warning" : "ok";
  const tone =
    warn === "error"
      ? "text-status-error"
      : warn === "warning"
        ? "text-status-waiting"
        : "text-primary-text";
  const fill =
    warn === "error"
      ? "bg-status-error"
      : warn === "warning"
        ? "bg-status-waiting"
        : "bg-primary";
  return (
    <div
      className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground"
      aria-label={t("chat.context.aria", { max, used: safeUsed })}
    >
      <Gauge className="size-2.5" aria-hidden="true" />
      <span className="font-sans">{t("chat.context.label")}</span>
      <span className="inline-flex items-center gap-0.5 tabular-nums">
        <span className="font-medium text-foreground">
          {formatTokens(safeUsed)}
        </span>
        <span className="text-subtle-foreground"> / </span>
        <span>{formatTokens(max)}</span>
      </span>
      <span
        className="h-1 w-24 overflow-hidden rounded-sm bg-border"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={Math.min(safeUsed, max)}
      >
        <span
          className={cn("block h-1 rounded-sm transition-[width]", fill)}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className={cn("font-medium tabular-nums", tone)}>{pct}%</span>
    </div>
  );
}

function ChatComposer({
  className,
  onSubmit,
  placeholder,
  userMessageHistory,
  editing = false,
  editDraft,
  onCancelEdit,
  topSlot,
  contextUsage,
  quotaUsage,
  quotaDeviceLabel,
  permissionModeSlot,
  onShiftTab,
  autoFocusOnMount = false,
  backendType,
  supportsImageInput = true,
  onSlashRpc,
  onPasteCapture,
  ...props
}: ChatComposerProps) {
  const { t } = useTranslation();
  const inputRef = React.useRef<AIChatInputHandle>(null);
  const fileInputRef = React.useRef<HTMLInputElement>(null);
  const [isEmpty, setIsEmpty] = React.useState(true);
  const [images, setImages] = React.useState<ChatImageAttachment[]>([]);
  const [imageError, setImageError] = React.useState("");

  // 切换到编辑模式（或换了编辑目标）时把目标文本载进 TipTap，并把光标抓回输入框；
  // 退出编辑模式时清空输入，免得上一次的编辑残留干扰下一条新消息。
  const wasEditingRef = React.useRef(false);
  React.useEffect(() => {
    if (editing) {
      if (editDraft !== undefined) {
        inputRef.current?.loadDraft(editDraft);
      }
      inputRef.current?.focus();
    } else if (wasEditingRef.current) {
      inputRef.current?.clear();
    }
    if (editing) {
      setImages([]);
      setImageError("");
    }
    wasEditingRef.current = editing;
  }, [editing, editDraft]);

  const wasAutoFocusOnMountRef = React.useRef(autoFocusOnMount);
  React.useEffect(() => {
    const wasAutoFocusOnMount = wasAutoFocusOnMountRef.current;
    wasAutoFocusOnMountRef.current = autoFocusOnMount;
    if (!autoFocusOnMount || wasAutoFocusOnMount) return;
    inputRef.current?.focus();
  }, [autoFocusOnMount]);

  React.useEffect(() => {
    if (supportsImageInput) return;
    setImages([]);
    setImageError("");
    if (fileInputRef.current) fileInputRef.current.value = "";
  }, [supportsImageInput]);

  const dropRef = React.useRef<HTMLFormElement>(null);

  const handleDroppedPaths = React.useCallback(
    (paths: string[]) => {
      void (async () => {
        const { attachments, text } = await resolveDroppedPaths(paths, {
          allowImages: !editing && supportsImageInput,
          remainingImageSlots: MAX_CHAT_IMAGE_COUNT - images.length,
          readImages: async (imagePaths) => {
            const resp = await ChatReadDroppedImages(
              chat_svc.ReadDroppedImagesRequest.createFrom({
                paths: imagePaths,
              }),
            );
            return (resp.items ?? []).map((it) => ({
              path: it.path,
              kind:
                it.kind === "image" ? ("image" as const) : ("path" as const),
              name: it.name,
              mediaType: it.mediaType,
              dataUrl: it.dataUrl,
            }));
          },
        });
        if (attachments.length > 0) {
          setImages((prev) => [...prev, ...attachments]);
          setImageError("");
        }
        if (text) inputRef.current?.insertText(text);
      })();
    },
    [editing, supportsImageInput, images.length],
  );

  const { isDragOver } = useFileDropZone({
    ref: dropRef,
    enabled: true,
    onPaths: handleDroppedPaths,
  });

  function handleSend(text: string) {
    const trimmed = text.trim();
    if (!trimmed && images.length === 0) return;
    onSubmit?.(
      images.length > 0 ? { images, text: trimmed } : { text: trimmed },
    );
    setImages([]);
    setImageError("");
  }

  function handleFormSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isEmpty && images.length > 0) {
      handleSend("");
      return;
    }
    inputRef.current?.submit();
  }

  async function handleImageFiles(files: FileList | readonly File[] | null) {
    try {
      if (!files || files.length === 0) return;
      const nextFiles = Array.from(files);
      if (images.length + nextFiles.length > MAX_CHAT_IMAGE_COUNT) {
        setImageError(
          t("chat.composer.imageErrors.tooMany", {
            count: MAX_CHAT_IMAGE_COUNT,
          }),
        );
        return;
      }
      const bad = nextFiles.find(
        (file) =>
          !CHAT_IMAGE_ACCEPT.split(",").includes(file.type) ||
          file.size > MAX_CHAT_IMAGE_BYTES,
      );
      if (bad) {
        setImageError(t("chat.composer.imageErrors.unsupported"));
        return;
      }
      const attachments = await Promise.all(nextFiles.map(readImageFile));
      setImages((prev) => [...prev, ...attachments]);
      setImageError("");
    } catch {
      setImageError(t("chat.composer.imageErrors.readFailed"));
    } finally {
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  }

  function handlePasteCapture(event: React.ClipboardEvent<HTMLFormElement>) {
    onPasteCapture?.(event);
    if (event.defaultPrevented || editing || !supportsImageInput) return;
    const pastedImages = imageFilesFromClipboard(event.clipboardData);
    if (pastedImages.length === 0) return;
    event.preventDefault();
    void handleImageFiles(pastedImages);
  }

  // Esc 取消编辑。TipTap 的 handleKeyDown 不处理 Esc，所以这里在 form 层捕获；
  // 非编辑态下不消费，让默认行为走。
  //
  // Shift+Tab 循环切换 permission mode —— 对齐 Claude TUI；focus 在 composer 内
  // （TipTap editor / 按钮）时都会冒泡到 form，preventDefault 拦掉默认 tab 切换。
  // 编辑模式下不消费 Shift+Tab，让无障碍 tab 反向焦点正常工作。
  function handleFormKeyDown(event: React.KeyboardEvent<HTMLFormElement>) {
    if (editing && event.key === "Escape") {
      event.preventDefault();
      onCancelEdit?.();
      return;
    }
    if (
      !editing &&
      onShiftTab &&
      event.key === "Tab" &&
      event.shiftKey &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.altKey
    ) {
      event.preventDefault();
      onShiftTab();
    }
  }

  return (
    <form
      ref={dropRef}
      className={cn(
        "relative w-full border-t border-border bg-background px-5 py-3.5",
        className,
      )}
      onSubmit={handleFormSubmit}
      onKeyDown={handleFormKeyDown}
      onPasteCapture={handlePasteCapture}
      {...props}
    >
      {isDragOver ? (
        <div
          className="pointer-events-none absolute inset-2 z-10 flex items-center justify-center rounded-md border-2 border-dashed border-ring bg-background/85 text-sm font-medium text-foreground"
          aria-hidden="true"
        >
          {t("chat.composer.dropHint")}
        </div>
      ) : null}
      <div
        className={cn(
          "flex w-full flex-col overflow-hidden rounded-md border bg-card shadow-xs transition-colors",
          "focus-within:ring-[3px] focus-within:ring-ring/50",
          editing
            ? "border-primary-text/45 focus-within:border-primary-text/70"
            : "border-border focus-within:border-ring",
        )}
      >
        {topSlot}
        {editing ? (
          <div
            role="status"
            aria-label={t("chat.composer.editing.aria")}
            className="flex items-center gap-2 border-b border-primary-text/20 bg-primary-soft px-3 py-1.5 text-[11px]"
          >
            <Pencil
              className="size-3 shrink-0 text-primary-text"
              aria-hidden="true"
            />
            <span className="font-semibold text-primary-text">
              {t("chat.composer.editing.title")}
            </span>
            <span className="text-muted-foreground">·</span>
            <span className="min-w-0 flex-1 truncate text-muted-foreground">
              {t("chat.composer.editing.description")}
            </span>
            <button
              type="button"
              aria-label={t("chat.composer.editing.cancel")}
              title={t("chat.composer.editing.cancelTitle")}
              className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              onClick={() => onCancelEdit?.()}
            >
              <X className="size-3" aria-hidden="true" />
            </button>
          </div>
        ) : null}
        <div className="flex flex-col gap-1 px-3.5 pt-2.5 pb-1">
          {!editing && images.length > 0 ? (
            <div className="flex flex-wrap gap-2 pb-1">
              {images.map((img, idx) => (
                <div
                  key={`${img.name}-${idx}`}
                  className="group relative h-16 w-20 overflow-hidden rounded-md border border-border bg-muted"
                >
                  <img
                    src={img.dataUrl}
                    alt={img.name || t("chat.image.attachmentAlt")}
                    className="h-full w-full object-cover"
                  />
                  <button
                    type="button"
                    aria-label={t("chat.composer.removeImage", {
                      name: img.name || idx + 1,
                    })}
                    className="absolute top-1 right-1 inline-flex size-5 items-center justify-center rounded-sm bg-background/90 text-muted-foreground opacity-0 shadow-sm transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                    onClick={() => {
                      setImages((prev) => prev.filter((_, i) => i !== idx));
                      setImageError("");
                    }}
                  >
                    <X className="size-3" aria-hidden="true" />
                  </button>
                </div>
              ))}
            </div>
          ) : null}
          <AIChatInput
            ref={inputRef}
            onSubmit={handleSend}
            onEmptyChange={setIsEmpty}
            sendOnEnter
            userMessageHistory={userMessageHistory}
            placeholder={placeholder ?? t("chat.composer.placeholder")}
            autoFocus={autoFocusOnMount}
            backendType={backendType}
            onSlashSelect={(cmd, exec) => {
              // literal_text 由 AIChatInput 内部直接填回编辑器(不自动发送),
              // 这里只接 rpc 类命令转交给父组件。
              if (exec.kind === "rpc") onSlashRpc?.(cmd, exec);
            }}
          />
          {imageError ? (
            <div className="text-[11px] text-status-error" role="alert">
              {imageError}
            </div>
          ) : null}
          <div className="flex items-center gap-2">
            {!editing && supportsImageInput ? (
              <>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept={CHAT_IMAGE_ACCEPT}
                  multiple
                  className="hidden"
                  onChange={(event) =>
                    void handleImageFiles(event.target.files)
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t("chat.composer.addImage")}
                  title={t("chat.composer.addImage")}
                  disabled={images.length >= MAX_CHAT_IMAGE_COUNT}
                  onClick={() => fileInputRef.current?.click()}
                >
                  <ImagePlus data-icon="only" aria-hidden="true" />
                </Button>
              </>
            ) : null}
            <span className="font-mono text-[10px] leading-none text-subtle-foreground">
              {editing
                ? t("chat.composer.shortcuts.edit")
                : t("chat.composer.shortcuts.send")}
            </span>
            {!editing && permissionModeSlot ? (
              <div className="flex items-center">{permissionModeSlot}</div>
            ) : null}
            <div className="min-w-0 flex-1" />
            {!editing ? (
              <QuotaMeter data={quotaUsage} deviceLabel={quotaDeviceLabel} />
            ) : null}
            {contextUsage && contextUsage.max > 0 && !editing ? (
              <ContextMeter used={contextUsage.used} max={contextUsage.max} />
            ) : null}
            {editing ? (
              <Button
                type="submit"
                disabled={isEmpty}
                size="xs"
                aria-label={t("chat.composer.saveEdit")}
              >
                <Check data-icon="inline-start" aria-hidden="true" />
                {t("common.save")}
              </Button>
            ) : (
              <Button
                type="submit"
                disabled={isEmpty && images.length === 0}
                size="icon-sm"
                aria-label={t("chat.composer.send")}
                title={t("chat.composer.sendTitle")}
              >
                <SendHorizontal data-icon="only" aria-hidden="true" />
              </Button>
            )}
          </div>
        </div>
      </div>
    </form>
  );
}

// Generic tool card extension point: canonical-tool/raw/card.tsx handles
// non-canonical tools; canonical-tool/<kind>/card.tsx handles canonical kinds.

// ─── ChatTranscript ──────────────────────────────────────────────────────────

// 距底 ≤32px 视为"贴底",与 chat-panel 的 TRANSCRIPT_BOTTOM_THRESHOLD 同义:
// 它是 anchorTo:"end" 在 live 行流式增长时"是否继续钉底"的容差;
// 用户上滑超过它就不再钉底,保住阅读历史的位置。
const STICK_TO_BOTTOM_THRESHOLD_PX = 32;
const TRANSCRIPT_VIRTUAL_OVERSCAN = 6;

type ChatTranscriptProps = {
  agentName: string;
  agentColor: AgentColor;
  /** 会话的工作目录，用于工具卡片把 cwd 内路径展示为相对路径。 */
  cwd?: string;
  /** 当前 chat session id —— AskUserQuestionCard 提交答案时要带它去 Wails 绑定。 */
  sessionId?: number;
  /** Transcript 的滚动容器。传入时启用动态高度虚拟列表。 */
  scrollElement?: HTMLElement | null;
  /** scrollElement 挂上前也保持虚拟化路径，避免长对话首帧全量 mount。 */
  virtualize?: boolean;
  /** 当前 tab 是否可见；从隐藏切回时触发虚拟列表重新测量。 */
  active?: boolean;
  messages: chat_svc.ChatMessage[];
  liveDelta?: string;
  /** 流式中累积的 thinking 增量。挂在 liveTargetId 对应的 assistant 上作为一张 streaming thinking 卡。 */
  liveThinking?: string;
  liveTargetId?: number | null;
  /**
   * 本轮 turn 已冻结但还没持久化的 blocks（text/tool_use/tool_result），跨路由
   * 由 chat-streams-store 维护。摆在 liveTargetId 对应的 assistant 的 persisted
   * blocks 之后、liveDelta 之前，整体顺序与真实流入顺序一致。
   */
  liveBlocks?: ChatBlockData[];
  liveRetry?: RetryNotice | null;
  /** 当前 stream 的开始时间，供合成 thinking 块计时使用。 */
  liveStreamStartedAt?: number | null;
  /** 用户点某条 assistant 上的「重新生成」时回调，参数是目标 assistant 的消息 id。 */
  onRerun?: (messageId: number) => void;
  /** 用户点某条 user 消息上的「编辑」时回调，参数是 user 消息 id。 */
  onEdit?: (messageId: number) => void;
  /** stream 是否进行中。true 时在末尾 assistant 内挂 typing 指示器，覆盖首 chunk 前 / 工具返回后的空窗期。 */
  streaming?: boolean;
  /** claudecode CLI 正在跑 /compact 时为 true;末尾 assistant 的 typing indicator 替换为
   *  "正在压缩上下文…" chip,让用户知道这段时间在做什么。compact_boundary 到达自动清空。*/
  liveCompacting?: boolean;
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void;
  /** Stable mounted chat tab key for UI drafts that survive route/tab remounts. */
  tabStateKey?: string;
};

type ChatTranscriptHandle = {
  scrollToMessage: (messageId: number) => void;
  // 锚点恢复:把锚点行钉到距视口顶 offset px 处,并随虚拟器逐行复测收敛。
  // rowKey(data-row-key)命中时精确钉回该行 —— 行级虚拟化下长消息拆成多行,
  // 只按 messageId 会塌到消息首行;rowKey 失效(行已消失/旧快照)回退消息首行。
  // 返回 false 表示该消息当前不在 displayMessages(被折叠 / 尚未加载),
  // 调用方应回退到像素恢复。
  scrollToAnchor: (
    messageId: number,
    offset: number,
    rowKey?: string,
  ) => boolean;
};

// findLastCompactBoundary 顺序扫所有 messages.blocks 找最后一条 type=compact_boundary
// 的位置;没找到返回 null。返回 (messageIdx, blockIdx) 让 ChatTranscript 知道从哪里
// 起算"压缩后"显示段:messages[messageIdx].blocks[blockIdx] 即 boundary 块本身。
function findLastCompactBoundary(
  messages: chat_svc.ChatMessage[],
): { messageIdx: number; blockIdx: number } | null {
  let found: { messageIdx: number; blockIdx: number } | null = null;
  messages.forEach((m, i) => {
    (m.blocks ?? []).forEach((b, j) => {
      if (b.type === "compact_boundary") {
        found = { messageIdx: i, blockIdx: j };
      }
    });
  });
  return found;
}

const ChatTranscript = React.forwardRef<
  ChatTranscriptHandle,
  ChatTranscriptProps
>(function ChatTranscript(
  {
    agentName,
    agentColor,
    cwd,
    sessionId,
    scrollElement,
    virtualize = false,
    active = true,
    messages,
    liveDelta,
    liveThinking,
    liveTargetId,
    liveBlocks,
    liveRetry,
    liveStreamStartedAt,
    onRerun,
    onEdit,
    onPlanActionStarted,
    tabStateKey,
    streaming = false,
    liveCompacting = false,
  },
  ref,
) {
  // 只有当整个序列的「最后一条」就是 assistant 时才挂指示器：
  // 正常 stream 流程下 doSend 会同时插入 user + assistant 占位，所以末尾一定是 assistant；
  // 如果末尾是 user（异常态或还没插占位），不应该在更早的 assistant 上挂指示器误导用户。
  const tail = messages.at(-1);
  const lastAssistantId =
    tail && tail.role === "assistant" ? tail.id : undefined;

  // 折叠"压缩前"的旧消息:扫所有 messages.blocks,找最后一条 compact_boundary 所在的
  // (messageIdx, blockIdx);该位置之前的所有消息默认隐藏,该消息自己的更早 blocks 也
  // 一并裁掉。expanded=true 时退化为原始 messages 渲染。
  const [expanded, setExpanded] = React.useState(false);
  const fold = React.useMemo(
    () => findLastCompactBoundary(messages),
    [messages],
  );
  const folding = !expanded && fold !== null;
  const foldedCount = folding ? fold.messageIdx : 0;
  const displayMessages = React.useMemo<chat_svc.ChatMessage[]>(() => {
    if (!folding) return messages;
    // 保留 boundary 所在消息及之后;boundary 消息的更早 blocks 裁掉。spread 出来的对象
    // 失去了 wails class 的 convertValues 方法,但下游 MessageItem 只读字段,as 强转
    // 即可,不需要走 Object.assign(Object.create(proto)) 这种重活。
    const out: chat_svc.ChatMessage[] = [];
    for (let i = fold.messageIdx; i < messages.length; i++) {
      if (i === fold.messageIdx && fold.blockIdx > 0) {
        const m = messages[i];
        out.push({
          ...m,
          blocks: (m.blocks ?? []).slice(fold.blockIdx),
        } as chat_svc.ChatMessage);
      } else {
        out.push(messages[i]);
      }
    }
    return out;
  }, [folding, messages, fold]);

  // autonomousIds:自主续轮(CLI 后台任务完成后自主跑的一轮)的消息 id 集合。
  // 判定:assistant 轮且其在**完整 messages**里紧邻的前一条不是 user —— 正常轮是
  // user→assistant、auto-continue / steer 也是 user→assistant,只有自主续轮是
  // assistant→assistant(无 user 行)。用完整 messages(而非 displayMessages)算,
  // 避免 compact 折叠把首条 assistant 误判成自主轮。会话首条(i===0)永不算。
  const autonomousIds = React.useMemo(() => {
    const ids = new Set<number>();
    for (let i = 1; i < messages.length; i++) {
      if (messages[i].role === "assistant" && messages[i - 1].role !== "user") {
        ids.add(messages[i].id);
      }
    }
    return ids;
  }, [messages]);

  // useEvent 模式：把 onRerun/onEdit/onPlanActionStarted 包成稳定引用,让行组件的
  // React.memo / TranscriptRenderContext 不会被 ChatPanel 传入的 inline lambda 击穿。
  // 父侧每次重渲都换新函数,但 ref 内部更新后稳定代理捕获最新值,语义不变。
  const onRerunRef = React.useRef(onRerun);
  const onEditRef = React.useRef(onEdit);
  const onPlanActionStartedRef = React.useRef(onPlanActionStarted);
  React.useEffect(() => {
    onRerunRef.current = onRerun;
    onEditRef.current = onEdit;
    onPlanActionStartedRef.current = onPlanActionStarted;
  });
  const stableOnRerun = React.useCallback((id: number) => {
    onRerunRef.current?.(id);
  }, []);
  const stableOnEdit = React.useCallback((id: number) => {
    onEditRef.current?.(id);
  }, []);
  const stableOnPlanActionStarted = React.useCallback(
    (stream: PlanActionStream, userText: string) => {
      onPlanActionStartedRef.current?.(stream, userText);
    },
    [],
  );

  // displayMessages → 虚拟行。persisted 消息的行缓存在实例级 WeakMap(引用稳定
  // → 行组件 memo 恒命中);live 消息每 chunk 现场重建,重渲上限 = 可见窗口行数。
  const rowsCacheRef = React.useRef(
    new WeakMap<chat_svc.ChatMessage, TranscriptRow[]>(),
  );
  const { rows, firstRowIndexByMessageId, rowIndexByKey } = React.useMemo(
    () =>
      buildTranscriptRows({
        autonomousIds,
        cache: rowsCacheRef.current,
        displayMessages,
        liveBlocks,
        liveTail: liveDelta ?? "",
        liveTargetId,
        liveThinking: liveThinking ?? "",
        liveThinkingStartedAt: liveStreamStartedAt,
      }),
    [
      autonomousIds,
      displayMessages,
      liveBlocks,
      liveDelta,
      liveStreamStartedAt,
      liveTargetId,
      liveThinking,
    ],
  );

  const renderCtx = React.useMemo<TranscriptRenderContextValue>(
    () => ({
      agentColor,
      agentName,
      cwd,
      onEdit: stableOnEdit,
      onPlanActionStarted: stableOnPlanActionStarted,
      onRerun: stableOnRerun,
      sessionId: sessionId ?? 0,
      tabStateKey,
    }),
    [
      agentColor,
      agentName,
      cwd,
      sessionId,
      stableOnEdit,
      stableOnPlanActionStarted,
      stableOnRerun,
      tabStateKey,
    ],
  );

  const shouldVirtualize = virtualize || scrollElement != null;
  const lastVirtualTotalSizeRef = React.useRef(0);
  const lastScrollRectRef = React.useRef({ height: 0, width: 0 });
  const lastScrollOffsetRef = React.useRef(0);
  const restoreScrollOffsetRef = React.useRef(false);
  const [, forceRestoreRender] = React.useState(0);

  const observeScrollRect = React.useCallback(
    (
      el: HTMLElement | null,
      cb: (rect: { height: number; width: number }) => void,
    ) => {
      const next = {
        height: el?.clientHeight ?? 0,
        width: el?.clientWidth ?? 0,
      };
      if (next.height > 0 || next.width > 0) {
        lastScrollRectRef.current = next;
        cb(next);
        return;
      }
      cb(active ? next : lastScrollRectRef.current);
    },
    [active],
  );

  // renderRowView:单个虚拟行的内容。非 live 行的 live* prop 全部收敛到稳定空值,
  // 让 TranscriptRowView 的 React.memo shallow 比较恒命中 —— 每个流式 chunk 只有
  // live 消息(和指示器宿主末行)重渲。
  const renderRowView = React.useCallback(
    (row: TranscriptRow) => {
      const isLiveTail =
        row.isLastOfMessage &&
        liveTargetId != null &&
        row.messageId === liveTargetId;
      const showIndicator =
        row.isLastOfMessage &&
        streaming &&
        lastAssistantId != null &&
        row.messageId === lastAssistantId;
      return (
        <TranscriptRowView
          row={row}
          liveTail={isLiveTail ? (liveDelta ?? "") : ""}
          liveBlocks={isLiveTail ? liveBlocks : undefined}
          liveRetry={isLiveTail ? (liveRetry ?? null) : null}
          showIndicator={showIndicator}
          compacting={showIndicator && isLiveTail && liveCompacting}
        />
      );
    },
    [
      lastAssistantId,
      liveBlocks,
      liveCompacting,
      liveDelta,
      liveRetry,
      liveTargetId,
      streaming,
    ],
  );

  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual intentionally owns mutable measurement callbacks.
  const virtualizer = useVirtualizer({
    count: rows.length,
    estimateSize: (index) => {
      const row = rows[index];
      return row ? estimateRowSize(row) : 132;
    },
    getItemKey: (index) => rows[index]?.key ?? index,
    getScrollElement: () => scrollElement ?? null,
    initialRect: {
      height: scrollElement?.clientHeight ?? 0,
      width: scrollElement?.clientWidth ?? 0,
    },
    observeElementOffset: (_instance, cb) => {
      const el = scrollElement;
      const readOffset = () => {
        const offset = el?.scrollTop ?? 0;
        if (active && !restoreScrollOffsetRef.current) {
          lastScrollOffsetRef.current = offset;
          return offset;
        }
        if (offset > 0) {
          lastScrollOffsetRef.current = offset;
          return offset;
        }
        return lastScrollOffsetRef.current;
      };
      cb(readOffset(), false);
      if (!el) return;
      let scrollEndTimer: number | null = null;
      const handler = () => {
        const offset = readOffset();
        cb(offset, true);
        if (scrollEndTimer != null) window.clearTimeout(scrollEndTimer);
        scrollEndTimer = window.setTimeout(() => {
          cb(readOffset(), false);
          scrollEndTimer = null;
        }, 150);
      };
      el.addEventListener("scroll", handler, { passive: true });
      return () => {
        if (scrollEndTimer != null) window.clearTimeout(scrollEndTimer);
        el.removeEventListener("scroll", handler);
      };
    },
    observeElementRect: (_instance, cb) => {
      const el = scrollElement ?? null;
      observeScrollRect(el, cb);
      if (!el || typeof ResizeObserver === "undefined") return;
      const observer = new ResizeObserver(() => {
        observeScrollRect(el, cb);
      });
      observer.observe(el);
      return () => observer.disconnect();
    },
    overscan: TRANSCRIPT_VIRTUAL_OVERSCAN,
    // 流式贴底交给虚拟器自己的测量回路,而不是 chat-panel 在每个 chunk 用
    // scrollTop=maxScrollTop 手动追(那条路读的是异步复测前的旧 getTotalSize,
    // 永远慢一帧→最新输出被压到折叠线以下)。anchorTo:"end" 在 live 行被
    // ResizeObserver 复测变高、且当前距底 ≤ 阈值时,于 resizeItem 测量回路内
    // 同步把滚动钉回底部,天然消除"慢一帧";上滑超过阈值则不钉,保住阅读位置。
    //
    // 刻意不开 followOnAppend:追"新追加整条消息"已由 chat-panel 的结构性 follow
    //(atBottom 时随 messages 变化滚到底)覆盖;而 followOnAppend 会在会话打开、
    // messages 从 0→N 时把空列表判定为"在末尾"抢先 scrollToEnd,覆盖掉
    //「恢复到上次上滑位置」的还原(正是要修的 wrong-restore),故不启用。
    anchorTo: "end",
    scrollEndThreshold: STICK_TO_BOTTOM_THRESHOLD_PX,
  });
  React.useLayoutEffect(() => {
    if (!scrollElement) return;
    virtualizer.measure();
  }, [scrollElement, virtualizer]);
  React.useLayoutEffect(() => {
    if (!active) {
      restoreScrollOffsetRef.current = true;
      return;
    }
    if (!restoreScrollOffsetRef.current) return;
    restoreScrollOffsetRef.current = false;
    const el = scrollElement;
    if (!el) return;
    const offset = lastScrollOffsetRef.current;
    if (el.scrollTop !== offset) el.scrollTop = offset;
    el.dispatchEvent(new Event("scroll"));
    forceRestoreRender((version) => version + 1);
  }, [active, scrollElement, virtualizer]);
  // 注意:这里不能在 active 翻成 true 时再调 virtualizer.measure()。
  // measure() 会 itemSizeCache.clear() 把所有行的真实测量值丢弃、整列瞬间塌回
  // estimateSize(132px),切回 tab 时引发可见的塌缩 / 闪烁 reflow。隐藏期间行的
  // ResizeObserver 不触发(display:none 不参与布局),重新可见时 measureElement 的
  // ResizeObserver 会自然对可见窗口逐行复测,无需整列清缓存。

  // 行级贴底跟随:anchorTo:"end" 只在「行 resize」时钉底(流式文本生长走那条路),
  // 而行模型下新 tool 卡 / indicator 是「行追加」—— followOnAppend 因 wrong-restore
  // (见 virtualizer 配置注释)刻意不开,这里自己补:仅当 ①tab 可见且不在恢复期
  // ②非首载(0→N 是打开会话回放,要让位给滚动恢复)③确实是尾部追加 ④追加前
  // 用户贴底(按追加前的 totalSize 判定)时,把滚动钉到新的末尾。
  const followTailRef = React.useRef({
    count: 0,
    tailKey: null as string | null,
    totalSize: 0,
  });
  React.useLayoutEffect(() => {
    const el = scrollElement;
    const prev = followTailRef.current;
    const tailKey = rows.at(-1)?.key ?? null;
    followTailRef.current = {
      count: rows.length,
      tailKey,
      totalSize: virtualizer.getTotalSize(),
    };
    if (!el || !active || restoreScrollOffsetRef.current) return;
    if (prev.count === 0) return;
    if (rows.length <= prev.count || tailKey === prev.tailKey) return;
    const wasAtEnd =
      prev.totalSize <= el.clientHeight ||
      el.scrollTop + el.clientHeight >=
        prev.totalSize - STICK_TO_BOTTOM_THRESHOLD_PX;
    if (!wasAtEnd) return;
    virtualizer.scrollToOffset(virtualizer.getTotalSize(), { align: "end" });
  }, [rows, active, scrollElement, virtualizer]);

  const [pendingScrollMessageId, setPendingScrollMessageId] = React.useState<
    number | null
  >(null);
  const scrollToMessage = React.useCallback(
    (messageId: number) => {
      // 消息首行 = 消息顶,align:"start" 视觉等价于旧 message 级行为。
      const index = firstRowIndexByMessageId.get(messageId);
      if (index != null) {
        virtualizer.scrollToIndex(index, { align: "start" });
        return;
      }
      if (folding && messages.some((m) => m.id === messageId)) {
        setExpanded(true);
        setPendingScrollMessageId(messageId);
      }
    },
    [firstRowIndexByMessageId, folding, messages, virtualizer],
  );

  React.useEffect(() => {
    if (pendingScrollMessageId == null) return;
    const index = firstRowIndexByMessageId.get(pendingScrollMessageId);
    if (index == null) return;
    virtualizer.scrollToIndex(index, { align: "start" });
    setPendingScrollMessageId(null);
  }, [firstRowIndexByMessageId, pendingScrollMessageId, virtualizer]);

  const anchorRestoreFrameRef = React.useRef<number | null>(null);
  const cancelAnchorRestore = React.useCallback(() => {
    if (anchorRestoreFrameRef.current != null) {
      window.cancelAnimationFrame(anchorRestoreFrameRef.current);
      anchorRestoreFrameRef.current = null;
    }
  }, []);
  // scrollToAnchor:把"保存时位于视口顶部的那条消息"(messageId)重新钉到距视口顶
  // offset px 处。路由重挂时虚拟器整列只有 estimate 高度,getOffsetForIndex 会随可见
  // 窗口逐行复测而变化;这里用 rAF 循环重算 target 直到稳定(连续 2 帧不变)或封顶,
  // 从而消除"仅凭像素 scrollTop 会落到错消息"的冷启动漂移——锚点钉的是消息身份,
  // 不是像素值。返回 false=该消息不在 displayMessages(被折叠/未加载),交回调用方
  // 回退像素恢复。
  const scrollToAnchor = React.useCallback(
    (messageId: number, offset: number, rowKey?: string): boolean => {
      const index =
        (rowKey != null ? rowIndexByKey.get(rowKey) : undefined) ??
        firstRowIndexByMessageId.get(messageId) ??
        -1;
      const el = scrollElement;
      if (index < 0 || !el) return false;
      cancelAnchorRestore();
      let prevTarget = -1;
      let stableFrames = 0;
      let frames = 0;
      const settle = () => {
        anchorRestoreFrameRef.current = null;
        const info = virtualizer.getOffsetForIndex(index, "start");
        if (!info) return;
        const target = Math.max(0, info[0] + offset);
        if (Math.abs(el.scrollTop - target) > 1) el.scrollTop = target;
        stableFrames =
          Math.abs(target - prevTarget) <= 1 ? stableFrames + 1 : 0;
        prevTarget = target;
        frames += 1;
        if (stableFrames < 2 && frames < 30) {
          anchorRestoreFrameRef.current = window.requestAnimationFrame(settle);
        }
      };
      // 同步先钉一帧(调用点是 chat-panel 的 useLayoutEffect,paint 前生效),
      // 避免路由重挂首帧闪在顶部;后续逐帧由 settle 自己挂 rAF 收敛。
      settle();
      return true;
    },
    [
      cancelAnchorRestore,
      firstRowIndexByMessageId,
      rowIndexByKey,
      scrollElement,
      virtualizer,
    ],
  );
  React.useEffect(() => () => cancelAnchorRestore(), [cancelAnchorRestore]);

  React.useImperativeHandle(
    ref,
    () => ({
      scrollToMessage,
      scrollToAnchor,
    }),
    [scrollToAnchor, scrollToMessage],
  );

  const renderVirtualRows =
    shouldVirtualize && active && !restoreScrollOffsetRef.current;
  const virtualTotalSize = virtualizer.getTotalSize();
  if (virtualTotalSize > 0) {
    lastVirtualTotalSizeRef.current = virtualTotalSize;
  }
  const virtualSpacerSize =
    virtualTotalSize > 0
      ? virtualTotalSize
      : lastVirtualTotalSizeRef.current || rows.length * 48;

  // 行间距:消息末行 pb-5(消息间距),消息内分片行 pb-2(block 间距)。padding
  // 打在行 wrapper 上,跟随 measureElement 一起计入行高。
  const rowWrapperPad = (index: number): string => {
    const row = rows[index];
    const next = rows[index + 1];
    return next == null || next.messageId !== row?.messageId ? "pb-5" : "pb-2";
  };

  return (
    <TooltipProvider delayDuration={200}>
      <TranscriptUIStateProvider>
        <TranscriptRenderContext.Provider value={renderCtx}>
          {/* 不再加 max-w-4xl —— 内部 ChatMessage 已经 cap 在 720px,
          这里再叠一层外层 max-w 没有任何收紧效果,只会留出 phantom 空白。 */}
          <div className={shouldVirtualize ? "min-h-full" : "flex flex-col"}>
            {folding && foldedCount > 0 ? (
              <CompactHistoryFold
                count={foldedCount}
                onExpand={() => setExpanded(true)}
              />
            ) : null}
            {shouldVirtualize ? (
              <div
                className="relative w-full"
                style={{ height: `${virtualSpacerSize}px` }}
              >
                {renderVirtualRows
                  ? virtualizer.getVirtualItems().map((virtualItem) => {
                      const row = rows[virtualItem.index];
                      if (!row) return null;
                      return (
                        <div
                          key={virtualItem.key}
                          ref={virtualizer.measureElement}
                          data-index={virtualItem.index}
                          data-message-id={row.messageId}
                          data-row-key={row.key}
                          className={cn(
                            "absolute left-0 top-0 w-full",
                            rowWrapperPad(virtualItem.index),
                          )}
                          style={{
                            transform: `translateY(${virtualItem.start}px)`,
                          }}
                        >
                          {renderRowView(row)}
                        </div>
                      );
                    })
                  : null}
              </div>
            ) : (
              rows.map((row, index) => (
                <div
                  key={row.key}
                  data-message-id={row.messageId}
                  data-row-key={row.key}
                  className={rowWrapperPad(index)}
                >
                  {renderRowView(row)}
                </div>
              ))
            )}
          </div>
        </TranscriptRenderContext.Provider>
      </TranscriptUIStateProvider>
    </TooltipProvider>
  );
});

export {
  ApprovalGate,
  ChatComposer,
  ChatMessage,
  ChatTranscript,
  CodeBlock,
  ErrorCard,
  MessageMeta,
  ToolCall,
};
export type { ChatTranscriptHandle };
