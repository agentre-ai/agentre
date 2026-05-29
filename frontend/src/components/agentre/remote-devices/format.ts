import type { TFunction } from "i18next";

// frontend/src/components/agentre/remote-devices/format.ts
export function relativeTime(
  thenMs: number,
  nowMs: number,
  t: TFunction,
): string {
  if (!thenMs) return t("remoteDevices.time.never");
  const delta = Math.max(0, nowMs - thenMs);
  if (delta < 60_000) return t("remoteDevices.time.justNow");
  const minutes = Math.floor(delta / 60_000);
  if (minutes < 60) return t("remoteDevices.time.minutesAgo", { minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("remoteDevices.time.hoursAgo", { hours });
  const days = Math.floor(hours / 24);
  return t("remoteDevices.time.daysAgo", { days });
}

const IP_RE = /^\d{1,3}(\.\d{1,3}){3}$/;

export function deriveDeviceName(
  url: string,
  existing: Array<{ name: string }>,
): string {
  try {
    const u = new URL(url);
    const host = u.hostname;
    if (!host) return "";
    if (IP_RE.test(host)) {
      let n = 1;
      const used = new Set(
        existing.map((d) => d.name).filter((n) => /^agentred-\d+$/.test(n)),
      );
      while (used.has(`agentred-${n}`)) n++;
      return `agentred-${n}`;
    }
    return host.split(".")[0] || host;
  } catch {
    return "";
  }
}

export function friendlyLastError(le: string, t: TFunction): string {
  if (!le) return "";
  if (le === "tofu_mismatch") return t("remoteDevices.errors.tofuMismatch");
  if (le === "unauthorized") return t("remoteDevices.errors.unauthorized");
  if (le.startsWith("dial_failed:"))
    return t("remoteDevices.errors.dialFailed", {
      message: le.slice("dial_failed:".length),
    });
  return le;
}
