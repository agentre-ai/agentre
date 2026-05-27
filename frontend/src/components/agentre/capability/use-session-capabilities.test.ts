import { renderHook, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

import { useSessionCapabilities } from "./use-session-capabilities";

const getSessionCapabilities = vi.fn();

vi.mock("@/../wailsjs/go/app/App", () => ({
  GetSessionCapabilities: (req: { sessionId: number }) =>
    getSessionCapabilities(req),
}));

describe("useSessionCapabilities", () => {
  beforeEach(() => {
    getSessionCapabilities.mockReset();
  });

  it("loads caps + permission mode meta", async () => {
    getSessionCapabilities.mockResolvedValue({
      capabilities: ["abort", "steer", "set_permission_mode"],
      permissionModeMeta: {
        allowedModes: ["default", "plan"],
        defaultMode: "default",
        switchableDuringTurn: false,
        order: ["default", "plan"],
      },
    });
    const { result } = renderHook(() => useSessionCapabilities(1));
    await waitFor(() => expect(result.current.caps).not.toBeNull());
    expect(result.current.caps?.has("abort")).toBe(true);
    expect(result.current.caps?.has("steer")).toBe(true);
    expect(result.current.caps?.has("fork_session")).toBe(false);
    expect(result.current.caps?.permissionModeMeta.allowedModes).toEqual([
      "default",
      "plan",
    ]);
    expect(result.current.caps?.permissionModeMeta.defaultMode).toBe("default");
    expect(result.current.caps?.permissionModeMeta.switchableDuringTurn).toBe(
      false,
    );
  });

  it("returns null caps when sessionId is 0/undefined", () => {
    const { result: r0 } = renderHook(() => useSessionCapabilities(0));
    expect(r0.current.caps).toBeNull();
    const { result: rU } = renderHook(() => useSessionCapabilities(undefined));
    expect(rU.current.caps).toBeNull();
    expect(getSessionCapabilities).not.toHaveBeenCalled();
  });

  it("falls back to empty meta when backend omits fields", async () => {
    getSessionCapabilities.mockResolvedValue({
      capabilities: [],
      // permissionModeMeta 缺省
    });
    const { result } = renderHook(() => useSessionCapabilities(42));
    await waitFor(() => expect(result.current.caps).not.toBeNull());
    expect(result.current.caps?.permissionModeMeta.allowedModes).toEqual([]);
    expect(result.current.caps?.permissionModeMeta.defaultMode).toBe("");
    expect(result.current.caps?.permissionModeMeta.switchableDuringTurn).toBe(
      false,
    );
    expect(result.current.caps?.permissionModeMeta.order).toEqual([]);
  });

  it("sets caps to null on backend error", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    getSessionCapabilities.mockRejectedValue(new Error("session not found"));
    const { result } = renderHook(() => useSessionCapabilities(99));
    await waitFor(() => expect(errSpy).toHaveBeenCalled());
    expect(result.current.caps).toBeNull();
    errSpy.mockRestore();
  });
});
