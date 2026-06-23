# piagent 通用 MCP 注入（使能层）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 piagent runtime 消费 `RunRequest.MCPServers`（声明 `CapMCPTools`），通过一个 agentre 自带的极小 pi 扩展把注入的 HTTP MCP server 翻成 pi 一等工具，从而解锁群聊 / 组织管理工具在 piagent 上的门控。

**Architecture:** pi CLI 无原生 MCP，给 pi 加工具只能走 JS 扩展（`--extension`，jiti 加载 default-export `(pi)=>void` 工厂，工厂内 `pi.registerTool(...)`）。piagent runtime 在 `len(req.MCPServers)>0` 时：用 `go:embed` 把一个极小 bridge 扩展 materialize 到数据目录、把 server 列表渲染成 JSON 配置、把扩展路径与配置路径分别经 `--extension` / `AGENTRE_PI_MCP_CONFIG` env 传给 pi。bridge 对每个 server 做 `initialize`/`tools/list`/`tools/call`（JSON-RPC over HTTP POST，带 Bearer header）。

**Tech Stack:** Go 1.26、`pkg/piagent`（pi RPC 封装）、`internal/pkg/agentruntime`（runtime/capability/MCPServerSpec）、`go:embed`、Node ESM 扩展（`typebox` 可选）、goconvey、node:test。

参考 spec：`docs/superpowers/specs/2026-06-11-piagent-mcp-injection-design.md`。第二消费者（组织工具）见 `docs/superpowers/specs/2026-06-11-agent-org-tool-design.md`。

---

## File Structure

| 文件 | 职责 | 动作 |
| --- | --- | --- |
| `pkg/piagent/client.go` | Client 增 `extensions []string` 字段 | Modify |
| `pkg/piagent/options.go` | 新增通用 `WithExtension(path)` | Modify |
| `pkg/piagent/types.go` | `buildRPCArgs` 追加 `--extension` | Modify |
| `pkg/piagent/types_test.go` | `buildRPCArgs` 含 extension 的断言 | Modify |
| `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs` | 内嵌 pi 扩展：HTTP-MCP → pi 工具 | Create |
| `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.go` | `Materialize()` + `RenderConfig()` + `//go:embed` | Create |
| `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge_test.go` | 上两者的 Go 单测 | Create |
| `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs` | bridge.mjs 的 node:test | Create |
| `internal/pkg/agentruntime/runtimes/piagent/runtime.go` | `Capabilities()` 加 `CapMCPTools` | Modify |
| `internal/pkg/agentruntime/runtimes/piagent/session.go` | `sessionFactory` 注入扩展+config env | Modify |
| `internal/pkg/agentruntime/runtimes/piagent/runtime_test.go` | 矩阵断言改为 `CapMCPTools=true` | Modify |
| `internal/pkg/agentruntime/runtimes/piagent/session_mcp_test.go` | sessionFactory 注入的副作用断言 | Create |

**已锁定的关键事实（实现时直接用，不要再猜）：**

- `MCPServerSpec{ Name string; URL string; Headers map[string]string; Tools []string }`（`internal/pkg/agentruntime/runner.go:272`）。`Tools` 是**裸工具名**（群聊为 `["group_send"]`，见 `group_svc/group.go:527`）。
- 群聊 system prompt 后缀让 agent 调 **裸名 `group_send`**（`group_svc/group.go:551`）。因此 bridge **按裸名注册** pi 工具，并按 `spec.Tools` 过滤。
- pi 扩展契约：default-export `async (pi) => {}`；`pi.registerTool({ name, label, description, parameters, execute })`（plain object 即可，`defineTool` 仅为 TS 推断，运行时不必）。
- `execute(toolCallId, params, signal, onUpdate, ctx)` 返回 `Promise<AgentToolResult>`；`AgentToolResult = { content: (TextContent|ImageContent)[]; details: any; terminate?: boolean }`；**失败要 throw**（不要把错误编进 content）。MCP `tools/call` 的 `result.content`（`[{type:"text",text:"..."}]`）形态与 pi 一致，可直接透传。
- `parameters` 期望 TypeBox `TSchema`（包名是裸 `typebox`）。是否能直接塞裸 JSON Schema 由 Task 0 spike 决定；bridge 用「动态 import typebox + 裸 schema 兜底」，两条路都能跑。
- `paths.AppDataDir()` 尊重 `AGENTRE_DATA_DIR` 环境变量（`internal/pkg/paths/paths.go:42`），测试用 `t.Setenv("AGENTRE_DATA_DIR", t.TempDir())` 隔离。
- piagent runtime 的注入缝是 `session.go` 的包级变量 `sessionFactory`（测试可用 `SetSessionFactoryForTest` 替换）。本计划把 MCP 注入逻辑放进**真实** `sessionFactory`，并以「写到磁盘的扩展/配置文件」作为可断言副作用（不替换 sessionFactory）。

---

## Task 0: Spike — 对真实 pi 验证扩展桥机制

