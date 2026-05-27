// frontend/src/components/agentre/remote-devices/device-providers-sync.tsx
// Inline provider-sync sub-panel rendered beneath a DeviceRow when expanded.
import { useEffect, useState } from "react";
import { CheckCircle2, Copy, Loader2, XCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";

import {
  ListAgentBackends,
  ListLLMProviders,
  RemoteDeviceListProviders,
} from "../../../../wailsjs/go/app/App";

// ── Local shims (wailsjs/ is gitignored; types match the Go JSON shapes) ──────

type ProviderSummary = { key: string; name: string; type: string };
type BackendItem = {
  deviceID?: string;
  deviceId?: string;
  llmProviderKey?: string;
};
type ProviderItem = {
  providerKey?: string;
  name: string;
  type: string;
  baseUrl?: string;
  model?: string;
};

// ── helpers ──────────────────────────────────────────────────────────────────

function buildFixCommand(
  key: string,
  name: string,
  type: string,
  baseUrl?: string,
  model?: string,
): string {
  let cmd = `agentred llm add --key=${key} --name="${name}" --type=${type} --api-key=<API_KEY>`;
  if (baseUrl && baseUrl.trim() !== "") {
    cmd += ` --base-url=${baseUrl.trim()}`;
  }
  if (model && model.trim() !== "") {
    cmd += ` --model=${model.trim()}`;
  }
  return cmd;
}

// ── types ─────────────────────────────────────────────────────────────────────

type ProviderRow = {
  key: string;
  name: string;
  type: string;
  baseUrl: string;
  model: string;
  synced: boolean;
};

type SyncState =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | { phase: "ready"; rows: ProviderRow[] };

// ── component ─────────────────────────────────────────────────────────────────

type Props = { deviceId: number };

export function DeviceProvidersSync({ deviceId }: Props) {
  const [state, setState] = useState<SyncState>({ phase: "idle" });
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    setState({ phase: "loading" });

    void Promise.all([
      RemoteDeviceListProviders(deviceId),
      ListAgentBackends(),
      ListLLMProviders(),
    ])
      .then(([remoteRaw, backendsResp, providersResp]) => {
        if (!mounted) return;

        // Remote keys set
        const remote = remoteRaw as ProviderSummary[] | null | undefined;
        const remoteKeys = new Set<string>(
          (remote ?? []).map((p) => p.key).filter(Boolean),
        );

        // Local backends filtered to this device
        const backends = (backendsResp?.items ?? []) as BackendItem[];
        const deviceIdStr = String(deviceId);
        const localKeys = new Set<string>(
          backends
            .filter((b) => {
              // Wails may expose it as deviceID or deviceId; handle both
              const bid = b.deviceID ?? b.deviceId ?? "";
              return bid !== "" && bid === deviceIdStr;
            })
            .map((b) => b.llmProviderKey ?? "")
            .filter(Boolean),
        );

        // Provider details map for fix-command construction
        const providers = (providersResp?.items ?? []) as ProviderItem[];
        const providerMap = new Map<string, ProviderItem>();
        for (const p of providers) {
          if (p.providerKey) providerMap.set(p.providerKey, p);
        }

        const rows: ProviderRow[] = [];
        for (const key of localKeys) {
          const p = providerMap.get(key);
          rows.push({
            key,
            name: p?.name ?? key,
            type: p?.type ?? "unknown",
            baseUrl: p?.baseUrl ?? "",
            model: p?.model ?? "",
            synced: remoteKeys.has(key),
          });
        }

        setState({ phase: "ready", rows });
      })
      .catch((err: unknown) => {
        if (!mounted) return;
        const msg = err instanceof Error ? err.message : String(err);
        setState({ phase: "error", message: msg });
      });

    return () => {
      mounted = false;
    };
  }, [deviceId]);

  if (state.phase === "idle" || state.phase === "loading") {
    return (
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
        正在加载 Provider 同步状态…
      </div>
    );
  }

  if (state.phase === "error") {
    return (
      <div className="px-3 py-2 text-xs text-destructive">
        加载失败：{state.message}
      </div>
    );
  }

  const { rows } = state;

  if (rows.length === 0) {
    return (
      <div className="px-3 py-2 text-xs text-muted-foreground">
        该设备上没有关联任何本地 Agent 后端 Provider。
      </div>
    );
  }

  function handleCopy(cmd: string, key: string) {
    void copyTextWithToast(cmd, {
      errorTitle: "复制修复命令失败",
      successTitle: "已复制修复命令",
      successDescription: "粘贴到远端 agentred 所在终端执行",
    }).then((copied) => {
      if (!copied) return;
      setCopied(key);
      setTimeout(() => setCopied(null), 2000);
    });
  }

  return (
    <div
      data-testid="device-providers-sync"
      className="flex flex-col gap-1.5 rounded-md border border-border bg-secondary/40 px-3 py-2"
    >
      <span className="text-2xs font-semibold text-muted-foreground uppercase tracking-wide">
        Provider 同步状态
      </span>
      <div className="flex flex-col gap-1">
        {rows.map((row) => {
          const fixCmd = buildFixCommand(
            row.key,
            row.name,
            row.type,
            row.baseUrl,
            row.model,
          );
          const isCopied = copied === row.key;
          return (
            <div key={row.key} className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                {row.synced ? (
                  <CheckCircle2
                    className="h-3.5 w-3.5 shrink-0 text-emerald-500"
                    aria-label="已同步"
                  />
                ) : (
                  <XCircle
                    className="h-3.5 w-3.5 shrink-0 text-destructive"
                    aria-label="未同步"
                  />
                )}
                <span className="text-xs font-medium truncate">{row.name}</span>
                <span className="font-mono text-2xs text-muted-foreground">
                  {row.type}
                </span>
                {!row.synced ? (
                  <span
                    data-testid={`missing-badge-${row.key}`}
                    className="ml-auto font-mono text-2xs text-destructive"
                  >
                    缺失
                  </span>
                ) : null}
              </div>
              {!row.synced ? (
                <div className="ml-5 flex items-center gap-1.5">
                  <code
                    data-testid={`fix-cmd-${row.key}`}
                    className="flex-1 rounded bg-muted px-2 py-1 font-mono text-2xs break-all"
                  >
                    {fixCmd}
                  </code>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`复制修复命令 ${row.key}`}
                    title={isCopied ? "已复制" : "复制命令"}
                    className="h-6 w-6 shrink-0 text-muted-foreground"
                    onClick={() => handleCopy(fixCmd, row.key)}
                  >
                    {isCopied ? (
                      <CheckCircle2
                        className="h-3 w-3 text-emerald-500"
                        aria-hidden="true"
                      />
                    ) : (
                      <Copy className="h-3 w-3" aria-hidden="true" />
                    )}
                  </Button>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
