import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useTerminal } from "../use-terminal";

vi.mock("@/../wailsjs/go/app/App", () => ({
  TerminalOpen: vi.fn().mockResolvedValue(undefined),
  TerminalWrite: vi.fn().mockResolvedValue(undefined),
  TerminalResize: vi.fn().mockResolvedValue(undefined),
  TerminalClose: vi.fn().mockResolvedValue(undefined),
}));

type EventHandler = (payload: unknown) => void;
const onHandlers: Record<string, EventHandler> = {};
vi.mock("@/../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn((name: string, cb: EventHandler) => {
    onHandlers[name] = cb;
    return () => {
      delete onHandlers[name];
    };
  }),
  EventsOff: vi.fn((name: string) => {
    delete onHandlers[name];
  }),
}));

import * as App from "@/../wailsjs/go/app/App";

beforeEach(() => {
  vi.clearAllMocks();
  for (const k of Object.keys(onHandlers)) delete onHandlers[k];
});

describe("useTerminal", () => {
  it("calls TerminalOpen(terminalID, projectId, deviceId, cols, rows) on mount and subscribes to events", async () => {
    const { result } = renderHook(() =>
      useTerminal({
        terminalID: "t1",
        projectId: 7,
        deviceId: "",
        cols: 80,
        rows: 24,
      }),
    );
    await act(async () => {
      await Promise.resolve();
    });
    expect(App.TerminalOpen).toHaveBeenCalledWith("t1", 7, "", 80, 24);
    expect(onHandlers["terminal:t1:data"]).toBeTypeOf("function");
    expect(onHandlers["terminal:t1:exit"]).toBeTypeOf("function");
    expect(result.current.state).toBe("open");
  });

  it("exposes incoming data via onData callback", async () => {
    const onData = vi.fn();
    renderHook(() =>
      useTerminal({
        terminalID: "t1",
        projectId: 7,
        deviceId: "",
        cols: 80,
        rows: 24,
        onData,
      }),
    );
    await act(async () => {
      await Promise.resolve();
    });
    act(() => onHandlers["terminal:t1:data"]({ data: "hello" }));
    expect(onData).toHaveBeenCalledWith("hello");
  });

  it("calls TerminalClose and EventsOff on unmount", async () => {
    const { unmount } = renderHook(() =>
      useTerminal({
        terminalID: "t1",
        projectId: 7,
        deviceId: "",
        cols: 80,
        rows: 24,
      }),
    );
    await act(async () => {
      await Promise.resolve();
    });
    unmount();
    expect(App.TerminalClose).toHaveBeenCalledWith("t1");
    expect(onHandlers["terminal:t1:data"]).toBeUndefined();
    expect(onHandlers["terminal:t1:exit"]).toBeUndefined();
  });

  it("write() proxies to App.TerminalWrite", async () => {
    const { result } = renderHook(() =>
      useTerminal({
        terminalID: "t1",
        projectId: 7,
        deviceId: "",
        cols: 80,
        rows: 24,
      }),
    );
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await result.current.write("ls\n");
    });
    expect(App.TerminalWrite).toHaveBeenCalledWith("t1", "ls\n");
  });

  it("resize() proxies to App.TerminalResize", async () => {
    const { result } = renderHook(() =>
      useTerminal({
        terminalID: "t1",
        projectId: 7,
        deviceId: "",
        cols: 80,
        rows: 24,
      }),
    );
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await result.current.resize(100, 40);
    });
    expect(App.TerminalResize).toHaveBeenCalledWith("t1", 100, 40);
  });
});
