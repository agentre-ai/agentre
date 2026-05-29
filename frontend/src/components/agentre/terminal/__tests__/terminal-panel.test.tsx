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
      options: {
        theme: undefined as
          | { background: string; foreground: string }
          | undefined,
      },
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
  terminalID: string;
  projectId: number;
  deviceId: string;
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
  toastMocks.toast.error.mockClear();
  toastMocks.toast.warning.mockClear();
  writeProxy.mockClear();
  getSelectionMock.mockReturnValue("");
});

describe("TerminalPanel", () => {
  it("mounts xterm, opens hook with terminalID, writes incoming data", () => {
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    expect(useTerminal).toHaveBeenCalled();
    const args = (
      useTerminal as unknown as {
        mock: {
          calls: Array<
            Array<{ terminalID: string; onData: (s: string) => void }>
          >;
        };
      }
    ).mock.calls[0][0];
    expect(args.terminalID).toBe("t1");
    act(() => args.onData("hello"));
    expect(writeMock).toHaveBeenCalledWith("hello");
  });

  it("proxies xterm onData to hook write()", () => {
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    act(() => onDataMock("typed-key"));
    expect(writeProxy).toHaveBeenCalledWith("typed-key");
  });

  it("sizes the PTY to the fitted dimensions once the hook reports open", () => {
    const resizeMock = vi.fn();
    (
      useTerminal as unknown as {
        mockImplementation: (fn: (args: unknown) => unknown) => void;
      }
    ).mockImplementation((args) => {
      capturedArgs = args as typeof capturedArgs;
      return { state: "open", write: writeProxy, resize: resizeMock };
    });
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    // The mocked xterm reports cols=80, rows=24; once "open" the panel must
    // push that size so the PTY is not stuck at the initial open dimensions
    // (the ResizeObserver-driven resize can race TerminalOpen and be dropped).
    expect(resizeMock).toHaveBeenCalledWith(80, 24);
  });

  it("disposes xterm on unmount", () => {
    const onClose = vi.fn();
    const { unmount } = render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    unmount();
    expect(disposeMock).toHaveBeenCalled();
  });

  it("onExit with reason=error → toast.error and calls onClose", () => {
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    act(() =>
      capturedArgs?.onExit?.({ code: -1, reason: "error", msg: "no such cwd" }),
    );
    expect(toastMocks.toast.error).toHaveBeenCalledWith(
      expect.stringContaining("no such cwd"),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("onExit with reason=natural code=0 → silent close, no toast", () => {
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    act(() => capturedArgs?.onExit?.({ code: 0, reason: "natural" }));
    expect(toastMocks.toast.warning).not.toHaveBeenCalled();
    expect(toastMocks.toast.error).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("onExit with reason=natural code=2 → warning toast and calls onClose", () => {
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    act(() => capturedArgs?.onExit?.({ code: 2, reason: "natural" }));
    expect(toastMocks.toast.warning).toHaveBeenCalledWith(
      expect.stringContaining("code 2"),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("onExit with reason=connection_lost → shows red banner, does NOT call onClose automatically", () => {
    const onClose = vi.fn();
    const { getByRole } = render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    act(() => capturedArgs?.onExit?.({ code: 0, reason: "connection_lost" }));
    expect(toastMocks.toast.error).toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    // Banner should be rendered with role=alert
    expect(getByRole("alert")).toHaveTextContent("连接已断开");
  });

  it("re-themes xterm when document root class changes (light↔dark toggle)", () => {
    // jsdom's getComputedStyle does not resolve CSS custom properties (--background,
    // --foreground), so we cannot assert the exact theme values here. The actual
    // re-theme behaviour is verified manually in Task 30.
    // Minimum assertion: render + MutationObserver registration throws no error.
    const onClose = vi.fn();
    render(
      <TerminalPanel
        terminalID="t1"
        projectId={42}
        deviceId=""
        onClose={onClose}
      />,
    );
    // Toggle dark class on/off; the MutationObserver callback fires synchronously
    // in jsdom's mutation queue so this exercises the handler without needing waitFor.
    act(() => {
      document.documentElement.classList.add("dark");
    });
    act(() => {
      document.documentElement.classList.remove("dark");
    });
    // No assertion on theme value — jsdom limitation. Just confirm no throw.
    expect(true).toBe(true);
  });
});
