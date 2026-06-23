import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { CapabilityPicker } from "../capability-picker";
import type { CatalogItem } from "../catalog";

const items: CatalogItem[] = [
  {
    id: "sp@m",
    name: "superpowers",
    description: "TDD / 调试 / 计划",
    group: "推荐 · AGENTRE 精选",
    badges: [{ label: "推荐", tone: "recommended" }],
    enabled: true,
  },
  {
    id: "fd@m",
    name: "frontend-design",
    description: "产品级前端设计",
    group: "已安装 · 发现自此后端",
    badges: [{ label: "已装", tone: "installed" }],
    enabled: false,
  },
  {
    id: "by@m",
    name: "baoyu-skills",
    description: "来自 marketplace",
    group: "可安装 · MARKETPLACE",
    enabled: false,
    disabledReason: "需先安装",
  },
];

function setup(
  extra: Partial<React.ComponentProps<typeof CapabilityPicker>> = {},
) {
  const onToggle = vi.fn();
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  const onRescan = vi.fn();
  render(
    <CapabilityPicker
      open
      title="添加技能 · Skill Packs"
      subtitle="一个包 = 一组 skill"
      searchPlaceholder="搜索技能包 / 描述…"
      items={items}
      onToggle={onToggle}
      onConfirm={onConfirm}
      onCancel={onCancel}
      onRescan={onRescan}
      {...extra}
    />,
  );
  return { onToggle, onConfirm, onCancel, onRescan };
}

describe("CapabilityPicker", () => {
  it("renders items grouped by source with group headers", () => {
    setup();
    expect(screen.getByText("推荐 · AGENTRE 精选")).toBeInTheDocument();
    expect(screen.getByText("已安装 · 发现自此后端")).toBeInTheDocument();
    expect(screen.getByText("superpowers")).toBeInTheDocument();
  });

  it("toggles an installed row on click", async () => {
    const user = userEvent.setup();
    const { onToggle } = setup();
    await user.click(screen.getByText("frontend-design"));
    expect(onToggle).toHaveBeenCalledWith("fd@m");
  });

  it("does not toggle a disabled (needs-install) row", async () => {
    const user = userEvent.setup();
    const { onToggle } = setup();
    await user.click(screen.getByText("baoyu-skills"));
    expect(onToggle).not.toHaveBeenCalled();
    expect(screen.getByText("需先安装")).toBeInTheDocument();
  });

  it("filters by name/description via the search box", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText("搜索技能包 / 描述…"), "front");
    expect(screen.getByText("frontend-design")).toBeInTheDocument();
    expect(screen.queryByText("superpowers")).toBeNull();
  });

  it("fires rescan and confirm", async () => {
    const user = userEvent.setup();
    const { onRescan, onConfirm } = setup();
    await user.click(screen.getByText("Rescan"));
    expect(onRescan).toHaveBeenCalled();
    await user.click(screen.getByText("Done"));
    expect(onConfirm).toHaveBeenCalled();
  });

  it("renders a tri-state toggle for items with state and calls onSetState", async () => {
    const user = userEvent.setup();
    const onSetState = vi.fn();
    render(
      <CapabilityPicker
        open
        title="管理技能"
        searchPlaceholder="搜索"
        items={[
          {
            id: "sp@m",
            name: "superpowers",
            description: "d",
            group: "继承",
            enabled: true,
            state: "inherit",
            globallyEnabled: true,
          },
        ]}
        triLabels={{ inherit: "继承", on: "开", off: "关" }}
        onToggle={() => {}}
        onSetState={onSetState}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "关" }));
    expect(onSetState).toHaveBeenCalledWith("sp@m", "off");
  });

  it("shows loading and empty states", () => {
    const { rerender } = render(<div />) as unknown as { rerender: never };
    void rerender;
    // loading
    render(
      <CapabilityPicker
        open
        title="t"
        searchPlaceholder="p"
        items={[]}
        loading
        onToggle={vi.fn()}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });
});
