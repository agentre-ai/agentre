import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentredOnboarding } from "./agentred-onboarding";

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
});
