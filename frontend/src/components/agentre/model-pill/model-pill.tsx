import * as React from "react";
import { useTranslation } from "react-i18next";
import { Box, ChevronDown, RotateCcw } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

import type { UseModelPillReturn } from "./use-model-pill";

export type ModelPillProps = UseModelPillReturn;

/**
 * ModelPill: composer 动作行里权限模式旁的模型切换 pill。
 *
 * 状态机（与规格决策 7/8 一致）:
 *  - 已绑 provider: 弹层列 /v1/models（名称 + contextWindow），当前生效项高亮；
 *    列表加载中 → pill 禁用；拉取失败 → 弹层底部错误行。
 *  - 未绑 provider + 已有会话: pill 灰显（disabled:opacity-60 + cursor-not-allowed）
 *    + tooltip，不渲染弹层交互。
 *  - 未绑 provider + 新建会话: pill 可用，弹层第一项「跟随默认」+ 自由输入模型 id。
 */
export function ModelPill({
  override,
  setOverride,
  models,
  loading,
  error,
  bound,
  unboundExisting,
  interactive,
  providerName,
  effectiveModel,
}: ModelPillProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);
  const [freeInput, setFreeInput] = React.useState("");

  const label =
    effectiveModel ||
    (bound
      ? t("modelPill.followDefault")
      : t("modelPill.followDefaultUnbound"));
  const disabled = !interactive || loading;

  function handleSelect(model: string) {
    setOpen(false);
    setFreeInput("");
    setOverride(model);
  }

  function handleFreeSubmit() {
    const model = freeInput.trim();
    if (!model) return;
    handleSelect(model);
  }

  return (
    <Popover open={interactive ? open : false} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          aria-label={t("modelPill.aria", { model: label })}
          title={
            unboundExisting
              ? t("modelPill.unboundExisting")
              : t("modelPill.title", { model: label })
          }
          data-testid="model-pill"
          className={cn(
            "inline-flex h-6 cursor-pointer items-center gap-1 rounded-md border px-2 text-2xs font-medium leading-none transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
            "disabled:cursor-not-allowed disabled:opacity-60",
            override
              ? "bg-primary-soft text-primary-text border-primary-text/60"
              : "bg-muted text-foreground border-border",
          )}
        >
          <Box
            className="size-3 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="max-w-[140px] truncate font-mono">{label}</span>
          {override ? (
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
      {interactive ? (
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
                {t("modelPill.heading")}
              </span>
              {providerName ? (
                <span className="truncate font-mono text-2xs text-subtle-foreground">
                  · {providerName}
                </span>
              ) : null}
            </div>
          </div>

          <ul className="flex flex-col gap-0.5 p-1.5" role="listbox">
            {/* 第一项：跟随供应商默认；override 非空时点击清空，为空时高亮。 */}
            <li>
              <button
                type="button"
                role="option"
                aria-selected={!override}
                onClick={() => handleSelect("")}
                className={cn(
                  "flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                  "cursor-pointer",
                  !override ? "bg-accent" : "hover:bg-accent/60",
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
                      {bound
                        ? t("modelPill.followDefault")
                        : t("modelPill.followDefaultUnbound")}
                    </span>
                    {!override ? (
                      <span className="ml-auto inline-flex items-center gap-1 rounded-sm bg-primary-soft px-1.5 py-px font-mono text-2xs font-semibold tracking-wider text-primary-text">
                        <span className="size-1 rounded-full bg-primary" />
                        {t("modelPill.active")}
                      </span>
                    ) : null}
                  </span>
                  <span className="mt-0.5 block text-2xs leading-snug text-muted-foreground">
                    {t("modelPill.followDefaultDesc")}
                  </span>
                </span>
              </button>
            </li>

            {bound
              ? models.map((model) => {
                  const active = model.id === effectiveModel;
                  return (
                    <li key={model.id}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={active}
                        onClick={() => handleSelect(model.id)}
                        className={cn(
                          "flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                          "cursor-pointer",
                          active ? "bg-primary-soft" : "hover:bg-accent/60",
                        )}
                      >
                        <span className="mt-1 inline-flex size-3 shrink-0 items-center justify-center">
                          {active ? (
                            <span className="size-1.5 rounded-full bg-primary" />
                          ) : (
                            <span className="size-1.5 rounded-full border border-border" />
                          )}
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="flex items-center gap-1.5">
                            <span className="truncate font-mono text-xs font-semibold text-foreground">
                              {model.id}
                            </span>
                          </span>
                          {model.vendor ? (
                            <span className="mt-0.5 block truncate text-2xs leading-snug text-muted-foreground">
                              {model.vendor}
                            </span>
                          ) : null}
                        </span>
                        {model.contextWindow > 0 ? (
                          <span className="shrink-0 self-center font-mono text-2xs text-subtle-foreground">
                            {formatContextWindow(model.contextWindow)}
                          </span>
                        ) : null}
                      </button>
                    </li>
                  );
                })
              : null}

            {/* 未绑 provider + 新建会话：自由输入模型 id。 */}
            {!bound ? (
              <li>
                <form
                  className="flex items-center gap-2 px-2 py-1.5"
                  onSubmit={(e) => {
                    e.preventDefault();
                    handleFreeSubmit();
                  }}
                >
                  <input
                    type="text"
                    value={freeInput}
                    onChange={(e) => setFreeInput(e.target.value)}
                    placeholder={t("modelPill.freeInputPlaceholder")}
                    aria-label={t("modelPill.freeInputPlaceholder")}
                    className="h-7 min-w-0 flex-1 rounded-md border border-border bg-background px-2 font-mono text-2xs text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring/40"
                  />
                  <button
                    type="submit"
                    disabled={!freeInput.trim()}
                    className="inline-flex h-7 shrink-0 cursor-pointer items-center rounded-md bg-primary px-2.5 text-2xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {t("modelPill.submit")}
                  </button>
                </form>
              </li>
            ) : null}
          </ul>

          <div className="border-t border-border px-3.5 py-2 text-2xs text-muted-foreground">
            {t("modelPill.footer")}
          </div>
          {error ? (
            <div
              data-testid="model-pill-error"
              className="border-t border-destructive/40 bg-destructive-soft px-3.5 py-1.5 text-2xs text-destructive"
            >
              {error}
            </div>
          ) : null}
        </PopoverContent>
      ) : null}
    </Popover>
  );
}

/** 把 token 数压成 "128K" 一类的紧凑展示；非整 K 时四舍五入。 */
function formatContextWindow(tokens: number): string {
  if (tokens < 1000) return String(tokens);
  return `${Math.round(tokens / 1000)}K`;
}
