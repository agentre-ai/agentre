import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("sonner", () => sonnerMocks);

import { FileWriteCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

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
  vi.clearAllMocks();
});

describe("FileWriteCard", () => {
  it("renders nothing when no canonical", () => {
    const block = {
      type: "tool_use",
      toolName: "Write",
    } as unknown as ChatBlockData;
    const { container } = render(<FileWriteCard toolBlock={block} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders path + RUNNING when no result yet", () => {
    const block = {
      type: "tool_use",
      toolName: "Write",
      canonical: {
        kind: "file.write",
        fileWrite: {
          path: "/root/app/foo.ts",
          content: "hello\nworld",
          lines: 2,
          bytes: 11,
        },
      },
    } as unknown as ChatBlockData;
    render(<FileWriteCard toolBlock={block} cwd="/root/app" />);
    expect(screen.getByText("./foo.ts")).toBeDefined();
    expect(screen.getByText("RUNNING")).toBeDefined();
  });

  it("renders DONE when result present", () => {
    const block = {
      type: "tool_use",
      toolName: "Write",
      canonical: {
        kind: "file.write",
        fileWrite: { path: "/x.ts", content: "", lines: 0, bytes: 0 },
      },
    } as unknown as ChatBlockData;
    const result = {
      type: "tool_result",
      text: "ok",
    } as unknown as ChatBlockData;
    render(<FileWriteCard toolBlock={block} resultBlock={result} />);
    expect(screen.getByText("DONE")).toBeDefined();
  });

  // Given 一行超出卡片宽度的内容,When 展开,Then 内容区可横向滚动
  // 且行盒撑到内容宽度(卡片本身 overflow-hidden,否则长行被裁掉且拖不出来)。
  it("scrolls long content lines horizontally instead of clipping them", () => {
    const longText = `const x = "${"y".repeat(400)}";`;
    const block = {
      type: "tool_use",
      toolName: "Write",
      canonical: {
        kind: "file.write",
        fileWrite: {
          path: "/root/app/foo.ts",
          content: `first\n${longText}`,
          lines: 2,
          bytes: longText.length + 6,
        },
      },
    } as unknown as ChatBlockData;
    render(<FileWriteCard toolBlock={block} cwd="/root/app" />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));

    const scroller = screen.getByTestId("file-write-content-scroll");
    expect(scroller.className).toContain("overflow-x-auto");

    const row = screen.getByText(longText).parentElement as HTMLElement;
    expect(scroller).toContainElement(row);
    expect(row.className).toContain("w-max");
    expect(row.className).toContain("min-w-full");
  });

  it("Given a truncated write, When full content is copied, Then Sonner shows a timed success toast", async () => {
    const writeText = mockClipboard();
    const block = {
      type: "tool_use",
      toolName: "Write",
      canonical: {
        kind: "file.write",
        fileWrite: {
          path: "/x.ts",
          content: "hello\nworld",
          lines: 2,
          bytes: 11,
          truncated: true,
        },
      },
    } as unknown as ChatBlockData;

    render(<FileWriteCard toolBlock={block} />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    fireEvent.click(screen.getByRole("button", { name: "Copy Full Content" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("hello\nworld");
    });
    expect(sonnerMocks.toast.success).toHaveBeenCalledWith(
      "Full content copied",
      expect.objectContaining({
        duration: 5000,
        position: "bottom-right",
      }),
    );
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });
});
