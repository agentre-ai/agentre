import * as React from "react";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  Info,
  Loader2,
  RefreshCw,
  RotateCw,
} from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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
import { cn } from "@/lib/utils";

import { Info as FetchAppInfo } from "../../../wailsjs/go/app/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime/runtime";
import {
  CHECKSUM_FETCH_ERROR_PREFIX,
  checkForUpdate,
  downloadAndInstallUpdate,
  getAvailableMirrors,
  getDownloadMirror,
  getUpdateChannel,
  restartApp,
  setDownloadMirror,
  setUpdateChannel,
  type MirrorInfo,
  type UpdateChannel,
  type UpdateInfo,
} from "./update-api";

const CHANNEL_LABEL: Record<UpdateChannel, string> = {
  stable: "稳定版",
  beta: "测试版",
  nightly: "每夜构建",
};

const CHANNEL_DESC: Record<UpdateChannel, string> = {
  stable: "面向所有用户的正式版本，更新频率最低。",
  beta: "尝鲜新特性，可能含少量已知问题。",
  nightly: "每日构建，更新最快，稳定性最低。",
};

// MIRROR_CUSTOM_ID select 中"自定义"选项的特殊值；选中时显示 input。
const MIRROR_CUSTOM_ID = "__custom__";

type Phase =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "uptodate" }
  | { kind: "available"; info: UpdateInfo }
  | { kind: "downloading"; info: UpdateInfo; progress: number }
  | { kind: "installed"; info: UpdateInfo }
  | { kind: "error"; message: string };

type ChecksumPrompt = { open: boolean; reason: string };

function formatVersion(v: string): string {
  if (!v) return "未知";
  return v.startsWith("v") ? v : `v${v}`;
}

function formatProgress(p: number): string {
  if (!Number.isFinite(p) || p <= 0) return "0%";
  if (p >= 100) return "100%";
  return `${p.toFixed(0)}%`;
}

function pickMirrorOption(
  builtins: MirrorInfo[],
  current: string,
): { selectValue: string; customDraft: string } {
  const found = builtins.find((m) => m.url === current);
  if (found) {
    return { selectValue: found.id, customDraft: "" };
  }
  if (current === "") {
    return { selectValue: "github", customDraft: "" };
  }
  return { selectValue: MIRROR_CUSTOM_ID, customDraft: current };
}

