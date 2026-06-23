import { test, expect } from "@playwright/test";
import { runningSessionCount, fakeAssistantMessageCount } from "../fixtures/db";

// 核心持久化链路:新建会话 → 发消息 → 看到回复 → 重载页面 → tab 与转录从真 DB 复原 → 仍 idle。
// 守"会话/对话能不能熬过一次重启":首发把临时 "new" tab resolve 成真实 session(落库 + 可持久化),
// 重载后 app 从 localStorage 复原打开的 tab,再从后端拉回历史消息重新渲染。
test("session + transcript survive a page reload", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. 新建 agent 会话并发首条消息。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page.locator('[data-testid^="agent-picker-item-"]').first().click();
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();
  await editor.click();
  await editor.pressSequentially("persist me");
  await page.getByRole("main").locator('button[type="submit"]').click();

  // 2. 首轮跑完:fake 回复可见 + 没有会话卡 running(此刻用户消息与回复都已落库)。
  await expect(page.getByText(/e2e-fake-reply: persist me/)).toBeVisible();
  await expect(page.getByTestId("tab-spinner")).toHaveCount(0);
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
  expect(fakeAssistantMessageCount()).toBeGreaterThanOrEqual(1);
  await page.waitForFunction(() => {
    const raw = localStorage.getItem("agentre.chatTabs");
    if (!raw) return false;
    try {
      const state = JSON.parse(raw) as {
        activeTabId?: string | null;
        tabs?: Array<{ id: string; meta?: { kind?: string } }>;
      };
      return state.tabs?.some(
        (tab) =>
          tab.id === state.activeTabId && tab.meta?.kind === "session",
      );
    } catch {
      return false;
    }
  });

  // 3. 重载页面 —— 真实的"重开 app"近似:清空内存态,只剩 localStorage + DB。
  await page.reload();

  // 4. 复原后:激活 tab 仍在,用户原话与 fake 回复都从 DB 重新渲染出来(不是内存里的残留)。
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  await expect(page.getByText(/e2e-fake-reply: persist me/)).toBeVisible();

  // 5. 复原态依然 idle:复原不应把历史会话错标成 running。
  await expect(page.getByTestId("tab-spinner")).toHaveCount(0);
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
});
