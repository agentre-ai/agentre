import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/app/App", () => ({
  GetAppSetting: vi.fn(),
  UpdateAppSettings: vi.fn(),
}));

import { GetAppSetting, UpdateAppSettings } from "../../../wailsjs/go/app/App";
import {
  DEFAULT_NOTIFICATION_SETTINGS,
  useNotificationSettingsStore,
} from "../notification-settings-store";

const getMock = vi.mocked(GetAppSetting);
const updateMock = vi.mocked(UpdateAppSettings);

beforeEach(() => {
  vi.clearAllMocks();
  useNotificationSettingsStore.setState({
    settings: { ...DEFAULT_NOTIFICATION_SETTINGS },
  });
});

describe("notification-settings-store", () => {
  it("默认值: 仅系统通知开 + 仅失焦时通知,toast 关", () => {
    expect(DEFAULT_NOTIFICATION_SETTINGS).toEqual({
      enabled: true,
      onlyWhenUnfocused: true,
      system: true,
      toast: false,
    });
  });

  it("load: 某 key 缺失(reject)时回落默认值", async () => {
    getMock.mockImplementation((req: { key: string }) => {
      if (req.key === "notify.toast")
        return Promise.reject(new Error("not found"));
      return Promise.resolve({ key: req.key, value: "false" });
    });
    await useNotificationSettingsStore.getState().load();
    const s = useNotificationSettingsStore.getState().settings;
    expect(s.toast).toBe(false); // reject → 默认 false
    expect(s.enabled).toBe(false); // 读到 "false"
    expect(s.system).toBe(false); // 读到 "false"
  });

  it("save: 写一个 partial 后 UpdateAppSettings 收到对应 entry,并更新本地 state", async () => {
    updateMock.mockResolvedValue({});
    await useNotificationSettingsStore.getState().save({ toast: true });
    expect(updateMock).toHaveBeenCalledWith({
      entries: [{ key: "notify.toast", value: "true" }],
    });
    expect(useNotificationSettingsStore.getState().settings.toast).toBe(true);
  });
});
