# chat_svc 错误详情透出 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 chat 路径的报错在界面上给出真实原因(通用文案 + cause 两段式),而不是一句「操作失败」。

**Architecture:** `chat_svc` 内 53 处 `i18n.NewError(ctx, code.OperationFailed)` 丢弃了 `err`。改为统一走
已存在的 `operationFailedWithCause(ctx, err)`;该 helper 内记一行日志(`AddCallerSkip(1)` 保 caller 精度),
并让 `localizedCauseError.Error()` 返回 `"操作失败\n<cause>"`。Wails 边界只过字符串,前端按首个换行拆成
headline / detail,detail 以可选中复制的次要块渲染。

**Tech Stack:** Go 1.26 + cago(`pkg/i18n`、`pkg/logger`/zap)、testify/goconvey;React 19 + TypeScript +
Vitest + shadcn `@/components/ui/*`。

**Spec:** `docs/superpowers/specs/2026-07-14-error-detail-surfacing-design.md`

## Global Constraints

- **TDD 强制**:先写失败测试,**跑一次看它按预期失败**,再实现。无失败测试不得写实现码。
- **只动 chat_svc + 前端 chat-panel/lib**:不碰其它 15 个包的 483 处 `i18n.NewError`;不做全仓 sweep。
- **提交必须带 pathspec**:`git commit <files> -m …`,**禁止裸 `git commit`**(develop/wyz 有并发会话共享 index)。
- **gitmoji 提交**;golangci-lint v2。
- **不新增 i18n key**:detail 是动态错误文本,按 AGENTS.md 不翻译动态输出。
- **前端 UI 文案一律 `t(...)`**,禁止硬编码中文。
- **detail 可选中复制**:用 `data-selectable-text="true"`,**不加复制按钮**。
- **默认语言 `zh-cn`**(cago `i18n.DefaultLang`),`internal/pkg/code` 在 init 注册,故测试中
  `context.Background()` 下 `i18n.T(ctx, code.OperationFailed) == "操作失败"`。
- **收尾门控**:`make test-backend`、`make lint`、`cd frontend && pnpm test` 全量跑,看**真 exit code**
  (`make … | tail` 会吞退出码);另跑 `gofmt -l internal/service/chat_svc`。

---

## File Structure

| 文件 | 动作 | 职责 |
| --- | --- | --- |
| `internal/service/chat_svc/errors.go` | 修改 | 错误类型:`Error()` 携带 cause;helper 记日志 |
| `internal/service/chat_svc/errors_internal_test.go` | 新建 | 上述类型的单测(须 `package chat_svc`,符号未导出) |
| `internal/service/chat_svc/chat.go` | 修改 | 47 处调用点转换 + 3 处日志去重 + 1 处过期注释 |
| `internal/service/chat_svc/git_state.go` | 修改 | 3 处调用点转换 |
| `internal/service/chat_svc/exec_target.go` | 修改 | 3 处调用点转换 + 3 处日志去重 |
| `frontend/src/lib/error-detail.ts` | 新建 | `splitErrorDetail` 纯函数 |
| `frontend/src/lib/error-detail.test.ts` | 新建 | 上述函数单测(`lib/` 用同目录共置约定) |
| `frontend/src/components/agentre/chat-panel.tsx` | 修改 | notice 加 detail 槽 + 12 个错误分支接线 |
| `frontend/src/components/agentre/__tests__/chat-panel.test.tsx` | 修改 | 追加 Notice detail 用例(复用该文件既有的 ~270 行 wails/组件 mock) |

---

### Task 1: 后端错误类型 —— `Error()` 携带 cause + helper 记日志

**Files:**
- Modify: `internal/service/chat_svc/errors.go`
- Test: `internal/service/chat_svc/errors_internal_test.go`(新建)

**Interfaces:**
- Consumes: 无(本任务是链路起点)
- Produces:
  - `operationFailedWithCause(ctx context.Context, cause error) error` —— 签名**不变**(已存在),
    但行为改变:`cause != nil` 时 `Error()` 返回 `"操作失败\n" + cause.Error()`,且内部记一行日志。
  - `cause == nil` 时行为不变,`Error()` 仍为 `"操作失败"`。
  - Task 2 的 53 处调用点全部消费此函数。

