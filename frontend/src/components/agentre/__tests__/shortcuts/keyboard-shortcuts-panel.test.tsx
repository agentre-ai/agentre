import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { KeyboardShortcutsPanel } from "../../shortcuts";
import { ShortcutsProvider } from "../../shortcuts/shortcuts-provider";

function renderPanel() {
  return render(
    <MemoryRouter>
      <ShortcutsProvider platform="darwin">
        <KeyboardShortcutsPanel />
      </ShortcutsProvider>
    </MemoryRouter>,
  );
}

describe("KeyboardShortcutsPanel", () => {
  it("shows current chat tab shortcuts instead of historical sidebar session copy", () => {
    renderPanel();

    const chatSection = screen
      .getByRole("heading", { name: "对话页" })
      .closest("section");

    expect(chatSection).not.toBeNull();
    const scope = within(chatSection!);

    expect(scope.getByText("切换到第 N 个 Tab")).toBeInTheDocument();
    expect(
      scope.getByText("⌘1 - ⌘9 · 按 TabStrip 排列顺序（固定 + 普通 + 预览）"),
    ).toBeInTheDocument();
    expect(scope.getByText("关闭当前 Tab")).toBeInTheDocument();
    expect(
      scope.getByText("关闭激活中的 Tab（钉住的 Tab 不可关闭）"),
    ).toBeInTheDocument();
    expect(scope.queryByText(/attention 会话/)).not.toBeInTheDocument();
    expect(scope.queryByText(/sidebar 自上而下/)).not.toBeInTheDocument();
  });
});
