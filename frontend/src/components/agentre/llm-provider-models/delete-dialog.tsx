import * as React from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Lock, PowerOff, Trash2 } from "lucide-react";

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

import {
  DeleteLLMModel,
  DeleteLLMProvider,
  LLMModelRefCounts,
  LLMProviderRefCounts,
  SetLLMModelEnabled,
  SetLLMProviderEnabled,
} from "../../../../wailsjs/go/app/App";
import { llm_provider_svc } from "../../../../wailsjs/go/models";
import {
  type Model,
  type Provider,
  type ReferenceCounts,
  errMessage,
  totalReferences,
} from "./index";

export type DeleteTarget =
  | { kind: "provider"; provider: Provider }
  | { kind: "model"; model: Model };

export type DeleteState =
  | { phase: "loading" }
  | { phase: "blocked"; counts: ReferenceCounts }
  | { phase: "confirm"; counts: ReferenceCounts }
  | { phase: "deleting" }
  | { phase: "disabling" };

function refsDetail(
  counts: ReferenceCounts,
  t: (key: string) => string,
): string {
  const parts: string[] = [];
  if (counts.backends > 0) {
    parts.push(`${counts.backends} ${t("llmProviders.delete.refBackends")}`);
  }
  if (counts.sessions > 0) {
    parts.push(`${counts.sessions} ${t("llmProviders.delete.refSessions")}`);
  }
  if (counts.routes > 0) {
    parts.push(`${counts.routes} ${t("llmProviders.delete.refRoutes")}`);
  }
  return parts.join(" · ") || t("llmProviders.delete.noRefs");
}

