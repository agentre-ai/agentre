import * as React from "react";
import { Loader2, RefreshCw, UserRound } from "lucide-react";
import { useTranslation } from "react-i18next";
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
import { cn } from "@/lib/utils";

import type { UseProviderPillReturn } from "./use-provider-pill";
import { LlmModelLogo, LlmProviderLogo } from "../ai-brand-logo";
import { ModelTargetPicker } from "../model-target-picker";

export type ProviderPillProps = UseProviderPillReturn;

/**
 * ProviderPill: composer 动作行里的 LLM ModelTarget 选择器。
 *
 * 常驻触发按钮直接呈现四态与解析结果：跟随 Agent、跟随供应商默认、固定模型、失效。
 * 共享 ModelTargetPicker 继续承载搜索、最近使用、远端门控与键盘交互。
 */
export function ProviderPill({
  providerKey,
  modelKey,
  setTarget,
  backendType,
  catalog,
  loading,
  catalogLoading,
  catalogError,
  error,
  unbound,
  effectiveKey,
  pillState,
  boundResolutionLabel,
  boundProviderType,
  boundProviderLabel,
  boundModelLabel,
  boundCliLogin,
  invalid,
  disabled,
  disabledReason,
  executionLocation = "",
  remoteCatalog,
  supportsFixedModel = true,
  remoteMissing = false,
  syncProvider,
  canSyncProvider = false,
}: ProviderPillProps) {
  const { t } = useTranslation();
  const [providerToSync, setProviderToSync] = React.useState<{
    providerKey: string;
    name: string;
  } | null>(null);
  const [syncing, setSyncing] = React.useState(false);
  const [syncError, setSyncError] = React.useState<string | null>(null);

  const disabledTitle =
    disabled && disabledReason === "unsupportedBackend"
      ? t("providerPill.disabledUnsupportedBackend")
      : disabled && disabledReason === "noCompatibleProviders"
        ? t("providerPill.disabledNoCompatibleProviders")
        : null;

  const ariaValue =
    pillState.resolutionLabel ||
    effectiveKey ||
    (unbound ? t("providerPill.unselected") : t("providerPill.unselected"));

  const providerLogo = pillState.providerType ? (
    <LlmProviderLogo
      providerType={pillState.providerType}
      providerName={pillState.providerLabel}
      className="size-3.5"
    />
  ) : null;
  const modelLogo = pillState.modelLabel ? (
    <LlmModelLogo
      model={pillState.modelLabel}
      providerType={pillState.providerType}
      providerName={pillState.providerLabel}
      className="size-3.5"
    />
  ) : (
    providerLogo
  );

  // 脸上写的是「实际会跑哪个模型」：解析出模型就写模型 ID（标识符走等宽），只解析到
  // 供应商（新建会话没有 agent model key）就退回供应商人读名；确知没绑供应商就写
  // 「CLI 自身登录态」（那才是这一轮真正的模型来源）。三者都不成立 = 还不知道，不写。
  const resolvedTarget = pillState.modelLabel ? (
    <span className="font-mono">{pillState.modelLabel}</span>
  ) : pillState.providerLabel ? (
    pillState.providerLabel
  ) : pillState.cliLogin ? (
    t("modelTargetPicker.special.backend")
  ) : null;
  const triggerIcon =
    pillState.mode === "follow-agent" ? (
      <UserRound
        data-testid="follow-agent-icon"
        className="size-3.5 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
    ) : pillState.mode === "invalid" ? null : (
      // 失效态的警示三角由 Picker 的 invalid 分支画，这里不重复挂图标。
      modelLogo
    );
  const modeMarker = pillState.dynamic ? (
    <RefreshCw
      data-testid="provider-pill-dynamic-icon"
      className="size-3 shrink-0 text-primary-text"
      aria-hidden="true"
    />
  ) : null;

  // mockup ?view=chat 的四态 pill 都是单行：图标 → 文字 → 可选 ↻ → chevron。模式不再
  // 写成一行字，而是由图标（人形 / 品牌标识 / 警示三角）加 ↻（跟随默认才有）表达，
  // 省下来的一行让「实际会跑哪个模型」直接上脸。
  const triggerText =
    pillState.mode === "follow-agent" ? (
      <>
        {t("modelTargetPicker.special.chat")}
        {resolvedTarget ? (
          <>
            {" · "}
            {resolvedTarget}
          </>
        ) : null}
      </>
    ) : pillState.mode === "invalid" ? (
      <>
        {resolvedTarget}
        {" · "}
        {t("providerPill.mode.invalid")}
      </>
    ) : (
      resolvedTarget
    );

  // 顶部「跟随 Agent 绑定」项的解析副行：箭头点出「解析到」，品牌标识让绑定的供应商
  // 一眼可认，模型 ID 单独走等宽（标识符不跟人读名一起排）。目录里解析不出供应商时
  // 回落纯文字 —— 宁可少画一个标识，也不画半个空标识。
  const specialSublabel = boundProviderType ? (
    <span
      data-testid="special-resolution"
      className="flex min-w-0 items-center gap-1"
    >
      <span aria-hidden="true">→</span>
      <LlmProviderLogo
        providerType={boundProviderType}
        providerName={boundProviderLabel}
        className="size-3.5"
      />
      <span className="min-w-0 truncate">
        {boundProviderLabel}
        {boundModelLabel ? (
          <>
            {" · "}
            <span className="font-mono">{boundModelLabel}</span>
          </>
        ) : null}
      </span>
    </span>
  ) : boundCliLogin ? (
    // 确知没绑供应商：箭头保留（它解析成的就是「CLI 自身的登录账号」），但没有供应商
    // 可认领，不画标识。
    <span
      data-testid="special-resolution"
      className="flex min-w-0 items-center gap-1"
    >
      <span aria-hidden="true">→</span>
      <span className="min-w-0 truncate">
        {t("modelTargetPicker.special.backendSublabel")}
      </span>
    </span>
  ) : (
    boundResolutionLabel || undefined
  );

  return (
    <>
      <ModelTargetPicker
        scenario="chat"
        backendType={backendType}
        executionLocation={executionLocation}
        selected={{ providerKey, modelKey }}
        onChange={setTarget}
        catalog={catalog}
        loading={loading || catalogLoading}
        error={catalogError || error !== null}
        errorText={error ?? undefined}
        disabled={disabled}
        invalid={invalid}
        specialSublabel={specialSublabel}
        onSyncProvider={
          canSyncProvider
            ? (provider) => {
                setSyncError(null);
                setProviderToSync({
                  providerKey: provider.providerKey,
                  name: provider.name,
                });
              }
            : undefined
        }
        remoteCatalog={remoteCatalog}
        supportsFixedModel={supportsFixedModel}
        remoteMissing={remoteMissing}
        triggerLabel={
          <>
            {triggerIcon}
            <span className="min-w-0 truncate">{triggerText}</span>
            {modeMarker}
          </>
        }
        title={disabledTitle ?? undefined}
        footer={t("providerPill.switchNote")}
        aria-label={t("providerPill.aria", { provider: ariaValue })}
        data-testid="provider-pill"
        className={cn(
          // mockup ?view=chat 的 .pill：中性描边 + card 底，全程一个样 —— 选没选、
          // 跟随还是固定，一律不靠常亮边框宣示，那份区分由图标与 ↻ 承担。
          // 悬停只加深背景，不动边框（边框式反馈是明确否掉的）；focus-visible ring
          // 由 Picker 基类提供，键盘定位不受影响。
          "h-[26px] w-auto cursor-pointer gap-1.5 rounded-md px-2.5 text-2xs font-medium",
          invalid
            ? "border-status-waiting bg-status-waiting-bg text-status-waiting hover:bg-status-waiting-bg/70"
            : "border-border bg-card text-foreground hover:bg-accent",
        )}
      />
      <Dialog
        open={providerToSync !== null}
        onOpenChange={(open) => {
          if (!open && !syncing) {
            setProviderToSync(null);
            setSyncError(null);
          }
        }}
      >
        <DialogContent className="max-w-[440px]">
          <DialogHeader>
            <DialogTitle>{t("modelTargetPicker.syncDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("modelTargetPicker.syncDialog.description", {
                provider: providerToSync?.name ?? "",
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {syncError ? (
              <p role="alert" className="text-xs text-status-error">
                {syncError}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t("modelTargetPicker.syncDialog.confirmation")}
              </p>
            )}
          </DialogBody>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={syncing}
              onClick={() => setProviderToSync(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={syncing || providerToSync === null}
              onClick={() => {
                if (!providerToSync) return;
                setSyncing(true);
                setSyncError(null);
                void syncProvider(providerToSync.providerKey)
                  .then(() => setProviderToSync(null))
                  .catch((err: unknown) => {
                    setSyncError(
                      err instanceof Error ? err.message : String(err),
                    );
                  })
                  .finally(() => setSyncing(false));
              }}
            >
              {syncing ? (
                <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
              ) : null}
              {t("modelTargetPicker.syncDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
