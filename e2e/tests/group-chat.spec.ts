import { test, expect } from "@playwright/test";
import {
  runningSessionCount,
  groupUserMessageCount,
  fakeAssistantMessageCount,
  agentGroupMessageCount,
} from "../fixtures/db";

// 核心群聊链路:打开 app → 建群 → 往群里发消息 → 主持人真跑一轮并把回复冒泡进群 → 全部回 idle。
// 与 smoke-chat 的单聊链路互补。群聊创建入口靠 fake 的 CapMCPTools 放行(否则没有可选主持人);
// 这一轮走真实的 group scheduler → gateway.Send → dispatcher / chat_svc / runtime / DB,
// 只有 agent backend 被确定性 fake 顶替。
//
// 发言进群严格依赖 `group_send` MCP 工具(group_svc 不解析回复文本/不落消息)。fake 像真 CLI 一样
// 充当 MCP 客户端:成员 turn 被注入 group MCP server 时,它对 gateway /mcp/group/ 调一次 group_send
// (body=回显文本,mentions=["用户"]),IngestAgentMessage 据此落一条 sender_kind='agent' 的群消息。
// 所以本 spec 一路断言到"主持人回复冒泡成可见群气泡"(可见层 + DB 孪生),覆盖完整群聊对话流。
test("core group chat chain", async ({ page }) => {
  await page.goto("/");

  // 1. app 加载完成:新建按钮可见。
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 2. 建群:新建菜单 → 新建群聊 → 填标题 → 选主持人(唯一 eligible = e2e 种子 agent)。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-group-item").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.locator("input").first().fill("e2e 群聊");
  await dialog.getByRole("combobox").first().click();
  const host = page.getByRole("option").first();
  await expect(host).toBeVisible();
  await host.click();
  await dialog.getByRole("button", { name: /Create group|创建群聊/ }).click();

  // 3. 群 tab 打开 + 群转录区与输入框就绪(群聊用 <textarea>,不是 .ProseMirror)。
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
  const main = page.getByRole("main");
  const composer = main.locator("textarea");
  await expect(composer).toBeVisible();

  // 发送前的基线(DB 跨用例共享 + 串行执行,用增量断言,与用例执行顺序无关)。
  const turnsBefore = fakeAssistantMessageCount();
  const groupPostsBefore = groupUserMessageCount();
  const agentPostsBefore = agentGroupMessageCount();

  // 4. 往群里发一条普通消息 → 投递给主持人并触发其一轮。
  await composer.fill("group ping");
  await main.getByRole("button", { name: /^(Send|发送)$/ }).click();

  // 5. 用户原话进入群转录区(可见层)。
  await expect(main.getByTestId("group-scroll")).toContainText("group ping");

  // 6. 主持人回复冒泡成可见群气泡:群转录区出现 fake 回复前缀。
  //    "group ping" 单独看会与用户气泡重名,故用 fake 前缀唯一锁定主持人那条。
  await expect(main.getByTestId("group-scroll")).toContainText("e2e-fake-reply:", {
    timeout: 20_000,
  });

  // 7. DB 孪生断言(权威来源,独立于 UI):
  //    a. 用户这条真落库到 group_messages(守"UI 说发出去了但 DB 没写")。
  expect(groupUserMessageCount()).toBeGreaterThan(groupPostsBefore);
  //    b. 主持人 backing session 真跑了一轮:多出一条 fake 回复 assistant 消息
  //       —— 证明群调度把这条投递成功拉起了一次完整 turn。
  await expect
    .poll(() => fakeAssistantMessageCount(), { timeout: 20_000 })
    .toBeGreaterThan(turnsBefore);
  //    c. 主持人回复经真实 group_send round-trip 落了一条 sender_kind='agent' 群消息
  //       —— 证明 fake 充当 MCP 客户端 → gateway /mcp/group/ → IngestAgentMessage 全链路打通。
  await expect
    .poll(() => agentGroupMessageCount(), { timeout: 20_000 })
    .toBeGreaterThan(agentPostsBefore);

  // 8. turn 收尾:没有会话卡在 running —— 守"卡 running / 状态写丢失"老坑。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
