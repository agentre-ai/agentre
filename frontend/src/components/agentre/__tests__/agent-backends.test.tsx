import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { truncateFlashText } from "../agent-backends-utils";

const appMocks = vi.hoisted(() => ({
  CancelTestAgentBackend: vi.fn(),
  CreateAgentBackend: vi.fn(),
  CreateOpenClawAgentBackend: vi.fn(),
  DeleteAgentBackend: vi.fn(),
  GetGatewayStatus: vi.fn(),
  ListAgentBackends: vi.fn(),
  ListLLMModels: vi.fn(),
  ListLLMProviders: vi.fn(),
  RemoteDeviceList: vi.fn(),
  RemoteDeviceListProviders: vi.fn(),
  RemoteDeviceSyncProvider: vi.fn(),
  ResolveAgentBackendCLIPath: vi.fn(),
  TestAgentBackend: vi.fn(),
  TestOpenClawAgentBackend: vi.fn(),
  UpdateAgentBackend: vi.fn(),
  UpdateOpenClawAgentBackend: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { AgentBackendsPanel } from "../agent-backends";

type AnyFn = (...args: unknown[]) => unknown;

type AppMockShape = {
  ListAgentBackends: AnyFn;
  ListLLMProviders: AnyFn;
  ListLLMModels: AnyFn;
  CreateAgentBackend?: AnyFn;
  CreateOpenClawAgentBackend?: AnyFn;
  UpdateAgentBackend?: AnyFn;
  UpdateOpenClawAgentBackend?: AnyFn;
  DeleteAgentBackend?: AnyFn;
  TestAgentBackend?: AnyFn;
  TestOpenClawAgentBackend?: AnyFn;
  CancelTestAgentBackend?: AnyFn;
  GetGatewayStatus?: AnyFn;
  ResolveAgentBackendCLIPath?: AnyFn;
  RemoteDeviceList?: AnyFn;
  RemoteDeviceListProviders?: AnyFn;
  RemoteDeviceSyncProvider?: AnyFn;
};

// mockModel 构造一条启用模型记录（modelKey 稳定，modelId 可展示）。
function mockModel(providerId: number, modelKey: string, modelId: string) {
  return {
    id: providerId * 100,
    providerId,
    providerKey: `key-${providerId}`,
    modelKey,
    modelId,
    name: "",
    contextWindow: 0,
    maxOutput: 0,
    enabled: true,
    isDefault: false,
    createtime: 0,
    updatetime: 0,
  };
}

// modelsById 默认给每个 provider 返回一条启用模型（默认模型）。
function defaultModelsById(id: number) {
  if (id === 1) return [mockModel(1, "mk-1", "claude-sonnet-4-6")];
  if (id === 2) return [mockModel(2, "mk-2", "gpt-5")];
  if (id === 3) return [mockModel(3, "mk-3", "gpt-5-codex")];
  return [];
}

function installAppMock(overrides: Partial<AppMockShape> = {}) {
  const base: AppMockShape = {
    ListAgentBackends: vi.fn(() =>
      Promise.resolve({
        items: [
          {
            id: 1,
            type: "builtin",
            name: "默认助手",
            llmProviderKey: "key-1",
            llmProviderName: "Anthropic",
            llmProviderType: "anthropic",
            llmProviderModel: "claude-sonnet-4-6",
            llmProviderActive: true,
            cliPath: "",
            agentCount: 3,
            createtime: 0,
            updatetime: 0,
          },
        ],
      }),
    ),
    ListLLMProviders: vi.fn(() =>
      Promise.resolve({
        items: [
          {
            id: 1,
            type: "anthropic",
            name: "Anthropic",
            providerKey: "key-1",
            baseUrl: "",
            maskedApiKey: "sk-•••",
            hasApiKey: true,
            enabled: true,
            defaultModelKey: "mk-1",
            createtime: 0,
            updatetime: 0,
          },
        ],
      }),
    ),
    // 默认每个 provider 返回一条启用默认模型（Picker 目录需要）。
    ListLLMModels: vi.fn((...args: unknown[]) => {
      const req = args[0] as { id?: number } | undefined;
      return Promise.resolve({ items: defaultModelsById(Number(req?.id)) });
    }),
    CreateAgentBackend: vi.fn(() => Promise.resolve({ item: { id: 2 } })),
    CreateOpenClawAgentBackend: vi.fn(() =>
      Promise.resolve({ item: { id: 3 } }),
    ),
    UpdateAgentBackend: vi.fn(() => Promise.resolve({ item: { id: 1 } })),
    UpdateOpenClawAgentBackend: vi.fn(() =>
      Promise.resolve({ item: { id: 3 } }),
    ),
    DeleteAgentBackend: vi.fn(() => Promise.resolve({})),
    TestAgentBackend: vi.fn(() =>
      Promise.resolve({ ok: true, latencyMs: 0, message: "" }),
    ),
    TestOpenClawAgentBackend: vi.fn(() =>
      Promise.resolve({
        ok: true,
        code: "",
        message: "",
        latencyMs: 3,
        gatewayVersion: "2026.7.1-2",
        protocol: 4,
        grantedScopes: [
          "operator.read",
          "operator.write",
          "operator.approvals",
        ],
        methods: [],
        events: [],
        openClawAgents: [],
        openClawModels: [],
      }),
    ),
    CancelTestAgentBackend: vi.fn(() => Promise.resolve({ canceled: true })),
    GetGatewayStatus: vi.fn(() =>
      Promise.resolve({
        status: "running",
        listenURL: "http://127.0.0.1:60080",
        reason: "",
        routes: [],
      }),
    ),
    // 默认让 ResolveAgentBackendCLIPath 兜底返回 found=false，避免每个用例都得显式注入。
    // 单独验证自动识别行为的用例会在 overrides 里覆盖这个 mock。
    ResolveAgentBackendCLIPath: vi.fn(() =>
      Promise.resolve({ path: "", found: false }),
    ),
    RemoteDeviceList: vi.fn(() => Promise.resolve([])),
    RemoteDeviceListProviders: vi.fn(() => Promise.resolve([])),
    RemoteDeviceSyncProvider: vi.fn(() => Promise.resolve(undefined)),
  };
  const merged = { ...base, ...overrides } as Required<AppMockShape>;
  for (const key of Object.keys(appMocks) as Array<keyof typeof appMocks>) {
    const mock = appMocks[key] as ReturnType<typeof vi.fn>;
    const fn = merged[key as keyof Required<AppMockShape>] as AnyFn;
    mock.mockReset();
    mock.mockImplementation((...args: unknown[]) => fn(...args));
  }
  return merged;
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("AgentBackendsPanel", () => {
  it("renders backends fetched from Wails bindings", async () => {
    installAppMock();
    render(<AgentBackendsPanel />);

    const list = await screen.findByRole("list", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(within(list).getByText("默认助手")).toBeInTheDocument();
      expect(within(list).getByText("Anthropic")).toBeInTheDocument();
      expect(within(list).getByText("claude-sonnet-4-6")).toBeInTheDocument();
      expect(within(list).getByText("Follow default")).toBeInTheDocument();
    });
    expect(
      within(list).getByRole("img", { name: "Agentre" }),
    ).toBeInTheDocument();
    expect(
      within(list).getByRole("img", { name: "Anthropic" }),
    ).toBeInTheDocument();
  });

  it("flags rows whose LLM provider is inactive", async () => {
    installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "builtin",
              name: "孤儿后端",
              llmProviderKey: "key-7",
              llmProviderName: "",
              llmProviderType: "",
              llmProviderModel: "",
              llmProviderActive: false,
              cliPath: "",
              agentCount: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    const list = await screen.findByRole("list", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(within(list).getByText("孤儿后端")).toBeInTheDocument();
      expect(within(list).getByText("Needs action")).toBeInTheDocument();
    });
  });

  it("Given follow-default, fixed, CLI-login and invalid backends, When the list renders, Then each row shows an independent binding summary and change-binding action", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 11,
              type: "claudecode",
              name: "Follow backend",
              llmProviderKey: "key-1",
              llmModelKey: "",
              llmProviderName: "Anthropic",
              llmProviderType: "anthropic",
              llmProviderModel: "claude-sonnet-4-6",
              llmProviderActive: true,
              cliPath: "/usr/bin/claude",
              agentCount: 2,
            },
            {
              id: 12,
              type: "claudecode",
              name: "Fixed backend",
              llmProviderKey: "key-1",
              llmModelKey: "mk-opus",
              llmProviderName: "Anthropic",
              llmProviderType: "anthropic",
              llmProviderModel: "claude-opus-4-1",
              llmProviderActive: true,
              cliPath: "/usr/bin/claude",
              agentCount: 1,
            },
            {
              id: 13,
              type: "claudecode",
              name: "CLI backend",
              llmProviderKey: "",
              llmModelKey: "",
              llmProviderName: "",
              llmProviderType: "",
              llmProviderModel: "",
              llmProviderActive: false,
              cliPath: "/usr/bin/claude",
              agentCount: 0,
            },
            {
              id: 14,
              type: "claudecode",
              name: "Invalid backend",
              llmProviderKey: "key-gone",
              llmModelKey: "mk-gone",
              llmProviderName: "Removed provider",
              llmProviderType: "anthropic",
              llmProviderModel: "claude-removed",
              llmProviderActive: false,
              cliPath: "/usr/bin/claude",
              agentCount: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    const followRow = (await screen.findByText("Follow backend")).closest(
      '[role="listitem"]',
    ) as HTMLElement;
    expect(within(followRow).getByText("Anthropic")).toBeInTheDocument();
    expect(
      within(followRow).getByText("claude-sonnet-4-6"),
    ).toBeInTheDocument();
    expect(within(followRow).getByText("Follow default")).toBeInTheDocument();
    expect(
      within(followRow).getByRole("img", { name: "Anthropic" }),
    ).toBeInTheDocument();

    const fixedRow = screen
      .getByText("Fixed backend")
      .closest('[role="listitem"]') as HTMLElement;
    expect(within(fixedRow).getByText("claude-opus-4-1")).toBeInTheDocument();
    expect(within(fixedRow).getByText("Fixed")).toBeInTheDocument();

    const cliRow = screen
      .getByText("CLI backend")
      .closest('[role="listitem"]') as HTMLElement;
    expect(within(cliRow).getByText("Use CLI login state")).toBeInTheDocument();

    const invalidRow = screen
      .getByText("Invalid backend")
      .closest('[role="listitem"]') as HTMLElement;
    expect(
      within(invalidRow).getByText(/binding is invalid/i),
    ).toBeInTheDocument();
    const invalidChange = within(invalidRow).getByRole("button", {
      name: "Change binding for Invalid backend",
    });
    expect(invalidChange).toHaveAttribute("data-variant", "default");

    await user.click(
      within(followRow).getByRole("button", {
        name: "Change binding for Follow backend",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Edit Agent Backend",
    });
    expect(
      within(dialog).getByRole("heading", { name: "Model binding" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("listbox", { name: "Model binding" }),
    ).toBeInTheDocument();
  });

  it("Given a Claude backend editor, When binding mode changes, Then the model-binding block keeps tier routes collapsed and only shows custom model for CLI login", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            mockModel(1, "mk-1", "claude-sonnet-4-6"),
            mockModel(1, "mk-opus", "claude-opus-4-1"),
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    const block = within(dialog).getByTestId("model-binding-block");
    expect(
      within(block).getByRole("heading", { name: "Model binding" }),
    ).toBeInTheDocument();
    const routesToggle = within(block).getByRole("button", {
      name: /Model tier routes/,
    });
    expect(routesToggle).toHaveAttribute("aria-expanded", "false");
    expect(routesToggle).toHaveTextContent(/OPUS.*main binding/i);
    expect(routesToggle).toHaveTextContent(/SONNET.*main binding/i);
    expect(routesToggle).toHaveTextContent(/HAIKU.*main binding/i);
    expect(within(block).getByLabelText("Custom Model")).toBeInTheDocument();
    expect(
      within(block).getByText(/only applies with CLI login/i),
    ).toBeInTheDocument();

    await user.click(
      within(block).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      await screen.findByRole("option", {
        name: /Follow this provider's default/,
      }),
    );
    expect(
      within(block).queryByLabelText("Custom Model"),
    ).not.toBeInTheDocument();
    expect(
      within(block).getByText(/retained but ignored/i),
    ).toBeInTheDocument();

    await user.click(routesToggle);
    expect(routesToggle).toHaveAttribute("aria-expanded", "true");
    expect(
      within(block).getAllByRole("button", { name: /Claude tier route/ }),
    ).toHaveLength(3);
  });

  it("Given a selected provider target, When the editor renders, Then the picker trigger uses target-and-mode plus effective-model consequence on two lines", async () => {
    const user = userEvent.setup();
    installAppMock();
    render(<AgentBackendsPanel />);
    const row = (await screen.findByText("默认助手")).closest(
      '[role="listitem"]',
    ) as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));
    const dialog = await screen.findByRole("dialog");
    const trigger = within(dialog).getByRole("button", {
      name: "Model binding",
    });
    expect(trigger).toHaveTextContent("Anthropic");
    expect(trigger).toHaveTextContent("Follow default");
    expect(trigger).toHaveTextContent("claude-sonnet-4-6");
    expect(trigger).toHaveTextContent(/next turn/i);
    expect(
      within(trigger).getByTestId("model-target-trigger-sub"),
    ).toBeVisible();
  });

  it("Given an editor draft, When runtime and target change, Then the effective configuration summary updates live and explains whether saving is possible", async () => {
    const user = userEvent.setup();
    installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            mockModel(1, "mk-1", "claude-sonnet-4-6"),
            mockModel(1, "mk-opus", "claude-opus-4-1"),
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    const summary = within(dialog).getByTestId("effective-config-summary");
    expect(summary).toHaveTextContent(/Local/);
    expect(summary).toHaveTextContent(/CLI login state/);
    expect(summary).toHaveTextContent(/Can save/);
    expect(summary).toHaveTextContent(/0 agents/);

    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(await screen.findByRole("option", { name: /linux-srv/ }));
    expect(summary).toHaveTextContent("linux-srv");

    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      await screen.findByRole("option", { name: /claude-opus-4-1/ }),
    );
    expect(summary).toHaveTextContent(/Anthropic/);
    expect(summary).toHaveTextContent(/claude-opus-4-1/);
    expect(summary).toHaveTextContent(/Fixed/);

    await user.clear(within(dialog).getByLabelText("Name"));
    expect(summary).toHaveTextContent(/Cannot save/);
    expect(summary).toHaveTextContent(/Enter a backend name/i);
  });

  it("does not show implementation compatibility terminology and only explains the empty-compatible-provider case with a settings action", async () => {
    const user = userEvent.setup();
    const onOpenLlmProviders = vi.fn();
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 2,
              type: "openai-response",
              name: "OpenAI",
              providerKey: "key-2",
              enabled: true,
              defaultModelKey: "mk-2",
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel onOpenLlmProviders={onOpenLlmProviders} />);
    await screen.findByText("默认助手");
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    expect(within(dialog).queryByText(/Strict match/)).not.toBeInTheDocument();
    expect(
      within(dialog).getByText(
        /No providers compatible with this backend type/,
      ),
    ).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Configure LLM providers" }),
    );
    expect(onOpenLlmProviders).toHaveBeenCalledTimes(1);
  });

  it("shows a provider-empty hint + configure button when the provider list is empty (task 8)", async () => {
    const user = userEvent.setup();
    const onOpenLlmProviders = vi.fn();
    installAppMock({
      ListLLMProviders: vi.fn(() => Promise.resolve({ items: [] })),
    });
    render(<AgentBackendsPanel onOpenLlmProviders={onOpenLlmProviders} />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("No LLM providers available yet"),
    ).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Configure LLM providers" }),
    );
    expect(onOpenLlmProviders).toHaveBeenCalledTimes(1);
  });

  it("submits create dialog with builtin type", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    // The Input is inside a <label> whose text node says "名称". Use placeholder
    // to grab it directly since shadcn Input doesn't tie label via htmlFor here.
    const nameInput = within(dialog).getByPlaceholderText(
      "Example: Local · Claude Code",
    );
    fireEvent.change(nameInput, { target: { value: "新助手" } });

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "builtin",
          name: "新助手",
          llmProviderKey: "key-1",
          cliPath: "",
        }),
      );
    });
  });

  it("clicking 测试连接 on a row shows success flash with latency + reply", async () => {
    const mocks = installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({ ok: true, latencyMs: 128, message: "pong" }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    const row = screen
      .getByText("默认助手")
      .closest('[role="listitem"]') as HTMLElement;
    fireEvent.click(
      within(row).getByRole("button", { name: /Test connection/ }),
    );

    await waitFor(() => {
      expect(screen.getByText(/128ms/)).toBeInTheDocument();
      expect(screen.getByText(/pong/)).toBeInTheDocument();
    });
    expect(mocks.TestAgentBackend).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 1,
        useDraft: false,
        type: "",
        name: "",
        llmProviderKey: "",
        cliPath: "",
      }),
    );
  });

  it("clicking 测试连接 on a row shows error flash on OK=false", async () => {
    installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({
          ok: false,
          latencyMs: 30,
          message: "401 Unauthorized",
        }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    const row = screen
      .getByText("默认助手")
      .closest('[role="listitem"]') as HTMLElement;
    fireEvent.click(
      within(row).getByRole("button", { name: /Test connection/ }),
    );

    await waitFor(() =>
      expect(screen.getByText(/401 Unauthorized/)).toBeInTheDocument(),
    );
  });

  it("clicking 测试连接 in dialog sends draft fields", async () => {
    const mocks = installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({ ok: true, latencyMs: 99, message: "pong" }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    fireEvent.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByPlaceholderText(/Claude Code/), {
      target: { value: "draft-name" },
    });

    fireEvent.click(
      within(dialog).getByRole("button", { name: /Test Connection/ }),
    );

    await waitFor(() =>
      expect(mocks.TestAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 0,
          useDraft: true,
          type: "builtin",
          name: "draft-name",
          llmProviderKey: expect.any(String),
          cliPath: "",
        }),
      ),
    );
  });

  it("dialog 测试连接 result is shown inside the dialog (not hidden behind overlay)", async () => {
    installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({ ok: true, latencyMs: 87, message: "pong" }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    fireEvent.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByPlaceholderText(/Claude Code/), {
      target: { value: "draft-name" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: /Test Connection/ }),
    );

    await waitFor(() => {
      expect(within(dialog).getByText(/87ms/)).toBeInTheDocument();
      expect(within(dialog).getByText(/pong/)).toBeInTheDocument();
    });
  });

  it("dialog 测试结果落在 footer，不在 body 滚动区里，避免长表单时被挤到看不到", async () => {
    installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({ ok: true, latencyMs: 87, message: "pong" }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    fireEvent.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByPlaceholderText(/Claude Code/), {
      target: { value: "draft-name" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: /Test Connection/ }),
    );

    const pong = await within(dialog).findByText(/pong/);
    const footer = dialog.querySelector(
      '[data-slot="dialog-footer"]',
    ) as HTMLElement | null;
    const body = dialog.querySelector(
      '[data-slot="dialog-body"]',
    ) as HTMLElement | null;
    expect(footer).not.toBeNull();
    expect(body).not.toBeNull();
    expect(footer!.contains(pong)).toBe(true);
    expect(body!.contains(pong)).toBe(false);
  });

  it("新建保存失败时错误显示在弹窗内，而不是表格提示区", async () => {
    const user = userEvent.setup();
    installAppMock({
      CreateAgentBackend: vi.fn(() =>
        Promise.reject(new Error("backend name exists")),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "Duplicate backend" } },
    );

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(
        within(dialog).getByText("backend name exists"),
      ).toBeInTheDocument();
    });

    expect(
      screen.getAllByRole("status").filter((node) => !dialog.contains(node)),
    ).toHaveLength(0);
  });

  it("claudecode/codex 行未关联供应商时显示「走 CLI 自身登录」而非需处理", async () => {
    installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 9,
              type: "claudecode",
              name: "无 provider 的 claude",
              llmProviderKey: "",
              llmProviderName: "",
              llmProviderType: "",
              llmProviderModel: "",
              llmProviderActive: false,
              cliPath: "",
              agentCount: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    const list = await screen.findByRole("list", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(
        within(list).getByText("无 provider 的 claude"),
      ).toBeInTheDocument();
      expect(within(list).getByText(/Use CLI login/)).toBeInTheDocument();
      expect(within(list).queryByText("Needs action")).not.toBeInTheDocument();
    });
  });

  it.each([
    ["claudecode", "无 provider 的 claude", "Anthropic", "anthropic"],
    ["codex", "无 provider 的 codex", "OpenAI", "openai-response"],
    ["piagent", "Pi Agent", "", ""],
  ])(
    "编辑 %s 且未关联供应商时不显示原供应商停用提示",
    async (type, name, providerName, providerType) => {
      const user = userEvent.setup();
      installAppMock({
        ListAgentBackends: vi.fn(() =>
          Promise.resolve({
            items: [
              {
                id: 9,
                type,
                name,
                llmProviderKey: "",
                llmProviderName: "",
                llmProviderType: "",
                llmProviderModel: "",
                llmProviderActive: false,
                cliPath: "",
                agentCount: 0,
                createtime: 0,
                updatetime: 0,
              },
            ],
          }),
        ),
        ListLLMProviders: vi.fn(() =>
          Promise.resolve({
            items: [
              {
                id: 1,
                type: providerType,
                name: providerName,
                providerKey: "key-1",
                baseUrl: "",
                maskedApiKey: "sk-•••",
                hasApiKey: true,
                model:
                  providerType === "anthropic"
                    ? "claude-sonnet-4-6"
                    : "gpt-5-codex",
                maxOutput: 0,
                contextWindow: 0,
                createtime: 0,
                updatetime: 0,
              },
            ],
          }),
        ),
      });
      render(<AgentBackendsPanel />);

      await screen.findByText(name);
      const row = screen
        .getByText(name)
        .closest('[role="listitem"]') as HTMLElement;
      await user.click(within(row).getByRole("button", { name: /Edit/ }));

      const dialog = await screen.findByRole("dialog");
      expect(
        within(dialog).queryByText(/original LLM provider is disabled/),
      ).not.toBeInTheDocument();
      expect(
        within(dialog).getByRole("button", { name: "Model binding" }),
      ).toHaveTextContent(/CLI login state/);
    },
  );

  it("新建 pi-agent 时不需要 provider，保存时提交 type=piagent 和 cliPath", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ResolveAgentBackendCLIPath: vi.fn(() =>
        Promise.resolve({ path: "/opt/homebrew/bin/pi", found: true }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "本机 Pi" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Pi Agent CLI/ }),
    );

    const input = within(dialog).getByPlaceholderText(
      "/usr/local/bin/pi",
    ) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("/opt/homebrew/bin/pi"));
    // piagent 现在显示可选的 provider 选择器（默认未关联走 CLI 自身登录）。
    expect(
      within(dialog).getByRole("button", { name: "Model binding" }),
    ).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "piagent",
          name: "本机 Pi",
          llmProviderKey: "",
          cliPath: "/opt/homebrew/bin/pi",
        }),
      );
    });
  });

  it("piagent 编辑器列出三类 LLM 供应商，可选其一保存绑定", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Anthropic",
              providerKey: "k-anthropic",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              enabled: true,
              defaultModelKey: "mk-anthropic",
              createtime: 0,
              updatetime: 0,
            },
            {
              id: 2,
              type: "openai-chat",
              name: "OpenAI Chat",
              providerKey: "k-chat",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              enabled: true,
              defaultModelKey: "mk-chat",
              createtime: 0,
              updatetime: 0,
            },
            {
              id: 3,
              type: "openai-response",
              name: "OpenAI Response",
              providerKey: "k-response",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              enabled: true,
              defaultModelKey: "mk-response",
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
      ListLLMModels: vi.fn((...args: unknown[]) => {
        const req = args[0] as { id?: number } | undefined;
        const byId: Record<number, unknown[]> = {
          1: [mockModel(1, "mk-anthropic", "claude-sonnet-4-6")],
          2: [mockModel(2, "mk-chat", "gpt-5")],
          3: [mockModel(3, "mk-response", "gpt-5-codex")],
        };
        return Promise.resolve({ items: byId[Number(req?.id)] ?? [] });
      }),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "Pi 绑供应商" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Pi Agent CLI/ }),
    );

    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    // piagent 三类全收：anthropic / openai-chat / openai-response 都要列出来。
    const defaultOptions = await screen.findAllByRole("option", {
      name: /Follow this provider's default/,
    });
    expect(defaultOptions).toHaveLength(3);
    expect(screen.getAllByText("Anthropic").length).toBeGreaterThan(0);
    expect(screen.getAllByText("OpenAI Chat").length).toBeGreaterThan(0);
    expect(screen.getAllByText("OpenAI Response").length).toBeGreaterThan(0);
    await user.click(defaultOptions[2]);

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "piagent",
          name: "Pi 绑供应商",
          llmProviderKey: "k-response",
        }),
      );
    });
  });

  it("piagent 绑定 Model 为空的供应商时显示校验提示并阻止保存", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Anthropic",
              providerKey: "k-empty-model",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              enabled: true,
              defaultModelKey: "mk-missing",
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
      // 目录里没有该 provider 的模型 → provider-default 解析不出默认模型。
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [] })),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "Pi 空模型" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Pi Agent CLI/ }),
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );

    // 选中的供应商 Model 为空 → 可见校验提示 + Save 被禁用。
    expect(
      within(dialog).getByText("Provider has no default model"),
    ).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    expect(mocks.CreateAgentBackend).not.toHaveBeenCalled();
  });

  it("新建 claudecode 时允许不选 provider 提交 llmProviderKey 空串", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "claude 走自身登录" } },
    );

    // 切换到 Claude Code CLI 类型 → provider 默认为空（CLI 自身登录）。
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "claudecode",
          name: "claude 走自身登录",
          llmProviderKey: "",
        }),
      );
    });
  });

  it("新建 claudecode 保持本地运行时提交 deviceId 为空串", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "mac-mini", online: true }]),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "本地 claude" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "claudecode",
          name: "本地 claude",
          deviceId: "",
        }),
      );
    });
  });

  it("保存远端 claudecode 且远端缺少 provider 时提示同步，确认后先同步再保存", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      RemoteDeviceListProviders: vi.fn(() => Promise.resolve([])),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));

    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    const syncDialog = await screen.findByRole("dialog", {
      name: /Sync Remote LLM Provider/,
    });
    expect(
      within(syncDialog).getByText(
        /API key, default model and model catalog to the remote agentred state file/,
      ),
    ).toBeInTheDocument();
    expect(mocks.CreateAgentBackend).not.toHaveBeenCalled();

    await user.click(
      within(syncDialog).getByRole("button", { name: "Sync and Save" }),
    );

    await waitFor(() => {
      expect(mocks.RemoteDeviceSyncProvider).toHaveBeenCalledWith(7, "key-1");
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "claudecode",
          name: "远端 claude",
          deviceId: "7",
          llmProviderKey: "key-1",
        }),
      );
    });
  });

  it("选择远端 provider 后在编辑弹窗里显示同步入口，手动同步成功后提示并关闭弹窗", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );

    expect(
      within(editorDialog).getByText("Remote Provider Sync"),
    ).toBeInTheDocument();
    await user.click(
      within(editorDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    const syncDialog = await screen.findByRole("dialog", {
      name: /Sync Remote LLM Provider/,
    });
    await user.click(
      within(syncDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    await waitFor(() => {
      expect(mocks.RemoteDeviceSyncProvider).toHaveBeenCalledWith(7, "key-1");
      expect(mocks.CreateAgentBackend).not.toHaveBeenCalled();
      expect(screen.getByText(/Remote provider synced/)).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("手动同步失败时错误显示在同步弹窗内，不刷到表格顶部", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      RemoteDeviceSyncProvider: vi.fn(() =>
        Promise.reject(new Error("remote sync failed")),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );
    await user.click(
      within(editorDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    const syncDialog = await screen.findByRole("dialog", {
      name: /Sync Remote LLM Provider/,
    });
    await user.click(
      within(syncDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    await waitFor(() => {
      expect(mocks.RemoteDeviceSyncProvider).toHaveBeenCalledWith(7, "key-1");
      expect(within(syncDialog).getByText("Sync Failed")).toBeInTheDocument();
      expect(
        within(syncDialog).getByText(/remote sync failed/),
      ).toBeInTheDocument();
      expect(screen.getAllByText(/remote sync failed/)).toHaveLength(1);
      expect(mocks.CreateAgentBackend).not.toHaveBeenCalled();
    });
  });

  it("手动同步遇到旧版远端 Secret Service 缺失时提示升级到状态文件存储", async () => {
    const user = userEvent.setup();
    installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      RemoteDeviceSyncProvider: vi.fn(() =>
        Promise.reject(
          new Error(
            "remote llm.upsert: keychain set: The name org.freedesktop.secrets was not provided by any .service files",
          ),
        ),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );
    await user.click(
      within(editorDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    const syncDialog = await screen.findByRole("dialog", {
      name: /Sync Remote LLM Provider/,
    });
    await user.click(
      within(syncDialog).getByRole("button", { name: "Sync to Remote" }),
    );

    await waitFor(() => {
      expect(within(syncDialog).getByText("Sync Failed")).toBeInTheDocument();
      expect(
        within(syncDialog).getByText(
          /older remote agentred is still writing to the system keychain/i,
        ),
      ).toBeInTheDocument();
      expect(
        within(syncDialog).getByText(
          /current version writes directly to the agentred state file/i,
        ),
      ).toBeInTheDocument();
      expect(
        within(syncDialog).getByText(/org\.freedesktop\.secrets/),
      ).toBeInTheDocument();
    });
    expect(
      screen.queryAllByText(
        /older remote agentred is still writing to the system keychain/i,
      ),
    ).toHaveLength(1);
  });

  it("Given a saved remote backend DTO, When it is edited and saved, Then its deviceId stays selected and remote Provider sync remains available", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 12,
              type: "claudecode",
              name: "saved remote claude",
              deviceId: "7",
              llmProviderKey: "key-1",
              llmModelKey: "",
              llmProviderName: "Anthropic",
              llmProviderType: "anthropic",
              llmProviderModel: "claude-sonnet-4-6",
              llmProviderActive: true,
              cliPath: "claude",
              modelRoutes: {},
              envJson: "{}",
              agentCount: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      RemoteDeviceListProviders: vi.fn(() =>
        Promise.resolve([
          { key: "key-1", name: "Anthropic", type: "anthropic" },
        ]),
      ),
    });
    render(<AgentBackendsPanel />);

    const row = (await screen.findByText("saved remote claude")).closest(
      '[role="listitem"]',
    ) as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));

    const dialog = await screen.findByRole("dialog");
    await waitFor(() => {
      expect(
        within(dialog).getByRole("combobox", { name: "Runtime Device" }),
      ).toHaveTextContent("linux-srv");
      expect(
        within(dialog).getByText("Remote Provider Sync"),
      ).toBeInTheDocument();
    });

    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(mocks.UpdateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({ id: 12, deviceId: "7" }),
      );
    });
  });

  it("保存远端 claudecode 且 provider 已在远端时直接保存，不弹同步提示", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
      RemoteDeviceListProviders: vi.fn(() =>
        Promise.resolve([
          {
            key: "key-1",
            name: "Anthropic",
            type: "anthropic",
            defaultModelKey: "mk-1",
            models: [
              {
                key: "mk-1",
                modelId: "claude-sonnet-4-6",
                name: "claude-sonnet-4-6",
                enabled: true,
              },
            ],
          },
        ]),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "已同步 claude" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      screen.getByRole("option", { name: /Follow this provider's default/ }),
    );

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          deviceId: "7",
          llmProviderKey: "key-1",
        }),
      );
    });
    expect(mocks.RemoteDeviceSyncProvider).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("dialog", { name: /Sync Remote LLM Provider/ }),
    ).not.toBeInTheDocument();
  });

  it("编辑 claudecode 时可清除 provider 关联并提交 llmProviderKey 空串", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 11,
              type: "claudecode",
              name: "走 gateway 的 claude",
              llmProviderKey: "key-1",
              llmProviderName: "Anthropic",
              llmProviderType: "anthropic",
              llmProviderModel: "claude-sonnet-4-6",
              llmProviderActive: true,
              cliPath: "",
              agentCount: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByText("走 gateway 的 claude");
    const row = screen
      .getByText("走 gateway 的 claude")
      .closest('[role="listitem"]') as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));

    const dialog = await screen.findByRole("dialog");
    // 通过 Picker 顶部特殊项（CLI 自身登录态）清除 provider 关联。
    await user.click(
      within(dialog).getByRole("button", { name: "Model binding" }),
    );
    await user.click(
      await screen.findByRole("option", { name: /CLI login state/ }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.UpdateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 11,
          llmProviderKey: "",
        }),
      );
    });
  });

  it("新建时切到 claudecode → 自动调 ResolveAgentBackendCLIPath 把识别到的路径填入 input", async () => {
    const user = userEvent.setup();
    const resolveFn = vi.fn(() =>
      Promise.resolve({ path: "/opt/homebrew/bin/claude", found: true }),
    );
    installAppMock({ ResolveAgentBackendCLIPath: resolveFn });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    await waitFor(() => {
      expect(resolveFn).toHaveBeenCalledWith(
        expect.objectContaining({ type: "claudecode" }),
      );
      const input = within(dialog).getByPlaceholderText(
        "/usr/local/bin/claude",
      ) as HTMLInputElement;
      expect(input.value).toBe("/opt/homebrew/bin/claude");
    });
  });

  it("切到 codex 时自动识别命中 → input 显示 codex 的绝对路径", async () => {
    const user = userEvent.setup();
    const resolveFn = vi.fn(() =>
      Promise.resolve({ path: "/usr/local/bin/codex", found: true }),
    );
    installAppMock({ ResolveAgentBackendCLIPath: resolveFn });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("radio", { name: /Codex CLI/ }));

    await waitFor(() => {
      expect(resolveFn).toHaveBeenCalledWith(
        expect.objectContaining({ type: "codex" }),
      );
      const input = within(dialog).getByPlaceholderText(
        "/usr/local/bin/codex",
      ) as HTMLInputElement;
      expect(input.value).toBe("/usr/local/bin/codex");
    });
  });

  it("Codex approval options match codex-cli 0.145.0 and omit on-failure", async () => {
    const user = userEvent.setup();
    installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("radio", { name: /Codex CLI/ }));
    await user.click(
      within(dialog).getByRole("combobox", { name: "Approval Policy" }),
    );

    expect(
      screen.getByRole("option", { name: /trusted tools/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /model requests/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /Never ask/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /tool fails/ }),
    ).not.toBeInTheDocument();
  });

  it("codex 思考力度开放 xhigh，保存时透传 reasoningEffort=xhigh", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "codex xhigh" } },
    );
    await user.click(within(dialog).getByRole("radio", { name: /Codex CLI/ }));

    await user.click(
      within(dialog).getByRole("combobox", { name: "Reasoning Effort" }),
    );
    expect(screen.getByRole("option", { name: /xhigh/ })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /max/ }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: /xhigh/ }));

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "codex",
          name: "codex xhigh",
          reasoningEffort: "xhigh",
        }),
      );
    });
  });

  it("自动识别未命中时不写入 input，input 维持空值（用户回退手填）", async () => {
    const user = userEvent.setup();
    installAppMock({
      ResolveAgentBackendCLIPath: vi.fn(() =>
        Promise.resolve({ path: "", found: false }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    // 给一点时机让 ResolveCLIPath 的 Promise 完成；命中分支已被其它用例覆盖，这里仅断终态。
    const input = within(dialog).getByPlaceholderText(
      "/usr/local/bin/claude",
    ) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe(""));
  });

  it("点「自动识别」按钮 → 用当前类型重跑探测并覆盖 input", async () => {
    const user = userEvent.setup();
    let nextPath = "/first/claude";
    const resolveFn = vi.fn(() =>
      Promise.resolve({ path: nextPath, found: true }),
    );
    installAppMock({ ResolveAgentBackendCLIPath: resolveFn });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    const input = within(dialog).getByPlaceholderText(
      "/usr/local/bin/claude",
    ) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("/first/claude"));

    // 用户手改了值，然后点按钮重识别 → 按钮要覆盖手填值。
    fireEvent.change(input, { target: { value: "/wrong/path" } });
    nextPath = "/second/claude";
    // 打开对话框时会对三个 CLI 各探一次，所以只能比「点按钮前后」的增量，不能比总次数。
    const callsBeforeDetect = resolveFn.mock.calls.length;
    await user.click(within(dialog).getByRole("button", { name: /Detect/ }));

    await waitFor(() => expect(input.value).toBe("/second/claude"));
    expect(resolveFn.mock.calls.length).toBe(callsBeforeDetect + 1);
  });

  it("自动识别按钮未命中时显示 $PATH 提示且不改 input", async () => {
    const user = userEvent.setup();
    installAppMock({
      ResolveAgentBackendCLIPath: vi.fn(() =>
        Promise.resolve({ path: "", found: false }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("radio", { name: /Codex CLI/ }));

    const input = within(dialog).getByPlaceholderText(
      "/usr/local/bin/codex",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "/manual/codex" } });

    await user.click(within(dialog).getByRole("button", { name: /Detect/ }));

    await waitFor(() =>
      expect(
        within(dialog).getByText(/codex was not found in \$PATH/),
      ).toBeInTheDocument(),
    );
    // miss 时不能覆盖用户手填的值。
    expect(input.value).toBe("/manual/codex");
  });

  it("测试中显示取消按钮，点取消 → 调 CancelTestAgentBackend 同一 requestId", async () => {
    // 用一个永远不 resolve 的 Promise 模拟"卡住的测试"。
    let capturedRequestId = "";
    const cancelFn = vi.fn(() => Promise.resolve({ canceled: true }));
    installAppMock({
      TestAgentBackend: vi.fn((...args: unknown[]) => {
        const req = args[0] as { requestId?: string };
        capturedRequestId = req?.requestId ?? "";
        return new Promise(() => {}); // 永远 pending
      }),
      CancelTestAgentBackend: cancelFn,
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    const row = screen
      .getByText("默认助手")
      .closest('[role="listitem"]') as HTMLElement;
    fireEvent.click(
      within(row).getByRole("button", { name: /Test connection/ }),
    );

    // 按钮 title 切换为"取消测试"
    const cancelBtn = await within(row).findByRole("button", {
      name: /Cancel test/,
    });
    expect(capturedRequestId).not.toBe("");
    fireEvent.click(cancelBtn);

    await waitFor(() =>
      expect(cancelFn).toHaveBeenCalledWith(
        expect.objectContaining({ requestId: capturedRequestId }),
      ),
    );
    // UI 应恢复成"Test Connection"
    await waitFor(() =>
      expect(
        within(row).getByRole("button", { name: /Test connection/ }),
      ).toBeInTheDocument(),
    );
  });

  it("flash banner 长 message 被截断到 80 字 + …，完整内容放 title", async () => {
    const long = "x".repeat(300);
    installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({ ok: false, latencyMs: 12, message: long }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    const row = screen
      .getByText("默认助手")
      .closest('[role="listitem"]') as HTMLElement;
    fireEvent.click(
      within(row).getByRole("button", { name: /Test connection/ }),
    );

    const banner = await screen.findByRole("status");
    // banner 文本应短于完整 message
    const span = banner.querySelector("span[title]") as HTMLElement | null;
    expect(span).not.toBeNull();
    expect(span!.textContent!.length).toBeLessThan(long.length);
    expect(span!.textContent!.endsWith("…")).toBe(true);
    // title 应包含完整 message
    expect(span!.getAttribute("title")).toContain(long);
  });

  it("远端 claudecode + bypassPermissions 显示 IS_SANDBOX 提示;点按钮把 IS_SANDBOX=1 一键写进 env_json", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    // 选远端 device
    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));

    // 选 bypassPermissions
    await user.click(
      within(dialog).getByRole("combobox", { name: "Default Permission Mode" }),
    );
    await user.click(screen.getByRole("option", { name: /bypassPermissions/ }));

    // 提示出现 + 按钮可点
    expect(
      within(dialog).getByText(/remote agentred runs as root\/sudo/),
    ).toBeInTheDocument();
    const addBtn = within(dialog).getByRole("button", {
      name: /Add IS_SANDBOX=1/,
    });

    await user.click(addBtn);

    // 按钮变成「Configured in env_json」灰态
    expect(
      within(dialog).getByText(/Configured in env_json/),
    ).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "claudecode",
          deviceId: "7",
          defaultPermissionMode: "bypassPermissions",
          envJson: expect.stringContaining(`"IS_SANDBOX":"1"`),
        }),
      );
    });
  });

  it("本地 claudecode + bypassPermissions 不显示 IS_SANDBOX 提示(只有远端才需要)", async () => {
    const user = userEvent.setup();
    installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    // 不改 device → 保持本地
    await user.click(
      within(dialog).getByRole("combobox", { name: "Default Permission Mode" }),
    );
    await user.click(screen.getByRole("option", { name: /bypassPermissions/ }));

    // 危险提示仍在(沙箱/CI 那句),但 root/sudo 提示不应出现
    expect(
      within(dialog).queryByText(/remote agentred runs as root\/sudo/),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: /Add IS_SANDBOX=1/ }),
    ).not.toBeInTheDocument();
  });

  it("creates an OpenClaw backend through dedicated Wails bindings and loads probe discovery", async () => {
    const user = userEvent.setup();
    const credential = "t".repeat(48);
    const mocks = installAppMock({
      TestOpenClawAgentBackend: vi.fn(() =>
        Promise.resolve({
          ok: true,
          code: "",
          message: "",
          latencyMs: 9,
          gatewayVersion: "2026.7.1-2",
          protocol: 4,
          grantedScopes: [
            "operator.read",
            "operator.write",
            "operator.approvals",
          ],
          methods: ["agent", "agent.wait"],
          events: ["agent", "exec.approval.requested"],
          openClawAgents: [
            {
              id: "main",
              name: "Main",
              primaryModel: "anthropic/claude-sonnet-4-6",
              fallbacks: [],
              default: true,
            },
          ],
          openClawModels: [
            {
              id: "anthropic/claude-sonnet-4-6",
              name: "Claude Sonnet 4.6",
              provider: "anthropic",
              available: true,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: "OpenClaw Gateway" }),
    );

    await user.clear(within(dialog).getByLabelText("Name"));
    await user.type(within(dialog).getByLabelText("Name"), "Local OpenClaw");
    await user.type(within(dialog).getByLabelText("Gateway token"), credential);
    await user.click(
      within(dialog).getByRole("button", { name: "Test Connection" }),
    );

    await waitFor(() => {
      expect(mocks.TestOpenClawAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "openclaw",
          openClawGatewayUrl: "ws://127.0.0.1:18789",
          openClawSessionMode: "per-agentre-session",
        }),
        credential,
      );
    });
    expect(within(dialog).getByText("2026.7.1-2")).toBeInTheDocument();
    expect(within(dialog).getByText("Protocol 4")).toBeInTheDocument();
    expect(within(dialog).getByText("operator.approvals")).toBeInTheDocument();
    expect(within(dialog).getAllByText(/Main/).length).toBeGreaterThan(0);
    expect(
      within(dialog).getAllByText(/Claude Sonnet 4.6/).length,
    ).toBeGreaterThan(0);

    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(mocks.CreateOpenClawAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "openclaw",
          name: "Local OpenClaw",
          openClawGatewayUrl: "ws://127.0.0.1:18789",
          openClawAgentId: "main",
          openClawDefaultModel: "anthropic/claude-sonnet-4-6",
          openClawSessionMode: "per-agentre-session",
        }),
        credential,
      );
    });
    expect(mocks.CreateAgentBackend).not.toHaveBeenCalled();
  });

  it("does not echo a saved OpenClaw token and supports explicit clearing", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 8,
              type: "openclaw",
              name: "OpenClaw Local",
              openClawGatewayUrl: "ws://127.0.0.1:18789",
              openClawAgentId: "main",
              openClawDefaultModel: "anthropic/claude-sonnet-4-6",
              openClawSessionMode: "per-agentre-session",
              hasToken: true,
              deviceId: "",
              agentCount: 1,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    const row = (await screen.findByText("OpenClaw Local")).closest(
      '[role="listitem"]',
    ) as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));
    const dialog = await screen.findByRole("dialog");
    const token = within(dialog).getByLabelText("Gateway token");
    expect(token).toHaveValue("");
    expect(token).toHaveAttribute(
      "placeholder",
      "Token is stored securely. Enter a new value to replace it.",
    );

    await user.click(
      within(dialog).getByRole("switch", { name: "Clear stored token" }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(mocks.UpdateOpenClawAgentBackend).toHaveBeenCalledWith(
        expect.objectContaining({ id: 8, name: "OpenClaw Local" }),
        "",
        true,
      );
    });
    expect(mocks.UpdateAgentBackend).not.toHaveBeenCalled();
  });

  it("maps structured OpenClaw scope errors and explains the remote boundary", async () => {
    const user = userEvent.setup();
    installAppMock({
      TestOpenClawAgentBackend: vi.fn(() =>
        Promise.resolve({
          ok: false,
          code: "OPENCLAW_SCOPE_MISSING",
          message: "missing scope",
          latencyMs: 1,
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("radio", { name: "OpenClaw Gateway" }),
    );
    expect(
      within(dialog).getByText(
        "Remote agentred support is unavailable until secure secret enrollment is implemented.",
      ),
    ).toBeInTheDocument();

    await user.click(
      within(dialog).getByRole("button", { name: "Test Connection" }),
    );
    expect(
      await within(dialog).findByText(
        "The Gateway did not grant all required operator scopes.",
      ),
    ).toBeInTheDocument();
  });

  it("dialog 测试连接 shows error message inside the dialog", async () => {
    installAppMock({
      TestAgentBackend: vi.fn(() =>
        Promise.resolve({
          ok: false,
          latencyMs: 12,
          message: "401 Unauthorized",
        }),
      ),
    });
    render(<AgentBackendsPanel />);
    await screen.findByText("默认助手");

    fireEvent.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByPlaceholderText(/Claude Code/), {
      target: { value: "draft-name" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: /Test Connection/ }),
    );

    await waitFor(() =>
      expect(within(dialog).getByText(/401 Unauthorized/)).toBeInTheDocument(),
    );
  });
});

describe("Agent backend type picker", () => {
  async function openCreateDialog(user: ReturnType<typeof userEvent.setup>) {
    render(<AgentBackendsPanel />);
    await screen.findByRole("list", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    return screen.findByRole("dialog");
  }

  it("Given the create dialog, When it opens, Then the five types render as a single-choice radiogroup with the current type checked", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    expect(within(group).getAllByRole("radio")).toHaveLength(5);
    expect(
      within(group).getByRole("radio", { name: /Built-in Agent/ }),
    ).toBeChecked();
    expect(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    ).not.toBeChecked();

    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    expect(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    ).toBeChecked();
    expect(
      within(group).getByRole("radio", { name: /Built-in Agent/ }),
    ).not.toBeChecked();
  });

  it("Given local CLIs on $PATH, When the create dialog opens, Then every CLI type is probed and shows an installed / not-installed badge", async () => {
    const user = userEvent.setup();
    const resolveFn = vi.fn((req: unknown) => {
      const { type } = req as { type: string };
      return Promise.resolve(
        type === "piagent"
          ? { path: "", found: false }
          : { path: `/usr/local/bin/${type}`, found: true },
      );
    });
    installAppMock({ ResolveAgentBackendCLIPath: resolveFn });

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    await waitFor(() => {
      expect(
        within(
          within(group).getByRole("radio", { name: /Claude Code CLI/ }),
        ).getByText("Installed"),
      ).toBeInTheDocument();
      expect(
        within(
          within(group).getByRole("radio", { name: /Pi Agent CLI/ }),
        ).getByText("Not installed"),
      ).toBeInTheDocument();
    });

    for (const type of ["claudecode", "codex", "piagent"]) {
      expect(resolveFn).toHaveBeenCalledWith(
        expect.objectContaining({ type, deviceId: "" }),
      );
    }
  });

  it("Given probes still in flight, When the create dialog has just opened, Then CLI types show a detecting badge and stay selectable", async () => {
    const user = userEvent.setup();
    let release: (v: { path: string; found: boolean }) => void = () => {};
    installAppMock({
      ResolveAgentBackendCLIPath: vi.fn(
        () =>
          new Promise<{ path: string; found: boolean }>((resolve) => {
            release = resolve;
          }),
      ),
    });

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });
    const codex = within(group).getByRole("radio", { name: /Codex CLI/ });

    await waitFor(() =>
      expect(within(codex).getByText(/Detecting/)).toBeInTheDocument(),
    );
    expect(codex).toBeEnabled();

    await user.click(codex);
    expect(
      within(group).getByRole("radio", { name: /Codex CLI/ }),
    ).toBeChecked();

    release({ path: "/usr/local/bin/codex", found: true });
  });

  it("Given a probe that already answered, When that CLI type is selected, Then the path is reused without another round-trip", async () => {
    const user = userEvent.setup();
    const resolveFn = vi.fn((req: unknown) => {
      const { type } = req as { type: string };
      return Promise.resolve({ path: `/usr/local/bin/${type}`, found: true });
    });
    installAppMock({ ResolveAgentBackendCLIPath: resolveFn });

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });
    await waitFor(() =>
      expect(
        within(
          within(group).getByRole("radio", { name: /Claude Code CLI/ }),
        ).getByText("Installed"),
      ).toBeInTheDocument(),
    );

    // 探测已经给出结论了，再点这个类型不该重新拨一次 —— 远端设备上这是一次真实的网络往返，
    // 而方向键会逐个 onChange，代价按键盘步数累加。
    const callsBeforeSelect = resolveFn.mock.calls.length;
    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    await waitFor(() => {
      const input = within(dialog).getByPlaceholderText(
        "/usr/local/bin/claude",
      ) as HTMLInputElement;
      expect(input.value).toBe("/usr/local/bin/claudecode");
    });
    expect(resolveFn.mock.calls.length).toBe(callsBeforeSelect);
  });

  it("Given non-CLI types, When the picker renders, Then Built-in carries a local-only badge and OpenClaw carries no badge", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    expect(
      within(
        within(group).getByRole("radio", { name: /Built-in Agent/ }),
      ).getByText("Local only"),
    ).toBeInTheDocument();
    expect(
      within(group).getByRole("radio", { name: /OpenClaw Gateway/ }),
    ).toHaveAccessibleName("OpenClaw Gateway");
  });

  it("Given a remote device is selected, When the CLI probe re-runs, Then it targets that device and refreshes the badges", async () => {
    const user = userEvent.setup();
    const resolveFn = vi.fn((req: unknown) => {
      const { type, deviceId } = req as { type: string; deviceId: string };
      // 本机没装 codex，远端 linux-srv 装了 —— 徽标必须跟着设备变。
      return Promise.resolve(
        deviceId === "7" && type === "codex"
          ? { path: "/opt/codex", found: true }
          : { path: "", found: false },
      );
    });
    installAppMock({
      ResolveAgentBackendCLIPath: resolveFn,
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
    });

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    await waitFor(() =>
      expect(
        within(
          within(group).getByRole("radio", { name: /Codex CLI/ }),
        ).getByText("Not installed"),
      ).toBeInTheDocument(),
    );

    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));

    await waitFor(() => {
      expect(
        within(
          within(group).getByRole("radio", { name: /Codex CLI/ }),
        ).getByText("Installed"),
      ).toBeInTheDocument();
    });
    expect(resolveFn).toHaveBeenCalledWith(
      expect.objectContaining({ type: "codex", deviceId: "7" }),
    );
  });

  it("Given an unreachable remote device, When the probe rejects, Then the badge says the probe failed instead of claiming the CLI is missing", async () => {
    const user = userEvent.setup();
    installAppMock({
      ResolveAgentBackendCLIPath: vi.fn((req: unknown) => {
        const { deviceId } = req as { deviceId: string };
        return deviceId === "7"
          ? Promise.reject(new Error("device offline"))
          : Promise.resolve({ path: "", found: false });
      }),
      RemoteDeviceList: vi.fn(() =>
        Promise.resolve([{ id: 7, name: "linux-srv", online: true }]),
      ),
    });

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));

    await waitFor(() => {
      expect(
        within(
          within(group).getByRole("radio", { name: /Codex CLI/ }),
        ).getByText("Probe failed"),
      ).toBeInTheDocument();
    });
    expect(
      within(
        within(group).getByRole("radio", { name: /Codex CLI/ }),
      ).queryByText("Not installed"),
    ).not.toBeInTheDocument();
  });

  it("Given the type field, When the create dialog renders, Then it precedes the name field", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });
    const nameInput = within(dialog).getByPlaceholderText(
      "Example: Local · Claude Code",
    );

    expect(
      group.compareDocumentPosition(nameInput) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("Given an untouched name, When the type changes, Then the name is prefilled from the type and keeps following further changes", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });
    const nameInput = within(dialog).getByPlaceholderText(
      "Example: Local · Claude Code",
    ) as HTMLInputElement;

    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );
    expect(nameInput.value).toBe("Local · Claude Code");

    await user.click(within(group).getByRole("radio", { name: /Codex CLI/ }));
    expect(nameInput.value).toBe("Local · Codex");
  });

  it("Given a name the user typed, When the type changes, Then the typed name is preserved", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });
    const nameInput = within(dialog).getByPlaceholderText(
      "Example: Local · Claude Code",
    ) as HTMLInputElement;

    fireEvent.change(nameInput, { target: { value: "my backend" } });
    await user.click(
      within(group).getByRole("radio", { name: /Claude Code CLI/ }),
    );

    expect(nameInput.value).toBe("my backend");
  });

  it("Given an existing backend, When the edit dialog opens, Then the type is a read-only summary with a locked hint and no radiogroup", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListAgentBackends: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "claudecode",
              name: "本机 claude",
              llmProviderKey: "",
              cliPath: "/usr/local/bin/claude",
              agentCount: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByText("本机 claude");
    const row = screen
      .getByText("本机 claude")
      .closest('[role="listitem"]') as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).queryByRole("radiogroup", { name: "Type" }),
    ).not.toBeInTheDocument();
    expect(within(dialog).getByText("Claude Code CLI")).toBeInTheDocument();
    expect(
      within(dialog).getByText("Cannot be changed after creation"),
    ).toBeInTheDocument();
  });

  it("Given keyboard focus inside the group, When arrow keys are pressed, Then the checked type moves without a mouse", async () => {
    const user = userEvent.setup();
    installAppMock();

    const dialog = await openCreateDialog(user);
    const group = within(dialog).getByRole("radiogroup", { name: "Type" });

    within(group)
      .getByRole("radio", { name: /Built-in Agent/ })
      .focus();
    await user.keyboard("{ArrowDown}");

    expect(
      within(group).getByRole("radio", { name: /OpenClaw Gateway/ }),
    ).toBeChecked();
    expect(
      within(group).getByRole("radio", { name: /OpenClaw Gateway/ }),
    ).toHaveFocus();

    await user.keyboard("{ArrowUp}{ArrowUp}");
    expect(
      within(group).getByRole("radio", { name: /Pi Agent CLI/ }),
    ).toBeChecked();
  });
});

describe("truncateFlashText", () => {
  it("短文本原样返回，truncated=false", () => {
    const r = truncateFlashText("✅ 128ms · pong");
    expect(r.display).toBe("✅ 128ms · pong");
    expect(r.truncated).toBe(false);
    expect(r.full).toBe("✅ 128ms · pong");
  });

  it("超过 80 字时截断 + …，truncated=true，full 保留原文", () => {
    const long = "a".repeat(300);
    const r = truncateFlashText(long);
    expect(r.truncated).toBe(true);
    expect(r.display.endsWith("…")).toBe(true);
    expect(r.display.length).toBeLessThanOrEqual(81); // 80 + …
    expect(r.full).toBe(long);
  });

  it("换行/制表符压成单空格防止 flash 行高被撑起", () => {
    const r = truncateFlashText("line1\nline2\t\tline3");
    expect(r.display).toBe("line1 line2 line3");
  });
});
