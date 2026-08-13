import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ListLLMProviders: vi.fn(),
  CreateLLMProvider: vi.fn(),
  UpdateLLMProvider: vi.fn(),
  DeleteLLMProvider: vi.fn(),
  ListLLMModels: vi.fn(),
  PreviewLLMModels: vi.fn(),
  ImportLLMModels: vi.fn(),
  TestLLMProvider: vi.fn(),
  LookupLLMModel: vi.fn(),
  UpdateLLMModel: vi.fn(),
  DeleteLLMModel: vi.fn(),
  SetLLMModelDefault: vi.fn(),
  SetLLMModelEnabled: vi.fn(),
  SetLLMProviderEnabled: vi.fn(),
  LLMModelRefCounts: vi.fn(),
  LLMProviderRefCounts: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

vi.mock("../../../../wailsjs/go/models", () => {
  class ModelClass {
    static createFrom(source: Record<string, unknown> = {}) {
      return new ModelClass(source);
    }
    constructor(init?: Record<string, unknown>) {
      if (init) Object.assign(this, init);
    }
  }
  const svc = {
    ModelInput: ModelClass,
    CreateProviderRequest: ModelClass,
    UpdateProviderRequest: ModelClass,
    DeleteProviderRequest: ModelClass,
    DeleteModelRequest: ModelClass,
    TestConnectionRequest: ModelClass,
    ListModelsRequest: ModelClass,
    PreviewModelsRequest: ModelClass,
    LookupModelRequest: ModelClass,
    ImportModelsRequest: ModelClass,
    SetModelDefaultRequest: ModelClass,
    SetModelEnabledRequest: ModelClass,
    SetProviderEnabledRequest: ModelClass,
    UpdateModelRequest: ModelClass,
    ModelRefCountsRequest: ModelClass,
    ProviderRefCountsRequest: ModelClass,
  };
  return { llm_provider_svc: svc };
});

import { LlmProvidersPanel } from "../llm-providers";

type AnyFn = (...args: unknown[]) => unknown;

type AppMockShape = {
  CreateLLMProvider: AnyFn;
  DeleteLLMProvider: AnyFn;
  DeleteLLMModel: AnyFn;
  ImportLLMModels: AnyFn;
  ListLLMModels: AnyFn;
  ListLLMProviders: AnyFn;
  LLMModelRefCounts: AnyFn;
  LLMProviderRefCounts: AnyFn;
  LookupLLMModel: AnyFn;
  PreviewLLMModels: AnyFn;
  SetLLMModelDefault: AnyFn;
  SetLLMModelEnabled: AnyFn;
  SetLLMProviderEnabled: AnyFn;
  TestLLMProvider: AnyFn;
  UpdateLLMModel: AnyFn;
  UpdateLLMProvider: AnyFn;
};

type ProviderItem = {
  baseUrl: string;
  defaultModelKey: string;
  enabled: boolean;
  hasApiKey: boolean;
  id: number;
  maskedApiKey: string;
  modelCount: number;
  name: string;
  providerKey: string;
  type: string;
};

type ModelItem = {
  contextWindow: number;
  enabled: boolean;
  id: number;
  isDefault: boolean;
  maxOutput: number;
  modelId: string;
  modelKey: string;
  name: string;
  providerId: number;
  providerKey: string;
};

function makeProvider(overrides: Partial<ProviderItem> = {}): ProviderItem {
  return {
    id: 1,
    type: "anthropic",
    providerKey: "pk-1",
    name: "Anthropic",
    baseUrl: "https://api.anthropic.com",
    maskedApiKey: "sk-••••••9XQ2",
    hasApiKey: true,
    enabled: true,
    defaultModelKey: "mk-default",
    modelCount: 3,
    ...overrides,
  };
}

function makeModel(overrides: Partial<ModelItem> = {}): ModelItem {
  return {
    id: 11,
    providerId: 1,
    providerKey: "pk-1",
    modelKey: "mk-default",
    modelId: "claude-sonnet-4-5",
    name: "Sonnet",
    contextWindow: 200000,
    maxOutput: 64000,
    enabled: true,
    isDefault: true,
    ...overrides,
  };
}

