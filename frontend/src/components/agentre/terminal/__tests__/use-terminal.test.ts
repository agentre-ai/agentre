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

  it("base64-decodes incoming data to raw bytes for onData", async () => {
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
    // base64 of bytes [0x68,0x69,0x20,0xe2,0x94,0x80] = "hi ─" — the box-drawing
    // char '─' is 3 bytes (E2 94 80) and must reach xterm intact as raw bytes.
    const b64 = btoa(String.fromCharCode(0x68, 0x69, 0x20, 0xe2, 0x94, 0x80));
    act(() => onHandlers["terminal:t1:data"]({ data: b64 }));
    expect(onData).toHaveBeenCalledTimes(1);
    const arg = onData.mock.calls[0][0] as Uint8Array;
    expect(arg).toBeInstanceOf(Uint8Array);
    expect(Array.from(arg)).toEqual([0x68, 0x69, 0x20, 0xe2, 0x94, 0x80]);
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
