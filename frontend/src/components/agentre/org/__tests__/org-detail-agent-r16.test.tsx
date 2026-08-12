import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OrgDetailAgent } from "../org-detail-agent";
import type { OrgAgent, OrgDepartment } from "../types";
import type {
  agent_backend_svc,
  agent_svc,
} from "../../../../../wailsjs/go/models";

// R16：组织架构的执行目标列表默认编辑**本端顺序**（覆盖，不同步），并给出
// 「恢复为账号默认顺序」与切到账号默认顺序的入口。

const agent = (overrides: Partial<OrgAgent> = {}): OrgAgent =>
  ({
    id: 7,
    name: "Eva",
    description: "工程总监",
    avatarColor: "agent-2",
    avatarDataUrl: "",
    systemBadge: "",
    departmentId: 1,
    parentAgentId: 0,
    agentBackendId: 51,
    sortOrder: 1,
    prompt: [],
    skills: [],
    execTargets: [
      { id: 1, agentBackendId: 51, skills: [] },
      { id: 2, agentBackendId: 52, skills: [] },
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
  sortOrder: 1,
  directAgentCount: 1,
  subdepartmentCount: 0,
  memberCount: 1,
  createtime: 0,
  updatetime: 0,
} as OrgDepartment;

function backend(id: number, deviceId = ""): agent_backend_svc.BackendItem {
  return {
    id,
    type: "claudecode",
    name: `claude-${id}`,
    llmProviderKey: "",
    llmProviderName: "",
    llmProviderType: "",
    llmProviderModel: "",
    llmProviderActive: false,
    cliPath: "",
    modelRoutes: "{}",
    sandbox: "",
    approval: "",
    envJson: "{}",
    reasoningEffort: "",
    defaultPermissionMode: "",
    defaultModel: "",
    openClawGatewayUrl: "",
    openClawAgentId: "",
    openClawDefaultModel: "",
    openClawSessionMode: "",
    hasToken: false,
    deviceId,
    deviceName: "",
    online: false,
    agentCount: 0,
    createtime: 0,
    updatetime: 0,
  } as agent_backend_svc.BackendItem;
}

// availabilityStub 让 ListAgentExecTargetAvailability 返回**解析后顺序**（R14），
// hasOverride 标注本端覆盖。
function availabilityStub(
  items: Array<{ agentBackendId: number; available: boolean }>,
  hasOverride = true,
) {
  const fn = vi
    .fn()
    .mockResolvedValue(
      items.map((it) => ({ reason: "", hint: "", hasOverride, ...it })),
    );
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = {
    app: {
      App: {
        ListAgentExecTargetAvailability: fn,
        GetBackendCapabilities: vi.fn().mockResolvedValue({}),
        ListAgentSkillPacks: vi.fn().mockResolvedValue({}),
      },
    },
  };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

function renderPanel(
  onUpdate: (req: agent_svc.UpdateAgentRequest) => Promise<unknown>,
) {
  render(
    <MemoryRouter initialEntries={["/org"]}>
      <OrgDetailAgent
        agent={agent()}
        departments={[dept]}
        agents={[]}
        backends={[backend(51), backend(52)]}
        isLeadOf={null}
        availableTools={[]}
        onUpdate={onUpdate}
        onDelete={vi.fn().mockResolvedValue(undefined)}
        onUploadAvatar={vi.fn().mockResolvedValue(undefined)}
        onDeleteAvatar={vi.fn().mockResolvedValue(undefined)}
        onClose={vi.fn()}
      />
    </MemoryRouter>,
  );
}

describe("OrgDetailAgent exec target scope (R16)", () => {
  it("device scope is the default: shows this-device hint and restore action, and a reorder writes the local override only", async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    // 解析后顺序：本端覆盖把 52 提到 51 之前。
    availabilityStub(
      [
        { agentBackendId: 52, available: true },
        { agentBackendId: 51, available: true },
      ],
      true,
    );
    renderPanel(onUpdate);

    // 设备顺序作用域默认：作用域提示可见 + 「恢复为账号默认顺序」按钮在。
    expect(
      await screen.findByText(/not synced to other devices/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Restore account default order" }),
    ).toBeInTheDocument();
    // 设备作用域没有「添加」入口（增删档是账号级）。
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();

    // 拖拽/移动重排第一档（52）→ 只写本端覆盖 orderOverride，不写账号默认。
    const user = userEvent.setup();
    const moveDownButtons = await screen.findAllByLabelText(/Move target down/);
    await user.click(moveDownButtons[0]);
    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        orderOverride: [51, 52],
      }),
    );
  });

  it("restore clears the local override by writing an empty orderOverride", async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    availabilityStub(
      [
        { agentBackendId: 52, available: true },
        { agentBackendId: 51, available: true },
      ],
      true,
    );
    renderPanel(onUpdate);

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", {
        name: "Restore account default order",
      }),
    );
    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        orderOverride: [],
      }),
    );
  });
});
