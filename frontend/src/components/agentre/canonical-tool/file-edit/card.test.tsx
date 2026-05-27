import { render, screen } from "@testing-library/react";
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
});
