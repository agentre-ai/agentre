import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

// task 14 (R12):「同步」这个设置导航项只在 SyncStatus().Enabled 为真时出现,
// 不是灰掉、也不是点进去提示先登录——是整项不存在。这份文件只盯这一条守卫 +
// 它带出的最小可见内容,详细的状态卡 / 列表行为在 sync/__tests__/sync-panel
// 里覆盖。
const appMocks = vi.hoisted(() => ({
  SyncStatus: vi.fn(),
  SyncListLostChanges: vi.fn().mockResolvedValue([]),
  SyncRestoreLostChange: vi.fn(),
  SyncRecreateFromLostChange: vi.fn(),
  SyncDiscardLostChange: vi.fn(),
  SyncNow: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { SettingsPage } from "../settings";
import { useChatAgentsStore } from "@/stores/chat-agents-store";

function renderSettings() {
  return render(
    <MemoryRouter initialEntries={["/settings"]}>
      <SettingsPage
        effectiveTheme="light"
        onThemePreferenceChange={() => {}}
        themePreference="system"
      />
    </MemoryRouter>,
  );
}

function settingsNav() {
  return screen.getByRole("complementary", { name: "Settings" });
}

function SyncDeepLinkNavigator() {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      onClick={() => navigate("/settings", { state: { settingsPage: "sync" } })}
    >
      Open sync settings
    </button>
  );
}

describe("Settings sync nav guard (task 14, R12)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.SyncListLostChanges.mockResolvedValue([]);
    useChatAgentsStore.getState().__reset();
    vi.spyOn(useChatAgentsStore.getState(), "reload").mockResolvedValue();
  });

  it("does not render the Sync nav item when signed out", async () => {
    appMocks.SyncStatus.mockResolvedValue({ Enabled: false });
    renderSettings();

    // 给 useSyncStatus 的首次 reload 一个机会跑完,证明「不存在」不是加载中的假象。
    await waitFor(() => expect(appMocks.SyncStatus).toHaveBeenCalled());
    expect(
      within(settingsNav()).queryByRole("button", { name: "Sync" }),
    ).not.toBeInTheDocument();
  });

  it("renders the Sync nav item once signed in, and opens the sync panel", async () => {
    const user = userEvent.setup();
    appMocks.SyncStatus.mockResolvedValue({
      Enabled: true,
      AccountID: 1,
      Cursor: 1,
      LastSuccessAt: Date.now(),
      PendingCount: 0,
      DeferredCount: 0,
      LostChangeCount: 0,
      LastError: "",
    });
    renderSettings();

    const syncButton = await within(settingsNav()).findByRole("button", {
      name: "Sync",
    });
    await user.click(syncButton);

    expect(await screen.findByText("Sync status")).toBeInTheDocument();
    expect(screen.getByText("Changes that didn't sync")).toBeInTheDocument();
  });

  it("keeps a deep-link to the sync page while the signed-in status is still loading, instead of bouncing to Appearance", async () => {
    const user = userEvent.setup();
    let resolveStatus!: (value: unknown) => void;
    appMocks.SyncStatus.mockReturnValue(
      new Promise((resolve) => {
        resolveStatus = resolve;
      }),
    );
    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <SyncDeepLinkNavigator />
        <SettingsPage
          effectiveTheme="light"
          onThemePreferenceChange={() => {}}
          themePreference="system"
        />
      </MemoryRouter>,
    );

    await user.click(
      screen.getByRole("button", { name: "Open sync settings" }),
    );

    // 加载中(SyncStatus 还没 resolve):不应该被守卫误判成「未登录」而弹回外观页。
    expect(
      screen.queryByRole("heading", { name: "Appearance" }),
    ).not.toBeInTheDocument();

    resolveStatus({
      Enabled: true,
      AccountID: 1,
      Cursor: 1,
      LastSuccessAt: Date.now(),
      PendingCount: 0,
      DeferredCount: 0,
      LostChangeCount: 0,
      LastError: "",
    });

    expect(await screen.findByText("Sync status")).toBeInTheDocument();
  });
});
