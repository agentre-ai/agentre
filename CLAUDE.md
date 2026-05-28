# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Agentre — Wails v2 desktop app (Go 1.26 + React 19 + TS) for orchestrating multiple AI coding agents (Claude Code / Codex / built-in) across projects, sessions, and remote machines. IPC only via Wails bindings, no HTTP API. Go module: `agentre` (no VCS prefix). User-facing concepts (Agent / Department / Session / Project / Issue / Hook) and the remote-chat pairing flow are described in `README.md`.

Two binaries ship from this repo:
- **`agentre`** (root `main.go`) — the desktop app. Also doubles as a CLI shim: `agentre claudecode …` short-circuits to `internal/cli/claudecodecmd` before booting wails/cago (used by Claude Code hook child processes).
- **`agentred`** (`cmd/agentred/`) — headless daemon that executes claude-code / codex subprocesses on behalf of a paired desktop over JSON-RPC-over-WebSocket on the LAN. Daemon-side handlers live in `internal/daemon/`.

## 高优先级约束（强制，不可绕过）

下面三条是硬性规则。如果当前任务和它们冲突，**先停下来问用户**，不要自作主张绕开。

1. **严格 TDD / BDD：Red → Green → Refactor，无例外。**
   - 新功能先写 BDD-style behavior spec（`Given … When … Then …` 或 goconvey `Convey("when X, then Y")`），覆盖 happy path + 至少一个 boundary/error case，**再写实现**。
   - 没有失败测试不写实现代码。展开细节见 [docs/development.md](docs/development.md)。
2. **修 bug 前先验证 bug 存在。**
   - 写一个 regression 测试复现失败，**跑一遍并看到它失败**（且是因为正确的原因失败），再动手 patch。
   - 如果 bug 实在无法在测试里复现，**显式告诉用户**这一点，然后再讨论 patch 方案。不要默默地"应该是这样修"就改代码。
3. **不要改动和当前任务无关的文件。**
   - Diff 只动 producer + 它的测试，最多再加一个明显就在 scope 内的 drift。
   - **禁止** drive-by refactor / 重命名 sweep / formatter pass / 死代码清理 / import 重排序 / 不相关 test churn —— 它们会埋掉真正的改动，破坏 `git bisect`。
   - 看到不相关的脏数据时，flag 给用户问一下，**不要顺手改**。

## 开发规范（必读）

写代码前先看这三份文档，规则在里面：

- [docs/architecture.md](docs/architecture.md) — 项目布局、cago 分层约定（entity / repo / service / wails 绑定）、远端 chat 架构、存储路径、数据库与迁移、生成文件。
- [docs/development.md](docs/development.md) — TDD/BDD 工作流、SOLID、Fix Discipline、测试栈（sqlmock / mockgen / goconvey）、日志规范、linter 例外。
- [docs/frontend.md](docs/frontend.md) — shadcn UI 强制约定、pnpm、格式化 / lint、commit 风格、模块路径。
- [docs/debugging.md](docs/debugging.md) — 读 SQLite / 查日志 / 复现线上 bug 的命令清单。
- [docs/agent-backend.md](docs/agent-backend.md) — 接入新 AI Agent backend 的路径：entity / migration / runtime / translator / capability / daemon import / 前端 gating，含 TDD 测试清单与常见坑。

> 详见 cago skill (`/cago`) —— 完整的 controller / service / repo / cron / queue 单测样例。

## Common Commands

```bash
make dev              # wails dev — hot reload
make build            # wails build with version/commit ldflags (current platform)
make build-windows    # cross-build windows/amd64 (override via WINDOWS_PLATFORM=)
make generate         # wails generate module — refresh frontend/wailsjs/ bindings
make test             # backend race tests + frontend Vitest (runs `generate` first)
make test-cover       # coverage.out + coverage.html
make lint / lint-fix  # golangci-lint + frontend ESLint (runs `generate` first)
make check            # lint + test
make mock             # go generate ./... (go.uber.org/mock)
make install-deps     # pnpm install in frontend/
make install          # build + install app bundle (macOS: /Applications/Agentre.app)
make clean            # rm build/bin frontend/dist coverage.*

# agentred daemon (remote execution box)
make agentred                # build local-platform binary → build/bin/agentred
make agentred-linux          # cross-build linux/amd64 (override via AGENTRED_GOOS/ARCH)
make agentred-deploy         # build linux + opsctl-cp + install (AGENTRED_TARGET= host)

# Focused tests
go test -race -run TestName ./internal/service/chat_svc/...
cd frontend && pnpm test -- path/to/file.test.tsx
cd frontend && pnpm install                  # pnpm is source of truth, not npm
```
