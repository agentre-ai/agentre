import * as React from "react";

import { ListLLMProviders } from "../../../../wailsjs/go/app/App";
import { llm_provider_svc } from "../../../../wailsjs/go/models";
import i18n from "@/i18n";

/**
 * 供应商选择器可渲染的后端集合（规格决策 4/5）：openclaw 不消费 agentre provider，
 * 不渲染供应商选择器；其余四类后端均可选。
 */
const PROVIDER_SELECTABLE_BACKENDS = new Set([
  "builtin",
  "claudecode",
  "codex",
  "piagent",
]);

/** 后端是否渲染供应商选择器（openclaw / 未知后端 → false）。 */
export function isProviderSelectableBackend(backendType: string): boolean {
  return PROVIDER_SELECTABLE_BACKENDS.has(backendType);
}

/**
 * 供应商类型与后端是否兼容 —— 与后端 agent_backend_entity.ProviderTypeMatch
 * 对齐（builtin→全部；claudecode→anthropic；codex→openai-response；
 * piagent→anthropic/openai-chat/openai-response；其余后端→false）。
 * 列不兼容项必然在后端 Send 校验失败，前端只列兼容供应商。
 */
export function isProviderCompatible(
  backendType: string,
  providerType: string,
): boolean {
  switch (backendType) {
    case "builtin":
      return true;
    case "claudecode":
      return providerType === "anthropic";
    case "codex":
      return providerType === "openai-response";
    case "piagent":
      return (
        providerType === "anthropic" ||
        providerType === "openai-chat" ||
        providerType === "openai-response"
      );
    default:
      return false;
  }
}

export interface UseProviderPillOptions {
  /** agent 后端类型；非可选择的 backend（openclaw 等）不拉取供应商。 */
  backendType: string;
  /** agent 已绑定的 provider key；空串 = 未绑（CLI 登录态）。 */
  boundProviderKey?: string | null;
}

export interface UseProviderPillReturn {
  /** 当前瞬态选择的 provider key；空串 = 跟随 agent 绑定。 */
  providerKey: string;
  setProviderKey: (key: string) => void;
  /** 与 backendType 兼容的供应商列表。 */
  providers: llm_provider_svc.ProviderItem[];
  /** 列表加载中 → pill 禁用。 */
  loading: boolean;
  /** 拉取失败 → 弹层底部错误行。 */
  error: string | null;
  /** 未绑 agent（CLI 登录态）。 */
  unbound: boolean;
  /** 生效 key：providerKey || boundProviderKey（用于 pill 标签 / 高亮）。 */
  effectiveKey: string;
}

/**
 * useProviderPill: 新建会话（sessionId===0）的 LLM 供应商选择器状态。
 *
 * 数据流:
 *  - 新建会话还没有 sessionId,瞬态选择只存本地 state,首发 Send 时随
 *    SendRequest.ProviderKey 透传给后端随 Session 一起落库（决策 2/UI）。
 *  - 只拉一次 ListLLMProviders() 并按 isProviderCompatible 过滤出兼容供应商;
 *    未绑 agent（CLI 登录态）同样显示选择器（决策 5）。
 *  - 已绑 agent:未选时 effectiveKey 落到 boundProviderKey,pill 显示绑定供应商名。
 */
export function useProviderPill({
  backendType,
  boundProviderKey,
}: UseProviderPillOptions): UseProviderPillReturn {
  const selectable = isProviderSelectableBackend(backendType);
  const [providerKey, setProviderKeyState] = React.useState("");
  const [providers, setProviders] = React.useState<
    llm_provider_svc.ProviderItem[]
  >([]);
  const [loading, setLoading] = React.useState(selectable);
  const [error, setError] = React.useState<string | null>(null);

  // backendTypeRef 让在途的 ListLLMProviders 响应按「解析时刻的当前 backend」过滤，
  // 避免 agent 快速切换时旧请求晚到、把上一后端的兼容集合短暂画进弹层。
  const backendTypeRef = React.useRef(backendType);

  const fetchProviders = React.useCallback(async () => {
    if (!isProviderSelectableBackend(backendTypeRef.current)) {
      setProviders([]);
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const resp = await ListLLMProviders();
      const all = resp.items ?? [];
      setProviders(
        all.filter((p) => isProviderCompatible(backendTypeRef.current, p.type)),
      );
    } catch {
      setProviders([]);
      setError(i18n.t("providerPill.errorFetch"));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    backendTypeRef.current = backendType;
    // backend 切换（agent 切换）时重置瞬态选择并重拉列表。
    setProviderKeyState("");
    setError(null);
    void fetchProviders();
  }, [backendType, fetchProviders]);

  const setProviderKey = React.useCallback((key: string) => {
    setProviderKeyState(key);
    setError(null);
  }, []);

  return {
    providerKey,
    setProviderKey,
    providers,
    loading,
    error,
    unbound: !boundProviderKey,
    effectiveKey: providerKey || boundProviderKey || "",
  };
}
