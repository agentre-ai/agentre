/**
 * chat-panel.test.tsx — ChatPanel 内部派生行为测试（T17 breadcrumb + T18 worktree merge）。
 *
 * 策略：mock 掉所有 wailsjs RPC、heavy child components（ChatComposer / ChatTranscript /
 * ProjectMergeDialog），以及 use-project-tree / use-chat-session，保持 ChatPanel
 * 自身的派生逻辑可测而不拉全量组件树。
 */

import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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
  GetCCUsage: vi.fn().mockResolvedValue({ reason: "" }),
  GetChatLaunchCommand: vi.fn(),
  GetChatGoal: vi.fn(),
  LoadChatSession: vi.fn(),
  MarkChatSessionRead: vi.fn().mockResolvedValue({}),
  RegenerateChatMessage: vi.fn(),
  RenameChatSession: vi.fn(),
  SendChatMessage: vi.fn(),
  SetChatGoal: vi.fn(),
  StartChatGoal: vi.fn(),
  StopChatMessage: vi.fn(),
  ClearChatGoal: vi.fn(),
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
  chatTranscriptProps: [] as Array<Record<string, unknown>>,
  permissionModePillProps: [] as Array<Record<string, unknown>>,
  permissionMode: "plan",
  cycleMode: vi.fn(),
  setMode: vi.fn(),
  // 控制 useSessionCapabilities 桩返回的 caps;测试按 backend 切换 switchableDuringTurn。
  capsSwitchableDuringTurn: true,
  capsAllowedModes: ["default", "plan", "acceptEdits", "bypassPermissions"],
  capsImageInput: true,
  computeComposerContextUsage: vi.fn((..._args: unknown[]) => ({
    max: 0,
    used: 0,
  })),
}));

// ── wailsjs runtime mock（EventsOn / EventsOff）────────────────────────────

const runtimeMocks = vi.hoisted(() => ({
  EventsOff: vi.fn(),
  EventsOn: vi.fn(() => vi.fn()),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => runtimeMocks);

// ── use-project-tree: 单例缓存 hook，直接 mock 返回测试用树 ──────────────────

vi.mock("@/hooks/use-project-tree", () => ({
  useProjectTree: () => ({
    tree: [
      {
        project: { id: 1, name: "Agentre" },
        children: [
          {
            project: { id: 2, name: "backend", color: "agent-5" },
            children: [],
          },
        ],
      },
    ],
    invalidate: () => {},
    loaded: true,
  }),
}));

// ── use-chat-session: 直接 mock，避免真实 LoadChatSession RPC 被调用 ────────

// makeMockSession 构造最小化的 ChatSessionDetail，只提供测试需要的字段。
// 通过 `overrides` 注入测试想要的字段（projectId / workMode / title 等）。
const mockSessionStore: {
  messages: Array<Record<string, unknown>>;
  session: Record<string, unknown> | null;
} = {
  messages: [],
  session: null,
};

// setMessagesSpy 允许断言 setMessages 是否被调用（T29 subagent_activity_started）
const setMessagesSpy = vi.hoisted(() => vi.fn());
// reloadSpy 允许断言点「停止」后是否主动 reload 会话（重启遗孤 reconcile 后收回按钮）
const reloadSpy = vi.hoisted(() => vi.fn(() => Promise.resolve()));

vi.mock("@/hooks/use-chat-session", () => ({
  useChatSession: () => ({
    session: mockSessionStore.session,
    messages: mockSessionStore.messages,
    loading: false,
    error: null,
    reload: reloadSpy,
    setMessages: setMessagesSpy,
  }),
}));

// useCCUsage: 捕获每次调用 deviceKey, 让测试断言 ChatPanel 把"哪台 device 的配额"
// 派给了 ChatComposer。返回值固定 undefined(未首探), 测试只关心 key 路由。
const ccUsageMock = vi.hoisted(() => ({
  calls: [] as string[],
}));

vi.mock("@/hooks/use-cc-usage", () => ({
  useCCUsage: (deviceKey: string) => {
    ccUsageMock.calls.push(deviceKey);
    return undefined;
  },
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
    ChatTranscript: (props: Record<string, unknown>) => {
      componentMocks.chatTranscriptProps.push(props);
      return React.createElement("div", { "data-testid": "chat-transcript" });
    },
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
function makeCapsStub(backendType?: string | null) {
  const supportsCompact = backendType === "codex" || backendType === "piagent";
  return {
    has: (c: string) =>
      c === "set_permission_mode" ||
      (c === "image_input" && componentMocks.capsImageInput) ||
      (c === "compact" && supportsCompact),
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
    caps:
      sessionId && sessionId > 0
        ? makeCapsStub(String(mockSessionStore.session?.backendType ?? ""))
        : null,
  }),
}));

// useBackendCapabilities 桩 — 新对话(sessionId<=0)按 backendType 拉 caps,
// 让 PermissionModePill 在首发前就能渲染。
vi.mock("../capability/use-backend-capabilities", () => ({
  useBackendCapabilities: (backendType?: string | null) => ({
    caps: backendType ? makeCapsStub(backendType) : null,
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

import { ChatPanel, computeTopVisibleAnchor } from "../chat-panel";
import {
  __resetChatPanelScrollStateForTesting,
  loadTranscriptScrollState,
} from "../chat-panel-scroll-state";
import {
  streamForMessage,
  useChatStreamsStore,
} from "@/stores/chat-streams-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";

/** 清 store streams 以避免测试间串台 */
function resetStore() {
  __resetChatPanelScrollStateForTesting();
  mockSessionStore.messages = [];
  useChatStreamsStore.getState().streams.clear();
  runtimeMocks.EventsOn.mockReset();
  runtimeMocks.EventsOn.mockImplementation(() => vi.fn());
  setMessagesSpy.mockClear();
  reloadSpy.mockClear();
  componentMocks.chatComposerProps.length = 0;
  componentMocks.chatTranscriptProps.length = 0;
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
  componentMocks.capsImageInput = true;
  componentMocks.computeComposerContextUsage.mockClear();
  componentMocks.cycleMode.mockClear();
  componentMocks.setMode.mockClear();
  ccUsageMock.calls.length = 0;
  appMocks.SendChatMessage.mockReset();
  appMocks.SetChatGoal.mockReset();
  appMocks.GetChatGoal.mockReset();
  appMocks.ClearChatGoal.mockReset();
  appMocks.StartChatGoal.mockReset();
  appMocks.CompactChatSession.mockReset();
  appMocks.EnqueueChatMessage.mockReset();
  appMocks.GetChatLaunchCommand.mockReset();
  sonnerMocks.toast.error.mockClear();
  sonnerMocks.toast.success.mockClear();
}

function transcriptScroller(container: HTMLElement): HTMLElement {
  const el = container.querySelector("section");
  if (!el) throw new Error("transcript scroller not found");
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    get: () => 480,
  });
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    get: () => 4_000,
  });
  return el as HTMLElement;
}

function transcriptScrollerWithDynamicHeight(
  container: HTMLElement,
  scrollHeight: () => number,
): HTMLElement {
  const el = transcriptScroller(container);
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    get: scrollHeight,
  });
  return el;
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
    const projectPath = screen.getByText("Agentre / backend");
    expect(projectPath).toHaveClass("text-agent-5");
    expect(projectPath.previousElementSibling).toHaveClass("text-agent-5");
    // session id 也显示
    expect(screen.getByText("sess-42")).toHaveClass("text-muted-foreground");
  });

  it("session.projectId=1 时 header 显示 'Agentre'（顶级）", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 10, projectId: 1 });

    render(<ChatPanel sessionId={10} />);

    expect(screen.getByText("Agentre")).toBeInTheDocument();
    expect(screen.getByText("sess-10")).toBeInTheDocument();
  });

  it("session.projectId=0 时 header 仍显示 session id", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42, projectId: 0 });

    render(<ChatPanel sessionId={42} />);

    expect(screen.queryByText(/Agentre/)).not.toBeInTheDocument();
    expect(screen.getByText("sess-42")).toBeInTheDocument();
  });
});