**目的**：在写基础设施前，用真实 pi 跑通一个最小桥，定死三件不确定的事，结论写进 Task 3 的代码。**这是探索性 spike，产物用完即删，不进 git。**

要定死：
1. `pi.registerTool` 的 `parameters` 是否接受**裸 JSON Schema**（不依赖 typebox）？还是必须 `Type.Unsafe(schema)`？
2. `typebox` 能否从一个**位于数据目录（非 pi node_modules 内）**的 `.mjs` 扩展里 `import` 到？
3. pi 能否 `--extension` 加载一个 `.mjs`（ESM）文件？`execute` 返回 `{content:[{type:"text",text}]}` 能否正确回流给模型？

**Files:**
- Create（临时，跑完删）：`/tmp/pi-spike/bridge.mjs`、`/tmp/pi-spike/fake-mcp.mjs`

- [ ] **Step 1: 写一个 fake MCP server（JSON-RPC over HTTP POST）**

`/tmp/pi-spike/fake-mcp.mjs`：

```js
import { createServer } from "node:http";
const srv = createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    const rpc = JSON.parse(body || "{}");
    const reply = (result) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result })); };
    if (rpc.method === "initialize") return reply({ protocolVersion: "2025-06-18", serverInfo: { name: "fake", version: "1" }, capabilities: { tools: {} } });
    if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
    if (rpc.method === "tools/list") return reply({ tools: [{ name: "spike_echo", description: "echo back the text", inputSchema: { type: "object", required: ["text"], properties: { text: { type: "string" } } } }] });
    if (rpc.method === "tools/call") { console.error("[fake] tools/call auth=", req.headers.authorization, "args=", JSON.stringify(rpc.params.arguments)); return reply({ content: [{ type: "text", text: "echo:" + (rpc.params.arguments?.text ?? "") }] }); }
    res.statusCode = 404; res.end();
  });
});
srv.listen(52999, () => console.error("[fake] listening :52999"));
```

- [ ] **Step 2: 写最小 bridge 扩展（先试裸 schema，不导 typebox）**

`/tmp/pi-spike/bridge.mjs`：

```js
export default async function (pi) {
  const url = "http://127.0.0.1:52999/";
  const rpc = async (method, params) => {
    const r = await fetch(url, { method: "POST", headers: { "content-type": "application/json", authorization: "Bearer spike-token" }, body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }) });
    const t = await r.text();
    return t ? JSON.parse(t).result : {};
  };
  await rpc("initialize", { protocolVersion: "2025-06-18" });
  const { tools } = await rpc("tools/list", {});
  for (const tool of tools) {
    pi.registerTool({
      name: tool.name,
      label: tool.name,
      description: tool.description,
      parameters: tool.inputSchema,               // ← 先试裸 JSON Schema
      execute: async (_id, params) => {
        const res = await rpc("tools/call", { name: tool.name, arguments: params });
        return { content: res.content, details: res };
      },
    });
  }
  console.error("[bridge] registered:", tools.map((t) => t.name).join(","));
}
```

- [ ] **Step 3: 跑通端到端**

```bash
cd /tmp/pi-spike
node fake-mcp.mjs &           # 后台起 fake server
pi --extension /tmp/pi-spike/bridge.mjs -p "调用 spike_echo 工具，text 传 hello，然后把结果原样告诉我"
```

预期：fake server 打印 `[fake] tools/call auth= Bearer spike-token args= {"text":"hello"}`；pi 最终回复包含 `echo:hello`。

- [ ] **Step 4: 记录结论 + 跑兜底分支**

- 若 Step 3 成功 → **裸 JSON Schema 可用**，Task 3 的 `toParameters` 优先返回裸 schema，typebox 仅作兜底。
- 若 pi 报 schema 校验类错误 → 改 `parameters: ` 为下行后重跑：

  ```js
  import { Type } from "typebox";
  // ...
  parameters: Type.Unsafe(tool.inputSchema),
  ```

  - 能 `import { Type } from "typebox"` 且通过 → Task 3 用 typebox 包裹（确认 typebox 可从数据目录扩展解析）。
  - import 失败（`Cannot find package 'typebox'`）→ 记下：bridge 必须改用 pi 包内的 re-export，或在 `RenderConfig` 侧把 schema 预转。把实际错误贴进 Task 3 备注。

- [ ] **Step 5: 清理**

```bash
kill %1 2>/dev/null; rm -rf /tmp/pi-spike
```

无 commit（spike 不进 git）。把 Step 4 的结论写在执行记录里，供 Task 3 选定 `toParameters` 实现。

---

## Task 1: `pkg/piagent` 新增通用 `WithExtension`

**Files:**
- Modify: `pkg/piagent/client.go`（`Client` 加字段）
- Modify: `pkg/piagent/options.go`（新 Option）
- Modify: `pkg/piagent/types.go:97`（`buildRPCArgs` 追加 `--extension`）
- Test: `pkg/piagent/types_test.go`

- [ ] **Step 1: 写失败测试**

