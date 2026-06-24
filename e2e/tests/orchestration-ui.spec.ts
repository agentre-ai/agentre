import { test, expect } from "@playwright/test";
import {
  orchestrationRunStatus,
  orchTaskRows,
  runningSessionCount,
} from "../fixtures/db";

// 编排 UI 创建链路(Task 16 e2e):通过界面新建 Run → 填写目标 + 选择 Leader →
// 提交 → 编排引擎完整走完 dispatch→finish 链路 → 结构图显示完成态。
//
// 与 orchestration.spec.ts 的区别:
//   - orchestration.spec.ts 直接调 Wails IPC(RunCreate)绕过 UI;
//   - 本 spec 通过真实 UI 交互(点击按钮、填表单、选 Leader)创建 Run,
//     验证 UI → IPC → 引擎的完整端到端链路。
//
// 假 runtime 链路与引擎 spec 相同:
//   goal 含「e2e-orch-dispatch:E2E Member:<brief>」→ fake 在 leader 轮里调 dispatch
//   → 子 agent 跑完 → reportToParent 续轮 → fake 调 finish → Run 推进到 done。
//
// 权威断言:DB 层 orchestration_runs.status='done' + orch_tasks 全 done + 子任务有 parentTaskId。
test("orchestration UI: 通过界面创建 Run 并等待结构图完成", async ({
  page,
}) => {
  const BRIEF = `e2e-ui子任务-${Date.now()}`;

  // 1. 打开 app:等待新建按钮可见,证明 app 完整加载。
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 2. 找到编排区域新建按钮并点击。
  //    - 空列表时显示 run-onboarding-cta(内含新建按钮);
  //    - 非空时显示 run-new-button。
  //    新 DB 下测试总是先走 onboarding 路径。
  const ctaContainer = page.getByTestId("run-onboarding-cta");
  const newButton = page.getByTestId("run-new-button");

  // 等待任意一个出现(onboarding CTA 或列表新建按钮)
  await expect(ctaContainer.or(newButton)).toBeVisible({ timeout: 10_000 });

  if (await ctaContainer.isVisible()) {
    // 空列表:点击 CTA 内的新建按钮(唯一 button)
    await ctaContainer.getByRole("button").click();
  } else {
    // 列表非空:点击 run-new-button
    await newButton.click();
  }

  // 3. 等待对话框出现(run-goal 输入框可见即表示弹窗已打开)。
  const goalInput = page.getByTestId("run-goal");
  await expect(goalInput).toBeVisible({ timeout: 5_000 });

  // 4. 填写目标:使用 fake runtime 识别的 e2e-orch-dispatch 指令前缀。
  //    格式与 orchestration.spec.ts 完全一致:
  //    「e2e-orch-dispatch:E2E Member:<brief>」
  const goal = `e2e-orch-dispatch:E2E Member:${BRIEF}`;
  await goalInput.fill(goal);

  // 5. 选择 Leader:点击 Select 触发器 → 在弹出列表中选择「CEO 助手」。
  //    shadcn Select 在真实浏览器环境下 option 渲染为 role="option"。
  await page.getByTestId("run-leader").click();
  await page.getByRole("option", { name: "CEO 助手" }).click();

  // 6. 点击「新建」提交按钮创建 Run。
  await page.getByTestId("run-create").click();

  // 7. 断言 Run 标签页已打开(orchestration-run 根元素可见)。
  await expect(page.getByTestId("orchestration-run")).toBeVisible({
    timeout: 10_000,
  });

  // 8. 等待 DB 权威来源:orchestration_runs.status 变为 'done'(超时 30s)。
  //    链路:dispatch → 子 agent 轮 → reportToParent 续轮 → finish → done。
  await expect
    .poll(() => orchestrationRunStatus(), { timeout: 30_000 })
    .toBe("done");

  // 9. 断言结构图显示完成态横幅(graph-completed-banner 可见)。
  //    结构图是 Run 标签页的默认视图,run.status=done 时渲染 graph-completed-banner。
  await expect(page.getByTestId("graph-completed-banner")).toBeVisible({
    timeout: 5_000,
  });

  // 10. DB 孪生断言:orch_tasks ≥2 行(根任务 + 至少一个子任务),全部 done,
  //     至少有一行 parentTaskId != 0(dispatch 出来的子任务有父引用)。
  const tasks = orchTaskRows();
  expect(tasks.length).toBeGreaterThanOrEqual(2);
  for (const t of tasks) {
    expect(t.status).toBe("done");
  }
  expect(tasks.some((t) => t.parentTaskId !== 0)).toBe(true);

  // 11. 全部会话收尾:无 session 卡 running(守状态写丢失老坑)。
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
});
