# 群任务卡编排 PR4:e2e seam + spec 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** fake runtime 学会经 group MCP 调 `group_task_create` / `group_task_complete`,新增 e2e spec 走「用户@主持人 → 建任务给成员 → 成员 complete → 主持人收到」全链路,`node:sqlite` oracle 验 `group_tasks` 行。

**Architecture:** 三段接缝——①fake runtime(`internal/pkg/agentruntime/runtimes/fake`)按确定性文本模式驱动 task tool(用户指令 `e2e-task:<assignee>:<title>` → 建卡;收到派活抬头 `任务 #N：` → 交付);②e2e 种子(`e2e/fakes/install.go`)多 seed 一个成员 agent,群里才有人可派;③Playwright spec + DB fixture 断言可见任务卡 + `group_tasks` 真落库翻状态。复用既有 group_send 接缝的全部基础设施(HTTP tools/call、无状态 token、`AGENTRE_PROXY_PORT=0`)。

**Tech Stack:** Go(`-tags e2e` 构建的 fake runtime + testify 单测)、Playwright + TypeScript、`node:sqlite` 只读 oracle。

**Spec:** [2026-06-11-group-task-orchestration-design.md](../specs/2026-06-11-group-task-orchestration-design.md) §9 末段、§10 PR4。

---

## 背景事实(执行前先读,全部已核实)

- **fake runtime 现状**(`internal/pkg/agentruntime/runtimes/fake/runtime.go`,`//go:build e2e`):
  `Run` 流式回显 `e2e-fake-reply: ` + UserText;尾声若注入的 MCPServers 里有 server 广告
  `group_send` tool,就对 `spec.URL` 发一次 JSON-RPC `tools/call`(`postGroupSend`,
  带 `spec.Headers` 的 Authorization,无需 initialize 握手)。失败只写 stderr,不发 ErrorEvent。
