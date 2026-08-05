import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useLocalCommandsStore } from "../../../../stores/local-commands-store";
import { LocalCommandCard } from "../card";

// Output is rendered by a read-only xterm; stub it so this test stays focused on
// card chrome (status / buttons / dismiss). Terminal rendering is covered by
// output-terminal.test.tsx.
vi.mock("../output-terminal", () => ({ OutputTerminal: () => null }));

describe("LocalCommandCard", () => {
  beforeEach(() => {
    useLocalCommandsStore.setState({ entries: {} });
  });

  it("Given a running command, When Stop is clicked, Then the card delegates the terminal id without owning settlement", async () => {
    const onStop = vi.fn();
    useLocalCommandsStore
      .getState()
      .start({ id: "t1", sessionId: 1, command: "go test", createdAt: 1 });
    render(
      <LocalCommandCard
        entryId="t1"
        onOpenInTerminal={vi.fn()}
        onStop={onStop}
      />,
    );

    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));

    expect(onStop).toHaveBeenCalledTimes(1);
    expect(onStop).toHaveBeenCalledWith("t1");
    expect(useLocalCommandsStore.getState().get("t1")?.status).toBe("running");
  });

  it("Given a read-only caller without onStop, When a command is running, Then the card omits the Stop control", () => {
    useLocalCommandsStore.getState().start({
      id: "t-readonly",
      sessionId: 1,
      command: "sleep 30",
      createdAt: 1,
    });
    render(
      <LocalCommandCard entryId="t-readonly" onOpenInTerminal={vi.fn()} />,
    );

    expect(screen.queryByRole("button", { name: /停止|Stop/ })).toBeNull();
    expect(
      screen.getByRole("button", { name: /在终端中打开|Open in terminal/ }),
    ).toBeInTheDocument();
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
