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
  ListLLMModels: vi.fn(),
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
  ListLLMModels: AnyFn;
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
    ListLLMModels: vi.fn(() => Promise.resolve({ items: [] })),
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
  it("shows providerKey row after save", async () => {
    const user = userEvent.setup();
    const mocks = installAppMock({
      CreateLLMProvider: vi.fn(() =>
        Promise.resolve({ item: { id: 1, providerKey: "9b1c-uuid" } }),
      ),
    });
    render(<LlmProvidersPanel />);

    await screen.findByRole("table", { name: "LLM provider list" });
    await user.click(screen.getByRole("button", { name: "New Provider" }));

    const dialog = await screen.findByRole("dialog");
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

    await waitFor(() => {
      expect(mocks.CreateLLMProvider).toHaveBeenCalled();
      const keyInput = within(dialog).getByRole("textbox", {
        name: "Provider Key",
      }) as HTMLInputElement;
      expect(keyInput.value).toBe("9b1c-uuid");
    });

    // Copy button should be present
    expect(
      within(dialog).getByRole("button", { name: /Copy Provider Key/ }),
    ).toBeInTheDocument();
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
});
