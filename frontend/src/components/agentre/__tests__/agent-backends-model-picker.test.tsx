import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ModelTargetPicker } from "../model-target-picker";
import {
  readRecentTargets,
  recordRecentTarget,
  removeRecentTarget,
  recentStorageKey,
} from "../model-target-picker/recents";
import { providerCompatibleForBackend } from "../model-target-picker/types";

function catalog() {
  return [
    {
      providerKey: "k-anthropic",
      id: 1,
      name: "Anthropic",
      type: "anthropic",
      enabled: true,
      defaultModel: {
        modelKey: "mk-1",
        modelId: "claude-sonnet-4-6",
        name: "Sonnet",
        enabled: true,
        contextWindow: 200000,
        maxOutput: 32000,
      },
      models: [
        {
          modelKey: "mk-1",
          modelId: "claude-sonnet-4-6",
          name: "Sonnet",
          enabled: true,
          contextWindow: 200000,
          maxOutput: 32000,
        },
        {
          modelKey: "mk-opus",
          modelId: "claude-opus-4-8",
          name: "Opus",
          enabled: true,
          contextWindow: 400000,
          maxOutput: 64000,
        },
        {
          modelKey: "mk-off",
          modelId: "claude-old",
          name: "",
          enabled: false,
          contextWindow: 0,
          maxOutput: 0,
        },
      ],
    },
    {
      providerKey: "k-openai",
      id: 2,
      name: "OpenAI",
      type: "openai-response",
      enabled: true,
      defaultModel: {
        modelKey: "mk-2",
        modelId: "gpt-5.4",
        name: "GPT",
        enabled: true,
        contextWindow: 128000,
        maxOutput: 16000,
      },
      models: [
        {
          modelKey: "mk-2",
          modelId: "gpt-5.4",
          name: "GPT",
          enabled: true,
          contextWindow: 128000,
          maxOutput: 16000,
        },
      ],
    },
  ];
}

function catalogWithDisabledProvider() {
  return [
    {
      providerKey: "k-anthropic",
      id: 1,
      name: "Anthropic",
      type: "anthropic",
      enabled: false,
      defaultModel: {
        modelKey: "mk-1",
        modelId: "claude-sonnet-4-6",
        enabled: true,
      },
      models: [
        { modelKey: "mk-1", modelId: "claude-sonnet-4-6", enabled: true },
        { modelKey: "mk-off", modelId: "claude-old", enabled: false },
      ],
    },
  ];
}

afterEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
});

describe("ModelTargetPicker recents", () => {
  it("按场景 + 执行位置指纹隔离存储，且只存 providerKey/modelKey", () => {
    recordRecentTarget("backend", "", { providerKey: "k-1", modelKey: "mk-1" });
    recordRecentTarget("backend", "", { providerKey: "k-2", modelKey: "mk-2" });
    // 远端设备隔离：local 里记过的 key 不污染 daemon 作用域。
    expect(readRecentTargets("backend", "")).toHaveLength(2);
    expect(readRecentTargets("backend", "7")).toHaveLength(0);
    recordRecentTarget("backend", "7", { providerKey: "k-3", modelKey: "" });
    expect(readRecentTargets("backend", "7")).toHaveLength(1);
    const raw = window.localStorage.getItem(recentStorageKey("backend", ""));
    expect(raw).not.toContain("name");
    expect(raw).not.toContain("modelId");
    expect(raw).not.toContain("apiKey");
  });

  it("native/inherit（双空 key）永远不进最近；最新在前；最多 5 项去重", () => {
    recordRecentTarget("backend", "", { providerKey: "", modelKey: "" });
    expect(readRecentTargets("backend", "")).toHaveLength(0);

    for (let i = 1; i <= 6; i++) {
      recordRecentTarget("backend", "", {
        providerKey: `k-${i}`,
        modelKey: `mk-${i}`,
      });
    }
    const all = readRecentTargets("backend", "");
    expect(all).toHaveLength(5);
    expect(all[0].providerKey).toBe("k-6");
    expect(all).not.toContainEqual({ providerKey: "k-1", modelKey: "mk-1" });

    recordRecentTarget("backend", "", { providerKey: "k-3", modelKey: "mk-3" });
    const deduped = readRecentTargets("backend", "");
    expect(deduped[0]).toEqual({ providerKey: "k-3", modelKey: "mk-3" });
    expect(deduped.filter((x) => x.providerKey === "k-3")).toHaveLength(1);
  });

  it("removeRecentTarget 只移除指定目标，其余保留", () => {
    recordRecentTarget("backend", "", { providerKey: "k-1", modelKey: "mk-1" });
    recordRecentTarget("backend", "", { providerKey: "k-2", modelKey: "mk-2" });
    removeRecentTarget("backend", "", { providerKey: "k-1", modelKey: "mk-1" });
    expect(readRecentTargets("backend", "")).toEqual([
      { providerKey: "k-2", modelKey: "mk-2" },
    ]);
  });
});