**背景(实现者必读):** `localizedCauseError` 由 `f527a50` 为 `orch_svc` 的 SQLite busy 重试引入,
但编排模块已整个删除,其 `Unwrap()` / `As()` **当前零消费者**。因此改 `Error()` **不会**打断任何现存链路。
`Unwrap()` 仍保留 —— 错误确实包着 cause,支持 `errors.Is/As` 是 Go 通用契约。

- [ ] **Step 1: 写失败测试**

创建 `internal/service/chat_svc/errors_internal_test.go`。注意 **`package chat_svc`**(内部测试包):
`operationFailedWithCause` 未导出,外部包 `chat_svc_test` 够不着。仓内已有先例
(`active_stream_internal_test.go`、`bg_running_test.go`)。

```go
package chat_svc

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/utils/httputils"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// cause 存在时:Error() = 本地化 headline + 换行 + 原始 cause,让 Wails 边界能把详情带给前端。
func TestOperationFailedWithCause_ErrorCarriesCause(t *testing.T) {
	cause := errors.New("SQL logic error: table chat_sessions has no column named run_id (1)")

	err := operationFailedWithCause(context.Background(), cause)

	assert.Equal(t,
		"操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
		err.Error())
}

// cause 为 nil 时退化成原来的通用错误,行为与改动前完全一致。
func TestOperationFailedWithCause_NilCauseDegrades(t *testing.T) {
	err := operationFailedWithCause(context.Background(), nil)

	assert.Equal(t, "操作失败", err.Error())
}

// 契约测试:errors.Is 能穿透到 cause。
// 注:原消费者 orch_svc 已随编排移除删除,这里锁的是 Go 错误包装的通用契约,不是回归护栏。
func TestOperationFailedWithCause_UnwrapsToCause(t *testing.T) {
	sentinel := errors.New("database is locked (5) (SQLITE_BUSY)")

	err := operationFailedWithCause(context.Background(), sentinel)

	assert.True(t, errors.Is(err, sentinel))
}

// 契约测试:errors.As 仍能取出 httputils.Error 且 Code 保持 OperationFailed。
func TestOperationFailedWithCause_AsHTTPError(t *testing.T) {
	err := operationFailedWithCause(context.Background(), errors.New("boom"))

	var httpErr *httputils.Error
	assert.True(t, errors.As(err, &httpErr))
	assert.Equal(t, code.OperationFailed, httpErr.Code)
}
```

- [ ] **Step 2: 跑测试,确认按预期失败**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race -run 'TestOperationFailedWithCause' ./internal/service/chat_svc/
```

预期:`TestOperationFailedWithCause_ErrorCarriesCause` **FAIL**,实际值为 `"操作失败"`(缺 `\n` + cause)。
其余 3 个应 PASS(现有行为已满足)。**若 ErrorCarriesCause 意外 PASS,停下来查原因,不要继续。**

- [ ] **Step 3: 改 `Error()` 并在 helper 内加日志**

修改 `internal/service/chat_svc/errors.go`。完整改后内容:

```go
package chat_svc

