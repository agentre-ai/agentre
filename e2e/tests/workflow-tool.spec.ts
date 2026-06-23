import { test, expect } from "@playwright/test";
import {
  runningSessionCount,
  workflowByName,
  groupWorkflowByTitle,
  groupCountByTitle,
} from "../fixtures/db";

// 流程工具 + 拉群带流程全链路(spec Part B/C):CEO 单聊里
//  1. 发 e2e-workflow-create → fake 经注入的 /mcp/workflow/ 调 workflow_create →
//     workflowtool_svc 弹审批卡挂起 → 点「批准」 → 落 workflows 行(PR3 流程管理工具)。
//  2. 读回新流程 id,发 e2e-group-create:<...>:<workflowId> → fake 调 group_create 带
//     workflowId → 审批卡 → 批准 → 建群且 groups.workflow_id == 该流程(PR5 拉群带流程)。
// 与 group-create(不带流程)互补,覆盖「agent 自己建流程,再选 agent 拉群并绑上它」。
// 依赖 e2e 种子(e2e/fakes/install.go):CEO 开了 workflow 工具 +「E2E Member」成员。
test("agent creates a workflow then pulls a group bound to it", async ({
  page,
}) => {
  const WF_NAME = `e2e流程-${Date.now()}`;
  const GROUP_TITLE = `e2e流程群-${Date.now()}`;
  const groupsBefore = groupCountByTitle(GROUP_TITLE);

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

  // ── 步骤 1:建流程(workflow_create,需审批)──────────────────────────────
  await editor.click();
  await editor.pressSequentially(`e2e-workflow-create:${WF_NAME}`);
  await main.locator('button[type="submit"]').click();

  // 审批卡出现(label = toolApproval.tools.workflow_create,双语兜底),点批准。
  const wfCard = main.getByTestId("tool-approval-card");
  await expect(wfCard.first()).toBeVisible({ timeout: 20_000 });
  await expect(
    main.getByText(/Create workflow|新建流程/),
  ).toBeVisible();
  await main.getByRole("button", { name: /^(Approve|批准)$/ }).click();

  // 批准落地:workflows 表真出现该流程(权威 DB 孪生,独立于 UI)。
  await expect
    .poll(() => workflowByName(WF_NAME)?.id ?? 0, { timeout: 20_000 })
    .toBeGreaterThan(0);
  const wfRow = workflowByName(WF_NAME);
  expect(wfRow).not.toBeNull();
  const workflowID = wfRow!.id;

  // 第一轮收尾:turn 结束(审批 MCP 调用返回 → fake Done),没有会话卡 running。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);

  // ── 步骤 2:拉群带流程(group_create 带 workflowId,需审批)─────────────────
  await editor.click();
  await editor.pressSequentially(
    `e2e-group-create:${GROUP_TITLE}:E2E Member:e2e-brief 带流程:${workflowID}`,
  );
  await main.locator('button[type="submit"]').click();

  // 第二张审批卡(group_create);第一张已批准不再有可点的 Approve 按钮,故按钮唯一。
  await expect(
    main.getByText(/Create group chat|创建群聊/),
  ).toBeVisible({ timeout: 20_000 });
  await main.getByRole("button", { name: /^(Approve|批准)$/ }).click();

  // 群真落库(按唯一 title)。
  await expect
    .poll(() => groupCountByTitle(GROUP_TITLE), { timeout: 20_000 })
    .toBe(groupsBefore + 1);

  // 关键断言:新群的 workflow_id 正是步骤 1 建的流程 —— 拉群带流程闭环。
  await expect
    .poll(() => groupWorkflowByTitle(GROUP_TITLE), { timeout: 20_000 })
    .toBe(workflowID);

  // 全部收尾:发起单聊轮 + 主持人首轮结束,没有会话卡 running(守状态写丢失老坑)。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