describe("ModelTargetPicker providerCompatibleForBackend", () => {
  it("claudecode 只收 anthropic；codex 只收 openai-response；piagent 三类全收", () => {
    expect(providerCompatibleForBackend("claudecode", "anthropic")).toBe(true);
    expect(providerCompatibleForBackend("claudecode", "openai-response")).toBe(
      false,
    );
    expect(providerCompatibleForBackend("codex", "openai-response")).toBe(true);
    expect(providerCompatibleForBackend("codex", "anthropic")).toBe(false);
    expect(providerCompatibleForBackend("piagent", "anthropic")).toBe(true);
    expect(providerCompatibleForBackend("piagent", "openai-chat")).toBe(true);
    expect(providerCompatibleForBackend("piagent", "openai-response")).toBe(
      true,
    );
    expect(providerCompatibleForBackend("builtin", "anthropic")).toBe(true);
  });
});

describe("ModelTargetPicker", () => {
  it("backend 场景顶部特殊项是 CLI 自身登录态，无 prop 回落「由 CLI 登录态决定」副行，选中发射双空 key", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const special = await screen.findByRole("option", {
      name: /CLI login state/,
    });
    expect(special).toHaveTextContent(
      /Determined by the CLI's own login account/,
    );
    await user.click(special);
    expect(onChange).toHaveBeenCalledWith({ providerKey: "", modelKey: "" });
  });

  it("chat / route 特殊项用 prop 副行显示解析结果", async () => {
    const user = userEvent.setup();
    const { unmount } = render(
      <ModelTargetPicker
        scenario="chat"
        specialSublabel="Anthropic · claude-sonnet-4-6"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    expect(
      await screen.findByRole("option", { name: /Follow agent binding/ }),
    ).toHaveTextContent("Anthropic · claude-sonnet-4-6");
    unmount();

    render(
      <ModelTargetPicker
        scenario="route"
        specialSublabel="Anthropic · claude-opus-4-8"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    expect(
      await screen.findByRole("option", { name: /Inherit main binding/ }),
    ).toHaveTextContent("Anthropic · claude-opus-4-8");
  });

  it("provider 组内 provider-default 首项（强调底色 + 动态图标 + 当前解析模型），再 fixed-model；不兼容的 provider 被过滤", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    // claudecode 只收 anthropic：OpenAI 组不可见。
    expect(within(list).queryByText("OpenAI")).not.toBeInTheDocument();
    const options = within(list).getAllByRole("option");
    expect(options[0]).toHaveTextContent(/CLI login state/);
    // provider-default 首项：强调底色 + 动态图标 + 「当前 = ×××」。
    expect(options[1]).toHaveAttribute("data-kind", "provider-default");
    // 核心语义分歧：跟随默认用 primary-soft 底 + primary-text 标题，与固定模型两态可分。
    expect(options[1]).toHaveClass("bg-primary-soft");
    expect(options[1]).toHaveTextContent(/Follow this provider's default/);
    expect(options[1]).toHaveTextContent(/Current = claude-sonnet-4-6/);
    expect(
      within(options[1] as HTMLElement).getByText(
        "Follow this provider's default",
      ),
    ).toHaveClass("text-primary-text");
    expect(
      within(options[1] as HTMLElement).getByTestId("provider-default-icon"),
    ).toBeInTheDocument();
    // fixed-model 紧随其后（包括当前默认模型的固定目标）；固定态不共用强调底色。
    expect(options[2]).toHaveTextContent("claude-sonnet-4-6");
    expect(options[2]).not.toHaveClass("bg-primary-soft");
    expect(options[3]).toHaveTextContent("claude-opus-4-8");
    // 停用模型不可选。
    expect(options[4]).toHaveAttribute("aria-disabled", "true");

    await user.click(options[3]);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
  });

  it("默认模型也作为 fixed-model 候选保留，让用户可以锁定当前默认而不随以后默认变化", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const fixedDefault = within(list).getByRole("option", {
      name: /Sonnet claude-sonnet-4-6/,
    });
    expect(fixedDefault).toHaveAttribute("data-kind", "fixed");

    await user.click(fixedDefault);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-1",
    });
  });

  it("provider-default 与 fixed-model 视觉分离：fixed-model 归入「固定到具体模型」分段", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    expect(
      within(list).getByText("Fixed to a specific model"),
    ).toBeInTheDocument();
  });

  it("fixed-model 行不再重复供应商名：label=display name、副行=modelId、右侧上下文/最大输出，供应商名由组头承载", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const options = within(list).getAllByRole("option");
    const opus = options[3];
    expect(opus).toHaveTextContent("Opus");
    expect(opus).toHaveTextContent("claude-opus-4-8");
    expect(opus).not.toHaveTextContent("Anthropic");
    expect(opus).toHaveTextContent(/400K ctx · 64K out/);
    // 组头承载品牌标识 + 供应商名。
    expect(within(list).getByText("Anthropic")).toBeInTheDocument();
    expect(
      within(list).getByRole("img", { name: "Anthropic" }),
    ).toBeInTheDocument();
  });

  it("provider-default 已选中时触发按钮显示 Provider · 实际模型（路由摘要不得只显示 Provider 名称）", () => {
    render(
      <ModelTargetPicker
        scenario="route"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={{ providerKey: "k-anthropic", modelKey: "" }}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    // 摘要 = Provider + 当前默认模型（解析出的生效模型），不得只显示 Provider 名。
    expect(
      screen.getByRole("button", { name: "LLM Provider" }),
    ).toHaveTextContent("Anthropic · claude-sonnet-4-6");
  });

  it("triggerSub 存在时触发按钮以主副两行呈现，未传时保持既有单行消费方兼容", () => {
    const { rerender } = render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="Model binding"
        backendType="claudecode"
        selected={{ providerKey: "k-anthropic", modelKey: "" }}
        onChange={vi.fn()}
        catalog={catalog()}
        triggerLabel="Anthropic · Follow default"
        triggerSub="Effective model: claude-sonnet-4-6 · changes next turn"
      />,
    );
    const trigger = screen.getByRole("button", { name: "Model binding" });
    expect(trigger).toHaveTextContent("Anthropic · Follow default");
    expect(trigger).toHaveTextContent(
      "Effective model: claude-sonnet-4-6 · changes next turn",
    );
    expect(
      within(trigger).getByTestId("model-target-trigger-sub"),
    ).toBeVisible();

    rerender(
      <ModelTargetPicker
        scenario="chat"
        aria-label="Model binding"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    expect(
      within(
        screen.getByRole("button", { name: "Model binding" }),
      ).queryByTestId("model-target-trigger-sub"),
    ).not.toBeInTheDocument();
  });

  it("搜索可过滤，空目录渲染空态", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={[]}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    await screen.findByRole("listbox", { name: "LLM Provider" });
    expect(screen.getByText(/No available providers/)).toBeInTheDocument();
  });

  it("没有任何与当前 backend 类型兼容的供应商时给出一句说明", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={[
          {
            providerKey: "k-openai",
            id: 2,
            name: "OpenAI",
            type: "openai-response",
            enabled: true,
            defaultModel: null,
            models: [],
          },
        ]}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    expect(
      await screen.findByText(/No providers compatible with this backend type/),
    ).toBeInTheDocument();
  });

  it("loading / error 状态各自呈现", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={[]}
        loading
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    expect(
      await screen.findByText(/Loading model catalog/),
    ).toBeInTheDocument();

    rerender(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={[]}
        error
      />,
    );
    expect(
      await screen.findByText(/Failed to load model catalog/),
    ).toBeInTheDocument();
  });

  it("失效目标顶部警示写出供应商与模型，被删目标以禁用项保留，键盘方向键 + Enter 可选中", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="route"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={{ providerKey: "k-gone", modelKey: "mk-gone" }}
        onChange={onChange}
        catalog={catalog()}
        invalid
      />,
    );
    // 已失效目标回显原始 key。
    expect(
      screen.getByRole("button", { name: "LLM Provider" }),
    ).toHaveTextContent(/k-gone/);
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    // 顶部警示（弹层内上方，不是底部 footer）。
    const banner = await screen.findByTestId("invalid-banner");
    expect(banner).toHaveTextContent(/no longer valid/);
    expect(banner).toHaveTextContent("k-gone");
    expect(banner).toHaveTextContent("mk-gone");
    // 被删目标以禁用项保留在列表中。
    const list = screen.getByRole("listbox", { name: "LLM Provider" });
    const options = within(list).getAllByRole("option");
    const preserved = options.find((o) => o.textContent?.includes("k-gone"));
    expect(preserved).toBeDefined();
    expect(preserved).toHaveAttribute("aria-disabled", "true");

    // 键盘：↑↓ 移动，Enter 选中。
    await user.keyboard("{ArrowDown}");
    await user.keyboard("{Enter}");
    expect(onChange).toHaveBeenCalled();
  });

  it("失效的当前选择行内写出「当前选择 · 已失效」与失效标记，不只靠顶部横幅", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="route"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={{ providerKey: "k-gone", modelKey: "mk-gone" }}
        onChange={vi.fn()}
        catalog={catalog()}
        invalid
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const preserved = within(list)
      .getAllByRole("option")
      .find((o) => o.textContent?.includes("k-gone"));
    expect(preserved).toBeDefined();
    expect(preserved).toHaveTextContent("Current selection · no longer valid");
    expect(
      within(preserved as HTMLElement).getByText("Invalid"),
    ).toBeInTheDocument();
  });

  it("route 场景顶部特殊项是继承主绑定，recent 只展示兼容项（chip）", async () => {
    const user = userEvent.setup();
    recordRecentTarget("route", "", {
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
    recordRecentTarget("route", "", {
      providerKey: "k-openai",
      modelKey: "mk-2",
    });
    render(
      <ModelTargetPicker
        scenario="route"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    // inherit-main 特殊项在最前。
    expect(
      within(list).getByRole("option", { name: /Inherit main binding/ }),
    ).toBeInTheDocument();
    // recent 是 chip：兼容的 Anthropic 可见，OpenAI 被隐藏。
    expect(
      screen.getByRole("button", { name: "claude-opus-4-8" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /gpt-5/ }),
    ).not.toBeInTheDocument();
  });

  it("最近使用为单行横向可移除 chip，每条可单独移除，移除后从 localStorage 删除", async () => {
    const user = userEvent.setup();
    recordRecentTarget("backend", "", {
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
    recordRecentTarget("backend", "", {
      providerKey: "k-anthropic",
      modelKey: "",
    });
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const chips = await screen.findByTestId("recent-chips");
    // 单行横向、不换行。
    expect(chips).toHaveClass("flex-nowrap", "overflow-x-auto");
    // chip 主体可点击选择。
    const opusChip = screen.getByRole("button", { name: "claude-opus-4-8" });
    expect(opusChip).not.toBeDisabled();
    // 单条移除。
    await user.click(
      screen.getByRole("button", { name: "Remove claude-opus-4-8" }),
    );
    expect(readRecentTargets("backend", "")).toEqual([
      { providerKey: "k-anthropic", modelKey: "" },
    ]);
    expect(
      screen.queryByRole("button", { name: "claude-opus-4-8" }),
    ).not.toBeInTheDocument();
  });

  it("最近使用排在顶部特殊项之后、供应商分组之前，并带「最近使用」标签", async () => {
    const user = userEvent.setup();
    recordRecentTarget("backend", "", {
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const special = within(list).getByRole("option", {
      name: /CLI login state/,
    });
    const chips = within(list).getByTestId("recent-chips");
    const providerDefault = within(list).getByRole("option", {
      name: /Follow this provider's default/,
    });
    // 顺序：特殊项 → 最近使用 → 供应商分组。
    expect(
      special.compareDocumentPosition(chips) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      chips.compareDocumentPosition(providerDefault) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    // chip 横条自带标签，不再是一排无名 chip。
    expect(within(chips).getByText("Recently used")).toBeInTheDocument();
  });

  it("recent chip 里非默认但启用的固定模型仍可选", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    recordRecentTarget("backend", "", {
      providerKey: "k-anthropic",
      modelKey: "mk-opus", // 非默认（默认 mk-1）但启用
    });
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const chip = await screen.findByRole("button", {
      name: "claude-opus-4-8",
    });
    expect(chip).not.toBeDisabled();
    await user.click(chip);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
  });

  it("recent chip 保留当前默认模型的 fixed 语义，不降级成 provider-default", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    recordRecentTarget("backend", "", {
      providerKey: "k-anthropic",
      modelKey: "mk-1",
    });
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const chip = await screen.findByRole("button", {
      name: "claude-sonnet-4-6",
    });
    await user.click(chip);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-1",
    });
  });

  it("禁用项给出行内原因：供应商停用 / 模型停用", async () => {
    const user = userEvent.setup();
    // 供应商停用：default 与 fixed 都给出「供应商已停用」。
    const { unmount } = render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalogWithDisabledProvider()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    let list = await screen.findByRole("listbox", { name: "LLM Provider" });
    let options = within(list).getAllByRole("option");
    expect(options[1]).toHaveTextContent("This provider is disabled");
    expect(options[2]).toHaveTextContent("This provider is disabled");
    unmount();

    // 供应商启用、模型停用：该模型行给出「模型已停用」。
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    list = await screen.findByRole("listbox", { name: "LLM Provider" });
    options = within(list).getAllByRole("option");
    const off = options.find((o) => o.textContent?.includes("claude-old"));
    expect(off).toBeDefined();
    expect(off).toHaveTextContent("This model is disabled");
  });
});

describe("ModelTargetPicker remote gating (task 6)", () => {
  function remoteCatalogWith() {
    return [
      {
        providerKey: "k-anthropic",
        id: 0,
        name: "Anthropic",
        type: "anthropic",
        enabled: true,
        defaultModel: {
          modelKey: "mk-1",
          modelId: "claude-sonnet-4-6",
          enabled: true,
        },
        models: [
          { modelKey: "mk-1", modelId: "claude-sonnet-4-6", enabled: true },
        ],
      },
    ];
  }

  it("旧 daemon（不支持 fixed-model）禁用所有 fixed-model，provider-default 仍可选", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel={false}
        remoteCatalog={remoteCatalogWith()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const options = within(list).getAllByRole("option");
    // provider-default（Anthropic）可选；所有 fixed-model（包括当前默认模型）被禁用。
    expect(options[1]).not.toHaveAttribute("aria-disabled", "true");
    expect(options[2]).toHaveAttribute("aria-disabled", "true");
    expect(options[3]).toHaveAttribute("aria-disabled", "true");
    expect(options[3]).toHaveAttribute("title");
    expect(options[3]).toHaveTextContent(
      "This device does not support fixed models",
    );
  });

  it("模型在 daemon 上不存在 → fixed-model、最近使用都禁用，并提供重新同步入口", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const onSyncProvider = vi.fn();
    recordRecentTarget("backend", "7", {
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        onSyncProvider={onSyncProvider}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const options = within(list).getAllByRole("option");
    // daemon 上存在 k-anthropic 的默认模型，但没有 mk-opus → 该 fixed-model 需同步。
    expect(options[1]).not.toHaveAttribute("aria-disabled", "true");
    // 当前默认模型在 daemon 上存在，可固定；mk-opus 缺失，需先同步。
    expect(options[2]).not.toHaveAttribute("aria-disabled", "true");
    expect(options[3]).toHaveAttribute("aria-disabled", "true");
    expect(options[3]).toHaveTextContent(
      "Local only — sync to this device first",
    );

    // 最近使用不能绕过同一远端门控。
    const recent = screen.getByRole("button", { name: "claude-opus-4-8" });
    expect(recent).toBeDisabled();
    await user.click(recent);
    expect(onChange).not.toHaveBeenCalled();

    // Provider 已存在但目录过期时仍需提供整包重新同步入口。
    const sync = screen.getByRole("button", {
      name: "Sync Anthropic to this device",
    });
    await user.click(sync);
    expect(onSyncProvider).toHaveBeenCalledWith(
      expect.objectContaining({ providerKey: "k-anthropic" }),
    );
  });

  it("daemon 的默认模型与本机不一致时，provider-default 禁用并要求同步", async () => {
    const user = userEvent.setup();
    const localCatalog = catalog();
    localCatalog[0] = {
      ...localCatalog[0],
      defaultModel: localCatalog[0].models[1],
    };
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={vi.fn()}
        onSyncProvider={vi.fn()}
        catalog={localCatalog}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const providerDefault = within(list).getByRole("option", {
      name: /Follow this provider's default/i,
    });
    expect(providerDefault).toHaveAttribute("aria-disabled", "true");
    expect(providerDefault).toHaveTextContent(
      "Local only — sync to this device first",
    );
    expect(
      screen.getByRole("button", { name: "Sync Anthropic to this device" }),
    ).toBeInTheDocument();
  });

  it("远端场景顶部说明以目标设备的配置为准，设备上已有的供应商组带标记；未传设备名时不渲染说明", async () => {
    const user = userEvent.setup();
    const { unmount } = render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="piagent"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
        deviceLabel="build-01"
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const header = await screen.findByTestId("remote-device-header");
    expect(header).toHaveTextContent("build-01");
    expect(header).toHaveTextContent(/configuration/i);
    // 设备上已有的供应商组标注，只在设备上真有的那一组出现。
    const list = screen.getByRole("listbox", { name: "LLM Provider" });
    const groups = within(list).getAllByTestId("picker-group");
    const anthropic = groups.find(
      (g) => g.getAttribute("data-provider-key") === "k-anthropic",
    );
    const openai = groups.find(
      (g) => g.getAttribute("data-provider-key") === "k-openai",
    );
    expect(
      within(anthropic as HTMLElement).getByText("Already on this device"),
    ).toBeInTheDocument();
    expect(
      within(openai as HTMLElement).queryByText("Already on this device"),
    ).not.toBeInTheDocument();
    unmount();

    // 本机 / 未传设备名 → 不渲染设备说明（既有消费方保持原形态）。
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="piagent"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    await screen.findByRole("listbox", { name: "LLM Provider" });
    expect(
      screen.queryByTestId("remote-device-header"),
    ).not.toBeInTheDocument();
  });

  it("「同步过去」落在需要同步的那一行内，而不是列表底部聚合", async () => {
    const user = userEvent.setup();
    const onSyncProvider = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="piagent"
        selected={null}
        onChange={vi.fn()}
        onSyncProvider={onSyncProvider}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    // 按钮在列表里、与它会修复的那一行同处一个 li；每个供应商只出现一次。
    const syncs = within(list).getAllByRole("button", {
      name: "Sync OpenAI to this device",
    });
    expect(syncs).toHaveLength(1);
    const row = syncs[0].closest("li") as HTMLElement;
    expect(within(row).getByRole("option")).toHaveTextContent(
      "Follow this provider's default",
    );
    expect(within(row).getByRole("option")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    await user.click(syncs[0]);
    expect(onSyncProvider).toHaveBeenCalledWith(
      expect.objectContaining({ providerKey: "k-openai" }),
    );
  });

  it("没有真实同步入口（未传 onSyncProvider）时不渲染任何同步按钮", async () => {
    const user = userEvent.setup();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="piagent"
        selected={null}
        onChange={vi.fn()}
        catalog={catalog()}
        executionLocation="7"
        supportsFixedModel
        remoteCatalog={remoteCatalogWith()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    await screen.findByRole("listbox", { name: "LLM Provider" });
    expect(
      screen.queryByRole("button", { name: /Sync .* to this device/ }),
    ).not.toBeInTheDocument();
  });

  it("本机（无 remoteCatalog）fixed-model 照常可选，不受门控影响", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ModelTargetPicker
        scenario="backend"
        aria-label="LLM Provider"
        backendType="claudecode"
        selected={null}
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    const options = within(list).getAllByRole("option");
    expect(options[3]).not.toHaveAttribute("aria-disabled", "true");
    await user.click(options[3]);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
  });
});
