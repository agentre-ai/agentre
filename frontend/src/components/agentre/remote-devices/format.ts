// frontend/src/components/agentre/remote-devices/format.ts
export function relativeTime(
  thenMs: number,
  nowMs: number = Date.now(),
): string {
  if (!thenMs) return "从未";
  const delta = Math.max(0, nowMs - thenMs);
  if (delta < 60_000) return "刚刚";
  const minutes = Math.floor(delta / 60_000);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  return `${days} 天前`;
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

export function friendlyLastError(le: string): string {
  if (!le) return "";
  if (le === "tofu_mismatch")
    return "服务端身份指纹已变化，请确认安全后重新配对";
  if (le === "unauthorized") return "凭据已失效，请重新配对";
  if (le.startsWith("dial_failed:"))
    return `连接失败：${le.slice("dial_failed:".length)}`;
  return le;
}