- **MCP server 端**(`internal/service/group_svc/mcp.go`):`tools/call` 无状态;
  `group_task_create` 参数 `assignee`(显示名)/`title`/`brief`/`parentTaskId`;
  `group_task_complete` 参数 `taskId`(群内编号 #N)/`result`。**所有成员**的 spec.Tools
  都广告三个 task tool(`buildmcp_internal_test.go` 已锁)。
- **投递文本格式**(fake 解析的依据):成员轮 UserText = `(来自 <发送者名>)\n<content>`
  (`scheduler.go` launchDelivery)。建卡 content = `任务 #%d：%s\n%s`(全角冒号紧跟编号,
  `task.go` HandleTaskCreate);完成通知 = `任务 #%d 已完成\n%s`(无紧跟冒号,不会误匹配);
  取消 = `任务 #%d 已取消`。
- **链路收敛**:HandleTaskComplete 把 completed 投回 creator(主持人)→ 主持人末轮 UserText
  是 `任务 #N 已完成…`,不匹配任何 fake 模式 → 只 group_send(mentions=["用户"])→ 无 agent
  收件人,自然收敛。RoundCount 只是计数器,**没有轮数上限**,3 轮链路不会被掐断。
- **e2e 种子**(`e2e/fakes/install.go`):目前只把 fake backend 挂到系统 agent(名字
  **`CEO 助手`**,migration `202605220004_agents.go` seed)。建第二个 agent 用
  `agent_svc.Agent().Create`,placement 必须二选一:给 `ParentAgentID: ceo.ID`(挂 CEO
  汇报线;子 agent 在建群弹窗 eligible 池内,PR1 已与 invite 池同口径)。幂等用
  `agent_repo.Agent().FindByName`。
- **建群弹窗**(`group-new-dialog.tsx` + `agent-multi-picker.tsx`):主持人 = shadcn Select
  (`getByRole("combobox").first()` → `getByRole("option", …)`);初始成员 = 「添加成员/Add member」
  按钮 → Popover 里按名字点 button。i18n key:`group.new.addMember` = zh `添加成员` / en `Add member`
  (执行时用 `grep addMember frontend/src/i18n/locales/*/common.json` 复核确切文案)。
- **任务卡气泡**:`data-testid="group-task-card"`(`group-task-card.tsx`),状态 pill 文案走
  `group.task.status.*`。群转录区 `data-testid="group-scroll"`。
- **DB oracle**(`e2e/fixtures/db.ts`):`node:sqlite` 只读连 `$AGENTRE_DATA_DIR/agentre.db`。
  `group_tasks.status` 值 = `open`/`done`/`canceled`(`group_entity/task.go`)。
  DB 跨用例共享、串行执行 → **一律增量断言**(先取基线再比较)。
- **运行**:`make e2e`(全套)/ `cd e2e && pnpm test -- tests/group-task.spec.ts`(单 spec,
  经 run-e2e.mjs 包装,webServer 自起)。fake 的 Go 单测:
  `go test -tags e2e -race ./internal/pkg/agentruntime/runtimes/fake`。
  `AGENTRE_PROXY_PORT=0` 已写死在 `playwright.config.ts:67`,无需再处理。
- **风险预案**:seed 第二个 agent 后,既有 `group-chat.spec.ts` 选主持人用
  `getByRole("option").first()`——两个 eligible 时仍能跑(任一 fake 当主持人行为相同),
  但若全量 e2e 因顺序翻车,把它改成按名字选 `CEO 助手`(属本任务引入的破坏,在 scope 内修)。

---

### Task 1: fake runtime 任务接缝(TDD)

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/fake/runtime.go`
- Test: `internal/pkg/agentruntime/runtimes/fake/runtime_test.go`

- [x] **Step 1: 写失败测试**

在 `runtime_test.go` 末尾追加(沿用 `TestRun_PostsGroupSendWhenInjected` 的 httptest 手法,
但 server 按 tool 名分发、收多次调用):

```go
// taskCaptureServer 收集本轮 fake 发出的全部 tools/call,按 tool 名归档参数。
func taskCaptureServer(t *testing.T) (*httptest.Server, func() map[string][]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string][]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var rpc struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		require.NoError(t, json.Unmarshal(b, &rpc))
		mu.Lock()
		calls[rpc.Params.Name] = append(calls[rpc.Params.Name], rpc.Params.Arguments)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string][]map[string]any {
		mu.Lock()
		defer mu.Unlock()
		out := map[string][]map[string]any{}
		for k, v := range calls {
			out[k] = append([]map[string]any(nil), v...)
		}
		return out
	}
}

func taskToolsSpec(url string) []agentruntime.MCPServerSpec {
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     url + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer tok"},
		Tools:   []string{"group_send", "group_task_create", "group_task_complete", "group_task_cancel"},
	}}
}

// 主持人轮:用户指令 e2e-task:<assignee>:<title> → fake 调 group_task_create 派活
// (brief 为确定性派生文本),group_send 照常发(回显冒泡进群)。
func TestRun_PostsTaskCreateOnDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := taskCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID:  11,
		UserText:   "(来自 用户)\ne2e-task:E2E Member:重构 UI",
		MCPServers: taskToolsSpec(srv.URL),
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["group_task_create"], 1)
	args := calls["group_task_create"][0]
	assert.Equal(t, "E2E Member", args["assignee"])
	assert.Equal(t, "重构 UI", args["title"])
	assert.Equal(t, "e2e-brief: 重构 UI", args["brief"])
	assert.Len(t, calls["group_send"], 1)         // 既有回显行为不受影响
	assert.Empty(t, calls["group_task_complete"]) // 指令轮绝不交付
}

// 成员轮:收到派活抬头「任务 #N：」→ fake 调 group_task_complete 交付,
// result 带 TaskResultPrefix(DB oracle 据此断言)。
func TestRun_PostsTaskCompleteOnAssignedTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := taskCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID:  12,
		UserText:   "(来自 CEO 助手)\n任务 #3：重构 UI\ne2e-brief: 重构 UI",
		MCPServers: taskToolsSpec(srv.URL),
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["group_task_complete"], 1)
	args := calls["group_task_complete"][0]
	assert.Equal(t, float64(3), args["taskId"]) // JSON 数字解码为 float64
	result, _ := args["result"].(string)
	assert.True(t, strings.HasPrefix(result, TaskResultPrefix), "result=%q", result)
	assert.Empty(t, calls["group_task_create"])
}

