# Agent 技能（Skill Pack）— PR1 后端实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 agent「技能」从死字段做成真实特性：按 Claude Code **plugin（skill-pack）** 粒度、**每 agent** 授权、`CapSkills` 门控，spawn 时经 `--settings` 注入 `enabledPlugins` 控制该 agent 会话的技能集。

**Architecture:** 复刻已上线的 org-tool 模式：leaf 注册表（`agentskill`）描述/发现 pack → `skill_svc` 组合（推荐 + 发现 + agent 授权集）→ `chat_svc` 在 turn 组装时按 `CapSkills` 填 `RunRequest.EnabledPlugins`（全量已安装 plugin→是否授予的 map）→ `claudecode` runtime 渲进 `--settings`。其余 backend 忽略（软降级）。发现/控制机制已实测（spec §2）。

**Tech Stack:** Go 1.26、cago、gorm + gormigrate、go.uber.org/mock（mockgen）、goconvey、sqlmock。本计划只含**后端**（PR2 前端另起）。

**Spec:** `docs/superpowers/specs/2026-06-12-agent-skills-tools-design.md`

---

## 工作区与基线（开工前一次）

- [ ] **W1：确认在 worktree + 基线编译**

本计划在 worktree `feature/agent-skills`（基于 `develop/group`）执行。worktree 里 Go 需 `GOWORK=off`，且 `make test-backend` 前若报缺生成物先 `make generate`（见 memory「agentre worktree 构建坑」）。

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre/.claude/worktrees/agent-skills
GOWORK=off go build ./internal/... ./pkg/... 2>&1 | tail -5
```
Expected: 编译通过（无输出或仅 warning）。失败则先 `make generate` 再重试；仍失败上报，不要在脏基线上继续。

---

## 文件改动地图

| 文件 | 责任 | 动作 |
| --- | --- | --- |
| `internal/model/entity/agent_entity/agent.go` | `AgentSkillItem{ID,Enabled}` + 访问方法 | 改 |
| `migrations/202606120001_agent_skills_reset.go` | 重置 `skills_json` 语义 | 建 |
| `migrations/migrations.go` | 注册 migration | 改 |
| `internal/service/department_svc/types.go` | `AgentSkillDTO{id,enabled}` | 改 |
| `internal/service/agent_svc/agent.go` | `skillsFromDTO`/`toItem` 用新字段 | 改 |
| `internal/pkg/agentskill/agentskill.go` | `SkillPack`/`Source`/`Recommended`/`Discoverer` 注册表（leaf） | 建 |
| `internal/pkg/agentskill/claudeskill/discover.go` | claudecode 发现器（`plugin list --json` 解析） | 建 |
| `internal/pkg/agentruntime/capability/capability.go` | `CapSkills` 常量 | 改 |
| `internal/pkg/agentruntime/runtimes/claudecode/runtime.go` | 声明 `CapSkills` | 改 |
| `internal/pkg/agentruntime/runner.go` | `RunRequest.EnabledPlugins map[string]bool` | 改 |
| `internal/pkg/agentruntime/runtimes/claudecode/skills.go` | 纯函数 `buildSkillsSettings` | 建 |
| `internal/pkg/agentruntime/runtimes/claudecode/session.go` | spawn 时把 `EnabledPlugins` 合进 `--settings` | 改 |
| `internal/service/skill_svc/{skill.go,types.go,register.go,deps.go}` | 组合服务 | 建 |
| `internal/service/chat_svc/chat.go` | turn 组装填 `RunRequest.EnabledPlugins` | 改 |
| `internal/app/agent.go` | `ListAgentSkillPacks` binding | 改 |
| `internal/bootstrap/cago.go` | 接线 `skill_svc` deps | 改 |

---

## Task 1：entity `AgentSkillItem{ID,Enabled}` + 访问方法

**Files:**
- Modify: `internal/model/entity/agent_entity/agent.go`（现 `AgentSkillItem{Label,Enabled}`，行 ~18-28；`GetSkills/SetSkills` 行 ~77-96）
- Test: `internal/model/entity/agent_entity/agent_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestAgentSkillPack(t *testing.T) {
	Convey("skill pack 序列化与查询", t, func() {
		a := &Agent{}
		a.SetSkills([]AgentSkillItem{
			{ID: "superpowers@claude-plugins-official", Enabled: true},
			{ID: "opsctl@opskat", Enabled: false},
		})

		Convey("GetEnabledPackIDs 只回 enabled 的 id", func() {
			So(a.GetEnabledPackIDs(), ShouldResemble, []string{"superpowers@claude-plugins-official"})
		})
		Convey("SkillPackEnabled 命中", func() {
			So(a.SkillPackEnabled("superpowers@claude-plugins-official"), ShouldBeTrue)
			So(a.SkillPackEnabled("opsctl@opskat"), ShouldBeFalse)
			So(a.SkillPackEnabled("missing@x"), ShouldBeFalse)
		})
		Convey("坏 JSON / 空串 → 空", func() {
			b := &Agent{SkillsJSON: "not json"}
			So(b.GetSkills(), ShouldResemble, []AgentSkillItem{})
			So(b.GetEnabledPackIDs(), ShouldResemble, []string{})
		})
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/model/entity/agent_entity/ -run TestAgentSkillPack -v`
Expected: FAIL（`GetEnabledPackIDs`/`SkillPackEnabled` 未定义；`AgentSkillItem` 无 `ID` 字段）

- [ ] **Step 3: 改 entity**

把 `AgentSkillItem` 改为：
```go
// AgentSkillItem Agent 技能包(= Claude Code plugin)开关。ID 形如 "name@marketplace"。
type AgentSkillItem struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
```
`GetSkills`/`SetSkills` 保持（字段名不变，仅元素结构变）。新增：
```go
// GetEnabledPackIDs 返回 enabled 的技能包 id。
func (a *Agent) GetEnabledPackIDs() []string {
	out := []string{}
	for _, it := range a.GetSkills() {
		if it.Enabled {
			out = append(out, it.ID)
		}
	}
	return out
}

// SkillPackEnabled 报告某技能包是否开启。
func (a *Agent) SkillPackEnabled(id string) bool {
	for _, it := range a.GetSkills() {
		if it.ID == id {
			return it.Enabled
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/model/entity/agent_entity/ -run TestAgentSkillPack -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/model/entity/agent_entity/agent.go internal/model/entity/agent_entity/agent_test.go
git commit -m "✨ agent_entity: AgentSkillItem 改 {ID,Enabled} + GetEnabledPackIDs/SkillPackEnabled"
```

---

## Task 2：migration 重置 `skills_json`

**Files:**
- Create: `migrations/202606120001_agent_skills_reset.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾，现最后一条 `migration202606110002()`）
- Test: `migrations/202606120001_agent_skills_reset_test.go`

> 旧 `{label}` 数据无 UI 入口、未发布，按项目惯例 hard delete（重置为 `'[]'`）。

- [ ] **Step 1: 写失败测试**

```go
func TestMigration202606120001(t *testing.T) {
	Convey("重置 agents.skills_json", t, func() {
		db := testutils.MigrateDB(t) // 跑全量 migration 的现成 helper；若无，照本目录其它 *_test.go 的连库方式
		So(db.Exec(`UPDATE agents SET skills_json = '[{"label":"old","enabled":true}]' WHERE system_badge = 'DEFAULT'`).Error, ShouldBeNil)
		So(migration202606120001().Migrate(db), ShouldBeNil)
		var v string
		So(db.Raw(`SELECT skills_json FROM agents WHERE system_badge = 'DEFAULT'`).Scan(&v).Error, ShouldBeNil)
		So(v, ShouldEqual, "[]")
	})
}
```
（连库方式照抄 `migrations/202606110001_agent_tools_test.go` 若存在；migrations 自身可连真库，是 sqlmock 规则的既有例外。）

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./migrations/ -run TestMigration202606120001 -v`
Expected: FAIL（`migration202606120001` 未定义）

- [ ] **Step 3: 建 migration**

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606120001 重置 agents.skills_json:旧 {label,enabled} 语义改为
// {id,enabled}(id=plugin id)。旧数据无 UI 入口、未发布,直接清空为 '[]'。
func migration202606120001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606120001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE agents SET skills_json = '[]'`).Error
		},
		Rollback: func(tx *gorm.DB) error { return nil },
	}
}
```

- [ ] **Step 4: 注册到 `migrationList()` 末尾**

在 `migrations/migrations.go` 的 `return []*gormigrate.Migration{...}` 末尾、`migration202606110002(),` 之后加：
```go
		migration202606120001(), // agents.skills_json 重置为 plugin-id 语义
```

- [ ] **Step 5: 跑测试确认通过**

Run: `GOWORK=off go test ./migrations/ -run TestMigration202606120001 -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add migrations/202606120001_agent_skills_reset.go migrations/202606120001_agent_skills_reset_test.go migrations/migrations.go
git commit -m "✨ migration: 重置 agents.skills_json 为 plugin-id 语义"
```

---

## Task 3：DTO 链路改 `{id,enabled}`

**Files:**
- Modify: `internal/service/department_svc/types.go`（`AgentSkillDTO`，行 ~35-39）
- Modify: `internal/service/agent_svc/agent.go`（`skillsFromDTO` 行 ~364-371；`toItem` 行 ~380-388）
- Test: `internal/service/agent_svc/agent_test.go`（现有 Create/Update 测试需同步改字段）

- [ ] **Step 1: 改 DTO**

`department_svc/types.go`：
```go
// AgentSkillDTO 与 agent_entity.AgentSkillItem 同结构(plugin id 开关)。
type AgentSkillDTO struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
```

- [ ] **Step 2: 改 agent_svc 转换**

`skillsFromDTO`：
```go
func skillsFromDTO(items []department_svc.AgentSkillDTO) []agent_entity.AgentSkillItem {
	out := make([]agent_entity.AgentSkillItem, 0, len(items))
	for _, s := range items {
		out = append(out, agent_entity.AgentSkillItem{ID: s.ID, Enabled: s.Enabled})
	}
	return out
}
```
`toItem` 里组 skills 的循环：
```go
	rawSkills := a.GetSkills()
	skills := make([]department_svc.AgentSkillDTO, 0, len(rawSkills))
	for _, s := range rawSkills {
		skills = append(skills, department_svc.AgentSkillDTO{ID: s.ID, Enabled: s.Enabled})
	}
```
`department_svc/department.go` 的 `toAgentSkillDTO`（Load 投影）同样把 `Label` 改 `ID`。

- [ ] **Step 3: 跑受影响测试（先看红）**

Run: `GOWORK=off go test ./internal/service/agent_svc/ ./internal/service/department_svc/ 2>&1 | tail -20`
Expected: 现有测试里用 `Label:` 字面量的断言编译失败或断言失败。把这些测试的 `Label` 改 `ID`（如 `{ID:"superpowers@x",Enabled:true}`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/service/agent_svc/ ./internal/service/department_svc/ 2>&1 | tail -10`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/department_svc/ internal/service/agent_svc/
git commit -m "♻️ skills DTO: Label → ID(plugin id),贯通保存/投影链路"
```

---

## Task 4：`agentskill` 域 — 类型 + 推荐 + 发现器注册表

**Files:**
- Create: `internal/pkg/agentskill/agentskill.go`
- Test: `internal/pkg/agentskill/agentskill_test.go`

- [ ] **Step 1: 写失败测试**

```go
package agentskill

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	. "github.com/smartystreets/goconvey/convey"
)

type fakeDisc struct{ packs []SkillPack }

func (f fakeDisc) Discover(context.Context, DiscoverQuery) ([]SkillPack, error) { return f.packs, nil }

func TestAgentSkill(t *testing.T) {
	Convey("recommended 非空且稳定", t, func() {
		r := Recommended()
		So(len(r), ShouldBeGreaterThan, 0)
		So(r[0].Recommended, ShouldBeTrue)
	})
	Convey("discoverer 注册/查询", t, func() {
		RegisterDiscoverer(agent_backend_entity.TypeClaudeCode, fakeDisc{packs: []SkillPack{{ID: "x@y"}}})
		d, ok := DiscovererFor(agent_backend_entity.TypeClaudeCode)
		So(ok, ShouldBeTrue)
		got, _ := d.Discover(context.Background(), DiscoverQuery{})
		So(got[0].ID, ShouldEqual, "x@y")
		_, ok2 := DiscovererFor(agent_backend_entity.TypeCodex)
		So(ok2, ShouldBeFalse)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/pkg/agentskill/ -v`
Expected: FAIL（包/符号不存在）

- [ ] **Step 3: 建 `agentskill.go`**

```go
// Package agentskill 维护 agent 技能包(= Claude Code plugin)目录:静态推荐 +
// 按 backend 的发现器注册表。leaf 层,不反向 import service。
package agentskill

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
)

type Source string

const (
	SourceRecommended Source = "recommended" // agentre 精选、当前未安装
	SourceInstalled   Source = "installed"   // 该 backend 安装命中
	SourceAvailable   Source = "available"   // marketplace 可装、未装
)

// SkillPack 一个技能包(= 一个 Claude Code plugin)。ID = "name@marketplace"。
type SkillPack struct {
	ID          string
	Name        string
	Description string
	Skills      []string // 包内 skill 名(用于 UI 展开/文案)
	Source      Source
	Recommended bool
	Installed   bool
}

// Recommended 返回 agentre 精选包(静态,纯函数)。id 用官方 marketplace 全名。
func Recommended() []SkillPack {
	return []SkillPack{
		{ID: "superpowers@claude-plugins-official", Name: "superpowers", Description: "TDD / 调试 / 计划 / 协作", Recommended: true, Source: SourceRecommended},
		{ID: "code-review@claude-plugins-official", Name: "code-review", Description: "提交前自查 diff 与回归", Recommended: true, Source: SourceRecommended},
	}
}

// DiscoverQuery 发现入参。
type DiscoverQuery struct {
	BackendType agent_backend_entity.BackendType
	CLIPath     string // 定位该 claude 安装(空 = 默认 binary)
}

// Discoverer 按 backend 枚举已安装技能包(消费者侧窄接口)。
type Discoverer interface {
	Discover(ctx context.Context, q DiscoverQuery) ([]SkillPack, error)
}

var discoverers = map[agent_backend_entity.BackendType]Discoverer{}

// RegisterDiscoverer init 注册(仿 runtime/prober);非线程安全,只在 init/bootstrap 调。
func RegisterDiscoverer(t agent_backend_entity.BackendType, d Discoverer) { discoverers[t] = d }

// DiscovererFor 取某 backend 的发现器。
func DiscovererFor(t agent_backend_entity.BackendType) (Discoverer, bool) {
	d, ok := discoverers[t]
	return d, ok
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/pkg/agentskill/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/pkg/agentskill/
git commit -m "✨ agentskill: SkillPack 类型 + Recommended + Discoverer 注册表"
```

---

## Task 5：claudeskill 发现器（解析 `plugin list --json`）

**Files:**
- Create: `internal/pkg/agentskill/claudeskill/discover.go`
- Test: `internal/pkg/agentskill/claudeskill/discover_test.go`

> 把「跑 CLI」与「解析 JSON」拆开：`parsePluginList([]byte) []SkillPack` 是纯函数（可表测）；`Discover` 只负责拼命令调它。实测输出形如 `[{"id":"superpowers@claude-plugins-official","enabled":true,"scope":"user"}]`（spec §2）。

- [ ] **Step 1: 写失败测试（纯解析）**

```go
func TestParsePluginList(t *testing.T) {
	Convey("解析 plugin list --json", t, func() {
		raw := []byte(`[
		  {"id":"superpowers@claude-plugins-official","enabled":true,"scope":"user"},
		  {"id":"opsctl@opskat","enabled":false,"scope":"user"}
		]`)
		packs, err := parsePluginList(raw)
		So(err, ShouldBeNil)
		So(len(packs), ShouldEqual, 2)
		So(packs[0].ID, ShouldEqual, "superpowers@claude-plugins-official")
		So(packs[0].Name, ShouldEqual, "superpowers") // id 取 @ 前段
		So(packs[0].Installed, ShouldBeTrue)
		So(packs[0].Source, ShouldEqual, agentskill.SourceInstalled)
		Convey("空/坏 JSON → 空,不 panic", func() {
			p, _ := parsePluginList([]byte(""))
			So(p, ShouldResemble, []agentskill.SkillPack{})
		})
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/pkg/agentskill/claudeskill/ -v`
Expected: FAIL（`parsePluginList` 未定义）

- [ ] **Step 3: 建 `discover.go`**

```go
// Package claudeskill 用 `claude plugin list --json` 发现该安装的技能包。
package claudeskill

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

func init() {
	agentskill.RegisterDiscoverer(agent_backend_entity.TypeClaudeCode, Discoverer{})
}

type Discoverer struct{}

type rawPlugin struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Scope   string `json:"scope"`
}

func parsePluginList(b []byte) ([]agentskill.SkillPack, error) {
	out := []agentskill.SkillPack{}
	if len(b) == 0 {
		return out, nil
	}
	var raws []rawPlugin
	if err := json.Unmarshal(b, &raws); err != nil {
		return out, nil // 坏 JSON 视为无发现,不阻断
	}
	for _, r := range raws {
		name := r.ID
		if i := strings.Index(r.ID, "@"); i > 0 {
			name = r.ID[:i]
		}
		out = append(out, agentskill.SkillPack{
			ID:        r.ID,
			Name:      name,
			Source:    agentskill.SourceInstalled,
			Installed: true,
		})
	}
	return out, nil
}

func (Discoverer) Discover(ctx context.Context, q agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	bin := strings.TrimSpace(q.CLIPath)
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, "plugin", "list", "--json")
	b, err := cmd.Output()
	if err != nil {
		return []agentskill.SkillPack{}, nil // CLI 不可用 → 软降级(空发现)
	}
	return parsePluginList(b)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/pkg/agentskill/claudeskill/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/pkg/agentskill/claudeskill/
git commit -m "✨ claudeskill: plugin list --json 发现器(解析纯函数 + CLI 软降级)"
```

---

## Task 6：`CapSkills` 能力

**Files:**
- Modify: `internal/pkg/agentruntime/capability/capability.go`（cap 常量块）
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime.go`（`Capabilities()` 行 ~88-110）
- Test: `internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go`（`TestClaudeCodeCapabilities` 矩阵）

- [ ] **Step 1: 加 cap 常量**

`capability.go` 常量块末尾（`CapAutonomousTurn` 后）加：
```go
	// CapSkills 标记 runtime 接受 RunRequest.EnabledPlugins,可按 agent 注入技能包
	// 开关(claudecode 经 --settings 的 enabledPlugins)。仅 claudecode 声明。
	CapSkills Capability = "skills"
```

- [ ] **Step 2: 改矩阵测试看红**

在 `TestClaudeCodeCapabilities` 期望集合里加 `capability.CapSkills: true`（照该测试现有断言写法）。
Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run TestClaudeCodeCapabilities -v`
Expected: FAIL（claudecode 尚未声明 CapSkills）

- [ ] **Step 3: claudecode 声明**

`runtime.go` 的 `Capabilities().Set` map 里（`CapAutonomousTurn: true,` 旁）加：
```go
			// 接受 RunRequest.EnabledPlugins,spawn 时渲进 --settings 的 enabledPlugins。
			capability.CapSkills: true,
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run TestClaudeCodeCapabilities -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/pkg/agentruntime/capability/capability.go internal/pkg/agentruntime/runtimes/claudecode/runtime.go internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go
git commit -m "✨ capability: CapSkills,claudecode 声明"
```

---

## Task 7：`RunRequest.EnabledPlugins` + claudecode `buildSkillsSettings`

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go`（`RunRequest` struct，`MCPServers` 字段旁）
- Create: `internal/pkg/agentruntime/runtimes/claudecode/skills.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/session.go`（`acquireSession` 组 `settingsJSON` 处；`ccBuildClientOpts` 已有 `WithSettings(spec.Settings)` 行 ~234-235，不改）
- Test: `internal/pkg/agentruntime/runtimes/claudecode/skills_test.go`

- [ ] **Step 1: 加 RunRequest 字段**

`runner.go` 的 `RunRequest` 里，`MCPServers []MCPServerSpec` 之后加：
```go
	// EnabledPlugins 非空 = 给本轮 claude 注入 enabledPlugins 覆盖(全量已安装
	// plugin → 是否授予;为约束到子集须含 false 项)。仅声明 CapSkills 的 runtime
	// (claudecode)消费,spawn 时渲进 --settings;其它 runtime 忽略。
	EnabledPlugins map[string]bool
```

- [ ] **Step 2: 写失败测试（纯函数）**

```go
func TestBuildSkillsSettings(t *testing.T) {
	Convey("把 enabledPlugins 合进 settings", t, func() {
		Convey("空 base", func() {
			got := buildSkillsSettings(map[string]bool{"a@m": true, "b@m": false}, "")
			var m map[string]any
			So(json.Unmarshal([]byte(got), &m), ShouldBeNil)
			ep := m["enabledPlugins"].(map[string]any)
			So(ep["a@m"], ShouldEqual, true)
			So(ep["b@m"], ShouldEqual, false)
		})
		Convey("空 map → 原样返回 base", func() {
			So(buildSkillsSettings(map[string]bool{}, `{"x":1}`), ShouldEqual, `{"x":1}`)
			So(buildSkillsSettings(nil, ""), ShouldEqual, "")
		})
		Convey("base 已是 JSON 对象 → 合并保留既有键", func() {
			got := buildSkillsSettings(map[string]bool{"a@m": true}, `{"effortLevel":"high"}`)
			var m map[string]any
			So(json.Unmarshal([]byte(got), &m), ShouldBeNil)
			So(m["effortLevel"], ShouldEqual, "high")
			So(m["enabledPlugins"].(map[string]any)["a@m"], ShouldEqual, true)
		})
	})
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run TestBuildSkillsSettings -v`
Expected: FAIL（`buildSkillsSettings` 未定义）

- [ ] **Step 4: 建 `skills.go`**

```go
package claudecode

import "encoding/json"

// buildSkillsSettings 把 enabledPlugins 覆盖合进 --settings 的 JSON。
// base 为空或非 JSON 对象时以空对象起。enabled 为空 → 原样返回 base。
func buildSkillsSettings(enabled map[string]bool, base string) string {
	if len(enabled) == 0 {
		return base
	}
	m := map[string]any{}
	if base != "" {
		_ = json.Unmarshal([]byte(base), &m) // 非对象/坏 JSON → 空起,不阻断
	}
	ep := map[string]bool{}
	for k, v := range enabled {
		ep[k] = v
	}
	m["enabledPlugins"] = ep
	b, _ := json.Marshal(m)
	return string(b)
}
```

- [ ] **Step 5: 在 spawn 路径接入**

在 `session.go` `acquireSession`（cache-miss 分支）组 `Settings:` 之前/处，把现有 `settingsJSON` 经 `buildSkillsSettings` 叠加。定位现有给 `ccLaunchSpec.Settings` 赋值的变量（如 `settingsJSON`，若当前恒为空则新建），改为：
```go
	settingsJSON = buildSkillsSettings(req.EnabledPlugins, settingsJSON)
```
确保它落进 `ccLaunchSpec{... Settings: settingsJSON ...}`。`ccBuildClientOpts` 里 `if spec.Settings != "" { WithSettings(spec.Settings) }` 已存在，不改。

- [ ] **Step 6: 跑测试 + 编译**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run TestBuildSkillsSettings -v && GOWORK=off go build ./internal/pkg/agentruntime/...`
Expected: PASS + 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/pkg/agentruntime/runner.go internal/pkg/agentruntime/runtimes/claudecode/skills.go internal/pkg/agentruntime/runtimes/claudecode/skills_test.go internal/pkg/agentruntime/runtimes/claudecode/session.go
git commit -m "✨ claudecode: RunRequest.EnabledPlugins → --settings enabledPlugins(buildSkillsSettings)"
```

---

## Task 8：`skill_svc` — 组合服务（含 mock）

**Files:**
- Create: `internal/service/skill_svc/{deps.go,skill.go,types.go,register.go}`
- Test: `internal/service/skill_svc/skill_test.go`
- Generate: `internal/service/skill_svc/mock_skill_svc/`（mockgen）

> DIP：`skill_svc` 依赖窄接口 `agentLookup`（按 id 取 agent）+ `backendLookup`（按 id 取 backend，拿 type/CLIPath），经 `RegisterXxx` 注入。发现走 `agentskill.DiscovererFor`。

- [ ] **Step 1: 定义依赖接口 + mockgen 指令（`deps.go`）**

```go
package skill_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

//go:generate mockgen -source deps.go -destination mock_skill_svc/mock_deps.go -package mock_skill_svc

type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	Update(ctx context.Context, a *agent_entity.Agent) error
}
type BackendLookup interface {
	Find(ctx context.Context, id int64) (*agent_backend_entity.AgentBackend, error)
}
```

- [ ] **Step 2: 写失败测试**

```go
// 本测试文件(package skill_svc)自带的 fake 发现器(与 Task 4 测试里的同形,但
// 跨包须各自定义一份)。
type fakeDisc struct{ packs []agentskill.SkillPack }

func (f fakeDisc) Discover(context.Context, agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	return f.packs, nil
}

func TestListAgentSkillPacks(t *testing.T) {
	Convey("合并推荐 + 发现 + 授权标注", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 1, AgentBackendID: 9}
		ag.SetSkills([]agent_entity.AgentSkillItem{{ID: "superpowers@claude-plugins-official", Enabled: true}})
		al.EXPECT().Find(gomock.Any(), int64(1)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{Type: "claudecode"}, nil).AnyTimes()
		agentskill.RegisterDiscoverer("claudecode", fakeDisc{[]agentskill.SkillPack{
			{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled},
			{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
		}})
		s := newForTest(al, bl)

		Convey("ListAgentSkillPacks", func() {
			cat, err := s.ListAgentSkillPacks(context.Background(), 1, false)
			So(err, ShouldBeNil)
			byID := map[string]SkillPackDTO{}
			for _, p := range cat.Packs {
				byID[p.ID] = p
			}
			So(byID["superpowers@claude-plugins-official"].Enabled, ShouldBeTrue)
			So(byID["superpowers@claude-plugins-official"].Installed, ShouldBeTrue)
			So(byID["superpowers@claude-plugins-official"].Recommended, ShouldBeTrue) // 推荐∩安装
			So(byID["opsctl@opskat"].Enabled, ShouldBeFalse)
		})
		Convey("EnabledPluginsMap = 全部已安装 → 是否授予(含 false)", func() {
			m, err := s.EnabledPluginsMap(context.Background(), 1)
			So(err, ShouldBeNil)
			So(m["superpowers@claude-plugins-official"], ShouldBeTrue)
			So(m["opsctl@opskat"], ShouldBeFalse) // 已装未授予 → 显式 false(约束子集)
		})
	})
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/skill_svc/ -v`
Expected: FAIL（包/符号不存在）

- [ ] **Step 4: 建 `types.go` + `skill.go` + `register.go`**

`types.go`：
```go
package skill_svc

type SkillPackDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	Source      string   `json:"source"`
	Recommended bool     `json:"recommended"`
	Installed   bool     `json:"installed"`
	Enabled     bool     `json:"enabled"`
}
type SkillCatalogDTO struct {
	Packs []SkillPackDTO `json:"packs"`
}
```

`skill.go`（核心，含合并去重）：
```go
package skill_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

type Service struct {
	agent   AgentLookup
	backend BackendLookup
}

func newForTest(a AgentLookup, b BackendLookup) *Service { return &Service{agent: a, backend: b} }

// discover 拿该 agent backend 的已安装包(claudecode 才有发现器)。
func (s *Service) discover(ctx context.Context, a *agent_entity.Agent) ([]agentskill.SkillPack, *agent_backend_entity.AgentBackend, error) {
	be, err := s.backend.Find(ctx, a.AgentBackendID)
	if err != nil || be == nil {
		return nil, be, err
	}
	d, ok := agentskill.DiscovererFor(agent_backend_entity.BackendType(be.Type))
	if !ok {
		return []agentskill.SkillPack{}, be, nil
	}
	packs, err := d.Discover(ctx, agentskill.DiscoverQuery{
		BackendType: agent_backend_entity.BackendType(be.Type),
		CLIPath:     be.CLIPath,
	})
	return packs, be, err
}

// merge 推荐 + 发现 按 id 去重,标注 enabled。
func merge(recommended, installed []agentskill.SkillPack, enabledIDs []string) []agentskill.SkillPack {
	enabled := map[string]bool{}
	for _, id := range enabledIDs {
		enabled[id] = true
	}
	byID := map[string]*agentskill.SkillPack{}
	order := []string{}
	add := func(p agentskill.SkillPack) {
		if ex, ok := byID[p.ID]; ok {
			if p.Recommended {
				ex.Recommended = true
			}
			if p.Installed {
				ex.Installed = true
				ex.Source = agentskill.SourceInstalled
			}
			return
		}
		cp := p
		byID[cp.ID] = &cp
		order = append(order, cp.ID)
	}
	for _, p := range installed {
		add(p)
	}
	for _, p := range recommended {
		add(p)
	}
	out := make([]agentskill.SkillPack, 0, len(order))
	for _, id := range order {
		byID[id].Enabled = enabled[id]
		out = append(out, *byID[id])
	}
	return out
}

func (s *Service) ListAgentSkillPacks(ctx context.Context, agentID int64, refresh bool) (SkillCatalogDTO, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return SkillCatalogDTO{}, err
	}
	installed, _, err := s.discover(ctx, a)
	if err != nil {
		return SkillCatalogDTO{}, err
	}
	packs := merge(agentskill.Recommended(), installed, a.GetEnabledPackIDs())
	dto := make([]SkillPackDTO, 0, len(packs))
	for _, p := range packs {
		dto = append(dto, SkillPackDTO{ID: p.ID, Name: p.Name, Description: p.Description, Skills: p.Skills, Source: string(p.Source), Recommended: p.Recommended, Installed: p.Installed, Enabled: p.Enabled})
	}
	return SkillCatalogDTO{Packs: dto}, nil
}

// EnabledPluginsMap 全部已安装 → 是否授予(含 false,用于约束子集)。注入用。
func (s *Service) EnabledPluginsMap(ctx context.Context, agentID int64) (map[string]bool, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return nil, err
	}
	installed, _, err := s.discover(ctx, a)
	if err != nil {
		return nil, err
	}
	granted := map[string]bool{}
	for _, id := range a.GetEnabledPackIDs() {
		granted[id] = true
	}
	out := map[string]bool{}
	for _, p := range installed {
		out[p.ID] = granted[p.ID]
	}
	return out, nil
}
```

`register.go`（accessor + 注入，仿其它 svc）：
```go
package skill_svc

var defaultSvc *Service

func Default() *Service { return defaultSvc }

// Register bootstrap 接线:注入依赖实现。
func Register(agent AgentLookup, backend BackendLookup) {
	defaultSvc = &Service{agent: agent, backend: backend}
}
```

- [ ] **Step 5: 生成 mock + 跑测试**

Run:
```bash
GOWORK=off go generate ./internal/service/skill_svc/... && GOWORK=off go test ./internal/service/skill_svc/ -v
```
Expected: PASS（`fakeDisc` 在测试文件里定义，结构同 Task 4）

- [ ] **Step 6: 提交**

```bash
git add internal/service/skill_svc/
git commit -m "✨ skill_svc: ListAgentSkillPacks 合并去重 + EnabledPluginsMap(注入用)"
```

---

## Task 9：注入接线 — chat_svc 填 `RunRequest.EnabledPlugins`

**Files:**
- Modify: `internal/service/chat_svc/chat.go`（`RunRequest{...}` 组装处，行 ~2424-2436，`MCPServers:` 旁）
- Create: `internal/service/chat_svc/turn_skills.go`（接缝 + 门控，仿 `turn_mcp.go`）
- Test: `internal/service/chat_svc/turn_skills_test.go`

> 与 `turn_mcp.go` 同构：bootstrap 注册 provider，`runTurn` 单点按 `CapSkills` 调用。单聊/群聊都过此点。

- [ ] **Step 1: 写失败测试**

```go
func TestAppendTurnSkills(t *testing.T) {
	Convey("门控 + provider 注入", t, func() {
		RegisterEnabledPluginsProvider(func(ctx context.Context, a *agent_entity.Agent) map[string]bool {
			return map[string]bool{"x@m": true}
		})
		Convey("capOK=false → 不注入", func() {
			So(enabledPluginsForTurn(context.Background(), &agent_entity.Agent{}, false), ShouldBeNil)
		})
		Convey("capOK=true → 注入", func() {
			So(enabledPluginsForTurn(context.Background(), &agent_entity.Agent{}, true), ShouldResemble, map[string]bool{"x@m": true})
		})
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/chat_svc/ -run TestAppendTurnSkills -v`
Expected: FAIL

- [ ] **Step 3: 建 `turn_skills.go`**

```go
package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

// EnabledPluginsProvider 按 agent 给 turn 返回技能包开关 map(全量已安装→是否授予)。
// bootstrap 注册 skill_svc 的实现;nil = 不注入。
type EnabledPluginsProvider func(ctx context.Context, a *agent_entity.Agent) map[string]bool

var enabledPluginsProvider EnabledPluginsProvider

func RegisterEnabledPluginsProvider(p EnabledPluginsProvider) { enabledPluginsProvider = p }

// enabledPluginsForTurn runTurn 组 RunRequest 时调;capOK = runner 声明 CapSkills。
func enabledPluginsForTurn(ctx context.Context, a *agent_entity.Agent, capOK bool) map[string]bool {
	if enabledPluginsProvider == nil || !capOK {
		return nil
	}
	return enabledPluginsProvider(ctx, a)
}
```

- [ ] **Step 4: chat.go 组 RunRequest 处加字段**

在 `req := agentruntime.RunRequest{...}` 里，`MCPServers: appendTurnMCP(...)` 之后加：
```go
		EnabledPlugins: enabledPluginsForTurn(ctx, a, runner.Capabilities().Has(capability.CapSkills)),
```

- [ ] **Step 5: 跑测试 + 编译**

Run: `GOWORK=off go test ./internal/service/chat_svc/ -run TestAppendTurnSkills -v && GOWORK=off go build ./internal/service/chat_svc/`
Expected: PASS + 编译通过

- [ ] **Step 6: 提交**

```bash
git add internal/service/chat_svc/turn_skills.go internal/service/chat_svc/turn_skills_test.go internal/service/chat_svc/chat.go
git commit -m "✨ chat_svc: 按 CapSkills 注入 RunRequest.EnabledPlugins(turn_skills 接缝)"
```

---

## Task 10：binding + bootstrap 接线

**Files:**
- Modify: `internal/app/agent.go`（加 `ListAgentSkillPacks` binding）
- Modify: `internal/bootstrap/cago.go`（`skill_svc.Register(...)` + `chat_svc.RegisterEnabledPluginsProvider(...)`，并 blank import claudeskill）
- Test: 手动 + 既有 bootstrap 测试

- [ ] **Step 1: binding（仅 parse→svc→return）**

`internal/app/agent.go`：
```go
// ListAgentSkillPacks 返回某 agent 可见的技能包目录(推荐 + 发现 + 已授权)。
func (a *App) ListAgentSkillPacks(agentID int64, refresh bool) (skill_svc.SkillCatalogDTO, error) {
	return skill_svc.Default().ListAgentSkillPacks(a.ctx, agentID, refresh)
}
```

- [ ] **Step 2: bootstrap 接线**

`internal/bootstrap/cago.go`：blank import 发现器（触发 init 注册）：
```go
	_ "github.com/agentre-ai/agentre/internal/pkg/agentskill/claudeskill"
```
在服务接线区（org tool 接线附近）加：
```go
	skill_svc.Register(agent_svc.Agent(), agent_backend_svc.Backend()) // accessor 名以实际为准
	chat_svc.RegisterEnabledPluginsProvider(func(ctx context.Context, a *agent_entity.Agent) map[string]bool {
		m, err := skill_svc.Default().EnabledPluginsMap(ctx, a.ID)
		if err != nil {
			return nil
		}
		return m
	})
```
> `agent_svc.Agent()` / `agent_backend_svc.Backend()` 需满足 `AgentLookup`/`BackendLookup` 的方法子集（`Find`/`Update`）。若现有 accessor 方法名不符，在 skill_svc 的接口按消费者侧 ISP 收窄到实际可用方法，或加薄适配，**不要**改既有 svc 公共签名。

- [ ] **Step 3: daemon import（远端）**

`internal/daemon/runtime_imports.go` 已 blank import claudecode runtime；本期发现走桌面端 CLI（远端 backend 软降级，spec §7），daemon 侧**不**需要 claudeskill。无需改。

- [ ] **Step 4: 全量后端测试 + 编译**

Run:
```bash
GOWORK=off go build ./... && GOWORK=off go test $(go list ./... | grep -v /frontend/) 2>&1 | tail -20
```
Expected: 编译通过；测试全绿（或仅与本改动无关的既有 flake，对照 memory「chat_svc Edit 测试 -race flake」辨别）。

- [ ] **Step 5: `make generate`（刷新前端绑定，供 PR2）**

Run: `make generate 2>&1 | tail -5`
Expected: `frontend/wailsjs/` 重生成（含 `ListAgentSkillPacks` + `SkillCatalogDTO` + `AgentSkillDTO{id,enabled}`）。本步产物给 PR2 用。

- [ ] **Step 6: 提交**

```bash
git add internal/app/agent.go internal/bootstrap/cago.go frontend/wailsjs/
git commit -m "🔌 bootstrap: 接线 skill_svc + EnabledPluginsProvider + ListAgentSkillPacks binding"
```

---

## 自检清单（实现完成后逐项核对）

- [ ] entity `AgentSkillItem{ID,Enabled}` + 两个新方法；坏 JSON 边界。
- [ ] migration 追加在 `migrationList()` 末尾，native SQL，重置为 `'[]'`。
- [ ] DTO 全链路 `Label→ID`：`department_svc` Load 投影 + `agent_svc` Create/Update/toItem。
- [ ] `agentskill`：Recommended 稳定；Discoverer 注册表；claudeskill 解析纯函数 + CLI 软降级。
- [ ] `CapSkills` 三处同步（常量 + claudecode 声明 + 矩阵测试）。
- [ ] `RunRequest.EnabledPlugins`；`buildSkillsSettings` 合并保留 base 键；spawn 路径接入。
- [ ] `skill_svc`：合并去重（推荐∩安装的正交标记）；`EnabledPluginsMap` 含 false 项；mock 注入不连库。
- [ ] 注入：`CapSkills` 门控；单聊/群聊都过 `runTurn` 单点（确认群聊 scheduler 走同一 `runTurn` 组装，否则群聊路径也要填 `EnabledPlugins`——实现时验证）。
- [ ] binding 仅 parse→svc→return；bootstrap blank import claudeskill。
- [ ] `make generate` 后 `make check`（lint + test，排除 /frontend/）全过。

## 不在 PR1 范围（PR2 前端 / 后续）

- 前端 `CapabilityPicker`/`GrantedChips`/技能区门控/i18n（PR2）。
- **保存路径**：PR1 技能授权的保存**复用既有 `agent_svc.Update`（`UpdateAgentRequest.skills`，Task 3）**——与 tools 一致；不引入 `skill_svc.UpdateEnabled`。`Provisioner`（v1 no-op）因此暂无接入点。
- `Provisioner` 真实 `plugin install`、远端发现、个人裸 skill 展示数据（spec §7）。
- 群聊 scheduler 若不走 `runTurn` 同一组装点，需补一处对称注入（实现时按实际路径定）。