import (
	"context"
	"net/http"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

type localizedCauseError struct {
	httpErr *httputils.Error
	cause   error
}

// Error 返回「本地化 headline + 换行 + 原始 cause」。
// Wails 过界只取 Error() 字符串,这是 cause 能到达前端的唯一通道;前端按首个换行拆成 headline/detail。
func (e *localizedCauseError) Error() string {
	if e.cause == nil {
		return e.httpErr.Msg
	}
	return e.httpErr.Msg + "\n" + e.cause.Error()
}

func (e *localizedCauseError) Unwrap() error {
	return e.cause
}

func (e *localizedCauseError) As(target any) bool {
	if p, ok := target.(**httputils.Error); ok {
		*p = e.httpErr
		return true
	}
	return false
}

// operationFailedWithCause 把通用的 OperationFailed 与真实 cause 绑在一起:
// cause 既进日志(供事后排查),也随 Error() 透到前端(供当场排查)。
//
// 日志 message 用固定串:helper 内拿不到调用方方法名,精确位置由 AddCallerSkip(1) 产生的
// caller 字段(file:line)给出 —— 那本就是 docs/debugging.md 指定的最快过滤维度。
func operationFailedWithCause(ctx context.Context, cause error) error {
	if cause == nil {
		return i18n.NewError(ctx, code.OperationFailed)
	}
	logger.Ctx(ctx).WithOptions(zap.AddCallerSkip(1)).
		Error("chat_svc: operation failed", zap.Error(cause))
	return &localizedCauseError{
		httpErr: &httputils.Error{
			Status: http.StatusBadRequest,
			Code:   code.OperationFailed,
			Msg:    i18n.T(ctx, code.OperationFailed),
		},
		cause: cause,
	}
}
```

- [ ] **Step 4: 跑测试,确认全绿**

```bash
go test -race -run 'TestOperationFailedWithCause' ./internal/service/chat_svc/
```

预期:4 个测试全部 PASS。

- [ ] **Step 5: 提交(带 pathspec)**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit internal/service/chat_svc/errors.go internal/service/chat_svc/errors_internal_test.go \
  -m "✨ chat_svc: 错误携带 cause 并统一记日志"
```

---

### Task 2: 后端调用点 —— 53 处转换 + 6 处日志去重

**Files:**
- Modify: `internal/service/chat_svc/chat.go`(47 处转换,3 处去重,1 处过期注释)
- Modify: `internal/service/chat_svc/git_state.go`(3 处转换)
- Modify: `internal/service/chat_svc/exec_target.go`(3 处转换,3 处去重)
- Test: `internal/service/chat_svc/chat_test.go`(**追加**路径级测试,见 Step 4.5)
  + 现有 chat_svc 测试套件(回归护栏)

**Interfaces:**
- Consumes: Task 1 的 `operationFailedWithCause(ctx, err) error`
- Produces: 无新符号;53 处调用点从此吐 cause

**已验证的前提:** 53 处**全部**有 `err` 在作用域内(46 处前一行即 `if err != nil {`,其余为
`if x, err := …; err != nil {` 变体或位于 `if err != nil` 块内)。**不存在「无 cause 可传」的例外** ——
`errors.go:35` 的 `if cause == nil` 是 helper 自身的兜底,不是调用点。

- [ ] **Step 1: 确认改动前的基线数字**

```bash
cd /Users/codfrm/Code/agentre/agentre/internal/service/chat_svc
grep -c "i18n.NewError(ctx, code.OperationFailed)" chat.go git_state.go exec_target.go
```

预期输出:`chat.go:47`、`git_state.go:3`、`exec_target.go:3`。
**数字对不上就停下来** —— 说明分支已变,须重新盘点再动手。

- [ ] **Step 2: 机械转换 53 处**

逐处把:

```go
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
```

改成:

```go
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
```

注意事项:
- 返回值元数不尽相同(有 `return nil, …`、`return 0, …`、`return …` 等),**只换错误表达式,别动返回值形状**。
- `if x, err := repo.Foo(ctx); err != nil {` 这类,`err` 同样在作用域内,照换。
- 转换后 `i18n` / `code` 两个 import 可能在某些文件里变成未使用 —— 由 Step 5 的 `make lint` 兜住;
  **仅删真正未使用的 import,不要顺手整理其它 import**。

- [ ] **Step 3: 删掉 6 处重复日志**

这 6 处已手写 logger,helper 现在接管了记录,保留会让同一个错误记两遍:

| 文件 | logger 行 | OperationFailed 行 |
| --- | --- | --- |
| `chat.go` | 2308 | 2313 |
| `chat.go` | 3645 | 3647 |
| `chat.go` | 3671 | 3673 |
| `exec_target.go` | 30 | 32 |
| `exec_target.go` | 39 | 41 |
| `exec_target.go` | 47 | 49 |

> 行号是改动前的,转换过程中会漂移 —— 按 `logger.Ctx(ctx).Error(` 紧邻 `operationFailedWithCause` 的形态定位,
> 别死认行号。

删除整个 `logger.Ctx(ctx).Error(...)` 调用(含其多行 zap 字段),只留 `return …, operationFailedWithCause(ctx, err)`。
若删后该文件的 `logger` / `zap` import 未使用则一并删。

**例外 —— `chat.go:2219`(startTurn)不在去重表内:** 它带 `sessionId` / `agentId` / `backendType` 三个
排查用字段,helper 的固定日志给不了。**保留该 logger**,并把它上方 2217-2218 行的注释改掉 —— 它写着
「前端只看到 OperationFailed,真错要进日志才能排查」,本改动后前端已看得到 cause,该说法即刻过期:

```go
	}); err != nil {
		lock.Unlock()
		// 持久化失败比较罕见(SQLite 锁 / disk full)。cause 会随 Error() 透到前端,
		// 这里额外记 sessionId/agentId/backendType 供事后排查(helper 的通用日志给不了这些字段)。
		logger.Ctx(ctx).Error("chat_svc.startTurn: persist user+assistant messages failed",
			zap.Int64("sessionId", sess.ID),
			zap.Int64("agentId", a.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, operationFailedWithCause(ctx, err)
	}
```

> 注:2219 这处**本就**已在调 `operationFailedWithCause`(全仓唯一一处),Step 2 不会碰它;本步只改注释。
> 它会因 helper 加了日志而变成「记两行」—— 这是有意的:一行通用 + 一行带业务字段。

- [ ] **Step 4: 确认转换彻底**

```bash
cd /Users/codfrm/Code/agentre/agentre/internal/service/chat_svc
grep -c "i18n.NewError(ctx, code.OperationFailed)" chat.go git_state.go exec_target.go
grep -c "operationFailedWithCause" chat.go git_state.go exec_target.go
```

预期:第一条在三个文件中**均为 0**(errors.go 的 fallback 不在此列);
第二条 `chat.go:48`(47 转换 + 原有的 2224 那处)、`git_state.go:3`、`exec_target.go:3`。

- [ ] **Step 4.5: 补一个路径级测试(证明 cause 真的从 repo 穿到调用方)**

Task 1 只测了 helper 本身。这里锁的是**整条链路**:repo 报错 → service 返回的 error 里带得到 cause。
追加到 `internal/service/chat_svc/chat_test.go` 末尾(**`package chat_svc_test`**,走公开 API):

```go
// repo 报错时,ListAgents 返回的 error 必须带上真实 cause —— 这是 2026-07-14「新建对话发送失败:
// 操作失败」事故的回归护栏:当时 53 处调用点把 err 整个丢掉,前端和日志都只剩一句「操作失败」。
func TestListAgents_SurfacesRepoCause(t *testing.T) {
	ctrl := gomock.NewController(t)
	agentMock := mock_agent_repo.NewMockAgentRepo(ctrl)
	prev := agent_repo.Agent()
	agent_repo.RegisterAgent(agentMock)
	t.Cleanup(func() { agent_repo.RegisterAgent(prev) })

	cause := errors.New("SQL logic error: no such column: run_id (1)")
	agentMock.EXPECT().List(gomock.Any()).Return(nil, cause)

	svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
	_, err := svc.ListAgents(context.Background(), &chat_svc.ListAgentsRequest{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no such column: run_id")
	assert.ErrorIs(t, err, cause)
}
```

所需 import(`chat_test.go` 多数已有,缺哪个补哪个):

```go
	"go.uber.org/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
```

跑:

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race -run 'TestListAgents_SurfacesRepoCause' ./internal/service/chat_svc/
```

预期:PASS(Step 2 已完成转换)。

> **顺序提示**:若想严格 red-first,可在 Step 2 之前先写此测试并看它失败(`err.Error()` 只有「操作失败」),
> 再做转换。若已先做了转换,则此测试是**事后护栏**,至少要确认它在 `git stash` 掉 chat.go 改动后会失败。

- [ ] **Step 5: 跑 chat_svc 全包测试 + lint**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race ./internal/service/chat_svc/ 2>&1 | tail -5
gofmt -l internal/service/chat_svc
make lint
```

预期:测试全 PASS;`gofmt -l` **无输出**;lint 通过。

> 现有 chat_svc 测试若有断言 `err.Error() == "操作失败"` 的,会在此暴露 —— 那是**真实的行为变更**,
> 把断言改成匹配新的 headline+cause 形态,**不要**为了让测试过而回退 Task 1。

- [ ] **Step 6: 提交(带 pathspec)**

```bash
git commit internal/service/chat_svc/chat.go internal/service/chat_svc/git_state.go \
  internal/service/chat_svc/exec_target.go internal/service/chat_svc/chat_test.go \
  -m "✨ chat_svc: 53 处 OperationFailed 改为携带 cause"
```

---

### Task 3: 前端 —— `splitErrorDetail` 工具

**Files:**
- Create: `frontend/src/lib/error-detail.ts`
- Test: `frontend/src/lib/error-detail.test.ts`

**Interfaces:**
- Consumes: Task 1 产出的错误字符串形态 `"操作失败\n<cause>"`
- Produces:
  - `splitErrorDetail(e: unknown): { msg: string; detail?: string }` —— Task 4 消费。
  - 按**首个**换行拆分;无换行时 `detail` 为 `undefined`;非 `Error` 值走 `String(e)`。

> `lib/` 的约定是**同目录共置** `.test.ts`(见 `link-classify.ts` + `link-classify.test.ts`),不用 `__tests__/`。

- [ ] **Step 1: 写失败测试**

创建 `frontend/src/lib/error-detail.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { splitErrorDetail } from "./error-detail";

describe("splitErrorDetail", () => {
  it("按首个换行把后端错误拆成 headline 与 detail", () => {
    const e = new Error(
      "操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
    );

    expect(splitErrorDetail(e)).toEqual({
      msg: "操作失败",
      detail: "SQL logic error: table chat_sessions has no column named run_id (1)",
    });
  });

  it("无换行时 detail 为 undefined", () => {
    expect(splitErrorDetail(new Error("操作失败"))).toEqual({
      msg: "操作失败",
      detail: undefined,
    });
  });

  it("cause 自身含换行时只按首个换行拆,余下整体留在 detail", () => {
    const e = new Error("操作失败\nline1\nline2");

    expect(splitErrorDetail(e)).toEqual({
      msg: "操作失败",
      detail: "line1\nline2",
    });
  });

  it("非 Error 值退回 String(e)", () => {
    expect(splitErrorDetail("boom")).toEqual({ msg: "boom", detail: undefined });
  });

  it("detail 两侧空白被裁掉;裁完为空则视作无 detail", () => {
    expect(splitErrorDetail(new Error("操作失败\n   "))).toEqual({
      msg: "操作失败",
      detail: undefined,
    });
  });
});
```

- [ ] **Step 2: 跑测试,确认按预期失败**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- src/lib/error-detail.test.ts
```

预期:**FAIL**,报错为无法解析模块 `./error-detail`(文件还不存在)。

- [ ] **Step 3: 写最小实现**

创建 `frontend/src/lib/error-detail.ts`:

```ts
/**
 * 后端错误经 Wails 过界时只剩一个字符串,chat_svc 的 operationFailedWithCause 因此把
 * 「本地化 headline + 换行 + 原始 cause」编码进 Error.message。这里按首个换行还原成两段。
 *
 * detail 是动态错误文本,不进 i18n(见 AGENTS.md:不翻译动态输出)。
 */
export function splitErrorDetail(e: unknown): {
  msg: string;
  detail?: string;
} {
  const raw = e instanceof Error ? e.message : String(e);
  const at = raw.indexOf("\n");
  if (at < 0) {
    return { msg: raw, detail: undefined };
  }
  const detail = raw.slice(at + 1).trim();
  return {
    msg: raw.slice(0, at),
    detail: detail.length > 0 ? detail : undefined,
  };
}
```

- [ ] **Step 4: 跑测试,确认全绿**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- src/lib/error-detail.test.ts
```

预期:5 个用例全部 PASS。

- [ ] **Step 5: 提交(带 pathspec)**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/lib/error-detail.ts frontend/src/lib/error-detail.test.ts \
  -m "✨ frontend: 新增 splitErrorDetail 拆分后端错误详情"
```

---

### Task 4: 前端 —— Notice detail 槽 + 12 个错误分支接线

**Files:**
- Modify: `frontend/src/components/agentre/chat-panel.tsx`(状态 468-471、渲染 1965-1989、12 个 catch 分支)
- Test: `frontend/src/components/agentre/__tests__/chat-panel.test.tsx`(**追加**用例,不新建文件)

**Interfaces:**
- Consumes: Task 3 的 `splitErrorDetail(e) → { msg, detail? }`
- Produces: 无对外符号(组件内部状态)

**改动前的现场(chat-panel.tsx:468-471):**

```tsx
  const [notice, setNotice] = React.useState<{
    kind: "error" | "info";
    text: string;
  } | null>(null);
```

12 个 catch 分支现在长这样(行号 1219 / 1236 / 1300 / 1349 / 1383 / 1446 / 1465 / 1486 / 1549 / 1570 / 1592 / 1662):

```tsx
      const msg = e instanceof Error ? e.message : String(e);
      setNotice({ kind: "error", text: t("chatPanel.errors.send", { msg }) });
```

- [ ] **Step 1: 写失败测试**

**追加**到既有的 `frontend/src/components/agentre/__tests__/chat-panel.test.tsx`,**不要新建文件** ——
该文件已备好 ~270 行 wails RPC / 组件 mock(`appMocks`、`componentMocks`、`mockSessionStore`、
`makeSession`、`resetStore`),新建文件等于把这套 mock 抄一遍。

> chat-panel 间接 import wails runtime,靠的正是该文件顶部的 per-file `vi.mock`。**严禁**加全局 vite alias
> (会破坏 `App.test` / `foundation.test` 的 importActual)。

在文件末尾追加(照抄既有 send 用例 1035-1061 的触发写法):

```tsx
describe("ChatPanel · notice 错误详情", () => {
  it("Given 后端错误带 cause, When 发送失败, Then 详情块渲染 cause 且可选中复制", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ backendType: "builtin", id: 42 });
    appMocks.SendChatMessage.mockRejectedValue(
      new Error(
        "操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
      ),
    );

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;
    expect(submit).toBeDefined();

    act(() => {
      submit?.("hi");
    });

    const detail = await screen.findByTestId("notice-detail");
    expect(detail).toHaveTextContent(
      "SQL logic error: table chat_sessions has no column named run_id (1)",
    );
    // 可选中复制(globals.css 全局 user-select:none 的 opt-in),而不是加复制按钮
    expect(detail).toHaveAttribute("data-selectable-text", "true");
  });

  it("Given 后端错误无 cause, When 发送失败, Then 不渲染详情块", async () => {
    resetStore();
    mockSessionStore.session = makeSession({ backendType: "builtin", id: 42 });
    appMocks.SendChatMessage.mockRejectedValue(new Error("操作失败"));

    render(<ChatPanel sessionId={42} />);
    const submit = componentMocks.chatComposerProps.at(-1)?.onSubmit as
      | ((text: string) => void)
      | undefined;

    act(() => {
      submit?.("hi");
    });

    // 先等 notice 出现,再断言详情块不存在 —— 否则可能在 notice 渲染前就通过了。
    await waitFor(() => {
      expect(appMocks.SendChatMessage).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByTestId("notice-detail")).toBeNull();
    });
  });
});
```

> 断言只针对 **detail**(动态错误文本,不经 i18n),因此不受测试环境 locale 影响;
> 别去断言 headline 的中英文。

- [ ] **Step 2: 跑测试,确认按预期失败**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- src/components/agentre/__tests__/chat-panel.test.tsx -t "notice 错误详情"
```

