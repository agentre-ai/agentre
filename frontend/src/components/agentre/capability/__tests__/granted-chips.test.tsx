import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Boxes } from "lucide-react";
import { describe, expect, it, vi } from "vitest";

import { GrantedChips } from "../granted-chips";

function setup(extra = {}) {
  const onRemove = vi.fn();
  const onAdd = vi.fn();
  render(
    <GrantedChips
      title="技能 · SKILL PACKS"
      countLabel="2 已启用"
      chipIcon={Boxes}
      chips={[
        { id: "sp@m", label: "superpowers", count: 14 },
        { id: "fd@m", label: "frontend-design", count: 1 },
      ]}
      addLabel="添加技能"
      removeLabel={(name) => `移除 ${name}`}
      onRemove={onRemove}
      onAdd={onAdd}
      footerNote="始终可用 · 个人技能不可单独开关"
      {...extra}
    />,
  );
  return { onRemove, onAdd };
}

describe("GrantedChips", () => {
  it("renders chips with labels and counts", () => {
    setup();
    expect(screen.getByText("superpowers")).toBeInTheDocument();
    expect(screen.getByText("14")).toBeInTheDocument();
    expect(screen.getByText("2 已启用")).toBeInTheDocument();
    expect(
      screen.getByText("始终可用 · 个人技能不可单独开关"),
    ).toBeInTheDocument();
  });

  it("calls onRemove with chip id", async () => {
    const user = userEvent.setup();
    const { onRemove } = setup();
    await user.click(screen.getByRole("button", { name: "移除 superpowers" }));
    expect(onRemove).toHaveBeenCalledWith("sp@m");
  });

  it("calls onAdd", async () => {
    const user = userEvent.setup();
    const { onAdd } = setup();
    await user.click(screen.getByRole("button", { name: "添加技能" }));
    expect(onAdd).toHaveBeenCalled();
  });

  it("renders an empty hint when there are no chips", () => {
    setup({ chips: [], emptyLabel: "未授予技能包" });
    expect(screen.getByText("未授予技能包")).toBeInTheDocument();
  });

  it("inherited chips have no remove button; off chips are struck through", () => {
    render(
      <GrantedChips
        title="技能"
        chipIcon={Boxes}
        chips={[
          { id: "i@m", label: "inherited-pack", tone: "inherit", locked: true },
          { id: "off@m", label: "off-pack", tone: "off" },
        ]}
        addLabel="管理技能"
        removeLabel={(name) => `移除 ${name}`}
        onRemove={() => {}}
        onAdd={() => {}}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "移除 inherited-pack" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "移除 off-pack" }),
    ).toBeInTheDocument();
  });
});