在 `pkg/piagent/types_test.go` 追加：

```go
func TestBuildRPCArgs_Extensions(t *testing.T) {
	c := New(WithExtension("/a/x.mjs"), WithExtension("/a/y.mjs"))
	args := buildRPCArgs(c)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--extension /a/x.mjs") || !strings.Contains(joined, "--extension /a/y.mjs") {
		t.Fatalf("expected both --extension flags, got: %q", joined)
	}
}

func TestBuildRPCArgs_NoExtensionByDefault(t *testing.T) {
	c := New()
	for _, a := range buildRPCArgs(c) {
		if a == "--extension" {
			t.Fatalf("did not expect --extension by default: %v", buildRPCArgs(c))
		}
	}
}
```

（`types_test.go` 已 `import "strings"`；若无则补。）

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./pkg/piagent/ -run TestBuildRPCArgs_Extensions -v`
Expected: 编译失败 `undefined: WithExtension`。

- [ ] **Step 3: 加字段**

`pkg/piagent/client.go`，在 `Client` struct 内 `runner processRunner` 上一行加：

```go
	// extensions 透传给 pi 的 --extension（可多次）。Agentre 用它加载内嵌的
	// MCP 桥扩展，把注入的 HTTP MCP server 翻成 pi 一等工具。
	extensions []string
```

- [ ] **Step 4: 加 Option**

`pkg/piagent/options.go`，在 `WithKillGrace` 上方加：

```go
// WithExtension 透传一个 pi 扩展文件路径（--extension <path>），可多次调用。
func WithExtension(path string) Option {
	return func(c *Client) {
		if p := strings.TrimSpace(path); p != "" {
			c.extensions = append(c.extensions, p)
		}
	}
}
```

`options.go` 顶部 import 加 `"strings"`（当前只 import `"time"`）：

```go
import (
	"strings"
	"time"
)
```

- [ ] **Step 5: `buildRPCArgs` 追加 flag**

`pkg/piagent/types.go`，在 `buildRPCArgs` 里 `--thinking` 段之后、`return args` 之前插入：

```go
	for _, ext := range c.extensions {
		if e := strings.TrimSpace(ext); e != "" {
			args = append(args, "--extension", e)
		}
	}
```

- [ ] **Step 6: 跑测试看它通过**

Run: `go test ./pkg/piagent/ -run TestBuildRPCArgs -v`
Expected: PASS（两个用例）。

- [ ] **Step 7: 全包回归 + 提交**

```bash
go test -race ./pkg/piagent/...
git add pkg/piagent/client.go pkg/piagent/options.go pkg/piagent/types.go pkg/piagent/types_test.go
git commit -m "✨ piagent: WithExtension 通用选项透传 --extension"
```

---

## Task 2: `mcpbridge` 包 — `RenderConfig` + `Materialize`（含内嵌占位 bridge.mjs）

**Files:**
- Create: `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs`（先占位，Task 3 填实现）
- Create: `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.go`
- Test: `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge_test.go`

- [ ] **Step 1: 占位 bridge.mjs（让 go:embed 有文件可嵌）**

`internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs`：

```js
// 占位：真实实现见 Task 3。
export default async function (_pi) {}
```

- [ ] **Step 2: 写失败测试**

`internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge_test.go`：

```go
package mcpbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

func TestRenderConfig_WritesServerListWithHeadersAndTools(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	specs := []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     "http://127.0.0.1:52401/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer tok"},
		Tools:   []string{"group_send"},
	}}
	path, err := RenderConfig(specs, 42)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Servers []struct {
			Name    string            `json:"name"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Tools   []string          `json:"tools"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "group" || cfg.Servers[0].Headers["Authorization"] != "Bearer tok" || cfg.Servers[0].Tools[0] != "group_send" {
		t.Fatalf("unexpected config: %s", raw)
	}
	if !strings.Contains(path, filepath.Join("piagent", "ext", "cfg")) {
		t.Fatalf("config path not under piagent/ext/cfg: %s", path)
	}
}

func TestMaterialize_IdempotentHashedPath(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	p1, err := Materialize()
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatalf("bridge file missing: %v", err)
	}
	p2, err := Materialize()
	if err != nil || p2 != p1 {
		t.Fatalf("Materialize not idempotent: p1=%s p2=%s err=%v", p1, p2, err)
	}
	if !strings.HasSuffix(p1, ".mjs") || !strings.Contains(filepath.Base(p1), "agentre-mcp-bridge-") {
		t.Fatalf("unexpected bridge path: %s", p1)
	}
}
```

- [ ] **Step 3: 跑测试看它失败**

Run: `go test ./internal/pkg/agentruntime/runtimes/piagent/mcpbridge/ -v`
Expected: 编译失败 `undefined: RenderConfig` / `undefined: Materialize`。

- [ ] **Step 4: 写实现**

`internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.go`：

```go
// Package mcpbridge 把注入给 piagent 的 HTTP MCP server 转成一个 pi 扩展可读的
// 配置 + 一份内嵌的桥接扩展（bridge.mjs）。pi 无原生 MCP，只能用 JS 扩展加工具。
package mcpbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

