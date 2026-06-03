import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  IssueCreate: vi.fn(),
  IssueUpdate: vi.fn(),
}));
vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { IssueNewDialog } from "./issue-new-dialog";

const labels = [{ id: 1, name: "bug", tone: "bug" }];
const projects = [{ id: 5, name: "Agentre" }];

describe("IssueNewDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.IssueCreate.mockResolvedValue({ id: 9 });
  });

  it("creates an issue with the typed title", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onCreated = vi.fn();
    render(
      <IssueNewDialog
        open
        onOpenChange={() => {}}
        projects={projects}
        labels={labels}
        onSaved={onCreated}
      />,
    );
    await user.type(
      screen.getByRole("textbox", { name: /Title/i }),
      "fix OAuth",
    );
    await user.click(screen.getByRole("button", { name: /Create issue/i }));
    await waitFor(() =>
      expect(appMocks.IssueCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "fix OAuth",
          projectID: 0,
          labelIDs: [],
        }),
      ),
    );
    expect(onCreated).toHaveBeenCalled();
  });

  it("disables submit when the title is empty", async () => {
    render(
      <IssueNewDialog
        open
        onOpenChange={() => {}}
        projects={projects}
        labels={labels}
        onSaved={() => {}}
      />,
    );
    expect(
      screen.getByRole("button", { name: /Create issue/i }),
    ).toBeDisabled();
  });
});
