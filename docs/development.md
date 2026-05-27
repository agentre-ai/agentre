# Development Conventions

测试、修 bug、设计接口时的强约束。**Red → Green → Refactor，无例外。**

## TDD / BDD 工作流

1. **Red — 先写失败的测试。**
   - **新功能：** 从 **BDD-style** behavior spec 开始 —— 描述用户可见的结果（`Given … When … Then …` 或 goconvey `Convey("when X, then Y")` block）。Happy path + 至少一个 boundary/error case。
   - **修 bug：** 写一个 **TDD** regression test 复现失败（同样的 error/assertion）。跑一遍，确认它因为正确的原因失败。如果 bug 实在没法在测试里复现，先**显式说明**再 patch。
   - Tests 紧贴代码（`foo_test.go`）；分支组合用表驱动；mock 走 `go.uber.org/mock`，放在 `mock_*/` 子目录里（`make mock` 生成）。
2. **Green — 让测试通过的最小代码。** 不投机泛化、不预留字段。跑 `make test`，确认红变绿且没回归别的。
3. **Refactor — 绿灯下清理。** 重命名 / 抽函数 / 去重。再跑一遍 `make test` 和 `make lint` 再提交。

**Layer-by-layer testing：** repository 测试覆盖持久化 + 边界（用 `testutils.Database(t)` sqlmock，**断 SQL/参数**，不要起真库）；service 测试通过 `mockgen` mock repo 覆盖业务规则；`internal/app/` bindings 太薄无需单测 —— **不要把测得了的逻辑塞进 `App`。**

**Coverage：** 新包 service/repository 层目标 ≥80%。`make test-cover` 查。

## SOLID

每个新 package / type / function 合并前过一遍：

- **S — Single Responsibility.** 一个改动理由。Service 同时干 parsing + persistence + notification → 拆。300 行的函数基本都违反 SRP。
- **O — Open/Closed.** 通过加新类型/策略扩展，不要改 10 个 caller 里的 switch。新增 Agent backend（Claude Code / Codex / 内置）应当是实现 interface，不是 patch `if engine == ...`。
- **L — Liskov.** 实现必须遵守接口契约（nil 语义、error 类型、side-effect 边界）。No "这个实现忽略 ctx" 这种 surprise。
- **I — Interface Segregation.** 在**消费方**定义窄接口（`service` 声明它需要 repo 的什么），不要在生产方搞大胖接口。消费方需要 2 个方法，不应该依赖 20 个方法的接口。
- **D — Dependency Inversion.** `service/` 依赖 repository **接口**，绝不依赖具体实现。具体实现在 `main.go` / `internal/app/` 装配 —— 这是 TDD 能 mock 的前提。

## Fix Discipline（强约束）

bug / 回归 / 异常行为出现时：

1. **先写测试，确认问题存在。** 写一个失败的测试并跑它。如果它不失败（或为错误的原因失败），说明你还没理解 bug —— 先调查再 patch。No fix lands without a red-then-green test pair.
2. **干净的修复 —— 修根因，不打补丁。** Fix the producer of the bad value，不是每个 consumer。**不要**加 `if x == nil` 来掩盖 caller bug。**不要**在三个调用点重复 normalize 同一个字段 —— 在边界 normalize 一次。`// workaround because X returns Y` 这种注释就是 smell，底下的代码大概率要改。
3. **不要改动不相关的内容。** Diff 只动 producer、它的测试，最多再加一个就在你光标下方明显在 scope 内的 drift。**No** drive-by refactors / rename sweeps / formatter passes / 死代码清理 / import 重排序 / 不相关 test churn —— 它们会埋掉真正的 fix 并破坏 `git bisect`。多日重构 / 热点子系统 / 设计问题 → flag 出来先问。
4. **Reuse first.** 加新组件 / hook / util / Go helper 前先 grep 找现成的。复制 >10 行？抽出来。两个近似 block 改同样的 bug？第二个就是 bug —— 删掉，调第一个。

## 测试栈