export function UpdateSection() {
  const [appVersion, setAppVersion] = React.useState<string>("");
  const [appCommit, setAppCommit] = React.useState<string>("");
  const [channel, setChannel] = React.useState<UpdateChannel>("stable");
  const [mirrors, setMirrors] = React.useState<MirrorInfo[]>([]);
  const [mirrorSelectValue, setMirrorSelectValue] =
    React.useState<string>("github");
  const [customMirror, setCustomMirror] = React.useState<string>("");
  const [phase, setPhase] = React.useState<Phase>({ kind: "idle" });
  const [checksumPrompt, setChecksumPrompt] = React.useState<ChecksumPrompt>({
    open: false,
    reason: "",
  });

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const info = await FetchAppInfo();
        if (cancelled) return;
        setAppVersion(info.version ?? "");
        setAppCommit(info.commit ?? "");
      } catch (err) {
        console.warn("fetch app info failed", err);
      }

      try {
        const [ch, mr, ms] = await Promise.all([
          getUpdateChannel(),
          getDownloadMirror(),
          getAvailableMirrors(),
        ]);
        if (cancelled) return;
        setChannel(ch);
        setMirrors(ms);
        const picked = pickMirrorOption(ms, mr);
        setMirrorSelectValue(picked.selectValue);
        setCustomMirror(picked.customDraft);
      } catch (err) {
        console.warn("fetch update settings failed", err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  React.useEffect(() => {
    const handler = (payload: { downloaded?: number; total?: number }) => {
      if (!payload || !payload.total || payload.total <= 0) return;
      const pct = Math.min(
        100,
        Math.round(((payload.downloaded ?? 0) / payload.total) * 100),
      );
      setPhase((prev) => {
        if (prev.kind === "downloading") {
          return { ...prev, progress: pct };
        }
        return prev;
      });
    };
    EventsOn("update:progress", handler);
    return () => {
      EventsOff("update:progress");
    };
  }, []);

  const handleChannelChange = React.useCallback(async (next: string) => {
    const value = next as UpdateChannel;
    setChannel(value);
    try {
      await setUpdateChannel(value);
      // 切通道后清掉先前的检查结果，避免误导。
      setPhase({ kind: "idle" });
    } catch (err) {
      console.warn("save update channel failed", err);
    }
  }, []);

  const persistMirror = React.useCallback(async (url: string) => {
    try {
      await setDownloadMirror(url);
    } catch (err) {
      console.warn("save mirror failed", err);
    }
  }, []);

  const handleMirrorSelectChange = React.useCallback(
    async (next: string) => {
      setMirrorSelectValue(next);
      if (next === MIRROR_CUSTOM_ID) {
        // 切到自定义不立即写库，等用户 onBlur 时再写。
        return;
      }
      const found = mirrors.find((m) => m.id === next);
      const url = found?.url ?? "";
      setCustomMirror("");
      await persistMirror(url);
    },
    [mirrors, persistMirror],
  );

  const handleCustomMirrorBlur = React.useCallback(async () => {
    if (mirrorSelectValue !== MIRROR_CUSTOM_ID) return;
    await persistMirror(customMirror.trim());
  }, [customMirror, mirrorSelectValue, persistMirror]);

  const handleCheck = React.useCallback(async () => {
    setPhase({ kind: "checking" });
    try {
      const info = await checkForUpdate();
      if (info.hasUpdate) {
        setPhase({ kind: "available", info });
      } else {
        setPhase({ kind: "uptodate" });
      }
    } catch (err) {
      setPhase({
        kind: "error",
        message: err instanceof Error ? err.message : String(err),
      });
    }
  }, []);

  const startDownload = React.useCallback(
    async (info: UpdateInfo, skipChecksum: boolean) => {
      setPhase({ kind: "downloading", info, progress: 0 });
      try {
        await downloadAndInstallUpdate(skipChecksum);
        setPhase({ kind: "installed", info });
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        if (message.startsWith(CHECKSUM_FETCH_ERROR_PREFIX)) {
          const detail = message.slice(CHECKSUM_FETCH_ERROR_PREFIX.length);
          setChecksumPrompt({ open: true, reason: detail });
          setPhase({ kind: "available", info });
          return;
        }
        setPhase({ kind: "error", message });
      }
    },
    [],
  );

  const handleDownload = React.useCallback(() => {
    if (phase.kind !== "available") return;
    void startDownload(phase.info, false);
  }, [phase, startDownload]);

  const handleSkipChecksum = React.useCallback(() => {
    if (phase.kind !== "available") {
      setChecksumPrompt({ open: false, reason: "" });
      return;
    }
    setChecksumPrompt({ open: false, reason: "" });
    void startDownload(phase.info, true);
  }, [phase, startDownload]);

  const handleRestart = React.useCallback(async () => {
    try {
      await restartApp();
    } catch (err) {
      setPhase({
        kind: "error",
        message: err instanceof Error ? err.message : String(err),
      });
    }
  }, []);

  const checkButtonState = (() => {
    if (phase.kind === "checking") {
      return { disabled: true, label: "检查中...", icon: Loader2, spin: true };
    }
    if (phase.kind === "downloading") {
      return { disabled: true, label: "下载中...", icon: Loader2, spin: true };
    }
    return { disabled: false, label: "检查更新", icon: RefreshCw, spin: false };
  })();
  const CheckIcon = checkButtonState.icon;

  return (
    <>
      <SectionHeader />

      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <h2 className="text-sm font-semibold">当前版本</h2>
            <p className="text-xs leading-relaxed text-muted-foreground">
              通过 GitHub Releases 检查并安装新版本。
            </p>
          </div>
          <Badge
            variant="secondary"
            className="rounded-sm px-1.5 py-0 font-mono text-2xs font-medium"
          >
            {formatVersion(appVersion)}
            {appCommit ? ` · ${appCommit}` : ""}
          </Badge>
        </div>

        <div className="flex flex-col gap-4 p-4">
          <ChannelRow
            channel={channel}
            onChange={handleChannelChange}
            disabled={phase.kind === "downloading"}
          />
          <MirrorRow
            mirrors={mirrors}
            selectValue={mirrorSelectValue}
            customDraft={customMirror}
            onSelectChange={handleMirrorSelectChange}
            onCustomChange={setCustomMirror}
            onCustomBlur={handleCustomMirrorBlur}
            disabled={phase.kind === "downloading"}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              onClick={handleCheck}
              disabled={checkButtonState.disabled}
              variant="default"
            >
              <CheckIcon
                aria-hidden="true"
                className={cn(
                  "size-4",
                  checkButtonState.spin && "animate-spin",
                )}
              />
              {checkButtonState.label}
            </Button>
            {phase.kind === "uptodate" ? (
              <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                <CheckCircle2 className="size-3.5 text-emerald-500" />
                已是最新版本
              </span>
            ) : null}
          </div>
        </div>
      </section>

      {phase.kind === "available" || phase.kind === "downloading" ? (
        <AvailableCard
          info={phase.info}
          downloading={phase.kind === "downloading"}
          progress={phase.kind === "downloading" ? phase.progress : 0}
          onDownload={handleDownload}
        />
      ) : null}

      {phase.kind === "installed" ? (
        <InstalledCard info={phase.info} onRestart={handleRestart} />
      ) : null}

      {phase.kind === "error" ? <ErrorCard message={phase.message} /> : null}

      <ChecksumDialog
        open={checksumPrompt.open}
        reason={checksumPrompt.reason}
        onCancel={() => setChecksumPrompt({ open: false, reason: "" })}
        onConfirm={handleSkipChecksum}
      />
    </>
  );
}

function SectionHeader() {
  return (
    <div className="flex max-w-3xl flex-col gap-1.5">
      <h1 className="text-2xl font-semibold tracking-normal">
        版本 &amp; 更新
      </h1>
      <p className="text-sm leading-relaxed text-muted-foreground">
        启动时会自动后台检查一次新版本。也可以手动切换通道或选择下载镜像加速国内访问。
      </p>
    </div>
  );
}

function ChannelRow({
  channel,
  onChange,
  disabled,
}: {
  channel: UpdateChannel;
  onChange: (next: string) => void;
  disabled: boolean;
}) {
  const labelId = React.useId();
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span id={labelId} className="text-sm font-medium">
          更新通道
        </span>
        <p className="text-xs leading-relaxed text-muted-foreground">
          {CHANNEL_DESC[channel]}
        </p>
      </div>
      <div className="w-full sm:w-[220px]">
        <Select value={channel} onValueChange={onChange} disabled={disabled}>
          <SelectTrigger aria-labelledby={labelId}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(["stable", "beta", "nightly"] as const).map((c) => (
              <SelectItem key={c} value={c}>
                {CHANNEL_LABEL[c]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}

function MirrorRow({
  mirrors,
  selectValue,
  customDraft,
  onSelectChange,
  onCustomChange,
  onCustomBlur,
  disabled,
}: {
  mirrors: MirrorInfo[];
  selectValue: string;
  customDraft: string;
  onSelectChange: (v: string) => void;
  onCustomChange: (v: string) => void;
  onCustomBlur: () => void;
  disabled: boolean;
}) {
  const labelId = React.useId();
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 flex-col gap-0.5 sm:max-w-[300px]">
        <span id={labelId} className="text-sm font-medium">
          下载镜像
        </span>
        <p className="text-xs leading-relaxed text-muted-foreground">
          国内访问 GitHub 慢时可以走加速镜像。自定义需保留协议头（例如
          https://your.mirror/）。
        </p>
      </div>
      <div className="flex w-full flex-col gap-2 sm:w-[260px]">
        <Select
          value={selectValue}
          onValueChange={onSelectChange}
          disabled={disabled}
        >
          <SelectTrigger aria-labelledby={labelId}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {mirrors.map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.name}
              </SelectItem>
            ))}
            <SelectItem value={MIRROR_CUSTOM_ID}>自定义...</SelectItem>
          </SelectContent>
        </Select>
        {selectValue === MIRROR_CUSTOM_ID ? (
          <Input
            type="url"
            value={customDraft}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              onCustomChange(e.target.value)
            }
            onBlur={onCustomBlur}
            placeholder="https://your.mirror/"
            disabled={disabled}
          />
        ) : null}
      </div>
    </div>
  );
}

