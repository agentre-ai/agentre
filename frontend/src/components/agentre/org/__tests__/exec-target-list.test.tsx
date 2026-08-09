import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ExecTargetList } from "../exec-target-list";
import type { agent_backend_svc } from "../../../../../wailsjs/go/models";

function backend(
  overrides: Partial<agent_backend_svc.BackendItem> = {},
): agent_backend_svc.BackendItem {
  return {
    id: 51,
    type: "claudecode",
    name: "claude-fable-5",
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
    deviceId: "",
    deviceName: "",
    online: false,
    agentCount: 0,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  } as agent_backend_svc.BackendItem;
}

const localBackend = (overrides: Partial<agent_backend_svc.BackendItem> = {}) =>
  backend({ id: 51, name: "claude-fable-5", ...overrides });
const remoteBackend = (
  overrides: Partial<agent_backend_svc.BackendItem> = {},
) =>
  backend({
    id: 52,
    name: "claude-opus-5",
    deviceId: "3",
    deviceName: "构建机",
    online: true,
    ...overrides,
  });

function availabilityStub(
  items: Array<{
    agentBackendId: number;
    available: boolean;
    reason?: string;
    hint?: string;
  }>,
) {
  const fn = vi
    .fn()
    .mockResolvedValue(items.map((it) => ({ reason: "", hint: "", ...it })));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = {
    app: { App: { ListAgentExecTargetAvailability: fn } },
  };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

describe("ExecTargetList", () => {
  it("single target: no sequence badge, no drag handle, shows Replace not per-row remove", async () => {
    availabilityStub([{ agentBackendId: 51, available: true }]);
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }]}
        backends={[localBackend()]}
        onChange={vi.fn()}
      />,
    );
    expect(await screen.findByText("Local machine")).toBeInTheDocument();
    expect(screen.getByText(/claude-fable-5/)).toBeInTheDocument();
    expect(screen.queryByText("1")).toBeNull();
    expect(screen.getByRole("button", { name: /Replace/ })).toBeInTheDocument();
    expect(screen.queryByLabelText(/Move target down/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    // R20：只有一项时「当前生效」徽标也不出现——只有一档时说它是废话。
    expect(screen.queryByText("Currently active")).toBeNull();
  });

  it("multiple targets: renders sequence badges and per-row move buttons", async () => {
    availabilityStub([
      { agentBackendId: 51, available: true },
      { agentBackendId: 52, available: true },
    ]);
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }, { agentBackendId: 52 }]}
        backends={[localBackend(), remoteBackend()]}
        onChange={vi.fn()}
      />,
    );
    await screen.findByText("Local machine");
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /Move target down/ }),
    ).toHaveLength(2);
  });

  it("shows Currently active for the first available target and Online for a later available remote target", async () => {
    availabilityStub([
      { agentBackendId: 51, available: true },
      { agentBackendId: 52, available: true },
    ]);
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }, { agentBackendId: 52 }]}
        backends={[localBackend(), remoteBackend()]}
        onChange={vi.fn()}
      />,
    );
    expect(await screen.findByText("Currently active")).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
  });

  it("shows the block reason for an unavailable target", async () => {
    availabilityStub([
      {
        agentBackendId: 51,
        available: false,
        reason: "backend-requires-provider",
      },
    ]);
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }]}
        backends={[localBackend()]}
        onChange={vi.fn()}
      />,
    );
    expect(
      await screen.findByText("An LLM provider must be specified"),
    ).toBeInTheDocument();
  });

  it("shows an all-unavailable banner listing each target's reason", async () => {
    availabilityStub([
      {
        agentBackendId: 51,
        available: false,
        reason: "backend-requires-provider",
      },
      { agentBackendId: 52, available: false, reason: "exec-target-offline" },
    ]);
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }, { agentBackendId: 52 }]}
        backends={[localBackend(), remoteBackend({ online: false })]}
        onChange={vi.fn()}
      />,
    );
    expect(
      await screen.findByText(
        '"开发" has no available execution target right now',
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Offline").length).toBeGreaterThan(0);
  });

  it("empty list: shows the CTA and, when saveRejected, the rejection message", () => {
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[]}
        backends={[localBackend()]}
        onChange={vi.fn()}
        saveRejected
      />,
    );
    expect(screen.getByText("No execution targets yet")).toBeInTheDocument();
    expect(
      screen.getByText(/Save rejected: add at least one execution target\./),
    ).toBeInTheDocument();
  });

  it("adding a target: opens the picker, disables the one already in the list, and appends the chosen one", async () => {
    availabilityStub([{ agentBackendId: 51, available: true }]);
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }]}
        backends={[
          localBackend(),
          backend({ id: 52, name: "gpt-5-codex", type: "codex" }),
        ]}
        onChange={onChange}
      />,
    );
    await screen.findByText("Local machine");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Already in the list")).toBeInTheDocument();
    await user.click(screen.getByText(/gpt-5-codex/));
    expect(onChange).toHaveBeenCalledWith([
      { agentBackendId: 51 },
      { agentBackendId: 52 },
    ]);
  });

  it("single target: Replace swaps the sole target instead of appending", async () => {
    availabilityStub([{ agentBackendId: 51, available: true }]);
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }]}
        backends={[
          localBackend(),
          backend({ id: 52, name: "gpt-5-codex", type: "codex" }),
        ]}
        onChange={onChange}
      />,
    );
    await screen.findByText("Local machine");
    await user.click(screen.getByRole("button", { name: /Replace/ }));
    await user.click(await screen.findByText(/gpt-5-codex/));
    expect(onChange).toHaveBeenCalledWith([{ agentBackendId: 52 }]);
  });

  it("removing a target (multi-target) drops it from the list", async () => {
    availabilityStub([
      { agentBackendId: 51, available: true },
      { agentBackendId: 52, available: true },
    ]);
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }, { agentBackendId: 52 }]}
        backends={[localBackend(), remoteBackend()]}
        onChange={onChange}
      />,
    );
    await screen.findByText("Local machine");
    await user.click(screen.getAllByRole("button", { name: "Remove" })[0]);
    expect(onChange).toHaveBeenCalledWith([{ agentBackendId: 52 }]);
  });

  it("keyboard equivalent: clicking Move target down reorders and announces via role=status", async () => {
    availabilityStub([
      { agentBackendId: 51, available: true },
      { agentBackendId: 52, available: true },
    ]);
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ExecTargetList
        agentId={7}
        agentName="开发"
        targets={[{ agentBackendId: 51 }, { agentBackendId: 52 }]}
        backends={[localBackend(), remoteBackend()]}
        onChange={onChange}
      />,
    );
    await screen.findByText("Local machine");
    const moveDown = screen.getAllByRole("button", {
      name: /Move target down/,
    })[0];
    await user.click(moveDown);
    expect(onChange).toHaveBeenCalledWith([
      { agentBackendId: 52 },
      { agentBackendId: 51 },
    ]);
    await waitFor(() =>
      expect(screen.getByRole("status")).not.toHaveTextContent(""),
    );
  });
});
