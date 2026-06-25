import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { QueuedMessagesBar, type QueuedItem } from "../queued-messages-bar";

function renderBar(queued: QueuedItem[]) {
  return render(
    <QueuedMessagesBar
      queued={queued}
      onCancel={vi.fn()}
      onClearAll={vi.fn()}
    />,
  );
}

describe("QueuedMessagesBar", () => {
  it("renders nothing when there are no queued messages", () => {
    const { container } = renderBar([]);
    expect(container.firstChild).toBeNull();
  });

  it("renders each queued message text", () => {
    renderBar([
      { id: "a", text: "第一条排队消息", cancellable: true },
      { id: "b", text: "second queued message", cancellable: false },
    ]);
    expect(screen.getByText("第一条排队消息")).toBeInTheDocument();
    expect(screen.getByText("second queued message")).toBeInTheDocument();
  });

  // 真实诉求:排队中的消息是用户刚写的原文,得能选中复制(比如复制到别处重发)。
  // body 全局 user-select:none,只有挂 data-selectable-text='true' 的子树才放开。
  it("marks queued message text as selectable for copying", () => {
    renderBar([{ id: "a", text: "可被选中复制的排队文字", cancellable: true }]);
    const textEl = screen.getByText("可被选中复制的排队文字");
    expect(textEl.closest("[data-selectable-text='true']")).not.toBeNull();
  });
});
