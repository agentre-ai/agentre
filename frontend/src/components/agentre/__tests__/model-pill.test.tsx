/**
 * model-pill.test.tsx — 会话 LLM ModelTarget 选择器（规格 2026-08-09 决策 4/5 +
 * 2026-08-10 决策 10 + 2026-08-11「新建与已有会话流程」）。
 *
 * 覆盖：只列兼容供应商（builtin→全部 / claudecode→anthropic / codex→openai-response /
 * piagent→anthropic+openai-chat+openai-response）；未绑 agent（CLI 登录态）也显示；
 * 新建会话瞬态选择 provider-default / fixed-model / 点「跟随 agent 绑定」清回；列表
 * 加载中 → pill 禁用；拉取失败 → 弹层底部错误行；已有会话（sessionId>0）选择立即持久化
 * （SetChatSessionModelTarget，providerKey + modelKey）；切换失败回滚；不可切换时 pill
 * 常显但 disabled + tooltip 说明原因（openclaw / 无兼容供应商），不隐藏（决策 10）。
 */

import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ListLLMProviders: vi.fn(),
  ListLLMModels: vi.fn().mockResolvedValue({ items: [] }),
  SetChatSessionModelTarget: vi.fn(),
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
  appMocks.ListLLMModels.mockResolvedValue({ items: [] });
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
  enabled: true,
  defaultModelKey: "mk-sonnet",
};

const CHAT_PROVIDER = {
  id: 2,
  providerKey: "acme-chat",
  name: "Acme Chat",
  type: "openai-chat",
  enabled: true,
  defaultModelKey: "mk-gpt",
};

const RESPONSE_PROVIDER = {
  id: 3,
  providerKey: "acme-response",
  name: "Acme Resp",
  type: "openai-response",
  enabled: true,
  defaultModelKey: "mk-gpt5-default",
};

const ALL_PROVIDERS = [ANTHROPIC_PROVIDER, CHAT_PROVIDER, RESPONSE_PROVIDER];

function model(key: string, modelId: string, enabled = true) {
  return { modelKey: key, modelId, name: modelId, enabled };
}

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

describe("ProviderPill · 新建会话 ModelTarget 选择器", () => {
  it("只列兼容供应商：claudecode 弹层只显示 anthropic，不列 openai-chat / openai-response", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="claudecode" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    expect(
      within(listbox).getByRole("option", { name: /Acme Claude/ }),
    ).toBeInTheDocument();
    expect(
      within(listbox).queryByRole("option", { name: /Acme Chat/ }),
    ).not.toBeInTheDocument();
    expect(
      within(listbox).queryByRole("option", { name: /Acme Resp/ }),
    ).not.toBeInTheDocument();
  });

  it("builtin 弹层列出全部供应商", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="builtin" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    expect(
      within(listbox).getByRole("option", { name: /Acme Claude/ }),
    ).toBeInTheDocument();
    expect(
      within(listbox).getByRole("option", { name: /Acme Chat/ }),
    ).toBeInTheDocument();
    expect(
      within(listbox).getByRole("option", { name: /Acme Resp/ }),
    ).toBeInTheDocument();
  });

  it("未绑 agent（CLI 登录态）也显示选择器，顶部特殊项为「跟随 agent 绑定」", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="piagent" boundProviderKey="" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    // 未选时 pill 标签显示顶部特殊项「跟随 agent 绑定」。
    expect(pill).toHaveTextContent("Follow agent binding");

    const user = userEvent.setup();
    await user.click(pill);

    const listbox = screen.getByRole("listbox");
    // 未选时「跟随 agent 绑定」项高亮（aria-selected）。
    const follow = within(listbox).getByRole("option", {
      name: /Follow agent binding/i,
    });
    expect(follow).toHaveAttribute("aria-selected", "true");
    // 兼容供应商仍全部列出。
    expect(
      within(listbox).getByRole("option", { name: /Acme Claude/ }),
    ).toBeInTheDocument();
    expect(
      within(listbox).getByRole("option", { name: /Acme Chat/ }),
    ).toBeInTheDocument();
    expect(
      within(listbox).getByRole("option", { name: /Acme Resp/ }),
    ).toBeInTheDocument();
  });

  it("选中 provider-default → pill 显示供应商名；点「跟随 agent 绑定」清回", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="codex" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /Acme Resp/,
      }),
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
      "Follow agent binding",
    );
  });

  it("选中 fixed-model → pill 显示供应商名 · 模型 ID", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    appMocks.ListLLMModels.mockResolvedValue({
      items: [
        model("mk-gpt5-default", "gpt-5"),
        model("mk-fixed", "gpt-5-codex"),
      ],
    });
    render(<Harness backendType="codex" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /gpt-5-codex/,
      }),
    );

    expect(screen.getByTestId("provider-pill")).toHaveTextContent(
      "Acme Resp · gpt-5-codex",
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

  it("拉取失败 → 弹层底部错误行", async () => {
    appMocks.ListLLMProviders.mockRejectedValue(new Error("boom"));
    render(<Harness backendType="claudecode" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    expect(
      screen.getByText("Failed to load providers. Please try again."),
    ).toBeInTheDocument();
  });

  it("openclaw backend 不拉取供应商；pill 常显但 disabled + tooltip 说明原因（决策 10：禁用而非隐藏）", () => {
    render(<Harness backendType="openclaw" />);

    const pill = screen.getByTestId("provider-pill");
    expect(pill).toBeDisabled();
    expect(pill).toHaveAttribute(
      "title",
      "This backend does not use agentre providers",
    );
    expect(appMocks.ListLLMProviders).not.toHaveBeenCalled();
  });

  it("无兼容供应商（列表拉取成功但为空）→ pill disabled + tooltip 说明原因", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: [CHAT_PROVIDER] });
    render(<Harness backendType="claudecode" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).toBeDisabled());
    expect(pill).toHaveAttribute(
      "title",
      "No available provider matches this backend type",
    );
  });

  it("弹层底部常显「自下一轮生效」说明（不随轮状态变化）", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    render(<Harness backendType="builtin" />);

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);

    expect(
      screen.getByText(
        "Switching takes effect from the next turn; the turn in progress is unaffected",
      ),
    ).toBeInTheDocument();
  });
});

