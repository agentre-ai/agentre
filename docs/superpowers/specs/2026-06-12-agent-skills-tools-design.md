# Agent 技能（skill-pack / plugin）按 agent 授权 + 技能/工具「添加」交互重做设计

日期：2026-06-12（已含 CLI 机制实测结论）
状态：已与用户确认；Claude Code 控制机制已实测验证
UI 稿：`~/Desktop/agentry.pen` 帧「⑥ Agent 能力区 v2 · 技能/工具」（plugin 粒度版）

## 1. 背景与目标

组织架构给 agent 配「技能 / 工具」当前两个问题：

- **技能是死字段**：`agents.skills_json` 只写入 + 投影回前端，运行期**无人消费**（见 `2026-06-11-agent-org-tool-design.md` §9）。前端**没有任何「添加技能」入口**。
- **工具 UI 不可扩展**：`agenttool` 注册表 + MCP 注入完整可用（首个 `org`），但配置区是一墙 toggle chip，注册表变长堆不下。

本期目标：把 skills 做成**真实、按 agent 授权、按 backend 能力门控**的特性，并把「技能/工具」配置统一为**「已授予芯片 + 添加目录弹窗」**交互。

> **关键实测结论（§2）**：Claude Code 的开关粒度是 **plugin（skill-pack）**，不是单个 skill。因此授权单元 = plugin。

### 1.1 锁定决策（含实测结论）

| 决策点 | 结论 |
| --- | --- |
| 授权单元 | **skill-pack = 一个 Claude Code plugin**（id 形如 `superpowers@claude-plugins-official`），其内含多个 skill。CLI 只能按 plugin 开关（§2 实测），不能按单 skill |
| 授权粒度 | **每个 agent 独立**，存 agent 上（与 `tools_json` 对称），按该 agent 的 turn 注入 |
| 控制机制 | spawn 时经 **`--settings` 注入 `enabledPlugins` 覆盖**（§2 实测：关 superpowers → 其 14 个 skill 全消失，32→18）。复用现成 `ccLaunchSpec.Settings` seam |
| 可用目录来源 | **混合**：`agentre 推荐`（精选 plugin id）+ `发现`（该 backend 的 claude 安装：`claude plugin list --json`）+ 可选 `可安装`（`--available`） |
| 个人 skills | `~/.claude/skills/*`（非 plugin 的裸 skill，如 `cago`/`deep-research`）**不可单独开关**（§2 实测）；列为只读「始终可用」组，不参与授权 |
| 推荐安装语义（v1） | 推荐但未安装的 pack **灰显标 `需先安装`、不可勾选**；`Provisioner` 接口预留（真实实现 = `claude plugin install <id>`，CLI 已支持），v1 不自动装 |
| 生效时机 | **spawn-time（cache-miss）生效**；改授权在该会话**下次 spawn** 生效（新会话即时；活跃缓存会话下次重启生效）。与 org 工具 / codex `permission_mode_at_launch` 同语义 |
| 门控 | 新 capability `CapSkills`，仅 `claudecode` 声明；其余 backend 隐藏技能区 + 门控说明 |
| 添加交互 | **弹窗目录**（搜索 + 来源分组 + 勾选），非内联 |
| 工具侧 | **复用同一 picker 组件**；工具仍 `{key,enabled}` + `CapMCPTools` 门控，沿用 org-tool 设计 |

## 2. CLI 机制实测（spike 结论，已验证）

环境：`claude` 2.1.174。结论用于支撑上表，**实现时无需再 spike**。

- **发现**：`claude plugin list --json` → `[{id:"name@marketplace", enabled, scope, installPath, mcpServers}]`；`--available --json` 追加 marketplace 可装项；`claude plugin details <id>` → 该 pack 的 skill 清单 + token 成本。
- **per-session 真相**：`--output-format stream-json` 的 `system.init` 帧带 `skills[]` / `plugins[]`（agentre runtime 已解析此帧）。plugin skill 命名 `superpowers:brainstorming`，个人 skill 裸名 `cago`。
- **控制（决定性实验）**：

  | run | 总 skills | superpowers skills | plugins[] 含 superpowers |
  | --- | --- | --- | --- |
  | baseline | 32 | 14 | ✅ |
  | `--settings '{"enabledPlugins":{"superpowers@claude-plugins-official":false}}'` | **18** | **0** | ❌ |

  → 注入 `enabledPlugins` 在 launch 时**完整控制 plugin 及其 skills**。
