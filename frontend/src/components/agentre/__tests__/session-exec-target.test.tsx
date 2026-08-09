import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  NewSessionExecTargetLine,
  SessionOfflineBanner,
} from "../session-exec-target";

// wailsjs/runtime/ 没有全局 vite alias（只有 go/app/App 与 go/models 有），渲染
// 会订阅 remote.device.state 的组件必须 per-file mock 掉它，否则真实 runtime.js
// 会去碰不存在的 window.runtime。
const eventHandlers = new Map<string, (payload: unknown) => void>();
vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: (name: string, cb: (payload: unknown) => void) => {
    eventHandlers.set(name, cb);
    return () => eventHandlers.delete(name);
  },
}));

/** 模拟一次「某台 agentred 的在线态变了」的后端推送（remote_device_watcher_svc）。*/
async function emitDeviceStateChange() {
  await act(async () => {
    eventHandlers.get("remote.device.state")?.({
      id: 3,
      name: "构建机",
      online: false,
      lastSeenAt: 0,
      lastError: "",
    });
  });
}

// 测试环境默认英文 locale（既有约定，见 org/__tests__/exec-target-list.test.tsx），
// 文案断言一律用 en/common.json 里的值；设备名/Agent 名是动态业务数据，测试里用
// 中文只是模拟真实用户输入，不受 i18n 约束。

type AvailabilityItem = {
  agentBackendId: number;
  available: boolean;
  reason?: string;
  hint?: string;
  projectPath?: string;
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
  const listAvailability = vi.fn().mockResolvedValue(
    availability.map((it) => ({
      reason: "",
      hint: "",
      projectPath: "",
      ...it,
    })),
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

beforeEach(() => {
  eventHandlers.clear();
});

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

function renderLine(
  overrides: Partial<{
    overrideBackendId: number | null;
    onOverride: (id: number | null) => void;
    projectId: number;
  }> = {},
) {
  const onOverride = overrides.onOverride ?? vi.fn();
  render(
    <MemoryRouter>
      <NewSessionExecTargetLine
        agentId={7}
        agentName="开发"
        projectId={overrides.projectId ?? 0}
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

  // (a) 改选浮层每档带那台机器上的项目路径——选机器时真正要判断的是「换过去在
  //     哪个目录干活」。
  it("reselect popover:每档列出那台机器上的项目路径，没有路径的档不留空行", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true, projectPath: "/Users/me/app" },
        { agentBackendId: 52, available: true, projectPath: "/srv/app" },
        {
          agentBackendId: 53,
          available: false,
          reason: "exec-target-project-path-missing",
          projectPath: "",
        },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
        { id: 53, deviceId: "4", deviceName: "测试机", online: true },
      ],
    );
    renderLine({ projectId: 900 });
    await screen.findByTestId("new-session-exec-target-line");

    await userEvent.click(screen.getByText("Change"));
    expect(await screen.findByText("/Users/me/app")).toBeInTheDocument();
    expect(screen.getByText("/srv/app")).toBeInTheDocument();
    // 没配路径的那一档不渲染一行空路径。
    expect(screen.getAllByTestId("exec-target-project-path")).toHaveLength(2);
  });

  // (b) 空会话态的机器 chip 复用共享的 DeviceTag（本机 → MapPin，远端在线 →
  //     Server），不自己再画一个 span。
  it("空会话态的 chip 是共享的 DeviceTag：本机档带 MapPin", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
      ],
    );
    renderLine();
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(within(line).getByText("Local")).toBeInTheDocument();
    expect(line.querySelector(".lucide-map-pin")).not.toBeNull();
  });

  it("空会话态的 chip 是共享的 DeviceTag：远端在线档带 Server", async () => {
    stubWails(
      [
        { agentBackendId: 52, available: true },
        { agentBackendId: 51, available: true },
      ],
      [
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
        { id: 51, deviceId: "" },
      ],
    );
    renderLine();
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(within(line).getByText("构建机")).toBeInTheDocument();
    expect(line.querySelector(".lucide-server")).not.toBeNull();
  });

  // (c) 起轮前选中结果是活的：可用性变化重新算并改写措辞，否则用户看着「将在
  //     构建机上运行」按下回车、实际跑到了本机。
  it("起轮前可用性变化：重新挑选并从「将在 X 上运行」改写成掉档措辞", async () => {
    const { listAvailability } = stubWails(
      [
        { agentBackendId: 52, available: true },
        { agentBackendId: 51, available: true },
      ],
      [
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
        { id: 51, deviceId: "" },
      ],
    );
    renderLine();
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(within(line).getByText("构建机")).toBeInTheDocument();
    expect(line.className).not.toContain("bg-status-waiting-bg");

    // 构建机掉线了：后端下一次判定翻转。
    listAvailability.mockResolvedValue([
      {
        agentBackendId: 52,
        available: false,
        reason: "exec-target-offline",
        hint: "",
        projectPath: "",
      },
      {
        agentBackendId: 51,
        available: true,
        reason: "",
        hint: "",
        projectPath: "",
      },
    ]);
    await emitDeviceStateChange();

    await waitFor(() => {
      expect(screen.getByText(/构建机 is unavailable/)).toBeInTheDocument();
    });
    const updated = screen.getByTestId("new-session-exec-target-line");
    expect(updated.className).toContain("bg-status-waiting-bg");
    expect(within(updated).getByText("Local")).toBeInTheDocument();
    expect(screen.getByText("Offline")).toBeInTheDocument();
  });

  it("manual override to a non-first available candidate: shown plainly, not flagged as dropped", async () => {
    stubWails(
      [
        { agentBackendId: 51, available: true },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "" },
        // 可用的远端档必然在线（R15 的判据之一就是在线），fixture 与之保持一致。
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
      ],
    );
    renderLine({ overrideBackendId: 52 });
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(line.className).not.toContain("bg-status-waiting-bg");
    expect(within(line).getByText("构建机")).toBeInTheDocument();
    // chip 就是共享的 DeviceTag（远端在线 → Server 图标）。
    expect(line.querySelector(".lucide-server")).not.toBeNull();
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
