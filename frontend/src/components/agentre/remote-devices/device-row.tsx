// frontend/src/components/agentre/remote-devices/device-row.tsx
import { useState } from "react";
import { Server } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { DeviceActionMenu } from "./device-action-menu";
import { DeviceProvidersSync } from "./device-providers-sync";
import { relativeTime, friendlyLastError } from "./format";
import type { DevicePath, DeviceRowModel } from "./use-remote-devices";

type Props = {
  device: DeviceRowModel;
  now: number;
  onRefresh: () => void;
  onRename: () => void;
  onEditTLS: () => void;
  onRemove: () => void;
};

function tlsBadgeVariant(
  mode: string,
): "secondary" | "outline" | "destructive" {
  if (mode === "skip-verify") return "destructive";
  if (mode === "pin-cert" || mode === "ca-bundle") return "outline";
  return "secondary";
}

function tlsBadgeLabel(mode: string, t: TFunction): string {
  switch (mode) {
    case "default":
      return t("remoteDevices.tls.modes.default.label");
    case "pin-cert":
      return t("remoteDevices.tls.modes.pinCert.label");
    case "ca-bundle":
      return t("remoteDevices.tls.modes.caBundle.label");
    case "skip-verify":
      return t("remoteDevices.tls.modes.skipVerify.label");
    default:
      return mode;
  }
}

function dotColor(device: DeviceRowModel): string {
  if (device.lastError === "tofu_mismatch") return "bg-destructive";
  if (device.online) return "bg-status-running";
  return "bg-muted-foreground";
}

// ── R15 可达路径 chip ───────────────────────────────────────────────────────
// 路径信息只在设备面板呈现(R16),不进入聊天界面。失效态除样式(划线/淡出)外
// 另有文字表达;在用态高亮并以 aria-label 说明 —— 均不靠颜色单独传达。
function PathChip({ path, t }: { path: DevicePath; t: TFunction }) {
  const label = t(
    path.kind === "lan" ? "remoteDevices.path.lan" : "remoteDevices.path.relay",
  );
  const inUse = path.state === "in-use";
  const dead = path.state === "dead";
  const aria = inUse
    ? t("remoteDevices.path.inUseAria", { path: label })
    : dead
      ? t("remoteDevices.path.unreachableAria", { path: label })
      : label;
  return (
    <span
      aria-label={aria}
      title={aria}
      className={cn(
        "inline-flex shrink-0 items-center rounded-full border px-2 text-[11px] leading-5",
        inUse
          ? "border-primary bg-primary-soft font-semibold text-primary-text"
          : "border-border-strong text-muted-foreground",
        dead && "line-through opacity-60",
      )}
    >
      {dead ? t("remoteDevices.path.unreachableLabel", { path: label }) : label}
    </span>
  );
}

// 未认领行的认领动作:认领是 daemon 侧动作(agentred login),桌面端给的是指引
// 与后果说明。认领后,同一账号下的其它设备才能看到这台机器。
function UnclaimedNote({ t }: { t: TFunction }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col gap-1 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline">{t("remoteDevices.unclaimed.label")}</Badge>
        <span className="text-muted-foreground">
          {t("remoteDevices.unclaimed.note")}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-6 px-1.5 text-xs"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          {t("remoteDevices.unclaimed.claim")}
        </Button>
      </div>
      {open ? (
        <div className="text-muted-foreground">
          {t("remoteDevices.unclaimed.instructions")}
        </div>
      ) : null}
    </div>
  );
}

export function DeviceRow({
  device,
  now,
  onRefresh,
  onRename,
  onEditTLS,
  onRemove,
}: Props) {
  const { t } = useTranslation();
  const friendlyErr = friendlyLastError(device.lastError, t);
  const isTofu = device.lastError === "tofu_mismatch";
  const [showProviders, setShowProviders] = useState(false);

  return (
    <div
      data-testid="device-row"
      className={`flex flex-col gap-1 rounded-md border p-3 ${
        isTofu ? "border-destructive bg-destructive/5" : "border-border bg-card"
      }`}
    >
      <div className="flex items-center gap-3">
        <span
          aria-label={
            device.online
              ? t("remoteDevices.status.online")
              : t("remoteDevices.status.offline")
          }
          className={`h-2 w-2 rounded-full ${dotColor(device)}`}
        />
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-secondary">
          <Server className="h-4 w-4" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-medium truncate">{device.name}</span>
            <Badge variant={tlsBadgeVariant(device.tlsMode)}>
              {tlsBadgeLabel(device.tlsMode, t)}
            </Badge>
          </div>
          <div className="text-xs text-muted-foreground truncate">
            {/* R15:地址位显示 LAN url,或中转路径在用时的「经中转」。 */}
            {device.viaRelay ? t("remoteDevices.status.viaRelay") : device.url}
            <span className="mx-2">·</span>
            {device.lastSeenAt > 0
              ? t("remoteDevices.status.lastConnected", {
                  time: relativeTime(device.lastSeenAt, now, t),
                })
              : t("remoteDevices.status.neverConnected")}
          </div>
        </div>
        {device.paths.length > 0 ? (
          <div className="flex shrink-0 items-center gap-1">
            {device.paths.map((p) => (
              <PathChip key={p.kind} path={p} t={t} />
            ))}
          </div>
        ) : null}
        <DeviceActionMenu
          onRefresh={onRefresh}
          onRename={onRename}
          onEditTLS={onEditTLS}
          onRemove={onRemove}
          onToggleProviders={() => setShowProviders((s) => !s)}
        />
      </div>
      {device.unclaimed ? <UnclaimedNote t={t} /> : null}
      {friendlyErr ? (
        <div
          className={`text-xs ${isTofu ? "text-destructive" : "text-muted-foreground"}`}
        >
          {friendlyErr}
        </div>
      ) : null}
      {/* R18：daemon 版本过旧是这台设备的固有属性，说明就落在这台设备这一行。 */}
      {device.daemonOutdated ? (
        <div className="text-xs text-status-waiting">
          {t("remoteDevices.status.daemonOutdated")}
        </div>
      ) : null}
      {showProviders ? <DeviceProvidersSync deviceId={device.id} /> : null}
    </div>
  );
}
