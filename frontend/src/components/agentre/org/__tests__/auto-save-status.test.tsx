import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AutoSaveStatus } from "../auto-save-status";

const { default: i18n } = await import("@/i18n");

describe("AutoSaveStatus", () => {
  beforeEach(async () => {
    await i18n.changeLanguage("zh-CN");
  });

  it("shows saved by default and no retry button", () => {
    render(
      <AutoSaveStatus
        status="saved"
        pendingInvalid={false}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getByText("已保存")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows saving label while saving", () => {
    render(
      <AutoSaveStatus
        status="saving"
        pendingInvalid={false}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getByText("保存中...")).toBeInTheDocument();
  });

  it("shows unsaved label when a change is held invalid", () => {
    render(<AutoSaveStatus status="idle" pendingInvalid onRetry={vi.fn()} />);
    expect(screen.getByText("未保存的修改")).toBeInTheDocument();
  });

  it("shows retry button on error and calls onRetry", () => {
    const onRetry = vi.fn();
    render(
      <AutoSaveStatus
        status="error"
        pendingInvalid={false}
        onRetry={onRetry}
      />,
    );
    expect(screen.getByText("保存失败")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
