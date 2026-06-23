# piagent 通用 MCP 注入（使能层）设计

日期：2026-06-11
状态：已与用户确认

## 1. 背景与目标

目前 `RunRequest.MCPServers` 这条「启动时注入 MCP 工具」的 seam 只有 claudecode / codex 消费（`CapMCPTools`）。piagent 没有声明该 cap，`Run()` 直接忽略 `RunRequest.MCPServers`，因此凡是门控在 `CapMCPTools` 上的能力，piagent agent 都被软降级排除：

- **群聊**：`group_svc.backendSupportsGroup` → `chat_svc.AgentBackendHasCapability(CapMCPTools)`；没有该 cap 的 backend 不能入群，前端「新建群聊」picker 也按 `supportsGroup` 过滤掉它。
- **组织管理工具**（见 `2026-06-11-agent-org-tool-design.md` §6/§134）：org MCP 同样「backend 无 `CapMCPTools` → 不注入，软降级」。piagent 的 CEO 即便开了 `org` 开关也用不上。

本 spec 的目标是给 piagent 接上这条**通用**使能层——让它和别的 backend 一样消费任意注入的 HTTP MCP server 并声明 `CapMCPTools`，从而群聊 / 组织工具 / 未来同 seam 的消费者一并解锁。

**根因**：pi CLI（`@mariozechner/pi-coding-agent`，本机 v0.73.0）刻意只内置 read/write/edit/bash 四个工具，**没有原生 MCP 支持**（`pi --help` 无 `--mcp-config`；dist bundle 内 `mcpServers`/`modelcontextprotocol` 零命中；作者公开说明 no built-in MCP）。给 pi 加工具的唯一途径是 **JS 扩展**（`--extension <path>`，loader 用 jiti 加载 default-export 的 `ExtensionFactory = (pi) => void`，`.js`/`.ts` 皆可，工厂内用 `defineTool` 注册工具、可读 `process.env`）。因此本方案在 pi 启动时挂一个 agentre 自带的极小扩展，把注入的 HTTP MCP server 翻译成 pi 的一等工具。

### 已锁定的决策

| 决策点 | 结论 |
| --- | --- |
| 桥接来源 | **agentre 自带极小桥**（`go:embed` 内嵌 JS）；不依赖第三方 `pi-mcp-adapter`，自包含、零用户配置、版本可控 |
| 本 spec 范围 | 只做使能层（piagent 消费 `RunRequest.MCPServers` + 声明 `CapMCPTools`）；验收走现有群聊 seam |
| transport 子集 | bridge 只实现 Streamable-HTTP 的 **JSON 响应**模式，对齐 agentre 自己的网关 server；不实现完整 SSE 流 |
| 远程（agentred） | 纳入范围：同一 runtime 代码两端通吃，bridge JS 编进 agentred 二进制 |
| 分层 | `pkg/piagent` 只加通用 `--extension` 支持（无 agentre 知识）；MCP 专属逻辑放在 runtime 子包 |

## 2. 总体架构

```
chat_svc / group_svc ──RunRequest.MCPServers──▶ piagent Runtime.Run
                                                    │ len(MCPServers) > 0
                                                    ▼
                       mcpbridge: RenderConfig(specs)→config.json + Materialize()→bridge.js
                                                    │ --extension <bridge.js>
                                                    │ env AGENTRE_PI_MCP_CONFIG=<config.json>
                                                    ▼
                                  pkg/piagent Client（spawn pi --mode rpc ...）
                                                    │ jiti 加载 bridge.js（default export factory）
                                                    ▼
        bridge.js: 对每个 server  initialize → tools/list → 每个 tool 用 defineTool 注册到 pi
                                                    │ 模型调用该工具
                                                    ▼
              POST tools/call（Authorization: Bearer <token>）──▶ 网关 /mcp/<server>/
                                                    例：group_send / org_create_department
```

数据流与 claudecode 的 `--mcp-config` 完全等价，只是把「CLI 原生 MCP 客户端」换成「pi 扩展里的极小 MCP 客户端」。注入的 `MCPServerSpec`（`Name` / `URL` / `Headers`，http transport）由上游 chat_svc / group_svc 不变地产出。

## 3. 组件与边界

每个单元单一职责、可独立测试：

### 3.1 `pkg/piagent`（通用 pi SDK，保持无 agentre 知识）

- 新增通用选项 `WithExtension(path string)`：在 `buildRPCArgs`（`types.go`）里 append `--extension <path>`（可多次）。
- env 注入复用现有 `WithEnv`（`options.go`）。
- 该包**不认识 MCP**，只暴露「能加载一个扩展 + 透传 env」这两个通用能力。这样 `pkg/piagent` 仍是一个干净的通用 pi 封装。

