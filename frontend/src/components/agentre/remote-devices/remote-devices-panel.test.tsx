import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RemoteDeviceList: vi.fn().mockResolvedValue([]),
  RemoteDeviceAdd: vi.fn(),
  RemoteDeviceRemove: vi.fn(),
  RemoteDeviceUpdateTLS: vi.fn(),
  RemoteDeviceRefresh: vi.fn(),
  RemoteDeviceRename: vi.fn(),
  // 默认未登录:账号来源 unknown。R15 合并用例在测试里单独覆盖成已登录。
  ServerListDevices: vi.fn().mockRejectedValue(new Error("not logged in")),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

import {
  RemoteDeviceList,
  ServerListDevices,
} from "../../../../wailsjs/go/app/App";
import { RemoteDevicesPanel } from "./remote-devices-panel";
import type { DeviceView } from "./use-remote-devices";

const mockList = RemoteDeviceList as unknown as ReturnType<typeof vi.fn>;
const mockServerList = ServerListDevices as unknown as ReturnType<typeof vi.fn>;

describe("RemoteDevicesPanel", () => {
  beforeEach(() => {
    mockList.mockReset();
    mockServerList.mockReset();
    mockServerList.mockRejectedValue(new Error("not logged in"));
  });

  it("shows empty state when no devices", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(
        screen.getByText(/No agentred devices paired/),
      ).toBeInTheDocument(),
    );
  });

  it("renders a row per device + counters", async () => {
    mockList.mockResolvedValueOnce([
      {
        id: 1,
        name: "mac",
        url: "ws://h1/rpc",
        tlsMode: "default",
        online: true,
        lastSeenAt: Date.now(),
      },
      {
        id: 2,
        name: "pi",
        url: "ws://h2/rpc",
        tlsMode: "default",
        online: false,
        lastSeenAt: 0,
      },
    ] as Partial<DeviceView>[]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(screen.getAllByTestId("device-row")).toHaveLength(2),
    );
    expect(screen.getByText("2 paired · 1 online")).toBeInTheDocument();
  });

  // 决策 12:移除那个形似筛选器的独立标签 —— 它看上去在等一个兄弟标签,
  // 但设备通常一到三台,按路径筛选没有意义。
  it("no longer renders the filter-like LAN tag", async () => {
    mockList.mockResolvedValueOnce([
      {
        id: 1,
        name: "mac",
        url: "ws://h1/rpc",
        tlsMode: "default",
        online: true,
        lastSeenAt: Date.now(),
      },
    ] as Partial<DeviceView>[]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(screen.getAllByTestId("device-row")).toHaveLength(1),
    );
    expect(screen.queryByText(/LAN direct · All/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/LAN 直连 · 全部/)).not.toBeInTheDocument();
  });

  // R15 测试接缝:同一指纹的两个来源合并为一行且路径标记正确。
  it("merges same-fingerprint LAN + account devices into one row with path markers", async () => {
    mockList.mockResolvedValueOnce([
      {
        id: 1,
        name: "home-server",
        url: "ws://192.168.1.50:7456/rpc",
        daemonFingerprint: "fp-1",
        tlsMode: "default",
        online: false,
        lastSeenAt: 1_700_000_000_000,
      },
    ] as Partial<DeviceView>[]);
    mockServerList.mockResolvedValueOnce([
      {
        ID: 10,
        Name: "home-server",
        Kind: "agentred",
        Platform: "linux",
        Version: "0.3.0",
        Fingerprint: "fp-1",
        Capabilities: {},
        LastSeenAt: 1_700_000_000_000,
        Status: 1,
        // 中转路径可达 = daemon 的中继在线登记(R20),不是账号侧授权标志。
        Online: true,
        IsThisDevice: false,
      },
    ]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(screen.getAllByTestId("device-row")).toHaveLength(1),
    );
    // LAN 离线 → 直连失效(带文字),中转在用(带文字),地址显示「经中转」。
    expect(screen.getByText("Direct · Unreachable")).toBeInTheDocument();
    expect(screen.getByLabelText("Relay · In use")).toBeInTheDocument();
    expect(screen.getByText(/Via relay/)).toBeInTheDocument();
    expect(screen.queryByText(/192\.168\.1\.50/)).not.toBeInTheDocument();
  });
});