预期:第 1 个用例 **FAIL**(找不到 `notice-detail`,detail 尚未渲染);第 2 个应 PASS。
**若第 1 个意外 PASS,停下来查原因。**

- [ ] **Step 3: 状态加 detail 槽**

`chat-panel.tsx:468-471` 改为:

```tsx
  const [notice, setNotice] = React.useState<{
    kind: "error" | "info";
    text: string;
    detail?: string;
  } | null>(null);
```

- [ ] **Step 4: 渲染详情块**

`chat-panel.tsx:1974-1977` 的 `<AlertDescription>` 内,把单个 `<span>` 换成 headline + 可选 detail:

```tsx
                    <AlertDescription className="flex min-w-0 items-start gap-2">
                      <div className="flex min-w-0 flex-1 flex-col gap-1">
                        <span className="min-w-0 break-words text-xs leading-snug">
                          {notice.text}
                        </span>
                        {notice.detail ? (
                          <span
                            data-testid="notice-detail"
                            data-selectable-text="true"
                            className="min-w-0 break-words font-mono text-[11px] leading-snug opacity-80"
                          >
                            {notice.detail}
                          </span>
                        ) : null}
                      </div>
                      <button
                        type="button"
                        aria-label={t("chatPanel.notice.close")}
                        onClick={() => setNotice(null)}
                        className="-mr-1 inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-current opacity-70 transition-opacity hover:opacity-100"
                      >
                        <X className="size-3" aria-hidden="true" />
                      </button>
                    </AlertDescription>
```

