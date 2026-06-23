import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { TriStateToggle } from "../tri-state-toggle";

describe("TriStateToggle", () => {
  it("marks the active segment via aria-pressed", () => {
    render(
      <TriStateToggle
        value="off"
        labels={{ inherit: "继承", on: "开", off: "关" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByRole("button", { name: "关" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "继承" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("calls onChange with the clicked state", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TriStateToggle
        value="inherit"
        labels={{ inherit: "继承", on: "开", off: "关" }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "开" }));
    expect(onChange).toHaveBeenCalledWith("on");
  });
});
