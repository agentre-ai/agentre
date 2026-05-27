import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("sonner", () => sonnerMocks);

import { MarkdownText } from "./markdown-text";

const originalClipboard = navigator.clipboard;

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  return writeText;
}

afterEach(() => {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: originalClipboard,
  });
});

describe("MarkdownText", () => {
  it("copies fenced code block text from AI markdown replies", async () => {
    const writeText = mockClipboard();

    render(<MarkdownText text={"结果如下：\n\n```\npnpm test\n```\n"} />);

    fireEvent.click(screen.getByRole("button", { name: "复制" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("pnpm test\n");
    });
    expect(sonnerMocks.toast.success).toHaveBeenCalledWith(
      "已复制代码",
      expect.objectContaining({
        duration: 5000,
        position: "bottom-right",
      }),
    );
    expect(screen.getByRole("button", { name: "已复制" })).toBeInTheDocument();
  });
});
