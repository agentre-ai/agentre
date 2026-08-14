import * as React from "react";
import { GitBranch, Loader2, RefreshCw } from "lucide-react";
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

  const triggerLabel =
    pillState.mode === "follow-agent"
      ? t("modelTargetPicker.special.chat")
      : pillState.mode === "provider-default"
        ? t("providerPill.mode.followDefault")
        : pillState.mode === "fixed"
          ? t("providerPill.mode.fixed")
          : t("providerPill.mode.invalid");
  const triggerSub = pillState.resolutionLabel || undefined;
  const triggerIcon =
    pillState.mode === "follow-agent" ? (
      <GitBranch
        data-testid="follow-agent-icon"
        className="size-3.5 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
    ) : pillState.mode === "invalid" ? null : (
      modelLogo
    );
  const modeMarker = pillState.dynamic ? (
    <RefreshCw
      data-testid="provider-pill-dynamic-icon"
      className="size-3 shrink-0 text-primary-text"
      aria-hidden="true"
    />
  ) : null;

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
        specialSublabel={boundResolutionLabel || undefined}
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
        triggerLabel={triggerLabel}
        triggerSub={
          <span className="flex min-w-0 items-center gap-1">
            {triggerIcon}
            <span className="truncate">{triggerSub}</span>
            {modeMarker}
          </span>
        }
        title={disabledTitle ?? undefined}
        footer={t("providerPill.switchNote")}
        aria-label={t("providerPill.aria", { provider: ariaValue })}
        data-testid="provider-pill"
        className={cn(
          "h-auto w-auto min-h-9 px-2 py-1 text-2xs font-medium",
          invalid
            ? "border-status-waiting/60 bg-status-waiting-bg text-status-waiting"
            : providerKey
              ? "border-primary-text/60 bg-primary-soft text-primary-text"
              : "border-border bg-muted text-foreground",
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
