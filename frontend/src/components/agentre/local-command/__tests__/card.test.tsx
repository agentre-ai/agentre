import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { useLocalCommandsStore } from "../../../../stores/local-commands-store";
import { LocalCommandCard } from "../card";

const close = vi.fn();
vi.mock("../../../../../wailsjs/go/app/App", () => ({ TerminalClose: (...a: unknown[]) => close(...a) }));
// Output is rendered by a read-only xterm; stub it so this test stays focused on
// card chrome (status / buttons / dismiss). Terminal rendering is covered by
// output-terminal.test.tsx.
vi.mock("../output-terminal", () => ({ OutputTerminal: () => null }));

describe("LocalCommandCard", () => {
  beforeEach(() => {
    close.mockReset();
    useLocalCommandsStore.setState({ entries: {} });
  });

  it("running shows stop + open-in-terminal; stop calls TerminalClose", async () => {
    useLocalCommandsStore.getState().start({ id: "t1", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("t1", "=== RUN x\n");
    const onOpen = vi.fn();
    render(<LocalCommandCard entryId="t1" onOpenInTerminal={onOpen} />);
    expect(screen.getByText("go test")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));
    expect(close).toHaveBeenCalledWith("t1");
    await userEvent.click(screen.getByRole("button", { name: /在终端中打开|Open in terminal/ }));
    expect(onOpen).toHaveBeenCalledWith("t1");
  });

  it("after exit shows exit code and no run-time action buttons", () => {
    useLocalCommandsStore.getState().start({ id: "t2", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().finish("t2", "failed", 1);
    render(<LocalCommandCard entryId="t2" onOpenInTerminal={vi.fn()} />);
    expect(screen.getByText(/退出码 1|Exit 1/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /停止|Stop/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /在终端中打开|Open in terminal/ })).toBeNull();
  });

  it("running shows no dismiss button (must stop first)", () => {
    useLocalCommandsStore.getState().start({ id: "t3", sessionId: 1, command: "sleep 1", createdAt: 1 });
    render(<LocalCommandCard entryId="t3" onOpenInTerminal={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /移除|Dismiss/ })).toBeNull();
  });

  it("after finish a dismiss button removes the card from the store", async () => {
    useLocalCommandsStore.getState().start({ id: "t4", sessionId: 1, command: "echo hi", createdAt: 1 });
    useLocalCommandsStore.getState().finish("t4", "done", 0);
    render(<LocalCommandCard entryId="t4" onOpenInTerminal={vi.fn()} />);
    expect(screen.getByText("echo hi")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /移除|Dismiss/ }));
    expect(useLocalCommandsStore.getState().get("t4")).toBeUndefined();
    expect(screen.queryByText("echo hi")).toBeNull();
  });
});