// 无指令、无派活抬头(含「任务 #N 已完成」回执)→ 绝不碰 task tool,只 group_send。
// 守主持人收 completed 后的末轮自然收敛 + 普通群聊轮(group-chat.spec)行为不变。
func TestRun_SkipsTaskCallsWithoutPatterns(t *testing.T) {
	for _, userText := range []string{
		"(来自 用户)\ngroup ping",
		"(来自 E2E Member)\n任务 #3 已完成\ne2e-fake-result: task #3",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		srv, snapshot := taskCaptureServer(t)

		r := New()
		events, _, err := r.Run(ctx, agentruntime.RunRequest{
			SessionID:  13,
			UserText:   userText,
			MCPServers: taskToolsSpec(srv.URL),
		})
		require.NoError(t, err)
		for range events { //nolint:revive // draining
		}

		calls := snapshot()
		assert.Empty(t, calls["group_task_create"], "userText=%q", userText)
		assert.Empty(t, calls["group_task_complete"], "userText=%q", userText)
		assert.Len(t, calls["group_send"], 1, "userText=%q", userText)
		cancel()
	}
}
```

import 块补 `"strings"` 与 `"sync"`。

- [x] **Step 2: 跑测试确认失败**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -tags e2e -race ./internal/pkg/agentruntime/runtimes/fake -run 'TestRun_PostsTaskCreateOnDirective|TestRun_PostsTaskCompleteOnAssignedTask|TestRun_SkipsTaskCallsWithoutPatterns'
```

预期:编译错误 `undefined: TaskResultPrefix`(或实现后断言失败)——失败原因必须是缺实现,不是测试笔误。

- [x] **Step 3: 最小实现**

`runtime.go` 改动(其余不动):

