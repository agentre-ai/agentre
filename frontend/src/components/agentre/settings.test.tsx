import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/hooks/use-chat-agents", () => ({
  useChatAgents: () => ({ agents: [] }),
}));

vi.mock("./sync", () => ({
  SyncPanel: () => null,
  useSyncStatus: () => ({ loading: false, status: null }),
}));

// Agent 后端页的 H1 现在由面板通过 renderHeader 交回宿主渲染（页级操作要落在标题
// 行），所以这个替身必须调一次 renderHeader，页头才还在。传 null 表示「没有按钮可
// 交」——替身不复刻真面板的 actions，别把它读成真实契约。
vi.mock("./agent-backends", () => ({
  AgentBackendsPanel: ({
    renderHeader,
  }: {
    renderHeader?: (actions: React.ReactNode) => React.ReactNode;
  }) => <>{renderHeader?.(null)}</>,
}));

vi.mock("./remote-devices/remote-devices-panel", () => ({
  RemoteDevicesPanel: ({
    onOpenAgentBackends,
  }: {
    onOpenAgentBackends: () => void;
  }) => (
    <button type="button" onClick={onOpenAgentBackends}>
      Configure Agent Backends
    </button>
  ),
}));

import { SettingsPage } from "./settings";

describe("SettingsPage remote-device onboarding navigation", () => {
  it("opens Agent Backends from the Remote Devices completion action", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter
        initialEntries={[
          { pathname: "/settings", state: { settingsPage: "remote-devices" } },
        ]}
      >
        <SettingsPage
          effectiveTheme="light"
          onThemePreferenceChange={() => {}}
          themePreference="system"
        />
      </MemoryRouter>,
    );

    await user.click(
      screen.getByRole("button", { name: "Configure Agent Backends" }),
    );

    // 真正证明「跳过去了」的是左侧导航的选中态：它由 SettingsNav 自己渲染，
    // 完全不经过被替身接管的面板。
    expect(screen.getByTestId("settings-nav-agent-backend")).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      screen.getByRole("heading", { name: "Agent Backends" }),
    ).toBeInTheDocument();
  });
});
