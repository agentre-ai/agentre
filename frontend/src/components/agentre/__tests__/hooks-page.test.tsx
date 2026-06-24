import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HooksPage } from "../hooks-page";

type AnyRecord = Record<string, unknown>;

function makeHook(over: AnyRecord = {}) {
  return {
    id: 2,
    name: "Jira urgent",
    interpreter: "bash",
    command: "echo '{\"events\":[]}'",
    scheduleExpr: "*/5 * * * *",
    timezone: "Asia/Shanghai",
    env: [{ key: "JIRA_TOKEN", value: "********", secret: true }],
    enabled: true,
    nextRunAt: 0,
    lastRunAt: Math.floor(Date.now() / 1000) - 120,
    lastStatus: "ok",
    lastError: "",
    lastDurationMs: 412,
    totalCount: 37,
    createtime: 0,
    updatetime: 0,
    ...over,
  };
}

const sampleEvent = {
  id: 100,
  hookId: 2,
  title: "payment callback timeout",
  dedupeKey: "OPS-4821",
  payloadJson: '{"severity":"high"}',
  receivedAt: Math.floor(Date.now() / 1000) - 60,
  createtime: 0,
};

function setBridge(over: AnyRecord = {}) {
  const hookA = makeHook();
  const hookB = makeHook({
    id: 3,
    name: "RSS advisories",
    interpreter: "node",
    enabled: false,
    lastStatus: "",
  });
  const app = {
    LoadHooks: vi.fn(() =>
      Promise.resolve({ hooks: [hookA, hookB], events: [sampleEvent] }),
    ),
    CreateHook: vi.fn((req: AnyRecord) =>
      Promise.resolve(makeHook({ id: 9, ...req })),
    ),
    UpdateHook: vi.fn((req: AnyRecord) => Promise.resolve(makeHook(req))),
    DeleteHook: vi.fn(() => Promise.resolve()),
    ToggleHook: vi.fn((id: number, enabled: boolean) =>
      Promise.resolve(makeHook({ id, enabled })),
    ),
    RunHook: vi.fn(() =>
      Promise.resolve({
        exitCode: 0,
        durationMs: 412,
        timedOut: false,
        stdout: '{"events":[]}',
        stderr: "",
        parseError: "",
        events: [sampleEvent],
        newCount: 1,
        dupCount: 1,
        persisted: false,
      }),
    ),
    ...over,
  };
  Object.defineProperty(window, "go", {
    configurable: true,
    value: { app: { App: app } },
  });
  return app;
}

afterEach(() => {
  delete (window as unknown as { go?: unknown }).go;
});

describe("HooksPage", () => {
  it("loads and lists hooks, auto-selecting the first into the header", async () => {
    setBridge();
    render(<HooksPage />);

    // both hooks appear in the list
    expect(await screen.findAllByText("Jira urgent")).not.toHaveLength(0);
    expect(screen.getByText("RSS advisories")).toBeInTheDocument();
    // disabled hook shows the "Off" label instead of a status dot
    expect(screen.getByText("Off")).toBeInTheDocument();
    // header reflects the selected hook + kind + tabs
    expect(screen.getByText("Bash Hook")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Script" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Run Log/ })).toBeInTheDocument();
  });

  it("edits cron + command and saves via UpdateHook", async () => {
    const app = setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    fireEvent.change(screen.getByLabelText("cron expression"), {
      target: { value: "0 * * * *" },
    });
    fireEvent.change(screen.getByLabelText("Script"), {
      target: { value: "echo hi" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(app.UpdateHook).toHaveBeenCalled());
    expect(app.UpdateHook).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 2,
        scheduleExpr: "0 * * * *",
        command: "echo hi",
      }),
    );
  });

  it("adds a secret env var that round-trips into the save payload", async () => {
    const app = setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    await userEvent.click(screen.getByRole("button", { name: "Add variable" }));
    const keyInputs = screen.getAllByLabelText("KEY");
    const valueInputs = screen.getAllByLabelText("value");
    fireEvent.change(keyInputs[keyInputs.length - 1], {
      target: { value: "API_KEY" },
    });
    fireEvent.change(valueInputs[valueInputs.length - 1], {
      target: { value: "s3cr3t" },
    });
    const secretSwitches = screen.getAllByLabelText("Secret");
    await userEvent.click(secretSwitches[secretSwitches.length - 1]);

    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(app.UpdateHook).toHaveBeenCalled());
    const arg = app.UpdateHook.mock.calls[0][0] as { env: Array<AnyRecord> };
    expect(arg.env).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          key: "API_KEY",
          value: "s3cr3t",
          secret: true,
        }),
      ]),
    );
  });

  it("dry-runs and shows the inline result", async () => {
    const app = setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    await userEvent.click(screen.getByRole("button", { name: "Dry run" }));
    await waitFor(() =>
      expect(app.RunHook).toHaveBeenCalledWith({ id: 2, dryRun: true }),
    );
    expect(await screen.findByText("Dry run · exit 0")).toBeInTheDocument();
    expect(screen.getByText("412ms · not persisted")).toBeInTheDocument();
  });

  it("shows produced events in the Run Log tab with payload detail", async () => {
    setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    await userEvent.click(screen.getByRole("tab", { name: /Run Log/ }));
    // first event auto-selects → its title shows in both list + detail
    expect(
      (await screen.findAllByText("payment callback timeout")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/OPS-4821/)).toBeInTheDocument();
    expect(screen.getByText(/"severity":"high"/)).toBeInTheDocument();
  });

  it("toggles the selected hook", async () => {
    const app = setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    await userEvent.click(screen.getByRole("button", { name: "Disable" }));
    await waitFor(() => expect(app.ToggleHook).toHaveBeenCalledWith(2, false));
  });

  it("creates a new hook from the + button", async () => {
    const app = setBridge();
    render(<HooksPage />);
    await screen.findAllByText("Jira urgent");

    await userEvent.click(screen.getByRole("button", { name: "New Hook" }));
    expect(screen.getByLabelText("Name")).toHaveValue("New Hook");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(app.CreateHook).toHaveBeenCalled());
  });
});
