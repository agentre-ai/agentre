/**
 * model-pill.test.tsx — composer ModelPill 渲染态 + 交互（规格 UI 决策 7/8）。
 *
 * 覆盖：默认/已覆盖/加载中/错误行、未绑已有会话灰显 disable + tooltip、
 * 未绑新建会话自由输入、popover 交互、新建会话瞬态（不落库）。
 */

import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ListLLMProviders: vi.fn(),
  ListLLMModels: vi.fn(),
  SetChatSessionModel: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { ModelPill, useModelPill } from "../model-pill";
import type { UseModelPillOptions } from "../model-pill";

beforeEach(() => {
  vi.clearAllMocks();
  appMocks.ListLLMProviders.mockResolvedValue({ items: [] });
  appMocks.ListLLMModels.mockResolvedValue({ items: [] });
  appMocks.SetChatSessionModel.mockResolvedValue(undefined);
});

function Harness(props: UseModelPillOptions) {
  const pill = useModelPill(props);
  return <ModelPill {...pill} />;
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const BOUND_SESSION: UseModelPillOptions = {
  sessionId: 5,
  llmProviderKey: "provider-key",
  initialOverride: "",
  providerDefaultModel: "default-model",
};

const MODELS = [
  {
    id: "default-model",
    vendor: "Acme",
    contextWindow: 128000,
    maxOutput: 0,
    modalities: [],
    thinking: false,
    knownInCago: true,
  },
  {
    id: "big-model",
    vendor: "Acme",
    contextWindow: 256000,
    maxOutput: 0,
    modalities: [],
    thinking: false,
    knownInCago: true,
  },
];

function mockBoundProviders() {
  appMocks.ListLLMProviders.mockResolvedValue({
    items: [{ id: 11, providerKey: "provider-key", name: "Acme Provider" }],
  });
  appMocks.ListLLMModels.mockResolvedValue({ items: MODELS });
}

describe("ModelPill", () => {
  it("已绑已有会话 · 默认：pill 显示 provider 默认模型，弹层「跟随供应商默认」高亮 + 模型列表 + 底部常显", async () => {
    mockBoundProviders();
    render(<Harness {...BOUND_SESSION} />);

    // 列表加载完成后 pill 可用并显示默认模型名。
    await waitFor(() =>
      expect(screen.getByTestId("model-pill")).not.toBeDisabled(),
    );
    expect(screen.getByTestId("model-pill")).toHaveTextContent("default-model");

    const user = userEvent.setup();
    await user.click(screen.getByTestId("model-pill"));

    expect(screen.getByText("Follow provider default")).toBeInTheDocument();
    const listbox = screen.getByRole("listbox");
    expect(within(listbox).getByText("default-model")).toBeInTheDocument();
    expect(within(listbox).getByText("big-model")).toBeInTheDocument();
    expect(within(listbox).getByText("128K")).toBeInTheDocument();
    // 底部常显「切换从下一轮开始生效」。
    expect(
      screen.getByText(/takes effect from the next turn/),
    ).toBeInTheDocument();
  });

  it("已绑已有会话 · 已覆盖：pill 高亮 active，选「跟随供应商默认」调 SetChatSessionModel 清空", async () => {
    mockBoundProviders();
    render(<Harness {...BOUND_SESSION} initialOverride="big-model" />);

    await waitFor(() =>
      expect(screen.getByTestId("model-pill")).not.toBeDisabled(),
    );
    expect(screen.getByTestId("model-pill")).toHaveTextContent("big-model");

    const user = userEvent.setup();
    await user.click(screen.getByTestId("model-pill"));
    await user.click(screen.getByText("Follow provider default"));

    expect(appMocks.SetChatSessionModel).toHaveBeenCalledWith(5, "");
  });

  it("已绑 · 选择列表项调 SetChatSessionModel 落库", async () => {
    mockBoundProviders();
    render(<Harness {...BOUND_SESSION} />);
    await waitFor(() =>
      expect(screen.getByTestId("model-pill")).not.toBeDisabled(),
    );

    const user = userEvent.setup();
    await user.click(screen.getByTestId("model-pill"));
    await user.click(
      within(screen.getByRole("listbox")).getByText("big-model"),
    );

    expect(appMocks.SetChatSessionModel).toHaveBeenCalledWith(5, "big-model");
  });

  it("已绑 · 列表加载中：pill 禁用", async () => {
    const slow = deferred<{ items: unknown[] }>();
    appMocks.ListLLMProviders.mockReturnValue(slow.promise);
    render(<Harness {...BOUND_SESSION} />);

    expect(screen.getByTestId("model-pill")).toBeDisabled();

    act(() => {
      slow.resolve({
        items: [{ id: 11, providerKey: "provider-key", name: "Acme" }],
      });
    });
    appMocks.ListLLMModels.mockResolvedValue({ items: MODELS });
    await waitFor(() =>
      expect(screen.getByTestId("model-pill")).not.toBeDisabled(),
    );
  });

  it("已绑 · provider 离线（provider 找不到）：弹层底部错误行", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: [] });
    render(<Harness {...BOUND_SESSION} />);
    await waitFor(() =>
      expect(screen.getByTestId("model-pill")).not.toBeDisabled(),
    );

    const user = userEvent.setup();
    await user.click(screen.getByTestId("model-pill"));

    expect(screen.getByTestId("model-pill-error")).toHaveTextContent(
      "No model list found",
    );
  });

  it("未绑 provider + 已有会话：pill 灰显 disable + tooltip，无弹层交互", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: [] });
    render(
      <Harness
        sessionId={5}
        llmProviderKey=""
        initialOverride=""
        providerDefaultModel=""
      />,
    );

    const pill = screen.getByTestId("model-pill");
    expect(pill).toBeDisabled();
    expect(pill).toHaveAttribute(
      "title",
      "No provider bound; existing sessions cannot switch models",
    );

    // 点击不打开弹层（disabled 按钮不触发 popover）。
    const user = userEvent.setup();
    await user.click(pill);
    expect(screen.queryByText("Model")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Type a model ID/)).not.toBeInTheDocument();
  });

  it("未绑 provider + 新建会话：弹层「跟随默认」+ 自由输入模型 id，回车/提交设置瞬态且不落库", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: [] });
    render(
      <Harness
        sessionId={0}
        llmProviderKey=""
        initialOverride=""
        providerDefaultModel=""
      />,
    );

    const pill = screen.getByTestId("model-pill");
    expect(pill).not.toBeDisabled();

    const user = userEvent.setup();
    await user.click(pill);
    expect(
      within(screen.getByRole("listbox")).getByText("Follow default"),
    ).toBeInTheDocument();

    const input = screen.getByLabelText(/Type a model ID/);
    await user.type(input, "cli-custom-model{Enter}");

    // 瞬态设置：不调 SetChatSessionModel（新建会话），pill 显示所选模型。
    expect(appMocks.SetChatSessionModel).not.toHaveBeenCalled();
    expect(screen.getByTestId("model-pill")).toHaveTextContent(
      "cli-custom-model",
    );
  });
});
