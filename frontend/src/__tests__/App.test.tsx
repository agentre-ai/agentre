import {
  act,
  createEvent,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "../App";

const themeStorageKey = "agentre.theme";
const windowSizeStorageKey = "agentre.windowSize";
const lastPathStorageKey = "agentre.lastPath";
const themeLabelByValue: Record<"system" | "light" | "dark", string> = {
  dark: "深色",
  light: "浅色",
  system: "跟随系统",
};
let restoreMatchMedia: (() => void) | undefined;

async function selectThemeOption(
  user: ReturnType<typeof userEvent.setup>,
  trigger: HTMLElement,
  value: "system" | "light" | "dark",
) {
  await user.click(trigger);
  const option = await screen.findByRole("option", {
    name: themeLabelByValue[value],
  });
  await user.click(option);
}

type MockWailsRuntimeOptions = {
  fullscreen?: boolean;
  platform?: string;
  size?: { h: number; w: number };
};

function expectHorizontalTableScroll(table: HTMLElement) {
  const tableContainer = table.closest("[data-slot='table-container']");
  const panel = table.closest("section");
  const settingsPage = table.closest("[data-slot='settings-page']");

  if (!(tableContainer instanceof HTMLElement)) {
    throw new Error("Expected table to render inside a table container");
  }

  if (!(panel instanceof HTMLElement)) {
    throw new Error("Expected table to render inside a panel");
  }

  if (!(settingsPage instanceof HTMLElement)) {
    throw new Error("Expected table to render inside the settings page");
  }

  expect(settingsPage).toHaveClass("min-w-0");
  expect(panel).toHaveClass("min-w-0");
  expect(tableContainer).toHaveClass("min-w-0", "overflow-x-auto");
}

function fireSelectAllKey(
  target: Document | HTMLElement,
  modifier: "ctrl" | "meta" = "meta",
) {
  const event = createEvent.keyDown(target, {
    bubbles: true,
    cancelable: true,
    code: "KeyA",
    ctrlKey: modifier === "ctrl",
    key: "a",
    metaKey: modifier === "meta",
  });

  Object.defineProperty(event, "ctrlKey", {
    configurable: true,
    value: modifier === "ctrl",
  });
  Object.defineProperty(event, "metaKey", {
    configurable: true,
    value: modifier === "meta",
  });

  fireEvent(target, event);

  return event;
}

function mockSystemColorScheme(initialDark = false) {
  const originalMatchMedia = window.matchMedia;
  const listeners = new Set<EventListenerOrEventListenerObject>();
  const mediaQueryList = {
    matches: initialDark,
    media: "(prefers-color-scheme: dark)",
    onchange: null,
    addEventListener: vi.fn(
      (_event: string, listener: EventListenerOrEventListenerObject) => {
        listeners.add(listener);
      },
    ),
    removeEventListener: vi.fn(
      (_event: string, listener: EventListenerOrEventListenerObject) => {
        listeners.delete(listener);
      },
    ),
    addListener: vi.fn((listener: EventListenerOrEventListenerObject) => {
      listeners.add(listener);
    }),
    removeListener: vi.fn((listener: EventListenerOrEventListenerObject) => {
      listeners.delete(listener);
    }),
    dispatchEvent: vi.fn(() => true),
  } as unknown as MediaQueryList;

  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn(() => mediaQueryList),
  });

  restoreMatchMedia = () => {
    if (originalMatchMedia) {
      Object.defineProperty(window, "matchMedia", {
        configurable: true,
        value: originalMatchMedia,
      });
    } else {
      Reflect.deleteProperty(window, "matchMedia");
    }
  };

  return {
    setDark(dark: boolean) {
      Object.defineProperty(mediaQueryList, "matches", {
        configurable: true,
        value: dark,
      });
      const event = {
        matches: dark,
        media: mediaQueryList.media,
      } as MediaQueryListEvent;

      listeners.forEach((listener) => {
        if (typeof listener === "function") {
          listener(event);
        } else {
          listener.handleEvent(event);
        }
      });
    },
  };
}

function mockDesktopViewport() {
  const originalMatchMedia = window.matchMedia;
  const listenersByQuery = new Map<
    string,
    Set<EventListenerOrEventListenerObject>
  >();

  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn((query: string) => {
      const listeners = new Set<EventListenerOrEventListenerObject>();
      listenersByQuery.set(query, listeners);

      return {
        matches: query.includes("min-width: 1024px"),
        media: query,
        onchange: null,
        addEventListener: vi.fn(
          (_event: string, listener: EventListenerOrEventListenerObject) => {
            listeners.add(listener);
          },
        ),
        removeEventListener: vi.fn(
          (_event: string, listener: EventListenerOrEventListenerObject) => {
            listeners.delete(listener);
          },
        ),
        addListener: vi.fn((listener: EventListenerOrEventListenerObject) => {
          listeners.add(listener);
        }),
        removeListener: vi.fn(
          (listener: EventListenerOrEventListenerObject) => {
            listeners.delete(listener);
          },
        ),
        dispatchEvent: vi.fn(() => true),
      } as unknown as MediaQueryList;
    }),
  });

  restoreMatchMedia = () => {
    listenersByQuery.clear();

    if (originalMatchMedia) {
      Object.defineProperty(window, "matchMedia", {
        configurable: true,
        value: originalMatchMedia,
      });
    } else {
      Reflect.deleteProperty(window, "matchMedia");
    }
  };
}

