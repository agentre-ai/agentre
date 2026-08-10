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
  CreateLLMProvider: vi.fn(),
  DeleteLLMProvider: vi.fn(),
  ListLLMProviders: vi.fn(),
  LookupLLMModel: vi.fn(),
  PreviewLLMModels: vi.fn(),
  TestLLMProvider: vi.fn(),
  UpdateLLMProvider: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { LlmProvidersPanel } from "../llm-providers";

type AnyFn = (...args: unknown[]) => unknown;

type AppMockShape = {
  CreateLLMProvider: AnyFn;
  DeleteLLMProvider: AnyFn;
  ListLLMProviders: AnyFn;
  LookupLLMModel: AnyFn;
  PreviewLLMModels: AnyFn;
  TestLLMProvider: AnyFn;
  UpdateLLMProvider: AnyFn;
};

function installAppMock(overrides: Partial<AppMockShape> = {}) {
  const base: AppMockShape = {
    CreateLLMProvider: vi.fn(() => Promise.resolve({ item: { id: 1 } })),
    DeleteLLMProvider: vi.fn(() => Promise.resolve({})),
    ListLLMProviders: vi.fn(() => Promise.resolve({ items: [] })),
    LookupLLMModel: vi.fn(() =>
      Promise.resolve({
        known: false,
        vendor: "",
        contextWindow: 0,
        maxOutput: 0,
      }),
    ),
    PreviewLLMModels: vi.fn(() => Promise.resolve({ items: [] })),
    TestLLMProvider: vi.fn(() =>
      Promise.resolve({ ok: true, message: "", modelCount: 0 }),
    ),
    UpdateLLMProvider: vi.fn(() => Promise.resolve({ item: { id: 1 } })),
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

afterEach(() => {
  vi.clearAllMocks();
});

describe("LlmProvidersPanel", () => {
  it("closes the dialog and shows a panel-level flash on successful create", async () => {
    const user = userEvent.setup();
    installAppMock({
      CreateLLMProvider: vi.fn(() =>
        Promise.resolve({ item: { id: 1, providerKey: "9b1c-uuid" } }),
      ),
      ListLLMProviders: vi
        .fn()
        .mockResolvedValueOnce({ items: [] })
        .mockResolvedValueOnce({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Test",
              providerKey: "9b1c-uuid",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));

    const dialog = await screen.findByRole("dialog");
    // The create dialog no longer surfaces the generated Provider Key.
    expect(
      within(dialog).queryByRole("textbox", { name: "Provider Key" }),
    ).not.toBeInTheDocument();

    fireEvent.change(
      screen.getByPlaceholderText("Example: production / local Ollama"),
      { target: { value: "Test" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "sk-... or self-hosted token. Leave empty for anonymous access.",
      ),
      { target: { value: "sk-test" } },
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    // Success closes the dialog.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    // Panel-level success flash appears.
    expect(screen.getByRole("alert")).toHaveTextContent(
      'Provider "Test" added',
    );
  });

  it("shows the created provider row in the table after the dialog closes", async () => {
    const user = userEvent.setup();
    installAppMock({
      CreateLLMProvider: vi.fn(() =>
        Promise.resolve({ item: { id: 1, providerKey: "created-key" } }),
      ),
      ListLLMProviders: vi
        .fn()
        .mockResolvedValueOnce({ items: [] })
        .mockResolvedValueOnce({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Created",
              providerKey: "created-key",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));

    fireEvent.change(
      screen.getByPlaceholderText("Example: production / local Ollama"),
      { target: { value: "Created" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "sk-... or self-hosted token. Leave empty for anonymous access.",
      ),
      { target: { value: "sk-test" } },
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    // The new row appears in the provider table, not inside the dialog.
    const table = await screen.findByRole("table", {
      name: "LLM provider list",
    });
    await waitFor(() => {
      expect(within(table).getByText("Created")).toBeInTheDocument();
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("shows create failures inside the dialog", async () => {
    const user = userEvent.setup();
    installAppMock({
      CreateLLMProvider: vi.fn(() => Promise.reject(new Error("name exists"))),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      screen.getByPlaceholderText("Example: production / local Ollama"),
      { target: { value: "Duplicate" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "sk-... or self-hosted token. Leave empty for anonymous access.",
      ),
      { target: { value: "sk-test" } },
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(within(dialog).getByRole("alert")).toHaveTextContent(
        "Save failed: name exists",
      );
    });

    expect(
      screen.getAllByRole("alert").filter((alert) => !dialog.contains(alert)),
    ).toHaveLength(0);
  });

  it("copies providerKey to clipboard", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Prod",
              providerKey: "copy-uuid-test",
              baseUrl: "",
              maskedApiKey: "sk-•••",
              hasApiKey: true,
              model: "claude-opus-4-7",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });

    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    render(<LlmProvidersPanel />);

    // Open edit dialog for the existing provider (which has a providerKey).
    const editBtn = await screen.findByRole("button", { name: /Edit Prod/ });
    await user.click(editBtn);

    const dialog = await screen.findByRole("dialog");
    const copyBtn = within(dialog).getByRole("button", {
      name: /Copy Provider Key/,
    });
    await user.click(copyBtn);

    expect(writeText).toHaveBeenCalledWith("copy-uuid-test");
  });

  it("shows the masked API key when editing a configured provider", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Prod",
              providerKey: "p-1",
              baseUrl: "",
              maskedApiKey: "sk-a••••••5678",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit Prod/ }));
    const dialog = await screen.findByRole("dialog");

    const apiKeyInput = within(dialog).getByPlaceholderText(
      "sk-... or self-hosted token. Leave empty for anonymous access.",
    ) as HTMLInputElement;
    expect(apiKeyInput).toHaveValue("sk-a••••••5678");
    expect(
      within(dialog).getByText(/Enter a new value to replace it/),
    ).toBeInTheDocument();
  });

  it("sends apiKey:'' when saving an untouched masked API key", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Prod",
              providerKey: "p-1",
              baseUrl: "",
              maskedApiKey: "sk-a••••••5678",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit Prod/ }));
    const dialog = await screen.findByRole("dialog");

    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.UpdateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ apiKey: "" }),
      );
    });
  });

  it("sends the newly typed API key when editing a configured provider", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Prod",
              providerKey: "p-1",
              baseUrl: "",
              maskedApiKey: "sk-a••••••5678",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit Prod/ }));
    const dialog = await screen.findByRole("dialog");

    const apiKeyInput = within(dialog).getByPlaceholderText(
      "sk-... or self-hosted token. Leave empty for anonymous access.",
    ) as HTMLInputElement;
    fireEvent.change(apiKeyInput, { target: { value: "sk-new-key" } });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.UpdateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ apiKey: "sk-new-key" }),
      );
    });
  });

  it("preserves the saved API key when the masked value has surrounding whitespace", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 1,
              type: "anthropic",
              name: "Prod",
              providerKey: "p-1",
              baseUrl: "",
              maskedApiKey: "sk-a••••••5678",
              hasApiKey: true,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit Prod/ }));
    const dialog = await screen.findByRole("dialog");
    const apiKeyInput = within(dialog).getByPlaceholderText(
      "sk-... or self-hosted token. Leave empty for anonymous access.",
    ) as HTMLInputElement;
    fireEvent.change(apiKeyInput, {
      target: { value: "  sk-a••••••5678  " },
    });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.UpdateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({ apiKey: "" }),
      );
    });
  });

  it("shows an empty API key with a hint when editing a provider without a key", async () => {
    const user = userEvent.setup();
    installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 2,
              type: "anthropic",
              name: "NoKey",
              providerKey: "p-2",
              baseUrl: "",
              maskedApiKey: "",
              hasApiKey: false,
              model: "",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit NoKey/ }));
    const dialog = await screen.findByRole("dialog");

    const apiKeyInput = within(dialog).getByPlaceholderText(
      "sk-... or self-hosted token. Leave empty for anonymous access.",
    ) as HTMLInputElement;
    expect(apiKeyInput).toHaveValue("");
    expect(
      within(dialog).getByText(/No API Key configured yet/),
    ).toBeInTheDocument();
  });

  it("does not apply stale preview limits after the model changes", async () => {
    const user = userEvent.setup();
    let resolvePreview: (value: {
      items: Array<{
        contextWindow: number;
        id: string;
        maxOutput: number;
      }>;
    }) => void = () => undefined;
    const preview = new Promise<{
      items: Array<{
        contextWindow: number;
        id: string;
        maxOutput: number;
      }>;
    }>((resolve) => {
      resolvePreview = resolve;
    });
    installAppMock({
      PreviewLLMModels: vi.fn(() => preview),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));
    fireEvent.change(
      screen.getByPlaceholderText("Example: production / local Ollama"),
      { target: { value: "Claude" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "sk-... or self-hosted token. Leave empty for anonymous access.",
      ),
      { target: { value: "sk-test" } },
    );
    const modelInput = screen.getByPlaceholderText(
      "Example: claude-opus-4-7 / gpt-4o-mini",
    );
    fireEvent.change(modelInput, { target: { value: "old-model" } });
    await user.click(screen.getByTitle("Fetch provider models"));

    fireEvent.change(modelInput, { target: { value: "new-model" } });
    resolvePreview({
      items: [
        {
          id: "old-model",
          contextWindow: 200000,
          maxOutput: 64000,
        },
      ],
    });

    await waitFor(() => {
      expect(
        (screen.getByLabelText("Context Window") as HTMLInputElement).value,
      ).toBe("");
      expect(
        (screen.getByLabelText("Max Output Tokens") as HTMLInputElement).value,
      ).toBe("");
    });
  });

  it("accepts real model token limits that are not multiples of 1024", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock();
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      screen.getByPlaceholderText("Example: production / local Ollama"),
      {
        target: { value: "Claude" },
      },
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "sk-... or self-hosted token. Leave empty for anonymous access.",
      ),
      {
        target: { value: "sk-test" },
      },
    );
    fireEvent.change(
      screen.getByPlaceholderText("Example: claude-opus-4-7 / gpt-4o-mini"),
      {
        target: { value: "claude-sonnet-4-6" },
      },
    );

    const contextWindow = screen.getByLabelText(
      "Context Window",
    ) as HTMLInputElement;
    const maxOutput = screen.getByLabelText(
      "Max Output Tokens",
    ) as HTMLInputElement;
    fireEvent.change(contextWindow, { target: { value: "200000" } });
    fireEvent.change(maxOutput, { target: { value: "64000" } });

    expect(contextWindow).toBeValid();
    expect(maxOutput).toBeValid();

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.CreateLLMProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Claude",
          model: "claude-sonnet-4-6",
          contextWindow: 200000,
          maxOutput: 64000,
        }),
      );
    });

    expect(dialog).not.toHaveTextContent("Save failed");
  });

  it("uses the edited draft with an empty API key when refreshing models", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      ListLLMProviders: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: 7,
              type: "anthropic",
              name: "GLM",
              providerKey: "provider-7",
              baseUrl: "https://old.example/anthropic",
              maskedApiKey: "configured-redacted",
              hasApiKey: true,
              model: "glm-old",
              maxOutput: 0,
              contextWindow: 0,
              createtime: 0,
              updatetime: 0,
            },
          ],
        }),
      ),
      PreviewLLMModels: vi.fn(() =>
        Promise.resolve({
          items: [
            {
              id: "glm-new",
              vendor: "anthropic",
              contextWindow: 0,
              maxOutput: 0,
              modalities: [],
              thinking: false,
              knownInCago: false,
            },
          ],
        }),
      ),
    });
    render(<LlmProvidersPanel />);

    await user.click(await screen.findByRole("button", { name: /Edit GLM/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByDisplayValue("https://old.example/anthropic"),
      { target: { value: "https://new.example/anthropic" } },
    );
    await user.click(within(dialog).getByTitle("Fetch provider models"));

    await waitFor(() => {
      expect(mocks.PreviewLLMModels).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 7,
          type: "anthropic",
          apiKey: "",
          baseUrl: "https://new.example/anthropic",
        }),
      );
    });
  });
});
