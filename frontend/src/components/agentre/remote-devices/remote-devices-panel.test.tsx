import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";

const ZERO_SERVER_STATE = {
  ID: 1,
  ServerURL: "",
  DeviceID: 0,
  DeviceFingerprint: "",
  ServerUserID: 0,
  KeychainAccount: "",
  Updatetime: 0,
};

const LOGGED_IN_SERVER_STATE = {
  ID: 1,
  ServerURL: "https://hub.example.com",
  DeviceID: 7,
  DeviceFingerprint: "sha256:abc",
  ServerUserID: 42,
  KeychainAccount: "agentre.server.refresh_token",
  Updatetime: 1_700_000_000_000,
};

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RemoteDeviceList: vi.fn().mockResolvedValue([]),
  RemoteDeviceAdd: vi.fn(),
  RemoteDeviceRemove: vi.fn(),
  RemoteDeviceUpdateTLS: vi.fn(),
  RemoteDeviceRefresh: vi.fn(),
  RemoteDeviceRename: vi.fn(),
  // 默认未登录:账号来源 unknown。R15 合并用例在测试里单独覆盖成已登录。
  ServerListDevices: vi.fn().mockRejectedValue(new Error("not logged in")),
  ServerGetState: vi.fn(),
  ServerCheckURL: vi.fn(),
  ServerStartLogin: vi.fn(),
  ServerPollLoginToken: vi.fn(),
  ServerCancelLogin: vi.fn(),
  ServerLogout: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
  BrowserOpenURL: vi.fn(),
}));

import {
  RemoteDeviceList,
  ServerListDevices,
  ServerGetState,
  ServerCheckURL,
  ServerStartLogin,
  ServerPollLoginToken,
  ServerLogout,
} from "../../../../wailsjs/go/app/App";
import { RemoteDevicesPanel } from "./remote-devices-panel";
import type { DeviceView } from "./use-remote-devices";

const mockList = RemoteDeviceList as unknown as ReturnType<typeof vi.fn>;
const mockServerList = ServerListDevices as unknown as ReturnType<typeof vi.fn>;
const mockGetState = ServerGetState as unknown as ReturnType<typeof vi.fn>;
const mockCheckURL = ServerCheckURL as unknown as ReturnType<typeof vi.fn>;
const mockStartLogin = ServerStartLogin as unknown as ReturnType<typeof vi.fn>;
const mockPollLoginToken = ServerPollLoginToken as unknown as ReturnType<
  typeof vi.fn
>;
const mockLogout = ServerLogout as unknown as ReturnType<typeof vi.fn>;

describe("RemoteDevicesPanel", () => {
  beforeEach(() => {
    mockList.mockReset();
    mockServerList.mockReset();
    mockServerList.mockRejectedValue(new Error("not logged in"));
    mockGetState.mockReset();
    mockGetState.mockResolvedValue(ZERO_SERVER_STATE);
    mockCheckURL.mockReset();
    mockCheckURL.mockResolvedValue("0.3.0");
    mockStartLogin.mockReset();
    mockPollLoginToken.mockReset();
    mockLogout.mockReset();
    mockLogout.mockResolvedValue(undefined);
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
    // 这一轮把 remoteDevices.panel.lanAll 从两份 locale 里一并删掉了,所以标签
    // 若被重新加回 JSX,渲染出来的是**原始 key**而不是译文。译文断言只在「key 也
    // 被加回来」时才可能命中,单靠它挡不住另一半;两条都要。
    // (原先还有一条 /LAN 直连 · 全部/ —— setup.ts 在每个用例前强制 en,中文文案
    // 永远不会被渲染,那条断言恒真、挡不住任何东西,已删。)
    expect(
      screen.queryByText("remoteDevices.panel.lanAll"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/LAN direct · All/i)).not.toBeInTheDocument();
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

  // 规格「界面与交互 › 登录」:设备面板是账号登录的入口。
  describe("account login", () => {
    it("shows a Sign in entry point when not connected to an account", async () => {
      mockList.mockResolvedValueOnce([]);
      render(<RemoteDevicesPanel />);
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Sign in" }),
        ).toBeInTheDocument(),
      );
      expect(screen.queryByText(/Signed in to/)).not.toBeInTheDocument();
    });

    // d) the logged-in state shows the account/server identity and offers logout.
    it("shows the account identity and Sign out when connected", async () => {
      mockList.mockResolvedValueOnce([]);
      mockGetState.mockResolvedValue(LOGGED_IN_SERVER_STATE);
      render(<RemoteDevicesPanel />);
      await waitFor(() =>
        expect(
          screen.getByText("Signed in to hub.example.com"),
        ).toBeInTheDocument(),
      );
      expect(
        screen.getByRole("button", { name: "Sign out" }),
      ).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Sign in" }),
      ).not.toBeInTheDocument();
    });

    it("driving the full device flow through the dialog updates the panel to signed-in without further action", async () => {
      mockList.mockResolvedValueOnce([]);
      mockStartLogin.mockResolvedValueOnce({
        DeviceCode: "device-abc",
        UserCode: "ABCD-1234",
        VerificationURI: "https://hub.example.com/device",
        VerificationURIComplete:
          "https://hub.example.com/device?code=ABCD-1234",
        // Short interval keeps this wiring test fast and deterministic
        // without fake timers (timing precision is covered separately in
        // login-dialog.test.tsx).
        Interval: 1,
        ExpiresIn: 900,
      });
      mockPollLoginToken.mockResolvedValueOnce(true);
      // Second GetState call (after onLoggedIn refresh) reports signed-in.
      mockGetState.mockResolvedValueOnce(ZERO_SERVER_STATE);
      mockGetState.mockResolvedValueOnce(LOGGED_IN_SERVER_STATE);

      render(<RemoteDevicesPanel />);
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Sign in" }),
        ).toBeInTheDocument(),
      );

      fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));

      await waitFor(() =>
        expect(screen.getByText("ABCD-1234")).toBeInTheDocument(),
      );

      await waitFor(
        () =>
          expect(
            screen.getByText("Signed in to hub.example.com"),
          ).toBeInTheDocument(),
        { timeout: 8_000 },
      );
      expect(mockPollLoginToken).toHaveBeenCalledWith("device-abc");
    }, 10_000);

    it("signing out returns to the Sign in entry point", async () => {
      mockList.mockResolvedValueOnce([]);
      mockGetState.mockResolvedValueOnce(LOGGED_IN_SERVER_STATE);
      mockGetState.mockResolvedValueOnce(ZERO_SERVER_STATE);
      render(<RemoteDevicesPanel />);
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Sign out" }),
        ).toBeInTheDocument(),
      );

      fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

      expect(mockLogout).toHaveBeenCalled();
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Sign in" }),
        ).toBeInTheDocument(),
      );
    });
  });
});