describe("ChatPanel · transcript cwd", () => {
  it("Given session has cwd, When transcript renders, Then cwd is passed through for local link classification", () => {
    resetStore();
    mockSessionStore.session = makeSession({
      cwd: "/Users/codfrm/Code/agentre/agentre",
      id: 42,
    });

    render(<ChatPanel sessionId={42} />);

    expect(componentMocks.chatTranscriptProps.at(-1)?.cwd).toBe(
      "/Users/codfrm/Code/agentre/agentre",
    );
  });
});

describe("computeTopVisibleAnchor", () => {
  function fakeRow(id: string, top: number, bottom: number): HTMLElement {
    return {
      getAttribute: (name: string) => (name === "data-message-id" ? id : null),
      getBoundingClientRect: () => ({ top, bottom }) as DOMRect,
    } as unknown as HTMLElement;
  }
  function fakeContainer(top: number, rows: HTMLElement[]): HTMLElement {
    return {
      getBoundingClientRect: () => ({ top }) as DOMRect,
      querySelectorAll: () => rows as unknown as NodeListOf<HTMLElement>,
    } as unknown as HTMLElement;
  }

  it("Given rows straddling the viewport top, Then it anchors to the first row whose bottom crosses the top and records the overscroll px", () => {
    const el = fakeContainer(100, [
      fakeRow("1", 0, 50), // 完全在视口上方 (bottom 50 ≤ 100) → 跳过
      fakeRow("2", 60, 140), // 第一条底边越过视口顶 → 命中
      fakeRow("3", 140, 300),
    ]);
    expect(computeTopVisibleAnchor(el)).toEqual({
      anchorId: 2,
      anchorOffset: 40,
    });
  });

  it("Given the top-visible row starts below the viewport top, Then anchorOffset clamps to 0", () => {
    const el = fakeContainer(100, [fakeRow("7", 120, 300)]);
    expect(computeTopVisibleAnchor(el)).toEqual({
      anchorId: 7,
      anchorOffset: 0,
    });
  });

  it("Given rows carry data-row-key, Then the anchor includes the row key for row-precise restore", () => {
    // 行级虚拟化下一条长消息会拆成多行;只记 anchorId 的话,恢复会塌到消息首行,
    // 偏差可达整条消息的高度。data-row-key 让恢复端精确钉回同一行。
    const row = {
      getAttribute: (name: string) =>
        name === "data-message-id"
          ? "1"
          : name === "data-row-key"
            ? "message:1:tool:tool:toolu-120"
            : null,
      getBoundingClientRect: () => ({ top: 60, bottom: 140 }) as DOMRect,
    } as unknown as HTMLElement;
    expect(computeTopVisibleAnchor(fakeContainer(100, [row]))).toEqual({
      anchorId: 1,
      anchorOffset: 40,
      anchorRowKey: "message:1:tool:tool:toolu-120",
    });
  });

  it("Given no message rows, Then it returns null", () => {
    expect(computeTopVisibleAnchor(fakeContainer(100, []))).toBeNull();
  });

  it("Given every row sits entirely above the viewport top, Then it returns null", () => {
    const el = fakeContainer(100, [fakeRow("1", 0, 40), fakeRow("2", 40, 90)]);
    expect(computeTopVisibleAnchor(el)).toBeNull();
  });
});

