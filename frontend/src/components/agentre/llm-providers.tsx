import * as React from "react";
import {
  AlertCircle,
  ArrowUpRight,
  CheckCircle2,
  ChevronDown,
  Copy,
  Cpu,
  Eye,
  EyeOff,
  Globe,
  Hash,
  KeyRound,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  SendHorizontal,
  Sparkles,
  Trash2,
} from "lucide-react";
import { Popover as PopoverPrimitive } from "radix-ui";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

import {
  CreateLLMProvider,
  DeleteLLMProvider,
  ListLLMModels,
  ListLLMProviders,
  LookupLLMModel,
  PreviewLLMModels,
  TestLLMProvider,
  UpdateLLMProvider,
} from "../../../wailsjs/go/app/App";
import { llm_provider_svc } from "../../../wailsjs/go/models";

type Provider = llm_provider_svc.ProviderItem;
type ModelInfo = llm_provider_svc.ModelInfo;

type ProviderType = "anthropic" | "openai-chat" | "openai-response";

type ProviderTypeMeta = {
  badge: string;
  defaultBaseUrl: string;
  description: string;
  label: string;
  tone: "dark" | "green" | "blue";
};

const providerTypeMeta: Record<ProviderType, ProviderTypeMeta> = {
  anthropic: {
    badge: "A",
    label: "Anthropic",
    defaultBaseUrl: "https://api.anthropic.com",
    description: "Claude 系列原生 API",
    tone: "dark",
  },
  "openai-chat": {
    // 两个 openai 变体首字母都是 O，用 OC/OR 区分。
    badge: "OC",
    label: "OpenAI / 兼容端（Chat）",
    defaultBaseUrl: "https://api.openai.com/v1",
    description:
      "GPT / 任何走 /v1/chat/completions 的 OpenAI 兼容协议（vLLM、Ollama、Azure 等）",
    tone: "green",
  },
  "openai-response": {
    badge: "OR",
    label: "OpenAI Responses",
    defaultBaseUrl: "https://api.openai.com/v1",
    description:
      "新版 /v1/responses 协议，覆盖 o-series、gpt-5-codex 等仅 Responses 模型",
    tone: "blue",
  },
};

function badgeToneClass(tone: ProviderTypeMeta["tone"]): string {
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

type EditorState =
  | { kind: "closed" }
  | { kind: "create" }
  | { kind: "edit"; provider: Provider };

type FlashState =
  | { kind: "ok"; text: string }
  | { kind: "err"; text: string }
  | null;

function errMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "未知错误";
}

async function fetchProviders() {
  const resp = await ListLLMProviders();

  return resp.items ?? [];
}

