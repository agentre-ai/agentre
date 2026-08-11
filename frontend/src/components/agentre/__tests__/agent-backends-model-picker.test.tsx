import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ModelTargetPicker } from "../model-target-picker";
import {
  readRecentTargets,
  recordRecentTarget,
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
      defaultModel: { modelKey: "mk-1", modelId: "claude-sonnet-4-6", enabled: true },
      models: [
        { modelKey: "mk-1", modelId: "claude-sonnet-4-6", enabled: true },
        { modelKey: "mk-opus", modelId: "claude-opus-4-8", enabled: true },
        { modelKey: "mk-off", modelId: "claude-old", enabled: false },
      ],
    },
    {
      providerKey: "k-openai",
      id: 2,
      name: "OpenAI",
      type: "openai-response",
      enabled: true,
      defaultModel: { modelKey: "mk-2", modelId: "gpt-5.4", enabled: true },
      models: [{ modelKey: "mk-2", modelId: "gpt-5.4", enabled: true }],
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
  it("backend 场景顶部特殊项是 CLI 自身登录态，选中发射双空 key", async () => {
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
    await user.click(special);
    expect(onChange).toHaveBeenCalledWith({ providerKey: "", modelKey: "" });
  });

  it("provider 组内 provider-default 首项，再 fixed-model；不兼容的 provider 被过滤", async () => {
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
    // provider-default 首项（Anthropic + 默认模型），fixed 在其后；特殊项在最前。
    const options = within(list).getAllByRole("option");
    expect(options[0]).toHaveTextContent(/CLI login state/);
    expect(options[1]).toHaveTextContent("Anthropic");
    expect(options[1]).toHaveTextContent("claude-sonnet-4-6");
    expect(options[2]).toHaveTextContent("claude-opus-4-8");
    // 停用模型不可选。
    expect(options[3]).toHaveAttribute("aria-disabled", "true");

    // 选中固定模型 → 发射 providerKey+modelKey。
    await user.click(options[2]);
    expect(onChange).toHaveBeenCalledWith({
      providerKey: "k-anthropic",
      modelKey: "mk-opus",
    });
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
    expect(await screen.findByText(/Loading model catalog/)).toBeInTheDocument();

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
    expect(await screen.findByText(/Failed to load model catalog/)).toBeInTheDocument();
  });

  it("已失效目标显示失效提示，键盘方向键 + Enter 可选中", async () => {
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
    // 已失效目标回显原始 key + 失效提示。
    expect(screen.getByText(/k-gone/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    expect(await screen.findByText(/no longer valid/)).toBeInTheDocument();

    // 键盘：↑↓ 移动，Enter 选中。
    await user.keyboard("{ArrowDown}");
    await user.keyboard("{Enter}");
    expect(onChange).toHaveBeenCalled();
  });

  it("route 场景顶部特殊项是继承主绑定，recent 只展示兼容项", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
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
        onChange={onChange}
        catalog={catalog()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "LLM Provider" }));
    const list = await screen.findByRole("listbox", { name: "LLM Provider" });
    // inherit-main 特殊项在最前。
    expect(within(list).getByRole("option", { name: /Inherit main binding/ })).toBeInTheDocument();
    // recent 里 claudecode 不兼容的 OpenAI 项被隐藏，只有 Anthropic 的 recent（固定模型）可见。
    // claude-opus-4-8 同时出现在 recent 与目录 fixed 项中（两处）。
    expect(within(list).getAllByText("claude-opus-4-8").length).toBeGreaterThan(0);
    expect(within(list).queryByText(/gpt-5/)).not.toBeInTheDocument();
  });
});
