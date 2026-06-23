import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ProjectCreate: vi.fn(),
  ProjectDetectGitRepo: vi.fn(),
  SelectDirectory: vi.fn(),
}));

vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { ProjectNewDialog } from "./project-new-dialog";

function renderDialog() {
  return render(
    <ProjectNewDialog
      open
      onOpenChange={vi.fn()}
      tree={[]}
      onCreated={vi.fn()}
    />,
  );
}

describe("ProjectNewDialog theme colors", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.ProjectCreate.mockResolvedValue({ id: 42 });
    appMocks.ProjectDetectGitRepo.mockResolvedValue({
      currentBranch: "",
      isGitRepo: false,
      origin: "",
    });
  });

  it("Given a new project dialog, when it opens, then the full theme palette is available", () => {
    renderDialog();

    expect(screen.getAllByRole("button", { name: /^agent-\d+$/ })).toHaveLength(
      16,
    );
    expect(screen.getByRole("button", { name: "agent-16" })).toBeVisible();
  });

  it("Given an expanded theme palette, when agent-16 is selected, then project creation submits that color", async () => {
    renderDialog();

    fireEvent.change(screen.getByPlaceholderText("/Users/you/Code/your-repo"), {
      target: { value: "/tmp/nebula" },
    });
    fireEvent.change(screen.getByPlaceholderText("Agentre"), {
      target: { value: "Nebula" },
    });
    fireEvent.click(screen.getByRole("button", { name: "agent-16" }));
    fireEvent.click(screen.getByRole("button", { name: "Create Project" }));

    await waitFor(() => {
      expect(appMocks.ProjectCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          color: "agent-16",
          name: "Nebula",
          path: "/tmp/nebula",
        }),
      );
    });
  });
});
