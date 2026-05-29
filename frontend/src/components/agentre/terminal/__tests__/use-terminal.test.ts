import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useTerminal } from "../use-terminal";

vi.mock("@/../wailsjs/go/app/App", () => ({
  TerminalOpen: vi.fn().mockResolvedValue(undefined),
  TerminalWrite: vi.fn().mockResolvedValue(undefined),
  TerminalResize: vi.fn().mockResolvedValue(undefined),
  TerminalClose: vi.fn().mockResolvedValue(undefined),
}));

const onHandlers: Record<string, (payload: any) => void> = {};
vi.mock("@/../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn((name: string, cb: (payload: any) => void) => {
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
  it("calls TerminalOpen(sessionID, cols, rows) on mount and subscribes to events", async () => {
    const { result } = renderHook(() =>
      useTerminal({ sessionID: 7, cols: 80, rows: 24 })
    );
    await act(async () => {
      await Promise.resolve();
    });
    expect(App.TerminalOpen).toHaveBeenCalledWith(7, 80, 24);
    expect(onHandlers["terminal:7:data"]).toBeTypeOf("function");
    expect(onHandlers["terminal:7:exit"]).toBeTypeOf("function");
    expect(result.current.state).toBe("open");
  });

  it("exposes incoming data via onData callback", async () => {
    const onData = vi.fn();
    renderHook(() =>
      useTerminal({ sessionID: 7, cols: 80, rows: 24, onData })
    );
    await act(async () => {
      await Promise.resolve();
    });
    act(() => onHandlers["terminal:7:data"]({ data: "hello" }));
    expect(onData).toHaveBeenCalledWith("hello");
  });

  it("calls TerminalClose and EventsOff on unmount", async () => {
    const { unmount } = renderHook(() =>
      useTerminal({ sessionID: 7, cols: 80, rows: 24 })
    );
    await act(async () => {
      await Promise.resolve();
    });
    unmount();
    expect(App.TerminalClose).toHaveBeenCalledWith(7);
    expect(onHandlers["terminal:7:data"]).toBeUndefined();
  });

  it("write() proxies to App.TerminalWrite", async () => {
    const { result } = renderHook(() =>
      useTerminal({ sessionID: 7, cols: 80, rows: 24 })
    );
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await result.current.write("ls\n");
    });
    expect(App.TerminalWrite).toHaveBeenCalledWith(7, "ls\n");
  });
});