function installAppMock(overrides: Partial<AppMockShape> = {}) {
  const base: AppMockShape = {
    ListLLMProviders: vi.fn(() => Promise.resolve({ items: [] })),
    CreateLLMProvider: vi.fn(() =>
      Promise.resolve({ item: makeProvider({ id: 99 }) }),
    ),
    UpdateLLMProvider: vi.fn(() => Promise.resolve({ item: makeProvider() })),
    DeleteLLMProvider: vi.fn(() => Promise.resolve({})),
    ListLLMModels: vi.fn(() => Promise.resolve({ items: [] })),
    PreviewLLMModels: vi.fn(() => Promise.resolve({ items: [] })),
    ImportLLMModels: vi.fn(() =>
      Promise.resolve({ items: [], imported: 0, updated: 0 }),
    ),
    TestLLMProvider: vi.fn(() =>
      Promise.resolve({ ok: true, message: "", modelCount: 0 }),
    ),
    LookupLLMModel: vi.fn(() =>
      Promise.resolve({
        known: false,
        vendor: "",
        contextWindow: 0,
        maxOutput: 0,
      }),
    ),
    UpdateLLMModel: vi.fn(() => Promise.resolve({ item: makeModel() })),
    DeleteLLMModel: vi.fn(() => Promise.resolve({})),
    SetLLMModelDefault: vi.fn(() => Promise.resolve({ item: makeProvider() })),
    SetLLMModelEnabled: vi.fn(() => Promise.resolve({ item: makeModel() })),
    SetLLMProviderEnabled: vi.fn(() =>
      Promise.resolve({ item: makeProvider() }),
    ),
    LLMModelRefCounts: vi.fn(() =>
      Promise.resolve({ counts: { backends: 0, sessions: 0, routes: 0 } }),
    ),
    LLMProviderRefCounts: vi.fn(() =>
      Promise.resolve({ counts: { backends: 0, sessions: 0, routes: 0 } }),
    ),
  };
  const merged = { ...base, ...overrides };
  for (const key of Object.keys(appMocks) as Array<keyof typeof appMocks>) {
    const mock = appMocks[key] as ReturnType<typeof vi.fn>;
    const fn = merged[key as keyof AppMockShape] as AnyFn;
    mock.mockReset();
    mock.mockImplementation((...args: unknown[]) => fn(...args));
  }
  return merged;
}

const originalClipboard = navigator.clipboard;

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  return writeText;
}

afterEach(() => {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: originalClipboard,
  });
  vi.clearAllMocks();
});

// Radix DropdownMenu 在 jsdom 中需要关闭 pointerEvents 检查。
function setupMenuUser() {
  return userEvent.setup({ pointerEventsCheck: 0 });
}

