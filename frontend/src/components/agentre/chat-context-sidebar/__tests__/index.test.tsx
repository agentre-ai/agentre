import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import { ChatContextSidebar } from "../index";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

const userM = (id: number, text: string): Msg =>
  ({
    id,
    role: "user",
    sessionId: 1,
    blocks: [{ type: "text", text }],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: 0,
  }) as unknown as Msg;

const assistantM = (id: number, text: string): Msg =>
  ({
    id,
    role: "assistant",
    sessionId: 1,
    blocks: [{ type: "text", text }],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: 0,
  }) as unknown as Msg;

describe("ChatContextSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
  });

  it("shows OutlineView by default", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("does not render the legacy git Context block at the top", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    expect(screen.queryByText("Context")).not.toBeInTheDocument();
    expect(screen.queryByTestId("branch-chip")).not.toBeInTheDocument();
  });

  it("exposes a resize separator on the left edge", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    const sep = screen.getByRole("separator");
    expect(sep).toHaveAttribute("aria-orientation", "vertical");
  });

  it("switches to FilesView when Files tab clicked", async () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("tab", { name: /files/i }));
    expect(screen.getByText(/没有改过任何文件|没有文件/)).toBeInTheDocument();
    expect(useChatSidebarStore.getState().activeTab).toBe("files");
  });

  it("calls onJumpToMessage when outline row clicked", async () => {
    const onJump = vi.fn();
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(99, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={onJump}
      />,
    );
    await userEvent.click(screen.getByText("hello world"));
    expect(onJump).toHaveBeenCalledWith(99);
  });

  it("highlights the outline row matching activeMessageId", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(11, "first"), userM(22, "second")]}
        activeMessageId={22}
        onJumpToMessage={() => {}}
      />,
    );
    const active = document.querySelector('[data-outline-message-id="22"]');
    const inactive = document.querySelector('[data-outline-message-id="11"]');
    expect(active).toHaveAttribute("data-active", "true");
    expect(inactive).toHaveAttribute("data-active", "false");
  });

  it("highlights the turn's user row when activeMessageId points to an assistant reply in that turn", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[
          userM(11, "first"),
          assistantM(12, "reply 1"),
          userM(22, "second"),
          assistantM(23, "reply 2"),
        ]}
        activeMessageId={12}
        onJumpToMessage={() => {}}
      />,
    );
    expect(
      document.querySelector('[data-outline-message-id="11"]'),
    ).toHaveAttribute("data-active", "true");
    expect(
      document.querySelector('[data-outline-message-id="22"]'),
    ).toHaveAttribute("data-active", "false");
  });

  it("switches the highlight to the next turn's user row once a later turn's assistant becomes active", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[
          userM(11, "first"),
          assistantM(12, "reply 1"),
          userM(22, "second"),
          assistantM(23, "reply 2"),
        ]}
        activeMessageId={23}
        onJumpToMessage={() => {}}
      />,
    );
    expect(
      document.querySelector('[data-outline-message-id="11"]'),
    ).toHaveAttribute("data-active", "false");
    expect(
      document.querySelector('[data-outline-message-id="22"]'),
    ).toHaveAttribute("data-active", "true");
  });
});
