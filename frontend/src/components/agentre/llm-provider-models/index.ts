import { llm_provider_svc } from "../../../../wailsjs/go/models";

export type Provider = llm_provider_svc.ProviderItem;
export type Model = llm_provider_svc.ModelItem;
export type ModelInfo = llm_provider_svc.ModelInfo;
export type ReferenceCounts = llm_provider_svc.ReferenceCounts;

export type ProviderType = "anthropic" | "openai-chat" | "openai-response";

export type ProviderTypeMeta = {
  badge: string;
  defaultBaseUrl: string;
};

export const providerTypeMeta: Record<ProviderType, ProviderTypeMeta> = {
  anthropic: {
    badge: "A",
    defaultBaseUrl: "https://api.anthropic.com",
  },
  "openai-chat": {
    // 两个 openai 变体首字母都是 O，用 OC/OR 区分。
    badge: "OC",
    defaultBaseUrl: "https://api.openai.com/v1",
  },
  "openai-response": {
    badge: "OR",
    defaultBaseUrl: "https://api.openai.com/v1",
  },
};

export const providerTypeOrder: ProviderType[] = [
  "anthropic",
  "openai-response",
  "openai-chat",
];

export function isProviderType(value: string): value is ProviderType {
  return value in providerTypeMeta;
}

export function errMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "Unknown error";
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}K`;
  }
  return n > 0 ? String(n) : "—";
}

export function totalReferences(
  counts: ReferenceCounts | null | undefined,
): number {
  if (!counts) return 0;
  return counts.backends + counts.sessions + counts.routes;
}

export type ModelDeleteability =
  | { kind: "ok" }
  | { kind: "default" }
  | { kind: "referenced"; count: number };

// 唯一的「可否删除」判定来源：默认模型不可删、被引用不可删、其余可删。
// 模型表行内标注与批量删除确认分组共用这一判定，避免另起一套规则。
export function modelDeleteability(
  model: Model,
  defaultModelKey: string,
  modelRefCounts: Map<string, ReferenceCounts>,
): ModelDeleteability {
  if (model.modelKey === defaultModelKey) return { kind: "default" };
  const count = totalReferences(modelRefCounts.get(model.modelKey));
  if (count > 0) return { kind: "referenced", count };
  return { kind: "ok" };
}

export function endpointFor(provider: Provider): string {
  const providerType = provider.type as ProviderType;
  const meta =
    providerType in providerTypeMeta
      ? providerTypeMeta[providerType]
      : undefined;
  return provider.baseUrl || meta?.defaultBaseUrl || "—";
}
