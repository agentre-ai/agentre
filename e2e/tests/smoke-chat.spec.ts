import { test, expect } from "@playwright/test";
import { runningSessionCount } from "../fixtures/db";

// 核心冒烟链路:打开 app → 新建 agent 会话 → 发消息 → 看到 fake 流式回复 → tab 回到 idle。
// 后端用 -tags e2e 的 fake runtime,回复固定前缀 "e2e-fake-reply: "。
test("core chat smoke chain", async ({ page }) => {
  await page.goto("/");

  // 1. app 加载完成:新建按钮可见。
  const newChat = page.getByTestId("new-chat-button");
  await expect(newChat).toBeVisible();

  // 2. 新建 agent 会话:打开下拉 → 选 "新建 agent 会话" → 选种子里的默认 agent。
  await newChat.click();
  await page.getByTestId("new-agent-chat-item").click();
  await page.locator('[data-testid^="agent-picker-item-"]').first().click();

  // 3. 会话已打开:激活 tab + 输入框就绪。
  // 注意 page 上有两个 textbox(侧栏搜索 input + composer),用 .ProseMirror 精确定位 composer。
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();

  // 4. 输入并发送(ProseMirror 需先聚焦;发送走 composer 的 submit 按钮,限定在 main 内避免歧义)。
  await editor.click();
  await editor.pressSequentially("ping");
  await page.getByRole("main").locator('button[type="submit"]').click();

  // 5. fake 回复出现在转录区(固定前缀,确定性)。
  await expect(page.getByText(/e2e-fake-reply: ping/)).toBeVisible();

  // 6. turn 收尾:tab 不再 running(spinner 消失) —— 守"卡 running"老坑。
  await expect(page.getByTestId("tab-spinner")).toHaveCount(0);

  // 7. DB 层孪生断言:权威来源 chat_sessions.agent_status 没有会话卡在 "running"
  //    —— 直接守"卡 running / 状态写丢失"老坑(不只看 UI spinner)。
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
});
