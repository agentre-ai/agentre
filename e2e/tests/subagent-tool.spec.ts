import { test, expect } from "@playwright/test";
import { runningSessionCount, subagentSessionCount } from "../fixtures/db";

// 子 agent 调用工具全链路(agent 工具体系 e2e 补缺）：CEO 单聊里发
// e2e-subagent-call → fake 经注入的 /mcp/subagent/ 调 agent_call（无审批，同步
// 委派）→ subagent_svc 在一条隔离的一次性会话里跑子 agent（E2E Member）一轮 →
// 返回其文本。权威断言：DB 真出现一条 purpose='subagent_call' 的委派会话
// （独立于 UI；该会话被 nonSubagentScope 从侧栏隐藏）。
// 依赖 e2e 种子（e2e/fakes/install.go）：CEO 开了 subagent 工具 +「E2E Member」。
test("agent delegates a subtask to a sub-agent via agent_call", async ({
  page,
}) => {
  const PROMPT = `整理这条 e2e 子任务-${Date.now()}`;
  const before = subagentSessionCount();

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
  await editor.pressSequentially(`e2e-subagent-call:E2E Member:${PROMPT}`);
  await main.locator('button[type="submit"]').click();

  // agent_call 无审批，同步阻塞到子 agent 跑完：DB 真出现一条委派会话（baseline+1
  // 钉死本用例创建的那条，独立于种子/其它会话）。
  await expect
    .poll(() => subagentSessionCount(), { timeout: 20_000 })
    .toBe(before + 1);

  // 全部收尾：发起轮 + 子 agent 轮都结束，没有会话卡 running（守状态写丢失老坑）。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
