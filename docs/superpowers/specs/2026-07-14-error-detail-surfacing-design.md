# chat_svc 错误详情透出 — Design

- 日期:2026-07-14
- 分支:develop/wyz
- 状态:Approved

## Goal

让 chat 路径的报错能给出**可诊断的原因**,而不是一句通用的「操作失败」。

触发这个设计的真实事故:2026-07-14 新建对话持续「发送失败:操作失败」,界面没有任何可用信息。真正的原因是
`SQL logic error: table chat_sessions has no column named run_id (1)`(装的 app 是 Jul 10 的旧包,而 DB 已被
迁移 `202607140001` 推进、`run_id` 列已删)。这个原因**只是碰巧**能查到——它落在 `chat.go:2229` 这条恰好写了
日志的路径上;换成 chat_svc 里其它任意一处,日志会是一片空白,无从下手。

现状有两个独立的缺口:

1. **cause 在调用点就被丢弃**。典型写法把 `err` 整个扔掉,既不 log 也不 wrap:

   ```go
   agents, err := agent_repo.Agent().List(ctx)
   if err != nil {
       return nil, i18n.NewError(ctx, code.OperationFailed)  // err 就此蒸发
   }
   ```

2. **已有的 cause 机制到不了 UI**。`chat_svc/errors.go` 的 `localizedCauseError`(由 `f527a50` 为
   SQLite busy 重试引入)虽然 `Unwrap()` 保住了 cause,但 `Error()` 只返回本地化的 `Msg`;而 Wails 过界只取
   `Error()` 字符串,cause 必然在边界上丢失。全代码库只有 1 处用到它。

> 背景判断:`code.OperationFailed` 这套「错误码 + 不泄漏内部细节」是 cago 的 **HTTP API** 惯例,防的是不可信的
> 远程客户端。Agentre 是桌面应用,"客户端"就是本机 UI、用户就是开发者本人,这层遮蔽什么都没换来,只赔掉了全部
> 可诊断性。本设计不推翻 `code.OperationFailed` 的语义,只在 chat_svc 内让 cause 能穿过边界。

## Scoping decisions (locked)

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| UI 形态 | 通用文案 + 详情两段式 | 顶行保留 i18n 文案,下方附真实 cause;i18n 规则不破 |
| 改造范围 | 仅 chat_svc 的 53 处 | 事故发生地,一包内全覆盖,diff 聚焦 |
| 跨边界方式 | 换行分隔 + 前端拆分 | 不新增 IPC 通道;详情是动态内容,天然不进 i18n |
| 错误类型位置 | 继续留在 `chat_svc/errors.go` | 只有一个消费者,不下沉 `internal/pkg`(YAGNI) |
| detail 复制方式 | `data-selectable-text="true"` | 可选中复制,不加复制按钮 |

**已否决的方案:**

- **`Error()` 返回 JSON** — 强契约,但 error 字符串本身变成一坨 JSON,所有未改造的 catch 点和日志都会显示
  `{"msg":...}`,污染面太大。
- **单行拼接 `操作失败: <cause>`** — 前端零改动,但长 SQL 错误挤成一行没有视觉层次。
- **全局 483 处 `i18n.NewError` 一起改** — 跨 15+ 个包,diff 巨大,踩中 AGENTS.md「不动无关文件」。

## 关键约束(已验证)

- **Wails 边界只过字符串**。前端一律 `e instanceof Error ? e.message : String(e)`,没有结构化通道。
  detail 必须由 `Error()` 自己携带,前端再拆。
- **`Unwrap()` / `As()` 必须保持行为**。`f527a50` 的 SQLite busy 重试依赖 `errors.Is/As` 穿透到 cause;
  改 `Error()` 不能破坏这条链路。
- **`logger.Ctx(ctx)` 返回 `*zap.Logger`**,因此 `.WithOptions(zap.AddCallerSkip(1))` 可用 —— helper 内记日志时
  `caller` 字段仍能指向真实调用点,而不是 `errors.go`。

## 现状盘点(已验证)

`i18n.NewError(ctx, code.OperationFailed)` 真实调用点共 **53 处**:

| 文件 | 处数 | 备注 |
| --- | --- | --- |
| `chat.go` | 47 | 其中 2 处已手写 logger |
| `git_state.go` | 3 | — |
| `exec_target.go` | 3 | 3 处均已手写 logger |

`errors.go` 内另有 3 处 `code.OperationFailed`,是 helper 自身的 fallback 与结构体字段,**不是调用点**。

53 处**全部**有 `err` 在作用域内(46 处前一行即 `if err != nil {`,其余为 `if x, err := …; err != nil {` 变体或
位于 `if err != nil` 块内的 logger 之后),因此转换是机械的,不存在「无 cause 可传」的例外。

## Architecture / data flow

