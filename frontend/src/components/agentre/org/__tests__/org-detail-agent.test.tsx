import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OrgDetailAgent } from "../org-detail-agent";
import type { OrgAgent, OrgDepartment } from "../types";
import type { agent_backend_svc } from "../../../../../wailsjs/go/models";

const agent = (overrides: Partial<OrgAgent> = {}): OrgAgent =>
  ({
    id: 7,
    name: "Eva",
    description: "工程总监",
    avatarColor: "agent-2",
    avatarDataUrl: "",
    systemBadge: "",
    departmentId: 1,
    departmentName: "工程部",
    parentAgentId: 0,
    parentAgentName: "",
    agentBackendId: 0,
    sortOrder: 1,
    prompt: ["你是 Eva。", "负责工程。"],
    skills: [
      { id: "superpowers@m", enabled: true },
      { id: "frontend-design@m", enabled: true },
    ],
    createtime: 0,
    updatetime: 0,
    ...overrides,
  }) as OrgAgent;

const dept: OrgDepartment = {
  id: 1,
  name: "工程部",
  description: "",
  icon: "hammer",
  accentColor: "agent-2",
  parentId: 0,
  leadAgentId: 0,
  leadAgentName: "",
  sortOrder: 1,
  directAgentCount: 1,
  subdepartmentCount: 0,
  memberCount: 1,
  createtime: 0,
  updatetime: 0,
} as OrgDepartment;

const backend = (
  overrides: Partial<agent_backend_svc.BackendItem> = {},
): agent_backend_svc.BackendItem =>
  ({
    id: 5,
    type: "claudecode",
    name: "Claude Code",
    llmProviderId: 3,
    llmProviderName: "Anthropic 官方",
    llmProviderType: "anthropic",
    llmProviderModel: "Sonnet 4.6",
    llmProviderActive: true,
    cliPath: "",
    modelRoutes: "{}",
    sandbox: "",
    approval: "",
    envJson: "{}",
    agentCount: 1,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  }) as agent_backend_svc.BackendItem;

function renderPanel(
  overrides: Partial<OrgAgent> = {},
  backends: agent_backend_svc.BackendItem[] = [],
  availableTools: string[] = [],
) {
  const onUpdate = vi.fn().mockResolvedValue(undefined);
  const onDelete = vi.fn().mockResolvedValue(undefined);
  const onUploadAvatar = vi.fn().mockResolvedValue(undefined);
  const onDeleteAvatar = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  render(
    <OrgDetailAgent
      agent={agent(overrides)}
      departments={[dept]}
      agents={[]}
      backends={backends}
      isLeadOf={null}
      availableTools={availableTools}
      onUpdate={onUpdate}
      onDelete={onDelete}
      onUploadAvatar={onUploadAvatar}
      onDeleteAvatar={onDeleteAvatar}
      onClose={onClose}
    />,
  );
  return { onUpdate, onDelete, onUploadAvatar, onDeleteAvatar };
}

function withCaps(caps: string[], packs: Array<Record<string, unknown>> = []) {
  // 面板渲染即调 GetBackendCapabilities（经 useBackendCapabilities）。
  // 该 binding 走真实 wailsjs App.js（读 window.go.app.App.*），故测试
  // 把返回挂到 window.go 上覆盖默认空能力（与 chat-panel 等既有模式一致）。
  // 技能区在 caps 含 "skills" 时挂载即拉目录，故 packs 可注入 globallyEnabled 等。
  window.go = {
    app: {
      App: {
        GetBackendCapabilities: vi.fn().mockResolvedValue({
          capabilities: caps,
          permissionModeMeta: null,
        }),
        ListAgentSkillPacks: vi.fn().mockResolvedValue({ packs }),
      },
    },
  };
}

beforeEach(() => {
  // 默认空能力 + 空技能目录，保证未显式 withCaps 的面板测试也能渲染
  // （否则真实 App.js 在 window.go 缺失时抛 Cannot read 'app'）。
  withCaps([]);
});

afterEach(() => {
  delete window.go;
});

