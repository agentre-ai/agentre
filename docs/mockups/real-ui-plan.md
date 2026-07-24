# 基于 AgentRE 真实前端组件的 OpenClaw Gateway Mockup 最小方案

> 调查分支：`feat/openclaw-integration-mockup`
> 目标：只做可启动、可交互、可截图的前端 Mockup；不实现正式 backend，不改 Go、数据库、migration、Wails binding 或生成代码。

## 结论

推荐做一个**独立的 Vite mockup 入口**，仍渲染现有 `App` / Settings / Chat / Dialog / Transcript 组件；入口启动前在浏览器里安装假的 `window.go.app.App` 和 `window.runtime`，并用 query scene + localStorage fixture 固定页面状态。

不要继续扩展现有 `docs/mockups/openclaw-integration.html` 作为最终验收物：它是手写 CSS/DOM，不会暴露真实组件的布局、i18n、Radix portal、虚拟 transcript、响应式导航等问题。它可保留为需求草图。

也不要直接复用 `frontend/src/__tests__/mocks/wailsApp.ts` 作为浏览器 mock：该文件依赖 `vitest`/`vi.fn`，只能在 Vitest 环境使用。

---

## 1. 推荐的 mock-only 文件与开关

### 1.1 新增文件（最小集合）

```text
frontend/
  openclaw-mockup.html
  src/mockups/openclaw/
    main.tsx
    install-runtime.ts
    fixtures.ts
    scene.ts
```

可选的临时截图脚本（仓库已 gitignore `e2e/scratch/*`）：

```text
e2e/scratch/openclaw-real-ui.spec.ts
```

职责：

- `openclaw-mockup.html`
  - 独立 Vite HTML entry，只加载 `src/mockups/openclaw/main.tsx`。
  - 不修改生产 `index.html`，也不要求改 `frontend/src/main.tsx`。
  - `vite build` 默认仍以正式 `index.html` 为入口；Mockup 只用于 dev server。
- `main.tsx`
  - 先解析 scene、安装 `window.go` / `window.runtime`、写入固定 localStorage，再动态 `import("@/App")` 并 render。
  - 必须动态 import，避免真实组件在 mock runtime 安装前执行 Wails 调用。
- `install-runtime.ts`
  - 安装浏览器版 Wails seam：`window.go.app.App` 和 `window.runtime`。
  - 只提供前端当前会调用的方法；未知方法用安全 fallback，并在 console 标出调用名，避免静默假成功。
  - 方法全部返回 Promise，保持与生成的 `frontend/wailsjs/go/app/App.js` 一致。
- `fixtures.ts`
  - 固定的 OpenClaw backend、agent、session、messages、capabilities、probe result。
  - 只使用假 token，例如 `mock-token-not-a-secret`；返回结构只给 `hasToken: true`，绝不返回 token。
- `scene.ts`
  - query 参数到页面状态的唯一映射，避免散落 `if (location.search...)`。

### 1.2 开关

用独立入口作为第一道硬开关：

```text
http://127.0.0.1:4173/openclaw-mockup.html?scene=backend-list
```

建议 scene：

- `backend-list`
- `backend-dialog`
- `backend-dialog-probed`
- `chat-connected`
- `chat-disconnected`
- `chat-scope-error`

可再要求显式环境开关作为第二道保险：

```bash
VITE_AGENTRE_MOCKUP=openclaw pnpm dev --host 127.0.0.1 --port 4173
```

`main.tsx` 在 `import.meta.env.VITE_AGENTRE_MOCKUP !== "openclaw"` 时直接显示“mockup disabled”或抛错，不安装 `window.go`。这样不会误把 mock runtime 带进普通浏览器预览。

### 1.3 Runtime mock 的最小方法表

全 App 启动与目标页面至少需要：

**App shell / 常驻 host**

- `Info`
- `ListChatAgents`
- `ConfirmQuit`
- `window.runtime.EventsOnMultiple` / `EventsOff` / `EventsOffAll` / `EventsEmit`
- `window.runtime.Environment`
- `WindowCenter` / `WindowShow` / `WindowSetSize` / `WindowGetSize` / `WindowIsFullscreen`
- 日志函数可 no-op。

**Settings / Agent Backend**

- `ListAgentBackends`
- `ListLLMProviders`
- `RemoteDeviceList`
- `TestAgentBackend`
- `CancelTestAgentBackend`
- `CreateAgentBackend`
- `UpdateAgentBackend`
- `DeleteAgentBackend`
- `ScanAndCreateAgentBackends`
- `GetGatewayStatus`（注意这是现有 AgentRE HTTP LLM gateway；OpenClaw probe 在 Mockup 内应是独立 mock action，不要把两者语义混用）

