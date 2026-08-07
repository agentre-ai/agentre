import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  ServerGetState: vi.fn(),
  ServerLogout: vi.fn(),
  ServerCheckURL: vi.fn(),
  ServerStartLogin: vi.fn(),
  ServerPollLoginToken: vi.fn(),
  ServerCancelLogin: vi.fn(),
}));

import { ServerGetState, ServerLogout } from "../../../../wailsjs/go/app/App";
import { useServerLogin, isLoggedIn } from "./use-server-login";

const mockGetState = ServerGetState as unknown as ReturnType<typeof vi.fn>;
const mockLogout = ServerLogout as unknown as ReturnType<typeof vi.fn>;

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