// ConfigEnvVar 是 bridge.mjs 读取配置文件路径的环境变量名。
const ConfigEnvVar = "AGENTRE_PI_MCP_CONFIG"

//go:embed bridge.mjs
var bridgeSource []byte

type bridgeServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Tools   []string          `json:"tools,omitempty"`
}

type bridgeConfig struct {
	Servers []bridgeServer `json:"servers"`
}

func extDir() (string, error) {
	root, err := paths.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "piagent", "ext"), nil
}

// Materialize 把内嵌的 bridge.mjs 写到 <AppDataDir>/piagent/ext/，文件名带内容哈希
// （版本隔离 + 幂等：同哈希已存在则不重写），返回绝对路径。
func Materialize() (string, error) {
	dir, err := extDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bridgeSource)
	name := fmt.Sprintf("agentre-mcp-bridge-%s.mjs", hex.EncodeToString(sum[:])[:16])
	path := filepath.Join(dir, name)
	if _, statErr := os.Stat(path); statErr == nil {
		return path, nil
	}
	if err := os.WriteFile(path, bridgeSource, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RenderConfig 把注入的 MCPServerSpec 列表渲染成 bridge.mjs 读的 JSON，写到会话私有
// 路径 <AppDataDir>/piagent/ext/cfg/<sessionID>.json，返回绝对路径。绝不写用户全局
// MCP 配置目录。
func RenderConfig(specs []agentruntime.MCPServerSpec, sessionID int64) (string, error) {
	dir, err := extDir()
	if err != nil {
		return "", err
	}
	cfgDir := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return "", err
	}
	cfg := bridgeConfig{Servers: make([]bridgeServer, 0, len(specs))}
	for _, s := range specs {
		cfg.Servers = append(cfg.Servers, bridgeServer{Name: s.Name, URL: s.URL, Headers: s.Headers, Tools: s.Tools})
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfgDir, fmt.Sprintf("%d.json", sessionID))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
```

注意：`bridge.go` 顶部 import 块要加 `_ "embed"` 不行——这里直接用 `//go:embed`，需 `import "embed"` 吗？`//go:embed` 到 `[]byte` 变量需要在文件里 **匿名 import** `embed`：在 import 块加一行 `_ "embed"`。把上面 import 块改成包含：

```go
	_ "embed"
```

（放在 import 块内任意位置即可，gofmt 会排序。）

- [ ] **Step 5: 跑测试看它通过**

Run: `go test -race ./internal/pkg/agentruntime/runtimes/piagent/mcpbridge/ -v`
Expected: PASS（两个用例）。

- [ ] **Step 6: 提交**

```bash
git add internal/pkg/agentruntime/runtimes/piagent/mcpbridge/
git commit -m "✨ piagent/mcpbridge: 内嵌桥扩展 Materialize + MCPServers→config RenderConfig"
```

---

## Task 3: `bridge.mjs` 实现 + node:test

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs`（替换占位）
- Create: `internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs`

> 用 Task 0 的结论选定 `toParameters`：默认裸 schema + typebox 兜底（下方代码已是这种形态，spike 若发现必须 typebox 也无需改——动态 import 会命中）。

- [ ] **Step 1: 写 node:test（先失败）**

`internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs`：

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

function startFake() {
  const calls = [];
  const srv = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const rpc = JSON.parse(body || "{}");
      calls.push({ method: rpc.method, auth: req.headers.authorization, args: rpc.params?.arguments });
      const reply = (result) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result })); };
      if (rpc.method === "initialize") return reply({ protocolVersion: "2025-06-18" });
      if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
      if (rpc.method === "tools/list") return reply({ tools: [
        { name: "group_send", description: "send", inputSchema: { type: "object", required: ["body"], properties: { body: { type: "string" } } } },
        { name: "secret_tool", description: "nope", inputSchema: { type: "object" } },
      ] });
      if (rpc.method === "tools/call") return reply({ content: [{ type: "text", text: "sent" }] });
      res.statusCode = 404; res.end();
    });
  });
  return new Promise((resolve) => srv.listen(0, () => resolve({ srv, port: srv.address().port, calls })));
}

function fakePi() {
  const tools = [];
  return { tools, registerTool: (t) => tools.push(t) };
}

test("registers only allowed tools by bare name and proxies tools/call with auth", async () => {
  const { srv, port, calls } = await startFake();
  const cfgPath = join(mkdtempSync(join(tmpdir(), "pibridge-")), "cfg.json");
  writeFileSync(cfgPath, JSON.stringify({ servers: [{ name: "group", url: `http://127.0.0.1:${port}/`, headers: { Authorization: "Bearer tok" }, tools: ["group_send"] }] }));
  process.env.AGENTRE_PI_MCP_CONFIG = cfgPath;

  const mod = await import("./bridge.mjs");
  const pi = fakePi();
  await mod.default(pi);

  // 仅注册 allowlist 内的工具，按裸名。
  assert.deepEqual(pi.tools.map((t) => t.name), ["group_send"]);
  assert.equal(pi.tools[0].label, "group_send");

  // 调 execute → 命中 tools/call，带 Bearer header，回流 content。
  const result = await pi.tools[0].execute("call-1", { body: "hi" });
  assert.deepEqual(result.content, [{ type: "text", text: "sent" }]);
  const callToolCall = calls.find((c) => c.method === "tools/call");
  assert.equal(callToolCall.auth, "Bearer tok");
  assert.deepEqual(callToolCall.args, { body: "hi" });

  srv.close();
});