describe("ChatPanel · transcript scroll restoration", () => {
  it("Given a tab-scoped scroll key, When ChatPanel unmounts across routes and remounts, Then it restores the previous scrollTop", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    const first = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const firstScroller = transcriptScroller(first.container);

    act(() => {
      firstScroller.scrollTop = 1_240;
      fireEvent.scroll(firstScroller);
    });

    first.unmount();
    const second = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const secondScroller = transcriptScroller(second.container);

    expect(secondScroller.scrollTop).toBe(1_240);
  });

  it("Given saved scroll before messages load, When messages arrive after route remount, Then it restores the saved scrollTop instead of following bottom", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    const first = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const firstScroller = transcriptScroller(first.container);

    act(() => {
      firstScroller.scrollTop = 1_240;
      fireEvent.scroll(firstScroller);
    });

    first.unmount();
    let height = 480;
    const second = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const secondScroller = transcriptScrollerWithDynamicHeight(
      second.container,
      () => height,
    );
    act(() => {
      secondScroller.scrollTop = 0;
    });

    act(() => {
      mockSessionStore.messages = [
        { blocks: [], createtime: 0, id: 1, role: "assistant" },
      ];
      height = 4_000;
      second.rerender(<ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />);
    });

    expect(secondScroller.scrollTop).toBe(1_240);
  });

  it("Given a tab resumes at a tall bottom position, When virtualized height briefly collapses, Then the collapsed scroll event does not overwrite the saved position", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    let height = 8_392;
    const view = render(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-collapse" />,
    );
    const scroller = transcriptScrollerWithDynamicHeight(
      view.container,
      () => height,
    );

    act(() => {
      scroller.scrollTop = 7_912;
      fireEvent.scroll(scroller);
    });
    expect(loadTranscriptScrollState("chat-tab-collapse")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });

    view.rerender(
      <ChatPanel
        active={false}
        sessionId={42}
        scrollStateKey="chat-tab-collapse"
      />,
    );
    view.rerender(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-collapse" />,
    );

    act(() => {
      height = 1_096;
      scroller.scrollTop = 896;
      fireEvent.scroll(scroller);
    });

    expect(loadTranscriptScrollState("chat-tab-collapse")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });
  });

  it("Given a tab resumes while virtualized height is collapsed, When active-follow runs, Then it does not overwrite the saved bottom position", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    let height = 8_392;
    const view = render(
      <ChatPanel
        active
        sessionId={42}
        scrollStateKey="chat-tab-active-follow"
      />,
    );
    const scroller = transcriptScrollerWithDynamicHeight(
      view.container,
      () => height,
    );

    act(() => {
      scroller.scrollTop = 7_912;
      fireEvent.scroll(scroller);
    });
    expect(loadTranscriptScrollState("chat-tab-active-follow")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });

    view.rerender(
      <ChatPanel
        active={false}
        sessionId={42}
        scrollStateKey="chat-tab-active-follow"
      />,
    );
    act(() => {
      height = 200;
    });
    view.rerender(
      <ChatPanel
        active
        sessionId={42}
        scrollStateKey="chat-tab-active-follow"
      />,
    );

    expect(loadTranscriptScrollState("chat-tab-active-follow")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });
  });

  it("Given a tab ignored collapsed scroll events, When the virtualized height recovers, Then it restores the saved position before saving again", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    let height = 8_392;
    const view = render(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-recover" />,
    );
    const scroller = transcriptScrollerWithDynamicHeight(
      view.container,
      () => height,
    );

    act(() => {
      scroller.scrollTop = 7_912;
      fireEvent.scroll(scroller);
    });

    view.rerender(
      <ChatPanel
        active={false}
        sessionId={42}
        scrollStateKey="chat-tab-recover"
      />,
    );
    view.rerender(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-recover" />,
    );

    act(() => {
      height = 1_096;
      scroller.scrollTop = 896;
      fireEvent.scroll(scroller);
    });
    expect(loadTranscriptScrollState("chat-tab-recover")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });

    act(() => {
      height = 8_392;
      scroller.scrollTop = 896;
      fireEvent.scroll(scroller);
    });

    expect(scroller.scrollTop).toBe(7_912);
    expect(loadTranscriptScrollState("chat-tab-recover")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });
  });

  it("Given a tab is visible at the top while virtualized height recovers, When no scroll event fires, Then it proactively restores the saved position", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    let height = 8_392;
    const view = render(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-raf-restore" />,
    );
    const scroller = transcriptScrollerWithDynamicHeight(
      view.container,
      () => height,
    );

    act(() => {
      scroller.scrollTop = 7_912;
      fireEvent.scroll(scroller);
    });

    view.rerender(
      <ChatPanel
        active={false}
        sessionId={42}
        scrollStateKey="chat-tab-raf-restore"
      />,
    );
    act(() => {
      height = 200;
      scroller.scrollTop = 0;
    });
    view.rerender(
      <ChatPanel active sessionId={42} scrollStateKey="chat-tab-raf-restore" />,
    );

    expect(scroller.style.visibility).toBe("hidden");

    act(() => {
      height = 8_392;
    });

    await waitFor(() => {
      expect(scroller.scrollTop).toBe(7_912);
    });
    expect(scroller.style.visibility).toBe("");
  });

  it("Given a new tab starts at the bottom on collapsed virtualized height, When height grows, Then it keeps following the bottom", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    let height = 1_096;
    const view = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-new-bottom" />,
    );
    const scroller = transcriptScrollerWithDynamicHeight(
      view.container,
      () => height,
    );
    act(() => {
      mockSessionStore.messages = [
        { blocks: [], createtime: 0, id: 1, role: "assistant" },
      ];
      view.rerender(
        <ChatPanel sessionId={42} scrollStateKey="chat-tab-new-bottom" />,
      );
    });

    expect(loadTranscriptScrollState("chat-tab-new-bottom")).toEqual({
      atBottom: true,
      scrollTop: 616,
    });

    act(() => {
      height = 8_392;
      fireEvent.scroll(scroller);
    });

    expect(scroller.scrollTop).toBe(7_912);
    expect(loadTranscriptScrollState("chat-tab-new-bottom")).toEqual({
      atBottom: true,
      scrollTop: 7_912,
    });
  });

  it("Given a different tab-scoped scroll key, When the same session opens in a new tab, Then it does not restore the old tab scrollTop", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    const first = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const firstScroller = transcriptScroller(first.container);

    act(() => {
      firstScroller.scrollTop = 1_240;
      fireEvent.scroll(firstScroller);
    });

    first.unmount();
    const second = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-b" />,
    );
    const secondScroller = transcriptScroller(second.container);

    expect(secondScroller.scrollTop).toBe(0);
  });

  it("Given the user scrolls away from the bottom, When the transcript is rendered, Then a back-to-bottom control appears and returns to the bottom", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 42 });
    const { container } = render(
      <ChatPanel sessionId={42} scrollStateKey="chat-tab-a" />,
    );
    const scroller = transcriptScroller(container);

    act(() => {
      scroller.scrollTop = 300;
      fireEvent.scroll(scroller);
    });

    const button = await screen.findByRole("button", {
      name: "Back to bottom",
    });
    fireEvent.click(button);

    expect(scroller.scrollTop).toBe(3_520);
    expect(
      screen.queryByRole("button", { name: "Back to bottom" }),
    ).not.toBeInTheDocument();
  });
});

