import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RemoteDeviceList: vi.fn(),
  RemoteDeviceAdd: vi.fn(),
  RemoteDeviceRemove: vi.fn(),
  RemoteDeviceUpdateTLS: vi.fn(),
  RemoteDeviceRefresh: vi.fn(),
  RemoteDeviceRename: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

import {
  RemoteDeviceList,
  RemoteDeviceAdd,
} from "../../../../wailsjs/go/app/App";
import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import { useRemoteDevices } from "./use-remote-devices";

const mockList = RemoteDeviceList as unknown as ReturnType<typeof vi.fn>;
const mockAdd = RemoteDeviceAdd as unknown as ReturnType<typeof vi.fn>;
const mockEventsOn = EventsOn as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockList.mockReset();
  mockAdd.mockReset();
  mockEventsOn.mockReset();
  mockEventsOn.mockImplementation(() => vi.fn()); // 默认返回 unsubscribe stub
});

describe("useRemoteDevices", () => {
  it("loads devices on mount", async () => {
    mockList.mockResolvedValueOnce([{ id: 1, name: "a" }]);
    const { result } = renderHook(() => useRemoteDevices());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.devices).toEqual([{ id: 1, name: "a" }]);
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