> `data-selectable-text="true"` 让详情可选中复制(globals.css 全局 `user-select:none` 的 opt-in 机制)。
> **不加复制按钮。**

- [ ] **Step 5: 12 个错误分支接线**

在 import 区加:

```tsx
import { splitErrorDetail } from "@/lib/error-detail";
```

把 12 处(行 1219 / 1236 / 1300 / 1349 / 1383 / 1446 / 1465 / 1486 / 1549 / 1570 / 1592 / 1662)从:

```tsx
      const msg = e instanceof Error ? e.message : String(e);
      setNotice({ kind: "error", text: t("chatPanel.errors.send", { msg }) });
```

改为:

```tsx
      const { msg, detail } = splitErrorDetail(e);
      setNotice({ kind: "error", text: t("chatPanel.errors.send", { msg }), detail });
```

注意:
- **各分支的 i18n key 不同**(`errors.send` / `errors.goal` / `errors.compact` / `errors.rename` …),
  **只替换 msg 的取法与补 detail,别动 key**。
- 个别分支的 `msg` 还被用在别处(如 `console.error`),**保留那些用法**。
- 行号会随编辑漂移,按 `e instanceof Error ? e.message : String(e)` 的形态定位。

- [ ] **Step 6: 跑测试 + 类型 + lint**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- src/components/agentre/__tests__/chat-panel.test.tsx
pnpm test -- src/lib/error-detail.test.ts
npx tsc --noEmit
pnpm lint
```

> 跑 **整个** chat-panel.test.tsx(而非只跑新用例):notice 渲染结构改了,既有用例可能被波及。

预期:测试全 PASS;tsc 无错;eslint 无错。
**LSP 内联诊断常 stale,以 `tsc --noEmit` 的输出为准。**

- [ ] **Step 7: 提交(带 pathspec)**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/chat-panel.tsx \
  frontend/src/components/agentre/__tests__/chat-panel.test.tsx \
  -m "✨ frontend: 错误提示展示后端 cause 详情(可选中复制)"
```

