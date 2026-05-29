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
      .getByRole("heading", { name: "Chat Page" })
      .closest("section");

    expect(chatSection).not.toBeNull();
    const scope = within(chatSection!);

    expect(scope.getByText("Switch to Tab N")).toBeInTheDocument();
    expect(
      scope.getByText(
        "⌘1 - ⌘9 · Uses TabStrip order (pinned + normal + preview)",
      ),
    ).toBeInTheDocument();
    expect(scope.getByText("Close Current Tab")).toBeInTheDocument();
    expect(
      scope.getByText("Close the active tab (pinned tabs cannot be closed)"),
    ).toBeInTheDocument();
    expect(scope.queryByText(/attention 会话/)).not.toBeInTheDocument();
    expect(scope.queryByText(/sidebar 自上而下/)).not.toBeInTheDocument();
  });
});