### 3.2 `internal/pkg/agentruntime/runtimes/piagent/mcpbridge`（新子包，agentre 专属）

三个纯职责函数 / 方法，独立可测：

- **内嵌 bridge.js**：`//go:embed bridge.js`。
- `Materialize() (path string, err error)`：按内容哈希写到 `<AppDataDir>/piagent/ext/agentre-mcp-bridge-<hash>.js`，幂等（已存在同哈希文件则跳过写入）、版本隔离（升级 bridge 即换文件名，不会读到旧版）。
- `RenderConfig(servers []agentruntime.MCPServerSpec) (configPath string, err error)`：把 server 列表渲染成 bridge 读的 JSON，写到**会话私有路径**（如 `<AppDataDir>/piagent/ext/cfg/<sessionID>.json`），用完即弃；**绝不写 `~/.config/mcp/mcp.json` 等共享路径**，避免污染用户全局配置。
- 仅在 `len(req.MCPServers) > 0` 时被 runtime 调用。

config JSON 形态（草案，实现时定稿）：

```json
{
  "servers": [
    {
      "name": "group",
      "url": "http://127.0.0.1:52xxx/mcp/group/",
      "headers": { "Authorization": "Bearer <token>" }
    }
  ]
}
```

### 3.3 `bridge.js`（内嵌 pi 扩展，约 80–120 行）

```js
export default async (pi) => {
  const cfg = JSON.parse(await readFile(process.env.AGENTRE_PI_MCP_CONFIG))
  for (const server of cfg.servers) {
    await rpc(server, "initialize", { protocolVersion: "2025-06-18" })
    await rpc(server, "notifications/initialized")        // 通知，无 id
    const { tools } = await rpc(server, "tools/list", {})
    for (const t of tools) {
      pi.addTool(defineTool({
        name: t.name,                                     // 见 §4 命名
        description: t.description,
        parameters: t.inputSchema,
        execute: async (args) => {
          const res = await rpc(server, "tools/call", { name: t.name, arguments: args })
          return textContentOf(res)                       // 把 content[].text 拼回给 pi
        },
      }))
    }
  }
}
```

- `rpc()` = 对 `server.url` 发 JSON-RPC over HTTP POST，带 `server.headers`，解析 `application/json` 响应（**仅 JSON 模式**；若响应是 `text/event-stream` 则解析其中单条 message，完整多事件 SSE 流不在范围）。
- 工具 `execute` 的 fetch **不设短超时 / 或设成足够长**：org 写工具走服务端审批（`agent-org-tool-design.md` §5）会阻塞最长 5 分钟。pi 没有 claude-code 那种内置 MCP tool 超时（§5.4 的坑对 pi 不存在），阻塞时长完全由本 bridge 控制——这反而让审批流在 piagent 上更省心。
- pi 默认启用扩展工具（piagent 启动未传 `--no-tools`），注册即可用，无需 allowedTools 概念。

### 3.4 `runtimes/piagent/runtime.go`

- `Capabilities()` 增加 `CapMCPTools: true`。`CapMCPTools` 无子接口，无需实现额外接口。
- `Run()`：当 `len(req.MCPServers) > 0` 时调 `mcpbridge.Materialize()` + `RenderConfig(req.MCPServers)`，把扩展路径经 `pkg/piagent` 的 `WithExtension` 传下去、config 路径经 `WithEnv(AGENTRE_PI_MCP_CONFIG=...)` 传下去（具体在 `sessionFactory` 装配 Client 选项处）。
- `req.MCPServers` 为空时：一字节不变，单聊路径与今天字节等价（回归保护）。

## 4. 工具命名与提示对齐

- bridge 默认用**裸 MCP 工具名**给 pi 工具命名（如 `group_send` / `org_create_department`）——不与 pi 内建 read/write/edit/bash/ls/grep/find 冲突。
- 但必须与编排在 `SystemPromptSuffix` / 工具说明里引用的工具名一致。实现时核对：群聊 `group_svc` 的 system prompt 后缀引用的是 `group_send` 还是 `mcp__group__group_send` 前缀形式；org 工具同理。若上游用前缀形式，bridge 改为 `mcp__<server>__<tool>` 命名（与 claudecode 对齐）。**这是实现计划里的一个核对项**，避免「工具存在但模型按错名字调用」。

## 5. 能力门控与前端

