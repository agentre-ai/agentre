import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

import type { UseProviderPillReturn } from "./use-provider-pill";
import { ModelTargetPicker } from "../model-target-picker";

export type ProviderPillProps = UseProviderPillReturn;

/**
 * ProviderPill: composer 动作行里权限模式旁的 LLM ModelTarget 选择器（规格
 * 2026-08-10「已有会话切换 LLM 供应商」决策 10 + 2026-08-11「新建与已有会话流程」）。
 *
 * 用共享 ModelTargetPicker（scenario="chat"）承载主体交互：顶部特殊项「跟随 Agent
 * 绑定」（inherit-agent），Provider 组内 provider-default 首项 + fixed-model 列表，
 * 搜索 / 最近使用 / 兼容过滤 / 键盘导航。只发射 providerKey/modelKey，绝不发射名称、
 * ModelID 或凭据。
 *
 * 状态机:
 *  - 未绑 agent（CLI 登录态）同样显示：顶部特殊项语义为「不选，走 CLI 登录态」。
 *  - pill 在所有会话/后端状态下都渲染，不可切换时 disabled 而不是隐藏（决策 10）。
 *  - 已有会话选中立即调 SetChatSessionModelTarget 持久化（hook 侧处理，乐观更新 +
 *    失败回滚）；新建会话纯瞬态，首发 Send 时随 SendRequest.ProviderKey/ModelKey 透传。
 *  - 选中目标在目录里解析不出来（Provider/Model 缺失/停用/被删）→「目标已失效」红边，
 *    不清除 key（spec「Failure and recovery semantics」fixed-model 严格阻止）。
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
  invalid,
  disabled,
  disabledReason,
}: ProviderPillProps) {
  const { t } = useTranslation();

  const disabledTitle =
    disabled && disabledReason === "unsupportedBackend"
      ? t("providerPill.disabledUnsupportedBackend")
      : disabled && disabledReason === "noCompatibleProviders"
        ? t("providerPill.disabledNoCompatibleProviders")
        : null;

  const label =
    effectiveKey ||
    (unbound ? t("providerPill.unselected") : t("providerPill.unselected"));

  // 未选（inherit-agent）但 agent 已绑 provider：触发按钮显示绑定供应商名（spec
  // 「新建与已有会话流程」：Agent Backend 已绑定 ProviderModel 时展示其解析结果），
  // 而不是顶部特殊项「跟随 Agent 绑定」；已选 / 未绑时交给 Picker 按目录解析。
  const boundLabel =
    !providerKey && !modelKey && effectiveKey
      ? (catalog.find((p) => p.providerKey === effectiveKey)?.name ??
        effectiveKey)
      : undefined;

  return (
    <ModelTargetPicker
      scenario="chat"
      backendType={backendType}
      selected={{ providerKey, modelKey }}
      onChange={setTarget}
      catalog={catalog}
      loading={loading || catalogLoading}
      error={catalogError || error !== null}
      errorText={error ?? undefined}
      disabled={disabled}
      invalid={invalid}
      triggerLabel={boundLabel}
      title={disabledTitle ?? undefined}
      footer={t("providerPill.switchNote")}
      aria-label={t("providerPill.aria", { provider: label })}
      data-testid="provider-pill"
      className={cn(
        "h-6 w-auto px-2 text-2xs font-medium",
        providerKey
          ? "border-primary-text/60 bg-primary-soft text-primary-text"
          : "border-border bg-muted text-foreground",
      )}
    />
  );
}