- **粒度边界（实测）**：单个 skill 不能关（`cago@skills-dir:false` 无效）；个人裸 skill 不受 `enabledPlugins` 控制；`--disable-slash-commands` = 关全部；`--setting-sources` 只能整层裁（排除 user → 32→14，plugins 归零）。**故授权单元只能是 plugin**。
- **约束到子集的做法**：`--settings` 是**叠加**（additional）。要让 agent 只拥有「授予的 pack」，须注入**全量** `enabledPlugins` 映射 `{每个已安装 plugin: 是否授予}`（授予→true，其余→false），覆盖用户全局开关。

## 3. 总体架构

```
组织架构页 agent 详情面板
  ├─ 技能区（CapSkills 门控）：已授予 pack 芯片 + 「添加技能」→ 目录弹窗
  │     · 只读「始终可用」组列出个人 ~/.claude/skills/* 裸 skill
  └─ 工具区（CapMCPTools 门控）：已授予芯片 + 「添加工具」→ 同一 picker
        │ 打开弹窗时 lazy 调 ListAgentSkillPacks(agentID)
        ▼
  skill_svc.ListAgentSkillPacks ── 合并去重 ──► 前端 CatalogItem[]
        ├─ agentskill.Recommended（静态精选 plugin id）
        ├─ agentskill.Discoverer[backendType]（claude plugin list --json，init 注册）
        └─ agent.GetEnabledPackIDs()（标注 enabled）
        │ 保存 skills_json（[{id,enabled}], id=plugin id）
        ▼
agents.skills_json ──► chat_svc 单聊 turnExtras / group scheduler.launchDelivery
        │ CapSkills 门控；skill_svc.EnabledPluginsMap(agentID) = {全部已安装: 授予?}
        ▼  RunRequest.EnabledPlugins = map[string]bool
  claudecode runtime（cache-miss spawn）：buildSettingsJSON 渲染进 --settings
  codex / builtin / piagent：忽略 RunRequest.EnabledPlugins（软降级）
```

## 4. 接口抽象（核心）

> 每个 seam 都是「消费者侧窄接口 + Register/accessor 注入」，依赖单向 `app → service → pkg(leaf)`。

### 4.1 能力门控 `CapSkills`

- `capability/capability.go` 增 `CapSkills Capability = "skills"`；`claudecode.Capabilities()` 声明 `true`，其余不声明。三处同步 + `TestClaudeCodeCapabilities` 矩阵断言。
- 前端 `useBackendCapabilities` 无需改，`caps.has("skills")` 直接可用。

### 4.2 技能目录域 `internal/pkg/agentskill`（leaf，不反向 import service）

```go
type Source string
const (
    SourceRecommended Source = "recommended" // agentre 精选、当前未安装
    SourceInstalled   Source = "installed"   // 该 backend 的 claude plugin list 命中
    SourceAvailable   Source = "available"   // marketplace 可装、未装
)

// SkillPack = 一个 Claude Code plugin（授权单元）
type SkillPack struct {
    ID          string   // "name@marketplace"，= enabledPlugins 的 key，= agents.skills_json 的 id
    Name        string   // 展示名（plugin name）
    Description string
    Skills      []string // 该 pack 暴露的 skill 名（plugin details；用于 UI 展开 + 文案）
    AlwaysOnTok int      // details 的 always-on token 成本（可选展示）
    Source      Source
    Recommended bool     // agentre 是否精选（与 Source 正交）
    Installed   bool     // 是否已装到该 backend
}

// 推荐：静态精选 plugin id 列表，纯函数（仿 agenttool.Registry）
func Recommended() []SkillPack

// 发现器：按 backend 枚举安装的 pack（消费者侧窄接口）
type Discoverer interface {
    Discover(ctx context.Context, q DiscoverQuery) ([]SkillPack, error)
}
type DiscoverQuery struct {
    BackendType agent_backend_entity.BackendType
    CLIPath     string // 定位该 claude 安装
}
func RegisterDiscoverer(t agent_backend_entity.BackendType, d Discoverer) // init 注册，仿 prober/runtime
func DiscovererFor(t agent_backend_entity.BackendType) (Discoverer, bool)
```

- claudecode 发现器实现放 `internal/pkg/agentskill/claudeskill`：跑 `<CLIPath> plugin list --json`（+ 可选 `details`）解析成 `[]SkillPack`。无 Discoverer 的 backend → 空，与 `CapSkills` 未声明一致。
- 个人裸 skill 从 `system.init` 的 `skills[]` 裸名拿（或 `~/.claude/skills` 扫目录），单独返回为只读组，**不进授权集**。