test("tool execute throws on rpc error (pi encodes as error result)", async () => {
  const srv = createServer((req, res) => {
    let body = ""; req.on("data", (c) => (body += c));
    req.on("end", () => {
      const rpc = JSON.parse(body || "{}");
      const reply = (o) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, ...o })); };
      if (rpc.method === "initialize") return reply({ result: {} });
      if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
      if (rpc.method === "tools/list") return reply({ result: { tools: [{ name: "group_send", description: "", inputSchema: { type: "object" } }] } });
      if (rpc.method === "tools/call") return reply({ error: { code: -32000, message: "forbidden" } });
      res.statusCode = 404; res.end();
    });
  });
  await new Promise((r) => srv.listen(0, r));
  const cfgPath = join(mkdtempSync(join(tmpdir(), "pibridge-")), "cfg.json");
  writeFileSync(cfgPath, JSON.stringify({ servers: [{ name: "group", url: `http://127.0.0.1:${srv.address().port}/`, headers: {}, tools: [] }] }));
  process.env.AGENTRE_PI_MCP_CONFIG = cfgPath;

  const mod = await import(`./bridge.mjs?e=${Date.now()}`);
  const pi = fakePi();
  await mod.default(pi);
  await assert.rejects(() => pi.tools[0].execute("c", {}), /forbidden/);
  srv.close();
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `node --test internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs`
Expected: FAIL（占位 bridge 不注册任何工具，`pi.tools` 为空 → 第一个断言失败）。

- [ ] **Step 3: 写实现，替换 bridge.mjs**

`internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs`：

```js
// agentre-mcp-bridge：把注入的 HTTP MCP server 翻成 pi 一等工具。
// 由 agentre piagent runtime 经 --extension 加载；server 列表经
// AGENTRE_PI_MCP_CONFIG 指向的 JSON 文件传入。pi 无原生 MCP（仅 read/write/edit/
// bash），加工具只能走扩展。仅实现 Streamable-HTTP 的 JSON 响应（对齐 agentre 自家
// 网关 server），不做完整多事件 SSE 流。
import { readFileSync } from "node:fs";

export default async function agentreMcpBridge(pi) {
  const cfgPath = process.env.AGENTRE_PI_MCP_CONFIG;
  if (!cfgPath) return;
  let cfg;
  try {
    cfg = JSON.parse(readFileSync(cfgPath, "utf8"));
  } catch (e) {
    console.error(`[agentre-mcp-bridge] read config failed: ${e}`);
    return;
  }
  const toParameters = await makeParamConverter();
  for (const server of cfg.servers ?? []) {
    try {
      await registerServer(pi, server, toParameters);
    } catch (e) {
      // 单个 server 失败只跳过它，不拖垮整轮 pi。
      console.error(`[agentre-mcp-bridge] server ${server?.name} skipped: ${e}`);
    }
  }
}

async function registerServer(pi, server, toParameters) {
  await rpc(server, "initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "agentre-pi-bridge", version: "1" } });
  await notify(server, "notifications/initialized", {});
  const listed = await rpc(server, "tools/list", {});
  const allow = new Set(server.tools ?? []);
  for (const t of listed.tools ?? []) {
    if (allow.size > 0 && !allow.has(t.name)) continue; // 按角色裁剪
    pi.registerTool({
      name: t.name,                 // 裸名，对齐 group system prompt 与 MCPServerSpec.Tools
      label: t.name,
      description: t.description ?? "",
      parameters: toParameters(t.inputSchema),
      execute: async (_toolCallId, params) => {
        const res = await rpc(server, "tools/call", { name: t.name, arguments: params ?? {} });
        return { content: normalizeContent(res.content), details: res };
      },
    });
  }
}

// makeParamConverter：pi 的 parameters 期望 TypeBox TSchema。优先用裸 JSON Schema
// （多数 pi 版本可直接消费）；能加载 typebox 时用 Type.Unsafe 包一层更稳。
async function makeParamConverter() {
  try {
    const { Type } = await import("typebox");
    return (schema) => Type.Unsafe(schema ?? { type: "object" });
  } catch {
    return (schema) => schema ?? { type: "object" };
  }
}

function normalizeContent(content) {
  if (Array.isArray(content) && content.length > 0) return content;
  return [{ type: "text", text: "" }];
}

let _id = 0;

async function rpc(server, method, params) {
  const res = await fetch(server.url, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json", ...(server.headers ?? {}) },
    body: JSON.stringify({ jsonrpc: "2.0", id: ++_id, method, params }),
  });
  if (!res.ok) throw new Error(`${method} HTTP ${res.status}`);
  const data = await parseBody(res);
  if (data && data.error) throw new Error(`${method}: ${data.error.message ?? "rpc error"}`);
  return (data && data.result) ?? {};
}

async function notify(server, method, params) {
  // 通知无 id、无需响应体。
  await fetch(server.url, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json", ...(server.headers ?? {}) },
    body: JSON.stringify({ jsonrpc: "2.0", method, params }),
  });
}

async function parseBody(res) {
  const ct = res.headers.get("content-type") ?? "";
  const text = await res.text();
  if (ct.includes("text/event-stream")) {
    for (const line of text.split("\n")) {
      const s = line.trim();
      if (s.startsWith("data:")) return JSON.parse(s.slice(5).trim());
    }
    return {};
  }
  return text ? JSON.parse(text) : {};
}
```

