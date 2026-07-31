import { fireEvent, render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { FileEditCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

describe("FileEditCard", () => {
  it("renders nothing without canonical", () => {
    const block = {
      type: "tool_use",
      toolName: "Edit",
    } as unknown as ChatBlockData;
    const { container } = render(<FileEditCard toolBlock={block} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders single-file diff header with relative path", () => {
    const block = {
      type: "tool_use",
      toolName: "Edit",
      canonical: {
        kind: "file.edit",
        fileEdit: {
          files: [
            {
              path: "/root/app/x.ts",
              kind: "modified",
              hunks: [],
              plus: 3,
              minus: 1,
            },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<FileEditCard toolBlock={block} cwd="/root/app" />);
    expect(screen.getByText("./x.ts")).toBeDefined();
    expect(screen.getByText("+3")).toBeDefined();
    expect(screen.getByText("−1")).toBeDefined();
  });

  it("collapses multi-file as N files", () => {
    const block = {
      type: "tool_use",
      toolName: "MultiEdit",
      canonical: {
        kind: "file.edit",
        fileEdit: {
          files: [
            { path: "/a", kind: "modified", hunks: [], plus: 1, minus: 0 },
            { path: "/b", kind: "created", hunks: [], plus: 2, minus: 0 },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<FileEditCard toolBlock={block} />);
    expect(screen.getByText("2 files")).toBeDefined();
  });

  // Given 一行超出卡片宽度的 diff,When 展开,Then diff 区可横向滚动
  // 且行盒撑到内容宽度(否则 +/- 背景条在滚动后断在卡片边缘)。
  it("scrolls long diff lines horizontally instead of clipping them", () => {
    const longText = `const x = "${"y".repeat(400)}";`;
    const block = {
      type: "tool_use",
      toolName: "Edit",
      canonical: {
        kind: "file.edit",
        fileEdit: {
          files: [
            {
              path: "/root/app/x.ts",
              kind: "modified",
              plus: 1,
              minus: 0,
              hunks: [
                {
                  oldStart: 1,
                  oldLines: 0,
                  newStart: 1,
                  newLines: 1,
                  lines: [{ op: "+", new: 1, text: longText }],
                },
              ],
            },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<FileEditCard toolBlock={block} cwd="/root/app" />);
    fireEvent.click(screen.getByRole("button"));

    const scroller = screen.getByTestId("file-edit-diff-scroll");
    expect(scroller.className).toContain("overflow-x-auto");

    const row = screen.getByText(longText).parentElement as HTMLElement;
    expect(scroller).toContainElement(row);
    expect(row.className).toContain("w-max");
    expect(row.className).toContain("min-w-full");
  });
});