// QuotaMeter 路由回归: 新建会话(sessionId=0)还没首发前, quotaDeviceKey 不能
// 一律落到 "local" —— 远端 agent 起的新对话必须取 newSessionAgent.deviceID 作为
// "remote:<id>", 否则前端会把本机 5h/7d 配额错画在远端 chat 上(bug repro: 用户
// 用远端 agent 新建会话, agentred 那台没登录, 但 HUD 显示桌面本机的配额数字)。
describe("ChatPanel · 新对话 QuotaMeter 路由", () => {
  it("Given 远端 claudecode agent 起的新会话, When 还没首发, Then useCCUsage 用 remote:<id> 而不是 local", () => {
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
            deviceID: "5",
            deviceName: "remote-box",
          } as never
        }
      />,
    );
    expect(ccUsageMock.calls).toContain("remote:5");
    expect(ccUsageMock.calls).not.toContain("local");
  });

  it("Given 本地 claudecode agent 起的新会话, When 还没首发, Then useCCUsage 用 local", () => {
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
            // 本地 backend: deviceID 为空串
            deviceID: "",
          } as never
        }
      />,
    );
    expect(ccUsageMock.calls).toContain("local");
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

describe("ChatPanel · 新对话空白态文案", () => {
  const newSessionAgent = {
    id: 7,
    name: "Eng",
    agentBackendId: 1,
    backendType: "claudecode",
  } as never;

  it("Given a chat is created from a project, When it has no first message yet, Then the empty copy names the project context", () => {
    resetStore();
    mockSessionStore.session = null;

    render(
      <ChatPanel
        sessionId={0}
        newSessionAgent={newSessionAgent}
        newSessionContext={{ projectId: 2 }}
      />,
    );

    expect(
      screen.getByText("Start a project chat with Eng in Agentre / backend"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Your first message will start this session in the project workspace.",
      ),
    ).toBeInTheDocument();
  });

  it("Given a free chat is created, When it has no first message yet, Then the empty copy stays generic", () => {
    resetStore();
    mockSessionStore.session = null;

    render(<ChatPanel sessionId={0} newSessionAgent={newSessionAgent} />);

    expect(screen.getByText("Start a chat with Eng")).toBeInTheDocument();
    expect(screen.queryByText(/project workspace/)).not.toBeInTheDocument();
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
      useChatStreamsStore.getState().patchLiveContextWindow(42, 1001, 258400);
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

  it("sends image attachments in the SendChatMessage payload", async () => {
    resetStore();
    mockSessionStore.session = makeSession({
      backendType: "builtin",
      id: 42,
    });
    appMocks.SendChatMessage.mockResolvedValue({
      assistantMessageId: 1001,
      sessionId: 42,
      stream: "chat:event:42:1001",
      userMessageId: 1000,
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((message: {
          text: string;
          images?: Array<{ dataUrl: string; mediaType: string; name: string }>;
        }) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.({
        text: "",
        images: [
          {
            dataUrl: "data:image/png;base64,AQID",
            mediaType: "image/png",
            name: "shot.png",
          },
        ],
      });
    });

    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 42,
          text: "",
          images: [
            {
              dataUrl: "data:image/png;base64,AQID",
              name: "shot.png",
            },
          ],
        }),
      );
    });
  });

  it("blocks image payloads when the backend capability is absent", async () => {
    resetStore();
    componentMocks.capsImageInput = false;
    mockSessionStore.session = makeSession({
      backendType: "claudecode",
      id: 42,
    });

    render(<ChatPanel sessionId={42} />);
    expect(componentMocks.chatComposerProps.at(-1)?.supportsImageInput).toBe(
      false,
    );
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((message: {
          text: string;
          images?: Array<{ dataUrl: string; mediaType: string; name: string }>;
        }) => void)
      | undefined;

    act(() => {
      submit?.({
        text: "describe",
        images: [
          {
            dataUrl: "data:image/png;base64,AQID",
            mediaType: "image/png",
            name: "shot.png",
          },
        ],
      });
    });

    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(
      await screen.findByText(
        "The current agent backend does not support image input",
      ),
    ).toBeInTheDocument();
  });

  it.each(["codex", "piagent"])(
    "exact /compact starts %s compact RPC instead of sending a user message",
    async (backendType) => {
      resetStore();
      componentMocks.capsSwitchableDuringTurn = false;
      componentMocks.capsAllowedModes = ["default", "plan"];
      mockSessionStore.session = makeSession({
        backendType,
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
      expect(
        streamForMessage(useChatStreamsStore.getState(), 42, 1001)?.name,
      ).toBe("chat:event:42:1001");
    },
  );

  it("rejects exact /compact when image attachments are present", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((message: {
          text: string;
          images?: Array<{ dataUrl: string; mediaType: string; name: string }>;
        }) => void)
      | undefined;

    act(() => {
      submit?.({
        text: "/compact",
        images: [
          {
            dataUrl: "data:image/png;base64,AQID",
            mediaType: "image/png",
            name: "shot.png",
          },
        ],
      });
    });

    expect(appMocks.CompactChatSession).not.toHaveBeenCalled();
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(
      await screen.findByText("/compact cannot be sent with images"),
    ).toBeInTheDocument();
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

  it("/goal objective sets Codex thread goal and starts a user turn", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    appMocks.SetChatGoal.mockResolvedValue({
      goal: { objective: "ship rpc", status: "active", tokensUsed: 0 },
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

    act(() => {
      submit?.("/goal ship rpc");
    });

    await waitFor(() => {
      expect(appMocks.SetChatGoal).toHaveBeenCalledWith({
        sessionId: 42,
        objective: "ship rpc",
        status: "active",
      });
    });
    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          permissionMode: "plan",
          sessionId: 42,
          text: "ship rpc",
        }),
      );
    });
  });

  it("/goal clear calls Codex clear goal RPC", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    appMocks.ClearChatGoal.mockResolvedValue({ cleared: true });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("/goal clear");
    });

    await waitFor(() => {
      expect(appMocks.ClearChatGoal).toHaveBeenCalledWith({ sessionId: 42 });
    });
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
  });

  it("/goal is rejected while the Codex turn is still streaming", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = makeSession({
      backendType: "codex",
      id: 42,
      permissionMode: "default",
    });
    useChatStreamsStore.getState().openStream({
      name: "chat:stream:goal-wait",
      sessionId: 42,
      assistantMessageId: 99,
      streamStartedAt: Date.now(),
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("/goal complete");
    });

    expect(
      await screen.findByText(
        "Wait for this turn to finish before changing the goal",
      ),
    ).toBeInTheDocument();
    expect(appMocks.SetChatGoal).not.toHaveBeenCalled();
    expect(appMocks.ClearChatGoal).not.toHaveBeenCalled();
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(appMocks.EnqueueChatMessage).not.toHaveBeenCalled();
  });

  it("/goal objective in a new Codex chat creates the goal session and starts a user turn", async () => {
    resetStore();
    componentMocks.capsSwitchableDuringTurn = false;
    componentMocks.capsAllowedModes = ["default", "plan"];
    mockSessionStore.session = null;
    const onSessionCreated = vi.fn();
    appMocks.StartChatGoal.mockResolvedValue({
      sessionId: 123,
      goal: { objective: "ship rpc", status: "active", tokensUsed: 0 },
    });
    appMocks.SendChatMessage.mockResolvedValue({
      assistantMessageId: 1001,
      sessionId: 123,
      stream: "chat:event:123:1001",
      userMessageId: 1000,
    });

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
        newSessionContext={{ projectId: 55 }}
        onSessionCreated={onSessionCreated}
      />,
    );
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("/goal ship rpc");
    });

    await waitFor(() => {
      expect(appMocks.StartChatGoal).toHaveBeenCalledWith({
        agentId: 7,
        projectId: 55,
        objective: "ship rpc",
        status: "active",
        permissionMode: "plan",
      });
    });
    expect(onSessionCreated).toHaveBeenCalledWith(123, 7);
    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          permissionMode: "plan",
          sessionId: 123,
          text: "ship rpc",
        }),
      );
    });
    expect(appMocks.SetChatGoal).not.toHaveBeenCalled();
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
        screen.getByText(/Send failed.*provider not configured/),
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
      expect(screen.getByText(/Send failed.*backend down/)).toBeInTheDocument();
    });
  });
});

