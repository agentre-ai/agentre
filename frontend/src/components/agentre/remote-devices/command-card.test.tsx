import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { copyTextWithToast } = vi.hoisted(() => ({
  copyTextWithToast: vi.fn(),
}));

vi.mock("@/lib/clipboard-toast", () => ({
  copyTextWithToast,
}));

import en from "../../../i18n/locales/en/common.json";
import zhCN from "../../../i18n/locales/zh-CN/common.json";
import { CommandCard } from "./command-card";

describe("CommandCard", () => {
  beforeEach(() => {
    copyTextWithToast.mockReset();
    copyTextWithToast.mockResolvedValue(true);
  });

  it("renders selectable command text and copies the exact command through the shared toast helper", () => {
    const command = "agentred service status";
    render(<CommandCard command={command} label="Check status" />);

    const region = screen.getByRole("group", { name: "Check status" });
    expect(within(region).getByText(command)).toHaveAttribute(
      "data-selectable-text",
      "true",
    );

    fireEvent.click(
      within(region).getByRole("button", { name: "Copy Check status" }),
    );
    expect(copyTextWithToast).toHaveBeenCalledWith(command, {
      successTitle: "Command copied",
    });
  });

  it("keeps every static onboarding command identical in both locales", () => {
    expect(zhCN.remoteDevices.onboarding.commands).toEqual(
      en.remoteDevices.onboarding.commands,
    );
  });
});
