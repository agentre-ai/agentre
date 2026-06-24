import { test, expect } from "@playwright/test";
import { runningSessionCount, workflowByName } from "../fixtures/db";

// 流程工具全链路:CEO 单聊里发 e2e-workflow-create → fake 经注入的 /mcp/workflow/ 调
// workflow_create → workflowtool_svc 弹审批卡挂起 → 点「批准」 → 落 workflows 行。
// 依赖 e2e 种子(e2e/fakes/install.go):CEO 开了 workflow 工具。
test("agent creates a workflow via the workflow tool with approval", async ({
  page,
}) => {
  const WF_NAME = `e2e流程-${Date.now()}`;

  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 打开 CEO 助手 单聊(种子有多个 agent,按名锁定)。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page
    .locator('[data-testid^="agent-picker-item-"]', { hasText: "CEO 助手" })
    .click();
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();
  const main = page.getByRole("main");

  // ── 建流程(workflow_create,需审批)──────────────────────────────
  await editor.click();
  await editor.pressSequentially(`e2e-workflow-create:${WF_NAME}`);
  await main.locator('button[type="submit"]').click();

  // 审批卡出现(label = toolApproval.tools.workflow_create,双语兜底),点批准。
  const wfCard = main.getByTestId("tool-approval-card");
  await expect(wfCard.first()).toBeVisible({ timeout: 20_000 });
  await expect(main.getByText(/Create workflow|新建流程/)).toBeVisible();
  await main.getByRole("button", { name: /^(Approve|批准)$/ }).click();

  // 批准落地:workflows 表真出现该流程(权威 DB 孪生,独立于 UI)。
  await expect
    .poll(() => workflowByName(WF_NAME)?.id ?? 0, { timeout: 20_000 })
    .toBeGreaterThan(0);
  const wfRow = workflowByName(WF_NAME);
  expect(wfRow).not.toBeNull();

  // 收尾:turn 结束(审批 MCP 调用返回 → fake Done),没有会话卡 running。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
