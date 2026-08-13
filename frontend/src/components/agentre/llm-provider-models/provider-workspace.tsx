import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  SendHorizontal,
  Sparkles,
  Trash2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

import { LlmModelLogo, LlmProviderLogo } from "../ai-brand-logo";
import { cn } from "@/lib/utils";
import {
  type Model,
  type Provider,
  endpointFor,
  formatTokens,
  providerTypeMeta,
} from "./index";

export type WorkspaceHandlers = {
  onAddModel: () => void;
  onDeleteModel: (model: Model) => void;
  onDeleteProvider: () => void;
  onDiscover: () => void;
  onEditConnection: () => void;
  onEditModel: (model: Model) => void;
  onRetryModels: () => void;
  onSetDefault: (model: Model) => void;
  onTestModel: (model: Model) => void;
  onTestProvider: () => void;
  onToggleModelEnabled: (model: Model) => void;
  onToggleProviderEnabled: () => void;
};

export function ProviderWorkspace({
  provider,
  models,
  modelsError,
  modelsLoading,
  onTestModel,
  onTestProvider,
  onEditConnection,
  onDeleteProvider,
  onDiscover,
  onAddModel,
  onSetDefault,
  onToggleModelEnabled,
  onEditModel,
  onDeleteModel,
  onToggleProviderEnabled,
  onRetryModels,
  testingDefault,
  testingModelId,
}: {
  provider: Provider;
  models: Model[];
  modelsError: string | null;
  modelsLoading: boolean;
  testingDefault: boolean;
  testingModelId: number | null;
} & WorkspaceHandlers) {
  const { t } = useTranslation();
  const [search, setSearch] = React.useState("");

  const meta =
    provider.type in providerTypeMeta
      ? providerTypeMeta[provider.type as keyof typeof providerTypeMeta]
      : undefined;
  const endpoint = endpointFor(provider);

  const trimmed = search.trim().toLowerCase();
  const visible = trimmed
    ? models.filter(
        (m) =>
          m.modelId.toLowerCase().includes(trimmed) ||
          m.modelKey.toLowerCase().includes(trimmed) ||
          m.name.toLowerCase().includes(trimmed),
      )
    : models;

  // Provider 启用需要属于它的启用默认模型
  const defaultModel = models.find(
    (m) => m.modelKey === provider.defaultModelKey,
  );
  const hasEnabledDefault =
    provider.enabled ||
    Boolean(provider.defaultModelKey && defaultModel && defaultModel.enabled);
  const enableDisabledReason = hasEnabledDefault
    ? undefined
    : t("llmProviders.workspace.cannotEnableNoDefault");

  return (
    <div
      role="region"
      aria-label={t("llmProviders.workspace.ariaLabel", {
        name: provider.name,
      })}
      className="@container flex min-w-0 flex-col overflow-hidden"
    >
      {/* Provider header */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-3 sm:px-4">
        <div className="flex min-w-0 items-start gap-2.5">
          <LlmProviderLogo
            providerType={provider.type}
            providerName={provider.name}
            baseUrl={provider.baseUrl}
            className="mt-0.5 size-8 rounded-md"
          />
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-sm font-semibold">
              {provider.name}
            </span>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-2xs text-muted-foreground">
              <span className="rounded-sm bg-secondary px-1.5 py-0.5 font-mono">
                {meta
                  ? t(`llmProviders.providerType.${provider.type}.label`)
                  : provider.type}
              </span>
              <span className="hidden truncate font-mono lg:inline">
                {endpoint}
              </span>
              <span className="hidden truncate font-mono lg:inline">
                {provider.hasApiKey
                  ? provider.maskedApiKey
                  : t("llmProviders.row.noApiKey")}
              </span>
              <span
                className={cn(
                  "inline-flex items-center gap-1 font-medium",
                  provider.enabled
                    ? "text-status-running"
                    : "text-status-waiting",
                )}
              >
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    provider.enabled
                      ? "bg-status-running"
                      : "bg-status-waiting",
                  )}
                  aria-hidden="true"
                />
                {provider.enabled
                  ? t("llmProviders.nav.enabled")
                  : t("llmProviders.nav.disabled")}
              </span>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          <label
            className={cn(
              "flex items-center gap-1.5 text-2xs text-muted-foreground",
              !hasEnabledDefault && "cursor-not-allowed",
            )}
            title={enableDisabledReason}
          >
            <Switch
              checked={provider.enabled}
              disabled={!hasEnabledDefault}
              onCheckedChange={() => onToggleProviderEnabled()}
              size="sm"
              title={enableDisabledReason}
              aria-label={t("llmProviders.workspace.enableNamed", {
                name: provider.name,
              })}
            />
            {provider.enabled
              ? t("llmProviders.workspace.enabledShort")
              : t("llmProviders.workspace.disabledShort")}
          </label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
            onClick={onTestProvider}
            disabled={testingDefault}
            aria-label={t("llmProviders.workspace.testNamed", {
              name: provider.name,
            })}
            title={t("llmProviders.workspace.testTitle")}
          >
            {testingDefault ? (
              <Loader2
                className="size-3.5 animate-spin"
                data-icon="inline-start"
                aria-hidden="true"
              />
            ) : (
              <SendHorizontal
                className="size-3.5"
                data-icon="inline-start"
                aria-hidden="true"
              />
            )}
            {t("llmProviders.workspace.test")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs text-status-error"
            onClick={onDeleteProvider}
            aria-label={t("llmProviders.workspace.deleteNamed", {
              name: provider.name,
            })}
            title={t("llmProviders.workspace.deleteTitle")}
          >
            <Trash2
              className="size-3.5"
              data-icon="inline-start"
              aria-hidden="true"
            />
            {t("llmProviders.workspace.delete")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
            onClick={onEditConnection}
            aria-label={t("llmProviders.workspace.editConnectionNamed", {
              name: provider.name,
            })}
          >
            <Pencil
              className="size-3.5"
              data-icon="inline-start"
              aria-hidden="true"
            />
            {t("llmProviders.workspace.editConnection")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
            onClick={onDiscover}
            aria-label={t("llmProviders.workspace.discoverNamed", {
              name: provider.name,
            })}
          >
            <RefreshCw
              className="size-3.5"
              data-icon="inline-start"
              aria-hidden="true"
            />
            {t("llmProviders.workspace.discover")}
          </Button>
        </div>
      </div>

      {/* Model tools */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-2 sm:px-4">
        <div className="relative min-w-0 flex-1">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            placeholder={t("llmProviders.workspace.searchPlaceholder")}
            aria-label={t("llmProviders.workspace.searchAria")}
            className="h-8 w-full rounded-md border border-input bg-transparent pl-8 pr-3 text-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>
        <span className="shrink-0 font-mono text-2xs text-muted-foreground">
          {t("llmProviders.workspace.modelCount", {
            shown: visible.length,
            total: models.length,
          })}
        </span>
      </div>

      {/* Model table */}
      {modelsError ? (
        <div
          role="alert"
          className="flex flex-col items-start gap-2 p-4 text-status-error"
        >
          <span className="text-xs">{modelsError}</span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 text-2xs"
            onClick={onRetryModels}
          >
            <RefreshCw
              className="size-3"
              data-icon="inline-start"
              aria-hidden="true"
            />
            {t("llmProviders.workspace.retry")}
          </Button>
        </div>
      ) : modelsLoading && models.length === 0 ? (
        <div className="flex items-center justify-center gap-2 py-8 text-2xs text-muted-foreground">
          <Loader2 className="size-4 animate-spin" aria-hidden="true" />
          {t("llmProviders.workspace.loadingModels")}
        </div>
      ) : visible.length === 0 ? (
        <div className="flex flex-col items-center gap-2 px-6 py-8 text-center">
          <div
            aria-hidden="true"
            className="flex size-10 items-center justify-center rounded-full bg-primary-soft text-primary-text"
          >
            <Sparkles className="size-4" />
          </div>
          <p className="text-sm font-semibold">
            {t("llmProviders.workspace.noModelsTitle")}
          </p>
          <p className="max-w-xs text-2xs leading-relaxed text-muted-foreground">
            {t("llmProviders.workspace.noModelsDescription")}
          </p>
          <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-[30px] gap-1.5 px-3 text-xs"
              onClick={onDiscover}
            >
              <RefreshCw
                className="size-3.5"
                data-icon="inline-start"
                aria-hidden="true"
              />
              {t("llmProviders.workspace.discoverModels")}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-[30px] gap-1.5 px-3 text-xs"
              onClick={onAddModel}
            >
              <Plus
                className="size-3.5"
                data-icon="inline-start"
                aria-hidden="true"
              />
              {t("llmProviders.workspace.addModelManual")}
            </Button>
          </div>
        </div>
      ) : (
        <div className="min-w-0 flex-1 overflow-auto">
          <Table
            aria-label={t("llmProviders.modelsTable.ariaLabel")}
            className="min-w-[560px] @min-[560px]:min-w-0"
          >
            <TableHeader>
              <TableRow className="bg-secondary hover:bg-secondary">
                <TableHead className="px-4 font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                  {t("llmProviders.modelsTable.modelId")}
                </TableHead>
                <TableHead className="w-[88px] @max-[640px]:hidden font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                  {t("llmProviders.modelsTable.context")}
                </TableHead>
                <TableHead className="w-[88px] @max-[640px]:hidden font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                  {t("llmProviders.modelsTable.maxOutput")}
                </TableHead>
                <TableHead className="w-[80px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                  {t("llmProviders.modelsTable.default")}
                </TableHead>
                <TableHead className="w-[60px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((model) => {
                const isDefault = model.modelKey === provider.defaultModelKey;
                const canDisable = !isDefault;
                const canSetDefault = model.enabled && !isDefault;
                return (
                  <TableRow
                    key={model.id}
                    className="align-top hover:bg-accent/45"
                  >
                    <TableCell className="px-4 py-2.5">
                      <div className="flex min-w-0 items-start gap-2">
                        <LlmModelLogo
                          providerType={provider.type}
                          providerName={provider.name}
                          baseUrl={provider.baseUrl}
                          model={model.modelId}
                          className="mt-0.5 size-4"
                        />
                        <div className="flex min-w-0 flex-col gap-0.5">
                          <span className="flex min-w-0 items-center gap-1.5">
                            <span className="truncate font-mono text-xs">
                              {model.modelId}
                            </span>
                            {isDefault ? (
                              <span className="shrink-0 rounded-sm bg-primary-soft px-1.5 py-0.5 text-2xs font-medium text-primary-text">
                                {t("llmProviders.modelsTable.defaultBadge")}
                              </span>
                            ) : null}
                          </span>
                          <span className="flex items-center gap-1.5 text-2xs text-muted-foreground">
                            {model.modelKey}
                            {model.enabled ? (
                              <span className="text-status-running">
                                {t("llmProviders.modelsTable.enabled")}
                              </span>
                            ) : (
                              <span className="text-status-waiting">
                                {t("llmProviders.modelsTable.disabled")}
                              </span>
                            )}
                          </span>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="py-2.5 @max-[640px]:hidden font-mono text-2xs text-muted-foreground">
                      {formatTokens(model.contextWindow)}
                    </TableCell>
                    <TableCell className="py-2.5 @max-[640px]:hidden font-mono text-2xs text-muted-foreground">
                      {formatTokens(model.maxOutput)}
                    </TableCell>
                    <TableCell className="py-2.5">
                      <RadioGroup
                        value={isDefault ? model.modelKey : ""}
                        onValueChange={() => onSetDefault(model)}
                        className="gap-0"
                      >
                        <RadioGroupItem
                          value={model.modelKey}
                          checked={isDefault}
                          disabled={!canSetDefault}
                          aria-label={
                            isDefault
                              ? t(
                                  "llmProviders.modelsTable.currentDefaultAria",
                                  {
                                    model: model.modelId,
                                  },
                                )
                              : t("llmProviders.modelsTable.setDefaultAria", {
                                  model: model.modelId,
                                })
                          }
                        />
                      </RadioGroup>
                    </TableCell>
                    <TableCell className="px-4 py-2.5">
                      <div className="flex justify-end gap-1">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          className="size-[26px] text-muted-foreground"
                          aria-label={t(
                            "llmProviders.modelsTable.testModelNamed",
                            {
                              model: model.modelId,
                            },
                          )}
                          title={t("llmProviders.modelsTable.testTitle")}
                          onClick={() => onTestModel(model)}
                          disabled={testingModelId === model.id}
                        >
                          {testingModelId === model.id ? (
                            <Loader2
                              className="size-3.5 animate-spin"
                              data-icon="only"
                              aria-hidden="true"
                            />
                          ) : (
                            <SendHorizontal
                              data-icon="only"
                              aria-hidden="true"
                            />
                          )}
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          className="size-[26px] text-muted-foreground"
                          aria-label={t(
                            "llmProviders.modelsTable.editModelNamed",
                            {
                              model: model.modelId,
                            },
                          )}
                          title={t("common.edit")}
                          onClick={() => onEditModel(model)}
                        >
                          <Pencil data-icon="only" aria-hidden="true" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          className="size-[26px] text-status-error"
                          aria-label={t(
                            "llmProviders.modelsTable.deleteModelNamed",
                            {
                              model: model.modelId,
                            },
                          )}
                          title={
                            isDefault
                              ? t(
                                  "llmProviders.modelsTable.deleteBlockedDefault",
                                )
                              : t("llmProviders.modelsTable.deleteTitle")
                          }
                          onClick={() => onDeleteModel(model)}
                          disabled={isDefault}
                        >
                          <Trash2 data-icon="only" aria-hidden="true" />
                        </Button>
                        <label
                          className="flex items-center"
                          title={
                            canDisable
                              ? undefined
                              : t(
                                  "llmProviders.modelsTable.disableBlockedDefault",
                                )
                          }
                        >
                          <Switch
                            checked={model.enabled}
                            disabled={!canDisable}
                            onCheckedChange={() => onToggleModelEnabled(model)}
                            size="sm"
                            aria-label={t(
                              "llmProviders.modelsTable.enableNamed",
                              {
                                model: model.modelId,
                              },
                            )}
                          />
                        </label>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
