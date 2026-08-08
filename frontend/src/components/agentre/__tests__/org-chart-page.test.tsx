import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  consumeNewAgentDialogIntent,
  requestNewAgentDialog,
} from "@/stores/new-agent-intent-store";

const mocks = vi.hoisted(() => ({
  useOrgData: vi.fn(),
  useOrgTreeView: vi.fn(),
}));

vi.mock("../org/use-org-data", () => ({
  useOrgData: mocks.useOrgData,
}));
vi.mock("../org/use-org-tree-view", () => ({
  useOrgTreeView: mocks.useOrgTreeView,
}));
vi.mock("../org/org-tree", () => ({
  OrgTree: ({
    onCreateDepartment,
    onCreateAgent,
  }: {
    onCreateDepartment?: () => void;
    onCreateAgent?: () => void;
  }) => (
    <div data-testid="org-tree">
      <button type="button" onClick={onCreateDepartment}>
        tree-new-dept
      </button>
      {onCreateAgent ? (
        <button type="button" onClick={onCreateAgent}>
          tree-new-agent
        </button>
      ) : null}
    </div>
  ),
}));
vi.mock("../org/org-list", () => ({
  OrgList: () => <div data-testid="org-list" />,
}));
vi.mock("../org/org-detail-agent", () => ({
  OrgDetailAgent: () => <div data-testid="agent-detail" />,
}));
vi.mock("../org/org-detail-department", () => ({
  OrgDetailDepartment: () => <div data-testid="department-detail" />,
}));
vi.mock("../icon-picker", () => ({
  AgentAvatarPicker: () => <div data-testid="agent-avatar-picker" />,
  IconPicker: () => <div data-testid="icon-picker" />,
}));

import { OrgChartPage } from "../org-chart-page";

const CEO = {
  id: 1,
  name: "CEO 助手",
  departmentId: 0,
  parentAgentId: 0,
};

beforeEach(() => {
  consumeNewAgentDialogIntent();
  mocks.useOrgTreeView.mockReturnValue({
    collapse: {},
    toggleCollapse: vi.fn(),
    zoom: 1,
    setZoom: vi.fn(),
    zoomIn: vi.fn(),
    zoomOut: vi.fn(),
    zoomReset: vi.fn(),
    pan: { x: 0, y: 0 },
    setPan: vi.fn(),
    selected: null,
    setSelected: vi.fn(),
    viewMode: "tree",
    setViewMode: vi.fn(),
  });
  mocks.useOrgData.mockReturnValue({
    loading: false,
    error: null,
    departments: [],
    agents: [CEO],
    backends: [{ id: 5, name: "Claude Code", type: "claudecode" }],
    availableTools: [],
    moveAgent: vi.fn(),
    moveDepartment: vi.fn(),
    reorderAgents: vi.fn(),
    reorderDepartments: vi.fn(),
    updateDepartment: vi.fn(),
    deleteDepartment: vi.fn(),
    updateAgent: vi.fn(),
    deleteAgent: vi.fn(),
    uploadAgentAvatar: vi.fn(),
    deleteAgentAvatar: vi.fn(),
    createDepartment: vi.fn(),
    createAgent: vi.fn(),
  });
});

