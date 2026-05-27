/**
 * chat-panel.test.tsx — ChatPanel 内部派生行为测试（T17 breadcrumb + T18 worktree merge）。
 *
 * 策略：mock 掉所有 wailsjs RPC、heavy child components（ChatComposer / ChatTranscript /
 * ProjectMergeDialog），以及 use-project-tree / use-chat-session，保持 ChatPanel
 * 自身的派生逻辑可测而不拉全量组件树。
 */

import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("sonner", () => sonnerMocks);

// ── wailsjs App RPC mocks ────────────────────────────────────────────────────

const appMocks = vi.hoisted(() => ({
  CancelQueuedChatMessage: vi.fn(),
  CompactChatSession: vi.fn(),
  DeleteChatSession: vi.fn(),
  EditChatMessage: vi.fn(),
  EnqueueChatMessage: vi.fn(),
  GetChatLaunchCommand: vi.fn(),
  LoadChatSession: vi.fn(),
  MarkChatSessionRead: vi.fn().mockResolvedValue({}),
  RegenerateChatMessage: vi.fn(),
  RenameChatSession: vi.fn(),
  SendChatMessage: vi.fn(),
  StopChatMessage: vi.fn(),
  GetSessionGitState: vi.fn().mockResolvedValue({
    state: {
      branch: "",
      worktree: "",
      dirty: 0,
      ahead: 0,
      behind: 0,
      hasUpstream: false,
      notARepo: true,
      updatedAt: 0,
    },
  }),
  // 需要 ProjectListTree 供 use-project-tree，但我们 mock 掉整个 hook
  ProjectListTree: vi.fn().mockResolvedValue([]),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

const componentMocks = vi.hoisted(() => ({
  chatComposerProps: [] as Array<Record<string, unknown>>,
  permissionModePillProps: [] as Array<Record<string, unknown>>,
  permissionMode: "plan",
  cycleMode: vi.fn(),
  setMode: vi.fn(),
  // 控制 useSessionCapabilities 桩返回的 caps;测试按 backend 切换 switchableDuringTurn。
  capsSwitchableDuringTurn: true,
  capsAllowedModes: ["default", "plan", "acceptEdits", "bypassPermissions"],
  computeComposerContextUsage: vi.fn((..._args: unknown[]) => ({
    max: 0,
    used: 0,
  })),
}));

// ── wailsjs runtime mock（EventsOn / EventsOff）────────────────────────────

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOff: vi.fn(),
  EventsOn: vi.fn(),
}));

// ── use-project-tree: 单例缓存 hook，直接 mock 返回测试用树 ──────────────────

vi.mock("@/hooks/use-project-tree", () => ({
  useProjectTree: () => ({
    tree: [
      {
        project: { id: 1, name: "Agentre" },
        children: [{ project: { id: 2, name: "backend" }, children: [] }],
      },
    ],
    invalidate: () => {},
    loaded: true,
  }),
}));

// ── use-chat-session: 直接 mock，避免真实 LoadChatSession RPC 被调用 ────────

// makeMockSession 构造最小化的 ChatSessionDetail，只提供测试需要的字段。
// 通过 `overrides` 注入测试想要的字段（projectId / workMode / title 等）。
const mockSessionStore: { session: Record<string, unknown> | null } = {
  session: null,
};

vi.mock("@/hooks/use-chat-session", () => ({
  useChatSession: () => ({
    session: mockSessionStore.session,
    messages: [],
    loading: false,
    error: null,
    reload: () => Promise.resolve(),
    setMessages: () => {},
  }),
}));

// ── child component mocks ──────────────────────────────────────────────────

// ChatComposer / ChatTranscript 各自有大量依赖（TipTap / prism 等），mock 成最简桩。
vi.mock("../chat", async () => {
  const React = await import("react");
  return {
    ChatComposer: (props: {
      onSubmit?: (text: string) => void;
      permissionModeSlot?: React.ReactNode;
      topSlot?: React.ReactNode;
    }) => {
      componentMocks.chatComposerProps.push(props as Record<string, unknown>);
      return React.createElement(
        React.Fragment,
        null,
        props.topSlot,
        props.permissionModeSlot,
      );
    },
    // ChatTranscript 不再参与 codex plan approve/continue 流程
    // (PlanCard 直接调 wailsResolvePlanAction,bypass chat-panel),
    // 桩成空即可。
    ChatTranscript: () => React.createElement("div", null),
  };
});

