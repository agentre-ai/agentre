# Architecture

Agentre 的代码组织、分层约定、远端执行架构与持久化布局。新写代码前先看这一份。

## 项目布局

```text
main.go                        (wails bootstrap + `agentre claudecode …` CLI shim)
cmd/agentred/                  (headless daemon binary — root.go / run.go / pair.go / status.go / client.go)
internal/
  app/                         (Wails bindings — 每个 domain 一个文件：app.go / agent.go / chat.go / project.go / …，
                                方法只做 parse → svc.Xxx().Method(ctx, …) → return)
  bootstrap/                   (启动顺序：dataDir → cago memory config → logger → SQLite → migrations)
  cli/claudecodecmd/           (agentre 二进制的 CLI 子命令，被 hook 子进程调用)
  daemon/                      (agentred 端 daemon：ipc / handlers / sessions / pairing / rpc / remotefs / notifier / state)
  service/<domain>_svc/        (业务逻辑；接口 + 单例访问器 + 私有实现)
  repository/<domain>_repo/    (数据访问；接口 + Register/accessor，统一走 db.Ctx(ctx))
    mock_<domain>_repo/        (mockgen 产物，供 service 单测注入)
  model/entity/<domain>_entity/(充血实体；GORM tag + 业务方法)
  pkg/                         (cross-cutting 内部包：agentprovider / agentruntime / claudecodehook / cliprober /
                                code (i18n 错误码) / diff / httpgateway / jsonrpc / keychain / llmcatalog / paths / remotefs)
  buildinfo/                   (CommitID ldflag target)
migrations/                    (gormigrate 顺序迁移，文件名前缀 YYYYMMDDNNNN)
pkg/                           (对外可复用包：claudecode、codex —— 各自维护的 CLI 子进程封装)
frontend/                      (React 19 + TS + Vite + Tailwind；wailsjs/ 由 wails 生成，gitignored)
```

`App` struct 在 `internal/app/app.go`（生命周期 + 通用方法），domain 方法散在 sibling 文件（`agent.go`、`chat.go`、……）。**Keep these bindings thin — logic inside `App` is unreachable from `go test`，业务一律放 `service/`。**

## 远端执行（remote chat）

桌面端可以把单个 chat 投到 LAN 上的 `agentred` 守护进程执行：

```
UI
  → internal/app Wails 绑定
  → chat_svc
  → internal/daemon/client (JSON-RPC client)
  → agentred (internal/daemon/{ipc,handlers,sessions})
  → claude-code / codex 子进程
```

- Tool approval / ask-user-question 仍由桌面端 UI 渲染。
- 断线会 abort 整个 chat。
- pairing / 设备状态走 `internal/pkg/remotefs` + `remote_device_svc`。

## 分层约定（cago 框架风格）

- **Entity（充血模型）** — 围绕单个实体的校验（`Check(ctx)`）、状态判断（`IsActive()`）、字段序列化（`GetXxx/SetXxx`）方法都放在 entity 上。Service 只做跨实体协调与外部依赖编排。**不要把规则一股脑堆进 service。**
- **Repository** — 消费方约束模式：`type XxxRepo interface { ... }` + `func Xxx() XxxRepo` + `RegisterXxx(impl)`。查询统一 `db.Ctx(ctx).…`。事务：

  ```go
  db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
      ctx = db.WithContextDB(ctx, tx)
      // … 事务里所有 repo 调用透明走 tx
  })
  ```

- **Service** — 接口 + 单例 + 私有实现；service 只依赖 repository 接口（依赖倒置），便于 mockgen 单测。后台任务用 `gogo.Go(func() error { … })`，**不要把请求 ctx 透传进 goroutine**。
- **Error / i18n** — 错误码定义在 `internal/pkg/code/code.go`（分段分配 10000+），文案在 `zh_cn.go` / `en.go`，调用 `i18n.NewError(ctx, code.Xxx)` 返回；HTTP 状态可用 `i18n.NewForbiddenError` / `i18n.NewErrorWithStatus`。
- **Wails 绑定层** — `internal/app/*.go` 的方法只做：解析 → 调 `svc.Xxx().Method(ctx, …)` → 返回。**不要往 App 结构里塞业务**，否则 go test 覆盖不到。新增一个 domain 就开一个新文件。
- **Entity 优先于硬编码** — 用持久化字段（type/status/icon/color/config）作 source of truth，避免 service 里硬编码默认值绕过 entity。

## 存储与路径

桌面端所有持久化都集中在 **AppDataDir**：

| 平台    | AppDataDir                                              |
| ------- | ------------------------------------------------------- |
| macOS   | `~/Library/Application Support/agentre/`                |
| Windows | `%LOCALAPPDATA%\agentre\`                               |
| Linux   | `~/.config/agentre/`                                    |

测试或排查时用 `AGENTRE_DATA_DIR` 覆盖。

```text
<AppDataDir>/
  agentre.db          ← SQLite 业务数据库（gorm + gormigrate）
  logs/
    agentre.log       ← 全量日志（info+，DebugMode 时降为 debug+）
    error.log         ← 仅 error+
```

- **业务数据** → SQLite，走 `internal/repository/*_repo`。
- **cago 运行时配置** → 内存 source（`configs.WithSource(...)`），**不落盘 `config.json`**。
- **前端体验偏好（主题、窗口大小等）** → 浏览器 localStorage。现有 key 包括 `agentre.theme`、`agentre.windowSize`、`agentre.lastPath`。
- **agentred** 用独立目录 `agentred`，可用 `AGENTRED_DATA_DIR` 覆盖。

环境变量：

- `AGENTRE_ENV` — `dev` / `test` / `pre` / `prod`，默认 `dev`。
- `AGENTRE_DEBUG` — `1` / `true` / `yes` / `on` 开启调试日志和 DB debug 模式。

## 数据库与迁移

- 驱动：纯 Go SQLite（`github.com/glebarez/sqlite`，无需 CGO），通过匿名导入 `_ "github.com/cago-frame/cago/database/db/sqlite"` 注册到 cago 的 `db` 组件。
- 初始化由 `internal/bootstrap.Init` 统一完成：注册 `db.Database()` 组件 → 调 `migrations.RunMigrations(db.Default())`。运行时取库走 `db.Ctx(ctx)`，跨函数事务用 `db.WithContextDB(ctx, tx)`。

新增迁移：

1. 在 `migrations/` 新建 `YYYYMMDDNNNN_xxx.go`，导出 `migrationYYYYMMDDNNNN() *gormigrate.Migration`。
2. 在 `migrations/migrations.go` 的 `migrationList()` **末尾**追加。**禁止改动既有迁移**，需要修复时新增补丁迁移。
3. DDL 优先用原生 SQL（`tx.Exec(…)`），不要依赖 `AutoMigrate` 的隐式行为。

## 生成 / 自管理文件

| Path                                | Producer                                 | Regenerate                  |
| ----------------------------------- | ---------------------------------------- | --------------------------- |
| `frontend/wailsjs/**`               | Wails (from `App` bindings + Go structs) | `make dev` / `wails build`  |
| `internal/**/mock_*/`               | `mockgen`                                | `make mock`                 |
| `frontend/dist/`                    | Vite (embedded via `//go:embed`)         | `wails build`               |
| `<AppDataDir>/agentre.db`           | gorm + gormigrate                        | 启动时自动迁移              |
| `<AppDataDir>/logs/*.log`           | cago logger                              | 运行时滚动                  |

Lockfiles —— 永不手编，用 `go mod tidy` / `pnpm add|remove|install`。`frontend/wailsjs/` 是 gitignored。