**Chat**

- `LoadChatSession`
- `ListChatAgentSessions`
- `MarkChatSessionRead`
- `GetSessionCapabilities`
- `GetBackendCapabilities`
- `ProjectListTree`（可返回空树）
- `AnswerToolPermission`
- `AnswerToolApproval`
- `SendChatMessage` 等非目标交互可返回安全的 mock response，或明确禁用按钮。

建议 `window.go.app.App` 用明确 handler map + Proxy：目标方法提供准确 fixture，未知方法 reject `mock handler missing: <name>`，而不是统一 `{}`。这能快速发现真实 App 新增了启动依赖。

### 1.4 Scene 初始化

- Settings scene：预写 `agentre.lastPath=/settings`，并让 Settings 初始 section 为 `agent-backend`。
- Chat scene：预写：
  - `agentre.lastPath=/chat`
  - `agentre.chatTabs` v2，包含固定 session tab（如 session `108`）和 activeTabId。
- Theme / language：预写 `agentre.theme=light`、语言为 `zh-CN` 或 `en`，截图脚本每个语言单独跑，避免依赖开发机状态。
- disconnected / scope-error 不靠定时器；由 scene fixture 直接给出确定状态，截图无 race。

---

## 2. 如何启动并截图

### 2.1 启动

前提：`frontend/wailsjs` 已生成。当前工作树存在生成文件，但它被 gitignore；新 checkout 若不存在，先运行项目已有的生成命令：

```bash
cd /root/code/agentre/agentre
make generate
```

启动独立 Vite mockup：

```bash
cd /root/code/agentre/agentre/frontend
VITE_AGENTRE_MOCKUP=openclaw pnpm dev --host 127.0.0.1 --port 4173
```

浏览：

```text
http://127.0.0.1:4173/openclaw-mockup.html?scene=backend-list
http://127.0.0.1:4173/openclaw-mockup.html?scene=backend-dialog-probed
http://127.0.0.1:4173/openclaw-mockup.html?scene=chat-connected
http://127.0.0.1:4173/openclaw-mockup.html?scene=chat-disconnected
```

不建议用 `make dev` 做这个 Mockup：它会启动真实 Go/Wails app，反而增加数据库、binding 和原生窗口变量；本方案目标是纯前端、零正式 backend。

### 2.2 截图

推荐写一次性的 `e2e/scratch/openclaw-real-ui.spec.ts`，直接连接上面的 Vite server；不要套用现有 `playwright.scratch.config.ts`，因为该 config 会额外启动 `wails dev -tags e2e`。

可以新增一个临时 config，或直接用 Playwright CLI/小脚本。确定性截图建议：

- viewport：`1440x900`
- `deviceScaleFactor: 1`
- 等待 `[data-mockup-ready="true"]`
- 禁止固定 sleep；ready 标记应在 fixtures 加载、Settings/Chat 数据完成渲染后设置。
- 截图前隐藏 caret，关闭动画（通过 mockup entry 注入一小段 mock-only CSS）。

建议输出：

```text
docs/mockups/openclaw-real-backend-list.png
docs/mockups/openclaw-real-backend-dialog.png
docs/mockups/openclaw-real-chat-connected.png
docs/mockups/openclaw-real-chat-disconnected.png
```

示意 Playwright 流程：

```ts
for (const scene of scenes) {
  await page.goto(`/openclaw-mockup.html?scene=${scene}`);
  await page.locator('[data-mockup-ready="true"]').waitFor();
  await page.screenshot({ path: output, fullPage: true });
}
```

Dialog scene 不要靠 Playwright 再点十几步构造状态；scene 可自动打开真实 `AgentreDialog`。另保留一个交互测试，从 `backend-list` 点击“新增后端”并选择 OpenClaw，验证真实点击路径没有坏。

---

## 3. 需要修改的具体组件位置

以下是做“真实组件 Mockup”不可避免的最小前端改动；均应被 mockup flag 隔离，不提交正式 backend contract。

### 3.1 `frontend/src/components/agentre/settings.tsx`

位置：

- `SettingsPageId`
- `SettingsPage` 的 `activePage` 初始化
- `AgentBackendSettings`

建议：

1. 给 `SettingsPage` 增加可选 `initialPage?: SettingsPageId`，默认仍为 `"appearance"`。
2. 正式 `SettingsRoute` 不传该 prop；mockup entry/shell 传 `"agent-backend"`。
3. 不要把 mock query 判断直接写进 Settings UI。

