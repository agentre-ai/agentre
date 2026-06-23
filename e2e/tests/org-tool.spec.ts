import { test, expect } from "@playwright/test";
import { runningSessionCount, departmentCountByName } from "../fixtures/db";

// 组织架构（org）写工具 + 审批全链路（agent 工具体系 e2e 补缺）：CEO（per-agent
// 门控开了 org 工具）单聊里发 e2e-org-create-dept → fake 经注入的 /mcp/org/ 调
// org_create_department（写工具需审批）→ 审批卡挂起 → 点批准 → departments 表
// 真出现该部门。覆盖「org 工具按 per-agent 门控注入（CEO 开启）+ 服务端审批 + 落库」。
// 反向门控（未开启的 agent 调用被 403 拒）由 orgtool_svc 单测覆盖，非 UI 可观测。
// 依赖 e2e 种子（e2e/fakes/install.go）：CEO 开了 org 工具。
test("agent creates a department via the approved org write tool", async ({
  page,
}) => {
  const DEPT = `e2e部门-${Date.now()}`;
  const before = departmentCountByName(DEPT);

  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 打开 CEO 助手 单聊（种子有多个 agent，按名锁定）。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page
    .locator('[data-testid^="agent-picker-item-"]', { hasText: "CEO 助手" })
    .click();
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();
  const main = page.getByRole("main");

  await editor.click();
  await editor.pressSequentially(`e2e-org-create-dept:${DEPT}`);
  await main.locator('button[type="submit"]').click();

  // org 写工具审批卡出现（通用 tool-approval-card）→ 点批准。
  const card = main.getByTestId("tool-approval-card");
  await expect(card.first()).toBeVisible({ timeout: 20_000 });
  await main.getByRole("button", { name: /^(Approve|批准)$/ }).click();

  // 批准落地：departments 真出现该部门（权威 DB 孪生，baseline+1 钉死本用例）。
  await expect
    .poll(() => departmentCountByName(DEPT), { timeout: 20_000 })
    .toBe(before + 1);

  // 收尾：审批 MCP 调用返回 → fake Done，没有会话卡 running。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