describe("ProviderPill · 已有会话（决策 1/9/10：选择立即持久化 + 切换 notice）", () => {
  it("已有会话水合当前选择：providerKey + modelKey 显示会话已持久化的目标", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    appMocks.ListLLMModels.mockResolvedValue({
      items: [model("mk-sonnet", "claude-sonnet-4-5")],
    });
    render(
      <Harness
        backendType="claudecode"
        sessionId={42}
        persistedProviderKey="acme-anthropic"
        persistedModelKey="mk-sonnet"
      />,
    );

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    // 目录异步加载（providers 空时 catalogLoading 已为 false，不能只等 not.toBeDisabled），
    // 必须等模型目录解析完成、pill 标签显示「供应商 · 模型」才断言。
    await waitFor(() =>
      expect(screen.getByTestId("provider-pill")).toHaveTextContent(
        "Acme Claude · claude-sonnet-4-5",
      ),
    );
  });

  it("选中 provider-default 调用 SetChatSessionModelTarget 并按响应更新显示，随后触发 onSwitched", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    appMocks.SetChatSessionModelTarget.mockResolvedValue({
      providerKey: "acme-anthropic",
      modelKey: "",
      agentProviderKey: "",
      agentModelKey: "",
    });
    const onSwitched = vi.fn();
    render(
      <Harness
        backendType="claudecode"
        sessionId={42}
        persistedProviderKey=""
        onSwitched={onSwitched}
      />,
    );

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /Acme Claude/,
      }),
    );

    expect(appMocks.SetChatSessionModelTarget).toHaveBeenCalledWith({
      sessionId: 42,
      providerKey: "acme-anthropic",
      modelKey: "",
    });
    await waitFor(() =>
      expect(screen.getByTestId("provider-pill")).toHaveTextContent(
        "Acme Claude",
      ),
    );
    await waitFor(() => expect(onSwitched).toHaveBeenCalledTimes(1));
  });

  it("清回「跟随 agent 绑定」调用 SetChatSessionModelTarget 传双空串，pill 落回绑定供应商名", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    appMocks.SetChatSessionModelTarget.mockResolvedValue({
      providerKey: "",
      modelKey: "",
      agentProviderKey: "acme-anthropic",
      agentModelKey: "",
    });
    render(
      <Harness
        backendType="claudecode"
        sessionId={42}
        boundProviderKey="acme-anthropic"
        persistedProviderKey="acme-response"
      />,
    );

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /Follow agent binding/i,
      }),
    );

    expect(appMocks.SetChatSessionModelTarget).toHaveBeenCalledWith({
      sessionId: 42,
      providerKey: "",
      modelKey: "",
    });
    await waitFor(() =>
      expect(screen.getByTestId("provider-pill")).toHaveTextContent(
        "Acme Claude",
      ),
    );
  });

  it("切换失败：回滚显示并在弹层底部报错", async () => {
    appMocks.ListLLMProviders.mockResolvedValue({ items: ALL_PROVIDERS });
    appMocks.SetChatSessionModelTarget.mockRejectedValue(new Error("nope"));
    render(
      <Harness
        backendType="claudecode"
        sessionId={42}
        persistedProviderKey=""
      />,
    );

    const pill = await waitFor(() => screen.getByTestId("provider-pill"));
    await waitFor(() => expect(pill).not.toBeDisabled());

    const user = userEvent.setup();
    await user.click(pill);
    await user.click(
      within(screen.getByRole("listbox")).getByRole("option", {
        name: /Acme Claude/,
      }),
    );

    await waitFor(() =>
      expect(screen.getByTestId("provider-pill")).toHaveTextContent(
        "Follow agent binding",
      ),
    );
    await user.click(screen.getByTestId("provider-pill"));
    expect(screen.getByText("nope")).toBeInTheDocument();
  });
});
