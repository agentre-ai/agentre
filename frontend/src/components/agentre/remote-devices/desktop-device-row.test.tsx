import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  PeerListSessions: vi.fn(),
}));

import { PeerListSessions } from "../../../../wailsjs/go/app/App";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { DesktopDeviceRow, type DesktopDevice } from "./desktop-device-row";

const mockList = PeerListSessions as unknown as ReturnType<typeof vi.fn>;

const runningDesktop = (over: Partial<DesktopDevice> = {}): DesktopDevice => ({
  ID: 2,
  Name: "MacBook Pro",
  Kind: "desktop",
  Fingerprint: "sha256:desktop-b",
  Online: true,
  LastSeenAt: 1_700_000_000_000,
  IsThisDevice: false,
  ...over,
});

beforeEach(() => {
  mockList.mockReset();
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
});

describe("DesktopDeviceRow", () => {
  it("given a running desktop, expands into its session list and opens a Peer Tab on click", async () => {
    mockList.mockResolvedValue({
      sessions: [
        {
          sessionId: 7,
          title: "Ship the release",
          lifecycleState: "running",
          waitingForInput: true,
        },
        {
          sessionId: 8,
          title: "",
          lifecycleState: "idle",
          waitingForInput: false,
        },
      ],
      supportsSessionMetadata: true,
    });
    render(
      <MemoryRouter>
        <DesktopDeviceRow device={runningDesktop()} now={1_700_000_100_000} />
      </MemoryRouter>,
    );

    await userEvent.click(screen.getByRole("button", { name: /expand/i }));
    await waitFor(() => {
      expect(screen.getByText("Ship the release")).toBeTruthy();
    });
    expect(mockList).toHaveBeenCalledWith("sha256:desktop-b");

    await userEvent.click(screen.getByText("Ship the release"));
    const tabs = useChatTabsStore.getState().tabs;
    expect(tabs).toHaveLength(1);
    expect(tabs[0].meta).toMatchObject({
      kind: "peer",
      fingerprint: "sha256:desktop-b",
      sessionId: 7,
    });
  });

  it("given a desktop whose Agentre is not running, shows the App-not-running wording and is not expandable", async () => {
    render(
      <MemoryRouter>
        <DesktopDeviceRow
          device={runningDesktop({ Online: false })}
          now={1_700_000_100_000}
        />
      </MemoryRouter>,
    );
    expect(screen.getByTestId("desktop-not-running").textContent).toContain(
      "not running",
    );
    // 未运行不可展开：无展开按钮
    expect(screen.queryByRole("button", { name: /expand/i })).toBeNull();
    // 不会发起 PeerListSessions
    expect(mockList).not.toHaveBeenCalled();
  });

  it("single-desktop account: the self desktop row renders as this device (R22 one-device regression)", () => {
    render(
      <MemoryRouter>
        <DesktopDeviceRow
          device={runningDesktop({ IsThisDevice: true, Online: true })}
          now={1_700_000_100_000}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("This device")).toBeTruthy();
    // 本机也是正在运行的桌面端，可展开列出会话（但绝不把本机当 peer 派活——那是
    // 聊天侧 effectiveTarget.kind==="local" 保证的）。
    expect(screen.getByRole("button", { name: /expand/i })).toBeTruthy();
  });
});