### 4.3 安装语义 `Provisioner`（v1 预留 no-op）

```go
type Provisioner interface {
    EnsureInstalled(ctx context.Context, q DiscoverQuery, packIDs []string) error // 真实实现 = claude plugin install
}
```

- v1 默认 = no-op：推荐未安装 pack 灰显 `需先安装`、不可勾选。升级到自动安装只换实现 + 在 `skill_svc.UpdateEnabled` 末尾调一次，调用方不变。

### 4.4 运行期注入 seam

- `agentruntime.RunRequest` 增 `EnabledPlugins map[string]bool`（全量已安装 plugin → 是否授予；为约束到子集须含 false 项，见 §2）。
- `claudecode` runtime：纯函数 `buildSkillsSettings(enabled map[string]bool, base string) string`，把 `{"enabledPlugins": <map>}` 合进 `--settings`（喂 `ccLaunchSpec.Settings`）。可独立表测。
- 其余 runtime 忽略 `req.EnabledPlugins`（LSP，与 `MCPServers`/`CapMCPTools` 同构）。
- **生效语义**：launch-time（cache-miss spawn）注入；cache-hit 复用不重注入 → 改授权下次 spawn 生效（spec §1.1）。

### 4.5 数据模型

- `agent_entity.AgentSkillItem` 由 `{Label,Enabled}` 改 **`{ID,Enabled}`**（ID = plugin id，与 `AgentToolItem` 对称）：

```go
type AgentSkillItem struct {
    ID      string `json:"id"`      // "name@marketplace"
    Enabled bool   `json:"enabled"`
}
func (a *Agent) GetSkills() []AgentSkillItem
func (a *Agent) SetSkills(items []AgentSkillItem)
func (a *Agent) GetEnabledPackIDs() []string
func (a *Agent) SkillPackEnabled(id string) bool
```

- **新 patch migration 追加 `migrationList()` 末尾**：native SQL 把 `agents.skills_json` 重置为 `'[]'`（旧 `{label}` 数据无 UI 入口、未发布，hard delete）。

### 4.6 服务层 `skill_svc`（DIP：依赖 §4.2/§4.3 接口 + accessor）

```go
func (s *Service) ListAgentSkillPacks(ctx, agentID int64, refresh bool) (SkillCatalogDTO, error) // lazy；含 packs[] + personalSkills[]
func (s *Service) UpdateEnabled(ctx, agentID int64, items []AgentSkillItemDTO) error              // 末尾调 Provisioner（v1 no-op）
func (s *Service) EnabledPluginsMap(ctx, agentID int64) (map[string]bool, error)                  // 注入用：{全部已安装: 授予?}；单聊/群聊共用
```

- 合并去重：按 ID 取并集；既推荐又安装 → `Installed=true,Recommended=true,Source=installed`；推荐未安装 → `Recommended=true,Source=recommended`。
- `EnabledPluginsMap`：发现全部已安装 → 与 agent 授予集求 `{id:授予?}`。**发现结果按 backend(CLIPath) 缓存**（rescan 失效），避免每 turn 跑 CLI。
- 依赖 `agent` / `agent_backend` accessor + `agentskill` 接口，全经 `RegisterXxx` 注入，单测 mock。

### 4.7 Wails binding

- 新 `App.ListAgentSkillPacks(agentID, refresh) → SkillCatalogDTO`（lazy，仅弹窗打开调）。
- `UpdateAgentRequest.skills` DTO 改 `[]{ id, enabled }`，沿用既有 agent 保存链路。
- binding 仅 parse → `skill_svc.Xxx` → return。

### 4.8 前端契约（技能/工具共用）

```ts
type CatalogItem = {
  id: string                     // pack id 或 tool key
  name: string
  description: string
  contents?: string[]            // pack 内 skill 名（可展开）
  group?: string                 // "推荐" / "已安装" / "可安装" / "始终可用(只读)"
  badge?: { label: string; tone: "recommended"|"installed"|"available"|"approval" }
  enabled: boolean
  disabledReason?: string        // 如「需先安装」
}
```

- `<CapabilityPicker title items onToggle onConfirm onRescan? />`：技能/工具同一组件。
- `<GrantedChips items onAdd onRemove />`：面板已授予芯片 + 添加按钮。
- 门控：`useBackendCapabilities(backendId).has("skills")` → 技能区或门控说明；工具区沿用 `CapMCPTools`。
- 文案 i18n：弹窗/区块/来源徽标进 `common.json`；pack name/description/skills 来自后端（动态，不进 `t()`）；工具沿用既有 `org.agent.tools.*`。

