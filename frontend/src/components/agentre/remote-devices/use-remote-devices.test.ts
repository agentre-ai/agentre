import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RemoteDeviceList: vi.fn(),
  RemoteDeviceAdd: vi.fn(),
  RemoteDeviceRemove: vi.fn(),
  RemoteDeviceUpdateTLS: vi.fn(),
  RemoteDeviceRefresh: vi.fn(),
  RemoteDeviceRename: vi.fn(),
  ServerListDevices: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

import {
  RemoteDeviceList,
  RemoteDeviceAdd,
  ServerListDevices,
} from "../../../../wailsjs/go/app/App";
import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import {
  useRemoteDevices,
  mergeDeviceSources,
  type DeviceView,
} from "./use-remote-devices";
import type { server_svc } from "../../../../wailsjs/go/models";

const mockList = RemoteDeviceList as unknown as ReturnType<typeof vi.fn>;
const mockAdd = RemoteDeviceAdd as unknown as ReturnType<typeof vi.fn>;
const mockServerList = ServerListDevices as unknown as ReturnType<typeof vi.fn>;
const mockEventsOn = EventsOn as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockList.mockReset();
  mockAdd.mockReset();
  mockEventsOn.mockReset();
  mockServerList.mockReset();
  mockEventsOn.mockImplementation(() => vi.fn()); // 默认返回 unsubscribe stub
  // 默认未登录:ServerListDevices 拒绝 → 账号来源 unknown,不判未认领。
  mockServerList.mockRejectedValue(new Error("not logged in"));
});

const lanDevice = (over: Partial<DeviceView> = {}): DeviceView => ({
  id: 1,
  name: "linux-srv",
  url: "ws://192.168.1.100:7456/rpc",
  daemonFingerprint: "fp-1",
  instanceUUID: "u1",
  tlsMode: "default",
  tlsCertPEM: "",
  pairedAt: 1,
  lastSeenAt: 1_700_000_000_000,
  lastError: "",
  online: true,
  daemonOutdated: false,
  ...over,
});

const accountDevice = (
  over: Partial<server_svc.Device> = {},
): server_svc.Device => ({
  ID: 10,
  Name: "linux-srv",
  Kind: "agentred",
  Platform: "linux",
  Version: "0.3.0",
  Fingerprint: "fp-1",
  LastSeenAt: 1_700_000_000_000,
  Status: 1, // ACTIVE
  Online: true, // 中继在线登记(R20)
  IsThisDevice: false,
  ...over,
});

describe("mergeDeviceSources (R15)", () => {
  it("merges LAN + account rows with the same fingerprint into one row", () => {
    const rows = mergeDeviceSources([lanDevice()], {
      known: true,
      devices: [accountDevice()],
    });
    expect(rows).toHaveLength(1);
    expect(rows[0].name).toBe("linux-srv");
    expect(rows[0].account?.Fingerprint).toBe("fp-1");
    expect(rows[0].unclaimed).toBe(false);
    expect(rows[0].viaRelay).toBe(false);
    // LAN 在线 → 直连在用,中转可用。
    expect(rows[0].paths).toEqual([
      { kind: "lan", state: "in-use" },
      { kind: "relay", state: "available" },
    ]);
  });

  it("marks relay in-use and LAN dead when the LAN path is offline", () => {
    const rows = mergeDeviceSources([lanDevice({ online: false })], {
      known: true,
      devices: [accountDevice()],
    });
    expect(rows).toHaveLength(1);
    expect(rows[0].viaRelay).toBe(true);
    expect(rows[0].paths).toEqual([
      { kind: "lan", state: "dead" },
      { kind: "relay", state: "in-use" },
    ]);
  });

  it("marks a LAN-only device unclaimed when the account list is known", () => {
    const rows = mergeDeviceSources([lanDevice()], {
      known: true,
      devices: [],
    });
    expect(rows).toHaveLength(1);
    expect(rows[0].unclaimed).toBe(true);
    expect(rows[0].account).toBeUndefined();
    expect(rows[0].paths).toEqual([{ kind: "lan", state: "in-use" }]);
  });

  // 「未认领」是一句断言:这台机器不在账号清单里。指纹为空的 LAN 行(还没握过手 /
  // 旧配对行)根本无从判断,accountByFp 也刻意不收空指纹键——拿空串去查必然 miss,
  // 于是一台**已认领**的机器被标成「未认领 · 其它设备看不到它」。缺少依据时不下结论。
  it("does not claim anything about a LAN row that carries no daemon fingerprint", () => {
    const rows = mergeDeviceSources([lanDevice({ daemonFingerprint: "" })], {
      known: true,
      devices: [accountDevice()],
    });
    expect(rows).toHaveLength(1);
    expect(rows[0].account).toBeUndefined();
    expect(rows[0].unclaimed).toBe(false);
  });

  it("does not mark unclaimed when the account list is unknown (not logged in)", () => {
    const rows = mergeDeviceSources([lanDevice()], {
      known: false,
      devices: [],
    });
    expect(rows[0].unclaimed).toBe(false);
  });

  it("treats a daemon with no relay presence as a dead relay path", () => {
    const rows = mergeDeviceSources([lanDevice()], {
      known: true,
      devices: [accountDevice({ Online: false })],
    });
    expect(rows[0].viaRelay).toBe(false);
    expect(rows[0].paths).toEqual([
      { kind: "lan", state: "in-use" },
      { kind: "relay", state: "dead" },
    ]);
  });

  // R15:「该行呈现它的可达路径而非凭据来源」。账号侧的 Status 是授权标志
  // (ACTIVE / REVOKED),不是可达性 —— 一台关机的机器账号行仍是 ACTIVE,
  // 拿 Status 当路径状态会让面板宣称一条通向关机机器的中转路径。
  it("does not claim a relay path from the account authorization flag alone", () => {
    const rows = mergeDeviceSources([lanDevice({ online: false })], {
      known: true,
      devices: [accountDevice({ Status: 1, Online: false })],
    });
    expect(rows[0].viaRelay).toBe(false);
    expect(rows[0].paths).toEqual([
      { kind: "lan", state: "dead" },
      { kind: "relay", state: "dead" },
    ]);
  });

  // 反向:授权已撤销但机器此刻仍挂在中转上 —— 路径标记跟随可达性,
  // 撤销后 daemon 无法续期在线登记,Online 自然落回 false。
  it("reports a reachable relay path even when the account row is not ACTIVE", () => {
    const rows = mergeDeviceSources([lanDevice({ online: false })], {
      known: true,
      devices: [accountDevice({ Status: 2, Online: true })],
    });
    expect(rows[0].viaRelay).toBe(true);
    expect(rows[0].paths).toEqual([
      { kind: "lan", state: "dead" },
      { kind: "relay", state: "in-use" },
    ]);
  });

  it("keeps distinct rows per LAN fingerprint", () => {
    const rows = mergeDeviceSources(
      [
        lanDevice({ id: 1, daemonFingerprint: "fp-1" }),
        lanDevice({ id: 2, name: "pi", daemonFingerprint: "fp-2" }),
      ],
      { known: true, devices: [accountDevice()] },
    );
    expect(rows).toHaveLength(2);
    expect(rows.map((r) => r.name)).toEqual(["linux-srv", "pi"]);
  });
});

