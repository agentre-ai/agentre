import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { TabTooltip } from "../tab-tooltip";

// Radix Tooltip 在 jsdom 中需要关闭 pointerEvents 检查;tests 把 delayDuration
// 置 0 以便 hover 即开,断言用 waitFor 等 Radix 内部状态切换。
// Radix Tooltip 会同时渲染可见 tooltip + 一份 aria 隐藏的副本(供 screen reader),
// 所以用 *AllBy* 查询断言数量,而不是 *By*。
function setupUser() {
  return userEvent.setup({ pointerEventsCheck: 0 });
}

describe("TabTooltip", () => {
  it("hover 后弹出, 显示标题与 breadcrumb", async () => {
    // 关闭(unhover)行为是 Radix 内部状态机的职责,不在此处断言 —
    // radix-ui Portal + jsdom 下 pointerleave 触发不稳定,跑通就行。
    const user = setupUser();
    render(
      <TabTooltip
        title="arch-review · jwt 模型"
        projectChain={["Agentre", "backend"]}
        projectColor="var(--agent-2)"
        status="running"
        sessionId={12}
        worktreeBranch="feat/jwt-rs256"
        keyboardIndex={3}
        delayDuration={0}
      >
        <button type="button">tab</button>
      </TabTooltip>,
    );
    const trigger = screen.getByRole("button", { name: "tab" });
    expect(screen.queryAllByText("Agentre / backend")).toHaveLength(0);
    await user.hover(trigger);
    await waitFor(() => {
      expect(screen.queryAllByText("Agentre / backend").length).toBeGreaterThan(
        0,
      );
    });
    expect(
      screen.queryAllByText("arch-review · jwt 模型").length,
    ).toBeGreaterThan(0);
  });

  it("projectColor 给定时 Folder 图标着色", async () => {
    const user = setupUser();
    render(
      <TabTooltip
        title="arch-review · jwt"
        projectChain={["Agentre"]}
        projectColor="rgb(91, 141, 239)"
        status="idle"
        sessionId={12}
        worktreeBranch={null}
        keyboardIndex={null}
        delayDuration={0}
      >
        <button type="button">tab</button>
      </TabTooltip>,
    );
    await user.hover(screen.getByRole("button", { name: "tab" }));
    await waitFor(() => {
      expect(
        screen.queryAllByTestId("tooltip-folder-icon").length,
      ).toBeGreaterThan(0);
    });
    const folders = screen.getAllByTestId("tooltip-folder-icon");
    expect(folders[0].getAttribute("style") ?? "").toContain(
      "rgb(91, 141, 239)",
    );
  });

  it("无 projectChain 时不渲染 breadcrumb 行", async () => {
    const user = setupUser();
    render(
      <TabTooltip
        title="CEO 助手 · 周报"
        projectChain={null}
        projectColor={null}
        status="idle"
        sessionId={5}
        worktreeBranch={null}
        keyboardIndex={1}
        delayDuration={0}
      >
        <button type="button">tab</button>
      </TabTooltip>,
    );
    await user.hover(screen.getByRole("button", { name: "tab" }));
    await waitFor(() => {
      expect(screen.queryAllByText("CEO 助手 · 周报").length).toBeGreaterThan(
        0,
      );
    });
    expect(screen.queryAllByTestId("tooltip-folder-icon")).toHaveLength(0);
  });
});
