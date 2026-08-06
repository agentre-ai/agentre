import * as React from "react";

import {
  ListLLMModels,
  ListLLMProviders,
  SetChatSessionModel,
} from "../../../../wailsjs/go/app/App";
import { llm_provider_svc } from "../../../../wailsjs/go/models";
import i18n from "@/i18n";

export interface UseModelPillOptions {
  /** chat session id；0 / null / undefined = 新建会话（瞬态选择，随 Send 落库）。 */
  sessionId: number | undefined | null;
  /**
   * 已绑 provider key（已有会话 = session.llmProviderKey；新建会话 =
   * newSessionAgent.llmProviderKey）。空串 = 未绑（CLI 登录态）。
   */
  llmProviderKey?: string | null;
  /** 已有会话的持久 override（session.modelOverride）；新建会话为 undefined。 */
  initialOverride?: string | null;
  /** 当前 agent 的生效默认模型（session.providerDefaultModel），供 pill 显示。 */
  providerDefaultModel?: string | null;
}

export interface UseModelPillReturn {
  /** 当前生效 override；空串 = 跟随供应商默认。 */
  override: string;
  /** 选择模型：已有会话 → SetChatSessionModel 落库；新建会话 → 仅瞬态。空串 = 回默认。 */
  setOverride: (model: string) => void;
  /** 已绑 provider 的 /v1/models 列表（未绑新建会话为空）。 */
  models: llm_provider_svc.ModelInfo[];
  /** 模型列表加载中 → pill 禁用。 */
  loading: boolean;
  /** 拉取失败（provider 离线等）→ 弹层底部错误行。 */
  error: string | null;
  /** 是否已绑 provider（llmProviderKey 非空）。 */
  bound: boolean;
  /** 未绑 provider + 已有会话 → 灰显不可交互，不渲染弹层。 */
  unboundExisting: boolean;
  /** 弹层可交互（非未绑已有会话）。 */
  interactive: boolean;
  /** 弹层头部 provider 显示名（未绑新建会话为空）。 */
  providerName: string;
  /** 当前生效模型名（override || providerDefaultModel）。 */
  effectiveModel: string;
}

/**
 * useModelPill: 串起 composer ModelPill 的状态 + 后端 SetChatSessionModel 调用。
 *
 * 数据流:
 *  - 已有会话: DB (chat_sessions.model_override) 是 source-of-truth,父组件从
 *    LoadSession 拿到 detail.modelOverride 后通过 initialOverride 注入;切换走
 *    SetChatSessionModel IPC 持久化,乐观更新 + 失败回滚。
 *  - 新建会话: 还没有 sessionId,override 只存本地瞬态,首发 Send 时随
 *    SendRequest.ModelOverride 透传给后端落库。
 *  - 模型列表: 已绑 provider → 用 llmProviderKey 在 ListLLMProviders() 里匹配
 *    provider id,再调 ListLLMModels 拉 /v1/models;未绑新建会话无列表,走自由输入。
 */
export function useModelPill({
  sessionId,
  llmProviderKey,
  initialOverride,
  providerDefaultModel,
}: UseModelPillOptions): UseModelPillReturn {
  const isExistingSession = !!sessionId && sessionId > 0;
  const bound = !!llmProviderKey;
  const unboundExisting = isExistingSession && !bound;

  const [override, setOverrideState] = React.useState<string>(
    initialOverride ?? "",
  );
  const [models, setModels] = React.useState<llm_provider_svc.ModelInfo[]>([]);
  const [providerName, setProviderName] = React.useState("");
  const [loading, setLoading] = React.useState(bound);
  const [error, setError] = React.useState<string | null>(null);
  const lastOverrideRef = React.useRef<string>(override);

  const fetchModels = React.useCallback(async () => {
    if (!llmProviderKey) {
      setModels([]);
      setProviderName("");
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const providersResp = await ListLLMProviders();
      const prov = (providersResp.items ?? []).find(
        (p) => p.providerKey === llmProviderKey,
      );
      if (!prov) {
        setProviderName("");
        setModels([]);
        setError(i18n.t("modelPill.errorProvider"));
        return;
      }
      setProviderName(prov.name);
      const modelsResp = await ListLLMModels(
        llm_provider_svc.ListModelsRequest.createFrom({ id: prov.id }),
      );
      setModels(modelsResp.items ?? []);
    } catch {
      setModels([]);
      setError(i18n.t("modelPill.errorFetch"));
    } finally {
      setLoading(false);
    }
  }, [llmProviderKey]);

  // session / provider 切换时重置本地状态并（已绑时）重拉模型列表。
  React.useEffect(() => {
    setOverrideState(initialOverride ?? "");
    lastOverrideRef.current = initialOverride ?? "";
    setError(null);
    void fetchModels();
    // 注意: fetchModels 依赖 llmProviderKey, 这里不再重复列 sessionId ——
    // 会话切换时 llmProviderKey 必然跟着变（新建态 = agent key）。
  }, [sessionId, initialOverride, fetchModels]);

  const setOverride = React.useCallback(
    (model: string) => {
      const next = model.trim();
      if (next === override) return;
      const previous = lastOverrideRef.current;
      setOverrideState(next);
      lastOverrideRef.current = next;
      setError(null);

      if (!isExistingSession) {
        return;
      }
      void SetChatSessionModel(sessionId as number, next)
        .then(() => {
          setError(null);
        })
        .catch(() => {
          // 落库失败 → 回滚到上一次成功值, pill 立即恢复。
          setOverrideState(previous);
          lastOverrideRef.current = previous;
          setError(i18n.t("modelPill.errorPersist"));
        });
    },
    [override, isExistingSession, sessionId],
  );

  return {
    override,
    setOverride,
    models,
    loading,
    error,
    bound,
    unboundExisting,
    interactive: !unboundExisting,
    providerName,
    effectiveModel: override || providerDefaultModel || "",
  };
}