- [ ] **Step 4: 跑测试看它通过**

Run: `node --test internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs`
Expected: PASS（两个用例）。`makeParamConverter` 在 agentre 仓库下 `import("typebox")` 失败 → 命中兜底裸 schema，测试不依赖 typebox。

- [ ] **Step 5: Go embed 回归（确认嵌的是新内容）+ 提交**

```bash
go test -race ./internal/pkg/agentruntime/runtimes/piagent/mcpbridge/...
git add internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.mjs internal/pkg/agentruntime/runtimes/piagent/mcpbridge/bridge.test.mjs
git commit -m "✨ piagent/mcpbridge: bridge.mjs 实现(HTTP-MCP→pi tools)+ node:test"
```

---

## Task 4: runtime 声明 `CapMCPTools` + `sessionFactory` 注入

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/piagent/runtime.go:40-50`（`Capabilities`）
- Modify: `internal/pkg/agentruntime/runtimes/piagent/runtime_test.go:31`（矩阵断言）
- Modify: `internal/pkg/agentruntime/runtimes/piagent/session.go`（`sessionFactory` 注入）
- Create: `internal/pkg/agentruntime/runtimes/piagent/session_mcp_test.go`

- [ ] **Step 1: 改矩阵测试（先失败）**

`runtime_test.go` 第 30-31 行，把：

```go
		// CapMCPTools=false:pi-agent 不支持 RunRequest.MCPServers 注入。
		So(caps.Has(capability.CapMCPTools), ShouldBeFalse)
```

改为：

```go
		// CapMCPTools=true:pi-agent 经内嵌桥扩展消费 RunRequest.MCPServers。
		So(caps.Has(capability.CapMCPTools), ShouldBeTrue)
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/pkg/agentruntime/runtimes/piagent/ -run TestPiAgentCapabilities -v`
Expected: FAIL（`Should be true` 但当前为 false）。

- [ ] **Step 3: `Capabilities` 加 cap**

`runtime.go` 的 `Capabilities()`，在 `capability.CapReportContextWindow: true,` 下一行加：

```go
			capability.CapMCPTools: true,
```

- [ ] **Step 4: 跑矩阵测试看它通过**

Run: `go test ./internal/pkg/agentruntime/runtimes/piagent/ -run TestPiAgentCapabilities -v`
Expected: PASS。

- [ ] **Step 5: 写 sessionFactory 注入的失败测试**

`internal/pkg/agentruntime/runtimes/piagent/session_mcp_test.go`：

```go
package piagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

func minimalReq(sessionID int64, specs []agentruntime.MCPServerSpec) agentruntime.RunRequest {
	return agentruntime.RunRequest{
		SessionID:  sessionID,
		Backend:    &agent_backend_entity.AgentBackend{},
		MCPServers: specs,
	}
}

func TestSessionFactory_InjectsBridgeWhenMCPServersPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dir)
	specs := []agentruntime.MCPServerSpec{{Name: "group", URL: "http://127.0.0.1:1/mcp/group/", Headers: map[string]string{"Authorization": "Bearer t"}, Tools: []string{"group_send"}}}

	if _, err := sessionFactory(minimalReq(7, specs), map[string]string{}, t.TempDir()); err != nil {
		t.Fatalf("sessionFactory: %v", err)
	}

	// 副作用：桥扩展 + 会话私有 config 都已落盘。
	cfg := filepath.Join(dir, "piagent", "ext", "cfg", "7.json")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("expected config at %s: %v", cfg, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "piagent", "ext", "agentre-mcp-bridge-*.mjs"))
	if len(matches) == 0 {
		t.Fatalf("expected materialized bridge .mjs under %s", filepath.Join(dir, "piagent", "ext"))
	}
}

