import { render, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

import { useLocalCommandsStore } from "@/stores/local-commands-store";
import { OutputTerminal } from "../output-terminal";

// --- xterm mocks (mirror terminal-panel.test.tsx) ---
const writeMock = vi.fn();
const openMock = vi.fn();
const disposeMock = vi.fn();
const resizeMock = vi.fn();
vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation(function () {
    return {
      open: openMock,
      write: writeMock,
      loadAddon: vi.fn(),
      dispose: disposeMock,
      resize: resizeMock,
      focus: vi.fn(),
      cols: 80,
      rows: 24,
      buffer: { active: { length: 1 } },
      options: { theme: undefined as Record<string, string> | undefined },
    };
  }),
}));
import { Terminal } from "@xterm/xterm";
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(function () {
    return { fit: vi.fn(), proposeDimensions: () => ({ cols: 80, rows: 24 }) };
  }),
}));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: vi.fn() }));

// IntersectionObserver harness: lets a test fire (or withhold) intersection.
type IOCallback = (
  entries: IntersectionObserverEntry[],
  observer: IntersectionObserver,
) => void;
let ioInstances: Array<{ cb: IOCallback; target?: Element }> = [];
class IOMock {
  cb: IOCallback;
  constructor(cb: IOCallback) {
    this.cb = cb;
    ioInstances.push({ cb });
  }
  observe(el: Element) {
    const i = ioInstances.find((x) => x.cb === this.cb);
    if (i) i.target = el;
  }
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}
function fireIntersect() {
  for (const inst of ioInstances) {
    inst.cb(
      [
        {
          isIntersecting: true,
          target: inst.target as Element,
        } as unknown as IntersectionObserverEntry,
      ],
      inst as unknown as IntersectionObserver,
    );
  }
}

let originalIO: typeof IntersectionObserver | undefined;
beforeEach(() => {
  writeMock.mockClear();
  resizeMock.mockClear();
  vi.mocked(Terminal).mockClear();
  ioInstances = [];
  useLocalCommandsStore.setState({ entries: {} });
  originalIO = globalThis.IntersectionObserver;
});
afterEach(() => {
  if (originalIO) {
    (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver =
      originalIO;
  } else {
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
  }
});

describe("OutputTerminal", () => {
  it("writes raw ANSI output to xterm verbatim — no stripping, so xterm renders color instead of leaving 乱码 control bytes", () => {
    // No IntersectionObserver → eager-mount fallback (output must always show).
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
    const raw = "\x1b[31mred\x1b[0m done";
    useLocalCommandsStore
      .getState()
      .start({ id: "t1", sessionId: 1, command: "echo", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("t1", raw);

    render(<OutputTerminal terminalId="t1" />);

    expect(writeMock).toHaveBeenCalledWith(raw);
  });

  it("creates a read-only xterm (stdin disabled, no blinking cursor)", () => {
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
    useLocalCommandsStore
      .getState()
      .start({ id: "t2", sessionId: 1, command: "echo", createdAt: 1 });

    render(<OutputTerminal terminalId="t2" />);

    const opts = vi.mocked(Terminal).mock.calls[0][0]!;
    expect(opts.disableStdin).toBe(true);
    expect(opts.cursorBlink).toBe(false);
  });

  it("streams later output deltas to xterm as the command keeps running", () => {
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
    useLocalCommandsStore
      .getState()
      .start({ id: "t3", sessionId: 1, command: "go test", createdAt: 1 });
    render(<OutputTerminal terminalId="t3" />);
    writeMock.mockClear();

    act(() => useLocalCommandsStore.getState().appendOutput("t3", "=== RUN\n"));

    expect(writeMock).toHaveBeenCalledWith("=== RUN\n");
  });

  it("lazy-mounts: builds no xterm until the card scrolls into view", () => {
    (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver =
      IOMock as unknown as typeof IntersectionObserver;
    useLocalCommandsStore
      .getState()
      .start({ id: "t4", sessionId: 1, command: "echo", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("t4", "hidden output");

    render(<OutputTerminal terminalId="t4" />);
    // Not yet visible → no terminal constructed, nothing written.
    expect(Terminal).not.toHaveBeenCalled();
    expect(writeMock).not.toHaveBeenCalled();

    act(() => fireIntersect());

    expect(Terminal).toHaveBeenCalled();
    expect(writeMock).toHaveBeenCalledWith("hidden output");
  });
});
