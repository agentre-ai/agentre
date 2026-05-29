import { render, act } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { TerminalPanel } from "../terminal-panel";

const writeMock = vi.fn();
const onDataMock = vi.fn();
const openMock = vi.fn();
const disposeMock = vi.fn();
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

const writeProxy = vi.fn();
vi.mock("../use-terminal", () => ({
  useTerminal: vi.fn().mockImplementation((_args: unknown) => ({
    state: "open",
    write: writeProxy,
    resize: vi.fn(),
  })),
}));
import { useTerminal } from "../use-terminal";

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
});