```go
// 顶部常量区追加:
// TaskDirectivePrefix 触发建任务卡的用户指令:e2e-task:<assignee显示名>:<title>。
// e2e spec 用它驱动主持人轮确定性建卡(真实场景这是 LLM 的判断,fake 用文本模式顶替)。
const TaskDirectivePrefix = "e2e-task:"

// TaskResultPrefix 是 fake 交付任务时 result 的前缀,DB oracle 据此锁定 fake 写入的行。
const TaskResultPrefix = "e2e-fake-result: "

// taskAssignedRe 匹配派活消息抬头「任务 #N：」(HandleTaskCreate 的 content 格式;
// 完成回执是「任务 #N 已完成」、取消是「任务 #N 已取消」,编号后无全角冒号,不会误匹配)。
var taskAssignedRe = regexp.MustCompile(`任务 #(\d+)：`)
```

`Run` 的 goroutine 里,group_send 块之后、`Done` 之前追加:

```go
		// 任务接缝(spec §9):主持人收到 e2e-task 指令 → 建卡派活;成员收到派活抬头 → 交付。
		// 与 group_send 一样尽力而为:失败只写 stderr,缺卡/缺交付由 e2e spec 显式抓红。
		if spec, ok := findGroupToolServer(req.MCPServers, "group_task_create"); ok {
			if assignee, title, found := parseTaskDirective(req.UserText); found {
				if err := postToolCall(ctx, spec, "group_task_create", map[string]any{
					"assignee": assignee, "title": title, "brief": "e2e-brief: " + title,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_create failed: %v\n", err)
				}
			}
		}
		if spec, ok := findGroupToolServer(req.MCPServers, "group_task_complete"); ok {
			if m := taskAssignedRe.FindStringSubmatch(req.UserText); m != nil {
				no, _ := strconv.Atoi(m[1])
				if err := postToolCall(ctx, spec, "group_task_complete", map[string]any{
					"taskId": no, "result": TaskResultPrefix + "task #" + m[1],
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_complete failed: %v\n", err)
				}
			}
		}
```

既有 helper 泛化(同一提交内的就地重构,不是 drive-by:新调用点需要它):

```go
// findGroupToolServer 返回首个广告 tool 的注入 MCP server(无 → !ok)。
func findGroupToolServer(specs []agentruntime.MCPServerSpec, tool string) (agentruntime.MCPServerSpec, bool) {
	for _, s := range specs {
		if slices.Contains(s.Tools, tool) {
			return s, true
		}
	}
	return agentruntime.MCPServerSpec{}, false
}

// parseTaskDirective 从 UserText 中解出 e2e-task:<assignee>:<title>(取指令所在行,
// 缺段/空段 → !ok)。
func parseTaskDirective(text string) (assignee, title string, ok bool) {
	idx := strings.Index(text, TaskDirectivePrefix)
	if idx < 0 {
		return "", "", false
	}
	rest := text[idx+len(TaskDirectivePrefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	assignee, title, found := strings.Cut(rest, ":")
	assignee, title = strings.TrimSpace(assignee), strings.TrimSpace(title)
	if !found || assignee == "" || title == "" {
		return "", "", false
	}
	return assignee, title, true
}

// postToolCall 对注入的 group MCP server 发一次无状态 tools/call(原 postGroupSend 泛化)。
func postToolCall(ctx context.Context, spec agentruntime.MCPServerSpec, tool string, args map[string]any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range spec.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %d", tool, resp.StatusCode)
	}
	return nil
}
```

收尾:原 `findGroupSendServer` 删除,group_send 调用点改为
`findGroupToolServer(req.MCPServers, "group_send")` + `postToolCall(ctx, spec, "group_send",
map[string]any{"body": reply, "mentions": []string{"用户"}})`;原 `postGroupSend` 删除。
import 块补 `"regexp"`、`"slices"`、`"strings"`(`strconv` 已有)。

- [x] **Step 4: 跑测试确认通过(全包,守住既有 6 个用例)**

```bash
go test -tags e2e -race ./internal/pkg/agentruntime/runtimes/fake
```

预期:全部 PASS(含既有 `TestRun_PostsGroupSendWhenInjected` 等)。

- [x] **Step 5: 提交**

```bash
git add internal/pkg/agentruntime/runtimes/fake/runtime.go internal/pkg/agentruntime/runtimes/fake/runtime_test.go
git commit -m "✨ e2e: fake runtime 任务接缝(e2e-task 指令建卡 + 派活抬头自动交付)"
```

---

### Task 2: DB oracle + 任务链路 spec(先红)

**Files:**
- Modify: `e2e/fixtures/db.ts`
- Create: `e2e/tests/group-task.spec.ts`

- [x] **Step 1: db.ts 追加 group_tasks oracle**

`e2e/fixtures/db.ts` 末尾追加(沿用现有 helper 的连接/busy_timeout 模式):

```ts
// Count group_tasks rows, optionally filtered by status ('open' | 'done' | 'canceled').
// Read-only — proves the task card actually landed/flipped in the source of truth.
export function groupTaskCount(status?: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = (
      status
        ? db.prepare("SELECT COUNT(*) AS n FROM group_tasks WHERE status = ?").get(status)
        : db.prepare("SELECT COUNT(*) AS n FROM group_tasks").get()
    ) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count done group_tasks whose result was written by the fake's group_task_complete
// (TaskResultPrefix). Proves the completion round-trip carried the deliverable, not just
// a status flip.
export function fakeDeliveredTaskCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_tasks WHERE status = 'done' AND result LIKE 'e2e-fake-result:%'",
      )
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}
```

- [x] **Step 2: 写 spec**

创建 `e2e/tests/group-task.spec.ts`:

```ts
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
    main.getByTestId("group-task-card").first().locator("text=/#\\d+/"),
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
```

- [x] **Step 3: 跑单 spec 确认红、且红在正确的地方**

```bash
cd /Users/codfrm/Code/agentre/agentre/e2e && pnpm test -- tests/group-task.spec.ts
```

预期:FAIL 在「`getByRole("button", { name: "E2E Member" })` 找不到候选」(种子还没有第二个
agent)。如果红在更早的步骤(弹窗结构/文案选择器),先修选择器再继续——那是测试笔误,不是缺实现。

> 此时**不提交**:红 spec 进库会破坏 `make e2e` 的可平分性,Task 3 转绿后一并提交。

---

### Task 3: e2e 种子第二个成员 agent(转绿)

**Files:**
- Modify: `e2e/fakes/install.go`

- [x] **Step 1: install.go 追加成员 agent 种子**

`Install` 函数末尾(`attach backend to agent` 成功之后、最后的 Info 日志之前)追加:

```go
	// seed 第二个 agent 当群成员(挂 CEO 汇报线;子 agent 与建群弹窗 eligible 池同口径)
	// —— 任务卡 e2e(group-task.spec)需要群里有人可派活。幂等同 backend:命中名字即复用。
	const memberName = "E2E Member"
	if existing, err := agent_repo.Agent().FindByName(ctx, memberName); err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: lookup member agent failed", zap.Error(err))
		return
	} else if existing == nil {
		if _, err := agent_svc.Agent().Create(ctx, &agent_svc.CreateAgentRequest{
			Name:           memberName,
			ParentAgentID:  ceo.ID,
			AgentBackendID: backendID,
		}); err != nil {
			logger.Ctx(ctx).Error("e2efakes.Install: create member agent failed", zap.Error(err))
			return
		}
	}
```

(`agent_repo` / `agent_svc` 已在 import 块里,无需新增 import。)

种子函数本身是 log-only 装配代码(包内无既有单测,失败由 Playwright 用例抓红)——
它的行为测试就是 Task 2 的 spec,符合 harness 既有纪律(`docs/e2e-harness-guide.md`)。

- [x] **Step 2: 编译检查 + 单 spec 转绿**

```bash
cd /Users/codfrm/Code/agentre/agentre
go build -tags e2e ./e2e/fakes
cd e2e && pnpm test -- tests/group-task.spec.ts
```

预期:spec PASS(两张任务卡可见、`group_tasks` 落库且翻 done、open 归零、无 running 残留)。
失败时看 `$TMPDIR/agentre-e2e-webserver.log`(run-e2e.mjs 失败时保留)里 fake 的 stderr
(`fake: group_task_create failed: …`)定位是接缝还是 spec 的问题。

- [x] **Step 3: 提交**

```bash
git add e2e/fakes/install.go e2e/fixtures/db.ts e2e/tests/group-task.spec.ts
git commit -m "✨ e2e: 任务卡编排全链路 spec(种子成员 agent + group_tasks DB oracle)"
```

---

### Task 4: 全量回归 + 收尾

- [x] **Step 1: 全量 e2e(守既有 spec 不被第二个种子 agent 扰动)**

```bash
cd /Users/codfrm/Code/agentre/agentre && make e2e
```

预期:`smoke-chat` / `group-chat` / `session-reload` / `group-task` 全绿。
若 `group-chat.spec.ts` 因主持人 `getByRole("option").first()` 选到 E2E Member 而行为变化:
它对任一 fake 主持人都应照常通过;真翻车才把它的主持人选择改成
`page.getByRole("option", { name: "CEO 助手" }).click()` 并在提交里注明
(这是本任务引入的破坏,在 scope 内;同时更新该 spec 内「唯一 eligible」的注释)。

- [x] **Step 2: 后端测试 + lint**

```bash
make test-backend
make lint
```

预期:全绿(fake 包带 `-tags e2e`,`make test-backend` 默认不带 tag 不会编译它——
所以 Task 1 的命令必须显式 `-tags e2e` 跑过;lint 同理可能跳过,以 Task 1 Step 4 为准)。

- [x] **Step 3: 勾掉本计划全部 checkbox 并提交计划文件**

```bash
git add docs/superpowers/plans/2026-06-12-group-task-orchestration-pr4-e2e.md
git commit -m "📝 plan: 群任务卡编排 PR4(e2e seam + spec)实施计划+完成勾选"
```

---

## Out of scope

- `group_create` 工具与其 e2e(PR5)。
- 任务 tab / 回指链接 / 状态 pill 翻转的 e2e 断言(前端 Vitest 已覆盖,PR2;e2e 只断卡片出现与 DB 真相)。
- `group_task_cancel` 的 e2e(svc 层已覆盖;fake 不做取消模式,避免接缝复杂化)。
- PR2/PR3 的真机手动验证(独立挂账事项,不混入本 PR)。