---

### Task 5: 收尾门控

**Files:** 无改动(纯验证)

- [ ] **Step 1: 后端全量**

```bash
cd /Users/codfrm/Code/agentre/agentre
make test-backend; echo "EXIT=$?"
```

预期:`EXIT=0`。**必须看 exit code** —— `make … | tail` 会吞掉 make 的退出码。

- [ ] **Step 2: 格式 + lint 全量**

```bash
gofmt -l internal/service/chat_svc; echo "GOFMT_EXIT=$?"
make lint; echo "EXIT=$?"
```

预期:`gofmt -l` 无输出;`EXIT=0`。

- [ ] **Step 3: 前端全量**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test; echo "EXIT=$?"
```

预期:`EXIT=0`。**跑全量而非 focused** —— 判断哪些测试需要补 wails mock,靠的就是全量。

- [ ] **Step 4: 复审自己的 diff**

```bash
cd /Users/codfrm/Code/agentre/agentre
git log --oneline -5
git show <本任务首个 commit>^..<本任务末个 commit> --stat
```

确认:
- 只含 `chat_svc/{errors.go,errors_internal_test.go,chat.go,chat_test.go,git_state.go,exec_target.go}`
  + `frontend/src/lib/error-detail.{ts,test.ts}`
  + `frontend/src/components/agentre/chat-panel.tsx` + `__tests__/chat-panel.test.tsx`
  + 本 plan/spec。
- **不含**任何并发会话的文件(例如 `chat-input/mentions/` 下的 `tmp-offset-probe.test.tsx`)。
- 用 `git show <commit>` 逐个复审,**不要**用 `BASE..HEAD`(会夹入他人 commit)。

---

## Out of scope (YAGNI)

- 其它 15 个包的 483 处 `i18n.NewError`。
- `code.OperationFailed` 的语义或错误码体系本身。
- 结构化 IPC / JSON 错误通道。
- 全仓替换 `e instanceof Error ? …` 的 sweep(只动本次涉及的 12 个分支)。
- 删除 `localizedCauseError.As()` —— 虽已确认零消费者,但属独立的死码清理决策,**发现即上报,不顺手删**。
- **本计划不修复触发它的那个事故**:`run_id` 报错的根因是装的 app 是 Jul 10 旧包、DB 已被迁移
  `202607140001` 推进,需 `make install` 重装解决。本计划保证的是**下次**出问题时原因当场可见。
