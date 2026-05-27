import * as React from "react";
import {
  AlertCircle,
  CheckCircle2,
  Copy,
  Info,
  Loader2,
  RotateCcw,
  ShieldCheck,
} from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

import {
  GetAppSetting,
  GetGatewayStatus,
  RestartGateway,
  UpdateAppSettings,
} from "../../../wailsjs/go/app/App";
import { app_settings_svc, httpgateway } from "../../../wailsjs/go/models";

type FlashState =
  | { kind: "ok"; text: string }
  | { kind: "err"; text: string }
  | null;

const KEY_HOST = "proxy.listen_host";
const KEY_PORT = "proxy.listen_port";

type RouteRow = {
  method: string;
  path: string;
  description: string;
  badge?: string;
};

const ROUTE_ROWS: RouteRow[] = [
  {
    method: "POST",
    path: "/v1/messages",
    description: "Anthropic Messages（claudecode CLI）",
  },
  {
    method: "POST",
    path: "/v1/responses",
    description: "OpenAI Responses（codex CLI 默认）",
  },
  {
    method: "POST",
    path: "/v1/chat/completions",
    description: "OpenAI Chat Completions（codex wire_api=chat）",
  },
  {
    method: "ALL",
    path: "/mcp/*",
    description: "本地 MCP 服务接入预留（未注册服务时 404）",
    badge: "预留",
  },
];

function validateHost(v: string): string | null {
  const s = v.trim();
  if (s === "") return "监听地址不能为空";
  // 简单 IPv4 / 主机名校验：交由后端二次校验
  if (!/^[0-9a-zA-Z.\-:]+$/.test(s)) return "监听地址格式不合法";
  return null;
}

function validatePort(v: string): string | null {
  const s = v.trim();
  if (s === "") return "端口不能为空";
  if (!/^\d+$/.test(s)) return "端口必须为数字";
  const n = Number(s);
  if (n < 0 || n > 65535) return "端口需在 0-65535 之间";
  return null;
}

