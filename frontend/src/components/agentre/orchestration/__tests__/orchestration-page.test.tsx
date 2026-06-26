import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({
    run: { id: 1, goal: "G", status: "running", leaderAgentId: 2 },
    tasks: [],
  }),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
  LoadChatSession: vi.fn().mockResolvedValue({ messages: [] }),
  RunList: vi.fn().mockResolvedValue([]),
  RunPause: vi.fn(),
  RunResume: vi.fn(),
  RunStop: vi.fn(),
  RunSpeak: vi.fn(),
}));
vi.mock("../../../../hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

// Stub heavy sub-components to avoid cascading mock needs
vi.mock("../structure-graph", () => ({
  StructureGraph: () => <div data-testid="stub-structure-graph" />,
}));
vi.mock("../conversation-panel", () => ({
  ConversationPanel: () => <div data-testid="stub-conversation-panel" />,
}));

import { useOrchRunListStore } from "../../../../stores/orch-run-list-store";
import { OrchestrationPage } from "../orchestration-page";

const renderAt = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/orchestration" element={<OrchestrationPage />} />
        <Route path="/orchestration/:runId" element={<OrchestrationPage />} />
      </Routes>
    </MemoryRouter>,
  );

beforeEach(() => {
  useOrchRunListStore.setState({ runs: [] });
});

describe("OrchestrationPage", () => {
  it("无选中 Run:渲染 RunList(起步 CTA)+ 起步主区", () => {
    renderAt("/orchestration");
    expect(screen.getByTestId("run-onboarding-cta")).toBeInTheDocument();
    expect(screen.getByTestId("orchestration-empty-main")).toBeInTheDocument();
  });

  it("带 :runId:主区渲染 OrchestrationRun", () => {
    useOrchRunListStore.setState({
      runs: [{ id: 1, goal: "G", status: "running" } as never],
    });
    renderAt("/orchestration/1");
    expect(screen.getByTestId("orchestration-run")).toBeInTheDocument();
  });
});