function mockLlmProviders() {
  const existing =
    (window as unknown as { go?: { app?: { App?: Record<string, unknown> } } })
      .go?.app?.App ?? {};
  Object.defineProperty(window, "go", {
    configurable: true,
    value: {
      app: {
        App: {
          ...existing,
          ListLLMProviders: vi.fn(() =>
            Promise.resolve({
              items: [
                {
                  baseUrl: "",
                  createtime: 0,
                  hasApiKey: true,
                  id: 1,
                  maskedApiKey: "sk-ant-•••••••••••••• xJ12",
                  model: "claude-sonnet-4-6",
                  name: "Production",
                  type: "anthropic",
                  updatetime: 0,
                },
                {
                  baseUrl: "http://localhost:11434/v1",
                  createtime: 0,
                  hasApiKey: false,
                  id: 2,
                  maskedApiKey: "",
                  model: "llama3.2",
                  name: "Ollama 本机",
                  type: "openai-chat",
                  updatetime: 0,
                },
              ],
            }),
          ),
          TestLLMProvider: vi.fn(() =>
            Promise.resolve({
              message: "模型调用成功",
              modelCount: 0,
              ok: true,
            }),
          ),
        },
      },
    },
  });
}

function mockOrgData() {
  const existing =
    (window as unknown as { go?: { app?: { App?: Record<string, unknown> } } })
      .go?.app?.App ?? {};
  Object.defineProperty(window, "go", {
    configurable: true,
    value: {
      app: {
        App: {
          ...existing,
          ListAgentBackends: vi.fn(() => Promise.resolve({ items: [] })),
          LoadOrg: vi.fn(() =>
            Promise.resolve({
              departments: [
                {
                  id: 1,
                  name: "工程部",
                  description: "",
                  icon: "hammer",
                  accentColor: "agent-2",
                  parentId: 0,
                  leadAgentId: 0,
                  leadAgentName: "",
                  sortOrder: 1,
                  directAgentCount: 1,
                  subdepartmentCount: 0,
                  memberCount: 1,
                  createtime: 0,
                  updatetime: 0,
                },
              ],
              agents: [
                {
                  id: 1,
                  name: "CEO 助手",
                  description: "默认入口",
                  avatarColor: "agent-1",
                  avatarDataUrl: "",
                  systemBadge: "DEFAULT",
                  departmentId: 0,
                  departmentName: "",
                  agentBackendId: 0,
                  sortOrder: 0,
                  prompt: [],
                  skills: [],
                  createtime: 0,
                  updatetime: 0,
                },
                {
                  id: 2,
                  name: "Eva",
                  description: "工程总监",
                  avatarColor: "agent-2",
                  avatarDataUrl: "",
                  systemBadge: "",
                  departmentId: 1,
                  departmentName: "工程部",
                  agentBackendId: 0,
                  sortOrder: 1,
                  prompt: [],
                  skills: [],
                  createtime: 0,
                  updatetime: 0,
                },
              ],
            }),
          ),
        },
      },
    },
  });
}

function mockAgentBackends() {
  const existing =
    (window as unknown as { go?: { app?: { App?: Record<string, unknown> } } })
      .go?.app?.App ?? {};
  Object.defineProperty(window, "go", {
    configurable: true,
    value: {
      app: {
        App: {
          ...existing,
          ListAgentBackends: vi.fn(() =>
            Promise.resolve({
              items: [
                {
                  id: 1,
                  type: "builtin",
                  name: "默认助手",
                  llmProviderId: 1,
                  llmProviderName: "Anthropic",
                  llmProviderType: "anthropic",
                  llmProviderModel: "sonnet-4-6",
                  llmProviderActive: true,
                  cliPath: "",
                  createtime: 0,
                  updatetime: 0,
                },
                {
                  id: 2,
                  type: "builtin",
                  name: "AWS Bedrock",
                  llmProviderId: 2,
                  llmProviderName: "AWS Bedrock",
                  llmProviderType: "anthropic",
                  llmProviderModel: "sonnet-4-6",
                  llmProviderActive: true,
                  cliPath: "",
                  createtime: 0,
                  updatetime: 0,
                },
              ],
            }),
          ),
        },
      },
    },
  });
}