这样可以直接展示真实 Settings 左栏和 `AgentBackendsPanel`，且生产行为零变化。

### 3.2 `frontend/src/components/agentre/agent-backends.tsx`

这是主要改动点。当前文件约 2500 行，新增 OpenClaw 时不要继续把所有字段塞进 `BackendEditor`。

需要触碰的符号：

- `BackendType`
  - mockup-only 增加 `"openclaw"`。
- `backendTypeMeta`
  - 增加 OpenClaw icon/label；建议 `RadioTower`、`Waypoints` 或 `Cable`，不要用现有 LLM `Sparkles`。
- `BackendDraft`
  - Mockup 可用独立 `OpenClawDraft`，不要伪装成正式 Wails request 字段。
- `matchingProviders` / `strictMatchLabel`
  - OpenClaw 返回空 provider 集合；它由 Gateway 管模型和认证。
- `isCliBackend` / `cliBinaryName`
  - OpenClaw 必须不是 CLI backend。
- `BackendRow`
  - OpenClaw 行不能显示“LLM Provider 未关联/Needs action”。
  - 第三列显示 Gateway URL，第四列显示 OpenClaw agent/model 或连接状态。
  - 推荐抽一个 `BackendConnectionSummary`，避免把 provider 逻辑继续嵌套三元表达式。
- `BackendEditor`
  - 当 `type === "openclaw"` 时隐藏：`LlmProviderField`、`CliPathField`、model routes、CLI env、AgentRE HTTP proxy note。
  - 显示独立 `OpenClawBackendFields`。
- `BackendTypeSegmented`
  - 当前固定 `grid-cols-4`；增加第 5 类后必须改为可响应的 5 列/换行布局。推荐小屏 `grid-cols-2`、大屏 `sm:grid-cols-5`，否则 640px Dialog 中标签会挤压。
- `handleTest` / `handleSubmit`
  - Mockup flag 下走前端 stateful fixture，不调用不存在的正式 OpenClaw Wails request。
  - 正式四类仍走原逻辑。

建议新增 mockup 专属组件文件：

```text
frontend/src/mockups/openclaw/openclaw-backend-fields.tsx
```

字段：

- backend name（仍复用现有 name field）
- runtime device
- Gateway WebSocket URL
- token（password；编辑态只显示“已保存”，fixture 不含真实值）
- Test & Load
- OpenClaw agent select
- default model select
- session policy
- reasoning effort
- probe result：latency / Gateway version / protocol / scopes / agents/models 数量
- insecure `ws://` 非 loopback warning

此组件可以复用真实 `Input` / `Select` / `Alert` / `Badge` / `Button`，但只能返回 mockup draft/state，不能创造假的 Wails model 类。

### 3.3 `frontend/src/components/agentre/app-dialog.tsx`

结论：**原则上不用改。**

现有 `AgentreDialog` 已提供：

- Radix portal/overlay
- header/body/footer
- `DialogBody max-h-[70vh] overflow-y-auto`
- footer 固定在滚动区外
- `contentClassName` 可覆盖宽度

OpenClaw editor 只需调用：

```tsx
contentClassName="max-w-2xl"
bodyClassName="flex flex-col gap-4"
footerClassName="flex-col items-stretch gap-2"
```

仅当真实截图证实 900px 高度仍放不下 probe metrics 时，才考虑给 `AgentreDialog` 加可选 `bodyMaxHeightClassName`；不要为 Mockup 改全局默认 Dialog 尺寸。

### 3.4 聊天组件

优先复用现有完整聊天链，不新增“OpenClawChat”平行组件。

具体位置：

- `frontend/src/components/agentre/chat-page.tsx`
  - 无需为 OpenClaw 分支；由 `ListChatAgents` fixture 返回 `backendType: "openclaw"` 的真实 agent/session sidebar 数据。
- `frontend/src/components/agentre/chat-panel.tsx`
  - `activeBackendType` 已从 session/agent 动态读取。
  - capability 控件已通过 `GetSessionCapabilities` / `GetBackendCapabilities`，Mockup 只需返回 fixture，避免在这里新增 `backendType === "openclaw"` 的大段硬编码。
  - Header 的 “Copy launch command” 目前只给 CLI 三类，OpenClaw 不应加入。
  - 若要展示 Gateway badge，建议加一个小的、可选的 `runtimeStatusSlot`/`SessionRuntimeBadge`，数据来自 mock scene；不要把连接逻辑写入 ChatPanel。