// ProjectMergeDialog：只渲染一个可识别的占位 span，供 T18 断言用。
vi.mock("../project-merge-dialog", () => ({
  ProjectMergeDialog: ({ sessionID }: { sessionID: number }) =>
    sessionID > 0
      ? React.createElement("div", { "data-testid": "merge-dialog" }, null)
      : null,
}));

// PermissionModePill / QueuedMessagesBar / TaskProgressBar：桩
vi.mock("../permission-mode", async () => {
  const React = await import("react");
  return {
    PermissionModePill: (props: Record<string, unknown>) => {
      componentMocks.permissionModePillProps.push(props);
      return React.createElement("button", {
        "data-testid": "permission-mode-pill",
        disabled: Boolean(props.disabled),
        type: "button",
      });
    },
    usePermissionMode: () => ({
      mode: componentMocks.permissionMode,
      modes: [],
      setMode: componentMocks.setMode,
      cycleMode: componentMocks.cycleMode,
      error: null,
      permissionModeAtLaunch: "",
      hasActiveSession: false,
    }),
  };
});

// useSessionCapabilities 桩 — Plan C 起 chat-panel 通过它读 set_permission_mode +
// PermissionModeMeta.SwitchableDuringTurn。codex 测试通过 capsSwitchableDuringTurn=false
// 模拟"turn 中不允许切 mode"行为(原走 backendType === 'codex' 硬分支)。
// 真实 hook 在 sessionId<=0 时返 null caps;桩同样按真实行为返 null,让"新对话"
// 路径走 useBackendCapabilities 分支。
function makeCapsStub() {
  return {
    has: (c: string) => c === "set_permission_mode",
    permissionModeMeta: {
      allowedModes: componentMocks.capsAllowedModes,
      defaultMode: "default",
      switchableDuringTurn: componentMocks.capsSwitchableDuringTurn,
      order: componentMocks.capsAllowedModes,
    },
  };
}

vi.mock("../capability/use-session-capabilities", () => ({
  useSessionCapabilities: (sessionId?: number | null) => ({
    caps: sessionId && sessionId > 0 ? makeCapsStub() : null,
  }),
}));

// useBackendCapabilities 桩 — 新对话(sessionId<=0)按 backendType 拉 caps,
// 让 PermissionModePill 在首发前就能渲染。
vi.mock("../capability/use-backend-capabilities", () => ({
  useBackendCapabilities: (backendType?: string | null) => ({
    caps: backendType ? makeCapsStub() : null,
  }),
}));

vi.mock("../queued-messages-bar", () => ({
  QueuedMessagesBar: () => null,
}));

vi.mock("../task-progress/task-progress-bar", () => ({
  TaskProgressBar: () => null,
}));

vi.mock("../task-progress/derive", () => ({
  deriveTaskProgress: () => ({ total: 0, done: 0 }),
}));

// chat-panel-context-usage 有复杂计算，桩掉
vi.mock("../chat-panel-context-usage", () => ({
  computeComposerContextUsage: (...args: unknown[]) =>
    componentMocks.computeComposerContextUsage(...args),
}));

// ── import after mocks ─────────────────────────────────────────────────────

import { ChatPanel } from "../chat-panel";
import { useChatStreamsStore } from "@/stores/chat-streams-store";

/** 清 store streams 以避免测试间串台 */
function resetStore() {
  useChatStreamsStore.getState().streams.clear();
  componentMocks.chatComposerProps.length = 0;
  componentMocks.permissionModePillProps.length = 0;
  componentMocks.permissionMode = "plan";
  // 默认 claudecode-like caps(允许 turn 中切 mode);Codex 测试用例显式置 false。
  componentMocks.capsSwitchableDuringTurn = true;
  componentMocks.capsAllowedModes = [
    "default",
    "plan",
    "acceptEdits",
    "bypassPermissions",
  ];
  componentMocks.computeComposerContextUsage.mockClear();
  componentMocks.cycleMode.mockClear();
  componentMocks.setMode.mockClear();
  appMocks.SendChatMessage.mockReset();
  appMocks.CompactChatSession.mockReset();
  appMocks.EnqueueChatMessage.mockReset();
  appMocks.GetChatLaunchCommand.mockReset();
  sonnerMocks.toast.error.mockClear();
  sonnerMocks.toast.success.mockClear();
}

