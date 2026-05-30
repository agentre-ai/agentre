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
  DeleteAgentBackend: vi.fn(),
  GetGatewayStatus: vi.fn(),
  ListAgentBackends: vi.fn(),
  ListLLMProviders: vi.fn(),
  RemoteDeviceList: vi.fn(),
  RemoteDeviceListProviders: vi.fn(),
  RemoteDeviceSyncProvider: vi.fn(),
  ResolveAgentBackendCLIPath: vi.fn(),
  TestAgentBackend: vi.fn(),
  UpdateAgentBackend: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { AgentBackendsPanel } from "../agent-backends";

type AnyFn = (...args: unknown[]) => unknown;

type AppMockShape = {
  ListAgentBackends: AnyFn;
  ListLLMProviders: AnyFn;
  CreateAgentBackend?: AnyFn;
  UpdateAgentBackend?: AnyFn;
  DeleteAgentBackend?: AnyFn;
  TestAgentBackend?: AnyFn;
  CancelTestAgentBackend?: AnyFn;
  GetGatewayStatus?: AnyFn;
  ResolveAgentBackendCLIPath?: AnyFn;
  RemoteDeviceList?: AnyFn;
  RemoteDeviceListProviders?: AnyFn;
  RemoteDeviceSyncProvider?: AnyFn;
};

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
            model: "claude-sonnet-4-6",
            maxOutput: 0,
            contextWindow: 0,
            createtime: 0,
            updatetime: 0,
          },
        ],
      }),
    ),
    CreateAgentBackend: vi.fn(() => Promise.resolve({ item: { id: 2 } })),
    UpdateAgentBackend: vi.fn(() => Promise.resolve({ item: { id: 1 } })),
    DeleteAgentBackend: vi.fn(() => Promise.resolve({})),
    TestAgentBackend: vi.fn(() =>
      Promise.resolve({ ok: true, latencyMs: 0, message: "" }),
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

    const table = await screen.findByRole("table", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(within(table).getByText("默认助手")).toBeInTheDocument();
      expect(
        within(table).getByText(/Anthropic · claude-sonnet-4-6/),
      ).toBeInTheDocument();
    });
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

    const table = await screen.findByRole("table", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(within(table).getByText("孤儿后端")).toBeInTheDocument();
      expect(within(table).getByText("Needs action")).toBeInTheDocument();
    });
  });

  it("submits create dialog with builtin type", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("table", { name: "Agent backend list" });
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

    const row = screen.getByText("默认助手").closest("tr") as HTMLElement;
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

    const row = screen.getByText("默认助手").closest("tr") as HTMLElement;
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

    const table = await screen.findByRole("table", {
      name: "Agent backend list",
    });
    await waitFor(() => {
      expect(
        within(table).getByText("无 provider 的 claude"),
      ).toBeInTheDocument();
      expect(within(table).getByText(/Use CLI login/)).toBeInTheDocument();
      expect(within(table).queryByText("Needs action")).not.toBeInTheDocument();
    });
  });

  it.each([
    ["claudecode", "无 provider 的 claude", "Anthropic", "anthropic"],
    ["codex", "无 provider 的 codex", "OpenAI", "openai-response"],
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
      const row = screen.getByText(name).closest("tr") as HTMLElement;
      await user.click(within(row).getByRole("button", { name: /Edit/ }));

      const dialog = await screen.findByRole("dialog");
      expect(
        within(dialog).queryByText(/original LLM provider is disabled/),
      ).not.toBeInTheDocument();
      expect(
        within(dialog).getByText(/No link \(use CLI login\)/),
      ).toBeInTheDocument();
    },
  );

  it("新建 claudecode 时允许不选 provider 提交 llmProviderKey 空串", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "claude 走自身登录" } },
    );

    // 切换到 Claude Code CLI 类型 → provider 默认为空（CLI 自身登录）。
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "本地 claude" } },
    );
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
    );

    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));

    await user.click(
      within(dialog).getByRole("combobox", { name: "LLM Provider" }),
    );
    await user.click(screen.getByRole("option", { name: /Anthropic/ }));

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    const syncDialog = await screen.findByRole("dialog", {
      name: /Sync Remote LLM Provider/,
    });
    expect(
      within(syncDialog).getByText(/API key to the remote agentred state file/),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("button", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "LLM Provider" }),
    );
    await user.click(screen.getByRole("option", { name: /Anthropic/ }));

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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("button", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "LLM Provider" }),
    );
    await user.click(screen.getByRole("option", { name: /Anthropic/ }));
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(editorDialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(editorDialog).getByRole("button", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(editorDialog).getByRole("combobox", { name: "LLM Provider" }),
    );
    await user.click(screen.getByRole("option", { name: /Anthropic/ }));
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

  it("保存远端 claudecode 且 provider 已在远端时直接保存，不弹同步提示", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "已同步 claude" } },
    );
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
    );
    await user.click(
      within(dialog).getByRole("combobox", { name: "Runtime Device" }),
    );
    await user.click(screen.getByRole("option", { name: /linux-srv/ }));
    await user.click(
      within(dialog).getByRole("combobox", { name: "LLM Provider" }),
    );
    await user.click(screen.getByRole("option", { name: /Anthropic/ }));

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
      .closest("tr") as HTMLElement;
    await user.click(within(row).getByRole("button", { name: /Edit/ }));

    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /Clear provider link/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: /Codex CLI/ }));

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

  it("codex 思考力度开放 xhigh，保存时透传 reasoningEffort=xhigh", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<AgentBackendsPanel />);

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "codex xhigh" } },
    );
    await user.click(within(dialog).getByRole("button", { name: /Codex CLI/ }));

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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
    );

    const input = within(dialog).getByPlaceholderText(
      "/usr/local/bin/claude",
    ) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("/first/claude"));

    // 用户手改了值，然后点按钮重识别 → 按钮要覆盖手填值。
    fireEvent.change(input, { target: { value: "/wrong/path" } });
    nextPath = "/second/claude";
    await user.click(within(dialog).getByRole("button", { name: /Detect/ }));

    await waitFor(() => expect(input.value).toBe("/second/claude"));
    expect(resolveFn).toHaveBeenCalledTimes(2);
  });

  it("自动识别按钮未命中时显示 $PATH 提示且不改 input", async () => {
    const user = userEvent.setup();
    installAppMock({
      ResolveAgentBackendCLIPath: vi.fn(() =>
        Promise.resolve({ path: "", found: false }),
      ),
    });
    render(<AgentBackendsPanel />);

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: /Codex CLI/ }));

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

    const row = screen.getByText("默认助手").closest("tr") as HTMLElement;
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

    const row = screen.getByText("默认助手").closest("tr") as HTMLElement;
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByPlaceholderText("Example: Local · Claude Code"),
      { target: { value: "远端 claude" } },
    );
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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

    await screen.findByRole("table", { name: "Agent backend list" });
    await user.click(screen.getByRole("button", { name: /New Backend/ }));

    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /Claude Code CLI/ }),
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
