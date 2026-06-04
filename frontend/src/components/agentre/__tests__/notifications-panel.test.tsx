import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const updateMock = vi.fn((_req: unknown) => Promise.resolve({}));
vi.mock("../../../../wailsjs/go/app/App", () => ({
  GetAppSetting: vi.fn(() => Promise.reject(new Error("nf"))),
  UpdateAppSettings: (req: unknown) => updateMock(req),
}));

import i18n from "../../../i18n";
import { NotificationsPanel } from "../notifications-panel";
import {
  DEFAULT_NOTIFICATION_SETTINGS,
  useNotificationSettingsStore,
} from "../../../stores/notification-settings-store";

beforeEach(async () => {
  vi.clearAllMocks();
  await i18n.changeLanguage("zh-CN");
  useNotificationSettingsStore.setState({
    settings: { ...DEFAULT_NOTIFICATION_SETTINGS },
  });
});
afterEach(() => vi.restoreAllMocks());

describe("NotificationsPanel", () => {
  it("渲染各开关", () => {
    render(<NotificationsPanel />);
    // getByText/getByRole 找不到会抛错,本身即断言。
    expect(screen.getByText("启用通知")).toBeTruthy();
    expect(screen.getByText("系统通知")).toBeTruthy();
  });

  it("切换系统通知开关写库", async () => {
    render(<NotificationsPanel />);
    const sw = screen.getByRole("switch", { name: "系统通知" });
    await userEvent.click(sw); // 默认开 → 关
    expect(updateMock).toHaveBeenCalledWith({
      entries: [{ key: "notify.system", value: "false" }],
    });
  });
});