describe("OrgDetailAgent", () => {
  it("does not render role or initials inputs", () => {
    renderPanel();
    expect(screen.queryByLabelText("agent-role")).toBeNull();
    expect(screen.queryByLabelText("agent-initials")).toBeNull();
  });

  it("renders only the four design-aligned basic fields", () => {
    renderPanel();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Description")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Upload Image" }),
    ).toBeInTheDocument();
    expect(screen.getByText("PNG / JPG / WEBP · Max 2 MB")).toBeInTheDocument();
    expect(
      screen.getByRole("radiogroup", { name: "Avatar Color" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("radio", { name: /Avatar color agent-/ }),
    ).toHaveLength(16);
    expect(
      screen.getByRole("radio", { name: "Avatar color agent-16" }),
    ).toBeInTheDocument();
    // 头像编辑入口仍用于选择图标 / 字母备用头像。
    expect(screen.getAllByLabelText("Change avatar").length).toBeGreaterThan(0);
  });

  it("keeps image upload out of the avatar popover", async () => {
    const user = userEvent.setup();
    renderPanel();
    await user.click(screen.getAllByLabelText("Change avatar")[0]);
    expect(
      await screen.findByRole("tab", { name: /Icon/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Letter/ })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /Image/ })).toBeNull();
  });

  it("renders the Agent backend section like the Pencil detail card", () => {
    renderPanel({ agentBackendId: 5 }, [backend()]);
    expect(
      screen.getByRole("heading", { name: "Agent Backend" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Agent Backend")).toBeInTheDocument();
    expect(screen.getByText("Claude Code")).toBeInTheDocument();
    expect(screen.getByText("Anthropic 官方 · Sonnet 4.6")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Runs tasks through Claude Code CLI. Tool calls are routed through the CLI.",
      ),
    ).toBeInTheDocument();
  });

  it("falls back to the Agent backend summary when the backend list is empty", () => {
    renderPanel({
      agentBackendId: 5,
      backend: {
        id: 5,
        type: "claudecode",
        name: "Claude Code",
        llmProviderName: "Anthropic 官方",
        llmProviderModel: "Sonnet 4.6",
        llmProviderActive: true,
      },
    });
    expect(screen.getByText("Claude Code")).toBeInTheDocument();
    expect(screen.getByText("Anthropic 官方 · Sonnet 4.6")).toBeInTheDocument();
  });

  it("counts non-whitespace chars in system prompt", async () => {
    const user = userEvent.setup();
    renderPanel({ prompt: [] });
    const ta = screen.getByLabelText("System Prompt");
    await user.type(ta, "hello 世界");
    expect(screen.getByText(/7 chars/)).toBeInTheDocument();
  });

  it("uploads avatar from the inline detail row control", async () => {
    const { onUploadAvatar } = renderPanel();
    expect(
      screen.getByRole("button", { name: "Upload Image" }),
    ).toBeInTheDocument();
    const file = new File([new Uint8Array([1, 2, 3])], "a.png", {
      type: "image/png",
    });
    const input = document.querySelector(
      'input[type="file"]',
    ) as HTMLInputElement | null;
    if (!input) throw new Error("file input not found");
    fireEvent.change(input, { target: { files: [file] } });
    await new Promise((r) => setTimeout(r, 50));
    expect(onUploadAvatar).toHaveBeenCalledWith({
      id: 7,
      dataUrl: expect.stringMatching(/^data:image\/png;base64,/),
    });
  });

  it("deletes an uploaded avatar from the inline detail row control", async () => {
    const user = userEvent.setup();
    const { onDeleteAvatar } = renderPanel({
      avatarDataUrl: "data:image/png;base64,aGVsbG8=",
    });
    await user.click(
      screen.getByRole("button", { name: "Delete uploaded avatar" }),
    );
    expect(onDeleteAvatar).toHaveBeenCalledWith({ id: 7 });
  });

  it("rejects inline avatar files over 2MB before upload", async () => {
    const { onUploadAvatar } = renderPanel();
    const file = new File([new Uint8Array(2 * 1024 * 1024 + 1)], "large.png", {
      type: "image/png",
    });
    const input = document.querySelector(
      'input[type="file"]',
    ) as HTMLInputElement | null;
    if (!input) throw new Error("file input not found");
    fireEvent.change(input, { target: { files: [file] } });
    expect(
      await screen.findByText("Image is too large. Upload a file under 2 MB."),
    ).toBeInTheDocument();
    expect(onUploadAvatar).not.toHaveBeenCalled();
  });

  it("shows the skills section for a claudecode backend (CapSkills)", async () => {
    withCaps(["skills", "mcp_tools"]);
    renderPanel({ agentBackendId: 5 }, [
      backend({ id: 5, type: "claudecode" }),
    ]);
    expect(await screen.findByText("Skills · Skill Packs")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Manage skills" }),
    ).toBeInTheDocument();
  });

  it("shows the gating box for a non-claudecode backend", async () => {
    withCaps(["mcp_tools"]); // codex: no skills cap
    renderPanel({ agentBackendId: 6 }, [backend({ id: 6, type: "codex" })]);
    expect(
      await screen.findByText("This backend doesn't support skills"),
    ).toBeInTheDocument();
  });

  it("granted skill chips render derived names from pack ids", async () => {
    withCaps(["skills", "mcp_tools"]);
    renderPanel({ agentBackendId: 5 }, [
      backend({ id: 5, type: "claudecode" }),
    ]);
    expect(await screen.findByText("superpowers")).toBeInTheDocument();
    expect(screen.getByText("frontend-design")).toBeInTheDocument();
  });

  it("renders the Tools section heading + Add Tool button (no switch)", async () => {
    withCaps(["skills", "mcp_tools"]);
    renderPanel(
      { tools: [], agentBackendId: 5 },
      [backend({ id: 5, type: "claudecode" })],
      ["org"],
    );
    expect(await screen.findByText("Tools · TOOLS")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Add Tool" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: /Org Structure/i })).toBeNull();
  });

  it("grants the org tool via the tool picker and saves it", async () => {
    withCaps(["skills", "mcp_tools"]);
    const user = userEvent.setup();
    const { onUpdate } = renderPanel(
      { tools: [], agentBackendId: 5 },
      [backend({ id: 5, type: "claudecode" })],
      ["org"],
    );
    await user.click(await screen.findByRole("button", { name: "Add Tool" }));
    await user.click(
      await screen.findByRole("checkbox", { name: "Org Structure" }),
    );
    await user.click(screen.getByText("Done"));
    const saveBtn = screen
      .getAllByRole("button")
      .find((b) => b.textContent?.trim() === "Save");
    if (!saveBtn) throw new Error("Save button not found");
    await user.click(saveBtn);
    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        tools: expect.arrayContaining([
          expect.objectContaining({ key: "org", enabled: true }),
        ]),
      }),
    );
  });

  it("renders the Org Structure chip when the org tool is enabled", async () => {
    withCaps(["skills", "mcp_tools"]);
    renderPanel(
      { tools: [{ key: "org", enabled: true }], agentBackendId: 5 },
      [backend({ id: 5, type: "claudecode" })],
      ["org"],
    );
    await screen.findByText("Tools · TOOLS");
    expect(screen.getByText("Org Structure")).toBeInTheDocument();
  });

  it("grants the workflow tool via the tool picker and saves it", async () => {
    withCaps(["skills", "mcp_tools"]);
    const user = userEvent.setup();
    const { onUpdate } = renderPanel(
      { tools: [], agentBackendId: 5 },
      [backend({ id: 5, type: "claudecode" })],
      ["org", "workflow"],
    );
    await user.click(await screen.findByRole("button", { name: "Add Tool" }));
    await user.click(
      await screen.findByRole("checkbox", { name: "Workflow Library" }),
    );
    await user.click(screen.getByText("Done"));
    const saveBtn = screen
      .getAllByRole("button")
      .find((b) => b.textContent?.trim() === "Save");
    if (!saveBtn) throw new Error("Save button not found");
    await user.click(saveBtn);
    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        tools: expect.arrayContaining([
          expect.objectContaining({ key: "workflow", enabled: true }),
        ]),
      }),
    );
  });

  it("renders the Workflow Library chip when the workflow tool is enabled", async () => {
    withCaps(["skills", "mcp_tools"]);
    renderPanel(
      { tools: [{ key: "workflow", enabled: true }], agentBackendId: 5 },
      [backend({ id: 5, type: "claudecode" })],
      ["org", "workflow"],
    );
    await screen.findByText("Tools · TOOLS");
    expect(screen.getByText("Workflow Library")).toBeInTheDocument();
    expect(screen.getByText("Approval")).toBeInTheDocument();
  });

  it("renders a globally-on pack as a locked (non-removable) inherited chip", async () => {
    withCaps(
      ["skills"],
      [
        {
          id: "global-on@m",
          name: "global-on",
          description: "globally enabled pack",
          skills: ["a", "b"],
          source: "m",
          recommended: false,
          installed: true,
          enabled: true,
          globallyEnabled: true,
        },
      ],
    );
    // agent 本地无 override：芯片纯由全局已启用 overlay 派生 → 锁定不可移除。
    renderPanel({ agentBackendId: 5, skills: [] }, [
      backend({ id: 5, type: "claudecode" }),
    ]);
    const chip = await screen.findByText("global-on");
    expect(chip).toBeInTheDocument();
    // 继承芯片无移除按钮（locked）。
    expect(
      screen.queryByRole("button", { name: "Remove global-on" }),
    ).toBeNull();
  });

  it("renders the reports-to badge avatar at a legible size (size-5, not the cramped size-3.5)", () => {
    const target = agent({ id: 99, name: "Boss", avatarColor: "agent-3" });
    render(
      <OrgDetailAgent
        agent={agent({ parentAgentId: 99 })}
        departments={[dept]}
        agents={[target]}
        backends={[]}
        isLeadOf={null}
        availableTools={[]}
        onUpdate={vi.fn().mockResolvedValue(undefined)}
        onDelete={vi.fn().mockResolvedValue(undefined)}
        onUploadAvatar={vi.fn().mockResolvedValue(undefined)}
        onDeleteAvatar={vi.fn().mockResolvedValue(undefined)}
        onClose={vi.fn()}
      />,
    );
    // 汇报给徽章里的目标头像（role=img, aria-label=目标名）必须 ≥ 行高 (16px)，
    // 否则字母/图标被挤成 8px 糊点。size-3.5(14px) 是历史遗留的过小值。
    const avatar = screen.getByRole("img", { name: "Boss" });
    expect(avatar.className).toContain("size-5");
    expect(avatar.className).not.toContain("size-3.5");
  });

  it("forces a globally-off installed pack to off and saves enabled:false", async () => {
    withCaps(
      ["skills"],
      [
        {
          id: "global-off@m",
          name: "global-off",
          description: "installed, globally disabled",
          skills: ["c"],
          source: "m",
          recommended: false,
          installed: true,
          enabled: false,
          globallyEnabled: false,
        },
      ],
    );
    const user = userEvent.setup();
    const { onUpdate } = renderPanel({ agentBackendId: 5, skills: [] }, [
      backend({ id: 5, type: "claudecode" }),
    ]);
    await user.click(
      await screen.findByRole("button", { name: "Manage skills" }),
    );
    // 三态行的「Off」分段：把该 installed pack 强制关。
    await user.click(await screen.findByRole("button", { name: "Off" }));
    await user.click(screen.getByText("Done"));
    const saveBtn = screen
      .getAllByRole("button")
      .find((b) => b.textContent?.trim() === "Save");
    if (!saveBtn) throw new Error("Save button not found");
    await user.click(saveBtn);
    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        skills: expect.arrayContaining([
          expect.objectContaining({ id: "global-off@m", enabled: false }),
        ]),
      }),
    );
  });
});
