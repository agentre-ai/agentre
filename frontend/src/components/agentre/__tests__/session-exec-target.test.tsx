import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
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

  // (d) 重叠请求：设备上下线推送 + agentId/projectId 变化都会让多次 reload 同时在飞，
  //     先发的那次晚返回时不能把新快照盖回旧的——那正是这个订阅要防的事。
  it("重叠的可用性请求：先发后到的旧响应不能盖掉新快照", async () => {
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

    // 第一次（挂载）：构建机掉线的旧判定，故意拖到最后才返回。
    let resolveStale: (v: unknown) => void = () => {};
    listAvailability.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveStale = resolve;
      }),
    );
    renderLine();
    await act(async () => {});

    // 第二次（设备重新上线的推送）：新判定立刻返回，构建机可用。
    listAvailability.mockResolvedValue([
      {
        agentBackendId: 52,
        available: true,
        reason: "",
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
    const line = await screen.findByTestId("new-session-exec-target-line");
    expect(within(line).getByText("构建机")).toBeInTheDocument();

    // 旧请求现在才落地：必须被丢弃。
    await act(async () => {
      resolveStale([
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
    });

    const after = screen.getByTestId("new-session-exec-target-line");
    expect(within(after).getByText("构建机")).toBeInTheDocument();
    expect(after.className).not.toContain("bg-status-waiting-bg");
    expect(screen.queryByText("Offline")).not.toBeInTheDocument();
  });

  // (e) 候选列表为空分两种：还没加载出来 / 加载失败。两种都不代表「钉住的那一档
  //     真的没了」，不能把用户刚手动选的机器悄悄换回自动挑选。
  it("候选还没加载出来时不清掉手动指定的档", async () => {
    const { listAvailability } = stubWails(
      [{ agentBackendId: 51, available: true }],
      [{ id: 51, deviceId: "" }],
    );
    listAvailability.mockReturnValue(new Promise(() => {}));

    const onOverride = vi.fn();
    renderLine({ overrideBackendId: 52, onOverride });
    await act(async () => {});

    expect(onOverride).not.toHaveBeenCalled();
  });

  it("一次失败的重新加载不清掉手动指定的档，成功后仍然钉在那台机器上", async () => {
    const { listAvailability } = stubWails(
      [
        { agentBackendId: 51, available: true },
        { agentBackendId: 52, available: true },
      ],
      [
        { id: 51, deviceId: "" },
        { id: 52, deviceId: "3", deviceName: "构建机", online: true },
      ],
    );

    const onOverride = vi.fn();
    function ControlledLine() {
      const [id, setId] = React.useState<number | null>(null);
      return (
        <NewSessionExecTargetLine
          agentId={7}
          agentName="开发"
          projectId={0}
          overrideBackendId={id}
          onOverride={(v) => {
            onOverride(v);
            setId(v);
          }}
        />
      );
    }
    render(
      <MemoryRouter>
        <ControlledLine />
      </MemoryRouter>,
    );
    await screen.findByTestId("new-session-exec-target-line");

    // 手动改选到构建机。
    await userEvent.click(screen.getByText("Change"));
    await userEvent.click(
      await screen.findByRole("button", { name: /构建机/ }),
    );
    expect(onOverride).toHaveBeenLastCalledWith(52);

    // 设备状态推送触发重新加载，但这次 RPC 失败了 → 候选被清空。
    listAvailability.mockRejectedValueOnce(new Error("rpc down"));
    await emitDeviceStateChange();
    expect(onOverride).not.toHaveBeenCalledWith(null);

    // 下一次成功加载：列表回来了，钉住的那一档也还在。
    await emitDeviceStateChange();
    await waitFor(() => {
      const line = screen.getByTestId("new-session-exec-target-line");
      expect(within(line).getByText("构建机")).toBeInTheDocument();
    });
    expect(onOverride).not.toHaveBeenCalledWith(null);
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