- **框架** — `github.com/smartystreets/goconvey/convey`（BDD 嵌套）+ `github.com/stretchr/testify`（断言）+ `go.uber.org/mock`（接口 mock）+ `github.com/DATA-DOG/go-sqlmock`（DB mock）。表驱动用例处理分支组合。
- **Repo 单测：一律走 `testutils.Database(t)`，禁止用真 SQLite。** 拿 `(ctx, _, mock)` 三元组，业务代码通过 `db.Ctx(ctx)` 自动命中 mock；每个用例用 `mock.ExpectQuery / ExpectExec` 精确断言 SQL 与参数，结尾必加 `assert.NoError(t, mock.ExpectationsWereMet())`。注意 **`testutils.Database` 用的是 MySQL 方言**，正则匹配里用 `` `table_name` `` 反引号；GORM `Create / Save / Update` 默认会自动开事务，匹配模板写 `ExpectBegin / ExpectExec / ExpectCommit`（错误路径补 `ExpectRollback`）。**例外**：迁移本身（`migrations/*`）+ `internal/bootstrap/cago_test.go` 这类端到端启动测试，可以用 `t.TempDir()` 起真 SQLite 验证完整 DDL/数据流。其它一切 repo / service 测试都走 mock。
  - **历史教训**：早期 `agent_backend_repo_test.go` 用 `t.TempDir()` + 真 SQLite，测的是 SQLite 方言副作用而不是 repo 自己拼出的 SQL；新增 repo test 必须从一开始就用 sqlmock。
- **Service 单测** — `mockgen` 生成 `repository` mock，通过 `RegisterXxx(mockRepo)` 注入。Service 只依赖仓储接口（DIP），所以不需要 DB，**禁止在 service test 里起 sqlmock 或真库**。
- **Mock 生成** — repository 接口顶部加 `//go:generate mockgen -source xxx.go -destination mock_xxx_repo/mock_xxx.go`，统一 `make mock`。

### 测试组织

- 每个 `*_svc` / `*_repo` 包内一个 `setupXxxTest(t)` helper：构造 `gomock.NewController(t)` + `t.Cleanup(ctrl.Finish)`，注入 mock，返回 `(ctx, mocks..., subject)`。
- 共享操作抽 `t.Helper()` 函数，断言失败定位到调用方而不是 helper 内部。
- **GoConvey 嵌套约定：** 顶层 = 功能名 / 方法名（`Convey(..., t, func() { ... })`）→ 嵌套 = 场景（成功 / 失败 / 边界）→ 深层 = 时序行为（"做完 A 之后再做 B"）。每个 `Convey` 块独立运行，可放心在外层 setup mock。
- 组件状态（`testutils.Cache()` / `testutils.Redis()` / `iam.SetDefault(...)`）有 `sync.Once` 缓存，**会跨用例残留**。涉及 IAM / cache 的 setup 在 helper 里手动 `SetDefault` 重新注入，避免 mock 句柄被旧引用持有。

> 详见 cago skill（`/cago`）—— 完整的 controller / service / repo / cron / queue 单测样例。

### Linter 例外（见 `.golangci.yml`）

cago tool handlers 把 error 包成 `tool.ErrorResult` 然后 return `(*ToolResultBlock, nil)`，这样 LLM 看到 failed call 但不会 abort 整个 turn —— 保留这个返回 shape，在 return 处加一行 `//nolint:nilerr` 注释说明原因（例：`internal/service/llm_provider_svc/llm_provider.go`、`project_svc/project.go`、`remote_device_svc/refresh.go`）。`llm_provider_svc/*_test.go` 和 `internal/daemon/*_test.go` 的 fake `*http.Response` body / `t.TempDir()` 派生的 cert path 免除 `bodyclose` / `gosec` 检查。

## 日志（关键流程必打）

cago zap 封装 `github.com/cago-frame/cago/pkg/logger`，落到 `<AppDataDir>/logs/agentre.log`（`AGENTRE_DEBUG=1` 降为 debug+）。读日志、查 SQLite、复现线上 bug 的命令清单见 [debugging.md](debugging.md)。

- **调用方式：** 有 ctx 用 `logger.Ctx(ctx)`，否则 `logger.Default()`（bootstrap / `gogo.Go` 里）。字段一律 `zap.Xxx(...)` 结构化，**禁止** `fmt.Sprintf` 拼 message。
- **Message 格式：** 小写 + `package.Method:` 前缀，动态值放字段，便于 grep。例：

  ```go
  logger.Ctx(ctx).Warn("chat_svc.Stop: runner.Abort failed",
      zap.String("session_id", sid), zap.Error(err))
  ```

- **必打点：**
  1. 生命周期边界（session/turn/runtime 启停、abort、auto-continue）
  2. 外部调用边界（CLI spawn/exit、remote borrow/return、HTTP、迁移、FS）
  3. 降级 / fallback / retry
  4. service/repo `return err` 之前
  5. 状态变更（permission mode、登录、远端连接）
- **级别：** Debug = 正常路径细节；Info = 业务里程碑；Warn = 可恢复错误 / 被吞的 defer 错误；Error = 状态可能损坏，字段必须够还原现场。
- **反模式：** 只 return err 不打日志；循环里刷 info；message 里塞动态值。
