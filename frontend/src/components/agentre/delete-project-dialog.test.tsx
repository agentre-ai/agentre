import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ProjectDelete: vi.fn(),
}));

vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { DeleteProjectDialog } from "./delete-project-dialog";

describe("DeleteProjectDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.ProjectDelete.mockResolvedValue(undefined);
  });

  it("Given no target, when rendered, then no dialog is shown", () => {
    render(
      <DeleteProjectDialog
        target={null}
        onClose={vi.fn()}
        onDeleted={vi.fn()}
      />,
    );

    expect(
      screen.queryByRole("heading", { name: "Delete Project" }),
    ).not.toBeInTheDocument();
  });

  it("Given a target, when the confirm name does not match, then the delete button stays disabled and ProjectDelete is not called", () => {
    render(
      <DeleteProjectDialog
        target={{ id: 7, name: "Nebula" }}
        onClose={vi.fn()}
        onDeleted={vi.fn()}
      />,
    );

    const deleteBtn = screen.getByRole("button", { name: "Delete Project" });
    expect(deleteBtn).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("Nebula"), {
      target: { value: "wrong" },
    });
    expect(deleteBtn).toBeDisabled();

    fireEvent.click(deleteBtn);
    expect(appMocks.ProjectDelete).not.toHaveBeenCalled();
  });

  it("Given a target, when the exact name is entered and delete is clicked, then ProjectDelete is called with the id and onDeleted fires", async () => {
    const onDeleted = vi.fn();
    render(
      <DeleteProjectDialog
        target={{ id: 7, name: "Nebula" }}
        onClose={vi.fn()}
        onDeleted={onDeleted}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("Nebula"), {
      target: { value: "Nebula" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Delete Project" }));

    await waitFor(() => {
      expect(appMocks.ProjectDelete).toHaveBeenCalledWith(7);
    });
    await waitFor(() => {
      expect(onDeleted).toHaveBeenCalledTimes(1);
    });
  });

  it("Given ProjectDelete rejects, when delete is confirmed, then the error is shown and onDeleted does not fire", async () => {
    appMocks.ProjectDelete.mockRejectedValue(new Error("has active sessions"));
    const onDeleted = vi.fn();
    render(
      <DeleteProjectDialog
        target={{ id: 7, name: "Nebula" }}
        onClose={vi.fn()}
        onDeleted={onDeleted}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("Nebula"), {
      target: { value: "Nebula" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Delete Project" }));

    await waitFor(() => {
      expect(screen.getByText(/has active sessions/)).toBeInTheDocument();
    });
    expect(onDeleted).not.toHaveBeenCalled();
  });
});