- `frontend/src/components/agentre/chat.tsx` 的 `ChatTranscript`
  - 可直接渲染 fixture messages，无需改。
- `frontend/src/components/agentre/transcript-rows.ts`
  - 已支持 `tool_use` / `tool_result` / `tool_permission_request`，无需加 OpenClaw 私有 block。
  - Mockup fixture 应先归一化成现有 canonical block。
- `frontend/src/components/agentre/canonical-tool/tool-permission/card.tsx`
  - 可直接展示三按钮审批卡。fixture 结构参考 `chat.test.tsx` 的 pending `canonical.kind = "tool.permission"`。
- `frontend/src/components/agentre/chat-context-sidebar/*`
  - 不适合塞 OpenClaw Gateway/session mapping：当前语义是 outline/files。
  - 最小 Mockup可先不做右侧 mapping 面板；用 Header badge + transcript canonical cards 已能验证核心真实 UI。
  - 若产品必须保留静态草图中的“Gateway/session/run 映射”侧栏，应新增独立 tab（例如 runtime），不要覆盖 outline/files；这会显著超出“最小”范围，建议第二阶段。

### 3.5 路由/启动

- `frontend/src/App.tsx` 使用 `MemoryRouter`，不会读取浏览器 pathname；它只读取 `agentre.lastPath`。
- 因此 mockup scene 必须在动态 import `App` 前预写 localStorage。
- Chat tab store 在模块 import 时同步读取 `agentre.chatTabs`；也必须在 import `App` 前写入。
- 不建议为了截图把 `MemoryRouter` 改成 `BrowserRouter`。
- 生产 `frontend/src/main.tsx` 不需要修改；独立 HTML entry 更安全。

### 3.6 i18n

真实组件新增的所有静态文案必须同时加入：

```text
frontend/src/i18n/locales/zh-CN/common.json
frontend/src/i18n/locales/en/common.json
```

建议 key 前缀：

```text
agentBackends.backendType.openclaw.*
agentBackends.openclaw.fields.*
agentBackends.openclaw.probe.*
agentBackends.openclaw.security.*
chatPanel.runtime.openclaw.*
```

不要把 Mockup 中文直接硬编码进 JSX；现有 i18n lint/test 会阻止。

### 3.7 测试位置

按仓库 TDD 规则，实现前先扩展：

- `frontend/src/components/agentre/__tests__/agent-backends.test.tsx`
  - OpenClaw 类型出现；
  - 选择 OpenClaw 后隐藏 provider/CLI，显示 Gateway 字段；
  - probe 成功后解锁 agent/model；
  - auth/scope 失败可恢复；
  - 编辑态 token 不回显；
  - OpenClaw 行不显示 provider “Needs action”。
- 可新增 `frontend/src/mockups/openclaw/openclaw-backend-fields.test.tsx`
  - 只测纯 UI state，不依赖正式 Wails model。
- Chat 复用现有 `chat.test.tsx` fixture 结构，补一条 `backendType=openclaw` 时 canonical tool + pending permission 正常渲染即可。

---

## 4. 推荐 fixture 形状

### Backend list

```ts
{
  id: 40,
  type: "openclaw",
  name: "本机 OpenClaw",
  agentCount: 2,
  // 以下仅是 mockup view model，不冒充当前 BackendItem 正式字段
  openClawGatewayURL: "ws://127.0.0.1:18789",
  openClawAgentID: "main",
  openClawDefaultModel: "huu/gpt-5.6-sol",
  openClawConnectionStatus: "connected",
  openClawProtocol: "v4"
}
```

### Probe result

```ts
{
  ok: true,
  latencyMs: 23,
  gatewayVersion: "2026.7.23",
  protocolVersion: "4",
  scopes: ["operator.read", "operator.write"],
  agents: [
    { id: "main", name: "小机" },
    { id: "research", name: "研究助手" }
  ],
  models: ["huu/gpt-5.6-sol", "openai/gpt-5.4"]
}
```

### Chat session

`LoadChatSession` 返回：

- `session.backendType = "openclaw"`
- `session.id = 108`
- `session.agentName = "小机"`
- `session.contextWindow` 给一个非零值，以展示真实 context usage。
- messages 使用现有 `chat_svc.ChatMessage` plain data shape：
  - user text
  - assistant text
  - `tool_use` + `tool_result`
  - pending `tool_permission_request`，同时包含 `canonical.toolPermission` 和兼容 sidecar `toolPermission`。

不要把 OpenClaw 原始 Gateway event schema直接塞进 ChatTranscript；Mockup 应展示最终 canonical UI contract。

---