function AvailableCard({
  info,
  downloading,
  progress,
  onDownload,
}: {
  info: UpdateInfo;
  downloading: boolean;
  progress: number;
  onDownload: () => void;
}) {
  return (
    <section className="overflow-hidden rounded-lg border border-agent-1/30 bg-agent-1/5">
      <div className="flex flex-wrap items-center gap-3 border-b border-agent-1/20 px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h2 className="text-sm font-semibold text-agent-1">
            发现新版本 {formatVersion(info.latestVersion)}
          </h2>
          <p className="text-xs leading-relaxed text-muted-foreground">
            发布于 {info.publishedAt || "未知时间"}
          </p>
        </div>
      </div>

      <div className="flex flex-col gap-4 p-4">
        {info.releaseNotes ? (
          <pre className="max-h-[220px] overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted/40 p-3 text-xs leading-relaxed">
            {info.releaseNotes}
          </pre>
        ) : (
          <p className="text-xs text-muted-foreground">本次发布暂无说明。</p>
        )}

        {downloading ? (
          <div className="flex flex-col gap-2">
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-agent-1 transition-[width] duration-200"
                style={{ width: `${progress}%` }}
              />
            </div>
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>下载中</span>
              <span className="font-mono">{formatProgress(progress)}</span>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" onClick={onDownload}>
              <Download aria-hidden="true" className="size-4" />
              下载并安装
            </Button>
          </div>
        )}
      </div>
    </section>
  );
}

