import { test, expect } from "@playwright/test";
import {
  runningSessionCount,
  groupTaskCount,
  fakeDeliveredTaskCount,
} from "../fixtures/db";

// 任务卡编排全链路(spec §9):用户 @主持人发 e2e-task 指令 → 主持人(fake)调
// group_task_create 建卡派活给成员 → 成员轮收到派活抬头(fake)调 group_task_complete
// 交付 → completed 投回主持人 → 全部收敛。覆盖 group_tasks 落库、状态翻转、
// 任务卡气泡渲染;与 group-chat.spec 的纯消息链路互补。
// 依赖 e2e 种子的第二个 agent「E2E Member」(e2e/fakes/install.go)——没有它群里无人可派。
test("group task orchestration chain", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. 建群:主持人按名字选 CEO 助手(种子有两个 agent,不能再用 first()),
  //    再把 E2E Member 加进初始成员。
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-group-item").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.locator("input").first().fill("e2e 任务群");
  await dialog.getByRole("combobox").first().click();
  await page.getByRole("option", { name: "CEO 助手" }).click();
  await dialog.getByRole("button", { name: /Add member|添加成员/ }).click();
  await page.getByRole("button", { name: "E2E Member" }).click();
  await dialog.getByRole("button", { name: /Create group|创建群聊/ }).click();

  // 2. 群转录区与输入框就绪。
  const main = page.getByRole("main");
  const composer = main.locator("textarea");
  await expect(composer).toBeVisible();

  // 发送前基线(DB 跨用例共享 + 串行执行 → 增量断言)。
  const tasksBefore = groupTaskCount();
  const openBefore = groupTaskCount("open");
  const deliveredBefore = fakeDeliveredTaskCount();

  // 3. 用户发任务指令 → 投给主持人,fake 据此建卡派活。
  await composer.fill("e2e-task:E2E Member:重构 UI");
  await main.getByRole("button", { name: /^(Send|发送)$/ }).click();

  // 4. 可见层:派活卡 + 交付卡两张任务卡气泡先后出现在群转录区。
  await expect(main.getByTestId("group-task-card").first()).toBeVisible({
    timeout: 20_000,
  });
  await expect(main.getByTestId("group-task-card")).toHaveCount(2, {
    timeout: 20_000,
  });
  // 卡头部 mono 编号可见(#N,具体编号取决于累计建卡数,断言形态不绑死值)。
  await expect(
    main.getByTestId("group-task-card").first().getByText(/#\d+/).first(),
  ).toBeVisible();

  // 5. DB 孪生断言(权威来源,独立于 UI):
  //    a. 真落了一张新卡。
  await expect
    .poll(() => groupTaskCount(), { timeout: 20_000 })
    .toBeGreaterThan(tasksBefore);
  //    b. 成员真经 group_task_complete 交付:status=done 且 result 带 fake 前缀
  //       —— 一并证明 result 软验收门(必填)在真实链路上被满足。
  await expect
    .poll(() => fakeDeliveredTaskCount(), { timeout: 20_000 })
    .toBeGreaterThan(deliveredBefore);
  //    c. 无 open 残留:本用例新建的卡全部离开派活态,链路收敛
  //       (增量口径:整库 open 数回到本用例开始前的水平)。
  await expect
    .poll(() => groupTaskCount("open"), { timeout: 20_000 })
    .toBe(openBefore);

  // 6. turn 收尾:没有会话卡 running(守状态写丢失老坑)。
  await expect.poll(() => runningSessionCount(), { timeout: 20_000 }).toBe(0);
});