```
repo/db 返回真实 err
   └─ chat_svc 调用点: operationFailedWithCause(ctx, err)
        ├─ 记一行日志(AddCallerSkip(1) → caller 指向真实调用点)
        └─ 返回 localizedCauseError{ httpErr: 操作失败, cause: err }
             └─ Error() → "操作失败\n<cause>"
                  └─ Wails 边界(仅字符串)
                       └─ 前端 splitErrorDetail(e) → { msg, detail }
                            └─ Notice: t("chatPanel.errors.send", { msg }) + detail 块
```

## Components

### A. 后端 — `internal/service/chat_svc/errors.go`

1. `localizedCauseError.Error()` 从只返回 `e.httpErr.Msg`,改为 `Msg + "\n" + cause.Error()`。
2. `Unwrap()` / `As()` **原样保留**,busy 重试链路不受影响。
3. `operationFailedWithCause(ctx, cause)`:
   - `cause == nil` 时退化为 `i18n.NewError(ctx, code.OperationFailed)`,行为与今天完全一致;
   - `cause != nil` 时在返回前记一行 `logger.Ctx(ctx).WithOptions(zap.AddCallerSkip(1)).Error(...)`,
     cause 走 `zap.Error(cause)`。

   一处改动 → 53 个调用点全部获得可观测性,正面关闭缺口 1。

   **日志 message 的取值**:helper 内部拿不到调用方的方法名,因此 message 为**固定串**
   `"chat_svc: operation failed"`;精确位置由 `AddCallerSkip(1)` 产生的 `caller` 字段(file:line)给出。

   这相对 AGENTS.md「message 用小写 `package.Method:` 前缀」是一处**有意识的偏离**:`Method` 在 helper 内不可得,
   而 `caller` 字段本就是 docs/debugging.md 指定的「最快过滤维度」,精度高于方法名。
   替代方案是给 53 处各传一个 op 字符串(`operationFailedWithCause(ctx, "ListAgents", err)`),徒增 53 处
   churn 且与 `caller` 信息重复,不采纳。

### B. 后端 — 调用点转换

53 处 `i18n.NewError(ctx, code.OperationFailed)` → `operationFailedWithCause(ctx, err)`。

5 处已手写 logger 的(`exec_target.go` ×3、`chat.go` ×2)**删掉原有 logger 调用**,由 helper 接管,
否则同一个错误会被记两遍。

### C. 前端 — 错误提取

新增 `splitErrorDetail(e: unknown): { msg: string; detail?: string }`,按**首个**换行拆分:
换行前为 headline,换行后(若有)为 detail。无换行时 `detail` 为 `undefined`,行为与今天一致。

取代散落各处的 `e instanceof Error ? e.message : String(e)`,**但只在本次改造涉及的 chat-panel 错误分支替换**,
不做全仓 sweep。

### D. 前端 — Notice 渲染

`chat-panel.tsx` 的 notice 状态 `{ kind, text }` 增加可选 `detail`。错误分支改为:

```ts
const { msg, detail } = splitErrorDetail(e);
setNotice({ kind: "error", text: t("chatPanel.errors.send", { msg }), detail });
```

detail 块以次要样式渲染在 headline 下方,挂 `data-selectable-text="true"` 使其可选中复制。
`detail` 为空时不渲染该块。

**无新增 i18n key** —— detail 是动态错误文本,按 AGENTS.md 不翻译动态输出。

## Testing (TDD — red first, per repo invariants)

**Go(`chat_svc`,不连 DB):**

1. `Error()` 在 cause 存在时返回 `"操作失败\n<cause>"`。
2. `cause == nil` 时退化,`Error()` 仍为 `"操作失败"`。
3. `errors.Is(err, target)` 仍能穿透到 cause —— 锁死 `f527a50` 的 busy 重试不被破坏。
4. `errors.As(err, **httputils.Error)` 仍拿得到 `Code == code.OperationFailed`。
5. 关键路径(以 mock repo 注入失败)断言返回的 error 文本含 cause。

**前端(Vitest):**

6. `splitErrorDetail`:有换行 → 拆成 msg + detail;无换行 → detail 为 undefined;非 Error 值 → 走 `String(e)`。
7. Notice 在 `detail` 存在时渲染详情块且带 `data-selectable-text`;为空时不渲染。

## Out of scope (YAGNI)

- 其它 15 个包的 483 处 `i18n.NewError` —— 后续按需接入。
- 改变 `code.OperationFailed` 本身的语义或错误码体系。
- 结构化 IPC / JSON 错误通道。
- 全仓替换 `e instanceof Error ? …` 的 sweep。
- **本设计不修复触发它的那个事故**:`run_id` 报错的根因是装的 app 是 Jul 10 旧包、DB 已被迁移推进,
  需 `make install` 重装解决。本设计保证的是**下次**出问题时原因直接可见。

## Concurrency / branch notes

develop/wyz 上有并发会话共享 index,提交一律 `git commit <files>` 带 pathspec,不裸 commit。
复审 diff 用 `git show <commit>`,不用 `BASE..HEAD`。
