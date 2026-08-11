import * as React from "react";

import {
  ListLLMProviders,
  SetChatSessionProvider,
} from "../../../../wailsjs/go/app/App";
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
  /** agent 后端类型；非可选择的 backend（openclaw 等）不拉取供应商，pill 常显但
   *  disabled（决策 10：禁用而非隐藏）。 */
  backendType: string;
  /** agent 已绑定的 provider key；空串 = 未绑（CLI 登录态）。 */
  boundProviderKey?: string | null;
  /**
   * >0 = 已有会话：选择立即持久化（调用 SetChatSessionProvider）；0 / undefined =
   * 新建会话：纯瞬态，首发 Send 时随 SendRequest.ProviderKey 透传（决策 2/UI 不变）。
   */
  sessionId?: number;
  /**
   * 会话当前已持久化的供应商 key（ChatSessionDetail.providerKey）。仅 sessionId>0
   * 时用于水合初始选择；会话切换 / 切换成功回填后随之更新本地显示。
   */
  persistedProviderKey?: string | null;
  /**
   * 持久化切换成功后的回调（典型 reloadSession()，把新追加的切换 notice 拉进
   * transcript；pill 标签本身已由 SetChatSessionProvider 的响应直接更新，不依赖它）。
   */
  onSwitched?: () => void;
}

export interface UseProviderPillReturn {
  /** 当前选择的 provider key；空串 = 跟随 agent 绑定。新建会话是纯瞬态本地值，
   *  已有会话镜像 chat_sessions.provider_key（乐观更新，失败回滚）。 */
  providerKey: string;
  setProviderKey: (key: string) => void;
  /** 与 backendType 兼容的供应商列表。 */
  providers: llm_provider_svc.ProviderItem[];
  /** 列表加载中 → pill 禁用。 */
  loading: boolean;
  /** 拉取失败 或 持久化切换失败 → 弹层底部错误行。 */
  error: string | null;
  /** 未绑 agent（CLI 登录态）。 */
  unbound: boolean;
  /** 生效 key：providerKey || boundProviderKey（用于 pill 标签 / 高亮）。 */
  effectiveKey: string;
  /** pill 是否禁用（规格「UI 与禁用状态」状态表：加载中 / 后端不可选 / 无兼容供应商）。 */
  disabled: boolean;
  /** disabled 的原因，供 tooltip 说明；null = 未禁用或禁用原因是既有的「加载中」
   *  （沿用既有行为，不需要新增说明）。 */
  disabledReason: "unsupportedBackend" | "noCompatibleProviders" | null;
}

/**
 * useProviderPill: composer 里 LLM 供应商选择器的状态（新建会话瞬态选择 + 已有会话
 * 立即持久化切换，规格 2026-08-10「已有会话切换 LLM 供应商」决策 1/9/10）。
 *
 * 数据流:
 *  - 新建会话（sessionId<=0）还没有 session 行，瞬态选择只存本地 state，首发 Send
 *    时随 SendRequest.ProviderKey 透传给后端随 Session 一起落库（决策 2/UI）。
 *  - 已有会话（sessionId>0）：初始选择水合自 persistedProviderKey；选中一项立即调
 *    SetChatSessionProvider(sessionId, providerKey) 持久化（乐观更新，失败回滚并把
 *    错误显示在弹层底部）；成功后按响应更新显示并调 onSwitched()（典型是
 *    reloadSession()，把后端追加的切换 notice 拉进 transcript）。
 *  - 供应商列表只随 backendType 变化拉取一次（跨会话、同 backend 类型不重复拉取，
 *    避免在已有会话之间切换 tab 时反复打 ListLLMProviders）；按 isProviderCompatible
 *    过滤出兼容供应商；未绑 agent（CLI 登录态）同样显示选择器（决策 5）。
 *  - 已绑 agent:未选时 effectiveKey 落到 boundProviderKey,pill 显示绑定供应商名。
 *  - pill 在任何会话/后端状态下都渲染（决策 10：禁用而非隐藏）：不可选择的 backend
 *    （openclaw）与「加载成功但无兼容供应商」都是 disabled + tooltip；拉取失败保持
 *    可用（既有行为，弹层底部报错）。
 */
export function useProviderPill({
  backendType,
  boundProviderKey,
  sessionId,
  persistedProviderKey,
  onSwitched,
}: UseProviderPillOptions): UseProviderPillReturn {
  const selectable = isProviderSelectableBackend(backendType);
  const [providerKey, setProviderKeyState] = React.useState(
    persistedProviderKey ?? "",
  );
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

  // 供应商列表只随 backendType 变化重新拉取：与后端类型强绑定，两条同类型的已有
  // 会话之间切换不重复请求。
  React.useEffect(() => {
    backendTypeRef.current = backendType;
    setError(null);
    void fetchProviders();
  }, [backendType, fetchProviders]);

  // 会话切换（sessionId 变化）、agent 切换（backendType 变化，新建会话场景）、或
  // 持久化选择被外部改写（切换成功回填 / 另一处 reload）时，把显示值同步回 DB 当前
  // 值：新建会话固定是空串；已有会话取 persistedProviderKey。
  React.useEffect(() => {
    setProviderKeyState(persistedProviderKey ?? "");
  }, [sessionId, backendType, persistedProviderKey]);

  const setProviderKey = React.useCallback(
    (key: string) => {
      // 选中的就是当前已生效的那一项：什么都不做，与后端 SetChatSessionProvider
      // 的同一处幂等 no-op 语义对齐（避免每点一次已选中项就打一次 IPC）。
      if (key === providerKey) {
        return;
      }
      const previous = providerKey;
      setProviderKeyState(key);
      setError(null);

      if (!sessionId || sessionId <= 0) {
        // 新建会话：纯瞬态，首发 Send 时随 SendRequest.ProviderKey 透传。
        return;
      }
      void SetChatSessionProvider({ sessionId, providerKey: key } as never)
        .then((resp) => {
          setProviderKeyState(resp.providerKey);
          onSwitched?.();
        })
        .catch((e: unknown) => {
          const msg = e instanceof Error ? e.message : String(e);
          console.error("[provider-pill] switch failed", e);
          setProviderKeyState(previous);
          setError(msg);
        });
    },
    [providerKey, sessionId, onSwitched],
  );

  // disabledReason 优先级：后端不可选 > 加载中（无专属原因，沿用既有行为）>
  // 加载成功但无兼容供应商。拉取失败时 providers 也是空数组，但不算「无兼容供应商」
  // ——失败态本身保持可用，靠弹层底部错误行说明（既有行为），不能被这条规则误判禁用。
  const disabledReason: "unsupportedBackend" | "noCompatibleProviders" | null =
    !selectable
      ? "unsupportedBackend"
      : !loading && !error && providers.length === 0
        ? "noCompatibleProviders"
        : null;

  return {
    providerKey,
    setProviderKey,
    providers,
    loading,
    error,
    unbound: !boundProviderKey,
    effectiveKey: providerKey || boundProviderKey || "",
    disabled: loading || disabledReason !== null,
    disabledReason,
  };
}