export function SettingsProxyPanel() {
  const [host, setHost] = React.useState("");
  const [port, setPort] = React.useState("");
  const [savedHost, setSavedHost] = React.useState("");
  const [savedPort, setSavedPort] = React.useState("");
  const [status, setStatus] = React.useState<httpgateway.GatewayStatus | null>(
    null,
  );
  const [loading, setLoading] = React.useState(true);
  const [applying, setApplying] = React.useState(false);
  const [restarting, setRestarting] = React.useState(false);
  const [flash, setFlash] = React.useState<FlashState>(null);

  React.useEffect(() => {
    let mounted = true;
    Promise.all([
      GetAppSetting({ key: KEY_HOST }),
      GetAppSetting({ key: KEY_PORT }),
      GetGatewayStatus(),
    ])
      .then(([h, p, s]) => {
        if (!mounted) return;
        setHost(h?.value ?? "");
        setPort(p?.value ?? "");
        setSavedHost(h?.value ?? "");
        setSavedPort(p?.value ?? "");
        setStatus(s ?? null);
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
  }, []);

  const hostErr = validateHost(host);
  const portErr = validatePort(port);
  const dirty = host !== savedHost || port !== savedPort;
  const canApply = !applying && !restarting && dirty && !hostErr && !portErr;

  async function handleApply() {
    if (!canApply) return;
    setApplying(true);
    setFlash(null);
    try {
      await UpdateAppSettings({
        entries: [
          { key: KEY_HOST, value: host.trim() },
          { key: KEY_PORT, value: port.trim() },
        ],
      } as app_settings_svc.UpdateRequest);
      const s = await GetGatewayStatus();
      setStatus(s ?? null);
      setSavedHost(host.trim());
      setSavedPort(port.trim());
      if (s?.status === "running") {
        setFlash({ kind: "ok", text: "已应用并重启代理" });
      } else {
        setFlash({
          kind: "err",
          text: `已保存设置，但代理未启动：${s?.reason || "未知错误"}`,
        });
      }
    } catch (err) {
      setFlash({ kind: "err", text: messageFromError(err) });
    } finally {
      setApplying(false);
    }
  }

  async function handleRestart() {
    if (restarting || applying) return;
    setRestarting(true);
    setFlash(null);
    try {
      const res = await RestartGateway();
      setStatus(res?.status ?? null);
      if (res?.status?.status === "running") {
        setFlash({ kind: "ok", text: "已重启本地 HTTP 代理" });
      } else {
        setFlash({
          kind: "err",
          text: `代理仍未启动：${res?.status?.reason || "未知错误"}`,
        });
      }
    } catch (err) {
      setFlash({ kind: "err", text: messageFromError(err) });
    } finally {
      setRestarting(false);
    }
  }

  async function handleCopy() {
    if (!status?.listenURL) return;
    await copyTextWithToast(status.listenURL, {
      errorTitle: "复制 URL 失败",
      successTitle: "已复制 URL",
    });
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {flash ? (
        <FlashBanner state={flash} onDismiss={() => setFlash(null)} />
      ) : null}

      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <h2 className="text-sm font-semibold">监听器</h2>
            <p className="text-2xs leading-relaxed text-muted-foreground">
              claudecode / codex 子进程的所有 LLM 请求都打到这里，App 用 token
              路由到真实 LLM 供应商。
            </p>
          </div>
          <StatusPill status={status} loading={loading} />
        </header>

        <div className="grid grid-cols-1 gap-3 px-4 py-4 sm:grid-cols-3">
          <FormField
            label="监听地址"
            hint="默认 127.0.0.1，loopback 仅本机可访问"
            error={dirty ? hostErr : null}
          >
            <Input
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="127.0.0.1"
              className="font-mono"
              disabled={loading}
            />
          </FormField>
          <FormField
            label="端口"
            hint="默认 52401；0 = 每次启动随机端口"
            error={dirty ? portErr : null}
          >
            <Input
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="52401"
              className="font-mono"
              disabled={loading}
              inputMode="numeric"
            />
          </FormField>
          <FormField label="当前 URL" hint="供 claudecode / codex 透传调用">
            <UrlReadout status={status} loading={loading} />
          </FormField>
        </div>

        <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-border bg-secondary/30 px-4 py-3">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleCopy}
            disabled={!status?.listenURL || loading}
            className="h-8 gap-1.5 px-3 text-xs"
          >
            <Copy className="size-3.5" aria-hidden="true" />
            复制 URL
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleRestart}
            disabled={loading || restarting || applying}
            className="h-8 gap-1.5 px-3 text-xs"
          >
            {restarting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <RotateCcw className="size-3.5" aria-hidden="true" />
            )}
            重启代理
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={handleApply}
            disabled={!canApply}
            className="h-8 gap-1.5 px-3 text-xs"
          >
            {applying ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            应用并重启
          </Button>
        </footer>
      </section>

      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <header className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <h2 className="text-sm font-semibold">路由暴露</h2>
            <p className="text-2xs leading-relaxed text-muted-foreground">
              凭临时 token 验签后转发；token 仅在测试 / 运行期间内存存活。
            </p>
          </div>
          <Badge
            variant="secondary"
            className="rounded-sm px-1.5 py-0 font-mono text-2xs"
          >
            {status?.routes?.length ?? 0} 条
          </Badge>
        </header>
        <ul className="divide-y divide-border">
          {ROUTE_ROWS.map((r) => (
            <li
              key={r.path}
              className="flex items-center gap-3 px-4 py-2.5 text-xs"
            >
              <Badge
                variant="secondary"
                className="w-[44px] shrink-0 justify-center rounded-sm px-1 py-0 font-mono text-2xs"
              >
                {r.method}
              </Badge>
              <span
                data-selectable-text="true"
                className="w-[180px] shrink-0 truncate font-mono text-xs"
              >
                {r.path}
              </span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground">
                {r.description}
              </span>
              {r.badge ? (
                <Badge
                  variant="secondary"
                  className="rounded-sm px-1.5 py-0 font-mono text-2xs"
                >
                  {r.badge}
                </Badge>
              ) : null}
            </li>
          ))}
        </ul>
      </section>

      <Alert className="border-primary-text/30 bg-primary-soft text-primary-text">
        <ShieldCheck className="size-4" aria-hidden="true" />
        <AlertTitle className="text-xs font-semibold">密钥不出 App</AlertTitle>
        <AlertDescription className="text-2xs leading-relaxed">
          LLM 供应商的真实密钥只在主进程内存里，CLI 子进程拿到的是 App
          颁发的临时 token；token 仅 60 秒有效、测试结束后立即撤销，绝不写到
          .env / config / 进程参数。
        </AlertDescription>
      </Alert>
    </div>
  );
}

function StatusPill({
  status,
  loading,
}: {
  status: httpgateway.GatewayStatus | null;
  loading: boolean;
}) {
  if (loading) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-sm bg-secondary px-2 py-0.5 font-mono text-2xs text-muted-foreground">
        <Loader2 className="size-3 animate-spin" aria-hidden="true" />
        加载中
      </span>
    );
  }
  const running = status?.status === "running";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 font-mono text-2xs",
        running
          ? "bg-status-running-bg text-status-running"
          : "bg-destructive-soft text-destructive",
      )}
    >
      <span
        className={cn(
          "size-1.5 rounded-full",
          running ? "bg-status-running" : "bg-destructive",
        )}
        aria-hidden="true"
      />
      {running ? "运行中" : "已停止"}
    </span>
  );
}

function FormField({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  error?: string | null;
  children: React.ReactNode;
}) {
  return (
    <label className="flex min-w-0 flex-col gap-1.5 text-xs">
      <span className="font-medium">{label}</span>
      {children}
      {error ? (
        <span className="text-2xs text-destructive">{error}</span>
      ) : hint ? (
        <span className="font-mono text-2xs text-muted-foreground">{hint}</span>
      ) : null}
    </label>
  );
}

function UrlReadout({
  status,
  loading,
}: {
  status: httpgateway.GatewayStatus | null;
  loading: boolean;
}) {
  if (loading) {
    return (
      <div className="flex h-9 items-center rounded-md border border-border bg-secondary/40 px-3 font-mono text-2xs text-muted-foreground">
        加载中…
      </div>
    );
  }
  if (status?.status === "running" && status.listenURL) {
    return (
      <div
        data-selectable-text="true"
        className="flex h-9 items-center truncate rounded-md border border-border bg-secondary/40 px-3 font-mono text-2xs"
      >
        {status.listenURL}
      </div>
    );
  }
  return (
    <div className="flex h-9 items-center truncate rounded-md border border-destructive/40 bg-destructive-soft px-3 font-mono text-2xs text-destructive">
      未启动{status?.reason ? ` · ${status.reason}` : ""}
    </div>
  );
}

function FlashBanner({
  state,
  onDismiss,
}: {
  state: Exclude<FlashState, null>;
  onDismiss: () => void;
}) {
  const ok = state.kind === "ok";
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
      <span className="min-w-0 flex-1 break-words">{state.text}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={onDismiss}
        aria-label="关闭提示"
      >
        <Info data-icon="only" aria-hidden="true" />
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