export function LlmProvidersPanel() {
  const [providers, setProviders] = React.useState<Provider[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [editor, setEditor] = React.useState<EditorState>({ kind: "closed" });
  const [flash, setFlash] = React.useState<FlashState>(null);
  const [confirmDeleteId, setConfirmDeleteId] = React.useState<number | null>(
    null,
  );
  const [deletingId, setDeletingId] = React.useState<number | null>(null);
  const [testingId, setTestingId] = React.useState<number | null>(null);

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      setProviders(await fetchProviders());
    } catch (err) {
      setFlash({ kind: "err", text: `加载失败：${errMessage(err)}` });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    let mounted = true;

    void fetchProviders()
      .then((items) => {
        if (mounted) {
          setProviders(items);
        }
      })
      .catch((err: unknown) => {
        if (mounted) {
          setFlash({ kind: "err", text: `加载失败：${errMessage(err)}` });
        }
      })
      .finally(() => {
        if (mounted) {
          setLoading(false);
        }
      });

    return () => {
      mounted = false;
    };
  }, []);

  const openCreate = React.useCallback(() => setEditor({ kind: "create" }), []);
  const openEdit = React.useCallback(
    (provider: Provider) => setEditor({ kind: "edit", provider }),
    [],
  );
  const closeEditor = React.useCallback(
    () => setEditor({ kind: "closed" }),
    [],
  );

  const handleSubmit = React.useCallback(
    async (input: ProviderFormValues): Promise<{ providerKey?: string }> => {
      try {
        if (editor.kind === "create") {
          const created = await CreateLLMProvider(
            new llm_provider_svc.CreateProviderRequest({
              type: input.type,
              name: input.name.trim(),
              apiKey: input.apiKey.trim(),
              baseUrl: input.baseUrl.trim(),
              model: input.model.trim(),
              maxOutput: input.maxOutput,
              contextWindow: input.contextWindow,
            }),
          );
          setFlash({ kind: "ok", text: `已新增供应商 "${input.name.trim()}"` });
          await refresh();
          // Return key to form so it can display it; form stays open.
          const key = (
            created as unknown as { item?: { providerKey?: string } }
          )?.item?.providerKey;
          return { providerKey: key };
        } else if (editor.kind === "edit") {
          await UpdateLLMProvider(
            new llm_provider_svc.UpdateProviderRequest({
              id: editor.provider.id,
              name: input.name.trim(),
              apiKey: input.apiKey.trim(),
              baseUrl: input.baseUrl.trim(),
              model: input.model.trim(),
              maxOutput: input.maxOutput,
              contextWindow: input.contextWindow,
            }),
          );
          setFlash({ kind: "ok", text: `已更新供应商 "${input.name.trim()}"` });
          closeEditor();
          await refresh();
        }
      } catch (err) {
        setFlash({ kind: "err", text: `保存失败：${errMessage(err)}` });
      }
      return {};
    },
    [closeEditor, editor, refresh],
  );

  const handleDeleteRequest = React.useCallback((provider: Provider) => {
    setConfirmDeleteId((current) =>
      current === provider.id ? null : provider.id,
    );
  }, []);

  const handleDeleteCancel = React.useCallback(() => {
    setConfirmDeleteId(null);
  }, []);

  const handleDeleteConfirm = React.useCallback(
    async (provider: Provider) => {
      setDeletingId(provider.id);
      try {
        await DeleteLLMProvider(
          new llm_provider_svc.DeleteProviderRequest({ id: provider.id }),
        );
        setFlash({ kind: "ok", text: `已删除供应商 "${provider.name}"` });
        setConfirmDeleteId(null);
        await refresh();
      } catch (err) {
        setFlash({ kind: "err", text: `删除失败：${errMessage(err)}` });
      } finally {
        setDeletingId(null);
      }
    },
    [refresh],
  );

  const handleTest = React.useCallback(async (provider: Provider) => {
    setTestingId(provider.id);
    try {
      const resp = await TestLLMProvider(
        new llm_provider_svc.TestConnectionRequest({ id: provider.id }),
      );
      if (resp.ok) {
        setFlash({
          kind: "ok",
          text: `"${provider.name}" 调用成功，已发送 hi 并收到模型响应`,
        });
      } else {
        setFlash({
          kind: "err",
          text: `"${provider.name}" 连接失败：${resp.message}`,
        });
      }
    } catch (err) {
      setFlash({ kind: "err", text: `测试失败：${errMessage(err)}` });
    } finally {
      setTestingId(null);
    }
  }, []);

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {flash ? (
        <Alert
          className={cn(
            "py-2",
            flash.kind === "ok"
              ? "border-status-running/40 bg-status-running/10 text-status-running"
              : "border-status-error/40 bg-status-error/10 text-status-error",
          )}
        >
          {flash.kind === "ok" ? (
            <CheckCircle2 className="size-4" aria-hidden="true" />
          ) : (
            <AlertCircle className="size-4" aria-hidden="true" />
          )}
          <AlertTitle className="text-xs font-semibold">
            {flash.kind === "ok" ? "操作成功" : "出错了"}
          </AlertTitle>
          <AlertDescription className="text-2xs">{flash.text}</AlertDescription>
        </Alert>
      ) : null}

      <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-3 sm:px-4">
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="text-sm font-semibold">已配置的供应商</span>
            <span className="text-2xs text-muted-foreground">
              共 {providers.length} 个
            </span>
          </div>
          <Button
            type="button"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
            onClick={openCreate}
          >
            <Plus data-icon="inline-start" aria-hidden="true" />
            新增供应商
          </Button>
        </div>

        <Table aria-label="LLM 供应商列表" className="min-w-[720px]">
          <TableHeader>
            <TableRow className="bg-secondary hover:bg-secondary">
              <TableHead className="w-[260px] px-4 font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                名称
              </TableHead>
              <TableHead className="w-[180px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                类型
              </TableHead>
              <TableHead className="min-w-[280px] font-mono text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                Endpoint / Key
              </TableHead>
              <TableHead className="w-[100px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={4} className="py-6 text-center text-xs">
                  <Loader2
                    className="mx-auto size-4 animate-spin text-muted-foreground"
                    aria-hidden="true"
                  />
                </TableCell>
              </TableRow>
            ) : providers.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="p-0">
                  <ProvidersEmptyState onCreate={openCreate} />
                </TableCell>
              </TableRow>
            ) : (
              providers.map((row) => (
                <ProviderRow
                  key={row.id}
                  provider={row}
                  onEdit={openEdit}
                  onDeleteRequest={handleDeleteRequest}
                  onDeleteConfirm={handleDeleteConfirm}
                  onDeleteCancel={handleDeleteCancel}
                  isConfirmingDelete={confirmDeleteId === row.id}
                  isDeleting={deletingId === row.id}
                  isTesting={testingId === row.id}
                  onTest={handleTest}
                />
              ))
            )}
          </TableBody>
        </Table>
      </section>

      <ProviderFormDialog
        editor={editor}
        onClose={closeEditor}
        onSubmit={handleSubmit}
      />
    </div>
  );
}