export function DeleteDialog({
  target,
  onClose,
  onDeleted,
  onDisabled,
}: {
  target: DeleteTarget | null;
  onClose: () => void;
  onDeleted: (target: DeleteTarget) => void;
  // 被引用无法删除时的第三条出路：停用（保留引用、可恢复）。
  onDisabled?: (target: DeleteTarget) => void;
}) {
  const { t } = useTranslation();
  const [state, setState] = React.useState<DeleteState>({ phase: "loading" });
  const [error, setError] = React.useState<string | null>(null);

  const provider = target?.kind === "provider" ? target.provider : null;
  const model = target?.kind === "model" ? target.model : null;
  const open = target !== null;

  React.useEffect(() => {
    if (!target) return;
    let cancelled = false;
    setState({ phase: "loading" });
    setError(null);
    void (async () => {
      try {
        if (target.kind === "provider") {
          const resp = await LLMProviderRefCounts(
            new llm_provider_svc.ProviderRefCountsRequest({
              providerKey: target.provider.providerKey,
            }),
          );
          if (cancelled) return;
          const counts = resp.counts ?? {
            backends: 0,
            sessions: 0,
            routes: 0,
          };
          setState(
            totalReferences(counts) > 0
              ? { phase: "blocked", counts }
              : { phase: "confirm", counts },
          );
        } else {
          const resp = await LLMModelRefCounts(
            new llm_provider_svc.ModelRefCountsRequest({
              modelKey: target.model.modelKey,
            }),
          );
          if (cancelled) return;
          const counts = resp.counts ?? {
            backends: 0,
            sessions: 0,
            routes: 0,
          };
          setState(
            totalReferences(counts) > 0
              ? { phase: "blocked", counts }
              : { phase: "confirm", counts },
          );
        }
      } catch (err) {
        if (!cancelled) setError(errMessage(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [target]);

  const confirmDelete = React.useCallback(async () => {
    if (!target) return;
    setState({ phase: "deleting" });
    try {
      if (target.kind === "provider") {
        await DeleteLLMProvider(
          new llm_provider_svc.DeleteProviderRequest({
            id: target.provider.id,
          }),
        );
      } else {
        await DeleteLLMModel(
          new llm_provider_svc.DeleteModelRequest({ id: target.model.id }),
        );
      }
      onDeleted(target);
      onClose();
    } catch (err) {
      setError(errMessage(err));
      setState({
        phase: "confirm",
        counts: { backends: 0, sessions: 0, routes: 0 },
      });
    }
  }, [onClose, onDeleted, target]);

  // 被引用挡住删除时的替代动作：停用保留全部引用，之后可恢复。
  const disableInstead = React.useCallback(async () => {
    if (!target) return;
    setState({ phase: "disabling" });
    setError(null);
    try {
      if (target.kind === "provider") {
        await SetLLMProviderEnabled(
          new llm_provider_svc.SetProviderEnabledRequest({
            id: target.provider.id,
            enabled: false,
          }),
        );
      } else {
        await SetLLMModelEnabled(
          new llm_provider_svc.SetModelEnabledRequest({
            id: target.model.id,
            enabled: false,
          }),
        );
      }
      onDisabled?.(target);
      onClose();
    } catch (err) {
      setError(errMessage(err));
      setState({
        phase: "blocked",
        counts: { backends: 0, sessions: 0, routes: 0 },
      });
    }
  }, [onClose, onDisabled, target]);

  const name = provider ? provider.name : model ? model.modelId : "";
  const deleting = state.phase === "deleting";
  const disabling = state.phase === "disabling";
  const blocked = state.phase === "blocked";
  const busy = deleting || disabling;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) onClose();
      }}
    >
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>
            {target?.kind === "provider"
              ? t("llmProviders.delete.providerTitle", { name })
              : t("llmProviders.delete.modelTitle", { model: name })}
          </DialogTitle>
          <DialogDescription>
            {target?.kind === "provider"
              ? t("llmProviders.delete.providerDescription")
              : t("llmProviders.delete.modelDescription")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody className="space-y-3">
          {error ? (
            <p className="text-2xs text-status-error">{error}</p>
          ) : state.phase === "loading" ? (
            <div className="flex items-center gap-2 text-2xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
              {t("llmProviders.delete.checking")}
            </div>
          ) : blocked ? (
            <div
              role="alert"
              className="flex flex-col gap-2 rounded-md border border-status-error/40 bg-destructive-soft px-3 py-2.5"
            >
              <div className="flex items-start gap-2 text-xs text-status-error">
                <Lock className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                <div className="flex flex-col gap-0.5">
                  <span className="font-semibold">
                    {t("llmProviders.delete.blockedTitle")}
                  </span>
                  <span className="text-2xs leading-relaxed">
                    {target?.kind === "provider"
                      ? t("llmProviders.delete.blockedProviderDetail")
                      : t("llmProviders.delete.blockedModelDetail", {
                          refs: refsDetail(state.counts, t),
                        })}
                  </span>
                </div>
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-2 text-2xs text-muted-foreground">
              <p className="leading-relaxed">
                {target?.kind === "provider"
                  ? t("llmProviders.delete.providerConfirmHint", { name })
                  : t("llmProviders.delete.modelConfirmHint", { model: name })}
              </p>
            </div>
          )}
        </DialogBody>

        <DialogFooter className={blocked ? "justify-between gap-2" : undefined}>
          {blocked ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              onClick={() => void disableInstead()}
              disabled={busy}
              title={t("llmProviders.delete.disableInsteadHint")}
            >
              {disabling ? (
                <Loader2
                  className="size-3.5 animate-spin"
                  data-icon="inline-start"
                  aria-hidden="true"
                />
              ) : (
                <PowerOff
                  className="size-3.5"
                  data-icon="inline-start"
                  aria-hidden="true"
                />
              )}
              {t("llmProviders.delete.disableInstead")}
            </Button>
          ) : null}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 text-xs"
              onClick={onClose}
              disabled={busy}
            >
              {t("common.cancel")}
            </Button>
            {state.phase === "confirm" || blocked || disabling ? (
              <Button
                type="button"
                variant="destructive"
                size="sm"
                className="h-8 gap-1.5 text-xs"
                onClick={() => void confirmDelete()}
                // 被引用时删除按钮保留但禁用 + 写明原因，而不是消失
                disabled={busy || blocked}
                title={
                  blocked
                    ? target?.kind === "provider"
                      ? t("llmProviders.delete.blockedProviderDetail")
                      : t("llmProviders.delete.blockedModelDetail", {
                          refs: refsDetail(
                            state.phase === "blocked"
                              ? state.counts
                              : { backends: 0, sessions: 0, routes: 0 },
                            t,
                          ),
                        })
                    : undefined
                }
              >
                {deleting ? (
                  <Loader2
                    className="size-3.5 animate-spin"
                    data-icon="inline-start"
                    aria-hidden="true"
                  />
                ) : (
                  <Trash2
                    className="size-3.5"
                    data-icon="inline-start"
                    aria-hidden="true"
                  />
                )}
                {target?.kind === "provider"
                  ? t("llmProviders.delete.providerConfirmButton")
                  : t("llmProviders.delete.modelConfirmButton")}
              </Button>
            ) : null}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
