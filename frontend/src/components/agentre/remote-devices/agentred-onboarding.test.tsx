import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentredOnboarding } from "./agentred-onboarding";

// 步骤条三格各自的稳定标识:第 3 格的标题与配对表单的提交按钮同名,所以用它独有的副标题认它。
const INSTALL_CELL = /Install agentred/;
const SERVICE_CELL = /Start the remote service/;
const PAIR_CELL = /One-time code/;

function stepCell(name: RegExp): HTMLElement {
  return screen.getByRole("button", { name });
}

describe("AgentredOnboarding", () => {
  it("moves through install, background service, and shared pairing form actions", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<AgentredOnboarding onSubmit={onSubmit} />);

    expect(
      screen.getByRole("link", { name: "Manual installation" }),
    ).toHaveAttribute(
      "href",
      "https://github.com/agentre-ai/agentre/releases/latest",
    );
    expect(screen.getByText("agentred --version")).toHaveAttribute(
      "data-selectable-text",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "Installed, next" }));

    expect(
      screen
        .getAllByText("agentred service status")
        .every((element) => element.dataset.selectableText === "true"),
    ).toBe(true);
    expect(screen.getByText("agentred service restart")).toHaveAttribute(
      "data-selectable-text",
      "true",
    );
    expect(screen.getByText(/ws:\/\/…:7456\/rpc/)).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Service is running" }),
    );
    await user.type(screen.getByLabelText("Address"), "ws://host:7456/rpc");
    await user.type(screen.getByLabelText("Pairing Code"), "ABC2DE");
    await user.click(screen.getByRole("button", { name: "Pair and verify" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "ws://host:7456/rpc",
        pairingCode: "ABC2DE",
      }),
    );
  });

  // 收起只有在宿主真的能收下它时才存在:零设备时没有可回退的地方。
  it("offers the collapse control only when the host provides somewhere to go back to", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const { rerender } = render(<AgentredOnboarding onSubmit={onSubmit} />);

    expect(
      screen.queryByRole("button", { name: "Collapse guide" }),
    ).not.toBeInTheDocument();

    rerender(<AgentredOnboarding onSubmit={onSubmit} onDismiss={onDismiss} />);
    await user.click(screen.getByRole("button", { name: "Collapse guide" }));

    expect(onDismiss).toHaveBeenCalledOnce();
  });

  // 步骤条即导航:任意步骤之间直达,含跳过中间步的前跳。
  it("jumps straight to any step from any other step", async () => {
    const user = userEvent.setup();
    render(
      <AgentredOnboarding onSubmit={vi.fn().mockResolvedValue(undefined)} />,
    );

    await user.click(stepCell(PAIR_CELL));

    expect(screen.getByText("Step 3 of 3")).toBeInTheDocument();
    expect(screen.getByLabelText("Address")).toBeInTheDocument();

    await user.click(stepCell(SERVICE_CELL));

    expect(screen.getByText("Step 2 of 3")).toBeInTheDocument();
    expect(
      screen.getByText("agentred service install --start"),
    ).toBeInTheDocument();

    await user.click(stepCell(INSTALL_CELL));

    expect(screen.getByText("Step 1 of 3")).toBeInTheDocument();
    expect(screen.getByText("agentred --version")).toBeInTheDocument();
  });

  it("reaches and activates the step cells with the keyboard", async () => {
    const user = userEvent.setup();
    render(
      <AgentredOnboarding onSubmit={vi.fn().mockResolvedValue(undefined)} />,
    );

    await user.tab();
    expect(stepCell(INSTALL_CELL)).toHaveFocus();

    await user.tab();
    expect(stepCell(SERVICE_CELL)).toHaveFocus();

    await user.tab();
    expect(stepCell(PAIR_CELL)).toHaveFocus();

    await user.keyboard("{Enter}");

    expect(screen.getByText("Step 3 of 3")).toBeInTheDocument();
    expect(screen.getByLabelText("Address")).toBeInTheDocument();
  });

  // 完成标记记录的是「用户点过这一步的下一步」,不是「走到过这一步之后」。
  it("marks a step complete only after its own next button was pressed", async () => {
    const user = userEvent.setup();
    render(
      <AgentredOnboarding onSubmit={vi.fn().mockResolvedValue(undefined)} />,
    );

    await user.click(stepCell(PAIR_CELL));

    const skippedInstall = stepCell(INSTALL_CELL);
    expect(
      within(skippedInstall).getByText("About 1 minute"),
    ).toBeInTheDocument();
    expect(
      within(skippedInstall).queryByText("Complete"),
    ).not.toBeInTheDocument();
    expect(within(skippedInstall).getByText("01")).toBeInTheDocument();

    const skippedService = stepCell(SERVICE_CELL);
    expect(within(skippedService).getByText("Stay online")).toBeInTheDocument();
    expect(
      within(skippedService).queryByText("Running"),
    ).not.toBeInTheDocument();
    expect(within(skippedService).getByText("02")).toBeInTheDocument();

    await user.click(stepCell(INSTALL_CELL));
    await user.click(screen.getByRole("button", { name: "Installed, next" }));

    const finishedInstall = stepCell(INSTALL_CELL);
    expect(within(finishedInstall).getByText("Complete")).toBeInTheDocument();
    expect(within(finishedInstall).queryByText("01")).not.toBeInTheDocument();
    // 第 2 步现在是当前步,但用户还没点过它的「服务已运行」。
    expect(
      within(stepCell(SERVICE_CELL)).getByText("Stay online"),
    ).toBeInTheDocument();
    expect(stepCell(SERVICE_CELL)).toHaveAttribute("aria-current", "step");
  });

  // 步骤条与收起都能卸载第 3 步的表单,而失败原因只存在于那张表单里 —— 提交在途
  // 时放行任何一条离开的路,配对失败就会被无声吞掉:设备没加上,用户却什么也看不到。
  it("never swallows a pairing failure when the user tries to leave mid-submit", async () => {
    const user = userEvent.setup();
    let rejectPairing: (reason: Error) => void = () => {};
    const onSubmit = vi.fn().mockImplementation(
      () =>
        new Promise<void>((_resolve, reject) => {
          rejectPairing = reject;
        }),
    );
    render(<AgentredOnboarding onSubmit={onSubmit} onDismiss={vi.fn()} />);

    await user.click(stepCell(PAIR_CELL));
    await user.type(screen.getByLabelText("Address"), "ws://host:7456/rpc");
    await user.type(screen.getByLabelText("Pairing Code"), "ABC2DE");
    await user.click(screen.getByRole("button", { name: "Pair and verify" }));

    await user.click(stepCell(INSTALL_CELL));
    await user.click(screen.getByRole("button", { name: "Collapse guide" }));

    rejectPairing(new Error("Pairing code expired"));

    expect(await screen.findByText("Pairing code expired")).toBeInTheDocument();
    // 还停在第 3 步,填过的地址与配对码都还在,用户可以直接改了重试。
    expect(screen.getByLabelText("Address")).toHaveValue("ws://host:7456/rpc");
    expect(screen.getByLabelText("Pairing Code")).toHaveValue("ABC2DE");
  });
});
