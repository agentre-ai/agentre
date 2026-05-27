// Package claudecode 是 agentre 自维护的 Anthropic Claude Code CLI 子进程封装。
//
// 范围：spawn `claude` 二进制（默认从 PATH），通过 stream-json input/output 与之
// 通信，把 stdout 的帧解码为 Event 暴露给上层。它承载 agentre 实际用到的 CLI
// 能力：
//
//   - 一次性 Stream / Text probe
//   - 常驻 OpenSession / Turn 多轮会话
//   - 新建 / resume / fork session
//   - 文本 / thinking / tool_use / tool_result / usage / api_retry 事件
//   - control_request / control_response 权限交互
//   - Interrupt 与运行时 SetPermissionMode
//   - session JSONL 读取与 user anchor 反查
//   - 错误识别（resume 不存在、二进制缺失、子进程退出）
//
// 不包含完整 MCP bridge；应用层 steer / hook 编排由
// internal/pkg/agentruntime/runtimes/claudecode 与 httpgateway 承担。
//
// 协议表面跟随已验证的 Claude Code stream-json 帧形状；未知字段由 JSON 解码
// 忽略，需要消费的新事件类型应在 stream/session parser 和 fake trace 测试中显式补齐。
package claudecode
