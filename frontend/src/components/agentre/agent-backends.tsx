import * as React from "react";
import {
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  Hammer,
  Loader2,
  Pencil,
  Plus,
  Puzzle,
  Radar,
  SendHorizontal,
  Sparkles,
  Trash2,
  Wand2,
  X,
} from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";

import { truncateFlashText } from "./agent-backends-utils";
import {
  CancelTestAgentBackend,
  CreateAgentBackend,
  DeleteAgentBackend,
  GetGatewayStatus,
  ListAgentBackends,
  ListLLMProviders,
  RemoteDeviceList,
  RemoteDeviceListProviders,
  RemoteDeviceSyncProvider,
  ResolveAgentBackendCLIPath,
  TestAgentBackend,
  UpdateAgentBackend,
} from "../../../wailsjs/go/app/App";
import {
  agent_backend_svc,
  httpgateway,
  llm_provider_svc,
} from "../../../wailsjs/go/models";
import { AgentreDialog } from "./app-dialog";

type Backend = agent_backend_svc.BackendItem;
type Provider = llm_provider_svc.ProviderItem;
type BackendType = "builtin" | "claudecode" | "codex";

// DeviceView — local shim matching remote_device_svc.DeviceView.
// wailsjs/go/models is generated at build time and not present in this worktree.
type DeviceView = { id: number; name: string; online: boolean };
type ProviderSummary = { key?: string; name?: string; type?: string };

const backendTypeMeta: Record<
  BackendType,
  {
    description: string;
    disabled: boolean;
    icon: typeof Puzzle;
    label: string;
  }
> = {
  builtin: {
    label: "内置 Agent",
    description: "由系统直接调 LLM 并执行工具调用（cago app/coding）",
    icon: Puzzle,
    disabled: false,
  },
  claudecode: {
    label: "Claude Code CLI",
    description:
      "包装本地 claude CLI；通过 App 内置 HTTP 代理转发到 anthropic 类型 LLM 供应商",
    icon: Hammer,
    disabled: false,
  },
  codex: {
    label: "Codex CLI",
    description:
      "包装本地 codex CLI；通过 OpenAI Responses API 匹配 openai-response 类型供应商",
    icon: Wand2,
    disabled: false,
  },
};

type EditorState =
  | { kind: "closed" }
  | { kind: "create" }
  | { kind: "edit"; backend: Backend };

type FlashState =
  | { kind: "ok"; text: string }
  | { kind: "err"; text: string }
  | null;

type SandboxValue = "" | "read-only" | "workspace-write" | "danger-full-access";
type ApprovalValue = "" | "untrusted" | "on-failure" | "on-request" | "never";
type ReasoningEffortValue = "" | "low" | "medium" | "high" | "xhigh" | "max";
type BackendDraft = {
  type: BackendType;
  name: string;
  deviceId: string;
  llmProviderKey: string;
  cliPath: string;
  modelRoutes: string;
  sandbox: string;
  approval: string;
  envJson: string;
  reasoningEffort: ReasoningEffortValue;
  defaultPermissionMode: string;
};
type PendingProviderSync = {
  draft: BackendDraft;
  providerKeys: string[];
  saveAfterSync: boolean;
};

// codex CLI 支持到 xhigh；UI 不暴露 max，避免「保存了 max 实际上下发 high」的迷惑。
// 类型切到 codex 时若历史值是 max，会自动降为 high（buildDraft / handleTypeChange）。
const REASONING_EFFORTS_FULL: ReasoningEffortValue[] = [
  "",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
];
const REASONING_EFFORTS_CODEX: ReasoningEffortValue[] = [
  "",
  "low",
  "medium",
  "high",
  "xhigh",
];
const REASONING_EFFORT_LABELS: Record<ReasoningEffortValue, string> = {
  "": "默认（由模型决定）",
  low: "低 · low",
  medium: "中 · medium",
  high: "高 · high",
  xhigh: "极高 · xhigh",
  max: "顶格 · max",
};

function normalizeForCodex(v: ReasoningEffortValue): ReasoningEffortValue {
  return v === "max" ? "high" : v;
}

type EnvEntry = { key: string; value: string };

const CLAUDE_TIERS = ["OPUS", "SONNET", "HAIKU"] as const;
type ClaudeTier = (typeof CLAUDE_TIERS)[number];

const APPROVAL_OPTIONS: {
  value: Exclude<ApprovalValue, "">;
  label: string;
}[] = [
  { value: "untrusted", label: "仅信任的工具自动执行" },
  { value: "on-failure", label: "工具失败时人工确认" },
  { value: "on-request", label: "模型请求时人工确认" },
  { value: "never", label: "从不需要人工确认" },
];

const SANDBOX_OPTIONS: {
  value: Exclude<SandboxValue, "">;
  label: string;
}[] = [
  { value: "read-only", label: "read-only" },
  { value: "workspace-write", label: "workspace-write" },
  { value: "danger-full-access", label: "danger-full-access" },
];

const RESERVED_ENV_KEYS = new Set([
  "ANTHROPIC_BASE_URL",
  "ANTHROPIC_API_KEY",
  "ANTHROPIC_AUTH_TOKEN",
  "ANTHROPIC_MODEL",
  "ANTHROPIC_DEFAULT_OPUS_MODEL",
  "ANTHROPIC_DEFAULT_SONNET_MODEL",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL",
  "OPENAI_API_KEY",
  "OPENAI_BASE_URL",
  "OPENAI_API_BASE",
]);

const LOCAL_DEVICE_SELECT_VALUE = "__local_device__";

function deviceIdToSelectValue(deviceId: string): string {
  return deviceId === "" ? LOCAL_DEVICE_SELECT_VALUE : deviceId;
}

function selectValueToDeviceId(value: string): string {
  return value === LOCAL_DEVICE_SELECT_VALUE ? "" : value;
}

function matchingProviders(t: BackendType, providers: Provider[]) {
  if (t === "claudecode")
    return providers.filter((p) => p.type === "anthropic");
  if (t === "codex")
    return providers.filter((p) => p.type === "openai-response");
  return providers;
}

function strictMatchLabel(
  t: BackendType,
  providerType?: string,
): string | null {
  if (t === "claudecode") return "anthropic";
  if (t === "codex") {
    if (providerType === "openai-response") return "openai-response";
    return "openai-response";
  }
  return null;
}

function safeParseRoutes(s: string): Record<string, string> {
  try {
    const obj = JSON.parse(s || "{}");
    if (!obj || typeof obj !== "object") return {};
    // Normalize: values may be legacy numeric IDs or new string keys — always stringify.
    return Object.fromEntries(
      Object.entries(obj as Record<string, unknown>).map(([k, v]) => [
        k,
        String(v ?? ""),
      ]),
    );
  } catch {
    return {};
  }
}

function safeParseEnv(s: string): EnvEntry[] {
  try {
    const obj = JSON.parse(s || "{}");
    if (!obj || typeof obj !== "object") return [];
    return Object.entries(obj as Record<string, unknown>).map(
      ([key, value]) => ({ key, value: String(value ?? "") }),
    );
  } catch {
    return [];
  }
}

function serializeRoutes(routes: Record<ClaudeTier, string>): string {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(routes)) {
    if (v && v.trim() !== "") {
      out[k] = v.trim();
    }
  }
  return Object.keys(out).length === 0 ? "{}" : JSON.stringify(out);
}

function serializeEnv(entries: EnvEntry[]): string {
  const out: Record<string, string> = {};
  for (const e of entries) {
    const k = e.key.trim();
    if (!k) continue;
    out[k] = e.value;
  }
  return Object.keys(out).length === 0 ? "{}" : JSON.stringify(out);
}

function emptyRoutes(): Record<ClaudeTier, string> {
  return { OPUS: "", SONNET: "", HAIKU: "" };
}

function referencedProviderKeys(draft: BackendDraft): string[] {
  const keys = new Set<string>();
  if (draft.llmProviderKey.trim() !== "") {
    keys.add(draft.llmProviderKey.trim());
  }
  if (draft.type === "claudecode") {
    const routes = safeParseRoutes(draft.modelRoutes);
    for (const value of Object.values(routes)) {
      const key = value.trim();
      if (key !== "") keys.add(key);
    }
  }
  return Array.from(keys);
}