- piagent 声明 `CapMCPTools` 后，`chat_svc` 自动算出该 backend `SupportsGroup = caps.Has(CapMCPTools)`，前端群聊 picker **自动**纳入 piagent；org 工具注入门控同样自动放行。**无需改前端代码**，也无新 wails 字段（不触发 `make generate`）。
- 这正是 capability 抽象的目的：新增 cap 的消费端已存在，使能端补齐即可。

## 6. 远程执行（agentred）

- `CapMCPTools` 本就跨 wire（claudecode/codex 远程在用，`remote.Runtime` 已转发 `RunRequest.MCPServers`）。
- bridge.js 由 `go:embed` 编进二进制，agentred 侧 `Materialize()` 写到 daemon 自己的数据目录；node 由 daemon 主机上的 pi 自带。
- 同一份 runtime 代码两端通吃，**远程基本零额外成本**。

## 7. 错误处理

- bridge 连不上某 server 或 `initialize`/`tools/list` 失败：记日志、**跳过该 server**（不让整轮 pi 起不来），其余 server 照常注册。
- `tools/call` 返回 JSON-RPC error 或非 2xx：作为**工具错误结果**回给模型（让它知道失败原因并自行纠正），不让扩展抛异常 crash。
- `mcpbridge.Materialize` / `RenderConfig` 失败：`Run` 返回 error（与现有 `BuildPiAgentEnv` 失败同一路径），本轮不启动。
- 关键流程按仓库约定记日志：`logger.Ctx(ctx)`，消息用小写 `package.Method:` 前缀，动态值走 `zap.Xxx`。

## 8. 测试（严格 TDD，Red → Green → Refactor）

- **mcpbridge 纯函数**：`RenderConfig` 给定 specs → 断言 config JSON 结构（含 headers/token）；`Materialize` 幂等 + 内容哈希路径稳定；空 server 列表的边界。
- **Capabilities 矩阵**：`runtime_test.go::TestPiAgentCapabilities` 断言 `CapMCPTools=true`（`capability.go` 常量 + 矩阵断言三处同步，见 agent-backend.md）。
- **runtime 行为**：`req.MCPServers` 非空 → 断言传给 fake `pkg/piagent` Client 的选项里带了 `--extension` + `AGENTRE_PI_MCP_CONFIG` env；为空 → 选项与今天字节等价。
- **bridge.js 单测**：用 node 自带 `node:test` 起一个 fake JSON-RPC HTTP server，断言 `initialize`/`tools/list`/`tools/call` 往返、Bearer header 透传、tool error 回传、单 server 失败时其余仍注册（测试目标挂载方式实现时定）。
- **远程 round-trip**：`RunRequest.MCPServers` 经 wire 编解码到 daemon 侧 piagent runtime（复用现有 wire 测试框架，确认 piagent 走 remote 时 specs 不丢）。
- **验收（范围内）**：现有 e2e 群聊 harness（fake runtime 已会做 group_send MCP HTTP 客户端）——把成员 backend 换成 piagent，断言它能 `group_send` 把可见气泡冒回群、DB 孪生 `agentGroupMessageCount` 增长（对齐既有 group-chat spec；注意 `AGENTRE_PROXY_PORT=0` 绑临时端口的坑）。

## 9. 不在本期范围

- 组织管理 MCP server 本身（`2026-06-11-agent-org-tool-design.md` 已独立覆盖；本 spec 只解锁它在 piagent 上的注入门控）。
- 非 agentre 的任意第三方 **SSE-only** MCP server 的完整流式（bridge 只支持 JSON 响应子集）。
- pi 的 tool-approval / `can_use_tool` 协议（pi 无该协议；MCP 工具默认放行，与 piagent 现状一致；org 写操作的审批在服务端，不依赖 CLI 层）。
- pi `pi install` 生态 / 第三方 `pi-mcp-adapter` 路线（已否决）。

## 10. 实施切分建议（供 writing-plans 参考）

1. `pkg/piagent`：`WithExtension` 通用选项 + `buildRPCArgs` 追加 `--extension`（含单测）。
2. `mcpbridge` 子包：`RenderConfig` + `Materialize` + 内嵌 `bridge.js` 占位（纯函数先红后绿）。
3. `bridge.js`：JSON-RPC HTTP 客户端 + `defineTool` 注册 + node:test。
4. `runtime.go`：声明 `CapMCPTools` + `Run` 装配（非空注入 / 空字节等价两条测试）。
5. 工具命名核对（§4）：读群聊/org 的 system prompt 后缀定裸名 vs 前缀。
6. 远程 round-trip 测试 + e2e 群聊验收（piagent 成员）。
