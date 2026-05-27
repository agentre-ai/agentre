# pkg/claudecode

agentre 自维护的 Anthropic Claude Code CLI 子进程封装。

## 范围

- spawn `claude` 二进制
- stream-json input/output 协议
- 一次性 `Stream` / `Text` probe
- 常驻 `OpenSession` / `Turn` 多轮会话
- session start / resume / fork (`--fork-session` + `--resume-session-at`)
- 文本 / thinking / tool_use / tool_result / usage / api_retry 事件
- `control_request` / `control_response` 权限交互
- `Interrupt` 与运行时 `SetPermissionMode`
- session JSONL 读取与 user anchor 反查
- 错误识别：resume 不存在、二进制缺失、子进程退出

## 不在范围

- 完整 MCP bridge
- 应用层 steer / hook 编排
  - runtime 集成见 `internal/pkg/agentruntime/runtimes/claudecode`
  - headless 模式 steer 调研与 PreToolUse hook 软 steer 方案见 `docs/superpowers/specs/2026-05-18-claudecode-headless-steer-design.md`

## 怎么扩 flag

1. `args.go` 加 `cliFlag` 常量与 `runSpec` 字段。
2. `runoptions.go` 暴露 `WithXxx` 形态的 `RunOption`。
3. `args_test.go` 加测试，按"出现且参数正确"断言。
4. 协议升级时补充对应 fake trace 或真实帧回放测试。