function InstalledCard({
  info,
  onRestart,
}: {
  info: UpdateInfo;
  onRestart: () => void;
}) {
  return (
    <section className="overflow-hidden rounded-lg border border-emerald-500/30 bg-emerald-500/5">
      <div className="flex flex-wrap items-center gap-3 border-b border-emerald-500/20 px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h2 className="text-sm font-semibold text-emerald-600">
            {formatVersion(info.latestVersion)} 已安装
          </h2>
          <p className="text-xs leading-relaxed text-muted-foreground">
            重启应用以加载新版本。
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 p-4">
        <Button type="button" onClick={onRestart}>
          <RotateCw aria-hidden="true" className="size-4" />
          立即重启
        </Button>
      </div>
    </section>
  );
}

function ErrorCard({ message }: { message: string }) {
  return (
    <Alert variant="destructive">
      <AlertCircle className="size-4" aria-hidden="true" />
      <AlertTitle className="text-xs font-semibold">操作失败</AlertTitle>
      <AlertDescription className="text-2xs leading-relaxed">
        {message}
      </AlertDescription>
    </Alert>
  );
}

function ChecksumDialog({
  open,
  reason,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  reason: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o: boolean) => (!o ? onCancel() : undefined)}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Info className="size-4 text-amber-500" aria-hidden="true" />
            校验文件下载失败
          </DialogTitle>
          <DialogDescription>
            SHA256SUMS.txt 获取不到，无法校验下载文件完整性。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="text-xs leading-relaxed">
          <div className="rounded-md border border-border bg-muted/40 p-2 font-mono text-2xs">
            {reason}
          </div>
          <p className="mt-3 text-muted-foreground">
            继续下载将跳过完整性校验，请确认信任当前下载源。
          </p>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onCancel}>
            取消
          </Button>
          <Button type="button" variant="destructive" onClick={onConfirm}>
            跳过校验继续
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
