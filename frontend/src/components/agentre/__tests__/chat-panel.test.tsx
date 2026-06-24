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

vi.mock("@/hooks/use-chat-session", () => ({
  useChatSession: () => ({
    session: mockSessionStore.session,
    messages: mockSessionStore.messages,
    loading: false,
    error: null,
    reload: () => Promise.resolve(),
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
import { useChatStreamsStore } from "@/stores/chat-streams-store";

/** 清 store streams 以避免测试间串台 */
function resetStore() {
  __resetChatPanelScrollStateForTesting();
  mockSessionStore.messages = [];
  useChatStreamsStore.getState().streams.clear();
  runtimeMocks.EventsOn.mockReset();
  runtimeMocks.EventsOn.mockImplementation(() => vi.fn());
  setMessagesSpy.mockClear();
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
      expect(useChatStreamsStore.getState().streams.get(42)?.name).toBe(
        "chat:event:42:1001",
      );
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
    const liveStream = useChatStreamsStore.getState().streams.get(1);
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
