import { render, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { TerminalPanel } from "../terminal-panel";

// --- sonner mock (must be hoisted) ---
const toastMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    warning: vi.fn(),
  },
}));
vi.mock("sonner", () => toastMocks);

// --- chat-terminal-store mock ---
const toggleMock = vi.fn();
vi.mock("@/stores/chat-terminal-store", () => ({
  useChatTerminalStore: vi.fn((selector?: unknown) => {
    const state = { toggle: toggleMock, isOpen: () => false };
    if (typeof selector === "function") {
      return (selector as (s: unknown) => unknown)(state);
    }
    return state;
  }),
}));

// --- xterm mocks ---
const writeMock = vi.fn();
const onDataMock = vi.fn();
const openMock = vi.fn();
const disposeMock = vi.fn();
const getSelectionMock = vi.fn(() => "");
vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation(function () {
    return {
      open: openMock,
      write: writeMock,
      onData: (cb: (s: string) => void) => {
        onDataMock.mockImplementation(cb);
        return { dispose: () => {} };
      },
      loadAddon: vi.fn(),
      dispose: disposeMock,
      cols: 80,
      rows: 24,
      getSelection: getSelectionMock,
      attachCustomKeyEventHandler: vi.fn(),
    };
  }),
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(function () {
    return {
      fit: vi.fn(),
      proposeDimensions: () => ({ cols: 80, rows: 24 }),
    };
  }),
}));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: vi.fn() }));

// --- use-terminal mock (captures args for onExit testing) ---
let capturedArgs: {
  sessionID: number;
  onData?: (d: string) => void;
  onExit?: (info: { code: number; reason: string; msg?: string }) => void;
} | null = null;
const writeProxy = vi.fn();
vi.mock("../use-terminal", () => ({
  useTerminal: vi.fn().mockImplementation((args) => {
    capturedArgs = args;
    return { state: "open", write: writeProxy, resize: vi.fn() };
  }),
}));
import { useTerminal } from "../use-terminal";

beforeEach(() => {
  capturedArgs = null;
  toggleMock.mockClear();
  toastMocks.toast.error.mockClear();
  toastMocks.toast.warning.mockClear();
  writeProxy.mockClear();
  getSelectionMock.mockReturnValue("");
});

describe("TerminalPanel", () => {
  it("mounts xterm, opens hook with sessionID, writes incoming data", () => {
    render(<TerminalPanel sessionID={42} />);
    expect(useTerminal).toHaveBeenCalled();
    const args = (
      useTerminal as unknown as {
        mock: {
          calls: Array<
            Array<{ sessionID: number; onData: (s: string) => void }>
          >;
        };
      }
    ).mock.calls[0][0];
    expect(args.sessionID).toBe(42);
    act(() => args.onData("hello"));
    expect(writeMock).toHaveBeenCalledWith("hello");
  });

  it("proxies xterm onData to hook write()", () => {
    render(<TerminalPanel sessionID={42} />);
    act(() => onDataMock("typed-key"));
    expect(writeProxy).toHaveBeenCalledWith("typed-key");
  });

  it("disposes xterm on unmount", () => {
    const { unmount } = render(<TerminalPanel sessionID={42} />);
    unmount();
    expect(disposeMock).toHaveBeenCalled();
  });

  it("onExit with reason=error → toast.error and toggles off", () => {
    render(<TerminalPanel sessionID={42} />);
    act(() =>
      capturedArgs?.onExit?.({ code: -1, reason: "error", msg: "no such cwd" }),
    );
    expect(toastMocks.toast.error).toHaveBeenCalledWith(
      expect.stringContaining("no such cwd"),
    );
    expect(toggleMock).toHaveBeenCalledWith(42);
  });

  it("onExit with reason=natural code=0 → silent toggle, no toast", () => {
    render(<TerminalPanel sessionID={42} />);
    act(() => capturedArgs?.onExit?.({ code: 0, reason: "natural" }));
    expect(toastMocks.toast.warning).not.toHaveBeenCalled();
    expect(toastMocks.toast.error).not.toHaveBeenCalled();
    expect(toggleMock).toHaveBeenCalledWith(42);
  });

  it("onExit with reason=natural code=2 → warning toast and toggles off", () => {
    render(<TerminalPanel sessionID={42} />);
    act(() => capturedArgs?.onExit?.({ code: 2, reason: "natural" }));
    expect(toastMocks.toast.warning).toHaveBeenCalledWith(
      expect.stringContaining("code 2"),
    );
    expect(toggleMock).toHaveBeenCalledWith(42);
  });

  it("onExit with reason=connection_lost → shows red banner, does NOT toggle automatically", () => {
    const { getByRole } = render(<TerminalPanel sessionID={42} />);
    act(() => capturedArgs?.onExit?.({ code: 0, reason: "connection_lost" }));
    expect(toastMocks.toast.error).toHaveBeenCalled();
    expect(toggleMock).not.toHaveBeenCalled();
    // Banner should be rendered with role=alert
    expect(getByRole("alert")).toHaveTextContent("连接已断开");
  });
});