describe("OrgChartPage tree empty placeholder wiring", () => {
  it("opens the new department dialog from the tree onCreateDepartment handler", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={["/org"]}>
        <OrgChartPage />
      </MemoryRouter>,
    );
    await user.click(screen.getByRole("button", { name: "tree-new-dept" }));
    expect(
      await screen.findByRole("dialog", { name: "New Department" }),
    ).toBeInTheDocument();
  });

  it("opens the new agent dialog from the tree onCreateAgent handler", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={["/org"]}>
        <OrgChartPage />
      </MemoryRouter>,
    );
    await user.click(screen.getByRole("button", { name: "tree-new-agent" }));
    expect(
      await screen.findByRole("dialog", { name: "New Agent" }),
    ).toBeInTheDocument();
  });

  it("does not expose a tree add-agent action when there is no valid placement target", () => {
    mocks.useOrgData.mockReturnValue({
      loading: false,
      error: null,
      departments: [],
      agents: [],
      backends: [],
      availableTools: [],
      moveAgent: vi.fn(),
      moveDepartment: vi.fn(),
      reorderAgents: vi.fn(),
      reorderDepartments: vi.fn(),
      updateDepartment: vi.fn(),
      deleteDepartment: vi.fn(),
      updateAgent: vi.fn(),
      deleteAgent: vi.fn(),
      uploadAgentAvatar: vi.fn(),
      deleteAgentAvatar: vi.fn(),
      createDepartment: vi.fn(),
      createAgent: vi.fn(),
    });

    render(
      <MemoryRouter initialEntries={["/org"]}>
        <OrgChartPage />
      </MemoryRouter>,
    );

    expect(
      screen.queryByRole("button", { name: "tree-new-agent" }),
    ).not.toBeInTheDocument();
  });
});

describe("OrgChartPage navigation intents", () => {
  it("applies a no-backend agent selection when navigation targets an already-mounted org chart", async () => {
    const setSelected = vi.fn();
    mocks.useOrgTreeView.mockReturnValue({
      collapse: {},
      toggleCollapse: vi.fn(),
      zoom: 1,
      setZoom: vi.fn(),
      zoomIn: vi.fn(),
      zoomOut: vi.fn(),
      zoomReset: vi.fn(),
      pan: { x: 0, y: 0 },
      setPan: vi.fn(),
      selected: null,
      setSelected,
      viewMode: "tree",
      setViewMode: vi.fn(),
    });

    render(
      <MemoryRouter
        initialEntries={[{
          pathname: "/org",
          state: { orgSelection: { kind: "agent", id: 42 } },
        }]}
      >
        <OrgChartPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(setSelected).toHaveBeenCalledWith({ kind: "agent", id: 42 }),
    );
  });

  it("Given a pending intent, when the org chart mounts, then it opens the dialog, selects CEO placement, leaves backend empty, and consumes the intent", async () => {
    requestNewAgentDialog();

    render(
      <MemoryRouter initialEntries={["/org"]}>
        <OrgChartPage />
      </MemoryRouter>,
    );

    expect(
      await screen.findByText(
        "Configure the backend after creating the agent, then start chatting",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("combobox", {
        name: "Select a department or parent agent",
      }),
    ).toHaveTextContent("Agent · CEO 助手");
    expect(screen.getByRole("combobox", { name: "Backend" })).toHaveTextContent(
      "Backend",
    );
    await waitFor(() => expect(consumeNewAgentDialogIntent()).toBe(false));
  });

  it("Given no departments and an unordered agent list, when a pending intent opens the dialog, then CEO remains the selected parent", async () => {
    mocks.useOrgData.mockReturnValue({
      loading: false,
      error: null,
      departments: [],
      agents: [
        { id: 2, name: "Worker", systemBadge: "" },
        { id: 1, name: "CEO 助手", systemBadge: "DEFAULT" },
      ],
      backends: [{ id: 5, name: "Claude Code", type: "claudecode" }],
      availableTools: [],
      moveAgent: vi.fn(),
      moveDepartment: vi.fn(),
      reorderAgents: vi.fn(),
      reorderDepartments: vi.fn(),
      updateDepartment: vi.fn(),
      deleteDepartment: vi.fn(),
      updateAgent: vi.fn(),
      deleteAgent: vi.fn(),
      uploadAgentAvatar: vi.fn(),
      deleteAgentAvatar: vi.fn(),
      createDepartment: vi.fn(),
      createAgent: vi.fn(),
    });
    requestNewAgentDialog();

    render(
      <MemoryRouter initialEntries={["/org"]}>
        <OrgChartPage />
      </MemoryRouter>,
    );

    expect(
      await screen.findByRole("combobox", {
        name: "Select a department or parent agent",
      }),
    ).toHaveTextContent("Agent · CEO 助手");
  });
});
