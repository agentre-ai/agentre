import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

import { GroupCreateCard, parseGroupCreateResult } from "./card";
import { CanonicalToolRouter } from "../registry";
import type { ChatBlockData } from "@/stores/chat-streams-store";

const mockOpenGroup = vi.fn();

vi.mock("@/stores/chat-tabs-store", () => ({
  useChatTabsStore: (
    selector: (s: { openGroup: typeof mockOpenGroup }) => unknown,
  ) => selector({ openGroup: mockOpenGroup }),
}));

const groupCreateUse = (
  overrides: Partial<ChatBlockData> = {},
): ChatBlockData =>
  ({
    type: "tool_use",
    toolName: "mcp__group__group_create",
    toolInput: { title: "新功能开发组", members: [] },
    ...overrides,
  }) as unknown as ChatBlockData;

const result = (overrides: Partial<ChatBlockData> = {}): ChatBlockData =>
  ({
    type: "tool_result",
    text: "group created: id=12 title=新功能开发组",
    ...overrides,
  }) as unknown as ChatBlockData;

beforeEach(() => {
  mockOpenGroup.mockClear();
});

describe("parseGroupCreateResult", () => {
  it("parses the success contract text", () => {
    expect(
      parseGroupCreateResult("group created: id=12 title=新功能开发组"),
    ).toEqual({ id: 12, title: "新功能开发组" });
  });

  it("returns null for non-success text (rejected / timeout / error)", () => {
    expect(parseGroupCreateResult("用户拒绝了此操作")).toBeNull();
    expect(parseGroupCreateResult(undefined)).toBeNull();
    expect(parseGroupCreateResult("")).toBeNull();
  });
});

describe("GroupCreateCard", () => {
  it("renders the created label + group title, and opens the group tab on click", () => {
    render(
      <GroupCreateCard toolBlock={groupCreateUse()} resultBlock={result()} />,
    );
    expect(screen.getByText("Group chat created")).toBeDefined();
    expect(screen.getByText("新功能开发组")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: /Open group/ }));
    expect(mockOpenGroup).toHaveBeenCalledWith(12, "新功能开发组");
  });

  it("falls back to RawToolCard while pending (no resultBlock)", () => {
    render(<GroupCreateCard toolBlock={groupCreateUse()} />);
    expect(screen.queryByText("Group chat created")).toBeNull();
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
  });

  it("falls back to RawToolCard when the result is a rejection text", () => {
    render(
      <GroupCreateCard
        toolBlock={groupCreateUse()}
        resultBlock={result({ text: "用户拒绝了此操作" })}
      />,
    );
    expect(screen.queryByText("Group chat created")).toBeNull();
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
  });
});

describe("CanonicalToolRouter group_create dispatch", () => {
  it("routes MCP-prefixed mcp__group__group_create (no canonical) to GroupCreateCard", () => {
    render(
      <CanonicalToolRouter
        toolBlock={groupCreateUse()}
        resultBlock={result()}
      />,
    );
    expect(screen.getByText("Group chat created")).toBeDefined();
  });

  it("routes bare group_create to GroupCreateCard", () => {
    render(
      <CanonicalToolRouter
        toolBlock={groupCreateUse({ toolName: "group_create" })}
        resultBlock={result()}
      />,
    );
    expect(screen.getByText("Group chat created")).toBeDefined();
  });

  it("keeps normal tools (Bash) on RawToolCard", () => {
    render(
      <CanonicalToolRouter
        toolBlock={
          {
            type: "tool_use",
            toolName: "Bash",
            toolInput: { command: "ls" },
          } as unknown as ChatBlockData
        }
        resultBlock={result({ text: "ok" })}
      />,
    );
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
    expect(screen.queryByText("Group chat created")).toBeNull();
  });
});
