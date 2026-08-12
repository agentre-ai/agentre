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

vi.mock("./agent-backends", () => ({
  AgentBackendsPanel: () => null,
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

    expect(
      screen.getByRole("heading", { name: "Agent Backends" }),
    ).toBeInTheDocument();
  });
});
