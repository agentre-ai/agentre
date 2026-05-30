import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

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
      { label: "read_file", enabled: true },
      { label: "write_file", enabled: false },
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
      onUpdate={onUpdate}
      onDelete={onDelete}
      onUploadAvatar={onUploadAvatar}
      onDeleteAvatar={onDeleteAvatar}
      onClose={onClose}
    />,
  );
  return { onUpdate, onDelete, onUploadAvatar, onDeleteAvatar };
}

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
    ).toHaveLength(10);
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

  it("toggles skill enabled state and updates counter", async () => {
    const user = userEvent.setup();
    renderPanel();
    expect(screen.getByText(/1 enabled · 1 disabled/)).toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: /write_file/ }));
    expect(screen.getByText(/2 enabled · 0 disabled/)).toBeInTheDocument();
  });
});
