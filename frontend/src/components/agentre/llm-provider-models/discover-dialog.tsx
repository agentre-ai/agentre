import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  CheckCircle2,
  Download,
  Inbox,
  Loader2,
  RefreshCw,
  Search,
  TriangleAlert,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
  ImportLLMModels,
  PreviewLLMModels,
} from "../../../../wailsjs/go/app/App";
import { llm_provider_svc } from "../../../../wailsjs/go/models";
import { LlmModelLogo } from "../ai-brand-logo";
import {
  type Model,
  type ModelInfo,
  type Provider,
  errMessage,
  formatTokens,
} from "./index";

export function DiscoverDialog({
  provider,
  existingModels,
  onClose,
  onImported,
}: {
  provider: Provider | null;
  existingModels: Model[];
  onClose: () => void;
  onImported: (resp: llm_provider_svc.ImportModelsResponse) => void;
}) {
  const { t } = useTranslation();
  const open = provider !== null;
  const [items, setItems] = React.useState<ModelInfo[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [selected, setSelected] = React.useState<Set<string>>(new Set());
  const [search, setSearch] = React.useState("");
  const [importing, setImporting] = React.useState(false);
  const requestRef = React.useRef(0);

  const existingIds = React.useMemo(
    () => new Set(existingModels.map((m) => m.modelId)),
    [existingModels],
  );

  const fetchPreview = React.useCallback(async () => {
    if (!provider) return;
    const requestId = ++requestRef.current;
    setLoading(true);
    setError(null);
    try {
      const resp = await PreviewLLMModels(
        new llm_provider_svc.PreviewModelsRequest({
          id: provider.id,
          type: provider.type,
          apiKey: "",
          baseUrl: "",
        }),
      );
      if (requestId !== requestRef.current) return;
      const list = resp.items ?? [];
      setItems(list);
      setSelected(
        new Set(list.filter((m) => !existingIds.has(m.id)).map((m) => m.id)),
      );
    } catch (err) {
      if (requestId === requestRef.current) setError(errMessage(err));
    } finally {
      if (requestId === requestRef.current) setLoading(false);
    }
  }, [existingIds, provider]);

  React.useEffect(() => {
    if (open) void fetchPreview();
  }, [open, fetchPreview]);

  const newCount = items.filter((m) => !existingIds.has(m.id)).length;
  const existingCount = items.length - newCount;
  const selectedCount = [...selected].filter(
    (id) => !existingIds.has(id),
  ).length;

  const trimmed = search.trim().toLowerCase();
  const visible = trimmed
    ? items.filter(
        (m) =>
          m.id.toLowerCase().includes(trimmed) ||
          m.vendor.toLowerCase().includes(trimmed),
      )
    : items;

  const toggle = React.useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const toggleAllNew = React.useCallback(
    (checked: boolean) => {
      setSelected((prev) => {
        const next = new Set(prev);
        for (const m of items) {
          if (existingIds.has(m.id)) continue;
          if (checked) {
            next.add(m.id);
          } else {
            next.delete(m.id);
          }
        }
        return next;
      });
    },
    [existingIds, items],
  );

  const allNewChecked =
    newCount > 0 &&
    items
      .filter((m) => !existingIds.has(m.id))
      .every((m) => selected.has(m.id));

  const importModels = React.useCallback(async () => {
    if (!provider || selectedCount === 0) return;
    setImporting(true);
    setError(null);
    try {
      const resp = await ImportLLMModels(
        new llm_provider_svc.ImportModelsRequest({
          providerId: provider.id,
          models: items
            .filter((m) => !existingIds.has(m.id) && selected.has(m.id))
            .map(
              (m) =>
                new llm_provider_svc.ModelInput({
                  modelId: m.id,
                  name: "",
                  contextWindow: m.contextWindow,
                  maxOutput: m.maxOutput,
                }),
            ),
        }),
      );
      onImported(resp);
      onClose();
    } catch (err) {
      setError(errMessage(err));
    } finally {
      setImporting(false);
    }
  }, [
    existingIds,
    items,
    onClose,
    onImported,
    provider,
    selected,
    selectedCount,
  ]);

  const providerName = provider ? provider.name : "";

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !importing) onClose();
      }}
    >
      <DialogContent className="max-w-[640px]">
        <DialogHeader>
          <DialogTitle>
            {t("llmProviders.discover.title", { name: providerName })}
          </DialogTitle>
          <DialogDescription>
            {t("llmProviders.discover.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-5 py-3">
          <div className="relative min-w-0 flex-1">
            <Search
              className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              value={search}
              onChange={(e) => setSearch(e.currentTarget.value)}
              placeholder={t("llmProviders.discover.searchPlaceholder")}
              className="h-8 pl-8 text-xs"
              aria-label={t("llmProviders.discover.searchAria")}
            />
          </div>
          <span className="shrink-0 font-mono text-2xs text-muted-foreground">
            {t("llmProviders.discover.count", {
              remote: items.length,
              new: newCount,
              existing: existingCount,
            })}
          </span>
        </div>

        <DialogBody className="space-y-2">
          {error ? (
            <div
              role="alert"
              className="flex flex-col items-start gap-2 rounded-md border border-status-error/40 bg-destructive-soft px-3 py-2.5 text-status-error"
            >
              <div className="flex items-start gap-2 text-xs">
                <TriangleAlert
                  className="mt-0.5 size-4 shrink-0"
                  aria-hidden="true"
                />
                <span className="leading-relaxed">{error}</span>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-7 gap-1.5 text-2xs"
                onClick={() => void fetchPreview()}
                disabled={loading}
              >
                {loading ? (
                  <Loader2 className="size-3 animate-spin" aria-hidden="true" />
                ) : (
                  <RefreshCw className="size-3" aria-hidden="true" />
                )}
                {t("llmProviders.discover.retry")}
              </Button>
            </div>
          ) : loading ? (
            <div className="flex items-center gap-2 py-6 text-2xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
              {t("llmProviders.discover.loading")}
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-6 text-center">
              <Inbox
                className="size-5 text-subtle-foreground"
                aria-hidden="true"
              />
              <p className="text-xs font-semibold text-foreground">
                {t("llmProviders.discover.empty")}
              </p>
            </div>
          ) : (
            <div className="space-y-1.5">
              <label className="flex items-center gap-2 rounded-md border border-border bg-secondary px-2.5 py-2">
                <Checkbox
                  checked={allNewChecked}
                  onCheckedChange={(next) => toggleAllNew(next === true)}
                  disabled={newCount === 0}
                  aria-label={t("llmProviders.discover.selectAllNew")}
                />
                <span className="text-2xs font-semibold">
                  {t("llmProviders.discover.selectAllNew")}
                </span>
                <span className="ml-auto text-2xs text-muted-foreground">
                  {t("llmProviders.discover.selectAllHint")}
                </span>
              </label>
              {visible.map((m) => {
                const existing = existingIds.has(m.id);
                const checked = selected.has(m.id);
                return (
                  <div
                    key={m.id}
                    className="flex items-center gap-2.5 rounded-md border border-border px-2.5 py-2"
                  >
                    <Checkbox
                      checked={existing ? false : checked}
                      disabled={existing}
                      onCheckedChange={() => toggle(m.id)}
                      aria-label={m.id}
                    />
                    <LlmModelLogo
                      providerType={provider?.type ?? ""}
                      model={m.id}
                      className="size-4"
                    />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">
                      {m.id}
                    </span>
                    {m.contextWindow > 0 || m.maxOutput > 0 ? (
                      <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                        {formatTokens(m.contextWindow)} ctx ·{" "}
                        {formatTokens(m.maxOutput)} out
                      </span>
                    ) : null}
                    <span
                      className={
                        existing
                          ? "shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-2xs text-muted-foreground"
                          : "shrink-0 rounded-sm bg-primary-soft px-1.5 py-0.5 text-2xs text-primary-text"
                      }
                    >
                      {existing
                        ? t("llmProviders.discover.existingSkip")
                        : t("llmProviders.discover.new")}
                    </span>
                  </div>
                );
              })}
            </div>
          )}
        </DialogBody>

        <DialogFooter className="justify-between gap-2">
          <span className="text-2xs leading-relaxed text-muted-foreground">
            {t("llmProviders.discover.importSummary", { count: selectedCount })}
          </span>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 text-xs"
              onClick={onClose}
              disabled={importing}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              onClick={() => void importModels()}
              disabled={importing || selectedCount === 0}
            >
              {importing ? (
                <Loader2
                  className="size-3.5 animate-spin"
                  data-icon="inline-start"
                  aria-hidden="true"
                />
              ) : (
                <Download
                  className="size-3.5"
                  data-icon="inline-start"
                  aria-hidden="true"
                />
              )}
              {t("llmProviders.discover.importButton", {
                count: selectedCount,
              })}
            </Button>
          </div>
        </DialogFooter>

        {importing ? (
          <p
            role="status"
            className="flex items-center gap-1.5 px-5 pb-3 text-2xs text-muted-foreground"
          >
            <CheckCircle2
              className="size-3 text-status-running"
              aria-hidden="true"
            />
            {t("llmProviders.discover.importing")}
          </p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