function mockHooks(
  options: {
    sourceConfig?: Record<string, unknown>;
    sourcePatch?: Record<string, unknown>;
  } = {},
) {
  const existing =
    (window as unknown as { go?: { app?: { App?: Record<string, unknown> } } })
      .go?.app?.App ?? {};
  const source = {
    id: 2,
    kind: "github",
    name: "agentre-bot",
    description: "GitHub Webhook",
    identifier: "agentre-frame",
    config: {
      webhookUrl: "https://agentre.local/hooks/g-RuP8X3kQwLm2N",
      secret: "••••••••",
      verifySignature: true,
      events: ["pull_request", "issues", "push", "release"],
      imapServer: "",
      imapPort: 993,
      imapMailbox: "INBOX",
      useTls: true,
      emailAddress: "",
      appPassword: "",
      pollingInterval: "",
      lastUid: 0,
      uidValidity: 0,
      botToken: "",
      channel: "",
      cronExpr: "",
      timezone: "",
      systemPermission: "",
    },
    enabled: true,
    connectionStatus: "connected",
    lastSyncTime: 1778934000,
    totalCount: 1284,
    createtime: 0,
    updatetime: 0,
  };
  const event = {
    id: 100,
    sourceId: 2,
    sourceName: "agentre-bot",
    title: "PR #142 修复 OAuth 回调",
    sourceRef: "agentre-frame",
    sender: "wangyizhi",
    eventType: "pr.opened",
    eventStatus: "dispatched",
    payloadJson: '{"action":"opened","number":142}',
    matchedRules: [
      {
        ruleId: 1,
        ruleName: "PR opened / review",
        matched: true,
        reason: 'event_type contains "pr"',
        agentId: 1,
        agentName: "CEO 助手",
      },
    ],
    dispatches: [
      {
        agentId: 1,
        agentName: "CEO 助手",
        sessionId: "s-142",
        status: "queued",
        message: "Agent runtime dispatch is not enabled yet.",
      },
    ],
    matchedRuleNames: ["PR opened / review"],
    targetAgentNames: ["CEO 助手"],
    receivedAt: 1778934120,
    createtime: 0,
    updatetime: 0,
  };
  const otherSourceEvent = {
    ...event,
    id: 200,
    sourceId: 99,
    sourceName: "n8n 自动化",
    title: "n8n deploy_webhook failed",
    eventStatus: "failed",
    matchedRules: [],
    dispatches: [],
    matchedRuleNames: [],
    targetAgentNames: [],
  };
  const loadedSource = {
    ...source,
    ...options.sourcePatch,
    config: options.sourceConfig
      ? { ...source.config, ...options.sourceConfig }
      : source.config,
  };
  const app = {
    ...existing,
    LoadHooks: vi.fn(() =>
      Promise.resolve({
        sources: [loadedSource],
        rules: [
          {
            id: 1,
            sourceId: 2,
            name: "PR opened / review",
            conditionExpr: 'event_type contains "pr"',
            targetAgentId: 1,
            targetAgentName: "CEO 助手",
            enabled: true,
            isFallback: false,
            sortOrder: 1,
            createtime: 0,
            updatetime: 0,
          },
          {
            id: 4,
            sourceId: 2,
            name: "兜底规则",
            conditionExpr: "未命中任何规则",
            targetAgentId: 1,
            targetAgentName: "CEO 助手",
            enabled: true,
            isFallback: true,
            sortOrder: 9999,
            createtime: 0,
            updatetime: 0,
          },
        ],
        events: [event, otherSourceEvent],
        agents: [
          {
            id: 1,
            name: "CEO 助手",
            avatarColor: "agent-1",
            systemBadge: "DEFAULT",
            departmentId: 0,
          },
        ],
      }),
    ),
    UpdateHookSource: vi.fn((req) =>
      Promise.resolve({
        item: {
          ...loadedSource,
          ...req,
          connectionStatus: source.connectionStatus,
          totalCount: source.totalCount,
        },
      }),
    ),
    TestHookSource: vi.fn(() =>
      Promise.resolve({
        item: { ...loadedSource, totalCount: 1285 },
        event: {
          ...event,
          id: 101,
          title: "连接测试 · agentre-bot",
          eventType: "connection_test",
        },
      }),
    ),
    SyncHookEmailSource: vi.fn(() =>
      Promise.resolve({
        item: {
          ...loadedSource,
          connectionStatus: "connected",
          lastSyncTime: 1778934300,
          totalCount: 1285,
          config: { ...loadedSource.config, lastUid: 42 },
        },
        events: [
          {
            ...event,
            id: 102,
            sourceId: loadedSource.id,
            sourceName: loadedSource.name,
            title: "Invoice approved",
            sourceRef: "message-42@example.com",
            sender: "Alice <alice@example.com>",
            eventType: "email.received",
            payloadJson:
              '{"type":"email.received","subject":"Invoice approved"}',
            receivedAt: 1778934300,
          },
        ],
        created: 1,
        skipped: 0,
      }),
    ),
    RedeliverHookEvent: vi.fn((req) =>
      Promise.resolve({
        item: {
          ...event,
          dispatches: [
            ...event.dispatches,
            {
              agentId: req.targetAgentId || 1,
              agentName: "CEO 助手",
              sessionId: "pending-100",
              status: "queued",
              message: "Agent runtime dispatch is not enabled yet.",
            },
          ],
        },
      }),
    ),
    CreateHookSource: vi.fn((req) =>
      Promise.resolve({ item: { ...loadedSource, ...req, id: 3 } }),
    ),
    DeleteHookSource: vi.fn(() => Promise.resolve({})),
    CreateHookRule: vi.fn((req) =>
      Promise.resolve({
        item: {
          id: 9,
          ...req,
          targetAgentName: "CEO 助手",
          isFallback: false,
          sortOrder: 2,
          createtime: 0,
          updatetime: 0,
        },
      }),
    ),
    UpdateHookRule: vi.fn((req) =>
      Promise.resolve({
        item: {
          id: req.id,
          sourceId: 2,
          name: req.name,
          conditionExpr: req.conditionExpr,
          targetAgentId: req.targetAgentId,
          targetAgentName: "CEO 助手",
          enabled: req.enabled,
          isFallback: req.id === 4,
          sortOrder: req.id === 4 ? 9999 : 1,
          createtime: 0,
          updatetime: 0,
        },
      }),
    ),
    DeleteHookRule: vi.fn(() => Promise.resolve({})),
  };

  Object.defineProperty(window, "go", {
    configurable: true,
    value: {
      app: {
        App: app,
      },
    },
  });

  return app;
}