/** 构造 ChatSessionDetail 最小形状 */
function makeSession(overrides: Record<string, unknown>) {
  return {
    agentColor: "agent-1",
    agentIcon: "",
    agentId: 7,
    agentName: "Eng",
    backendType: "builtin",
    createtime: 0,
    id: 42,
    lastMessageAt: 0,
    lastReadAt: 0,
    needsAttention: false,
    agentStatus: "idle",
    permissionMode: "",
    permissionModeAtLaunch: "",
    contextWindow: 0,
    llmProviderType: "",
    title: "Test session",
    workMode: "",
    worktreeBranch: "",
    projectId: 0,
    ...overrides,
  };
}

// ─── T17: breadcrumb 派生 ─────────────────────────────────────────────────────

describe("ChatPanel · T17 breadcrumb 派生", () => {
  it("长会话标题在 toolbar 中最多显示两行而不是单行截断", () => {
    resetStore();
    const longTitle =
      "这是一个很长的 AI 对话标题，用来确认工具栏会尽量展示完整内容而不是过早省略";
    mockSessionStore.session = makeSession({ id: 42, title: longTitle });

    render(<ChatPanel sessionId={42} />);

    const title = screen.getByText(longTitle);
    expect(title).toHaveClass("line-clamp-2");
    expect(title).not.toHaveClass("truncate");
    expect(title).toHaveAttribute("title", longTitle);
  });

  it("session.projectId=2 时 header 显示 'Agentre / backend'", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42, projectId: 2 });

    render(<ChatPanel sessionId={42} />);

    // 树里 id=2 的路径是 Agentre → backend
    expect(screen.getByText("Agentre / backend")).toBeInTheDocument();
    // session id 也显示
    expect(screen.getByText("sess-42")).toBeInTheDocument();
  });

  it("session.projectId=1 时 header 显示 'Agentre'（顶级）", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 10, projectId: 1 });

    render(<ChatPanel sessionId={10} />);

    expect(screen.getByText("Agentre")).toBeInTheDocument();
    expect(screen.getByText("sess-10")).toBeInTheDocument();
  });

  it("session.projectId=0 时 header 无 breadcrumb", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42, projectId: 0 });

    render(<ChatPanel sessionId={42} />);

    expect(screen.queryByText(/Agentre/)).not.toBeInTheDocument();
    expect(screen.queryByText(/sess-/)).not.toBeInTheDocument();
  });
});

describe("ChatPanel · 新对话 PermissionModePill", () => {
  it("sessionId=0 + newSessionAgent 是 claudecode 时,按 backend caps 渲染 pill (回归: 此前因 caps 永为 null 而隐藏)", () => {
    resetStore();
    mockSessionStore.session = null;
    render(
      <ChatPanel
        sessionId={0}
        newSessionAgent={
          {
            id: 7,
            name: "Eng",
            agentBackendId: 1,
            backendType: "claudecode",
            defaultPermissionMode: "plan",
          } as never
        }
      />,
    );
    expect(screen.getByTestId("permission-mode-pill")).toBeInTheDocument();
  });

  it("sessionId=0 且无 newSessionAgent 时不渲染 pill (空态)", () => {
    resetStore();
    mockSessionStore.session = null;
    render(<ChatPanel sessionId={0} />);
    expect(
      screen.queryByTestId("permission-mode-pill"),
    ).not.toBeInTheDocument();
  });
});