describe("useRemoteDevices", () => {
  it("loads devices on mount", async () => {
    mockList.mockResolvedValueOnce([{ id: 1, name: "a" }]);
    const { result } = renderHook(() => useRemoteDevices());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.devices[0].name).toBe("a");
    // 未登录 → 无账号来源,单行只有 LAN 直连路径。
    expect(result.current.devices[0].paths).toEqual([
      { kind: "lan", state: "dead" },
    ]);
    expect(result.current.devices[0].unclaimed).toBe(false);
  });

  it("reloads on window focus", async () => {
    mockList.mockResolvedValueOnce([]);
    renderHook(() => useRemoteDevices());
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
    mockList.mockResolvedValueOnce([{ id: 2, name: "b" }]);
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it("add() calls binding then reloads", async () => {
    mockList.mockResolvedValue([]);
    mockAdd.mockResolvedValueOnce({ id: 3, name: "c" });
    const { result } = renderHook(() => useRemoteDevices());
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => {
      await result.current.add({
        url: "ws://h/rpc",
        pairingCode: "ABC2DE",
        displayName: "c",
        tlsMode: "default",
        tlsCertPEM: "",
      });
    });
    expect(mockAdd).toHaveBeenCalled();
    expect(mockList).toHaveBeenCalledTimes(2);
  });

  it("merges remote.device.state events into devices by id", async () => {
    mockList.mockResolvedValueOnce([
      { id: 1, name: "a", online: false, lastSeenAt: 0, lastError: "" },
      { id: 2, name: "b", online: false, lastSeenAt: 0, lastError: "" },
    ]);
    const handlers: Record<string, (p: unknown) => void> = {};
    mockEventsOn.mockImplementation(
      (name: string, fn: (p: unknown) => void) => {
        handlers[name] = fn;
        return () => {};
      },
    );

    const { result } = renderHook(() => useRemoteDevices());
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      handlers["remote.device.state"]({
        id: 1,
        name: "a",
        online: true,
        lastSeenAt: 12345,
        lastError: "",
      });
    });

    expect(result.current.devices.find((d) => d.id === 1)?.online).toBe(true);
    expect(result.current.devices.find((d) => d.id === 1)?.lastSeenAt).toBe(
      12345,
    );
    // 在线态变化后路径重算:LAN 直连从 dead 翻成 in-use。
    expect(result.current.devices.find((d) => d.id === 1)?.paths).toEqual([
      { kind: "lan", state: "in-use" },
    ]);
    expect(result.current.devices.find((d) => d.id === 2)?.online).toBe(false);
  });

  it("ignores events for unknown id", async () => {
    mockList.mockResolvedValueOnce([
      { id: 1, name: "a", online: false, lastSeenAt: 0, lastError: "" },
    ]);
    const handlers: Record<string, (p: unknown) => void> = {};
    mockEventsOn.mockImplementation(
      (name: string, fn: (p: unknown) => void) => {
        handlers[name] = fn;
        return () => {};
      },
    );
    const { result } = renderHook(() => useRemoteDevices());
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => {
      handlers["remote.device.state"]({
        id: 999,
        name: "?",
        online: true,
        lastSeenAt: 1,
        lastError: "",
      });
    });
    expect(result.current.devices).toHaveLength(1);
    expect(result.current.devices[0].id).toBe(1);
  });
});
