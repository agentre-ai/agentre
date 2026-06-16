import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { QuitConfirmDialog } from "../quit-confirm-dialog";

import type { AgentStatus } from "@/stores/types";

const runtimeMocks = vi.hoisted(() => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => runtimeMocks);

function installConfirmQuit() {
  const ConfirmQuit = vi.fn(() => Promise.resolve());
  Object.defineProperty(window, "go", {
    configurable: true,
    value: { app: { App: { ConfirmQuit } } },
  });
  return ConfirmQuit;
}

function emitQuitBlocked(count: number) {
  const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
    [string, (p: { count: number }) => void]
  >;
  const entry = calls.find((c) => c[0] === "app:quit-blocked");
  if (!entry) throw new Error("app:quit-blocked handler was never registered");
  act(() => entry[1]({ count }));
}

function seedSession(
  id: number,
  status: AgentStatus,
  agentName: string,
  title: string,
) {
  useSessionStatusStore.getState().upsert(id, {
    agentStatus: status,
    needsAttention: status === "waiting",
  });
  useSessionMetaStore.getState().setMeta(id, {
    agentId: id,
    agentName,
    agentColor: "agent-1",
    title,
  });
}

beforeEach(() => {
  useSessionStatusStore.getState().__reset();
  useSessionMetaStore.getState().__reset();
  runtimeMocks.EventsOn.mockReset();
  runtimeMocks.EventsOn.mockImplementation(() => vi.fn());
});

describe("QuitConfirmDialog", () => {
  it("stays closed until a quit-blocked event arrives", () => {
    installConfirmQuit();
    render(<QuitConfirmDialog />);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens and lists the in-progress sessions when quit is blocked", async () => {
    installConfirmQuit();
    seedSession(1, "running", "Backend refactor", "Drop group table");
    seedSession(2, "waiting", "Frontend i18n", "Proofread copy");
    render(<QuitConfirmDialog />);
    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());

    emitQuitBlocked(2);

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Backend refactor")).toBeInTheDocument();
    expect(screen.getByText("Frontend i18n")).toBeInTheDocument();
  });

  it("quits when the user confirms", async () => {
    const ConfirmQuit = installConfirmQuit();
    seedSession(1, "running", "Backend refactor", "Drop group table");
    render(<QuitConfirmDialog />);
    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    emitQuitBlocked(1);

    fireEvent.click(
      await screen.findByRole("button", { name: /quit anyway/i }),
    );

    expect(ConfirmQuit).toHaveBeenCalledTimes(1);
  });

  it("closes without quitting when the user cancels", async () => {
    const ConfirmQuit = installConfirmQuit();
    seedSession(1, "running", "Backend refactor", "Drop group table");
    render(<QuitConfirmDialog />);
    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    emitQuitBlocked(1);
    expect(await screen.findByRole("dialog")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(ConfirmQuit).not.toHaveBeenCalled();
  });

  it("caps the visible list at three rows and summarizes the rest", async () => {
    installConfirmQuit();
    for (let i = 1; i <= 6; i++) {
      seedSession(i, "running", `Agent ${i}`, `Task ${i}`);
    }
    render(<QuitConfirmDialog />);
    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());

    emitQuitBlocked(6);

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText(/3 more/i)).toBeInTheDocument();
  });
});
