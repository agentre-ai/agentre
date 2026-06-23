import { test, expect } from "@playwright/test";
import {
  runningSessionCount,
  groupCountByTitle,
  groupMemberCountByTitle,
  groupMessageCountByTitleAndContent,
} from "../fixtures/db";

// group_create 全链路(spec §7):用户在单聊里发 e2e-group-create 指令 → fake 经注入的
// group MCP server 调 group_create → svc 弹审批卡挂起 → 用户点「批准」 → 建群(host=发起
// agent + 按名解析成员)+ system「自会话拉起」消息 + brief 投主持人触发其群内首轮 →
// 主持人(fake)回复经 group_send 冒泡成可见群气泡。与 group-chat(手动建群)/group-task
// (任务卡)互补,覆盖"agent 自会话拉起团队"的入口。
// 依赖 e2e 种子(e2e/fakes/install.go):「CEO 助手」(系统 agent,host)+「E2E Member」(成员)。
test("agent self-initiated group creation chain", async ({ page }) => {
  // 群标题带时间戳唯一化:DB 跨用例共享 + 串行执行,按 title 锁定本用例建的群,
  // 与执行顺序/既有群无关(增量断言纪律同 group-task.spec)。
  const TITLE = `e2e拉起群-${Date.now()}`;
  const groupsBefore = groupCountByTitle(TITLE);

  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. 打开 CEO 助手 单聊(种子有两个 agent,按名锁定,不能用 first())。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page
    .locator('[data-testid^="agent-picker-item-"]', { hasText: "CEO 助手" })
    .click();

  // 2. 会话已打开:激活 tab + composer 就绪(单聊用 .ProseMirror,非 textarea)。
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();

  // 3. 发建群指令 → fake 调 group_create → 审批卡挂起出现。
  const main = page.getByRole("main");
  await editor.click();
  await editor.pressSequentially(
    `e2e-group-create:${TITLE}:E2E Member:e2e-brief 建群冒烟`,
  );
  await main.locator('button[type="submit"]').click();

  // 4. 审批卡出现(工具 label = toolApproval.tools.group_create,locale 双语兜底,
  //    与既有 spec 的 /Create group|创建群聊/ 写法同口径),点「批准」放行挂起的 MCP 调用。
  //    testid 随 PR2 审批管线泛化由 org-approval-card 改名 tool-approval-card。
  const approvalCard = main.getByTestId("tool-approval-card");
  await expect(approvalCard).toBeVisible({ timeout: 20_000 });
  await expect(
    approvalCard.getByText(/Create group chat|创建群聊/),
  ).toBeVisible();
  await approvalCard.getByRole("button", { name: /^(Approve|批准)$/ }).click();

  // 5. 批准落地:侧栏群列表自动 reload,新群 TITLE 出现(审批卡 approved 副作用,
  //    fake 不产 tool block,这是用户能看到新群的唯一入口)。
  //    页面有多个 complementary(导航栏/会话侧栏/上下文栏),按 aria-label 锁定会话侧栏。
  const sidebar = page.getByRole("complementary", {
    name: /Agent list|Agent 列表/,
  });
  const groupRow = sidebar.getByText(TITLE, { exact: true });
  await expect(groupRow).toBeVisible({ timeout: 20_000 });

  // 6. DB 孪生断言(权威来源,独立于 UI):
  //    a. 真落了一个新群(按唯一 title 锁定)。
  await expect
    .poll(() => groupCountByTitle(TITLE), { timeout: 20_000 })
    .toBe(groupsBefore + 1);
  //    b. 成员名真被解析成 membership:CEO 助手(host)+ E2E Member = 2 个 active 成员。
  await expect
    .poll(() => groupMemberCountByTitle(TITLE), { timeout: 20_000 })
    .toBe(2);
  //    c. system「自会话拉起」消息恰好一条(本群由 <发起者> 自会话拉起)。
  await expect
    .poll(() => groupMessageCountByTitleAndContent(TITLE, "自会话拉起"), {
      timeout: 20_000,
    })
    .toBe(1);
  //    d. brief 真作为首条群消息投递(主持人回显也含 brief,故 >= 1)。
  await expect
    .poll(() => groupMessageCountByTitleAndContent(TITLE, "e2e-brief 建群冒烟"), {
      timeout: 20_000,
    })
    .toBeGreaterThanOrEqual(1);

  // 7. 点开新群:主持人对 brief 的首轮回复经真实 group_send round-trip 冒泡成可见群气泡
  //    (mentions=["用户"] → 本轮自然收敛,不触发 agent 互投)。
  await groupRow.click();
  await expect(main.getByTestId("group-scroll")).toContainText("e2e-fake-reply:", {
    timeout: 20_000,
  });

  // 8. turn 收尾:发起单聊轮 + 主持人首轮全部结束,没有会话卡 running(守状态写丢失老坑)。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