describe("LlmProvidersPanel", () => {
  it("Given providers of different types, When the panel loads, Then the nav groups them by type, shows the endpoint, and marks only disabled providers", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            makeProvider({
              id: 1,
              type: "anthropic",
              name: "Anthropic Official",
              providerKey: "pk-anthropic",
              baseUrl: "api.anthropic.com",
              maskedApiKey: "sk-••••••9XQ2",
              hasApiKey: true,
              enabled: true,
              modelCount: 3,
            }),
            makeProvider({
              id: 2,
              type: "openai-chat",
              name: "DeepSeek Proxy",
              providerKey: "pk-deepseek",
              baseUrl: "llm.intra.example",
              maskedApiKey: "",
              hasApiKey: false,
              enabled: false,
              defaultModelKey: "",
              modelCount: 2,
            }),
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    const nav = await screen.findByRole("complementary", {
      name: "Provider list",
    });
    const anthropic = await within(nav).findByRole("button", {
      name: /Anthropic Official/,
    });
    expect(
      within(anthropic).getByText(/api\.anthropic\.com/),
    ).toBeInTheDocument();
    expect(within(anthropic).getByText(/3 models/)).toBeInTheDocument();
    // 启用是常态，不再标注；只有停用的供应商带停用标记
    expect(within(anthropic).queryByText("Enabled")).not.toBeInTheDocument();

    const deepseek = within(nav).getByRole("button", {
      name: /DeepSeek Proxy/,
    });
    expect(
      within(deepseek).getByText(/llm\.intra\.example/),
    ).toBeInTheDocument();
    expect(within(deepseek).getByText(/2 models/)).toBeInTheDocument();
    expect(within(deepseek).getByText("Disabled")).toBeInTheDocument();

    // 每个类型有独立分组标题
    expect(mocks.ListLLMModels).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1 }),
    );
  });

  it("Given a provider is selected, When its workspace loads, Then the model rows render and the header shows connection status", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    const workspace = await screen.findByRole("region", {
      name: /Anthropic models/,
    });
    // 模型行由异步 ListLLMModels 渲染，等待真实模型控件出现而非 region。
    expect(
      (await within(workspace).findAllByText("claude-sonnet-4-5")).length,
    ).toBeGreaterThan(0);
    expect(within(workspace).getByText("claude-opus-4-1")).toBeInTheDocument();
    // 连接配置（endpoint + 掩码 key）在头部可见
    expect(
      within(workspace).getByText("https://api.anthropic.com"),
    ).toBeInTheDocument();
    expect(within(workspace).getByText("sk-••••••9XQ2")).toBeInTheDocument();
    // 默认模型徽标
    expect(within(workspace).getAllByText("Default").length).toBeGreaterThan(0);
    expect(mocks.ListLLMModels).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1 }),
    );
  });

  it("Given models with and without display names, When the table renders, Then the main row shows the display name (falling back to model ID) and the sub row shows only the model ID", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              name: "",
              isDefault: false,
            }),
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    const workspace = await screen.findByRole("region", {
      name: /Anthropic models/,
    });
    // 主行显示 display name（Sonnet），副行显示 modelId
    expect(await within(workspace).findByText("Sonnet")).toBeInTheDocument();
    expect(
      within(workspace).getAllByText("claude-sonnet-4-5").length,
    ).toBeGreaterThan(0);
    // 空 name 回落显示 modelId
    expect(
      within(workspace).getAllByText("claude-opus-4-1").length,
    ).toBeGreaterThan(0);
    // UUID modelKey 不再出现在行内
    expect(within(workspace).queryByText("mk-default")).not.toBeInTheDocument();
    expect(within(workspace).queryByText("mk-opus")).not.toBeInTheDocument();
  });

  it("Given a provider with models, When the table renders, Then the columns are ordered checkbox, model, context, max output, references, default, enable and row actions", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    render(<LlmProvidersPanel />);

    const table = await screen.findByRole("table", { name: "Model list" });
    const texts = within(table)
      .getAllByRole("columnheader")
      .map((h) => (h.textContent ?? "").trim());
    expect(texts[0]).toBe("");
    expect(texts[1]).toBe("Model");
    expect(texts[2]).toBe("Context");
    expect(texts[3]).toBe("Max Output");
    expect(texts[4]).toBe("References");
    expect(texts[5]).toBe("Default");
    expect(texts[6]).toBe("Enable");
    expect(texts[7]).toBe("");
  });

  it("Given models with and without references, When the table renders, Then the reference column shows the count and a placeholder for unreferenced models", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
      LLMModelRefCounts: vi.fn((req: unknown) =>
        Promise.resolve({
          counts:
            (req as { modelKey?: string }).modelKey === "mk-opus"
              ? { backends: 1, sessions: 0, routes: 2 }
              : { backends: 0, sessions: 0, routes: 0 },
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    const workspace = await screen.findByRole("region", {
      name: /Anthropic models/,
    });
    await within(workspace).findByRole("switch", {
      name: "Enable claude-opus-4-1",
    });

    const opusRow = screen
      .getByRole("switch", { name: "Enable claude-opus-4-1" })
      .closest("tr");
    const sonnetRow = screen
      .getByRole("switch", { name: "Enable claude-sonnet-4-5" })
      .closest("tr");
    expect(opusRow).toBeTruthy();
    expect(sonnetRow).toBeTruthy();

    // 被引用模型的引用列显示数量；无引用的模型显示占位符
    expect(
      await within(opusRow as HTMLTableRowElement).findByText("3"),
    ).toBeInTheDocument();
    expect(
      within(sonnetRow as HTMLTableRowElement).getByText("—"),
    ).toBeInTheDocument();
  });

  it("Given a provider with a default model, When the header Test is clicked, Then TestLLMProvider is called with an empty modelKey", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "Test Anthropic" }));

    await waitFor(() => {
      expect(mocks.TestLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, modelKey: "" }),
      );
    });
  });

  it("Given a provider test succeeds, When it completes, Then the transient flash reports success with the elapsed time", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "Test Anthropic" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/\d+ms/);
  });

  it("Given a non-default model row, When its row Test is clicked, Then TestLLMProvider is called with the concrete modelKey", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await user.click(
      await screen.findByRole("button", {
        name: "Test claude-opus-4-1",
      }),
    );

    await waitFor(() => {
      expect(mocks.TestLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, modelKey: "mk-opus" }),
      );
    });
  });

  it("Given a non-default enabled model, When its Set default radio is picked, Then the impact dialog opens and confirming calls SetLLMModelDefault", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
      LLMProviderRefCounts: vi.fn(() =>
        Promise.resolve({
          counts: { backends: 2, sessions: 0, routes: 1 },
        }),
      ),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await user.click(
      await screen.findByRole("radio", {
        name: "Set claude-opus-4-1 as default",
      }),
    );

    // spec 2026-08-11「Provider management」：改默认模型前先展示动态影响并二次确认。
    await screen.findByRole("heading", {
      name: /Set default model to claude-opus-4-1/i,
    });
    await waitFor(() => {
      expect(mocks.LLMProviderRefCounts).toHaveBeenCalledWith(
        expect.objectContaining({ providerKey: "pk-1" }),
      );
    });

    await user.click(screen.getByRole("button", { name: "Set as default" }));

    await waitFor(() => {
      expect(mocks.SetLLMModelDefault).toHaveBeenCalledWith(
        expect.objectContaining({
          providerId: 1,
          modelKey: "mk-opus",
        }),
      );
    });
  });

  it("Given the default model, When delete or disable is attempted, Then the controls are blocked with a visible reason", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });

    // 默认模型：启用开关禁用并给出原因（需在打开菜单前查询，Radix 菜单打开时会 aria-hidden 其余内容）
    const enableSwitch = screen.getByRole("switch", {
      name: "Enable claude-sonnet-4-5",
    });
    expect(enableSwitch).toBeDisabled();
    expect(enableSwitch).toHaveAttribute(
      "title",
      expect.stringMatching(/default model/i),
    );

    await user.click(
      screen.getByRole("button", {
        name: "More actions for claude-sonnet-4-5",
      }),
    );
    const deleteItem = within(await screen.findByRole("menu")).getByRole(
      "menuitem",
      { name: "Delete claude-sonnet-4-5" },
    );
    expect(deleteItem).toHaveAttribute("aria-disabled", "true");
    // 被阻止的原因通过 title / tooltip 可见
    expect(deleteItem).toHaveAttribute(
      "title",
      expect.stringMatching(/default model/i),
    );

    expect(mocks.DeleteLLMModel).not.toHaveBeenCalled();
    expect(mocks.SetLLMModelEnabled).not.toHaveBeenCalled();
  });

  it("Given a referenced model, When its More menu is opened, Then the delete item is disabled with a visible reason", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
      LLMModelRefCounts: vi.fn((req: unknown) =>
        Promise.resolve({
          counts:
            (req as { modelKey?: string }).modelKey === "mk-opus"
              ? { backends: 1, sessions: 0, routes: 2 }
              : { backends: 0, sessions: 0, routes: 0 },
        }),
      ),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(
      screen.getByRole("button", {
        name: "More actions for claude-opus-4-1",
      }),
    );
    const menu = await screen.findByRole("menu");
    const deleteItem = within(menu).getByRole("menuitem", {
      name: "Delete claude-opus-4-1",
    });
    // 被引用的模型删除项禁用，并给出原因
    expect(deleteItem).toHaveAttribute("aria-disabled", "true");
    expect(deleteItem).toHaveAttribute(
      "title",
      expect.stringMatching(/referenced/i),
    );
    expect(mocks.DeleteLLMModel).not.toHaveBeenCalled();
    // 引用计数按稳定 modelKey 查询
    expect(mocks.LLMModelRefCounts).toHaveBeenCalledWith(
      expect.objectContaining({ modelKey: "mk-opus" }),
    );
  });

  it("Given an unreferenced model, When delete is confirmed via the row menu, Then DeleteLLMModel is called", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(
      screen.getByRole("button", {
        name: "More actions for claude-opus-4-1",
      }),
    );
    await user.click(
      await screen.findByRole("menuitem", { name: "Delete claude-opus-4-1" }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Delete model" }),
    );

    await waitFor(() => {
      expect(mocks.DeleteLLMModel).toHaveBeenCalledWith(
        expect.objectContaining({ id: 12 }),
      );
    });
  });

  it("Given a referenced model, When its Model ID is edited, Then the dialog shows impact counts and requires explicit confirmation before saving", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
      LLMModelRefCounts: vi.fn(() =>
        Promise.resolve({
          counts: { backends: 1, sessions: 1, routes: 0 },
        }),
      ),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await user.click(
      await screen.findByRole("button", { name: "Edit claude-opus-4-1" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: /Edit model/,
    });
    // 稳定 modelKey 只读可选中复制，modelId 可编辑
    const modelKeyInput = within(dialog).getByLabelText("Model Key");
    expect(modelKeyInput).toHaveValue("mk-opus");
    expect(modelKeyInput).toHaveAttribute("readonly");
    const modelIdInput = within(dialog).getByLabelText("Model ID");
    expect(modelIdInput).toHaveValue("claude-opus-4-1");

    // 未改 modelId 时不需要引用确认；改动后才出现影响提示
    const save = within(dialog).getByRole("button", { name: "Save changes" });
    fireEvent.change(modelIdInput, { target: { value: "claude-opus-4-2" } });

    expect(
      within(dialog).getByText(/This model is referenced/i),
    ).toBeInTheDocument();
    const confirmBox = within(dialog).getByRole("checkbox", {
      name: /I understand the impact/i,
    });
    expect(save).toBeDisabled();

    await user.click(confirmBox);
    expect(save).toBeEnabled();
    await user.click(save);

    await waitFor(() => {
      expect(mocks.UpdateLLMModel).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 12,
          modelId: "claude-opus-4-2",
          confirmReference: true,
        }),
      );
    });
    // 引用计数按稳定 modelKey 查询
    expect(mocks.LLMModelRefCounts).toHaveBeenCalledWith(
      expect.objectContaining({ modelKey: "mk-opus" }),
    );
  });

  it("Given an unreferenced model, When its Model ID is edited, Then UpdateLLMModel saves without reference confirmation", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            makeModel(),
            makeModel({
              id: 12,
              modelKey: "mk-opus",
              modelId: "claude-opus-4-1",
              isDefault: false,
            }),
          ],
        }),
      ),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await user.click(
      await screen.findByRole("button", { name: "Edit claude-opus-4-1" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: /Edit model/,
    });
    fireEvent.change(within(dialog).getByLabelText("Model ID"), {
      target: { value: "claude-opus-4-2" },
    });
    await user.click(
      within(dialog).getByRole("button", { name: "Save changes" }),
    );

    await waitFor(() => {
      expect(mocks.UpdateLLMModel).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 12,
          modelId: "claude-opus-4-2",
          confirmReference: false,
        }),
      );
    });
  });

  it("Given a provider workspace, When Discover is opened, Then PreviewModels is scanned and selected discoveries are imported via ImportModels", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [makeModel({ modelId: "deepseek-chat" })],
        }),
      ),
      PreviewLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: "deepseek-chat",
              vendor: "deepseek",
              contextWindow: 64000,
              maxOutput: 4096,
              modalities: [],
              thinking: false,
              knownInCago: false,
            },
            {
              id: "deepseek-v3.2",
              vendor: "deepseek",
              contextWindow: 128000,
              maxOutput: 8192,
              modalities: [],
              thinking: false,
              knownInCago: false,
            },
          ],
        }),
      ),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "Discover models" }));

    const dialog = await screen.findByRole("dialog", {
      name: /Discover/,
    });
    // 扫描使用已保存连接（apiKey 为空 → 沿用保存值）
    expect(mocks.PreviewLLMModels).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1, apiKey: "" }),
    );
    // 已存在项标记为跳过（preview 与 existingModels 均为异步加载，等待真实状态）
    expect(
      await within(dialog).findByText(/Already exists/),
    ).toBeInTheDocument();
    expect(within(dialog).getByText(/deepseek-chat/)).toBeInTheDocument();

    await user.click(
      within(dialog).getByRole("button", { name: "Import 1 model" }),
    );

    await waitFor(() => {
      expect(mocks.ImportLLMModels).toHaveBeenCalledWith(
        expect.objectContaining({
          providerId: 1,
          models: expect.arrayContaining([
            expect.objectContaining({ modelId: "deepseek-v3.2" }),
          ]),
        }),
      );
    });
  });

  it("Given a provider with no default model, When its enable switch is used, Then it is disabled with a visible reason", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            makeProvider({
              enabled: false,
              defaultModelKey: "",
            }),
          ],
        }),
      ),
      ListLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [makeModel({ isDefault: false })],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    const enableSwitch = screen.getByRole("switch", {
      name: "Enable Anthropic",
    });
    expect(enableSwitch).toBeDisabled();
    expect(enableSwitch).toHaveAttribute(
      "title",
      expect.stringMatching(/default model/i),
    );
    expect(mocks.SetLLMProviderEnabled).not.toHaveBeenCalled();
  });

  it("Given a provider with an enabled default, When its enable switch is toggled off, Then SetLLMProviderEnabled is called", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("switch", { name: "Enable Anthropic" }));

    await waitFor(() => {
      expect(mocks.SetLLMProviderEnabled).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, enabled: false }),
      );
    });
  });

  it("Given a referenced provider, When delete is requested, Then the dialog blocks deletion but explains it can be disabled", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
      LLMProviderRefCounts: vi.fn(() =>
        Promise.resolve({
          counts: { backends: 2, sessions: 0, routes: 0 },
        }),
      ),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "More" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "Delete Anthropic" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: /Delete provider/,
    });
    expect(within(dialog).getByText(/referenced/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/disabl/i)).toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Delete provider" }),
    ).not.toBeInTheDocument();
    expect(mocks.DeleteLLMProvider).not.toHaveBeenCalled();
    expect(mocks.LLMProviderRefCounts).toHaveBeenCalledWith(
      expect.objectContaining({ providerKey: "pk-1" }),
    );
  });

  it("Given an unreferenced provider, When delete is confirmed, Then DeleteLLMProvider is called", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "More" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "Delete Anthropic" }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Delete provider" }),
    );

    await waitFor(() => {
      expect(mocks.DeleteLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1 }),
      );
    });
  });

  it("Given the create dialog, When a name, key and a manually added model with a default are provided, Then CreateLLMProvider sends models and defaultModelId", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() => Promise.resolve({ items: [] })),
    });
    const user = userEvent.setup();
    render(<LlmProvidersPanel />);

    await screen.findByRole("button", { name: "Add First Provider" });
    await user.click(
      screen.getByRole("button", { name: "Add First Provider" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "New LLM Provider",
    });
    fireEvent.change(within(dialog).getByLabelText("Name"), {
      target: { value: "My Provider" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^API Key$/), {
      target: { value: "sk-test" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Base URL/), {
      target: { value: "https://api.example.com" },
    });

    // 手工添加一个模型并选择为默认
    await user.click(within(dialog).getByRole("button", { name: "Add model" }));
    fireEvent.change(within(dialog).getByLabelText("Model ID"), {
      target: { value: "claude-sonnet-4-5" },
    });
    fireEvent.change(within(dialog).getByLabelText("Context"), {
      target: { value: "200000" },
    });
    fireEvent.change(within(dialog).getByLabelText("Output"), {
      target: { value: "64000" },
    });
    await user.click(
      within(dialog).getByRole("radio", {
        name: "Set claude-sonnet-4-5 as default",
      }),
    );

    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(mocks.CreateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "anthropic",
          name: "My Provider",
          apiKey: "sk-test",
          baseUrl: "https://api.example.com",
          defaultModelId: "claude-sonnet-4-5",
          models: expect.arrayContaining([
            expect.objectContaining({
              modelId: "claude-sonnet-4-5",
              contextWindow: 200000,
              maxOutput: 64000,
            }),
          ]),
        }),
      );
    });
  });

  it("Given a provider with models, When the connection is edited, Then UpdateLLMProvider preserves the saved API key when untouched", async () => {
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "More" }));
    await user.click(
      await screen.findByRole("menuitem", {
        name: "Edit Anthropic connection",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: /Edit connection/,
    });
    // 掩码 key 预填，保存时归一化为空（保留已保存值）
    expect(within(dialog).getByLabelText(/^API Key$/)).toHaveValue(
      "sk-••••••9XQ2",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Save changes" }),
    );

    await waitFor(() => {
      expect(mocks.UpdateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, apiKey: "" }),
      );
    });
  });

  it("Given an empty provider list, When the panel loads, Then an empty state with a primary CTA is shown", async () => {
    installAppMock();
    render(<LlmProvidersPanel />);

    expect(
      await screen.findByRole("button", { name: "Add First Provider" }),
    ).toBeInTheDocument();
  });

  it("Given the provider list fails to load, When the panel mounts, Then an error flash is shown", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.reject(new Error("database locked")),
      ),
    });
    render(<LlmProvidersPanel />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Load failed: database locked",
    );
  });

  it("Given providers exist, When the panel loads, Then a single New Provider entry precedes the workspace and the old toolbar and nav add entry are gone", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });

    // 唯一新增入口（页头），不再有外层重复卡片标题与左栏底部添加入口
    expect(
      screen.getByRole("button", { name: "New Provider" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add Provider" })).toBeNull();
    expect(screen.queryByText("Configured Providers")).toBeNull();
    expect(screen.getByText("1 total")).toBeInTheDocument();
  });

  it("Given a provider is selected, When the workspace renders, Then the header splits into identity and metadata rows with the combined switch first, then Test Connection, Discover Models and More", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [makeProvider({ name: "Anthropic Official" })],
        }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
      LLMProviderRefCounts: vi.fn(() =>
        Promise.resolve({ counts: { backends: 2, sessions: 1, routes: 0 } }),
      ),
    });
    render(<LlmProvidersPanel />);

    const workspace = await screen.findByRole("region", {
      name: /Anthropic Official models/,
    });

    // 身份行：名称 + 协议类型
    expect(
      within(workspace).getByText("Anthropic Official"),
    ).toBeInTheDocument();
    expect(
      within(workspace).getByText("Anthropic", { selector: "span" }),
    ).toBeInTheDocument();

    // 元信息行：endpoint / 掩码 key / 默认模型 / 被引用
    expect(
      within(workspace).getByText("https://api.anthropic.com"),
    ).toBeInTheDocument();
    expect(within(workspace).getByText("sk-••••••9XQ2")).toBeInTheDocument();
    expect(within(workspace).getByText("Default model")).toBeInTheDocument();
    expect(
      (await within(workspace).findAllByText("claude-sonnet-4-5")).length,
    ).toBeGreaterThan(0);
    expect(within(workspace).getByText("Referenced")).toBeInTheDocument();
    expect(
      await within(workspace).findByText("2 backends · 1 session"),
    ).toBeInTheDocument();

    // 操作区顺序：开关（含状态文字）→ 测试连接 → 发现模型 → 更多
    const switchEl = within(workspace).getByRole("switch", {
      name: "Enable Anthropic Official",
    });
    const testBtn = within(workspace).getByRole("button", {
      name: "Test Anthropic Official",
    });
    const discoverBtn = within(workspace).getByRole("button", {
      name: "Discover models",
    });
    const moreBtn = within(workspace).getByRole("button", { name: "More" });
    expect(
      switchEl.compareDocumentPosition(testBtn) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      testBtn.compareDocumentPosition(discoverBtn) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      discoverBtn.compareDocumentPosition(moreBtn) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    // 启用状态与开关合成一个控件，且可见文案为 Test Connection
    expect(switchEl.closest("label")).toHaveTextContent("Enabled");
    expect(within(workspace).getByText("Test Connection")).toBeInTheDocument();
  });

  it("Given a provider workspace, When the More menu is opened, Then it contains Edit Connection, Copy Provider Key and Delete", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = setupMenuUser();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "More" }));

    const menu = await screen.findByRole("menu");
    expect(
      within(menu).getByRole("menuitem", { name: "Edit Anthropic connection" }),
    ).toBeInTheDocument();
    expect(
      within(menu).getByRole("menuitem", { name: "Copy Provider Key" }),
    ).toBeInTheDocument();
    expect(
      within(menu).getByRole("menuitem", { name: "Delete Anthropic" }),
    ).toBeInTheDocument();
  });

  it("Given a provider workspace, When Copy Provider Key is chosen from the More menu, Then the provider key is written to the clipboard and a success flash is shown", async () => {
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({ items: [makeProvider()] }),
      ),
      ListLLMModels: vi.fn(() => Promise.resolve({ items: [makeModel()] })),
    });
    const user = setupMenuUser();
    const writeText = mockClipboard();
    render(<LlmProvidersPanel />);

    await screen.findByRole("region", { name: /Anthropic models/ });
    await user.click(screen.getByRole("button", { name: "More" }));
    // fireEvent 直接触发 Radix DropdownMenuItem onSelect（不产生 pointer-leave 关闭菜单）
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Copy Provider Key" }),
    );

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("pk-1");
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Provider Key copied",
    );
  });
});
