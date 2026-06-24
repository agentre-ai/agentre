import { test, expect } from "@playwright/test";
import {
  orchestrationRunStatus,
  orchTaskRows,
  runningSessionCount,
} from "../fixtures/db";

// 编排引擎最小链路(Task 18 e2e):RunCreate → leader fake 经注入的 /mcp/orchestrate/ 调
// dispatch 派发子任务(E2E Member) → 子 agent fake 跑完 → 完成回报续轮触发 leader
// 自动调 finish → Run 推进到 done。
//
// 链路说明:
//   1. RunCreate(goal=e2e-orch-dispatch:…) → orch_svc 在 leader(CEO 助手)会话里发消息
//   2. fake runtime 收到含「e2e-orch-dispatch:E2E Member:<brief>」的 UserText
//      → 经注入的 /mcp/orchestrate/ 调 dispatch(agent=E2E Member, brief=…)
//   3. orch_svc.dispatch 在独立会话里跑 E2E Member 一轮 → 子 Task 落 done
//   4. 子任务完成后 reportToParent 在 leader 会话里续轮(UserText=「【子任务 #1 完成…」)
//   5. fake runtime 检测到「【子任务」→ 自动调 finish(summary=e2e-orchestration-complete)
//   6. orch_svc.finish 把根 Task + Run 推进到 done
//
// 权威断言:DB 层 orchestration_runs.status='done' + orch_tasks 全 done + 子任务有 parentTaskId。
test("orchestration engine: dispatch sub-task then finish run", async ({
  page,
}) => {
  const BRIEF = `e2e子任务-${Date.now()}`;

  // 1. 打开 app:等待新建按钮可见,证明 app 完整加载。
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 2. 通过 Wails IPC 找到 CEO 助手(system_badge='DEFAULT')的 agent ID。
  //    ListChatAgents 返回 {agents: [{id, name, ...}]}。
  const leaderAgentId = await page.evaluate(async () => {
    const resp = await (
      window as unknown as {
        go: {
          app: {
            App: {
              ListChatAgents: () => Promise<{
                agents: Array<{ id: number; name: string }>;
              }>;
            };
          };
        };
      }
    ).go.app.App.ListChatAgents();
    const ceo = resp.agents.find((a) => a.name === "CEO 助手");
    return ceo?.id ?? 0;
  });
  expect(leaderAgentId).toBeGreaterThan(0);

  // 3. RunCreate:goal 含 e2e-orch-dispatch 指令 → fake 在 leader 轮里调 dispatch。
  //    leaderAgentId 从页面取回,通过 arg 传给 evaluate(避免序列化问题)。
  const runDetail = await page.evaluate(async (args) => {
    return await (
      window as unknown as {
        go: {
          app: {
            App: {
              RunCreate: (req: {
                goal: string;
                leaderAgentId: number;
                flowId: number;
                flowContent: string;
                projectId: number;
                allowedAgentIds: number[];
              }) => Promise<{
                run: { id: number; status: string };
                tasks: Array<{ id: number; status: string; parentTaskId: number }>;
              }>;
            };
          };
        };
      }
    ).go.app.App.RunCreate({
      goal: args.goal,
      leaderAgentId: args.leaderAgentId,
      flowId: 0,
      flowContent: "",
      projectId: 0,
      allowedAgentIds: [],
    });
  }, { goal: `e2e-orch-dispatch:E2E Member:${BRIEF}`, leaderAgentId });

  expect(runDetail.run.id).toBeGreaterThan(0);

  // 4. 等 DB 权威来源:orchestration_runs.status 变为 'done'(超时 30s)。
  //    链路:dispatch → 子 agent 轮 → reportToParent 续轮 → finish → done。
  await expect
    .poll(() => orchestrationRunStatus(), { timeout: 30_000 })
    .toBe("done");

  // 5. 断言 orch_tasks:≥2 行(根任务 + 至少一个 dispatch 子任务),全部 done,
  //    至少有一行 parentTaskId != 0(子任务有父引用)。
  const tasks = orchTaskRows();
  expect(tasks.length).toBeGreaterThanOrEqual(2);
  for (const t of tasks) {
    expect(t.status).toBe("done");
  }
  expect(tasks.some((t) => t.parentTaskId !== 0)).toBe(true);

  // 6. 全部会话收尾:无 session 卡 running(守状态写丢失老坑)。
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
});
