# AGENTS.md

Guidance for Codex when working in this repository.

## Project Facts

- Agentre is a Wails v2 desktop app with a Go backend and a React/TypeScript frontend.
- Go module path is `agentre`. Do not invent a `github.com/...` import prefix.
- Primary stack: Go 1.26, Wails v2, React 19, TypeScript, Vite, Tailwind CSS v4, pnpm 10.33.
- Frontend-to-backend IPC goes through Wails bindings in `internal/app`; generated bindings live under `frontend/wailsjs`.
- `main.go` boots the desktop app and has a special `agentre claudecode ...` passthrough mode for Claude Code hook subprocesses.
- `cmd/agentred` builds the companion headless daemon used for remote execution over JSON-RPC/WebSocket.

## 高优先级约束（强制，不可绕过）

下面三条是硬性规则。和当前任务冲突时**先停下来问用户**，不要自作主张绕开。

1. **严格 TDD / BDD：Red → Green → Refactor，无例外。**
   - 新功能先写 BDD-style behavior spec（`Given … When … Then …` 或 goconvey `Convey("when X, then Y")`），覆盖 happy path + 至少一个 boundary/error case，**再写实现**。
   - 没有失败测试，不写实现代码。细节见 [docs/development.md](docs/development.md)。
2. **修 bug 前先验证 bug 存在。**
   - 写 regression 测试复现失败，**跑一遍并看到它因为正确的原因失败**，再 patch。
   - 实在没法复现时**显式说明**，再和用户讨论 patch 方案 —— 不要默默地"应该是这样修"就改。
3. **不要改动和当前任务无关的文件。**
   - Diff 只动 producer + 它的测试，最多一个明显在 scope 内的 drift。
   - **禁止** drive-by refactor / 重命名 sweep / formatter pass / 死代码清理 / import 重排序 / 不相关 test churn。
   - 看到无关脏数据先 flag 给用户，**不要顺手改**。

## 开发规范（必读）

写代码 / 修 bug / 写测试前，先看：

- [docs/architecture.md](docs/architecture.md) — repository layout、cago 分层约定（entity / repo / service / wails binding）、远端执行架构、`AppDataDir` 存储路径、`AGENTRE_DATA_DIR` / `AGENTRE_ENV` 环境变量（Debug 日志改由「设置 → 版本 & 更新」开关控制）、迁移流程、生成文件清单。
- [docs/development.md](docs/development.md) — Red→Green→Refactor、SOLID、Fix Discipline、测试栈（`testutils.Database(t)` + sqlmock + mockgen + goconvey）、commit 风格、日志规范、`.golangci.yml` 例外。
- [docs/frontend.md](docs/frontend.md) — shadcn `@/components/ui/*` 强制约定、pnpm、`make lint` / `gofmt` / `goimports`。
- [docs/debugging.md](docs/debugging.md) — sqlite3 / jq / 日志过滤命令、table-to-feature 映射、常见踩坑（macOS `Application Support` 路径要引号）。
- [docs/agent-backend.md](docs/agent-backend.md) — 接入新 AI Agent backend 的完整路径（entity / migration / runtime / translator / capability / daemon import / 前端 gating），含 TDD 测试清单与常见反模式。

## 关键约束（必记）

- **Wails 绑定层只做 parse → svc.Xxx().Method → return**，业务塞进 `App` 结构会被 go test 漏掉。
- **修 bug 必须先写失败的回归测试**；不要在 consumer 加 guard 来掩盖 producer bug；同次提交不夹带 drive-by refactor / formatter pass。
- **Repository 单测一律 `testutils.Database(t)` + sqlmock**，禁止起真 SQLite（迁移自身和 `internal/bootstrap/cago_test.go` 是仅有例外）。
- **Service 单测**通过 `mockgen` 生成 repo mock + `RegisterXxx` 注入，**不接 DB**。
- **新增迁移 append 到 `migrationList()` 末尾**，禁止改动既有迁移；DDL 优先原生 SQL，避免依赖 `AutoMigrate`。
- **关键流程必打日志**：用 `logger.Ctx(ctx)`，message 用 `package.Method:` 前缀小写，动态值走 `zap.Xxx(...)` 字段。
- **前端表单控件统一用 shadcn `@/components/ui/*`**，禁止新增原生 `<select>`。
- **前端新增可见文案必须走 i18n**：用 `react-i18next` 的 `t(...)` 和 `frontend/src/i18n/locales/{zh-CN,en}/common.json`，不要新增写死中文；`i18next/no-literal-string` 会拦 JSX 中的中文硬编码文案。不要引入旁路文本改写机制。Agent / 用户 / 终端 / markdown 等动态内容不要翻译。详见 [docs/frontend.md](docs/frontend.md)。

## Useful Commands

```bash
make install-deps     # pnpm install in frontend/
make dev              # Wails dev mode with frontend hot reload
make build            # Production Wails build for current platform
make build-windows    # Cross-build Windows, default windows/amd64
make run              # Build and launch production app
make install          # Install app bundle/binary for the current platform
make agentred         # Build local agentred binary
make agentred-linux   # Cross-build agentred for Linux
make generate         # Generate Wails frontend bindings
make test             # Backend race tests + frontend Vitest
make test-backend     # Go race tests excluding /frontend/
make test-frontend    # Wails generate + frontend Vitest
make test-cover       # Go coverage.out + coverage.html
make lint             # golangci-lint + frontend ESLint
make mock             # Regenerate mockgen outputs
make clean            # Remove build/bin, frontend/dist, and coverage files

# Focused tests
go test -race ./internal/service/chat_svc -run TestName
go test -race ./internal/repository/llm_provider_repo -run TestName
go test -race ./pkg/codex -run TestName
cd frontend && pnpm test -- path/to/file.test.tsx
```

> `go test ./...` 在本仓库会扫到 `frontend/node_modules` 里一个 Go 包；默认走 `make test-backend`（已显式排除 `/frontend/`）。
