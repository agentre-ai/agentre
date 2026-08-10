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

/** Formats a whole-second countdown as M:SS, clamped at 0:00. */
export function formatCountdown(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const m = Math.floor(s / 60);
  const sec = s % 60;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

/** Extracts host[:port] from a URL for display; falls back to the raw input. */
export function hostOf(url: string): string {
  try {
    return new URL(url).host || url;
  } catch {
    return url;
  }
}

// server_svc's login/poll bindings return raw Go error strings (not
// i18n.NewError-wrapped — see internal/app/server.go), so the frontend maps
// the known server_svc sentinels to translated copy and falls back to the
// raw message for anything else, mirroring friendlyLastError above.
export function friendlyLoginError(e: unknown, t: TFunction): string {
  const msg = e instanceof Error ? e.message : typeof e === "string" ? e : "";
  switch (msg) {
    case "server: unreachable":
      return t("remoteDevices.login.errors.unreachable");
    case "server: access denied":
      return t("remoteDevices.login.errors.accessDenied");
    case "server: device code expired":
      return t("remoteDevices.login.errors.expired");
    case "server: login already in progress":
      return t("remoteDevices.login.errors.inProgress");
    default:
      return msg || t("remoteDevices.login.errors.generic");
  }
}