describe("ChatPanel · launch command copy feedback", () => {
  it("Given the backend is Pi Agent, When the menu opens, Then copy launch command is available", async () => {
    const user = userEvent.setup();
    resetStore();
    mockSessionStore.session = makeSession({
      backendType: "piagent",
      id: 42,
      title: "Pi turn",
    });

    render(<ChatPanel sessionId={42} />);

    await user.click(screen.getByRole("button", { name: "More actions" }));

    expect(await screen.findByText("Copy Launch Command")).toBeInTheDocument();
  });

  it("Given the backend is built-in, When the menu opens, Then copy launch command is unavailable", async () => {
    const user = userEvent.setup();
    resetStore();
    mockSessionStore.session = makeSession({
      backendType: "builtin",
      id: 42,
      title: "Built-in turn",
    });

    render(<ChatPanel sessionId={42} />);

    await user.click(screen.getByRole("button", { name: "More actions" }));

    expect(screen.queryByText("Copy Launch Command")).not.toBeInTheDocument();
  });

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

    await user.click(screen.getByRole("button", { name: "More actions" }));
    await user.click(await screen.findByText("Copy Launch Command"));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(
        "AGENTRE_TOKEN=t agentre claudecode resume 42",
      );
    });
    expect(sonnerMocks.toast.success).toHaveBeenCalledWith(
      "Launch command copied",
      expect.objectContaining({
        description: expect.stringContaining("Includes a token"),
        duration: 5000,
        position: "bottom-right",
      }),
    );
    expect(
      screen.queryByText(/Launch command copied.*Includes a token/),
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

// ─── T26: 会话内终端 toggle 已移除 ───────────────────────────────────────────

describe("chat-panel · 终端 toggle 已移除", () => {
  it("渲染后不存在 title 含「终端」的 toggle 按钮", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 7 });

    render(<ChatPanel sessionId={7} />);

    expect(screen.queryByTitle(/终端/)).not.toBeInTheDocument();
  });

  it("⌘` 快捷键不再触发任何 terminal 动作", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 7 });

    render(<ChatPanel sessionId={7} />);
    // 触发原来的快捷键，不应抛错也不应改变任何可观测状态
    fireEvent.keyDown(window, { key: "`", metaKey: true });

    // 只要不报错且 TerminalPanel 不出现即为通过
    expect(screen.queryByTestId("terminal-panel")).not.toBeInTheDocument();
  });

  it("不渲染 TerminalPanel（终端已移至独立 tab）", () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 5 });

    render(<ChatPanel sessionId={5} />);

    expect(screen.queryByTestId("terminal-panel")).not.toBeInTheDocument();
  });
});

