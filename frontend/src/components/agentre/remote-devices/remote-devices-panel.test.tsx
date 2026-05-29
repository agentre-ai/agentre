import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RemoteDeviceList: vi.fn().mockResolvedValue([]),
  RemoteDeviceAdd: vi.fn(),
  RemoteDeviceRemove: vi.fn(),
  RemoteDeviceUpdateTLS: vi.fn(),
  RemoteDeviceRefresh: vi.fn(),
  RemoteDeviceRename: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

import { RemoteDeviceList } from "../../../../wailsjs/go/app/App";
import { RemoteDevicesPanel } from "./remote-devices-panel";
import type { DeviceView } from "./use-remote-devices";

const mockList = RemoteDeviceList as unknown as ReturnType<typeof vi.fn>;

describe("RemoteDevicesPanel", () => {
  beforeEach(() => mockList.mockReset());

  it("shows empty state when no devices", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(
        screen.getByText(/No agentred devices paired/),
      ).toBeInTheDocument(),
    );
  });

  it("renders a row per device + counters", async () => {
    mockList.mockResolvedValueOnce([
      {
        id: 1,
        name: "mac",
        url: "ws://h1/rpc",
        tlsMode: "default",
        online: true,
        lastSeenAt: Date.now(),
      },
      {
        id: 2,
        name: "pi",
        url: "ws://h2/rpc",
        tlsMode: "default",
        online: false,
        lastSeenAt: 0,
      },
    ] as Partial<DeviceView>[]);
    render(<RemoteDevicesPanel />);
    await waitFor(() =>
      expect(screen.getAllByTestId("device-row")).toHaveLength(2),
    );
    expect(screen.getByText("2 paired · 1 online")).toBeInTheDocument();
  });
});
