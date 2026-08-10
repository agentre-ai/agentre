import * as React from "react";
import { useTranslation } from "react-i18next";
import { Box, ChevronDown, RotateCcw } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

import type { UseProviderPillReturn } from "./use-provider-pill";
import { LlmModelLogo, LlmProviderLogo } from "../ai-brand-logo";

export type ProviderPillProps = UseProviderPillReturn;

/**
 * ProviderPill: composer 动作行里权限模式旁的 LLM 供应商选择器（规格「新建会话
 * 供应商选择器」+ 决策 4/5）。
 *
 * 状态机:
 *  - 只列与后端兼容的供应商（isProviderCompatible 过滤,后端 ProviderTypeMatch 对齐）。
 *  - 未绑 agent（CLI 登录态）同样显示:弹层第一项「跟随 agent 绑定」语义为
 *    「不选,走 CLI 登录态」;选具体供应商可接管该会话。
 *  - 未选时 pill 标签:agent 已绑 → 绑定供应商名;未绑 → 「选择供应商」占位。
 *  - 列表加载中 → pill 禁用;拉取失败 → 弹层底部错误行。
 *  - 瞬态选择不发任何 IPC,首发 Send 时随 SendRequest.ProviderKey 透传落库。
 */
export function ProviderPill({
  providerKey,
  setProviderKey,
  providers,
  loading,
  error,
  unbound,
  effectiveKey,
}: ProviderPillProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);

  const selected = providers.find((p) => p.providerKey === effectiveKey);
  const label = selected?.name || effectiveKey || t("providerPill.unselected");
  const disabled = loading;

  function handleSelect(key: string) {
    setOpen(false);
    setProviderKey(key);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          aria-label={t("providerPill.aria", { provider: label })}
          title={t("providerPill.title", { provider: label })}
          data-testid="provider-pill"
          className={cn(
            "inline-flex h-6 cursor-pointer items-center gap-1 rounded-md border px-2 text-2xs font-medium leading-none transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
            "disabled:cursor-not-allowed disabled:opacity-60",
            providerKey
              ? "bg-primary-soft text-primary-text border-primary-text/60"
              : "bg-muted text-foreground border-border",
          )}
        >
          <Box
            className="size-3 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          {selected ? (
            <LlmModelLogo
              providerType={selected.type}
              providerName={selected.name}
              baseUrl={selected.baseUrl}
              model={selected.model ?? ""}
              className="size-3.5"
            />
          ) : null}
          <span className="max-w-[140px] truncate font-mono">{label}</span>
          {providerKey ? (
            <RotateCcw
              className="size-2.5 shrink-0 text-primary-text"
              aria-hidden="true"
            />
          ) : (
            <ChevronDown
              className="size-2.5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        side="top"
        sideOffset={8}
        className="w-[320px] p-0"
      >
        <div className="flex items-center justify-between border-b border-border px-3.5 py-2.5">
          <div className="flex items-center gap-1.5">
            <Box className="size-3.5 text-foreground" aria-hidden="true" />
            <span className="text-xs font-semibold text-foreground">
              {t("providerPill.heading")}
            </span>
          </div>
        </div>

        <ul className="flex flex-col gap-0.5 p-1.5" role="listbox">
          {/* 第一项：跟随 agent 绑定。未选时高亮；选中具体供应商后点击可清回。 */}
          <li>
            <button
              type="button"
              role="option"
              aria-selected={!providerKey}
              onClick={() => handleSelect("")}
              className={cn(
                "flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                "cursor-pointer",
                !providerKey ? "bg-accent" : "hover:bg-accent/60",
              )}
            >
              <span className="mt-px inline-flex size-5 shrink-0 items-center justify-center">
                <RotateCcw
                  className="size-3.5 text-muted-foreground"
                  aria-hidden="true"
                />
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5">
                  <span className="text-xs font-semibold text-foreground">
                    {t("providerPill.followAgentBinding")}
                  </span>
                  {!providerKey ? (
                    <span className="ml-auto inline-flex items-center gap-1 rounded-sm bg-primary-soft px-1.5 py-px font-mono text-2xs font-semibold tracking-wider text-primary-text">
                      <span className="size-1 rounded-full bg-primary" />
                      {t("providerPill.active")}
                    </span>
                  ) : null}
                </span>
                <span className="mt-0.5 block text-2xs leading-snug text-muted-foreground">
                  {unbound
                    ? t("providerPill.followAgentBindingUnboundDesc")
                    : t("providerPill.followAgentBindingDesc")}
                </span>
              </span>
            </button>
          </li>

          {providers.map((p) => {
            const active = providerKey === p.providerKey;
            return (
              <li key={p.providerKey}>
                <button
                  type="button"
                  role="option"
                  aria-selected={active}
                  onClick={() => handleSelect(p.providerKey)}
                  className={cn(
                    "flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                    "cursor-pointer",
                    active ? "bg-primary-soft" : "hover:bg-accent/60",
                  )}
                >
                  <span className="relative mt-0.5 inline-flex size-6 shrink-0 items-center justify-center">
                    <LlmProviderLogo
                      providerType={p.type}
                      providerName={p.name}
                      baseUrl={p.baseUrl}
                      className="size-6 rounded-md"
                    />
                    {active ? (
                      <span className="absolute -bottom-0.5 -right-0.5 size-2 rounded-full border border-background bg-primary" />
                    ) : null}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="truncate font-mono text-xs font-semibold text-foreground">
                      {p.name}
                    </span>
                    <span className="mt-0.5 block truncate font-mono text-2xs leading-snug text-muted-foreground">
                      <span className="inline-flex items-center gap-1">
                        {p.model ? (
                          <LlmModelLogo
                            providerType={p.type}
                            providerName={p.name}
                            baseUrl={p.baseUrl}
                            model={p.model}
                            className="size-3"
                          />
                        ) : null}
                        {p.type}
                      </span>
                      {p.model ? ` · ${p.model}` : ""}
                    </span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>

        {error ? (
          <div
            data-testid="provider-pill-error"
            className="border-t border-destructive/40 bg-destructive-soft px-3.5 py-1.5 text-2xs text-destructive"
          >
            {error}
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
