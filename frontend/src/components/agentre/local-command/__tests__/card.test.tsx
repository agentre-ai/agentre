import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { useLocalCommandsStore } from "../../../../stores/local-commands-store";
import { isTerminalNotOpenError, LocalCommandCard } from "../card";

const close = vi.fn();
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  TerminalClose: (...a: unknown[]) => close(...a),
}));
// Output is rendered by a read-only xterm; stub it so this test stays focused on
// card chrome (status / buttons / dismiss). Terminal rendering is covered by
// output-terminal.test.tsx.
vi.mock("../output-terminal", () => ({ OutputTerminal: () => null }));

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("isTerminalNotOpenError", () => {
  it.each([
    [new Error("terminal not open"), true],
    ["terminal not open", true],
    [new Error("remote cleanup failed after terminal not open"), false],
    ["terminal not open\n", false],
    [{ message: "terminal not open" }, false],
    [new Error("terminal closed"), false],
  ])(
    "classifies only the exact Wails terminal-not-open rejection",
    (error, expected) => {
      expect(isTerminalNotOpenError(error)).toBe(expected);
    },
  );
});

describe("LocalCommandCard", () => {
  beforeEach(() => {
    close.mockReset();
    useLocalCommandsStore.setState({ entries: {} });
  });

  it("Given no exit listener and TerminalClose succeeds, When Stop is clicked, Then the transient entry settles stopped with its output preserved", async () => {
    close.mockResolvedValueOnce(undefined);
    useLocalCommandsStore
      .getState()
      .start({ id: "t1", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("t1", "=== RUN x\n");
    render(<LocalCommandCard entryId="t1" onOpenInTerminal={vi.fn()} />);

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));

    expect(close).toHaveBeenCalledWith("t1");
    await waitFor(() => {
      expect(useLocalCommandsStore.getState().get("t1")).toMatchObject({
        output: "=== RUN x\n",
        status: "stopped",
      });
    });
    expect(screen.queryByRole("button", { name: /停止|Stop/ })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /在终端中打开|Open in terminal/ }),
    ).toBeNull();
  });

  it("Given TerminalClose rejects, When Stop is clicked, Then the rejection is contained and Stop remains available for a successful retry", async () => {
    close
      .mockRejectedValueOnce(new Error("close unavailable"))
      .mockResolvedValueOnce(undefined);
    useLocalCommandsStore.getState().start({
      id: "t-retry",
      sessionId: 1,
      command: "sleep 30",
      createdAt: 1,
    });
    useLocalCommandsStore.getState().appendOutput("t-retry", "partial\n");
    render(<LocalCommandCard entryId="t-retry" onOpenInTerminal={vi.fn()} />);

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(useLocalCommandsStore.getState().get("t-retry")).toMatchObject({
      output: "partial\nError: close unavailable",
      status: "running",
    });

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));
    await waitFor(() => {
      expect(useLocalCommandsStore.getState().get("t-retry")?.status).toBe(
        "stopped",
      );
    });
    expect(close).toHaveBeenCalledTimes(2);
  });

  it("Given TerminalClose rejects with an unrelated message containing terminal not open, When Stop is clicked, Then the diagnostic is appended and the card stays retryable", async () => {
    close.mockRejectedValueOnce(
      new Error("remote cleanup failed after terminal not open"),
    );
    useLocalCommandsStore.getState().start({
      id: "t-unrelated",
      sessionId: 1,
      command: "sleep 30",
      createdAt: 1,
    });
    render(
      <LocalCommandCard entryId="t-unrelated" onOpenInTerminal={vi.fn()} />,
    );

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));

    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(useLocalCommandsStore.getState().get("t-unrelated")).toMatchObject({
      output: "Error: remote cleanup failed after terminal not open",
      status: "running",
    });
    expect(
      screen.getByRole("button", { name: /停止|Stop/ }),
    ).toBeInTheDocument();
  });

  it("Given a normal exit wins while TerminalClose is pending, When close later succeeds, Then the exit status, code, and output are preserved", async () => {
    const closing = deferred<void>();
    close.mockReturnValueOnce(closing.promise);
    useLocalCommandsStore.getState().start({
      id: "t-exit",
      sessionId: 1,
      command: "printf ok",
      createdAt: 1,
    });
    useLocalCommandsStore.getState().appendOutput("t-exit", "ok\n");
    render(<LocalCommandCard entryId="t-exit" onOpenInTerminal={vi.fn()} />);

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));
    act(() => {
      useLocalCommandsStore.getState().finish("t-exit", "done", 0);
    });
    await act(async () => {
      closing.resolve();
      await closing.promise;
    });

    expect(useLocalCommandsStore.getState().get("t-exit")).toMatchObject({
      exitCode: 0,
      output: "ok\n",
      status: "done",
    });
  });

  it("after exit shows exit code and no run-time action buttons", () => {
    useLocalCommandsStore
      .getState()
      .start({ id: "t2", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().finish("t2", "failed", 1);
    render(<LocalCommandCard entryId="t2" onOpenInTerminal={vi.fn()} />);
    expect(screen.getByText(/退出码 1|Exit 1/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /停止|Stop/ })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /在终端中打开|Open in terminal/ }),
    ).toBeNull();
  });

  it("running shows no dismiss button (must stop first)", () => {
    useLocalCommandsStore
      .getState()
      .start({ id: "t3", sessionId: 1, command: "sleep 1", createdAt: 1 });
    render(<LocalCommandCard entryId="t3" onOpenInTerminal={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /移除|Dismiss/ })).toBeNull();
  });

  it("after finish a dismiss button removes the card from the store", async () => {
    useLocalCommandsStore
      .getState()
      .start({ id: "t4", sessionId: 1, command: "echo hi", createdAt: 1 });
    useLocalCommandsStore.getState().finish("t4", "done", 0);
    render(<LocalCommandCard entryId="t4" onOpenInTerminal={vi.fn()} />);
    expect(screen.getByText("echo hi")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /移除|Dismiss/ }));
    expect(useLocalCommandsStore.getState().get("t4")).toBeUndefined();
    expect(screen.queryByText("echo hi")).toBeNull();
  });

  it("finished command defaults to a collapsed one-line summary (no chip / no 'not sent to AI')", () => {
    useLocalCommandsStore.setState({
      entries: {
        s1: {
          id: "s1",
          sessionId: 1,
          command: "git status",
          createdAt: 1000,
          finishedAt: 2200, // 1.2s
          status: "done",
          exitCode: 0,
          output: "clean\n",
        },
      },
    });
    render(<LocalCommandCard entryId="s1" onOpenInTerminal={vi.fn()} />);

    expect(screen.getByText("git status")).toBeInTheDocument();
    expect(screen.getByText("1.2s")).toBeInTheDocument();
    expect(screen.getByText(/退出码 0|Exit 0/)).toBeInTheDocument();
    // 折叠行不含 chip 与 "不发送给 AI"
    expect(screen.queryByText(/本地命令|Local command/)).toBeNull();
    expect(screen.queryByText(/不发送给 AI|Not sent to AI/)).toBeNull();
  });

  it("clicking a collapsed summary expands it to reveal the header chip", async () => {
    useLocalCommandsStore.setState({
      entries: {
        s2: {
          id: "s2",
          sessionId: 1,
          command: "ls",
          createdAt: 1000,
          finishedAt: 1500,
          status: "done",
          exitCode: 0,
          output: "a\n",
        },
      },
    });
    render(<LocalCommandCard entryId="s2" onOpenInTerminal={vi.fn()} />);
    // collapsed: no chip yet
    expect(screen.queryByText(/本地命令|Local command/)).toBeNull();

    await userEvent.click(
      screen.getByRole("button", { name: /展开输出|Expand output/ }),
    );

    // expanded header now shows chip + "not sent to AI"
    expect(screen.getByText(/本地命令|Local command/)).toBeInTheDocument();
    expect(screen.getByText(/不发送给 AI|Not sent to AI/)).toBeInTheDocument();
  });
});