function providerLabel(key: string, providers: Provider[]): string {
  const p = providers.find(
    (item) => item.providerKey === key || String(item.id) === key,
  );
  if (!p) return key;
  return p.model ? `${p.name} · ${p.model}` : p.name;
}

export function AgentBackendsPanel({
  onOpenProxySettings,
}: {
  onOpenProxySettings?: () => void;
} = {}) {
  const [backends, setBackends] = React.useState<Backend[]>([]);
  const [providers, setProviders] = React.useState<Provider[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [editor, setEditor] = React.useState<EditorState>({ kind: "closed" });
  const [pendingDelete, setPendingDelete] = React.useState<Backend | null>(
    null,
  );
  const [flash, setFlash] = React.useState<FlashState>(null);
  const [testingId, setTestingId] = React.useState<number | null>(null);
  // 当前正在跑的 TestAgentBackend 的 requestId；用户点取消时拿它去后端 CancelTest。
  // 用 ref 而不是 state，避免 await TestAgentBackend 拿到的是闭包里的旧值。
  const testReqIdRef = React.useRef<string | null>(null);

  function flashFromTestResponse(res: agent_backend_svc.TestBackendResponse) {
    if (res.ok) {
      setFlash({
        kind: "ok",
        text: `✅ ${res.latencyMs}ms · ${res.message}`,
      });
    } else {
      setFlash({ kind: "err", text: `❌ ${res.message}` });
    }
  }

  async function handleTestRow(id: number) {
    if (testingId !== null) return;
    const requestId = newRequestId();
    testReqIdRef.current = requestId;
    setTestingId(id);
    try {
      const res = await TestAgentBackend({
        id,
        useDraft: false,
        type: "",
        name: "",
        llmProviderKey: "",
        cliPath: "",
        requestId,
      } as agent_backend_svc.TestBackendRequest);
      // 用户在等待期间点了取消 → testReqIdRef 已被清掉，丢弃 stale 响应。
      if (testReqIdRef.current !== requestId) return;
      flashFromTestResponse(res);
    } catch (err) {
      if (testReqIdRef.current !== requestId) return;
      setFlash({ kind: "err", text: messageFromError(err) });
    } finally {
      if (testReqIdRef.current === requestId) {
        testReqIdRef.current = null;
        setTestingId(null);
      }
    }
  }

  async function handleCancelRow() {
    const requestId = testReqIdRef.current;
    if (!requestId) return;
    // 立刻清前端 in-flight 状态，UI 即时恢复。Backend 收到 cancel 后 prober ctx Done，
    // 那个 await TestAgentBackend 还会返回，但 stale 检测会丢弃它。
    testReqIdRef.current = null;
    setTestingId(null);
    try {
      await CancelTestAgentBackend({
        requestId,
      } as agent_backend_svc.CancelTestBackendRequest);
    } catch {
      // best effort — 后端不响应 cancel 也别刷红 flash。
    }
  }

  const reload = React.useCallback(async () => {
    setLoading(true);
    try {
      const [b, p] = await Promise.all([
        ListAgentBackends(),
        ListLLMProviders(),
      ]);
      setBackends(b?.items ?? []);
      setProviders(p?.items ?? []);
    } catch (err) {
      setFlash({ kind: "err", text: messageFromError(err) });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    let mounted = true;
    Promise.all([ListAgentBackends(), ListLLMProviders()])
      .then(([b, p]) => {
        if (!mounted) return;
        setBackends(b?.items ?? []);
        setProviders(p?.items ?? []);
      })
      .catch((err: unknown) => {
        if (!mounted) return;
        setFlash({ kind: "err", text: messageFromError(err) });
      })
      .finally(() => {
        if (!mounted) return;
        setLoading(false);
      });
    return () => {
      mounted = false;
    };
    // reload is for explicit refreshes only; initial load runs directly
  }, []);

  return (
    <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
      <Toolbar
        count={backends.length}
        onCreate={() => setEditor({ kind: "create" })}
      />
      {flash ? (
        <FlashBanner state={flash} onDismiss={() => setFlash(null)} />
      ) : null}
      <div data-slot="table-container" className="min-w-0 overflow-x-auto">
        <Table aria-label="Agent 后端列表" className="min-w-[980px]">
          <TableHeader>
            <TableRow className="bg-secondary hover:bg-secondary">
              <TableHead className="w-[260px] px-4 font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                名称
              </TableHead>
              <TableHead className="w-[180px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                类型
              </TableHead>
              <TableHead className="min-w-[260px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                CLI / 标识
              </TableHead>
              <TableHead className="w-[250px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                模型 / 供应商
              </TableHead>
              <TableHead className="w-[100px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="py-6 text-center text-xs text-muted-foreground"
                >
                  <Loader2
                    className="mr-2 inline size-3.5 animate-spin"
                    aria-hidden="true"
                  />
                  加载中...
                </TableCell>
              </TableRow>
            ) : backends.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="py-8 text-center text-xs text-muted-foreground"
                >
                  <span data-selectable-text="true">
                    还没有 Agent 后端，点右上「+ 新增后端」开始配置
                  </span>
                </TableCell>
              </TableRow>
            ) : (
              backends.map((b) => (
                <BackendRow
                  key={b.id}
                  backend={b}
                  testing={testingId === b.id}
                  testDisabled={testingId !== null}
                  onTest={() => handleTestRow(b.id)}
                  onCancelTest={handleCancelRow}
                  onEdit={() => setEditor({ kind: "edit", backend: b })}
                  onDelete={() => setPendingDelete(b)}
                />
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {editor.kind !== "closed" ? (
        <BackendEditor
          state={editor}
          providers={providers}
          onClose={() => setEditor({ kind: "closed" })}
          onSaved={async (message) => {
            setEditor({ kind: "closed" });
            setFlash({ kind: "ok", text: message });
            await reload();
          }}
          onError={(text) => setFlash({ kind: "err", text })}
          onOpenProxySettings={onOpenProxySettings}
        />
      ) : null}

      {pendingDelete ? (
        <DeleteDialog
          backend={pendingDelete}
          onCancel={() => setPendingDelete(null)}
          onConfirmed={async () => {
            setPendingDelete(null);
            setFlash({ kind: "ok", text: "已删除" });
            await reload();
          }}
          onError={(text) => {
            setPendingDelete(null);
            setFlash({ kind: "err", text });
          }}
        />
      ) : null}
    </section>
  );
}

function Toolbar({ count, onCreate }: { count: number; onCreate: () => void }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-3 sm:px-4">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-semibold">已配置的后端</span>
        <span className="text-2xs text-muted-foreground">共 {count} 个</span>
      </div>
      <Button
        type="button"
        size="sm"
        className="h-[30px] gap-1.5 px-3 text-xs"
        onClick={onCreate}
      >
        <Plus data-icon="inline-start" aria-hidden="true" />
        新增后端
      </Button>
    </div>
  );
}

function BackendRow({
  backend,
  testing,
  testDisabled,
  onTest,
  onCancelTest,
  onEdit,
  onDelete,
}: {
  backend: Backend;
  testing: boolean;
  testDisabled: boolean;
  onTest: () => void;
  onCancelTest: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const typ = (backend.type as BackendType) ?? "builtin";
  const meta = backendTypeMeta[typ] ?? backendTypeMeta.builtin;
  const Icon = meta.icon;
  const cliBased = typ === "claudecode" || typ === "codex";
  // 未关联 provider 的 CLI 后端 = 走 CLI 自身 login，不算需处理。
  const unlinkedCli =
    cliBased &&
    !((backend as unknown as { llmProviderKey?: string }).llmProviderKey ?? "");
  const providerLabel = unlinkedCli
    ? "走 CLI 自身登录"
    : backend.llmProviderName
      ? `${backend.llmProviderName} · ${backend.llmProviderModel || "（未选模型）"}`
      : "未关联供应商";
  const warning = !unlinkedCli && !backend.llmProviderActive;

  return (
    <TableRow className="hover:bg-accent/45">
      <TableCell className="px-4 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <span
            className={cn(
              "inline-flex size-1.5 rounded-full",
              warning ? "bg-status-waiting" : "bg-status-running",
            )}
            aria-hidden="true"
          />
          <div className="flex min-w-0 flex-col gap-0.5">
            <div className="flex min-w-0 items-center gap-1.5">
              <span
                data-selectable-text="true"
                className="truncate text-sm font-medium"
              >
                {backend.name}
              </span>
              {warning ? (
                <Badge
                  variant="secondary"
                  className="rounded-sm bg-status-waiting-bg px-1.5 py-0 font-mono text-2xs text-status-waiting"
                >
                  需处理
                </Badge>
              ) : null}
            </div>
            <span className="font-mono text-2xs text-subtle-foreground">
              {backend.agentCount > 0
                ? `${backend.agentCount} agents 使用中`
                : "暂未被 Agent 使用"}
            </span>
          </div>
        </div>
      </TableCell>
      <TableCell className="py-3 text-xs">
        <span className="inline-flex items-center gap-1.5">
          <Icon
            className="size-3.5 shrink-0 text-primary-text"
            aria-hidden="true"
          />
          {meta.label}
        </span>
      </TableCell>
      <TableCell className="py-3 text-xs text-muted-foreground">—</TableCell>
      <TableCell className="py-3">
        <span className="inline-flex min-w-0 items-center gap-1.5 font-mono text-2xs">
          <Sparkles className="size-3 shrink-0 text-muted-foreground" />
          <span data-selectable-text="true" className="truncate">
            {providerLabel}
          </span>
        </span>
      </TableCell>
      <TableCell className="px-4 py-3">
        <div className="flex justify-end gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            // testing 时按钮变成"取消测试"，必须保持可点击；其它行 testDisabled 仍 disable。
            aria-label={
              testing ? `取消测试 ${backend.name}` : `测试连接 ${backend.name}`
            }
            title={testing ? "取消测试" : "测试连接"}
            className={cn(
              "size-[26px]",
              testing ? "text-status-error" : "text-muted-foreground",
            )}
            disabled={testDisabled && !testing}
            onClick={testing ? onCancelTest : onTest}
          >
            {testing ? (
              <X data-icon="only" aria-hidden="true" />
            ) : (
              <SendHorizontal data-icon="only" aria-hidden="true" />
            )}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`编辑 ${backend.name}`}
            title="编辑"
            className="size-[26px] text-muted-foreground"
            onClick={onEdit}
          >
            <Pencil data-icon="only" aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`删除 ${backend.name}`}
            title="删除"
            className="size-[26px] text-status-error"
            onClick={onDelete}
          >
            <Trash2 data-icon="only" aria-hidden="true" />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function BackendEditor({
  state,
  providers,
  onClose,
  onSaved,
  onError,
  onOpenProxySettings,
}: {
  state: EditorState;
  providers: Provider[];
  onClose: () => void;
  onSaved: (message: string) => Promise<void> | void;
  onError: (text: string) => void;
  onOpenProxySettings?: () => void;
}) {
  const editing = state.kind === "edit" ? state.backend : null;
  const initialType: BackendType = (editing?.type as BackendType) ?? "builtin";

  const [type, setType] = React.useState<BackendType>(initialType);
  const [name, setName] = React.useState(editing?.name ?? "");
  const [cliPath, setCliPath] = React.useState(editing?.cliPath ?? "");
  const [llmProviderKey, setLlmProviderKey] = React.useState<string>(
    () =>
      (editing as unknown as { llmProviderKey?: string } | null)
        ?.llmProviderKey ?? "",
  );
  const [routes, setRoutes] = React.useState<Record<ClaudeTier, string>>(() => {
    const parsed = safeParseRoutes(editing?.modelRoutes ?? "");
    const next = emptyRoutes();
    for (const tier of CLAUDE_TIERS) {
      const v = parsed[tier];
      if (v) next[tier] = v;
    }
    return next;
  });
  const [sandbox, setSandbox] = React.useState<SandboxValue>(
    (editing?.sandbox as SandboxValue) ?? "",
  );
  const [approval, setApproval] = React.useState<ApprovalValue>(
    (editing?.approval as ApprovalValue) ?? "",
  );
  const [envEntries, setEnvEntries] = React.useState<EnvEntry[]>(() =>
    safeParseEnv(editing?.envJson ?? ""),
  );
  const [reasoningEffort, setReasoningEffort] =
    React.useState<ReasoningEffortValue>(
      // BackendItem.reasoningEffort 在 Wails 重新生成绑定前还未出现在 TS 类型里；
      // 这里用宽类型读出。后端 entity.Check 已经把非法值挡掉，所以兜底空串。
      ((editing as unknown as { reasoningEffort?: string } | null)
        ?.reasoningEffort as ReasoningEffortValue) || "",
    );
  const [defaultPermissionMode, setDefaultPermissionMode] =
    React.useState<string>(
      ((editing as unknown as { defaultPermissionMode?: string } | null)
        ?.defaultPermissionMode as string) || "",
    );
  const [deviceId, setDeviceId] = React.useState<string>(
    // BackendItem.deviceID may not yet appear in the Wails-generated TS type;
    // use unknown cast to read it safely. Empty string = local.
    (editing as unknown as { deviceID?: string } | null)?.deviceID ?? "",
  );
  const [devices, setDevices] = React.useState<DeviceView[]>([]);
  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [pendingProviderSync, setPendingProviderSync] =
    React.useState<PendingProviderSync | null>(null);
  const [providerSyncError, setProviderSyncError] = React.useState<
    string | null
  >(null);
  const [syncingProvider, setSyncingProvider] = React.useState(false);
  const [testing, setTesting] = React.useState(false);
  const [testResult, setTestResult] = React.useState<FlashState>(null);
  const [gatewayStatus, setGatewayStatus] =
    React.useState<httpgateway.GatewayStatus | null>(null);
  const [cliProbing, setCliProbing] = React.useState(false);
  // 「$PATH 没挂到 binary」的提示文案；命中后清空。
  const [cliProbeMiss, setCliProbeMiss] = React.useState<string | null>(null);

  const filteredProviders = React.useMemo(
    () => matchingProviders(type, providers),
    [type, providers],
  );

  const autoProviderKey =
    state.kind === "create" &&
    type === "builtin" &&
    llmProviderKey === "" &&
    filteredProviders[0]
      ? (filteredProviders[0].providerKey ?? String(filteredProviders[0].id))
      : "";
  const effectiveLlmProviderKey = llmProviderKey || autoProviderKey;

  // detectCLIPath 调后端 ResolveAgentBackendCLIPath；非 CLI 类型直接返回 null。
  // 选了远端 device 时把 deviceId 一起传过去，让 agent_backend_svc 按 device 派发到远端 daemon。
  // 注意：远端调用可能 throw（设备离线 / 超时 / 探测失败），调用方需要自行决定要不要兜底。
  // - handleTypeChange 的隐式自动填：用 .catch(() => undefined) 静默吞错，避免打扰新建流程
  // - handleDetectCli 的显式按钮：catch 后落到 cliProbeMiss 文案槽
  async function detectCLIPath(
    t: BackendType,
    dev: string = "",
  ): Promise<string | null> {
    if (t !== "claudecode" && t !== "codex") return null;
    const r = await ResolveAgentBackendCLIPath({
      type: t,
      deviceId: dev,
    } as agent_backend_svc.ResolveCLIPathRequest);
    return r.found ? r.path : null;
  }

  function handleTypeChange(nextType: BackendType) {
    setType(nextType);
    setLlmProviderKey("");
    setRoutes(emptyRoutes());
    setSandbox("");
    setApproval("");
    setTestResult(null);
    // 切离 claudecode 时清空 default permission mode：entity.Check 仅放行 claudecode + 非空。
    if (nextType !== "claudecode") {
      setDefaultPermissionMode("");
    }
    // 切到 codex 时把 max 自动降到 high，避免「保存了一个 codex 不支持的档位」。
    if (nextType === "codex") {
      setReasoningEffort((cur) => normalizeForCodex(cur));
    }
    // 切类型时清空 cliPath，避免 claude / codex 两个不同的可执行文件串台。
    setCliPath("");
    setCliProbeMiss(null);
    // create 模式下，切到 CLI 类型自动尝试探测一次，命中就填进去；用户随时可手改/清空。
    // edit 模式 type 是 disabled 的，所以这里不会跑；编辑场景只靠 Input 旁的「自动识别」按钮。
    if (
      state.kind === "create" &&
      (nextType === "claudecode" || nextType === "codex")
    ) {
      void (async () => {
        // 新建流程的隐式自动填：静默吞错，远端不可达就当没识别到。
        const path = await detectCLIPath(nextType, deviceId).catch(() => null);
        if (path) setCliPath(path);
      })();
    }
  }

  // 手动「自动识别」按钮：无论命中与否都给用户视觉反馈。命中时覆盖当前值。
  async function handleDetectCli() {
    if (cliProbing) return;
    setCliProbing(true);
    setCliProbeMiss(null);
    try {
      const path = await detectCLIPath(type, deviceId);
      if (path) {
        setCliPath(path);
      } else {
        setCliProbeMiss(
          `$PATH 中未找到 ${type === "claudecode" ? "claude" : "codex"}，请手动填写`,
        );
      }
    } catch (e) {
      // 远端报错（设备离线 / 超时 / 探测失败）也要给用户反馈，避免 unhandled promise rejection。
      setCliProbeMiss(e instanceof Error ? e.message : String(e));
    } finally {
      setCliProbing(false);
    }
  }

  const cliBased = type === "claudecode" || type === "codex";
  React.useEffect(() => {
    if (!cliBased) return;
    let mounted = true;
    GetGatewayStatus()
      .then((s) => {
        if (mounted) setGatewayStatus(s);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [cliBased]);

  // Fetch paired remote devices when the dialog opens (or re-opens).
  React.useEffect(() => {
    if (state.kind === "closed") return;
    void RemoteDeviceList()
      .then((rows) => setDevices((rows ?? []) as unknown as DeviceView[]))
      .catch(() => setDevices([]));
  }, [state.kind]);

  const reservedOffenders = React.useMemo(
    () =>
      envEntries
        .map((e) => e.key.trim())
        .filter((k) => k && RESERVED_ENV_KEYS.has(k)),
    [envEntries],
  );

  const open = state.kind !== "closed";

  function buildDraft(): BackendDraft {
    // 三种 backend 都保留 reasoningEffort；codex 二次兜底 normalize（防止历史脏数据 / 跨 type 残留）。
    const effort: ReasoningEffortValue =
      type === "codex" ? normalizeForCodex(reasoningEffort) : reasoningEffort;
    return {
      type,
      name,
      // builtin 后端只能在本地运行（无 HTTP 网关路由到 daemon），强制清空以防误保存。
      deviceId: type === "builtin" ? "" : deviceId,
      llmProviderKey: effectiveLlmProviderKey,
      cliPath: type === "builtin" ? "" : cliPath.trim(),
      modelRoutes: type === "claudecode" ? serializeRoutes(routes) : "{}",
      sandbox: type === "codex" ? sandbox : "",
      approval: type === "codex" ? approval : "",
      envJson: type === "builtin" ? "{}" : serializeEnv(envEntries),
      reasoningEffort: effort,
      defaultPermissionMode: type === "claudecode" ? defaultPermissionMode : "",
    };
  }

  async function missingRemoteProviderKeys(
    draft: BackendDraft,
  ): Promise<string[]> {
    if (draft.deviceId === "") return [];
    const deviceID = Number(draft.deviceId);
    if (!Number.isFinite(deviceID) || deviceID <= 0) return [];

    const keys = referencedProviderKeys(draft);
    if (keys.length === 0) return [];

    const remoteRaw = (await RemoteDeviceListProviders(deviceID)) as
      | ProviderSummary[]
      | null
      | undefined;
    const remoteKeys = new Set(
      (remoteRaw ?? []).map((p) => p.key ?? "").filter(Boolean),
    );
    return keys.filter((key) => !remoteKeys.has(key));
  }

  async function saveDraft(draft: BackendDraft) {
    if (state.kind === "create") {
      await CreateAgentBackend({
        ...draft,
      } as agent_backend_svc.CreateBackendRequest);
      await onSaved("已新增 Agent 后端");
    } else if (state.kind === "edit" && editing) {
      await UpdateAgentBackend({
        id: editing.id,
        name: draft.name,
        deviceId: draft.deviceId,
        llmProviderKey: draft.llmProviderKey,
        cliPath: draft.cliPath,
        modelRoutes: draft.modelRoutes,
        sandbox: draft.sandbox,
        approval: draft.approval,
        envJson: draft.envJson,
        reasoningEffort: draft.reasoningEffort,
        defaultPermissionMode: draft.defaultPermissionMode,
      } as unknown as agent_backend_svc.UpdateBackendRequest);
      await onSaved("已保存");
    }
  }

  // 同 handleTestRow：用 ref 跟踪 in-flight requestId，方便点取消时丢弃 stale 响应。
  const testReqIdRef = React.useRef<string | null>(null);

  async function handleTest() {
    if (testing || submitting) return;
    if (reservedOffenders.length > 0) {
      setTestResult({
        kind: "err",
        text: `保留键禁用：${reservedOffenders.join(", ")}`,
      });
      setAdvancedOpen(true);
      return;
    }
    const requestId = newRequestId();
    testReqIdRef.current = requestId;
    setTesting(true);
    setTestResult(null);
    try {
      const draft = buildDraft();
      const res = await TestAgentBackend({
        id: state.kind === "edit" && editing ? editing.id : 0,
        useDraft: true,
        ...draft,
        requestId,
      } as agent_backend_svc.TestBackendRequest);
      if (testReqIdRef.current !== requestId) return;
      if (res.ok) {
        setTestResult({
          kind: "ok",
          text: `测试通过 · ${res.latencyMs}ms · ${res.message}`,
        });
      } else {
        setTestResult({ kind: "err", text: res.message });
      }
    } catch (err) {
      if (testReqIdRef.current !== requestId) return;
      setTestResult({ kind: "err", text: messageFromError(err) });
    } finally {
      if (testReqIdRef.current === requestId) {
        testReqIdRef.current = null;
        setTesting(false);
      }
    }
  }

  async function handleCancelTest() {
    const requestId = testReqIdRef.current;
    if (!requestId) return;
    testReqIdRef.current = null;
    setTesting(false);
    setTestResult(null);
    try {
      await CancelTestAgentBackend({
        requestId,
      } as agent_backend_svc.CancelTestBackendRequest);
    } catch {
      // best effort
    }
  }

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (submitting) return;
    if (reservedOffenders.length > 0) {
      onError(`保留键禁用：${reservedOffenders.join(", ")}`);
      setAdvancedOpen(true);
      return;
    }
    setSubmitting(true);
    try {
      const draft = buildDraft();
      const missingKeys = await missingRemoteProviderKeys(draft);
      if (missingKeys.length > 0) {
        setProviderSyncError(null);
        setPendingProviderSync({
          draft,
          providerKeys: missingKeys,
          saveAfterSync: true,
        });
        return;
      }
      await saveDraft(draft);
    } catch (err) {
      onError(messageFromError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function handleConfirmProviderSync() {
    if (!pendingProviderSync || syncingProvider) return;
    const deviceID = Number(pendingProviderSync.draft.deviceId);
    const saveAfterSync = pendingProviderSync.saveAfterSync;
    setSyncingProvider(true);
    setSubmitting(saveAfterSync);
    setProviderSyncError(null);
    try {
      for (const key of pendingProviderSync.providerKeys) {
        await RemoteDeviceSyncProvider(deviceID, key);
      }
      const draft = pendingProviderSync.draft;
      if (saveAfterSync) {
        await saveDraft(draft);
      } else {
        setPendingProviderSync(null);
        await onSaved("已同步远端 Provider");
      }
    } catch (err) {
      setProviderSyncError(providerSyncMessageFromError(err));
    } finally {
      setSyncingProvider(false);
      setSubmitting(false);
    }
  }

  function handleManualProviderSync() {
    const draft = buildDraft();
    if (draft.deviceId === "") return;
    const keys = referencedProviderKeys(draft);
    if (keys.length === 0) return;
    setProviderSyncError(null);
    setPendingProviderSync({
      draft,
      providerKeys: keys,
      saveAfterSync: false,
    });
  }

  function closeProviderSyncDialog() {
    setPendingProviderSync(null);
    setProviderSyncError(null);
  }

  const selectedProvider = filteredProviders.find(
    (p) =>
      (p.providerKey && p.providerKey === effectiveLlmProviderKey) ||
      String(p.id) === effectiveLlmProviderKey,
  );
  const strictLabel = strictMatchLabel(type, selectedProvider?.type);
  // builtin 必须有 provider；claudecode / codex 允许未关联（CLI 自身登录）。
  const providerOptional = type === "claudecode" || type === "codex";
  const submitDisabled =
    submitting ||
    (!providerOptional &&
      (filteredProviders.length === 0 || effectiveLlmProviderKey === "")) ||
    reservedOffenders.length > 0;
  const manualProviderSyncKeys =
    deviceId !== "" ? referencedProviderKeys(buildDraft()) : [];
  const showManualProviderSync =
    deviceId !== "" && manualProviderSyncKeys.length > 0;

  return (
    <>
      <AgentreDialog
        open={open}
        onOpenChange={(o) => (!o ? onClose() : undefined)}
        title={state.kind === "edit" ? "编辑 Agent 后端" : "新增 Agent 后端"}
        description="选择执行引擎并关联 LLM 供应商；CLI 后端通过本地 HTTP 代理转发请求，无需 claude/codex login。"
        contentClassName="max-w-xl"
        bodyClassName="flex flex-col gap-4"
        onSubmit={handleSubmit}
        footerClassName="flex-col items-stretch gap-2"
        footer={
          <>
            {testResult ? <TestResultPill state={testResult} /> : null}
            <div className="flex w-full items-center gap-2">
              {testing ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleCancelTest}
                  className="gap-1.5 text-status-error"
                >
                  <X className="size-3.5" aria-hidden="true" />
                  取消测试
                </Button>
              ) : (
                <Button
                  type="button"
                  variant="outline"
                  disabled={submitting || syncingProvider}
                  onClick={handleTest}
                  className="gap-1.5"
                >
                  <SendHorizontal className="size-3.5" aria-hidden="true" />
                  测试连接
                </Button>
              )}
              <div className="ml-auto flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  onClick={onClose}
                  disabled={submitting || syncingProvider}
                >
                  取消
                </Button>
                <Button
                  type="submit"
                  disabled={submitDisabled || syncingProvider}
                >
                  {submitting ? "保存中..." : "保存"}
                </Button>
              </div>
            </div>
          </>
        }
      >
        <label className="flex flex-col gap-1.5 text-xs">
          <span className="font-medium">名称</span>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="例如：本机 · Claude Code"
            required
            autoFocus
          />
        </label>

        <div className="flex flex-col gap-1.5 text-xs">
          <span className="font-medium">类型</span>
          <BackendTypeSegmented
            value={type}
            onChange={handleTypeChange}
            disabled={state.kind === "edit"}
          />
        </div>

        <div className="flex flex-col gap-1.5 text-xs">
          <span className="font-medium">运行设备</span>
          <Select
            value={deviceIdToSelectValue(deviceId)}
            onValueChange={(v) => setDeviceId(selectValueToDeviceId(v))}
            disabled={type === "builtin"}
          >
            <SelectTrigger aria-label="运行设备">
              <SelectValue placeholder="选择运行设备" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={LOCAL_DEVICE_SELECT_VALUE}>
                📍 本地（当前机器）
              </SelectItem>
              {devices.map((d) => (
                <SelectItem
                  key={d.id}
                  value={String(d.id)}
                  disabled={!d.online}
                >
                  📡 {d.name}
                  {d.online ? "" : " · offline"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {type === "builtin" ? (
            <span className="text-2xs text-muted-foreground">
              builtin 后端只能在本地运行
            </span>
          ) : null}
        </div>

        <LlmProviderField
          type={type}
          providers={filteredProviders}
          value={effectiveLlmProviderKey}
          onChange={setLlmProviderKey}
          strictLabel={strictLabel}
          editing={!!editing}
        />

        {showManualProviderSync ? (
          <Alert className="border-border bg-secondary text-xs">
            <Radar className="size-4" aria-hidden="true" />
            <AlertTitle className="text-xs">远端 Provider 同步</AlertTitle>
            <AlertDescription className="flex flex-col gap-2 text-2xs">
              <span>
                当前后端会在远端 agentred 运行；可先把所选 LLM Provider
                同步到远端状态文件。
              </span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="self-start"
                disabled={syncingProvider}
                onClick={handleManualProviderSync}
              >
                同步到远端
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}

        {cliBased ? (
          <CliPathField
            type={type}
            value={cliPath}
            onChange={(v) => {
              setCliPath(v);
              if (cliProbeMiss) setCliProbeMiss(null);
            }}
            onDetect={handleDetectCli}
            detecting={cliProbing}
            missMessage={cliProbeMiss}
          />
        ) : null}

        {type === "claudecode" ? (
          <ModelRoutesField
            providers={filteredProviders}
            routes={routes}
            onChange={setRoutes}
            mainProviderKey={llmProviderKey}
          />
        ) : null}

        {type === "claudecode" ? (
          <DefaultPermissionModeField
            value={defaultPermissionMode}
            onChange={setDefaultPermissionMode}
            isRemote={deviceId !== ""}
            hasIsSandbox={envEntries.some(
              (e) => e.key.trim() === "IS_SANDBOX" && e.value.trim() !== "",
            )}
            onAddIsSandbox={() => {
              setEnvEntries((prev) => {
                const idx = prev.findIndex(
                  (e) => e.key.trim() === "IS_SANDBOX",
                );
                if (idx >= 0) {
                  const next = prev.slice();
                  next[idx] = { key: "IS_SANDBOX", value: "1" };
                  return next;
                }
                return [...prev, { key: "IS_SANDBOX", value: "1" }];
              });
              // env_json 默认折叠;一键填后展开让用户能看见结果
              setAdvancedOpen(true);
            }}
          />
        ) : null}

        {type === "codex" ? (
          <>
            <SandboxField value={sandbox} onChange={setSandbox} />
            <ApprovalField value={approval} onChange={setApproval} />
          </>
        ) : null}

        <ReasoningEffortField
          type={type}
          value={reasoningEffort}
          onChange={setReasoningEffort}
        />

        {cliBased ? (
          <EnvJsonField
            entries={envEntries}
            onChange={setEnvEntries}
            open={advancedOpen}
            onToggle={() => setAdvancedOpen((o) => !o)}
            reservedOffenders={reservedOffenders}
          />
        ) : null}

        {cliBased ? (
          <ProxyNote
            status={gatewayStatus}
            providerLinked={llmProviderKey !== ""}
            onOpenProxySettings={onOpenProxySettings}
          />
        ) : null}
      </AgentreDialog>
      {pendingProviderSync ? (
        <AgentreDialog
          open
          onOpenChange={(o) =>
            !o && !syncingProvider ? closeProviderSyncDialog() : undefined
          }
          title="同步远端 LLM Provider"
          description={
            pendingProviderSync.saveAfterSync
              ? "所选远端 agentred 尚未配置这些 Provider。同步会把本机保存的 API Key 写入远端 agentred 状态文件，然后继续保存 Agent 后端。"
              : "同步会把本机保存的 API Key 写入远端 agentred 状态文件；不会保存表单其它改动。"
          }
          bodyClassName="flex flex-col gap-3"
          footer={
            <div className="flex w-full items-center justify-end gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={syncingProvider}
                onClick={closeProviderSyncDialog}
              >
                取消
              </Button>
              <Button
                type="button"
                disabled={syncingProvider}
                onClick={handleConfirmProviderSync}
              >
                {syncingProvider ? (
                  <Loader2
                    className="size-3.5 animate-spin"
                    aria-hidden="true"
                  />
                ) : null}
                {syncingProvider
                  ? "同步中..."
                  : pendingProviderSync.saveAfterSync
                    ? "同步并保存"
                    : "同步到远端"}
              </Button>
            </div>
          }
        >
          <Alert className="border-status-waiting/40 bg-status-waiting-bg text-xs">
            <AlertCircle className="size-4" aria-hidden="true" />
            <AlertTitle className="text-xs">需要先同步 Provider</AlertTitle>
            <AlertDescription className="text-2xs">
              远端运行时只能读取它自己状态文件中的 Provider；未同步时会触发
              provider not configured。
            </AlertDescription>
          </Alert>
          {providerSyncError ? (
            <Alert className="border-status-error/40 bg-status-error-bg text-xs">
              <AlertCircle className="size-4" aria-hidden="true" />
              <AlertTitle className="text-xs">同步失败</AlertTitle>
              <AlertDescription className="whitespace-pre-line text-2xs">
                {providerSyncError}
              </AlertDescription>
            </Alert>
          ) : null}
          <div className="flex flex-col gap-1.5 text-xs">
            {pendingProviderSync.providerKeys.map((key) => (
              <div
                key={key}
                className="flex items-center justify-between rounded-md border border-border bg-secondary px-2 py-1.5"
              >
                <span className="min-w-0 truncate">
                  {providerLabel(key, providers)}
                </span>
                <span className="ml-2 shrink-0 font-mono text-2xs text-muted-foreground">
                  {key}
                </span>
              </div>
            ))}
          </div>
        </AgentreDialog>
      ) : null}
    </>
  );
}

function BackendTypeSegmented({
  value,
  onChange,
  disabled,
}: {
  value: BackendType;
  onChange: (v: BackendType) => void;
  disabled?: boolean;
}) {
  const items = Object.keys(backendTypeMeta) as BackendType[];
  return (
    <div className="grid grid-cols-3 gap-0 rounded-md border border-border bg-secondary p-0.5">
      {items.map((t) => {
        const m = backendTypeMeta[t];
        const Icon = m.icon;
        const active = value === t;
        const itemDisabled = disabled || m.disabled;
        return (
          <button
            key={t}
            type="button"
            onClick={() => !itemDisabled && onChange(t)}
            disabled={itemDisabled}
            aria-pressed={active}
            className={cn(
              "flex items-center justify-center gap-1.5 rounded-[5px] px-2 py-1.5 text-xs font-medium transition-colors",
              active
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground",
              itemDisabled && !active && "cursor-not-allowed opacity-60",
              itemDisabled && active && "cursor-default",
            )}
          >
            <Icon className="size-3.5" aria-hidden="true" />
            {m.label}
          </button>
        );
      })}
    </div>
  );
}

function LlmProviderField({
  type,
  providers,
  value,
  onChange,
  strictLabel,
  editing,
}: {
  type: BackendType;
  providers: Provider[];
  value: string;
  onChange: (v: string) => void;
  strictLabel: string | null;
  editing: boolean;
}) {
  // claudecode / codex 允许「不关联」走 CLI 自身登录；builtin 必填。
  const optional = type === "claudecode" || type === "codex";
  // Match by providerKey (preferred) or fall back to string id for legacy data.
  const matchesProvider = (p: Provider) =>
    (p.providerKey && p.providerKey === value) || String(p.id) === value;
  const stale = editing && value !== "" && !providers.some(matchesProvider);
  const empty = providers.length === 0;
  const selected = providers.some(matchesProvider);
  // Resolve which key to use for a provider: prefer providerKey, fall back to id.
  const providerSelectValue = (p: Provider) => p.providerKey || String(p.id);

  if (empty && !optional) {
    return (
      <div className="flex flex-col gap-1.5 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-medium">LLM 供应商</span>
        </div>
        <Alert className="border-status-waiting/40 bg-status-waiting-bg text-xs">
          <AlertCircle className="size-4" aria-hidden="true" />
          <AlertTitle className="text-xs">暂无 LLM 供应商</AlertTitle>
          <AlertDescription className="text-2xs">
            请先到「LLM 供应商」页面新增一个，再回来配置后端。
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">
          LLM 供应商
          {optional ? (
            <span className="ml-1 font-mono text-2xs text-muted-foreground">
              · 可选
            </span>
          ) : null}
        </span>
        {strictLabel ? (
          <Badge
            variant="secondary"
            className="rounded-sm bg-primary-soft px-1.5 py-0 font-mono text-2xs text-primary-text"
          >
            严格匹配 · {strictLabel}
          </Badge>
        ) : null}
      </div>
      {stale ? (
        <Alert className="border-status-waiting/40 bg-status-waiting-bg text-xs">
          <AlertCircle className="size-4" aria-hidden="true" />
          <AlertTitle className="text-xs">
            原 LLM 供应商已停用或与当前类型不匹配
          </AlertTitle>
          <AlertDescription className="text-2xs">
            请从下方重新挑选一个启用中的供应商
            {optional ? "，或清除关联走 CLI 自身登录" : ""}。
          </AlertDescription>
        </Alert>
      ) : null}
      {empty && optional ? (
        <Alert className="border-border bg-secondary text-xs">
          <AlertCircle className="size-4" aria-hidden="true" />
          <AlertTitle className="text-xs">没有匹配类型的 LLM 供应商</AlertTitle>
          <AlertDescription className="text-2xs">
            {type === "claudecode"
              ? "未关联将走 claude CLI 自身登录态；如需通过本机代理转发请先新增 anthropic 类型供应商。"
              : "未关联将走 codex CLI 自身登录态；如需通过本机代理转发请先新增 openai-response 类型供应商。"}
          </AlertDescription>
        </Alert>
      ) : (
        <div className="flex items-center gap-1.5">
          <Select value={selected ? value : ""} onValueChange={onChange}>
            <SelectTrigger aria-label="LLM 供应商" className="flex-1">
              <SelectValue
                placeholder={
                  optional ? "不关联（走 CLI 自身登录）" : "请选择 LLM 供应商"
                }
              />
            </SelectTrigger>
            <SelectContent>
              {providers.map((p) => (
                <SelectItem key={p.id} value={providerSelectValue(p)}>
                  <span className="inline-flex items-center gap-2">
                    <Sparkles
                      className="size-3 text-muted-foreground"
                      aria-hidden="true"
                    />
                    <span>{p.name}</span>
                    <span className="font-mono text-2xs text-muted-foreground">
                      {p.model || "未选模型"}
                    </span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {optional && selected ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="清除供应商关联"
              title="清除（走 CLI 自身登录）"
              onClick={() => onChange("")}
            >
              <X data-icon="only" aria-hidden="true" />
            </Button>
          ) : null}
        </div>
      )}
    </div>
  );
}

function CliPathField({
  type,
  value,
  onChange,
  onDetect,
  detecting,
  missMessage,
}: {
  type: BackendType;
  value: string;
  onChange: (v: string) => void;
  onDetect: () => void;
  detecting: boolean;
  missMessage: string | null;
}) {
  const bin = type === "claudecode" ? "claude" : "codex";
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">CLI 路径</span>
        <span className="font-mono text-2xs text-muted-foreground">
          {value.trim() === "" ? `空 = $PATH 中的 ${bin}` : "显式 binary 路径"}
        </span>
      </div>
      <div className="flex items-center gap-1.5">
        <Input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={`/usr/local/bin/${bin}`}
          className="font-mono"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-9 shrink-0 gap-1 px-2 text-2xs"
          onClick={onDetect}
          disabled={detecting}
          aria-label="自动识别 CLI 路径"
          title={`在 $PATH 中查找 ${bin} 并覆盖填入`}
        >
          {detecting ? (
            <Loader2 className="size-3 animate-spin" aria-hidden="true" />
          ) : (
            <Radar className="size-3" aria-hidden="true" />
          )}
          自动识别
        </Button>
      </div>
      {missMessage ? (
        <span className="font-mono text-2xs text-status-waiting">
          {missMessage}
        </span>
      ) : null}
    </div>
  );
}

function ModelRoutesField({
  providers,
  routes,
  onChange,
  mainProviderKey,
}: {
  providers: Provider[];
  routes: Record<ClaudeTier, string>;
  onChange: (r: Record<ClaudeTier, string>) => void;
  mainProviderKey: string;
}) {
  const inheritName =
    providers.find(
      (p) =>
        (p.providerKey && p.providerKey === mainProviderKey) ||
        String(p.id) === mainProviderKey,
    )?.name ?? "继承主供应商";
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">模型分级路由</span>
        <span className="font-mono text-2xs text-muted-foreground">
          ANTHROPIC_DEFAULT_*_MODEL，留空走主供应商
        </span>
      </div>
      <div className="flex flex-col gap-1.5">
        {CLAUDE_TIERS.map((tier) => {
          const value = routes[tier] ?? "";
          return (
            <div
              key={tier}
              className="grid grid-cols-[64px_1fr] items-center gap-2"
            >
              <Badge
                variant="secondary"
                className="justify-self-start rounded-sm px-1.5 py-0.5 font-mono text-2xs"
              >
                {tier}
              </Badge>
              <Select
                value={value === "" ? "__inherit__" : value}
                onValueChange={(v) =>
                  onChange({
                    ...routes,
                    [tier]: v === "__inherit__" ? "" : v,
                  })
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择供应商" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__inherit__">
                    <span className="text-muted-foreground">
                      继承主供应商 · {inheritName}
                    </span>
                  </SelectItem>
                  {providers.map((p) => (
                    <SelectItem
                      key={p.id}
                      value={p.providerKey || String(p.id)}
                    >
                      <span className="inline-flex items-center gap-2">
                        <span>{p.name}</span>
                        <span className="font-mono text-2xs text-muted-foreground">
                          {p.model || "未选模型"}
                        </span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function SandboxField({
  value,
  onChange,
}: {
  value: SandboxValue;
  onChange: (v: SandboxValue) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">Sandbox</span>
        <span className="font-mono text-2xs text-muted-foreground">
          codex 子进程文件系统隔离
        </span>
      </div>
      <div className="grid grid-cols-3 gap-1 rounded-md border border-border bg-secondary p-0.5">
        {SANDBOX_OPTIONS.map((opt) => {
          const active = value === opt.value;
          return (
            <button
              key={opt.value}
              type="button"
              onClick={() => onChange(active ? "" : opt.value)}
              aria-pressed={active}
              className={cn(
                "rounded-[5px] px-2 py-1.5 font-mono text-2xs transition-colors",
                active
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {opt.label}
            </button>
          );
        })}
      </div>
      {value === "" ? (
        <span className="font-mono text-2xs text-muted-foreground">
          空 = 走 codex CLI 默认
        </span>
      ) : null}
    </div>
  );
}

function ApprovalField({
  value,
  onChange,
}: {
  value: ApprovalValue;
  onChange: (v: ApprovalValue) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">Approval Policy</span>
        <span className="font-mono text-2xs text-muted-foreground">
          工具执行前是否人工确认
        </span>
      </div>
      <Select
        value={value === "" ? "never" : value}
        onValueChange={(v) => onChange(v as ApprovalValue)}
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {APPROVAL_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              <span className="inline-flex items-center gap-2">
                <span className="font-mono text-2xs">{opt.value}</span>
                <span className="text-muted-foreground">{opt.label}</span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

// ReasoningEffortField shadcn Select 把"思考力度"以六档（默认 + low/medium/high/xhigh/max）
// 暴露给用户。codex 类型下展示到 xhigh，隐藏 max——max 在底层会 clamp 到 high，
// UI 直接隐藏避免「保存了 max 实际上等于 high」的迷惑。
//
// Select 不接受空字符串作为 SelectItem value，所以把 "" 映射为字面量 "default"，
// 在 onValueChange 回传时再翻译回 ""，与后端枚举对齐。
function ReasoningEffortField({
  type,
  value,
  onChange,
}: {
  type: BackendType;
  value: ReasoningEffortValue;
  onChange: (v: ReasoningEffortValue) => void;
}) {
  const options =
    type === "codex" ? REASONING_EFFORTS_CODEX : REASONING_EFFORTS_FULL;
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-medium">思考力度</span>
        <span className="font-mono text-2xs text-muted-foreground">
          reasoning_effort
        </span>
      </div>
      <Select
        value={value === "" ? "default" : value}
        onValueChange={(v) =>
          onChange((v === "default" ? "" : v) as ReasoningEffortValue)
        }
      >
        <SelectTrigger aria-label="思考力度">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((opt) => (
            <SelectItem
              key={opt || "default"}
              value={opt === "" ? "default" : opt}
            >
              <span className="inline-flex items-center gap-2">
                <span className="font-mono text-2xs">{opt || "default"}</span>
                <span className="text-muted-foreground">
                  {REASONING_EFFORT_LABELS[opt]}
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {type === "codex" ? (
        <span className="text-2xs text-muted-foreground">
          codex CLI 支持 low / medium / high / xhigh；max 将被折叠到 high。
        </span>
      ) : null}
    </div>
  );
}

// DefaultPermissionModeField 是 claudecode 的「新会话默认起手 mode」配置。
// 取值：
//   - "" → 走 pkg/claudecode 兜底（acceptEdits）。
//   - default / acceptEdits / plan → 普通模式。
//   - bypassPermissions → 起手即跳审批；CLI spawn 时下发 --permission-mode bypassPermissions，
//     runtime 仍可在 4 档之间自由切换（实测 claude v2.1.x 行为）。
//
// 用 shadcn Select 而不是 Switch：4 档枚举 + 危险等级递进的视觉提示。
//
// 远端 + bypass 的额外坑：claude CLI 内部把 --permission-mode bypassPermissions
// 当作 --dangerously-skip-permissions 走 root 检查，若 agentred 以 root/sudo
// 运行会被 CLI 直接拒掉。设 IS_SANDBOX=1 可让 CLI 跳过该检查（CLI 自带的
// 沙箱逃生口）。此处展示提示 + 一键填到 env_json。
function DefaultPermissionModeField({
  value,
  onChange,
  isRemote,
  hasIsSandbox,
  onAddIsSandbox,
}: {
  value: string;
  onChange: (v: string) => void;
  isRemote: boolean;
  hasIsSandbox: boolean;
  onAddIsSandbox: () => void;
}) {
  const isBypass = value === "bypassPermissions";
  const showRootHint = isBypass && isRemote;
  return (
    <div
      className={cn(
        "flex flex-col gap-1.5 rounded-md border px-3 py-2 text-xs",
        isBypass
          ? "border-destructive/40 bg-destructive-soft"
          : "border-border bg-secondary/40",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span
            className={cn(
              "inline-flex items-center gap-1.5 font-medium",
              isBypass ? "text-destructive" : "",
            )}
          >
            {isBypass ? (
              <AlertCircle className="size-3.5 shrink-0" aria-hidden="true" />
            ) : null}
            默认权限模式
          </span>
          <span className="font-mono text-2xs text-muted-foreground">
            --permission-mode · 新会话起手 mode
          </span>
        </div>
        <Select
          value={value === "" ? "__inherit__" : value}
          onValueChange={(v) => onChange(v === "__inherit__" ? "" : v)}
        >
          <SelectTrigger
            aria-label="默认权限模式"
            className="h-7 w-[170px] text-2xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__inherit__">
              <span className="text-muted-foreground">默认 · acceptEdits</span>
            </SelectItem>
            <SelectItem value="default">default · 每次询问</SelectItem>
            <SelectItem value="acceptEdits">
              acceptEdits · 自动接受编辑
            </SelectItem>
            <SelectItem value="plan">plan · 只读分析</SelectItem>
            <SelectItem value="bypassPermissions">
              bypassPermissions · 跳过审批
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      {isBypass ? (
        <span className="text-2xs text-destructive">
          会话起手即跳过 permission gate；运行时可在 4
          档之间自由切换。仅建议在隔离沙箱 / CI 中使用。
        </span>
      ) : null}
      {showRootHint ? (
        <div className="flex flex-wrap items-center gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-2xs text-amber-700 dark:text-amber-300">
          <span className="min-w-0 flex-1">
            远端 agentred 若以 root/sudo 运行，claude CLI 会拒绝
            bypassPermissions（视同 --dangerously-skip-permissions）。 设{" "}
            <span className="font-mono">IS_SANDBOX=1</span> 可让 CLI
            跳过该检查。
          </span>
          {hasIsSandbox ? (
            <span className="inline-flex items-center gap-1 font-mono text-2xs text-muted-foreground">
              <CheckCircle2 className="size-3" aria-hidden="true" />
              已在 env_json 配置
            </span>
          ) : (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-6 gap-1 px-2 text-2xs"
              onClick={onAddIsSandbox}
            >
              <Plus className="size-3" aria-hidden="true" />
              添加 IS_SANDBOX=1
            </Button>
          )}
        </div>
      ) : null}
    </div>
  );
}

function EnvJsonField({
  entries,
  onChange,
  open,
  onToggle,
  reservedOffenders,
}: {
  entries: EnvEntry[];
  onChange: (next: EnvEntry[]) => void;
  open: boolean;
  onToggle: () => void;
  reservedOffenders: string[];
}) {
  const filledCount = entries.filter((e) => e.key.trim() !== "").length;
  return (
    <div className="flex flex-col gap-1.5 rounded-md border border-border bg-secondary/40 px-3 py-2 text-xs">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex items-center justify-between gap-2 text-left"
      >
        <span className="inline-flex items-center gap-1.5 font-medium">
          {open ? (
            <ChevronDown className="size-3.5" aria-hidden="true" />
          ) : (
            <ChevronRight className="size-3.5" aria-hidden="true" />
          )}
          高级 · 自定义环境变量
        </span>
        <span className="font-mono text-2xs text-muted-foreground">
          {filledCount} 项
        </span>
      </button>
      {open ? (
        <div className="flex flex-col gap-1.5 pt-1.5">
          {reservedOffenders.length > 0 ? (
            <div className="rounded-sm bg-destructive-soft px-2 py-1 text-2xs text-destructive">
              保留键禁用：{reservedOffenders.join(", ")}
            </div>
          ) : null}
          {entries.length === 0 ? (
            <span className="text-2xs text-muted-foreground">
              暂无键值；点下方「+ 新增」加一行。
            </span>
          ) : null}
          {entries.map((entry, i) => {
            const trimmed = entry.key.trim();
            const isReserved = trimmed !== "" && RESERVED_ENV_KEYS.has(trimmed);
            return (
              <div
                key={i}
                className="grid grid-cols-[1fr_1fr_28px] items-center gap-1.5"
              >
                <Input
                  value={entry.key}
                  onChange={(ev) =>
                    onChange(
                      entries.map((x, j) =>
                        j === i ? { ...x, key: ev.target.value } : x,
                      ),
                    )
                  }
                  placeholder="KEY"
                  className={cn(
                    "h-7 font-mono text-2xs",
                    isReserved && "border-destructive",
                  )}
                />
                <Input
                  value={entry.value}
                  onChange={(ev) =>
                    onChange(
                      entries.map((x, j) =>
                        j === i ? { ...x, value: ev.target.value } : x,
                      ),
                    )
                  }
                  placeholder="VALUE"
                  className="h-7 font-mono text-2xs"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label="删除此键"
                  onClick={() => onChange(entries.filter((_, j) => j !== i))}
                >
                  <X data-icon="only" aria-hidden="true" />
                </Button>
              </div>
            );
          })}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 self-start gap-1 px-2 text-2xs"
            onClick={() => onChange([...entries, { key: "", value: "" }])}
          >
            <Plus className="size-3" aria-hidden="true" />
            新增
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function ProxyNote({
  status,
  providerLinked,
  onOpenProxySettings,
}: {
  status: httpgateway.GatewayStatus | null;
  providerLinked: boolean;
  onOpenProxySettings?: () => void;
}) {
  // 未关联 provider 时 CLI 走自身登录，本地代理不参与，无需提示其状态。
  if (!providerLinked) {
    return (
      <div className="flex items-center gap-2 rounded-md border border-border bg-secondary px-3 py-2 text-2xs text-muted-foreground">
        <span
          className="size-1.5 shrink-0 rounded-full bg-muted-foreground"
          aria-hidden="true"
        />
        <span className="min-w-0 flex-1 truncate">
          未关联供应商，将直接使用 CLI 自身的 login 状态，密钥不经 App。
        </span>
      </div>
    );
  }

  const running = status?.status === "running";
  const label = running
    ? status?.listenURL || "127.0.0.1"
    : "本地 HTTP 代理未启动";
  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-md border px-3 py-2 text-2xs",
        running
          ? "border-primary-text/30 bg-primary-soft text-primary-text"
          : "border-border bg-secondary text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          running ? "bg-status-running" : "bg-muted-foreground",
        )}
        aria-hidden="true"
      />
      <span className="min-w-0 flex-1 truncate">
        {running
          ? `通过本地 HTTP 代理 (${label}) 转发请求，密钥不出 App`
          : `${label}${status?.reason ? ` · ${status.reason}` : ""}`}
      </span>
      {onOpenProxySettings ? (
        <button
          type="button"
          onClick={onOpenProxySettings}
          className="inline-flex shrink-0 items-center gap-1 font-medium underline-offset-2 hover:underline"
        >
          前往设置
          <ExternalLink className="size-3" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}

function TestResultPill({ state }: { state: FlashState }) {
  if (!state) return null;
  const ok = state.kind === "ok";
  const { display, full, truncated } = truncateFlashText(state.text);
  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-md border px-3 py-2 text-xs",
        ok
          ? "border-status-running/40 bg-status-running-bg text-status-running"
          : "border-destructive/40 bg-destructive-soft text-destructive",
      )}
      role="status"
    >
      {ok ? (
        <CheckCircle2 className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      ) : (
        <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      )}
      <span
        className="min-w-0 flex-1 break-words"
        title={truncated ? full : undefined}
      >
        {display}
      </span>
    </div>
  );
}

function DeleteDialog({
  backend,
  onCancel,
  onConfirmed,
  onError,
}: {
  backend: Backend;
  onCancel: () => void;
  onConfirmed: () => Promise<void> | void;
  onError: (text: string) => void;
}) {
  const [submitting, setSubmitting] = React.useState(false);
  return (
    <AgentreDialog
      open
      onOpenChange={(o) => (!o ? onCancel() : undefined)}
      title="删除 Agent 后端"
      description={`确认删除「${backend.name}」？该操作仅软删，不影响已存在的 Agent 引用。`}
      footer={
        <>
          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
            disabled={submitting}
          >
            取消
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={submitting}
            onClick={async () => {
              setSubmitting(true);
              try {
                await DeleteAgentBackend({
                  id: backend.id,
                } as agent_backend_svc.DeleteBackendRequest);
                await onConfirmed();
              } catch (err) {
                onError(messageFromError(err));
              } finally {
                setSubmitting(false);
              }
            }}
          >
            删除
          </Button>
        </>
      }
    />
  );
}

function FlashBanner({
  state,
  onDismiss,
}: {
  state: FlashState;
  onDismiss: () => void;
}) {
  if (!state) return null;
  const ok = state.kind === "ok";
  const { display, full, truncated } = truncateFlashText(state.text);
  return (
    <div
      className={cn(
        "flex items-center gap-2 px-4 py-2 text-xs",
        ok
          ? "bg-status-running-bg text-status-running"
          : "bg-destructive-soft text-destructive",
      )}
      role="status"
    >
      {ok ? (
        <CheckCircle2 className="size-3.5" />
      ) : (
        <AlertCircle className="size-3.5" />
      )}
      <span
        className="min-w-0 flex-1 truncate"
        title={truncated ? full : undefined}
      >
        {display}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={onDismiss}
        aria-label="关闭提示"
      >
        <ChevronDown className="size-3.5 rotate-45" aria-hidden="true" />
      </Button>
    </div>
  );
}

function messageFromError(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  try {
    return JSON.stringify(err);
  } catch {
    return "未知错误";
  }
}

function providerSyncMessageFromError(err: unknown): string {
  const message = messageFromError(err);
  if (
    message.includes("org.freedesktop.secrets") ||
    message.includes("Secret Service")
  ) {
    return [
      "旧版远端 agentred 仍在写系统 keychain，但远端 Linux 没有可用的 Secret Service（org.freedesktop.secrets）。",
      "处理方式：升级并重启远端 agentred；当前版本会直接写入 agentred 状态文件，不再使用 keychain。若暂时不能升级，请在远端安装并启动 gnome-keyring / KWallet 等 Secret Service。",
      `原始错误：${message}`,
    ].join("\n");
  }
  return message;
}

// newRequestId 为一次 Test 调用分配 uuid；优先用 crypto.randomUUID，
// 老环境（理论上不会在 wails webview 出现）回落到 Math.random 拼接。
function newRequestId(): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