function mockWailsRuntime({
  fullscreen = false,
  platform = "darwin",
  size = { h: 768, w: 1024 },
}: MockWailsRuntimeOptions = {}) {
  const runtime = {
    Environment: vi.fn(() =>
      Promise.resolve({
        arch: "arm64",
        buildType: "dev",
        platform,
      }),
    ),
    WindowGetSize: vi.fn(() => Promise.resolve(size)),
    WindowCenter: vi.fn(),
    WindowIsFullscreen: vi.fn(() => Promise.resolve(fullscreen)),
    WindowSetSize: vi.fn(),
    WindowShow: vi.fn(),
  };

  Object.defineProperty(window, "runtime", {
    configurable: true,
    value: runtime,
  });

  return runtime;
}

afterEach(() => {
  restoreMatchMedia?.();
  restoreMatchMedia = undefined;
  Reflect.deleteProperty(window, "go");
  Reflect.deleteProperty(window, "runtime");
  vi.useRealTimers();
});

describe("App", () => {
  it("boots into the chat page and surfaces settings from the rail", async () => {
    const user = userEvent.setup();

    render(<App />);

    expect(screen.getByText("Agentre")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "对话" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "设置" })).not.toHaveAttribute(
      "aria-current",
    );

    await user.click(screen.getByRole("button", { name: "设置" }));

    expect(screen.getByRole("button", { name: "设置" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("heading", { name: "外观" })).toBeInTheDocument();
    expect(
      screen.getByText(
        "调整 Agentre 的界面显示方式。主题偏好会保存在当前设备。",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "外观" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      screen.getByRole("complementary", { name: "设置导航" }),
    ).not.toHaveClass("hidden");
    expect(
      screen.getByRole("combobox", { name: "主题模式" }),
    ).toBeInTheDocument();
  });

  it("restores the last opened page from localStorage on startup", async () => {
    localStorage.setItem(lastPathStorageKey, "/projects");

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "项目" })).toHaveAttribute(
        "aria-current",
        "page",
      );
    });
    expect(screen.getByRole("button", { name: "对话" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("falls back to the chat page when the stored last path is unknown", async () => {
    localStorage.setItem(lastPathStorageKey, "/does-not-exist");

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "对话" })).toHaveAttribute(
        "aria-current",
        "page",
      );
    });
  });

  it("persists the current page to localStorage when navigating", async () => {
    const user = userEvent.setup();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    await waitFor(() => {
      expect(localStorage.getItem(lastPathStorageKey)).toBe("/settings");
    });

    await user.click(screen.getByRole("button", { name: "对话" }));

    await waitFor(() => {
      expect(localStorage.getItem(lastPathStorageKey)).toBe("/chat");
    });
  });

  it("keeps the theme toggle directly above settings in the left rail", () => {
    render(<App />);

    const navRail = screen.getByRole("complementary", { name: "主导航" });
    const settingsButton = within(navRail).getByRole("button", {
      name: "设置",
    });
    const themeToggle = within(navRail).getByRole("button", {
      name: /切换主题/,
    });

    expect(navRail).toHaveClass("w-14", "px-2");
    expect(settingsButton).toHaveClass("size-10");
    expect(themeToggle).toHaveClass("size-10");
    expect(themeToggle).toHaveClass("mt-auto");
    expect(settingsButton).not.toHaveClass("mt-auto");
    expect(Array.from(navRail.children).at(-2)).toBe(themeToggle);
    expect(Array.from(navRail.children).at(-1)).toBe(settingsButton);
  });

  it("restores the saved Wails window size before showing the hidden startup window", async () => {
    const runtime = mockWailsRuntime();

    localStorage.setItem(
      windowSizeStorageKey,
      JSON.stringify({ height: 720, width: 1120 }),
    );

    render(<App />);

    await waitFor(() => {
      expect(runtime.WindowSetSize).toHaveBeenCalledWith(1120, 720);
      expect(runtime.WindowCenter).toHaveBeenCalled();
      expect(runtime.WindowShow).toHaveBeenCalled();
    });

    expect(runtime.WindowSetSize.mock.invocationCallOrder[0]).toBeLessThan(
      runtime.WindowCenter.mock.invocationCallOrder[0],
    );
    expect(runtime.WindowCenter.mock.invocationCallOrder[0]).toBeLessThan(
      runtime.WindowShow.mock.invocationCallOrder[0],
    );
  });

  it("stores the normal Wails window size after resize", async () => {
    const runtime = mockWailsRuntime({ size: { h: 760, w: 1180 } });

    render(<App />);

    fireEvent(window, new Event("resize"));

    await waitFor(() => {
      expect(runtime.WindowGetSize).toHaveBeenCalled();
      expect(localStorage.getItem(windowSizeStorageKey)).toBe(
        JSON.stringify({ height: 760, width: 1180 }),
      );
    });
  });

  it("stores the current Wails window size after maximized resize", async () => {
    const runtime = mockWailsRuntime({
      size: { h: 900, w: 1600 },
    });

    localStorage.setItem(
      windowSizeStorageKey,
      JSON.stringify({ height: 760, width: 1180 }),
    );

    render(<App />);

    fireEvent(window, new Event("resize"));

    await waitFor(() => {
      expect(runtime.WindowGetSize).toHaveBeenCalled();
      expect(localStorage.getItem(windowSizeStorageKey)).toBe(
        JSON.stringify({ height: 900, width: 1600 }),
      );
    });
  });

  it("does not overwrite the saved window size while fullscreen", async () => {
    const runtime = mockWailsRuntime({
      fullscreen: true,
      size: { h: 900, w: 1600 },
    });

    localStorage.setItem(
      windowSizeStorageKey,
      JSON.stringify({ height: 760, width: 1180 }),
    );

    render(<App />);

    fireEvent(window, new Event("resize"));

    await waitFor(() => {
      expect(runtime.WindowIsFullscreen).toHaveBeenCalled();
      expect(runtime.WindowGetSize).not.toHaveBeenCalled();
      expect(localStorage.getItem(windowSizeStorageKey)).toBe(
        JSON.stringify({ height: 760, width: 1180 }),
      );
    });
  });

  it("uses Command for global select-all on darwin while preserving editable fields", async () => {
    const user = userEvent.setup();
    const runtime = mockWailsRuntime({ platform: "darwin" });

    render(<App />);

    await waitFor(() => {
      expect(runtime.Environment).toHaveBeenCalled();
      expect(screen.getByText("⌘P")).toBeInTheDocument();
    });

    const appChrome = screen.getByText("Agentre").closest("header");

    if (!(appChrome instanceof HTMLElement)) {
      throw new Error("Expected Agentre to render inside the app chrome");
    }

    const ctrlEvent = fireSelectAllKey(appChrome, "ctrl");
    const metaEvent = fireSelectAllKey(appChrome, "meta");

    expect(ctrlEvent.defaultPrevented).toBe(false);
    expect(metaEvent.defaultPrevented).toBe(true);

    await user.click(screen.getByRole("button", { name: "对话" }));

    const textareaEvent = fireSelectAllKey(
      screen.getByPlaceholderText("搜索 Agent / 会话"),
      "meta",
    );

    expect(textareaEvent.defaultPrevented).toBe(false);
  });

  it("uses Ctrl for global select-all on windows", async () => {
    const runtime = mockWailsRuntime({ platform: "windows" });

    render(<App />);

    await waitFor(() => {
      expect(runtime.Environment).toHaveBeenCalled();
    });

    const appChrome = screen.getByText("Agentre").closest("header");

    if (!(appChrome instanceof HTMLElement)) {
      throw new Error("Expected Agentre to render inside the app chrome");
    }

    const metaEvent = fireSelectAllKey(appChrome, "meta");
    const ctrlEvent = fireSelectAllKey(appChrome, "ctrl");

    expect(metaEvent.defaultPrevented).toBe(false);
    expect(ctrlEvent.defaultPrevented).toBe(true);
  });

  it("switches between implemented pages from the left rail", async () => {
    const user = userEvent.setup();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "对话" }));

    expect(screen.getByRole("button", { name: "对话" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "设置" })).not.toHaveAttribute(
      "aria-current",
    );
    expect(
      screen.getByRole("complementary", { name: "Agent 列表" }),
    ).toHaveStyle({ width: "320px" });
    expect(
      screen.getByPlaceholderText("搜索 Agent / 会话"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("选一个 Agent 或项目下的会话开始"),
    ).toBeInTheDocument();
    // TabStrip + ChatPanelHost right pane is visible on /chat
    expect(
      document.querySelector('[data-page-has-chat="true"]'),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Agent 后端" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "设置" }));

    expect(screen.getByRole("button", { name: "设置" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "对话" })).not.toHaveAttribute(
      "aria-current",
    );
    expect(screen.getByRole("heading", { name: "外观" })).toBeInTheDocument();
  });

  it("opens the implemented Issues workspace from the left rail", async () => {
    const user = userEvent.setup();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Issues" }));

    const main = screen.getByRole("main");

    expect(screen.getByRole("button", { name: "Issues" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(main).getByRole("heading", { name: "看板" }),
    ).toBeInTheDocument();
    expect(
      within(main).getByText("12 个 Open · 47 个 Closed · 3 个 Agent 在跟进"),
    ).toBeInTheDocument();
    expect(
      within(main).getByRole("button", { name: "新建 Issue" }),
    ).toBeInTheDocument();
    expect(within(main).getByText("作者")).toBeInTheDocument();
    expect(within(main).getByText("分派 Agent")).toBeInTheDocument();
    expect(
      within(main).getByText("修复 OAuth 回调在 Safari 下丢失 state 参数"),
    ).toBeInTheDocument();
    expect(within(main).getByText("#142")).toBeInTheDocument();
    expect(within(main).queryByText("建设中")).not.toBeInTheDocument();

    await user.click(within(main).getByRole("button", { name: "Board" }));

    expect(
      within(main).getByText("按状态分列 · 拖卡片可在列间流转"),
    ).toBeInTheDocument();
    expect(
      within(main).getByRole("heading", { name: "待派发" }),
    ).toBeInTheDocument();
    expect(
      within(main).getByRole("heading", { name: "进行中" }),
    ).toBeInTheDocument();
    expect(
      within(main).getByRole("heading", { name: "待审批" }),
    ).toBeInTheDocument();
    expect(
      within(main).getByRole("heading", { name: "已关闭" }),
    ).toBeInTheDocument();
  });

  it("opens the implemented Hooks workspace from the left rail", async () => {
    const user = userEvent.setup();
    mockHooks();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Hooks" }));

    expect(screen.getByRole("button", { name: "Hooks" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      await screen.findByRole("complementary", { name: "信号源列表" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "agentre-bot" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("建设中")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Webhook URL")).toHaveDisplayValue(
      "https://agentre.local/hooks/g-RuP8X3kQwLm2N",
    );
    expect(screen.getByText("PR opened / review")).toBeInTheDocument();
  });

  it("opens Hooks when Wails returns a source config with null event list", async () => {
    const user = userEvent.setup();
    mockHooks({ sourceConfig: { events: null } });

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Hooks" }));

    expect(
      await screen.findByRole("heading", { name: "agentre-bot" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("监听事件")).toHaveDisplayValue("");
  });

  it("saves Hook source config and writes a test event to the log", async () => {
    const user = userEvent.setup();
    const appBridge = mockHooks();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Hooks" }));
    const nameInput = await screen.findByLabelText("信号源名称");

    await user.clear(nameInput);
    await user.type(nameInput, "agentre-prod");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    await waitFor(() => {
      expect(appBridge.UpdateHookSource).toHaveBeenCalledWith(
        expect.objectContaining({ id: 2, name: "agentre-prod" }),
      );
    });

    await user.click(screen.getByRole("button", { name: "测试连接" }));

    await waitFor(() => {
      expect(appBridge.TestHookSource).toHaveBeenCalledWith({ id: 2 });
    });
    expect(
      (await screen.findAllByText("连接测试 · agentre-bot")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /事件日志/ })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("syncs a real email Hook source into the event log", async () => {
    const user = userEvent.setup();
    const appBridge = mockHooks({
      sourcePatch: {
        kind: "email",
        name: "工作邮箱",
        description: "IMAP inbox",
        identifier: "ops@example.com",
      },
      sourceConfig: {
        imapServer: "imap.example.com",
        imapPort: 993,
        imapMailbox: "INBOX",
        useTls: true,
        emailAddress: "ops@example.com",
        appPassword: "secret",
        pollingInterval: "5m",
      },
    });

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Hooks" }));
    expect(await screen.findByLabelText("IMAP 服务器")).toHaveDisplayValue(
      "imap.example.com",
    );

    await user.click(screen.getByRole("button", { name: "更多操作" }));
    await user.click(screen.getByRole("button", { name: "同步邮箱" }));

    await waitFor(() => {
      expect(appBridge.SyncHookEmailSource).toHaveBeenCalledWith({
        id: 2,
        limit: 20,
      });
    });
    expect(
      (await screen.findAllByText("Invoice approved")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /事件日志/ })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("redelivers a Hook event without starting the Agent runtime", async () => {
    const user = userEvent.setup();
    const appBridge = mockHooks();

    render(<App />);

    await user.click(screen.getByRole("button", { name: "Hooks" }));
    await user.click(await screen.findByRole("button", { name: /事件日志/ }));
    expect(
      screen.getByRole("button", { name: /全部\s+1/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /失败\s+0/ }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重新派发" }));

    await waitFor(() => {
      expect(appBridge.RedeliverHookEvent).toHaveBeenCalledWith({
        id: 100,
        targetAgentId: 0,
      });
    });
    expect(await screen.findByText("已记录重新派发请求")).toBeInTheDocument();
  });

  it("loads and lists departments + agents on the organization page", async () => {
    const user = userEvent.setup();
    mockOrgData();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "组织" }));

    // wait for LoadOrg promise to resolve
    await waitFor(() => {
      expect(screen.queryByText("正在加载组织架构…")).not.toBeInTheDocument();
    });

    // department row from the mock
    expect(screen.getByText("工程部")).toBeInTheDocument();
    // CEO + Eva rows
    expect(screen.getByText("CEO 助手")).toBeInTheDocument();
    expect(screen.getByText("Eva")).toBeInTheDocument();
  });

  it("uses the shared dialog shell for organization create dialogs", async () => {
    const user = userEvent.setup();
    mockOrgData();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "组织" }));
    await waitFor(() => {
      expect(screen.queryByText("正在加载组织架构…")).not.toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: "新建部门" }));

    let dialog = await screen.findByRole("dialog");
    let body = within(dialog)
      .getByLabelText("new-dept-name")
      .closest("[data-slot='dialog-body']");
    let footer = dialog.querySelector("[data-slot='dialog-footer']");

    expect(body).toHaveClass("px-5", "py-4");
    expect(footer).toHaveClass("border-t", "border-border");

    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: "新建 Agent" }));

    dialog = await screen.findByRole("dialog");
    body = within(dialog)
      .getByLabelText("new-agent-name")
      .closest("[data-slot='dialog-body']");
    footer = dialog.querySelector("[data-slot='dialog-footer']");

    expect(body).toHaveClass("px-5", "py-4");
    expect(footer).toHaveClass("border-t", "border-border");
  });

  it("opens detail panel when selecting an agent", async () => {
    const user = userEvent.setup();
    mockOrgData();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "组织" }));
    await waitFor(() => {
      expect(screen.queryByText("正在加载组织架构…")).not.toBeInTheDocument();
    });

    // initial state: empty detail panel
    expect(
      screen.getByText("选择一个部门或 Agent 查看详情"),
    ).toBeInTheDocument();

    // click Eva row
    const evaRow = screen.getByText("Eva").closest("button");
    if (!evaRow) throw new Error("Eva row not found");
    await user.click(evaRow);

    // agent detail rendered — description field carries the editable label
    const descInput = await screen.findByDisplayValue("工程总监");
    expect(descInput).toBeInTheDocument();
  });

  it("uses the same fixed detail panel in organization list mode", async () => {
    const user = userEvent.setup();
    localStorage.setItem("agentre.orgView.mode", "list");
    mockOrgData();
    const { container } = render(<App />);

    await user.click(screen.getByRole("button", { name: "组织" }));
    await waitFor(() => {
      expect(screen.queryByText("正在加载组织架构…")).not.toBeInTheDocument();
    });

    const detailPanel = container.querySelector(
      '[data-slot="org-detail-panel"]',
    );
    expect(detailPanel).toBeInTheDocument();
    expect(detailPanel).toHaveClass("w-[380px]", "shrink-0", "border-l");
    expect(
      container.querySelector('[data-slot="org-detail-drawer"]'),
    ).toBeNull();
    expect(
      within(detailPanel as HTMLElement).getByText(
        "选择一个部门或 Agent 查看详情",
      ),
    ).toBeInTheDocument();

    const evaRow = screen.getByText("Eva").closest("button");
    if (!evaRow) throw new Error("Eva row not found");
    await user.click(evaRow);

    expect(await screen.findByDisplayValue("工程总监")).toBeInTheDocument();
    expect(
      container.querySelector('[data-slot="org-detail-drawer"]'),
    ).toBeNull();
  });

  it("renders only backend management on the Agent backend page", async () => {
    const user = userEvent.setup();

    mockAgentBackends();
    mockLlmProviders();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "Agent 后端" }));

    const backendTable = await screen.findByRole("table", {
      name: "Agent 后端列表",
    });

    expectHorizontalTableScroll(backendTable);
    await waitFor(() => {
      expect(within(backendTable).getByText("默认助手")).toBeInTheDocument();
      expect(within(backendTable).getByText("AWS Bedrock")).toBeInTheDocument();
      expect(
        within(backendTable).getByText(/Anthropic · sonnet-4-6/),
      ).toBeInTheDocument();
    });

    expect(screen.getByText("运行时参数下放到每个 Agent")).toBeInTheDocument();
    expect(
      screen.queryByRole("table", { name: "LLM 供应商列表" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "新增供应商" }),
    ).not.toBeInTheDocument();
  });

  it("marks copyable app content as selectable text", async () => {
    const user = userEvent.setup();

    render(<App />);

    mockAgentBackends();
    mockLlmProviders();

    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "Agent 后端" }));

    const backendTable = await screen.findByRole("table", {
      name: "Agent 后端列表",
    });
    await waitFor(() => {
      expect(within(backendTable).getByText("默认助手")).toBeInTheDocument();
    });

    expect(
      within(backendTable)
        .getByText("默认助手")
        .closest("[data-selectable-text='true']"),
    ).toBeInTheDocument();
  });

  it("shows provider management after selecting LLM providers", async () => {
    const user = userEvent.setup();

    mockLlmProviders();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "LLM 供应商" }));

    const providerTable = await screen.findByRole("table", {
      name: "LLM 供应商列表",
    });

    expectHorizontalTableScroll(providerTable);
    expect(screen.getByRole("button", { name: "LLM 供应商" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      screen.getByRole("button", { name: "Agent 后端" }),
    ).not.toHaveAttribute("aria-current");
    expect(
      screen.queryByRole("table", { name: "Agent 后端列表" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("list", { name: "LLM 供应商紧凑列表" }),
    ).not.toBeInTheDocument();
    await waitFor(() => {
      expect(within(providerTable).getByText("Production")).toBeInTheDocument();
      expect(
        within(providerTable).getByText("Ollama 本机"),
      ).toBeInTheDocument();
      expect(
        within(providerTable).getByText("http://localhost:11434/v1"),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "新增供应商" }),
    ).toBeInTheDocument();
  });

  it("tests an LLM provider by calling the configured model", async () => {
    const user = userEvent.setup();

    mockLlmProviders();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "LLM 供应商" }));

    const providerTable = await screen.findByRole("table", {
      name: "LLM 供应商列表",
    });
    await waitFor(() => {
      expect(within(providerTable).getByText("Production")).toBeInTheDocument();
    });

    await user.click(
      within(providerTable).getByRole("button", { name: "测试 Production" }),
    );

    const appBridge = (
      window as unknown as {
        go?: { app?: { App?: Record<string, ReturnType<typeof vi.fn>> } };
      }
    ).go?.app?.App;

    expect(appBridge?.TestLLMProvider).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1 }),
    );
    expect(
      await screen.findByText(
        '"Production" 调用成功，已发送 hi 并收到模型响应',
      ),
    ).toBeInTheDocument();
  });

  it("tests a draft LLM provider from the create dialog", async () => {
    const user = userEvent.setup();

    mockLlmProviders();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "LLM 供应商" }));
    await user.click(await screen.findByRole("button", { name: "新增供应商" }));

    const dialog = await screen.findByRole("form", {
      name: "新增 LLM 供应商",
    });
    await user.type(within(dialog).getByLabelText("名称"), "Draft Anthropic");
    await user.type(
      within(dialog).getByPlaceholderText(/sk-\.\.\./),
      "sk-draft",
    );
    await user.type(
      within(dialog).getByPlaceholderText(/claude-opus-4-7/),
      "claude-sonnet-4-6",
    );

    await user.click(within(dialog).getByRole("button", { name: "测试调用" }));

    const appBridge = (
      window as unknown as {
        go?: { app?: { App?: Record<string, ReturnType<typeof vi.fn>> } };
      }
    ).go?.app?.App;

    expect(appBridge?.TestLLMProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        apiKey: "sk-draft",
        id: 0,
        model: "claude-sonnet-4-6",
        type: "anthropic",
        useDraft: true,
      }),
    );
    expect(
      await within(dialog).findByText("调用成功，已发送 hi 并收到模型响应"),
    ).toBeInTheDocument();
  });

  it("opens under construction pages from unimplemented settings items", async () => {
    const user = userEvent.setup();
    const unimplementedSettingsItems = ["通知", "MCP 服务器", "技能 / 工具"];

    mockDesktopViewport();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsNav = screen.getByRole("complementary", {
      name: "设置导航",
    });

    for (const label of unimplementedSettingsItems) {
      await user.click(
        within(settingsNav).getByRole("button", { name: label }),
      );

      const main = screen.getByRole("main");

      expect(
        within(settingsNav).getByRole("button", { name: label }),
      ).toHaveAttribute("aria-current", "page");
      expect(
        within(main).getByRole("heading", { name: label }),
      ).toBeInTheDocument();
      expect(within(main).getByText("建设中")).toBeInTheDocument();
      expect(
        within(main).queryByRole("combobox", { name: "主题模式" }),
      ).not.toBeInTheDocument();
    }
  });

  it("opens data-backup panel from settings nav", async () => {
    const user = userEvent.setup();

    mockDesktopViewport();
    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsNav = screen.getByRole("complementary", {
      name: "设置导航",
    });

    await user.click(
      within(settingsNav).getByRole("button", { name: "数据 & 备份" }),
    );

    const main = screen.getByRole("main");

    expect(
      within(settingsNav).getByRole("button", { name: "数据 & 备份" }),
    ).toHaveAttribute("aria-current", "page");
    expect(
      within(main).getByRole("heading", { name: "数据 & 备份" }),
    ).toBeInTheDocument();
    expect(within(main).queryByText("建设中")).not.toBeInTheDocument();
  });

  it("restores the saved dark theme before user interaction", async () => {
    const user = userEvent.setup();
    localStorage.setItem(themeStorageKey, "dark");

    render(<App />);

    expect(document.documentElement).toHaveClass("dark");

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsMain = screen.getByRole("main");

    expect(
      within(settingsMain).getByRole("combobox", { name: "主题模式" }),
    ).toHaveTextContent("深色");
  });

  it("selects manual light and dark themes from settings appearance", async () => {
    const user = userEvent.setup();

    localStorage.setItem(themeStorageKey, "light");

    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsMain = screen.getByRole("main");
    const navRail = screen.getByRole("complementary", { name: "主导航" });
    const topBar = screen.getByRole("banner");
    const themeSelect = within(settingsMain).getByRole("combobox", {
      name: "主题模式",
    });

    expect(document.documentElement).not.toHaveClass("dark");
    expect(
      within(topBar).queryByRole("combobox", { name: "主题模式" }),
    ).not.toBeInTheDocument();
    expect(
      within(topBar).queryByRole("button", { name: /切换主题/ }),
    ).not.toBeInTheDocument();
    expect(
      within(navRail).getByRole("button", { name: /切换主题/ }),
    ).toBeInTheDocument();
    expect(themeSelect).toHaveTextContent("浅色");

    await selectThemeOption(user, themeSelect, "dark");

    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem(themeStorageKey)).toBe("dark");
    expect(themeSelect).toHaveTextContent("深色");

    await selectThemeOption(user, themeSelect, "light");

    expect(document.documentElement).not.toHaveClass("dark");
    expect(localStorage.getItem(themeStorageKey)).toBe("light");
    expect(themeSelect).toHaveTextContent("浅色");
  });

  it("follows the saved system theme and reacts to system color-scheme changes", async () => {
    const user = userEvent.setup();
    const systemColorScheme = mockSystemColorScheme(false);
    localStorage.setItem(themeStorageKey, "system");

    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsMain = screen.getByRole("main");
    const themeSelect = within(settingsMain).getByRole("combobox", {
      name: "主题模式",
    });

    expect(document.documentElement).not.toHaveClass("dark");
    expect(themeSelect).toHaveTextContent("跟随系统");

    act(() => {
      systemColorScheme.setDark(true);
    });

    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem(themeStorageKey)).toBe("system");
    expect(themeSelect).toHaveTextContent("跟随系统");
  });

  it("can switch between following the system and manual preferences", async () => {
    const user = userEvent.setup();
    mockSystemColorScheme(true);
    localStorage.setItem(themeStorageKey, "light");

    render(<App />);

    await user.click(screen.getByRole("button", { name: "设置" }));

    const settingsMain = screen.getByRole("main");
    const themeSelect = within(settingsMain).getByRole("combobox", {
      name: "主题模式",
    });

    expect(document.documentElement).not.toHaveClass("dark");
    expect(themeSelect).toHaveTextContent("浅色");

    await selectThemeOption(user, themeSelect, "system");

    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem(themeStorageKey)).toBe("system");
    expect(themeSelect).toHaveTextContent("跟随系统");

    await selectThemeOption(user, themeSelect, "light");

    expect(document.documentElement).not.toHaveClass("dark");
    expect(localStorage.getItem(themeStorageKey)).toBe("light");
    expect(themeSelect).toHaveTextContent("浅色");
  });
});