func TestSessionFactory_NoBridgeWhenNoMCPServers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dir)

	if _, err := sessionFactory(minimalReq(7, nil), map[string]string{}, t.TempDir()); err != nil {
		t.Fatalf("sessionFactory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "piagent", "ext")); !os.IsNotExist(err) {
		t.Fatalf("did not expect piagent/ext dir when no MCPServers, stat err=%v", err)
	}
}
```

- [ ] **Step 6: 跑测试看它失败**

Run: `go test ./internal/pkg/agentruntime/runtimes/piagent/ -run TestSessionFactory_ -v`
Expected: FAIL（当前 sessionFactory 不碰 MCPServers，config/bridge 不存在）。

- [ ] **Step 7: 在 sessionFactory 注入**

`session.go` 的 `sessionFactory`，把 `env` 在装配 opts **之前**补上 config 路径，并在有 MCPServers 时追加 `WithExtension`。改写 `sessionFactory` 函数体——在 `opts := []piagent.Option{...}` 之前插入：

```go
	// MCP 注入：有 RunRequest.MCPServers 时，materialize 内嵌桥扩展 + 渲染会话私有
	// config，扩展路径走 --extension、config 路径走 AGENTRE_PI_MCP_CONFIG env。
	var extPath string
	if len(req.MCPServers) > 0 {
		p, err := mcpbridge.Materialize()
		if err != nil {
			return nil, err
		}
		cfgPath, err := mcpbridge.RenderConfig(req.MCPServers, req.SessionID)
		if err != nil {
			return nil, err
		}
		extPath = p
		env = withEnv(env, mcpbridge.ConfigEnvVar, cfgPath)
	}
```

然后在 `opts := []piagent.Option{ ... }` 列表**之后**、`if sessionDir, derr := ...` **之前**加：

```go
	if extPath != "" {
		opts = append(opts, piagent.WithExtension(extPath))
	}
