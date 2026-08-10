import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProjectMergeDialog } from "../project-merge-dialog";

function stubProjectMerge(
  impl: (...args: unknown[]) => unknown = () => Promise.resolve({ id: 2 }),
) {
  const fn = vi.fn(impl);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = { app: { App: { ProjectMerge: fn } } };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

describe("ProjectMergeDialog", () => {
  it("closed when source is null", () => {
    render(
      <ProjectMergeDialog
        source={null}
        candidates={[{ id: 2, name: "agentre-server" }]}
        onClose={vi.fn()}
        onMerged={vi.fn()}
      />,
    );
    expect(screen.queryByText("Merge into existing project")).toBeNull();
  });

  it("no candidates: shows the empty hint and disables confirm", () => {
    render(
      <ProjectMergeDialog
        source={{ id: 1, name: "agentre-hub (local)" }}
        candidates={[]}
        onClose={vi.fn()}
        onMerged={vi.fn()}
      />,
    );
    expect(
      screen.getByText("No other project to merge into"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Merge" })).toBeDisabled();
  });

  it("picking a target and confirming calls ProjectMerge with {sourceID, targetID} and onMerged on success", async () => {
    const fn = stubProjectMerge();
    const onMerged = vi.fn();
    render(
      <ProjectMergeDialog
        source={{ id: 1, name: "agentre-hub (local)" }}
        candidates={[{ id: 2, name: "agentre-hub" }]}
        onClose={vi.fn()}
        onMerged={onMerged}
      />,
    );

    await userEvent.click(
      screen.getByRole("combobox", { name: /merge into/i }),
    );
    await userEvent.click(await screen.findByText("agentre-hub"));

    expect(screen.getByRole("button", { name: "Merge" })).toBeEnabled();
    await userEvent.click(screen.getByRole("button", { name: "Merge" }));

    await waitFor(() => {
      expect(fn).toHaveBeenCalledWith({ sourceID: 1, targetID: 2 });
    });
    await waitFor(() => expect(onMerged).toHaveBeenCalledTimes(1));
  });

  it("merge failure shows the error and does not call onMerged", async () => {
    stubProjectMerge(() => Promise.reject(new Error("boom")));
    const onMerged = vi.fn();
    render(
      <ProjectMergeDialog
        source={{ id: 1, name: "agentre-hub (local)" }}
        candidates={[{ id: 2, name: "agentre-hub" }]}
        onClose={vi.fn()}
        onMerged={onMerged}
      />,
    );

    await userEvent.click(
      screen.getByRole("combobox", { name: /merge into/i }),
    );
    await userEvent.click(await screen.findByText("agentre-hub"));
    await userEvent.click(screen.getByRole("button", { name: "Merge" }));

    expect(await screen.findByText(/Merge failed/)).toBeInTheDocument();
    expect(onMerged).not.toHaveBeenCalled();
  });
});