type ProviderRowProps = {
  isConfirmingDelete: boolean;
  isDeleting: boolean;
  isTesting: boolean;
  onDeleteCancel: () => void;
  onDeleteConfirm: (provider: Provider) => void;
  onDeleteRequest: (provider: Provider) => void;
  onEdit: (provider: Provider) => void;
  onTest: (provider: Provider) => void;
  provider: Provider;
};

function ProviderRow({
  isConfirmingDelete,
  isDeleting,
  isTesting,
  onDeleteCancel,
  onDeleteConfirm,
  onDeleteRequest,
  onEdit,
  onTest,
  provider,
}: ProviderRowProps) {
  const meta = providerTypeMeta[provider.type as ProviderType];
  const endpoint = provider.baseUrl || meta?.defaultBaseUrl || "—";

  return (
    <TableRow className="align-top hover:bg-accent/45">
      <TableCell className="px-4 py-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate text-sm font-medium">{provider.name}</span>
          <span className="font-mono text-2xs text-subtle-foreground">
            {provider.hasApiKey ? provider.maskedApiKey : "未配置 API Key"}
          </span>
          {provider.model ? (
            <span className="mt-0.5 inline-flex w-fit items-center gap-1 rounded-sm bg-primary-soft px-1.5 py-0.5 font-mono text-2xs text-primary-text">
              <Cpu className="size-3" aria-hidden="true" />
              {provider.model}
            </span>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="py-3 text-xs">
        <span className="inline-flex min-w-0 items-center gap-1.5">
          <span
            role="img"
            aria-label={meta?.label ?? provider.type}
            className={cn(
              "inline-flex size-[18px] shrink-0 items-center justify-center rounded-sm text-2xs font-bold",
              badgeToneClass(meta?.tone ?? "dark"),
            )}
          >
            {meta?.badge ?? (meta?.label ?? provider.type).slice(0, 1)}
          </span>
          <span className="truncate">{meta?.label ?? provider.type}</span>
        </span>
      </TableCell>
      <TableCell className="py-3">
        <span className="block max-w-[280px] truncate font-mono text-2xs">
          {endpoint}
        </span>
      </TableCell>
      <TableCell className="px-4 py-3">
        <div className="flex justify-end gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`测试 ${provider.name}`}
            title={isTesting ? "测试中" : "测试调用（发送 hi）"}
            className="size-[26px] text-muted-foreground"
            onClick={() => onTest(provider)}
            disabled={isTesting}
          >
            {isTesting ? (
              <Loader2
                data-icon="only"
                aria-hidden="true"
                className="animate-spin"
              />
            ) : (
              <SendHorizontal data-icon="only" aria-hidden="true" />
            )}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`编辑 ${provider.name}`}
            title="编辑"
            className="size-[26px] text-muted-foreground"
            onClick={() => onEdit(provider)}
          >
            <Pencil data-icon="only" aria-hidden="true" />
          </Button>
          {isConfirmingDelete ? (
            <div
              role="group"
              aria-label={`确认删除 ${provider.name}`}
              className="flex items-center gap-1 rounded-md border border-status-error/40 bg-destructive-soft px-1 py-0.5"
            >
              <span className="font-mono text-2xs text-status-error">
                确认删除？
              </span>
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="h-6 px-2 text-2xs text-muted-foreground"
                onClick={onDeleteCancel}
                disabled={isDeleting}
              >
                取消
              </Button>
              <Button
                type="button"
                variant="destructive"
                size="xs"
                className="h-6 px-2 text-2xs"
                onClick={() => onDeleteConfirm(provider)}
                disabled={isDeleting}
              >
                {isDeleting ? (
                  <Loader2
                    className="size-3 animate-spin"
                    data-icon="inline-start"
                    aria-hidden="true"
                  />
                ) : null}
                删除
              </Button>
            </div>
          ) : (
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={`删除 ${provider.name}`}
              title="删除（需二次确认）"
              className="size-[26px] text-status-error"
              onClick={() => onDeleteRequest(provider)}
            >
              <Trash2 data-icon="only" aria-hidden="true" />
            </Button>
          )}
        </div>
      </TableCell>
    </TableRow>
  );
}

type ProvidersEmptyStateProps = {
  onCreate: () => void;
};

function ProvidersEmptyState({ onCreate }: ProvidersEmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 px-6 py-10 text-center">
      <div
        aria-hidden="true"
        className="relative flex size-12 items-center justify-center rounded-full bg-primary-soft text-primary-text"
      >
        <Sparkles className="size-5" />
        <span className="absolute -bottom-0.5 -right-0.5 inline-flex size-5 items-center justify-center rounded-full border-2 border-card bg-card text-primary-text shadow-xs">
          <Plus className="size-3" />
        </span>
      </div>
      <div className="flex max-w-md flex-col gap-1">
        <p className="text-sm font-semibold">还没有配置任何 LLM 供应商</p>
        <p className="text-2xs leading-relaxed text-muted-foreground">
          先添加一个供应商（Anthropic、OpenAI
          或兼容端点），并选择默认模型即可使用。
        </p>
      </div>
      <Button
        type="button"
        size="sm"
        className="h-[30px] gap-1.5 px-3 text-xs"
        onClick={onCreate}
      >
        <Plus data-icon="inline-start" aria-hidden="true" />
        新增第一个供应商
      </Button>
      <a
        href="https://docs.anthropic.com/"
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 text-2xs text-muted-foreground transition-colors hover:text-primary-text"
      >
        如何获取 API Key
        <ArrowUpRight className="size-3" aria-hidden="true" />
      </a>
    </div>
  );
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}K`;
  }
  return String(n);
}

type ProviderFormValues = {
  apiKey: string;
  baseUrl: string;
  contextWindow: number;
  maxOutput: number;
  model: string;
  name: string;
  type: ProviderType;
};

type ProviderFormDialogProps = {
  editor: EditorState;
  onClose: () => void;
  onSubmit: (values: ProviderFormValues) => Promise<{ providerKey?: string }>;
};

function ProviderFormDialog({
  editor,
  onClose,
  onSubmit,
}: ProviderFormDialogProps) {
  const open = editor.kind !== "closed";
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent className="max-w-[560px]">
        {editor.kind !== "closed" ? (
          <ProviderForm
            key={
              editor.kind === "edit" ? `edit-${editor.provider.id}` : "create"
            }
            editor={editor}
            onCancel={onClose}
            onSubmit={onSubmit}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

type ProviderFormProps = {
  editor: Exclude<EditorState, { kind: "closed" }>;
  onCancel: () => void;
  onSubmit: (values: ProviderFormValues) => Promise<{ providerKey?: string }>;
};

function ProviderForm({ editor, onCancel, onSubmit }: ProviderFormProps) {
  const initial = React.useMemo<ProviderFormValues>(() => {
    if (editor.kind === "edit") {
      return {
        type: (editor.provider.type as ProviderType) ?? "anthropic",
        name: editor.provider.name,
        apiKey: "",
        baseUrl: editor.provider.baseUrl,
        model: editor.provider.model ?? "",
        maxOutput: editor.provider.maxOutput ?? 0,
        contextWindow: editor.provider.contextWindow ?? 0,
      };
    }
    return {
      type: "anthropic",
      name: "",
      apiKey: "",
      baseUrl: "",
      model: "",
      maxOutput: 0,
      contextWindow: 0,
    };
  }, [editor]);

  const [values, setValues] = React.useState<ProviderFormValues>(initial);
  const [showKey, setShowKey] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  // providerKey: for edit mode, initialized from existing provider; for create mode,
  // updated after successful save with the server-generated UUID.
  const [providerKey, setProviderKey] = React.useState<string>(
    editor.kind === "edit" ? (editor.provider.providerKey ?? "") : "",
  );
  const [keyCopied, setKeyCopied] = React.useState(false);
  const [modelOptions, setModelOptions] = React.useState<ModelInfo[]>([]);
  const [modelsLoading, setModelsLoading] = React.useState(false);
  const [modelsError, setModelsError] = React.useState<string | null>(null);
  const [fetchedOnce, setFetchedOnce] = React.useState(false);
  const [testingDraft, setTestingDraft] = React.useState(false);
  const [testFlash, setTestFlash] = React.useState<FlashState>(null);

  const meta = providerTypeMeta[values.type];
  const isEdit = editor.kind === "edit";

  const update = React.useCallback(
    <K extends keyof ProviderFormValues>(key: K, v: ProviderFormValues[K]) => {
      setTestFlash(null);
      setValues((prev) => ({ ...prev, [key]: v }));
    },
    [],
  );

  // 当用户改动模型 id（不是首屏挂载）时，解析 cago 默认参数并直接写进
  // maxOutput / contextWindow 输入框；用户随后还可以手动覆盖。
  const didMountRef = React.useRef(false);
  React.useEffect(() => {
    if (!didMountRef.current) {
      didMountRef.current = true;
      return; // 初始渲染保留 props 传入的值（edit 模式下保留用户已保存的覆盖）
    }
    const id = values.model.trim();
    if (!id) return;
    let cancelled = false;
    void (async () => {
      let maxOut = 0;
      let ctxWin = 0;
      const local = modelOptions.find(
        (m) => m.id.toLowerCase() === id.toLowerCase(),
      );
      if (local && (local.maxOutput > 0 || local.contextWindow > 0)) {
        maxOut = local.maxOutput;
        ctxWin = local.contextWindow;
      } else {
        try {
          const resp = await LookupLLMModel(
            new llm_provider_svc.LookupModelRequest({ id }),
          );
          if (cancelled) return;
          if (resp.known) {
            maxOut = resp.maxOutput;
            ctxWin = resp.contextWindow;
          }
        } catch {
          return;
        }
      }
      if (cancelled) return;
      if (maxOut <= 0 && ctxWin <= 0) return;
      setValues((prev) => {
        const nextMax = maxOut > 0 ? maxOut : prev.maxOutput;
        const nextCtx = ctxWin > 0 ? ctxWin : prev.contextWindow;
        if (prev.maxOutput === nextMax && prev.contextWindow === nextCtx) {
          return prev;
        }
        return { ...prev, maxOutput: nextMax, contextWindow: nextCtx };
      });
    })();
    return () => {
      cancelled = true;
    };
    // 仅监听 model 字段：modelOptions 变化由 fetch 回调里单独同步，
    // 避免拉取列表时覆盖用户手填的限额。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [values.model]);

  const fetchPreviewModels = React.useCallback(async () => {
    setModelsLoading(true);
    setModelsError(null);
    try {
      const items = isEdit
        ? ((
            await ListLLMModels(
              new llm_provider_svc.ListModelsRequest({
                id: editor.provider.id,
              }),
            )
          ).items ?? [])
        : ((
            await PreviewLLMModels(
              new llm_provider_svc.PreviewModelsRequest({
                type: values.type,
                apiKey: values.apiKey.trim(),
                baseUrl: values.baseUrl.trim(),
              }),
            )
          ).items ?? []);
      setModelOptions(items);
      setFetchedOnce(true);
      // 拉到列表后如果当前 model 命中且用户限额仍为 0，顺手填上 enriched 数据。
      const currentId = values.model.trim().toLowerCase();
      if (currentId) {
        const hit = items.find((m) => m.id.toLowerCase() === currentId);
        if (hit && (hit.maxOutput > 0 || hit.contextWindow > 0)) {
          setValues((prev) => {
            const nextMax =
              prev.maxOutput === 0 && hit.maxOutput > 0
                ? hit.maxOutput
                : prev.maxOutput;
            const nextCtx =
              prev.contextWindow === 0 && hit.contextWindow > 0
                ? hit.contextWindow
                : prev.contextWindow;
            if (prev.maxOutput === nextMax && prev.contextWindow === nextCtx) {
              return prev;
            }
            return { ...prev, maxOutput: nextMax, contextWindow: nextCtx };
          });
        }
      }
    } catch (err) {
      setModelsError(errMessage(err));
    } finally {
      setModelsLoading(false);
    }
  }, [
    editor,
    isEdit,
    values.apiKey,
    values.baseUrl,
    values.model,
    values.type,
  ]);

  const submit = React.useCallback(
    async (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setError(null);
      setTestFlash(null);
      if (!values.name.trim()) {
        setError("名称不能为空");
        return;
      }
      if (!isEdit && !values.apiKey.trim()) {
        setError("API Key 不能为空");
        return;
      }
      setSubmitting(true);
      try {
        const result = await onSubmit(values);
        if (result?.providerKey) {
          setProviderKey(result.providerKey);
        }
      } catch (err) {
        setError(errMessage(err));
      } finally {
        setSubmitting(false);
      }
    },
    [isEdit, onSubmit, values],
  );

  const testDraft = React.useCallback(async () => {
    setError(null);
    setTestFlash(null);
    if (!values.model.trim()) {
      setTestFlash({ kind: "err", text: "请先填写默认模型" });
      return;
    }
    setTestingDraft(true);
    try {
      const resp = await TestLLMProvider(
        new llm_provider_svc.TestConnectionRequest({
          id: isEdit ? editor.provider.id : 0,
          useDraft: true,
          type: values.type,
          apiKey: values.apiKey.trim(),
          baseUrl: values.baseUrl.trim(),
          model: values.model.trim(),
        }),
      );
      setTestFlash(
        resp.ok
          ? { kind: "ok", text: "调用成功，已发送 hi 并收到模型响应" }
          : { kind: "err", text: resp.message || "调用失败" },
      );
    } catch (err) {
      setTestFlash({ kind: "err", text: errMessage(err) });
    } finally {
      setTestingDraft(false);
    }
  }, [editor, isEdit, values]);

  const canFetchModels = isEdit || values.apiKey.trim() !== "";

  return (
    <form
      onSubmit={submit}
      aria-label={isEdit ? "编辑 LLM 供应商" : "新增 LLM 供应商"}
    >
      <DialogHeader>
        <DialogTitle>
          {isEdit ? `编辑：${editor.provider.name}` : "新增 LLM 供应商"}
        </DialogTitle>
        <DialogDescription>
          {isEdit
            ? "更新凭证或默认模型；API Key 留空则保留原值"
            : "配置一组 LLM 凭证。新建后即可在对话中使用。"}
        </DialogDescription>
      </DialogHeader>

      <DialogBody className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <FormField label="类型" hint={meta?.description}>
            <Select
              value={values.type}
              onValueChange={(v) => update("type", v as ProviderType)}
              disabled={isEdit}
            >
              <SelectTrigger aria-label="供应商类型" className="font-medium">
                <SelectValue placeholder="选择供应商类型" />
              </SelectTrigger>
              <SelectContent>
                {(
                  Object.entries(providerTypeMeta) as [
                    ProviderType,
                    (typeof providerTypeMeta)[ProviderType],
                  ][]
                ).map(([key, info]) => (
                  <SelectItem key={key} value={key}>
                    <span
                      aria-hidden="true"
                      className={cn(
                        "inline-flex size-[16px] shrink-0 items-center justify-center rounded-sm font-mono text-2xs font-bold",
                        badgeToneClass(info.tone),
                      )}
                    >
                      {info.badge}
                    </span>
                    <span className="flex min-w-0 flex-col">
                      <span className="text-sm font-medium leading-tight">
                        {info.label}
                      </span>
                      <span className="font-mono text-2xs text-muted-foreground leading-tight">
                        {info.defaultBaseUrl}
                      </span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>

          <FormField label="名称">
            <Input
              value={values.name}
              placeholder="例如：production / 本地 Ollama"
              onChange={(e) => update("name", e.currentTarget.value)}
              className="h-9 text-sm"
              required
            />
          </FormField>
        </div>

        <FormField
          label={isEdit ? "API Key（留空保留原值）" : "API Key"}
          icon={KeyRound}
        >
          <div className="relative">
            <Input
              type={showKey ? "text" : "password"}
              value={values.apiKey}
              placeholder={
                isEdit
                  ? "********（不修改则留空）"
                  : "sk-... 或自托管 token，留空走匿名"
              }
              onChange={(e) => update("apiKey", e.currentTarget.value)}
              className="h-9 pr-9 font-mono text-xs"
              autoComplete="off"
            />
            <button
              type="button"
              aria-label={showKey ? "隐藏 API Key" : "显示 API Key"}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setShowKey((s) => !s)}
            >
              {showKey ? (
                <EyeOff className="size-3.5" aria-hidden="true" />
              ) : (
                <Eye className="size-3.5" aria-hidden="true" />
              )}
            </button>
          </div>
        </FormField>

        <FormField
          label="Base URL（可选）"
          hint={`留空走默认：${meta?.defaultBaseUrl}`}
          icon={Globe}
        >
          <Input
            value={values.baseUrl}
            placeholder={meta?.defaultBaseUrl ?? ""}
            onChange={(e) => update("baseUrl", e.currentTarget.value)}
            className="h-9 font-mono text-xs"
          />
        </FormField>

        <FormField
          label="默认模型"
          hint={
            canFetchModels
              ? "可手动填写模型 id，或点右侧 ⟳ 拉取供应商列表后下拉选择"
              : "请先填写 API Key 才能拉取模型列表"
          }
          icon={Cpu}
        >
          <ModelCombobox
            value={values.model}
            onChange={(v) => update("model", v)}
            options={modelOptions}
            loading={modelsLoading}
            onFetch={() => void fetchPreviewModels()}
            canFetch={canFetchModels}
          />
          {modelsError ? (
            <p className="mt-1.5 text-2xs text-status-error">{modelsError}</p>
          ) : fetchedOnce && modelOptions.length === 0 && !modelsLoading ? (
            <p className="mt-1.5 text-2xs text-muted-foreground">
              供应商没有返回任何模型，请手动填写模型 id。
            </p>
          ) : null}
        </FormField>

        <div className="grid grid-cols-2 gap-4">
          <FormField label="最大输出 Token" icon={Hash}>
            <Input
              type="number"
              min={0}
              step={1}
              inputMode="numeric"
              value={values.maxOutput || ""}
              placeholder="由供应商决定"
              onChange={(e) => {
                const n = parseInt(e.currentTarget.value, 10);
                update("maxOutput", Number.isFinite(n) && n > 0 ? n : 0);
              }}
              className="h-9 font-mono text-xs"
            />
          </FormField>
          <FormField label="上下文窗口" icon={Hash}>
            <Input
              type="number"
              min={0}
              step={1}
              inputMode="numeric"
              value={values.contextWindow || ""}
              placeholder="由供应商决定"
              onChange={(e) => {
                const n = parseInt(e.currentTarget.value, 10);
                update("contextWindow", Number.isFinite(n) && n > 0 ? n : 0);
              }}
              className="h-9 font-mono text-xs"
            />
          </FormField>
        </div>

        <FormField
          label="Provider Key"
          hint="稳定标识符，供 Agent 后端、远端设备引用此供应商使用"
        >
          <div className="flex items-center gap-1.5">
            <Input
              value={providerKey || ""}
              readOnly
              disabled
              placeholder={
                providerKey ? undefined : isEdit ? "—" : "保存后自动生成"
              }
              className="h-9 flex-1 font-mono text-xs"
              aria-label="Provider Key"
            />
            {providerKey ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label="复制 Provider Key"
                title={keyCopied ? "已复制" : "复制 Provider Key"}
                className="size-9 shrink-0 text-muted-foreground"
                onClick={() => {
                  void copyTextWithToast(providerKey, {
                    errorTitle: "复制 Provider Key 失败",
                    successTitle: "已复制 Provider Key",
                  }).then((copied) => {
                    if (!copied) return;
                    setKeyCopied(true);
                    setTimeout(() => setKeyCopied(false), 2000);
                  });
                }}
              >
                {keyCopied ? (
                  <CheckCircle2 className="size-3.5" aria-hidden="true" />
                ) : (
                  <Copy className="size-3.5" aria-hidden="true" />
                )}
              </Button>
            ) : null}
          </div>
        </FormField>

        {error ? <p className="text-2xs text-status-error">{error}</p> : null}
        {testFlash ? (
          <Alert
            className={cn(
              "py-2",
              testFlash.kind === "ok"
                ? "border-status-running/40 bg-status-running/10 text-status-running"
                : "border-status-error/40 bg-status-error/10 text-status-error",
            )}
          >
            {testFlash.kind === "ok" ? (
              <CheckCircle2 className="size-4" aria-hidden="true" />
            ) : (
              <AlertCircle className="size-4" aria-hidden="true" />
            )}
            <AlertTitle className="text-xs font-semibold">
              {testFlash.kind === "ok" ? "测试成功" : "测试失败"}
            </AlertTitle>
            <AlertDescription className="text-2xs">
              {testFlash.text}
            </AlertDescription>
          </Alert>
        ) : null}
      </DialogBody>

      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mr-auto h-8 gap-1.5 text-xs"
          onClick={() => void testDraft()}
          disabled={submitting || testingDraft}
        >
          {testingDraft ? (
            <Loader2
              className="size-3.5 animate-spin"
              data-icon="inline-start"
              aria-hidden="true"
            />
          ) : (
            <SendHorizontal
              className="size-3.5"
              data-icon="inline-start"
              aria-hidden="true"
            />
          )}
          测试调用
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-8 text-xs"
          onClick={onCancel}
          disabled={submitting}
        >
          取消
        </Button>
        <Button
          type="submit"
          size="sm"
          className="h-8 text-xs"
          disabled={submitting}
        >
          {submitting ? (
            <Loader2
              className="size-3.5 animate-spin"
              data-icon="inline-start"
              aria-hidden="true"
            />
          ) : null}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}

type ModelComboboxProps = {
  canFetch: boolean;
  loading: boolean;
  onChange: (id: string) => void;
  onFetch: () => void;
  options: ModelInfo[];
  value: string;
};

function ModelCombobox({
  canFetch,
  loading,
  onChange,
  onFetch,
  options,
  value,
}: ModelComboboxProps) {
  const [open, setOpen] = React.useState(false);
  const [highlight, setHighlight] = React.useState(0);
  const inputRef = React.useRef<HTMLInputElement>(null);

  const safeHighlight =
    options.length === 0 ? 0 : Math.min(highlight, options.length - 1);

  const select = React.useCallback(
    (id: string) => {
      onChange(id);
      setOpen(false);
      // 让 input 重新拿到焦点；用 requestAnimationFrame 等 Popover 关闭后再聚焦
      requestAnimationFrame(() => inputRef.current?.focus());
    },
    [onChange],
  );

  const hasOptions = options.length > 0;
  const trimmed = value.trim().toLowerCase();

  return (
    <PopoverPrimitive.Root
      open={open && hasOptions}
      onOpenChange={(next) => {
        if (!hasOptions && next) return; // 没有选项时禁止打开
        setOpen(next);
      }}
    >
      <PopoverPrimitive.Anchor asChild>
        <div
          className={cn(
            "flex items-stretch overflow-hidden rounded-md border border-input bg-transparent shadow-xs transition-[color,box-shadow,border-color] dark:bg-input/30",
            "focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/40",
            open && hasOptions && "border-ring",
          )}
        >
          <input
            ref={inputRef}
            value={value}
            autoCapitalize="off"
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            placeholder="例如：claude-opus-4-7 / gpt-4o-mini"
            className="h-9 min-w-0 flex-1 bg-transparent px-3 font-mono text-xs text-foreground outline-none placeholder:text-muted-foreground"
            onChange={(e) => {
              onChange(e.currentTarget.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                if (hasOptions) setOpen(true);
                setHighlight((h) =>
                  Math.min(h + 1, Math.max(0, options.length - 1)),
                );
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setHighlight((h) => Math.max(h - 1, 0));
              } else if (e.key === "Enter" && open && options[safeHighlight]) {
                e.preventDefault();
                select(options[safeHighlight].id);
              } else if (e.key === "Escape") {
                setOpen(false);
              }
            }}
          />
          <button
            type="button"
            title={canFetch ? "拉取供应商可用模型" : "请先填写 API Key"}
            disabled={!canFetch || loading}
            onClick={() => onFetch()}
            className="flex items-center justify-center border-l border-input px-2.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            {loading ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <RefreshCw className="size-3.5" aria-hidden="true" />
            )}
          </button>
          <button
            type="button"
            title={hasOptions ? "查看模型列表" : "尚未获取模型列表"}
            disabled={!hasOptions}
            onClick={() => {
              setOpen((o) => !o);
              inputRef.current?.focus();
            }}
            className="flex items-center justify-center border-l border-input px-2.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ChevronDown
              className={cn(
                "size-3.5 transition-transform duration-150",
                open && hasOptions && "rotate-180",
              )}
              aria-hidden="true"
            />
          </button>
        </div>
      </PopoverPrimitive.Anchor>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          side="bottom"
          align="start"
          sideOffset={6}
          onOpenAutoFocus={(e) => e.preventDefault()}
          onCloseAutoFocus={(e) => e.preventDefault()}
          // Portal 出去后，外层 Dialog 的 react-remove-scroll 会吞掉
          // 落到 document 上的 wheel/touch 事件，导致这里 overflow-y-auto 不滚动。
          // 在这层拦截冒泡，让浏览器走默认滚动逻辑。
          onWheel={(e) => e.stopPropagation()}
          onTouchMove={(e) => e.stopPropagation()}
          className={cn(
            "z-50 w-[var(--radix-popover-trigger-width)] max-h-[280px] overflow-y-auto overscroll-contain rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-[0_12px_36px_-12px_rgba(0,0,0,0.25),0_0_0_1px_rgba(0,0,0,0.04)]",
            "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
          )}
          role="listbox"
        >
          {options.map((m, i) => {
            const selected = m.id.toLowerCase() === trimmed;
            return (
              <button
                key={m.id}
                type="button"
                role="option"
                aria-selected={selected}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => select(m.id)}
                onMouseEnter={() => setHighlight(i)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm outline-none",
                  i === safeHighlight && "bg-accent text-accent-foreground",
                  selected && "bg-primary-soft text-primary-text",
                )}
              >
                <span className="truncate font-mono text-xs">{m.id}</span>
                {m.knownInCago && (m.maxOutput > 0 || m.contextWindow > 0) ? (
                  <span className="ml-auto pl-2 font-mono text-2xs text-muted-foreground">
                    {formatTokens(m.contextWindow)}/{formatTokens(m.maxOutput)}
                  </span>
                ) : null}
              </button>
            );
          })}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}

function FormField({
  children,
  className,
  hint,
  icon: Icon,
  label,
}: {
  children: React.ReactNode;
  className?: string;
  hint?: string;
  icon?: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
  label: string;
}) {
  return (
    <label className={cn("flex flex-col gap-1.5", className)}>
      <span className="flex items-center gap-1.5 text-xs font-medium text-foreground">
        {Icon ? (
          <Icon className="size-3.5 text-muted-foreground" aria-hidden />
        ) : null}
        {label}
      </span>
      {children}
      {hint ? (
        <span className="text-2xs leading-relaxed text-muted-foreground">
          {hint}
        </span>
      ) : null}
    </label>
  );
}