```

在文件末尾（`SetSessionFactoryForTest` 之后）加 helper：

```go
// withEnv 返回 env 的副本并设置一个键，避免就地改调用方的 map。
func withEnv(env map[string]string, key, val string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out[key] = val
	return out
}
```

`session.go` 顶部 import 块加：

```go
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent/mcpbridge"
```

- [ ] **Step 8: 跑测试看它通过**

Run: `go test -race ./internal/pkg/agentruntime/runtimes/piagent/ -run TestSessionFactory_ -v`
Expected: PASS（两个用例）。

- [ ] **Step 9: 包级全量回归 + 提交**

```bash
go test -race ./internal/pkg/agentruntime/runtimes/piagent/...
git add internal/pkg/agentruntime/runtimes/piagent/runtime.go internal/pkg/agentruntime/runtimes/piagent/runtime_test.go internal/pkg/agentruntime/runtimes/piagent/session.go internal/pkg/agentruntime/runtimes/piagent/session_mcp_test.go
git commit -m "✨ piagent: 声明 CapMCPTools + sessionFactory 注入桥扩展/config"
```

---

## Task 5: 远程（agentred）round-trip 确认

`CapMCPTools` 与 `RunRequest.MCPServers` 已是跨 wire 字段（claudecode/codex 在用），piagent 走 remote 时本应自动转发。本任务加一个针对性断言锁住「piagent 远程也带 MCPServers」，避免回归。

**Files:**
- 先定位现有 wire 测试：`internal/pkg/agentruntime/runtimes/remote/wire/wire_test.go`
- Modify（或在同目录新增 `*_test.go`）

- [ ] **Step 1: 确认 MCPServers 已在 RunRequest 的 wire 编解码里**

Run: `grep -rn "MCPServers" internal/pkg/agentruntime/runtimes/remote/`
Expected: 能看到 `RunRequest` 编解码已覆盖 `MCPServers`（claudecode 已用）。

- [ ] **Step 2: 若已覆盖 → 加一个最小 round-trip 断言**

在 `remote/wire/wire_test.go` 追加（按该文件既有 helper 风格；下示意 RunRequest 编解码对称）：

```go
func TestRunRequest_MCPServers_RoundTrip(t *testing.T) {
	in := agentruntime.RunRequest{
		SessionID:  9,
		MCPServers: []agentruntime.MCPServerSpec{{Name: "group", URL: "http://x/mcp/group/", Headers: map[string]string{"Authorization": "Bearer t"}, Tools: []string{"group_send"}}},
	}
	out := roundTripRunRequest(t, in) // 复用文件内既有 encode→decode helper；若无则按现有用例同款手写
	if len(out.MCPServers) != 1 || out.MCPServers[0].Name != "group" || out.MCPServers[0].Tools[0] != "group_send" {
		t.Fatalf("MCPServers not preserved across wire: %+v", out.MCPServers)
	}
}
```

> 若 `wire_test.go` 没有现成 `roundTripRunRequest`，照该文件里 RunRequest 已有用例的 encode/decode 调用方式照搬（不要新发明 API）。若 RunRequest 的 wire 路径本就透传整个 struct（无需逐字段 codec），本任务只保留 Step 1 的 grep 验证 + 在 plan 执行记录里说明「已天然覆盖」，跳过 Step 2/3。

- [ ] **Step 3: 跑测试 + 提交（如有新增）**

```bash
go test -race ./internal/pkg/agentruntime/runtimes/remote/...
git add internal/pkg/agentruntime/runtimes/remote/wire/wire_test.go
git commit -m "✅ piagent: 锁住 RunRequest.MCPServers 远程 round-trip"
```

---

## Task 6: e2e 群聊验收（piagent 成员）

用现有 e2e 群聊 harness（fake runtime 已会做 group_send MCP HTTP 客户端）验收：把成员 backend 设为 piagent，断言它能 group_send 把可见气泡冒回群。

> 注意 memory 坑：gateway 端口非 data-dir-scoped，正式 Agentre 在跑会占用 → 必须 `AGENTRE_PROXY_PORT=0` 绑临时端口，否则回投静默失效。

**Files:**
- 先读 `docs/e2e-harness-guide.md` + 现有群聊 spec（`e2e/tests/` 下 group-chat 用例）
- Create（throwaway 验证）：`e2e/scratch/piagent-group.spec.ts`（gitignored）

- [ ] **Step 1: 读懂现有 group-chat e2e 用例怎么建群/选 backend**

Run: `ls e2e/tests && grep -rln "group" e2e/tests`
读现有用例：看它怎么 seed backend 类型、怎么断言 `e2e-fake-reply:` 气泡 + DB 孪生 `agentGroupMessageCount`。

- [ ] **Step 2: 在 scratch 复制一份，成员 backend 改 piagent**

把现有 group-chat spec 复制到 `e2e/scratch/piagent-group.spec.ts`，把群成员 agent 的 backend kind 从 claudecode 换成 piagent（seed 数据处），其余断言不变：成员被 @ 后产出 `e2e-fake-reply:` 可见气泡、`agentGroupMessageCount` 增长。

- [ ] **Step 3: 跑 scratch 验证**

Run: `make e2e-scratch`（按 `docs/e2e-harness-guide.md`；必要时 `AGENTRE_PROXY_PORT=0`）
Expected: piagent 成员气泡可见、DB oracle 计数增长 = 注入链路在群聊端到端打通。

- [ ] **Step 4: 真机冒烟（可选但推荐）**

`make build` 后建一个 piagent 成员的真实群聊，发一条 @ 它的消息，确认它通过 `group_send` 回话。这一步验证真实 pi + typebox/schema 路径（spike 之外的最终确认）。

- [ ] **Step 5: 收尾**

scratch spec 不进 git（已 gitignored）。若想沉淀为常驻用例，再单独和用户确认是否进 `e2e/tests/`。最终全量：

```bash
make test-backend && make lint
```

Expected: 全绿。

---

## Self-Review

**1. Spec coverage（逐节对照）：**
- spec §3.1 `pkg/piagent` 通用 `WithExtension` → Task 1 ✅
- spec §3.2 `mcpbridge` `Materialize`/`RenderConfig` → Task 2 ✅
- spec §3.3 `bridge.mjs`（initialize/tools/list/tools/call、Bearer、JSON 子集、单 server 失败跳过、throw on error）→ Task 3 ✅
- spec §3.4 runtime 声明 `CapMCPTools` + `Run`/sessionFactory 装配 + 空 MCPServers 不变 → Task 4 ✅
- spec §4 工具命名（裸名、按 Tools 过滤）→ Task 3 已锁裸名 + allowlist ✅
- spec §5 能力门控自动放行（无前端改动）→ 无需任务（capability 抽象自动生效）；Task 6 端到端验证 ✅
- spec §6 远程 → Task 5 ✅
- spec §7 错误处理（server 跳过 / tool error throw / materialize 失败 Run error）→ Task 2/3/4 ✅
- spec §8 测试矩阵 → Task 1-6 覆盖纯函数/矩阵/runtime 行为/node:test/远程/e2e ✅

**2. Placeholder scan：** 无 TBD/“稍后实现”。Task 0 是显式 spike（探索性，按 skill 允许）；Task 5 Step 2 给了「若无 helper 则照搬现有用例」的明确指引而非占位。

**3. Type consistency：** `Materialize() (string,error)` / `RenderConfig([]agentruntime.MCPServerSpec, int64) (string,error)` / `ConfigEnvVar` / `WithExtension(string)` 在 Task 2/3/4 用法一致；bridge.mjs 的 `registerTool({name,label,description,parameters,execute})` 与 pi `ToolDefinition` 字段一致；`execute` 返回 `{content,details}` 与 `AgentToolResult` 一致；env 键 `AGENTRE_PI_MCP_CONFIG` = `mcpbridge.ConfigEnvVar`，bridge 读同名，三处一致。

---

## Execution Handoff

见技能提示，实现时二选一：subagent-driven（推荐）或 inline executing-plans。Task 0 spike 必须最先做（它定死 Task 3 的 schema 路径）。