describe("ChatPanel · Codex collaboration mode", () => {
  it("uses live Codex contextWindow while session detail still has 0", () => {
    resetStore();
    mockSessionStore.session = makeSession({
      backendType: "codex",
      contextWindow: 0,
      id: 42,
      permissionMode: "default",
    });

    act(() => {
      useChatStreamsStore.getState().openStream({
        assistantMessageId: 1001,
        name: "chat:event:42:1001",
        sessionId: 42,
        streamStartedAt: Date.now(),
      });
      useChatStreamsStore.getState().patchLiveContextWindow(42, 258400);
    });

    render(<ChatPanel sessionId={42} />);

    expect(componentMocks.computeComposerContextUsage).toHaveBeenLastCalledWith(
      [],
      258400,
      null,
    );
  });

  it("disables mode switching while the current Codex turn is streaming", () => {
    resetStore();
    // Codex caps: switchableDuringTurn=false → turn 中 pill 应被禁用。
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    act(() => {
      useChatStreamsStore.getState().openStream({
        assistantMessageId: 1001,
        name: "chat:event:42:1001",
        sessionId: 42,
        streamStartedAt: Date.now(),
      });
    });

    render(<ChatPanel sessionId={42} />);

    expect(componentMocks.permissionModePillProps.at(-1)?.disabled).toBe(true);
    expect(componentMocks.chatComposerProps.at(-1)?.onShiftTab).toBeUndefined();
    expect(screen.getByTestId("permission-mode-pill")).toBeDisabled();
  });

  it("disables mode switching when Codex session status is already running", () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      agentStatus: "running",
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });

    render(<ChatPanel sessionId={42} />);

    expect(componentMocks.permissionModePillProps.at(-1)?.disabled).toBe(true);
    expect(componentMocks.chatComposerProps.at(-1)?.onShiftTab).toBeUndefined();
    expect(screen.getByTestId("permission-mode-pill")).toBeDisabled();
  });

  it("sends the selected plan mode after the Codex turn is idle", async () => {
    resetStore();
    // Codex caps: switchableDuringTurn=false → turn 中 pill 应被禁用。
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "plan",
    });
    appMocks.SendChatMessage.mockResolvedValue({
      assistantMessageId: 1001,
      sessionId: 42,
      stream: "chat:event:42:1001",
      userMessageId: 1000,
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("next turn");
    });

    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          permissionMode: "plan",
          sessionId: 42,
          text: "next turn",
        }),
      );
    });
  });

  it("exact /compact starts Codex compact RPC instead of sending a user message", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    appMocks.CompactChatSession.mockResolvedValue({
      assistantMessageId: 1001,
      sessionId: 42,
      stream: "chat:event:42:1001",
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("/compact");
    });

    await waitFor(() => {
      expect(appMocks.CompactChatSession).toHaveBeenCalledWith({
        sessionId: 42,
      });
    });
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(useChatStreamsStore.getState().streams.get(42)?.name).toBe(
      "chat:event:42:1001",
    );
  });

  it("exact /compact is rejected while the Codex turn is streaming", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    act(() => {
      useChatStreamsStore.getState().openStream({
        assistantMessageId: 1001,
        name: "chat:event:42:1001",
        sessionId: 42,
        streamStartedAt: Date.now(),
      });
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("/compact");
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(appMocks.CompactChatSession).not.toHaveBeenCalled();
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(appMocks.EnqueueChatMessage).not.toHaveBeenCalled();
  });

  it("exact /compact in a new Codex chat asks for an existing thread", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = null;

    render(
      <ChatPanel
        sessionId={0}
        newSessionAgent={
          {
            id: 7,
            name: "Codex",
            agentBackendId: 1,
            backendType: "codex",
            defaultPermissionMode: "default",
          } as never
        }
      />,
    );
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("/compact");
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(appMocks.CompactChatSession).not.toHaveBeenCalled();
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
  });

  // codex plan approve/continue 不再由 chat-panel 中转 SendChatMessage —— PlanCard
  // 直接调 wailsResolvePlanAction(canonical-tool/plan/card.test.tsx 覆盖)。
  // backend 端 plan_action_test.go 验证 actionId → Send 的入参映射。
});

