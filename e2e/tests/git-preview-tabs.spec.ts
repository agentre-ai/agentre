import { join } from "node:path";

import { test, expect } from "@playwright/test";

import { agentIdByName, e2eDataDir } from "../fixtures/db";
import { seedDirtyGitRepo } from "../fixtures/git-repo";

// 跨侧栏与预览面板的接线（spec 决策 18）：切到 Git 顶层 tab 看到变动行 → 单击一行
// 开出临时标签 → 单击第二行原地替换那个临时标签 → 双击第三行转常驻使标签数达到
// 2 → 标签条出现且可切换 → 关闭后面板收起。这条链路跨越 chat-sidebar-store /
// ChatContextSidebar / FilePreviewPanel 三处，各自的单测都 mock 掉另外两边，
// 结构上覆盖不到它们的接合处（尤其是 SidebarRow 的 onClick / onDoubleClick 在真实
// 浏览器里的事件序列——见 frontend/src/components/agentre/chat-context-sidebar/
// views/sidebar-row.tsx 与 frontend/src/stores/chat-sidebar-store.ts 的
// PreviewClickOutcome / restoreClobberedPreviewTab）。
//
// Git 页要有真实变动行：种子会话的 cwd 落在
// <AGENTRE_DATA_DIR>/agents/<agentID>/（project_svc 对自由会话 ProjectID=0 的
// 回落，internal/service/project_svc/cwd.go），这里用 git CLI 把它变成一个带未
// 提交变动的真实仓库（e2e/fixtures/git-repo.ts），agentID 通过 node:sqlite 直读
// 种子里的 CEO 助手 agent 拿到（e2e/fixtures/db.ts 的 agentIdByName），不硬编码。
const CEO_NAME = "CEO 助手";
const FILES = ["e2e-note-1.txt", "e2e-note-2.txt", "e2e-note-3.txt"] as const;

test("Git tab rows wire into the preview panel's temp/permanent tab strip", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 种子仓库要在会话开始读取 cwd 之前就绪：CEO 助手的 agent id 一确定就落盘，
  // 独立于 Go 后端的文件系统调用（AgentCwd 的 os.MkdirAll 在已存在的目录上是
  // no-op，不会覆盖这里种下的仓库）。
  const ceoId = agentIdByName(CEO_NAME);
  expect(ceoId).not.toBeNull();
  const repoDir = join(e2eDataDir(), "agents", String(ceoId));
  seedDirtyGitRepo(repoDir, FILES);

  // 打开 CEO 助手 单聊并发一条消息：会话（和它解析出的 cwd）只在首轮发出后才
  // 真正落库，仅仅选中 agent 还停在"开始对话"的占位态，没有真实 chat_sessions
  // 行（也就没有 cwd）。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page
    .locator('[data-testid^="agent-picker-item-"]', { hasText: CEO_NAME })
    .click();
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();
  await editor.click();
  await editor.pressSequentially("spin up preview tabs");
  await page.getByRole("main").locator('button[type="submit"]').click();
  await expect(
    page.getByText(/e2e-fake-reply: spin up preview tabs/),
  ).toBeVisible();

  // (a) 切到 Git 顶层 tab，看到种下的三条变动行。用 "Chat context" 这个
  // complementary landmark 限定作用域：聊天会话自己的 tab 条也有一个
  // role="tab" 且可能因为消息文案巧合命中同名的 accessible-name 子串匹配。
  const sidebar = page.getByRole("complementary", { name: "Chat context" });
  await sidebar.getByRole("tab", { name: "Git" }).click();

  const row = (name: string) =>
    page.locator(`[data-testid="git-row"][data-path="${name}"]`);
  await expect(row(FILES[0])).toBeVisible();
  await expect(row(FILES[1])).toBeVisible();
  await expect(row(FILES[2])).toBeVisible();

  const previewHeader = page.getByTestId("file-preview-header");
  const tabStrip = page.locator(
    '[role="tablist"][aria-label="Preview tabs"]',
  );

  // (b) 单击第一行开出临时标签；单击第二行原地替换它——标签数全程停在 1，
  // 标签条（≥2 才渲染）都不出现。
  await row(FILES[0]).click();
  await expect(previewHeader).toContainText(FILES[0]);
  await expect(tabStrip).toHaveCount(0);

  await row(FILES[1]).click();
  await expect(previewHeader).toContainText(FILES[1]);
  await expect(tabStrip).toHaveCount(0);

  // (c) 双击第三行：它转常驻，而第二行的临时标签被保留（不是被这次双击的第一次
  // click 原地吞掉）——标签数达到 2，标签条出现。
  await row(FILES[2]).dblclick();
  await expect(previewHeader).toContainText(FILES[2]);
  await expect(tabStrip).toBeVisible();

  const tab = (name: string) =>
    tabStrip.locator(`[role="tab"][data-tab-path="${name}"]`);
  await expect(tab(FILES[1])).toHaveAttribute("data-preview", "true");
  await expect(tab(FILES[2])).toHaveAttribute("data-preview", "false");
  await expect(tab(FILES[2])).toHaveAttribute("data-active", "true");

  // (d) 标签条可切换：点第二行那个（仍然临时的）标签，预览面板与 data-active
  // 都跟着切过去。
  await tab(FILES[1]).click();
  await expect(previewHeader).toContainText(FILES[1]);
  await expect(tab(FILES[1])).toHaveAttribute("data-active", "true");
  await expect(tab(FILES[2])).toHaveAttribute("data-active", "false");

  // (e) 关闭后面板收起：先用标签自己的关闭按钮关掉一个（标签数掉到 1，标签条
  // 随之消失），再用预览面板 header 的关闭按钮关掉最后一个——面板整个收起。
  await tab(FILES[2])
    .getByRole("button", { name: "Close Tab" })
    .click();
  await expect(tabStrip).toHaveCount(0);
  await expect(previewHeader).toContainText(FILES[1]);

  await previewHeader.getByRole("button", { name: "Close preview" }).click();
  await expect(previewHeader).toBeHidden();
});