## 5. per-agent 独立怎么成立 + caveat（实现须知）

同后端的多个 agent 共用**一份** `~/.claude/` 安装与 `settings.json`，但 agentre **从不改那个共享文件**——而是在**每次 spawn 子进程时**单独给它传一份 `--settings` 覆盖。两层分开：「装了什么」（目录）由共享安装决定；「该 agent 实际拿到什么」（授权）由它自己的 `enabledPlugins` 覆盖决定。

- 一 chat-session = 一 claude 子进程（LRU 按 `sessionKey(req.SessionID)`，`runtime.go:83,426`）；群成员各自 `BackingSessionID`（`scheduler.go:305-330`）→ **每 agent 独立子进程**。
- 注入**全量** `enabledPlugins` map（每个已装 plugin→是否授予，含 false）→ 该 agent 技能集**只由自己的覆盖决定**，与用户全局 `settings.json` 开了什么无关。
- 并发互不干扰示例（同后端两 agent 同时跑）：
  ```
  后端工程师 → claude … --settings '{"enabledPlugins":{"superpowers@…":true ,"frontend-design@…":false}}'
  前端工程师 → claude … --settings '{"enabledPlugins":{"superpowers@…":false,"frontend-design@…":true }}'
  ```
- 与已上线 org 工具（per-agent `MCPServers`→`--mcp-config`，`session.go:246-250`）**同模式，已验证**（实测关 superpowers→14 skill 整包消失，§2）。
- **caveat**：launch-time 生效，cache-hit 复用不重下发（`runtime.go:448-453`）→ 改授权**下次 spawn** 生效（新会话即时；活跃缓存会话下次重启）。无 per-call gateway，纯 launch-time。

## 6. 测试（严格 TDD，Red → Green → Refactor）

- **capability**：claudecode 声明 `CapSkills` 矩阵断言。
- **entity**：`GetSkills/SetSkills/GetEnabledPackIDs/SkillPackEnabled` 序列化与边界。
- **migration**：`skills_json` 重置迁移（沿用 migrations 连库例外）。
- **agentskill**：`Recommended()` 稳定；fake Discoverer；claudeskill 解析 `plugin list --json` 样例（含空/坏 JSON、`details` 缺失）。
- **skill_svc**（mockgen 注入）：合并去重 + 推荐/安装正交；`EnabledPluginsMap` = {全部已安装:授予?}（含 false 项）；`UpdateEnabled` 调 Provisioner（v1 no-op）；发现缓存命中/rescan 失效。
- **runtime**：`buildSkillsSettings` 纯函数表测（含与既有 base settings 合并）；claudecode `Run` 把 `EnabledPlugins` 渲进 settings；其余 runtime 忽略。
- **注入**：单聊 turnExtras + group scheduler 按 `CapSkills` 门控填 `RunRequest.EnabledPlugins`。
- **前端 Vitest**：CapabilityPicker 分组/勾选/确认/禁用项、GrantedChips 增删、技能区按 cap 门控、i18n key 覆盖。

## 7. 不在本期范围

- 自动安装推荐 pack（`Provisioner` 真实 `plugin install` 实现）——接口预留，v1 no-op。
- 单个 skill 粒度授权（CLI 不支持）；个人裸 skill 的单独开关（CLI 不支持，只读展示）。
- 远端 backend 的 pack 发现（discovery 须在 claude 所在机器；远端经 daemon 转发，与 org-tool/group_send remote 限制同源，统一方案再做）→ v1 仅本地 backend，远端软降级。
- 改授权即时热生效（当前 = 下次 spawn 生效）。
- 工具侧审批/注入逻辑（沿用 org-tool 设计）。

## 8. 实施切分建议（供 writing-plans 参考）

1. **数据层**：entity `AgentSkillItem{ID,Enabled}` + migration 重置 + DTO 链路。
2. **目录域**：`agentskill`（SkillPack + Recommended + Discoverer 注册表）+ `claudeskill` 发现器（`plugin list --json` 解析，含缓存）。
3. **服务层**：`skill_svc`（ListAgentSkillPacks 合并去重 + UpdateEnabled + EnabledPluginsMap）+ `ListAgentSkillPacks` binding。
4. **能力 + 注入**：`CapSkills` + `RunRequest.EnabledPlugins` + claudecode `buildSkillsSettings` + 单聊/群聊门控注入。
5. **前端**：`CapabilityPicker` + `GrantedChips` + 技能/工具区改造（含只读个人 skill 组）+ 门控 + i18n。