## 5. 风险

### 高风险

1. **把 Mockup 字段误当正式 Wails contract**
   - 当前生成的 `agent_backend_svc.BackendItem/CreateBackendRequest` 没有 OpenClaw 字段。
   - 对策：Mockup view model 放在 `src/mockups/openclaw`；mock flag 下单独处理，不修改 `frontend/wailsjs`，不伪造 Go binding。

2. **现有 AgentBackend editor 已过大**
   - 继续内联会把条件组合扩大，后续正式实现难以 TDD。
   - 对策：现在就拆 `OpenClawBackendFields`，只通过 typed draft/callback 与 editor 交互。

3. **浏览器 mock runtime 不完整导致白屏**
   - App 有常驻 `ChatStreamsHost`、通知、退出确认、窗口 API。
   - 对策：明确 runtime handler map；未知调用报出方法名；用 ready marker 作为截图 gate。

4. **测试 mock不能直接用于 Vite**
   - `wailsApp.ts` 使用 `vitest`。
   - 对策：浏览器 mock单独实现，fixture 数据可共享纯对象，但不能共享 `vi.fn` wrapper。

### 中风险

5. **五种 backend 挤坏 segmented control**
   - 当前固定 `grid-cols-4`，Dialog 默认 `max-w-xl`。
   - 对策：响应式 2/5 列；OpenClaw editor 用 `max-w-2xl`。

6. **OpenClaw 与现有 `GetGatewayStatus` 名称冲突**
   - 现有 Gateway 是 AgentRE 的 HTTP LLM proxy，不是 OpenClaw Gateway。
   - 对策：Mockup state 命名 `openClawProbe` / `openClawConnection`；UI 文案显式写 OpenClaw Gateway。

7. **聊天右侧映射面板会扩大范围**
   - 当前 sidebar 只有 outline/files，硬塞 runtime 会破坏语义和测试。
   - 对策：最小版只做 header connection badge + canonical transcript；映射 tab 第二阶段。

8. **虚拟列表首帧/字体/动画造成截图不稳定**
   - ChatTranscript 使用 TanStack virtual 和 ResizeObserver。
   - 对策：固定 viewport、等待 ready、禁动画；必要时让 fixture 消息量较少，但仍走真实 `ChatTranscript`。

9. **localStorage 污染场景**
   - App 主题、路由、tabs 都持久化。
   - 对策：mockup main 每次启动先清理 `agentre.*` 目标 key，再写 fixture；Playwright 每 scene 用新 context。

10. **mock交互给人“已实现 backend”的错觉**
    - 对策：页面角落显示非侵入式 `Mock data · No Gateway connection` badge；截图说明也写清 mock-only。

### 低风险

11. **`frontend/wailsjs` 在新 checkout 不存在**
    - 对策：启动文档先 `make generate`；不要提交生成文件。

12. **明文 token 进入 Git**
    - 对策：fixture 只用明显假值；列表/编辑回包只暴露 `hasToken`，输入值用运行时临时 state。

---

## 6. 最小实施顺序

1. Red：给 `agent-backends.test.tsx` 增加 OpenClaw UI behavior tests。
2. 新增独立 mockup HTML/main/runtime/fixtures/scene。
3. `SettingsPage` 增加默认不变的 `initialPage` prop。
4. `BackendTypeSegmented` 支持第 5 类和响应式布局。
5. 新增 mock-only `OpenClawBackendFields`，接入 `BackendEditor` 的 flag 分支。
6. `BackendRow` 增加 OpenClaw summary 分支。
7. Chat fixture 走真实 App + `ChatPanel` + `ChatTranscript`，先不新增右侧 runtime tab。
8. 跑：
   - `cd frontend && pnpm test -- src/components/agentre/__tests__/agent-backends.test.tsx`
   - `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
   - `cd frontend && pnpm lint`
9. 启 Vite mockup，Playwright 截四张确定性图片。
10. 检查 git diff：不得出现 Go、migration、DB、`frontend/wailsjs` 或正式 Wails binding 变更。

## 最小验收标准

- Backend list 使用真实 Settings + Table + Button + Badge 组件。
- New/Edit 使用真实 `AgentreDialog`、Input、Select、Alert。
- probe success/error 至少两个 scene，可恢复，不含真实 token。
- Chat 使用真实 App shell、ChatPage、ChatPanel、ChatTranscript 和 canonical permission card。
- 纯 `pnpm dev` 可运行；无需真实 DB/Gateway/Go backend。
- 截图可重复生成，scene 无随机时间、无固定 sleep、无网络请求。
