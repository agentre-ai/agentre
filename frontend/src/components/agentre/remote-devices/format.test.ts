// frontend/src/components/agentre/remote-devices/format.test.ts
import { describe, it, expect } from "vitest";
import type { TFunction } from "i18next";

import {
  relativeTime,
  deriveDeviceName,
  friendlyLastError,
  hostOf,
  friendlyLoginError,
  formatCountdown,
} from "./format";

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
    "remoteDevices.login.errors.unreachable":
      "无法连接服务器，请检查地址后重试。",
    "remoteDevices.login.errors.accessDenied": "登录已被拒绝。",
    "remoteDevices.login.errors.expired": "验证码已过期，请重新登录。",
    "remoteDevices.login.errors.inProgress": "已有登录流程正在进行。",
    "remoteDevices.login.errors.generic": "登录失败。",
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

describe("formatCountdown", () => {
  it("formats whole minutes", () => {
    expect(formatCountdown(900)).toBe("15:00");
  });
  it("pads seconds below a minute", () => {
    expect(formatCountdown(65)).toBe("1:05");
    expect(formatCountdown(59)).toBe("0:59");
  });
  it("floors at zero and ignores negatives", () => {
    expect(formatCountdown(0)).toBe("0:00");
    expect(formatCountdown(-5)).toBe("0:00");
  });
  it("rounds fractional seconds down", () => {
    expect(formatCountdown(899.9)).toBe("14:59");
  });
});

describe("hostOf", () => {
  it("extracts host from a full URL", () => {
    expect(hostOf("https://hub.example.com/rpc")).toBe("hub.example.com");
  });
  it("keeps a port suffix", () => {
    expect(hostOf("http://localhost:8080")).toBe("localhost:8080");
  });
  it("falls back to the raw string when parsing fails", () => {
    expect(hostOf("not a url")).toBe("not a url");
  });
});

describe("friendlyLoginError (login error surfacing)", () => {
  it("translates known server_svc sentinel errors", () => {
    expect(friendlyLoginError(new Error("server: unreachable"), t)).toBe(
      "无法连接服务器，请检查地址后重试。",
    );
    expect(friendlyLoginError(new Error("server: access denied"), t)).toBe(
      "登录已被拒绝。",
    );
    expect(
      friendlyLoginError(new Error("server: device code expired"), t),
    ).toBe("验证码已过期，请重新登录。");
    expect(
      friendlyLoginError(new Error("server: login already in progress"), t),
    ).toBe("已有登录流程正在进行。");
  });
  it("falls back to the raw message for unknown errors", () => {
    expect(friendlyLoginError(new Error("boom"), t)).toBe("boom");
  });
  it("falls back to a generic message when there is no message at all", () => {
    expect(friendlyLoginError({}, t)).toBe("登录失败。");
  });
});
