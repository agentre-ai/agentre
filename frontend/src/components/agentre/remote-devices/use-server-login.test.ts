import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  ServerGetState: vi.fn(),
  ServerLogout: vi.fn(),
  ServerCheckURL: vi.fn(),
  ServerStartLogin: vi.fn(),
  ServerPollLoginToken: vi.fn(),
  ServerCancelLogin: vi.fn(),
  ServerOffline: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

import {
  ServerGetState,
  ServerLogout,
  ServerOffline,
} from "../../../../wailsjs/go/app/App";
import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import { useServerLogin, isLoggedIn } from "./use-server-login";

const mockGetState = ServerGetState as unknown as ReturnType<typeof vi.fn>;
const mockLogout = ServerLogout as unknown as ReturnType<typeof vi.fn>;
const mockOffline = ServerOffline as unknown as ReturnType<typeof vi.fn>;
const mockEventsOn = EventsOn as unknown as ReturnType<typeof vi.fn>;

/** 拿到 useServerLogin 注册在 "server.state" 上的那个回调。 */
function serverStateHandler(): (payload: unknown) => void {
  const call = mockEventsOn.mock.calls.find((c) => c[0] === "server.state");
  if (!call)
    throw new Error("useServerLogin did not subscribe to server.state");
  return call[1] as (payload: unknown) => void;
}

const loggedOutState = {
  ID: 1,
  ServerURL: "",
  DeviceID: 0,
  DeviceFingerprint: "",
  ServerUserID: 0,
  KeychainAccount: "",
  Updatetime: 0,
};

const loggedInState = {
  ID: 1,
  ServerURL: "https://hub.example.com",
  DeviceID: 7,
  DeviceFingerprint: "sha256:abc",
  ServerUserID: 42,
  KeychainAccount: "agentre.server.refresh_token",
  Updatetime: 1_700_000_000_000,
};

beforeEach(() => {
  mockGetState.mockReset();
  mockLogout.mockReset();
  mockOffline.mockReset();
  mockEventsOn.mockReset();
  mockEventsOn.mockImplementation(() => vi.fn());
  mockOffline.mockResolvedValue(false);
});

describe("isLoggedIn (mirrors server_state_entity.ServerState.IsLoggedIn)", () => {
  it("requires user, device, and keychain account all populated", () => {
    expect(isLoggedIn(loggedInState)).toBe(true);
  });
  it("treats a partial row as logged out", () => {
    expect(isLoggedIn({ ...loggedInState, DeviceID: 0 })).toBe(false);
    expect(isLoggedIn({ ...loggedInState, ServerUserID: 0 })).toBe(false);
    expect(isLoggedIn({ ...loggedInState, KeychainAccount: "" })).toBe(false);
  });
  it("treats null as logged out", () => {
    expect(isLoggedIn(null)).toBe(false);
  });
});

describe("useServerLogin", () => {
  it("loads state on mount and derives loggedIn", async () => {
    mockGetState.mockResolvedValueOnce(loggedInState);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.loggedIn).toBe(true);
    expect(result.current.state?.ServerURL).toBe("https://hub.example.com");
  });

  it("treats a fresh-install zero row as logged out", async () => {
    mockGetState.mockResolvedValueOnce(loggedOutState);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.loggedIn).toBe(false);
  });

  it("does not throw when GetState rejects, and stays logged out", async () => {
    mockGetState.mockRejectedValueOnce(new Error("db unavailable"));
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.loggedIn).toBe(false);
  });

  it("logout() calls ServerLogout then refreshes state to logged-out", async () => {
    mockGetState.mockResolvedValueOnce(loggedInState);
    mockLogout.mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.loggedIn).toBe(true);

    mockGetState.mockResolvedValueOnce(loggedOutState);
    await act(async () => {
      await result.current.logout();
    });
    expect(mockLogout).toHaveBeenCalled();
    expect(result.current.loggedIn).toBe(false);
  });

  // ServerLogout 会真的报错(logout.go 的 ClearLoginFields 落库失败)。此时登录
  // 状态一点没变,界面必须照实说「还登录着」—— 而不是卡在退登之前那一帧。过去
  // refresh() 排在 await 之后,一旦 ServerLogout 抛出就整个跳过,界面从此与真相
  // 脱节(而调用方那边只剩一个 unhandled rejection)。
  it("logout() still re-reads state when ServerLogout fails, and surfaces the failure", async () => {
    mockGetState.mockResolvedValueOnce(loggedInState);
    mockLogout.mockRejectedValueOnce(new Error("database is locked"));
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.loggedIn).toBe(true);

    mockGetState.mockClear();
    mockGetState.mockResolvedValueOnce(loggedInState);
    await act(async () => {
      await expect(result.current.logout()).rejects.toThrow(
        "database is locked",
      );
    });
    expect(mockGetState).toHaveBeenCalled();
    expect(result.current.loggedIn).toBe(true);
  });
});

// 服务端够不着不再等于登出(bootstrap/server.go 过去一律清登录)。此时登录态原样
// 留着,界面要说的是「服务端离线」,而不是把用户推回登录页。
describe("useServerLogin — 服务端离线", () => {
  it("takes the initial offline flag from the backend, since the event may predate mount", async () => {
    mockGetState.mockResolvedValue(loggedInState);
    mockOffline.mockResolvedValue(true);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.serverOffline).toBe(true));
    expect(result.current.loggedIn).toBe(true);
  });

  it("flips on server_offline and back on server_online without touching the login", async () => {
    mockGetState.mockResolvedValue(loggedInState);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.serverOffline).toBe(false);

    act(() => {
      serverStateHandler()({ kind: "server_offline", reason: "unreachable" });
    });
    expect(result.current.serverOffline).toBe(true);
    expect(result.current.loggedIn).toBe(true);

    act(() => {
      serverStateHandler()({ kind: "server_online" });
    });
    expect(result.current.serverOffline).toBe(false);
  });

  it("re-reads the persisted state when the backend reports logged_out", async () => {
    mockGetState.mockResolvedValueOnce(loggedInState);
    const { result } = renderHook(() => useServerLogin());
    await waitFor(() => expect(result.current.loggedIn).toBe(true));

    mockGetState.mockResolvedValueOnce(loggedOutState);
    await act(async () => {
      serverStateHandler()({ kind: "logged_out", reason: "refresh_expired" });
    });
    await waitFor(() => expect(result.current.loggedIn).toBe(false));
  });
});
