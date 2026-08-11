import { llm_provider_svc } from "../../../../wailsjs/go/models";

export type Provider = llm_provider_svc.ProviderItem;
export type Model = llm_provider_svc.ModelItem;
export type ModelInfo = llm_provider_svc.ModelInfo;
export type ReferenceCounts = llm_provider_svc.ReferenceCounts;

export type ProviderType = "anthropic" | "openai-chat" | "openai-response";

export type ProviderTypeMeta = {
  badge: string;
  defaultBaseUrl: string;
  tone: "dark" | "green" | "blue";
};

export const providerTypeMeta: Record<ProviderType, ProviderTypeMeta> = {
  anthropic: {
    badge: "A",
    defaultBaseUrl: "https://api.anthropic.com",
    tone: "dark",
  },
  "openai-chat": {
    // 两个 openai 变体首字母都是 O，用 OC/OR 区分。
    badge: "OC",
    defaultBaseUrl: "https://api.openai.com/v1",
    tone: "green",
  },
  "openai-response": {
    badge: "OR",
    defaultBaseUrl: "https://api.openai.com/v1",
    tone: "blue",
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

export function badgeToneClass(tone: ProviderTypeMeta["tone"]): string {
  switch (tone) {
    case "green":
      return "bg-agent-3 text-primary-foreground";
    case "blue":
      return "bg-agent-2 text-primary-foreground";
    case "dark":
    default:
      return "bg-foreground text-background";
  }
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

export function endpointFor(provider: Provider): string {
  const providerType = provider.type as ProviderType;
  const meta =
    providerType in providerTypeMeta
      ? providerTypeMeta[providerType]
      : undefined;
  return provider.baseUrl || meta?.defaultBaseUrl || "—";
}
