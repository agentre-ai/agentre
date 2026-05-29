// frontend/src/components/agentre/remote-devices/device-row.tsx
import { useState } from "react";
import { Server } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { Badge } from "@/components/ui/badge";

import { DeviceActionMenu } from "./device-action-menu";
import { DeviceProvidersSync } from "./device-providers-sync";
import { relativeTime, friendlyLastError } from "./format";
import type { DeviceView } from "./use-remote-devices";

type Props = {
  device: DeviceView;
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

function dotColor(device: DeviceView): string {
  if (device.lastError === "tofu_mismatch") return "bg-destructive";
  if (device.online) return "bg-emerald-500";
  return "bg-muted-foreground";
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
            {device.url}
            <span className="mx-2">·</span>
            {device.lastSeenAt > 0
              ? t("remoteDevices.status.lastConnected", {
                  time: relativeTime(device.lastSeenAt, now, t),
                })
              : t("remoteDevices.status.neverConnected")}
          </div>
        </div>
        <DeviceActionMenu
          onRefresh={onRefresh}
          onRename={onRename}
          onEditTLS={onEditTLS}
          onRemove={onRemove}
          onToggleProviders={() => setShowProviders((s) => !s)}
        />
      </div>
      {friendlyErr ? (
        <div
          className={`text-xs ${isTofu ? "text-destructive" : "text-muted-foreground"}`}
        >
          {friendlyErr}
        </div>
      ) : null}
      {showProviders ? <DeviceProvidersSync deviceId={device.id} /> : null}
    </div>
  );
}