// ─── T29: subagent_activity_started 旁路事件 ─────────────────────────────────
// 后台 subagent 开始产生内部活动时，后端经 "chat:autonomous:<sessionId>" 推
// subagent_activity_started 事件。前端必须仅调 openStream（指向发起消息），
// 不插入新消息行，不将 session 标记为 running。
describe("ChatPanel · T29 subagent_activity_started 旁路订阅", () => {
  /**
   * 找 EventsOn 中注册在 "chat:autonomous:<sessionId>" 信道上的 handler。
   * useChatStream 调 EventsOn(stream, handler) —— 我们从 mock.calls 里找对应条目。
   */
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given a subagent_activity_started event, When it arrives on the autonomous channel, Then openStream is called with the launch message id", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 1 });

    render(<ChatPanel sessionId={1} />);

    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    expect(handler).not.toBeNull();

    act(() => {
      handler!({
        kind: "subagent_activity_started",
        stream: "chat:event:1:42",
        sessionId: 1,
        launchMessageId: 42,
        toolUseId: "toolu_agent",
      } as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    // (a) openStream was called with the launch message id and stream name
    const liveStream = streamForMessage(useChatStreamsStore.getState(), 1, 42);
    expect(liveStream).toBeDefined();
    expect(liveStream?.assistantMessageId).toBe(42);
    expect(liveStream?.name).toBe("chat:event:1:42");
  });

  it("Given a subagent_activity_started event, When it fires, Then setMessages is NOT called to add a new message row", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 1 });

    render(<ChatPanel sessionId={1} />);

    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    act(() => {
      handler!({
        kind: "subagent_activity_started",
        stream: "chat:event:1:42",
        sessionId: 1,
        launchMessageId: 42,
        toolUseId: "toolu_agent",
      } as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    // (b) setMessages must NOT be called — the launch message already exists
    expect(setMessagesSpy).not.toHaveBeenCalled();
  });

  it("Given a subagent_activity_started event, When it fires, Then the session is NOT marked running", async () => {
    resetStore();
    useSessionStatusStore.getState().__reset();
    mockSessionStore.session = makeSession({ id: 1, agentStatus: "idle" });

    render(<ChatPanel sessionId={1} />);

    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    act(() => {
      handler!({
        kind: "subagent_activity_started",
        stream: "chat:event:1:42",
        sessionId: 1,
        launchMessageId: 42,
        toolUseId: "toolu_agent",
      } as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    // (c) session must NOT be marked running — background activity keeps session idle
    const status = useSessionStatusStore.getState().statuses.get(1);
    expect(status?.agentStatus).not.toBe("running");
  });
});

// ─── T30: 后台任务完成翻转 ────────────────────────────────────────────────────
// 后台任务(run_in_background bash / subagent)的完成是跨轮的:主轮结束后,发起它的
// tool_use block 已从 liveBlocks 落进 messages。autonomous_started 携带 completedTask
// 到达时,只翻 liveBlocks(mergeSubagentMeta)翻不到那条块 —— 必须同时翻 messages,
// 否则后台任务面板胶囊 + 行内 pill 永远 spin(bug #2)。
describe("ChatPanel · T30 后台任务完成翻转 messages", () => {
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given an autonomous_started with completedTask, When it arrives, Then setMessages flips the persisted background block to completed", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 1 });
    mockSessionStore.messages = [
      {
        id: 42,
        role: "assistant",
        blocks: [
          {
            type: "tool_use",
            toolUseId: "toolu_bg",
            toolInput: { run_in_background: true },
            subagent: { kind: "local_bash", status: "running" },
          },
        ],
      },
    ];

    render(<ChatPanel sessionId={1} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    act(() => {
      handler!({
        kind: "autonomous_started",
        sessionId: 1,
        completedTask: {
          toolUseId: "toolu_bg",
          status: "completed",
          summary: "Background command finished (exit 0)",
        },
      } as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    expect(setMessagesSpy).toHaveBeenCalled();
    const updater = setMessagesSpy.mock.calls.at(-1)![0] as (
      prev: Array<Record<string, unknown>>,
    ) => Array<Record<string, unknown>>;
    const result = updater(
      mockSessionStore.messages as Array<Record<string, unknown>>,
    );
    const block = (result[0].blocks as Array<Record<string, unknown>>)[0];
    const subagent = block.subagent as Record<string, unknown>;
    expect(subagent.status).toBe("completed");
    expect(subagent.summary).toBe("Background command finished (exit 0)");
  });
});

// ─── T30b: 空闲态后台 subagent 的进度回写 messages(sess-2275)──────────────────
// 后台 subagent 在会话空闲态一直跑,CLI 每次工具调用都吐 task_progress。派遣卡的
// tool_use block 早已从 liveBlocks 落进 messages —— store 的 mergeSubagentMeta 只翻
// liveBlocks,合并落空,卡片上的工具数 / token 会一直停在派遣那一刻不动。会话级流上
// 镜像的那份 subagent_progress 必须同时合并进 messages。
describe("ChatPanel · T30b 空闲后台 subagent 进度回写", () => {
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given a session-level subagent_progress, When it arrives, Then setMessages merges the new tool count / tokens into the persisted spawn card", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 1 });
    mockSessionStore.messages = [
      {
        id: 42,
        role: "assistant",
        blocks: [
          {
            type: "tool_use",
            toolUseId: "toolu_agent",
            toolInput: { run_in_background: true },
            subagent: {
              kind: "local_agent",
              status: "running",
              toolUses: 9,
              totalTokens: 84739,
            },
          },
        ],
      },
    ];

    render(<ChatPanel sessionId={1} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    act(() => {
      handler!({
        kind: "subagent_progress",
        sessionId: 1,
        toolUseId: "toolu_agent",
        subagent: {
          toolUses: 21,
          totalTokens: 132480,
          lastToolName: "Edit",
        },
      } as unknown as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    expect(setMessagesSpy).toHaveBeenCalled();
    const updater = setMessagesSpy.mock.calls.at(-1)![0] as (
      prev: Array<Record<string, unknown>>,
    ) => Array<Record<string, unknown>>;
    const result = updater(
      mockSessionStore.messages as Array<Record<string, unknown>>,
    );
    const block = (result[0].blocks as Array<Record<string, unknown>>)[0];
    const subagent = block.subagent as Record<string, unknown>;
    expect(subagent.toolUses).toBe(21);
    expect(subagent.totalTokens).toBe(132480);
    expect(subagent.lastToolName).toBe("Edit");
    // 这一帧没带的字段保持不变
    expect(subagent.status).toBe("running");
  });
});

// ─── T30c: 空闲态后台 subagent 的模型回写 messages ────────────────────────────
// subagent_model 后端只带 toolUseId + model(不复用整份 Subagent 快照),避免浅合并
// 把已累计的 toolUses/totalTokens/status 覆盖成空值(R4)。派遣卡的 tool_use block
// 早已从 liveBlocks 落进 messages 时,会话级流上镜像的这份事件同样要合并进 messages。
describe("ChatPanel · T30c 空闲后台 subagent 模型回写", () => {
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given a session-level subagent_model, When it arrives, Then setMessages merges model into the persisted spawn card without clearing progress/status", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ id: 1 });
    mockSessionStore.messages = [
      {
        id: 42,
        role: "assistant",
        blocks: [
          {
            type: "tool_use",
            toolUseId: "toolu_agent",
            toolInput: { run_in_background: true },
            subagent: {
              kind: "local_agent",
              status: "running",
              toolUses: 9,
              totalTokens: 84739,
            },
          },
        ],
      },
    ];

    render(<ChatPanel sessionId={1} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(1);
    act(() => {
      handler!({
        kind: "subagent_model",
        sessionId: 1,
        toolUseId: "toolu_agent",
        model: "claude-haiku-4-5-20251001",
      } as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    expect(setMessagesSpy).toHaveBeenCalled();
    const updater = setMessagesSpy.mock.calls.at(-1)![0] as (
      prev: Array<Record<string, unknown>>,
    ) => Array<Record<string, unknown>>;
    const result = updater(
      mockSessionStore.messages as Array<Record<string, unknown>>,
    );
    const block = (result[0].blocks as Array<Record<string, unknown>>)[0];
    const subagent = block.subagent as Record<string, unknown>;
    expect(subagent.model).toBe("claude-haiku-4-5-20251001");
    // 已累计的进度/状态字段不得被这次纯模型更新清空
    expect(subagent.status).toBe("running");
    expect(subagent.toolUses).toBe(9);
    expect(subagent.totalTokens).toBe(84739);
  });
});

// ─── T31: 自主续轮进行中用户又发消息(sess-1950)────────────────────────────────
// 后台任务完成 → 自主续轮正在流式输出。此时用户在输入框再发一条消息:
//   1. streaming=true(自主轮已 openStream)→ 走 doEnqueue;
//   2. 后端 Steer 只认 user turn 的 inTurn 标记,自主轮不置该标记 → 返
//      ChatSteerNoActive → 前端 fallback 到 doSend 起新一轮;
//   3. doSend 拿到新 assistant 行后调 openStream —— 而 chat-streams-store 的
//      LiveStream 是 **按 sessionId 单槽位** 的,新 openStream 直接覆盖整条 entry:
//      自主续轮已经流到屏幕上、但还没落库的 liveDelta / liveBlocks 全部丢失,
//      transcript 回退到该消息的持久化态(sess-1950 里就是稀疏 checkpoint),
//      同时 ChatStreamsHost 的订阅也从自主轮流名切走,自主轮后续事件无人接收。
// 用户可见症状:「已经输出的内容清空回退」。
describe("ChatPanel · T31 自主续轮流式中再发消息", () => {
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given an autonomous turn is streaming, When the user sends a new message, Then the autonomous turn's already-streamed output is not discarded", async () => {
    resetStore();
    mockSessionStore.session = makeSession({
      id: 1950,
      backendType: "claudecode",
    });
    // 自主续轮的 assistant 行:落库时 blocks 还是空的(内容只在 liveDelta 里)。
    mockSessionStore.messages = [{ id: 13912, role: "assistant", blocks: [] }];
    // Enqueue 打到后端 → 自主轮没置 inTurn → ChatSteerNoActive(前端按文案前缀识别)。
    appMocks.EnqueueChatMessage.mockRejectedValue(
      new Error("没有进行中的对话可以插入消息"), // code.ChatSteerNoActive zh_cn 原文
    );
    appMocks.SendChatMessage.mockResolvedValue({
      sessionId: 1950,
      stream: "chat:event:1950:13914",
      assistantMessageId: 13914,
      userMessage: { id: 13913, role: "user", blocks: [] },
      assistantMessage: { id: 13914, role: "assistant", blocks: [] },
    });

    render(<ChatPanel sessionId={1950} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:1950",
        expect.any(Function),
      ),
    );

    // 自主续轮开始并流出一段文字。
    const handler = getAutonomousHandler(1950);
    act(() => {
      handler!({
        kind: "autonomous_started",
        sessionId: 1950,
        stream: "chat:event:1950:13912",
        trigger: "background_task",
        assistantMessage: { id: 13912, role: "assistant", blocks: [] },
      } as unknown as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });
    act(() => {
      useChatStreamsStore
        .getState()
        .appendLiveText(1950, 13912, "AUTONOMOUS-PARTIAL-OUTPUT");
    });
    expect(
      streamForMessage(useChatStreamsStore.getState(), 1950, 13912)?.liveDelta,
    ).toContain("AUTONOMOUS-PARTIAL-OUTPUT");

    // 用户在自主轮流式中又发一条。
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();
    await act(async () => {
      submit!("给 e2e 补一个 AI 场景 spec");
      await new Promise((r) => setTimeout(r, 0));
    });

    // 已经流到屏幕上的自主轮输出不能凭空消失。
    const live = streamForMessage(useChatStreamsStore.getState(), 1950, 13912);
    const stillRenderable =
      live?.liveDelta?.includes("AUTONOMOUS-PARTIAL-OUTPUT") ||
      JSON.stringify(live?.liveBlocks ?? []).includes(
        "AUTONOMOUS-PARTIAL-OUTPUT",
      ) ||
      JSON.stringify(mockSessionStore.messages).includes(
        "AUTONOMOUS-PARTIAL-OUTPUT",
      );
    expect(stillRenderable).toBe(true);
  });
});

// ─── T32: 自主轮/后台活动轮的 per-turn 终态被漏(sess-2146)──────────────────────
// per-turn 流的 openStream(ChatPanel 收到 autonomous_started 才调)与 EventsOn 订阅
// (ChatStreamsHost 下一 render 才挂)跨 render 解耦。短轮的 per-turn done/closed 可能
// 赶在订阅注册前发完、被 fire-and-forget 事件丢掉 → LiveStream 永远留在 store →
// streaming 卡死:输入框被逼走 doEnqueue 发不出、自主轮那条空 assistant 行也不 reload
// 回填内容(用户可见症状:「发不出消息 + 有结果也不显示」)。
// 修复:收尾在**会话级**流(ChatPanel 挂载即订阅、无此 race)补发 autonomous_finished,
// 前端据 launchMessageId 兜底 finishStream(幂等)→ streaming 解卡 + doneTick 触发 reload。
describe("ChatPanel · T32 会话级 autonomous_finished 兜底漏掉的 per-turn 终态", () => {
  function getAutonomousHandler(
    sessionId: number,
  ): ((ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void) | null {
    const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
      [string, (ev: import("@/hooks/use-chat-stream").ChatStreamEvent) => void]
    >;
    const found = calls.find(
      ([name]) => name === `chat:autonomous:${sessionId}`,
    );
    return found ? found[1] : null;
  }

  it("Given an autonomous turn's per-turn done was missed (orphan stream), When autonomous_finished arrives on the session channel, Then the stream is finished and the session reloads", async () => {
    resetStore();
    mockSessionStore.session = makeSession({
      id: 2146,
      backendType: "claudecode",
    });
    mockSessionStore.messages = [{ id: 5001, role: "assistant", blocks: [] }];

    render(<ChatPanel sessionId={2146} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:2146",
        expect.any(Function),
      ),
    );

    const handler = getAutonomousHandler(2146);
    expect(handler).toBeTruthy();

    // 自主轮开始:插入 assistant 行 + openStream。随后模拟「per-turn done 被漏掉」——
    // 本测试没挂 ChatStreamsHost,per-turn 流根本没有订阅者,done 发到虚空,orphan 成立。
    act(() => {
      handler!({
        kind: "autonomous_started",
        sessionId: 2146,
        stream: "chat:event:2146:5001",
        trigger: "background_task",
        assistantMessage: { id: 5001, role: "assistant", blocks: [] },
      } as unknown as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });
    // orphan 流在 store 里 → streaming=true(输入框被卡)。
    expect(
      streamForMessage(useChatStreamsStore.getState(), 2146, 5001),
    ).toBeTruthy();

    // 初次挂载的 reload 不算数,清掉,只断言兜底触发的那次。
    reloadSpy.mockClear();

    // 会话级流补发终态兜底。
    act(() => {
      handler!({
        kind: "autonomous_finished",
        sessionId: 2146,
        launchMessageId: 5001,
      } as unknown as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    // orphan 流被 finish → streaming 解卡。
    expect(
      streamForMessage(useChatStreamsStore.getState(), 2146, 5001),
    ).toBeNull();
    // doneTick 自增 → ChatPanel 兜底 reload,把空 assistant 行回填成落库的最终内容。
    await waitFor(() => expect(reloadSpy).toHaveBeenCalled());
  });

  it("Given the per-turn done was already received (stream gone), When autonomous_finished arrives, Then it is a no-op (idempotent, no extra reload)", async () => {
    resetStore();
    mockSessionStore.session = makeSession({
      id: 2146,
      backendType: "claudecode",
    });
    mockSessionStore.messages = [{ id: 5001, role: "assistant", blocks: [] }];

    render(<ChatPanel sessionId={2146} />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:autonomous:2146",
        expect.any(Function),
      ),
    );
    const handler = getAutonomousHandler(2146);

    // 没有任何 orphan 流(per-turn done 正常收到、流已被移除的场景)。
    reloadSpy.mockClear();
    act(() => {
      handler!({
        kind: "autonomous_finished",
        sessionId: 2146,
        launchMessageId: 5001,
      } as unknown as import("@/hooks/use-chat-stream").ChatStreamEvent);
    });

    expect(
      streamForMessage(useChatStreamsStore.getState(), 2146, 5001),
    ).toBeNull();
    // 流本就不在 → 不 finishStream → 不额外 reload。
    expect(reloadSpy).not.toHaveBeenCalled();
  });
});

describe("ChatPanel · /new 斜杠命令", () => {
  it("exact /new 在新 tab 中开同 agent+项目的空白会话并跳转,不动当前会话", () => {
    resetStore();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    mockSessionStore.session = makeSession({
      backendType: "claudecode",
      id: 42,
      agentId: 7,
      projectId: 3,
      permissionMode: "default",
    });

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("/new");
    });

    const state = useChatTabsStore.getState();
    expect(state.tabs).toHaveLength(1);
    expect(state.tabs[0].meta).toMatchObject({
      kind: "new",
      agentId: 7,
      projectId: 3,
    });
    expect(state.activeTabId).toBe(state.tabs[0].id);
    // 当前会话完全不受影响:既不发消息,也不压缩。
    expect(appMocks.SendChatMessage).not.toHaveBeenCalled();
    expect(appMocks.CompactChatSession).not.toHaveBeenCalled();
  });
});

// ─── 停止「重启遗孤」会话:Stop 成功后主动 reload 把死按钮收回去 ──────────────────

describe("ChatPanel · 停止重启遗孤会话", () => {
  it("Given DB 卡在 running 但本地无活跃 stream, When 点停止且后端 reconcile 成功, Then 主动 reload 收回按钮", async () => {
    resetStore();
    // 重启遗孤:turn goroutine 早死了,DB agent_status 还停在 running,本地 store 无 stream。
    // 后端 Stop 会把它 reconcile 回 idle 并返 stopped:true;但这类会话没有活跃 stream
    // 不会推 aborted 事件,前端必须主动 reload 才能让「停止」按钮回灰。
    appMocks.StopChatMessage.mockResolvedValue({ stopped: true });
    mockSessionStore.session = makeSession({ id: 42, agentStatus: "running" });

    render(<ChatPanel sessionId={42} />);

    const stopBtn = screen.getByRole("button", { name: "Stop" });
    expect(stopBtn).toBeEnabled();

    // 只断言点击后那一次 reload,排除 mount 期可能的 reload 干扰。
    reloadSpy.mockClear();
    fireEvent.click(stopBtn);

    await waitFor(() => {
      expect(appMocks.StopChatMessage).toHaveBeenCalledWith({ sessionId: 42 });
    });
    await waitFor(() => {
      expect(reloadSpy).toHaveBeenCalled();
    });
  });
});

// ─── notice 错误详情:后端 cause 拆分渲染 ─────────────────────────────────────

describe("ChatPanel · notice 错误详情", () => {
  it("Given 后端错误带 cause, When 发送失败, Then 详情块渲染 cause 且可选中复制", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ backendType: "builtin", id: 42 });
    appMocks.SendChatMessage.mockRejectedValue(
      new Error(
        "操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
      ),
    );

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("hi");
    });

    const detail = await screen.findByTestId("notice-detail");
    expect(detail).toHaveTextContent(
      "SQL logic error: table chat_sessions has no column named run_id (1)",
    );
    // 可选中复制(globals.css 全局 user-select:none 的 opt-in),而不是加复制按钮
    expect(detail).toHaveAttribute("data-selectable-text", "true");
  });

  // Wails 真实形状:dispatcher 写 callbackMessage.Err = err.Error(),runtime 的 Callback 再
  // reject(message.error) —— reject 的是**裸字符串**,不是 Error 对象。上一个用例沿用了本文件
  // 既有的 new Error(...) 惯例,但那个形状生产环境不会出现;这里锁死真实形状也能拆出详情。
  it("Given Wails 以裸字符串 reject(生产真实形状), When 发送失败, Then 详情块照样渲染", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ backendType: "builtin", id: 42 });
    appMocks.SendChatMessage.mockRejectedValue(
      "操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
    );

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("hi");
    });

    const detail = await screen.findByTestId("notice-detail");
    expect(detail).toHaveTextContent(
      "SQL logic error: table chat_sessions has no column named run_id (1)",
    );
  });

  it("Given 后端错误无 cause, When 发送失败, Then 不渲染详情块", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ backendType: "builtin", id: 42 });
    appMocks.SendChatMessage.mockRejectedValue(new Error("操作失败"));

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("hi");
    });

    // 先等 notice 出现,再断言详情块不存在 —— 否则可能在 notice 渲染前就通过了。
    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByTestId("notice-detail")).toBeNull();
    });
  });
});
