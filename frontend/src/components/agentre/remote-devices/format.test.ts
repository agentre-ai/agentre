// frontend/src/components/agentre/remote-devices/format.test.ts
import { describe, it, expect } from "vitest";
import type { TFunction } from "i18next";

import { relativeTime, deriveDeviceName, friendlyLastError } from "./format";

const t = ((key: string, params?: Record<string, unknown>) => {
  const values: Record<string, string> = {
    "remoteDevices.time.never": "从未",
    "remoteDevices.time.justNow": "刚刚",
    "remoteDevices.time.minutesAgo": `${params?.minutes} 分钟前`,
    "remoteDevices.time.hoursAgo": `${params?.hours} 小时前`,
    "remoteDevices.time.daysAgo": `${params?.days} 天前`,
    "remoteDevices.errors.tofuMismatch":
      "服务端身份指纹已变化，请确认安全后重新配对",
    "remoteDevices.errors.unauthorized": "凭据已失效，请重新配对",
    "remoteDevices.errors.dialFailed": `连接失败：${params?.message}`,
  };
  return values[key] ?? key;
}) as TFunction;

describe("relativeTime", () => {
  it("returns '刚刚' for sub-minute deltas", () => {
    const now = 1_000_000_000_000;
    expect(relativeTime(now - 5_000, now, t)).toBe("刚刚");
  });
  it("formats minutes", () => {
    const now = 1_000_000_000_000;
    expect(relativeTime(now - 3 * 60_000, now, t)).toBe("3 分钟前");
  });
  it("formats days", () => {
    const now = 1_000_000_000_000;
    expect(relativeTime(now - 2 * 86_400_000, now, t)).toBe("2 天前");
  });
  it("returns '从未' for zero", () => {
    expect(relativeTime(0, 1, t)).toBe("从未");
  });
});

describe("deriveDeviceName", () => {
  it("returns hostname segment for FQDN", () => {
    expect(deriveDeviceName("ws://linux-srv.local:7456/rpc", [])).toBe(
      "linux-srv",
    );
  });
  it("returns agentred-N for IP host", () => {
    expect(deriveDeviceName("ws://192.168.1.100:7456/rpc", [])).toBe(
      "agentred-1",
    );
  });
  it("increments N past existing agentred-N names", () => {
    expect(
      deriveDeviceName("ws://10.0.0.5:7456/rpc", [
        { name: "agentred-1" },
        { name: "agentred-2" },
        { name: "other" },
      ]),
    ).toBe("agentred-3");
  });
  it("returns empty for invalid URL", () => {
    expect(deriveDeviceName("garbage", [])).toBe("");
  });
});

describe("friendlyLastError", () => {
  it("translates known sentinels", () => {
    expect(friendlyLastError("tofu_mismatch", t)).toMatch(/fingerprint|身份/);
    expect(friendlyLastError("unauthorized", t)).toMatch(
      /credential|凭据|授权/i,
    );
  });
  it("strips dial_failed prefix", () => {
    expect(friendlyLastError("dial_failed:ECONNREFUSED", t)).toContain(
      "ECONNREFUSED",
    );
  });
  it("returns empty for empty", () => {
    expect(friendlyLastError("", t)).toBe("");
  });
});
