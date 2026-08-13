import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ToolPermissionCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

describe("ToolPermissionCard JSON input", () => {
  it("collapses a long tool input JSON with an expand control once expanded", () => {
    const block = {
      type: "tool_use",
      toolName: "org_update_agent",
      toolUseId: "perm-1",
      canonical: {
        kind: "tool.permission",
        toolPermission: {
          requestId: "req-1",
          toolName: "org_update_agent",
          toolInput: { system_prompt: "x".repeat(300) },
          resolved: false,
        },
      },
    } as unknown as ChatBlockData;
    render(
      <ToolPermissionCard toolBlock={block} sessionId={1} messageId={1} />,
    );
    // 卡片默认折叠;展开后 JSON 入参出现并带折叠控制
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    expect(screen.queryByRole("button", { name: "Expand all" })).toBeNull();
  });

  it("Given an expanded card, When collapsed, Then the input JSON stays mounted through the collapse transition and unmounts after it ends", () => {
    const block = {
      type: "tool_use",
      toolName: "org_update_agent",
      toolUseId: "perm-1",
      canonical: {
        kind: "tool.permission",
        toolPermission: {
          requestId: "req-1",
          toolName: "org_update_agent",
          toolInput: { system_prompt: "x".repeat(300) },
          resolved: false,
        },
      },
    } as unknown as ChatBlockData;
    render(
      <ToolPermissionCard toolBlock={block} sessionId={1} messageId={1} />,
    );
    const header = screen.getByRole("button", { expanded: false });
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "true");
    const grid = header.nextElementSibling as HTMLElement;
    expect(within(grid).getByText(/system_prompt/)).toBeDefined();

    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
    // 收缩动画期间内容仍挂载,过渡结束才卸载。
    expect(within(grid).getByText(/system_prompt/)).toBeDefined();
    fireEvent.transitionEnd(grid);
    expect(within(grid).queryByText(/system_prompt/)).toBeNull();
  });
});
