import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  NewSessionExecTargetLine,
  SessionOfflineBanner,
} from "../session-exec-target";

// 测试环境默认英文 locale（既有约定，见 org/__tests__/exec-target-list.test.tsx），
// 文案断言一律用 en/common.json 里的值；设备名/Agent 名是动态业务数据，测试里用
// 中文只是模拟真实用户输入，不受 i18n 约束。

type AvailabilityItem = {
  agentBackendId: number;
  available: boolean;
  reason?: string;
  hint?: string;
};

type BackendItem = {
  id: number;
  type?: string;
  name?: string;
  deviceId?: string;
  deviceName?: string;
  online?: boolean;
};

function stubWails(availability: AvailabilityItem[], backends: BackendItem[]) {
  const listAvailability = vi
    .fn()
    .mockResolvedValue(
      availability.map((it) => ({ reason: "", hint: "", ...it })),
    );
  const listBackends = vi.fn().mockResolvedValue({ items: backends });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = {
    app: {
      App: {
        ListAgentExecTargetAvailability: listAvailability,
        ListAgentBackends: listBackends,
      },
    },
  };
  return { listAvailability, listBackends };
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

function renderLine(
  overrides: Partial<{
    overrideBackendId: number | null;
    onOverride: (id: number | null) => void;
  }> = {},
) {
  const onOverride = overrides.onOverride ?? vi.fn();
  render(
    <MemoryRouter>
      <NewSessionExecTargetLine
        agentId={7}
        agentName="开发"
        projectId={0}
        overrideBackendId={overrides.overrideBackendId ?? null}
        onOverride={onOverride}
      />
    </MemoryRouter>,
  );
  return { onOverride };
}

describe("NewSessionExecTargetLine", () => {
  it("single candidate: renders nothing", async () => {
    stubWails([{ agentBackendId: 51, available: true }], [{ id: 51 }]);
    const { container } = render(
      <MemoryRouter>
        <NewSessionExecTargetLine
          agentId={7}
          agentName="开发"
          projectId={0}
          overrideBackendId={null}
          onOverride={vi.fn()}
        />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(container.textContent).toBe("");
    });
  });

  it("first candidate available: shows the plain 'will run on X · Change' line without highlight", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "", type: "claudecode", name: "claude-fable-5" },
        {
          id: 52,
          deviceId: "3",
          deviceName: "构建机",
          online: true,
          type: "claudecode",
          name: "claude-opus-5",
        },
      ],
    );
    renderLine();
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(line.className).not.toContain("bg-status-waiting-bg");
    expect(within(line).getByText("Local")).toBeInTheDocument();
    expect(screen.getByText("Change")).toBeInTheDocument();
  });

  it("first candidate unavailable, auto-picked second: highlights the dropped state with reason", async () => {
    stubWails(
      [
        {
          agentBackendId: 51,
          available: false,
          reason: "backend-requires-provider",
        },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "", type: "claudecode", name: "claude-fable-5" },
        {
          id: 52,
          deviceId: "3",
          deviceName: "构建机",
          online: true,
          type: "claudecode",
          name: "claude-opus-5",
        },
      ],
    );
    renderLine();
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(line.className).toContain("bg-status-waiting-bg");
    expect(screen.getByText(/Local is unavailable/)).toBeInTheDocument();
    expect(within(line).getByText("构建机")).toBeInTheDocument();
    expect(screen.getByText("LLM provider required")).toBeInTheDocument();
  });

  it("all candidates unavailable: lists every reason instead of the plain line", async () => {
    stubWails(
      [
        {
          agentBackendId: 51,
          available: false,
          reason: "backend-requires-provider",
        },
        { agentBackendId: 52, available: false, reason: "exec-target-offline" },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机" },
      ],
    );
    renderLine();
    const panel = await screen.findByTestId(
      "new-session-exec-target-all-unavailable",
    );
    expect(panel).toHaveTextContent(
      '"开发" has no available execution machine right now',
    );
    expect(panel).toHaveTextContent("LLM provider required");
    expect(panel).toHaveTextContent("Offline");
    expect(
      screen.queryByTestId("new-session-exec-target-line"),
    ).not.toBeInTheDocument();
  });

  it("reselect popover: picking an available candidate calls onOverride, unavailable ones are disabled", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true },
        {
          agentBackendId: 52,
          available: false,
          reason: "exec-target-offline",
        },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机" },
      ],
    );
    const { onOverride } = renderLine();
    await screen.findByTestId("new-session-exec-target-line");

    await userEvent.click(screen.getByText("Change"));
    const disabledRow = await screen.findByRole("button", {
      name: /构建机/,
    });
    expect(disabledRow).toBeDisabled();

    const pickableRow = screen.getByRole("button", { name: /Local/ });
    await userEvent.click(pickableRow);
    expect(onOverride).toHaveBeenCalledWith(51);
  });

  it("manual override to a non-first available candidate: shown plainly, not flagged as dropped", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机" },
      ],
    );
    renderLine({ overrideBackendId: 52 });
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(line.className).not.toContain("bg-status-waiting-bg");
    expect(within(line).getByText("构建机")).toBeInTheDocument();
  });
});

describe("SessionOfflineBanner", () => {
  it("renders device name + hint and invokes the callback on click", async () => {
    const onCreateNewSession = vi.fn();
    render(
      <SessionOfflineBanner
        deviceName="构建机 · mac-mini-01"
        onCreateNewSession={onCreateNewSession}
      />,
    );
    expect(
      screen.getByText("构建机 · mac-mini-01 is currently offline"),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Start a new session" }),
    );
    expect(onCreateNewSession).toHaveBeenCalledTimes(1);
  });
});
