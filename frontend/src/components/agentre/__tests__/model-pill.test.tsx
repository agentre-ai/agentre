/**
 * model-pill.test.tsx — 新建会话 LLM 供应商选择器（规格「新建会话供应商选择器」+ 决策 4/5）。
 *
 * 覆盖：只列兼容供应商（builtin→全部 / claudecode→anthropic / codex→openai-response /
 * piagent→anthropic+openai-chat+openai-response）；未绑 agent（CLI 登录态）也显示；
 * 瞬态选择 / 点击「跟随 agent 绑定」清回；列表加载中 → pill 禁用；拉取失败 → 弹层底部错误行。
 * （openclaw 不渲染 / 已有会话无 pill 是 chat-panel 级门控，见 chat-panel.test.tsx。）
 */

import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ListLLMProviders: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import {
  isProviderCompatible,
  isProviderSelectableBackend,
  ProviderPill,
  useProviderPill,
} from "../model-pill";
import type { UseProviderPillOptions } from "../model-pill";

beforeEach(() => {
  vi.clearAllMocks();
  appMocks.ListLLMProviders.mockResolvedValue({ items: [] });
});

function Harness(props: UseProviderPillOptions) {
  const pill = useProviderPill(props);
  return <ProviderPill {...pill} />;
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const ANTHROPIC_PROVIDER = {
  id: 1,
  providerKey: "acme-anthropic",
  name: "Acme Claude",
  type: "anthropic",
  model: "claude-sonnet-4-5",
  baseUrl: "",
  maskedApiKey: "",
  hasApiKey: true,
  maxOutput: 0,
  contextWindow: 200000,
  createtime: 0,
  updatetime: 0,
};

const CHAT_PROVIDER = {
  id: 2,
  providerKey: "acme-chat",
  name: "Acme Chat",
  type: "openai-chat",
  model: "gpt-4o",
  baseUrl: "",
  maskedApiKey: "",
  hasApiKey: true,
  maxOutput: 0,
  contextWindow: 128000,
  createtime: 0,
  updatetime: 0,
};

const RESPONSE_PROVIDER = {
  id: 3,
  providerKey: "acme-response",
  name: "Acme Resp",
  type: "openai-response",
  model: "gpt-5",
  baseUrl: "",
  maskedApiKey: "",
  hasApiKey: true,
  maxOutput: 0,
  contextWindow: 128000,
  createtime: 0,
  updatetime: 0,
};

const ALL_PROVIDERS = [ANTHROPIC_PROVIDER, CHAT_PROVIDER, RESPONSE_PROVIDER];

describe("provider compatibility gate（与后端 ProviderTypeMatch 对齐）", () => {
  it("builtin → 全部供应商兼容", () => {
    expect(isProviderCompatible("builtin", "anthropic")).toBe(true);
    expect(isProviderCompatible("builtin", "openai-chat")).toBe(true);
    expect(isProviderCompatible("builtin", "openai-response")).toBe(true);
  });

  it("claudecode → 仅 anthropic", () => {
    expect(isProviderCompatible("claudecode", "anthropic")).toBe(true);
    expect(isProviderCompatible("claudecode", "openai-chat")).toBe(false);
    expect(isProviderCompatible("claudecode", "openai-response")).toBe(false);
  });

  it("codex → 仅 openai-response", () => {
    expect(isProviderCompatible("codex", "openai-response")).toBe(true);
    expect(isProviderCompatible("codex", "anthropic")).toBe(false);
    expect(isProviderCompatible("codex", "openai-chat")).toBe(false);
  });

  it("piagent → anthropic / openai-chat / openai-response", () => {
    expect(isProviderCompatible("piagent", "anthropic")).toBe(true);
    expect(isProviderCompatible("piagent", "openai-chat")).toBe(true);
    expect(isProviderCompatible("piagent", "openai-response")).toBe(true);
  });

  it("openclaw 不渲染供应商选择器", () => {
    expect(isProviderSelectableBackend("openclaw")).toBe(false);
    expect(isProviderCompatible("openclaw", "anthropic")).toBe(false);
    expect(isProviderSelectableBackend("")).toBe(false);
  });

  it("builtin / claudecode / codex / piagent 均可选供应商", () => {
    expect(isProviderSelectableBackend("builtin")).toBe(true);
    expect(isProviderSelectableBackend("claudecode")).toBe(true);
    expect(isProviderSelectableBackend("codex")).toBe(true);
    expect(isProviderSelectableBackend("piagent")).toBe(true);
  });
});

describe("ProviderPill · 新建会话供应商选择器", () => {
  it("只列兼容供应商：claudecode 弹层只显示 anthropic，不列 openai-chat / openai-response", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="claudecode" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    expect(within(listbox).getByText("Acme Claude")).toBeInTheDocument();
    expect(
      within(listbox).getByRole("img", { name: "Anthropic" }),
    ).toBeInTheDocument();
    expect(
      within(listbox).getByRole("img", { name: "Claude" }),
    ).toBeInTheDocument();
    expect(within(listbox).queryByText("Acme Chat")).not.toBeInTheDocument();
    expect(within(listbox).queryByText("Acme Resp")).not.toBeInTheDocument();
  });

  it("builtin 弹层列出全部供应商", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="builtin" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    expect(within(listbox).getByText("Acme Claude")).toBeInTheDocument();
    expect(within(listbox).getByText("Acme Chat")).toBeInTheDocument();
    expect(within(listbox).getByText("Acme Resp")).toBeInTheDocument();
  });

  it("未绑 agent（CLI 登录态）也显示供应商选择器，第一项语义为「不选，走 CLI 登录态」", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="piagent" boundProviderKey="" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    // 未选时 pill 标签显示占位文案「选择供应商」。
    expect(pill).toHaveTextContent("Select provider");

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    // 未选时「跟随 agent 绑定」项高亮（aria-selected）且语义为 CLI 登录态。
    const follow = within(listbox).getByRole("option", {
      name: /CLI login state/i,
    });
    expect(follow).toHaveAttribute("aria-selected", "true");
    // 兼容供应商仍全部列出。
    expect(within(listbox).getByText("Acme Claude")).toBeInTheDocument();
    expect(within(listbox).getByText("Acme Chat")).toBeInTheDocument();
    expect(within(listbox).getByText("Acme Resp")).toBeInTheDocument();
  });

  it("选中供应商 → pill 显示供应商名；点「跟随 agent 绑定」清回占位", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="codex" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByText("Acme Resp"),
    );

    // 选中后 pill 显示供应商名。
    expect(screen.getByTestId("provider-pill")).toHaveTextContent("Acme Resp");

    // 点击「跟随 agent 绑定」清回（无瞬态选择）。
    await user.click(screen.getByTestId("provider-pill"));
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /Follow agent binding/i,
      }),
    );
    expect(screen.getByTestId("provider-pill")).toHaveTextContent(
      "Select provider",
    );
  });

  it("列表加载中 → pill 禁用；加载完成 → 可用", async () => {
    const slow = deferred<{ items: unknown[] }>();
    appMocks.ListLLMProviders.mockReturnValue(slow.promise);
    render(<Harness backendType="claudecode" />);

    expect(screen.getByTestId("provider-pill")).toBeDisabled();

    act(() => {
      slow.resolve({ items: [ANTHROPIC_PROVIDER] });
    });
    await waitFor(() =>
      expect(screen.getByTestId("provider-pill")).not.toBeDisabled(),
    );
  });

  it("拉取失败 → 弹层底部错误行（provider-pill-error）", async () => {
    appMocks.ListLLMProviders.mockRejectedValue(new Error("boom"));
    render(<Harness backendType="claudecode" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    expect(screen.getByTestId("provider-pill-error")).toHaveTextContent(
      "Failed to load providers",
    );
  });

  it("openclaw backend 不拉取供应商（列表为空、不加载）", () => {
    render(<Harness backendType="openclaw" />);

    expect(screen.getByTestId("provider-pill")).not.toBeDisabled();
    expect(appMocks.ListLLMProviders).not.toHaveBeenCalled();
  });
});