// ─── 回归: SendChatMessage 失败需在 UI 上 inline 显示, 不能被 void 吞掉 ─────
// doSend 的所有调用点 (新建会话首发 / 已有会话续发) 都是 void doSend(...) fire-and-forget,
// 历史上整个函数没有 try/catch, 失败时 Promise rejection 进 console 都未必到,
// UI 完全无声, 用户体感"发出去有错误但没任何报错信息出来"。
describe("ChatPanel · doSend error surfacing", () => {
  it("shows an inline error notice when SendChatMessage rejects on a new chat", async () => {
    resetStore();
    mockSessionStore.session = null;
    appMocks.SendChatMessage.mockRejectedValueOnce(
      new Error("provider not configured"),
    );

    render(
      <ChatPanel
        sessionId={0}
        newSessionAgent={
          {
            id: 7,
            name: "Eng",
            agentBackendId: 1,
            backendType: "claudecode",
            defaultPermissionMode: "default",
          } as never
        }
      />,
    );
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("hello");
    });

    await waitFor(() => {
      expect(
        screen.getByText(/发送失败.*provider not configured/),
      ).toBeInTheDocument();
    });
  });

  it("shows an inline error notice when SendChatMessage rejects on an existing session", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    appMocks.SendChatMessage.mockRejectedValueOnce(new Error("backend down"));

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("next turn");
    });

    await waitFor(() => {
      expect(screen.getByText(/发送失败.*backend down/)).toBeInTheDocument();
    });
  });
});

describe("ChatPanel · launch command copy feedback", () => {
  it("Given the launch command is copied, When the user selects the copy action, Then feedback appears as a timed bottom-right Sonner toast", async () => {
    const user = userEvent.setup();
    resetStore();
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      title: "Remote turn",
    });
    appMocks.GetChatLaunchCommand.mockResolvedValueOnce({
      command: "AGENTRE_TOKEN=t agentre claudecode resume 42",
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    render(<ChatPanel sessionId={42} />);

    await user.click(screen.getByRole("button", { name: "更多操作" }));
    await user.click(await screen.findByText("复制启动命令"));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(
        "AGENTRE_TOKEN=t agentre claudecode resume 42",
      );
    });
    expect(sonnerMocks.toast.success).toHaveBeenCalledWith(
      "已复制启动命令",
      expect.objectContaining({
        description: expect.stringContaining("含 token"),
        duration: 5000,
        position: "bottom-right",
      }),
    );
    expect(
      screen.queryByText(/已复制启动命令（含 token）/),
    ).not.toBeInTheDocument();
  });
});

// ─── Mark-read gating by `active` prop ────────────────────────────────────────
// chat-panel-host 把所有 tab 都 mount 起来,只用 display:none 控制可见;
// 隐藏 tab 的 ChatPanel 不应在 Done 时把 session 标记成已读 ——
// 那会让用户在另一个 tab 时,后台 turn 完成后未读 indicator 永远不出现。
// 同时 active=true 时,session.lastMessageAt 非零 / 推进时应 MarkRead。

import { useSessionStatusStore } from "@/stores/session-status-store";

describe("ChatPanel · mark-read gated by active prop", () => {
  it("does not call MarkChatSessionRead when active=false and Done fires", async () => {
    resetStore();
    appMocks.MarkChatSessionRead.mockClear();
    useSessionStatusStore.getState().__reset();
    mockSessionStore.session = makeSession({
      id: 42,
      lastMessageAt: 2000,
    });

    render(<ChatPanel sessionId={42} active={false} />);

    // 模拟 turn 完成 — chat-streams-host 会调 bumpDone。
    act(() => {
      useSessionStatusStore.getState().bumpDone(42, {
        kind: "done",
        message: { sessionId: 42 } as never,
      });
    });

    // 给 effect 一个 tick;若隐藏 tab 错误地 MarkRead,这里就会断言失败。
    await waitFor(() => {
      expect(useSessionStatusStore.getState().statuses.get(42)?.doneTick).toBe(
        1,
      );
    });
    expect(appMocks.MarkChatSessionRead).not.toHaveBeenCalled();
  });

  it("calls MarkChatSessionRead when active=true with non-zero lastMessageAt", async () => {
    resetStore();
    appMocks.MarkChatSessionRead.mockClear();
    useSessionStatusStore.getState().__reset();
    mockSessionStore.session = makeSession({
      id: 7,
      lastMessageAt: 1500,
    });

    render(<ChatPanel sessionId={7} active={true} />);

    await waitFor(() => {
      expect(appMocks.MarkChatSessionRead).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: 7, timestamp: 1500 }),
      );
    });
  });
});
