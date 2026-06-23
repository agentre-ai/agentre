# Agent 技能继承 + 双向三态 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 agent 技能从「默认拒绝白名单」改成「继承全局 `~/.claude` 已启用插件 + 按 agent 三态覆盖(继承/强制开/强制关)」。

**Architecture:** 实体 `AgentSkillItem{ID,Enabled}` 不变,语义改为「不在列表=继承 / 在列表 true=强制开 / 在列表 false=强制关」。后端核心改动是 `skill_svc.EnabledPluginsMap`:只下发 agent 的显式覆盖(true/false 都发),其余 absent→CLI 沿用全局。发现层透出全局 `enabled`。前端弹窗每行从单选框换成「继承|开|关」分段控件,芯片区显示三态(含继承)。

**Tech Stack:** Go 1.26 / cago / goconvey + gomock / Wails v2 / React 19 + TS / Vitest + Testing Library / react-i18next。

**Spec:** `docs/superpowers/specs/2026-06-13-agent-skills-inherit-tristate-design.md`

---

## 前置条件 / 协调(执行前必读)

1. **`internal/pkg/agentskill/claudeskill/discover.go` + `discover_test.go` 有未提交的并行改动**(`scanSkills` 填充 `SkillPack.Skills`,是弹窗 `N skills` 计数的前提)。Task 1 在此基础上**叠加** `GloballyEnabled`。
   - **执行前先确认这两个文件已提交**(由其负责人提交那一个自包含 bugfix)。提交后再从 `develop/group` 开 worktree,否则 worktree(基于已提交 HEAD)会缺这份改动,`N skills` 失效且后续合并冲突。
   - 若无法先提交:在主工作树就地实现 Task 1(与现有 WIP 并存),不要开 worktree。
2. **worktree 构建坑**(见记忆 `project_agentre_worktree_build_gotchas`):worktree 里 Go 命令要 `GOWORK=off`;`frontend/wailsjs`、`frontend/dist` 是 gitignore 生成物,跑前端/`make generate` 前需先 `make generate`(且占位 `frontend/dist`);git 写操作关 sandbox。
3. 全程**只改本任务相关文件**,不顺手重构 / 格式化无关代码。

---

## File Structure

**后端(Go):**
- Modify `internal/pkg/agentskill/agentskill.go` — `SkillPack` 加 `GloballyEnabled`。
- Modify `internal/pkg/agentskill/claudeskill/discover.go` — `parsePluginList` 填 `GloballyEnabled`(叠加在 WIP 上)。
- Modify `internal/pkg/agentskill/claudeskill/discover_test.go` — 断言 `GloballyEnabled`。
- Modify `internal/service/skill_svc/types.go` — `SkillPackDTO` 加 `GloballyEnabled`。
- Modify `internal/service/skill_svc/skill.go` — `ListAgentSkillPacks` 透传 `GloballyEnabled`;`EnabledPluginsMap` 翻转语义。
- Modify `internal/service/skill_svc/skill_test.go` — 改/加断言。
- Regenerate `frontend/wailsjs/go/models.ts`(`make generate`)。

**前端(TS):**
- Modify `frontend/src/components/agentre/capability/catalog.ts` — `CatalogItem` 加 `globallyEnabled` / `state`;新增 `TriState` 类型。
- Create `frontend/src/components/agentre/capability/tri-state-toggle.tsx` + `__tests__/tri-state-toggle.test.tsx` — 三态分段控件。
- Modify `frontend/src/components/agentre/capability/capability-picker.tsx` — 行控件按 `item.state` 分支(三态 vs 单选框);加 `onSetState` / `footerSummary`。
- Modify `frontend/src/components/agentre/capability/granted-chips.tsx` + 其测试 — chip 加 `tone`/`locked`。
- Modify `frontend/src/components/agentre/org/skill-catalog.ts` + 其测试 — 按 `globallyEnabled` 分组,产出三态目录项。
- Modify `frontend/src/components/agentre/org/use-skill-catalog.ts` — 提供 mount 自动加载。
- Modify `frontend/src/components/agentre/org/org-detail-agent.tsx` + 其测试 — 三态 setter、pickerItems overlay、芯片三态、挂载即拉、计数。
- Modify `frontend/src/i18n/locales/{zh-CN,en}/common.json` — 新 key。

---

## Task 1: 发现层透出全局 enabled(GloballyEnabled)

**Files:**
- Modify: `internal/pkg/agentskill/agentskill.go:20-29`
- Modify: `internal/pkg/agentskill/claudeskill/discover.go:23-30`, `:70-76`
- Test: `internal/pkg/agentskill/claudeskill/discover_test.go:24-36`

- [ ] **Step 1: 在 `SkillPack` 结构加字段**

`internal/pkg/agentskill/agentskill.go`,把 `SkillPack` 改成:

```go
// SkillPack 一个技能包(= 一个 Claude Code plugin)。ID = "name@marketplace"。
type SkillPack struct {
	ID              string
	Name            string
	Description     string
	Skills          []string // 包内 skill 名(用于 UI 展开/文案)
	Source          Source
	Recommended     bool
	Installed       bool
	GloballyEnabled bool // CLI 全局是否启用(claude plugin list --json 的 enabled)
}
```

- [ ] **Step 2: 写失败测试 —— `GloballyEnabled` 由 `enabled` 映射**

`internal/pkg/agentskill/claudeskill/discover_test.go`,在 `TestParsePluginList` 顶层(`So(packs[0].Source, ...)` 之后)加两行:

```go
So(packs[0].GloballyEnabled, ShouldBeTrue)  // superpowers enabled:true
So(packs[1].GloballyEnabled, ShouldBeFalse) // opsctl enabled:false
```

- [ ] **Step 3: 运行测试,确认失败**

Run: `GOWORK=off go test ./internal/pkg/agentskill/claudeskill/ -run TestParsePluginList -v`
Expected: FAIL(`packs[0].GloballyEnabled` 为 false,因为还没填)。

- [ ] **Step 4: 填充 `GloballyEnabled`**

`internal/pkg/agentskill/claudeskill/discover.go`:
1. 更新 `rawPlugin` 注释(`:23-24`),去掉"当前发现不消费"的说法:

```go
// rawPlugin 映射 `claude plugin list --json` 单元素。Enabled = CLI 全局启用态
// (透出到 SkillPack.GloballyEnabled,供"继承"模型判定);Scope 暂不消费。
```

2. 在 `parsePluginList` 的 `agentskill.SkillPack{...}` 字面量(`:70-76`)加一行 `GloballyEnabled: r.Enabled,`:

```go
out = append(out, agentskill.SkillPack{
	ID:              r.ID,
	Name:            name,
	Skills:          scanSkills(r.InstallPath),
	Source:          agentskill.SourceInstalled,
	Installed:       true,
	GloballyEnabled: r.Enabled,
})
```

- [ ] **Step 5: 运行测试,确认通过**

Run: `GOWORK=off go test ./internal/pkg/agentskill/... -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/agentskill/agentskill.go internal/pkg/agentskill/claudeskill/discover.go internal/pkg/agentskill/claudeskill/discover_test.go
git commit -m "✨ agentskill: SkillPack 透出 GloballyEnabled(CLI 全局启用态)"
```

---

## Task 2: SkillPackDTO 透传 GloballyEnabled

**Files:**
- Modify: `internal/service/skill_svc/types.go:3-12`
- Modify: `internal/service/skill_svc/skill.go:98-110`
- Test: `internal/service/skill_svc/skill_test.go:39-53`

- [ ] **Step 1: DTO 加字段**

`internal/service/skill_svc/types.go`:

```go
type SkillPackDTO struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Skills          []string `json:"skills"`
	Source          string   `json:"source"`
	Recommended     bool     `json:"recommended"`
	Installed       bool     `json:"installed"`
	Enabled         bool     `json:"enabled"`         // 该 agent 显式强制开授予
	GloballyEnabled bool     `json:"globallyEnabled"` // CLI 全局启用态(继承判定用)
}
```

- [ ] **Step 2: 写失败测试**

`internal/service/skill_svc/skill_test.go`,在 fakeDisc 的包里给 superpowers 标 `GloballyEnabled: true`,opsctl 不标。先把 `restore := agentskill.SwapDiscovererForTest(...)` 里的 packs 改成:

```go
restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fakeDisc{[]agentskill.SkillPack{
	{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
	{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
}})
```

在 `Convey("ListAgentSkillPacks", ...)` 块内加断言:

```go
So(byID["superpowers@claude-plugins-official"].GloballyEnabled, ShouldBeTrue)
So(byID["opsctl@opskat"].GloballyEnabled, ShouldBeFalse)
```

- [ ] **Step 3: 运行测试,确认失败**

Run: `GOWORK=off go test ./internal/service/skill_svc/ -run TestListAgentSkillPacks -v`
Expected: FAIL(`GloballyEnabled` 恒为 false,DTO 还没透传)。

- [ ] **Step 4: `ListAgentSkillPacks` 透传**

`internal/service/skill_svc/skill.go`,在构造 DTO 的字面量(`:100-109`)加 `GloballyEnabled: p.GloballyEnabled,`:

```go
dto = append(dto, SkillPackDTO{
	ID:              p.ID,
	Name:            p.Name,
	Description:     p.Description,
	Skills:          p.Skills,
	Source:          string(p.Source),
	Recommended:     p.Recommended,
	Installed:       p.Installed,
	Enabled:         mr.enabled[i],
	GloballyEnabled: p.GloballyEnabled,
})
```

> `merge` 复制整 pack,`GloballyEnabled` 已随 `e.pack` 透传,无需改 `merge`。

- [ ] **Step 5: 运行测试,确认通过**

Run: `GOWORK=off go test ./internal/service/skill_svc/ -run TestListAgentSkillPacks -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/service/skill_svc/types.go internal/service/skill_svc/skill.go internal/service/skill_svc/skill_test.go
git commit -m "✨ skill_svc: SkillPackDTO 透传 GloballyEnabled"
```

---

## Task 3: 注入语义翻转(EnabledPluginsMap → 只下发覆盖)

**Files:**
- Modify: `internal/service/skill_svc/skill.go:114-133`
- Test: `internal/service/skill_svc/skill_test.go:54-59`

- [ ] **Step 1: 改失败测试 —— 断言"只发覆盖、其余继承"**

`internal/service/skill_svc/skill_test.go`,把 `Convey("EnabledPluginsMap = 全部已安装 ...")` 整块替换为:

```go
Convey("EnabledPluginsMap 只发 agent 显式覆盖(true/false),其余继承", func() {
	ag.SetSkills([]agent_entity.AgentSkillItem{
		{ID: "superpowers@claude-plugins-official", Enabled: true}, // 强制开
		{ID: "frontend-design@claude-plugins-official", Enabled: false}, // 强制关(全局开的也能关)
	})
	m, err := s.EnabledPluginsMap(context.Background(), 1)
	So(err, ShouldBeNil)
	So(m["superpowers@claude-plugins-official"], ShouldBeTrue)
	val, hasFD := m["frontend-design@claude-plugins-official"]
	So(hasFD, ShouldBeTrue)
	So(val, ShouldBeFalse)
	_, hasOpsctl := m["opsctl@opskat"] // 已装、未覆盖 → 不在 map(继承全局)
	So(hasOpsctl, ShouldBeFalse)
})
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `GOWORK=off go test ./internal/service/skill_svc/ -run TestListAgentSkillPacks -v`
Expected: FAIL(当前实现给 `opsctl` 发了 `false`,`hasOpsctl` 应为 true → 断言 false 失败;且不含 frontend-design)。

- [ ] **Step 3: 翻转实现**

`internal/service/skill_svc/skill.go`,把整个 `EnabledPluginsMap` 替换为:

```go
// EnabledPluginsMap 返回该 agent 的显式覆盖(强制开=true / 强制关=false)。
// 其余(含全局已开但未覆盖)不出现在 map → CLI 沿用全局 enabledPlugins,实现继承。
func (s *Service) EnabledPluginsMap(ctx context.Context, agentID int64) (map[string]bool, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, it := range a.GetSkills() {
		out[it.ID] = it.Enabled
	}
	return out, nil
}
```

> 不再调 `discover`。stale/未安装的覆盖原样下发,CLI 自行忽略(无害)。

- [ ] **Step 4: 运行测试,确认通过**

Run: `GOWORK=off go test ./internal/service/skill_svc/... -v`
Expected: PASS。

- [ ] **Step 5: 跑相邻包确认没踩到注入接缝**

Run: `GOWORK=off go test ./internal/service/chat_svc/... ./internal/pkg/agentruntime/runtimes/claudecode/... -count=1`
Expected: PASS(`buildSkillsSettings`/`turn_skills` 未改,行为=空 map 时继承)。

- [ ] **Step 6: Commit**

```bash
git add internal/service/skill_svc/skill.go internal/service/skill_svc/skill_test.go
git commit -m "♻️ skill_svc: EnabledPluginsMap 只下发覆盖(继承全局,推翻默认拒绝)"
```

---

## Task 4: 重新生成 Wails 绑定

**Files:**
- Regenerate: `frontend/wailsjs/go/models.ts:5363-5383`(`SkillPackDTO`)

- [ ] **Step 1: 生成**

Run(worktree 里需先占位 dist,见前置条件 2): `make generate`
Expected: 无错误。

- [ ] **Step 2: 确认绑定含新字段**

Run: `grep -n "globallyEnabled" frontend/wailsjs/go/models.ts`
Expected: 在 `class SkillPackDTO` 内出现 `this.globallyEnabled = source["globallyEnabled"];`(及字段声明)。

- [ ] **Step 3: Commit**

```bash
git add frontend/wailsjs/go/models.ts
git commit -m "🔧 generate: SkillPackDTO.globallyEnabled 绑定"
```

---

## Task 5: TriStateToggle 组件(继承|开|关)

**Files:**
- Modify: `frontend/src/components/agentre/capability/catalog.ts:1-26`
- Create: `frontend/src/components/agentre/capability/tri-state-toggle.tsx`
- Test: `frontend/src/components/agentre/capability/__tests__/tri-state-toggle.test.tsx`

- [ ] **Step 1: 在 catalog.ts 定义 TriState + 扩展 CatalogItem**

`frontend/src/components/agentre/capability/catalog.ts`,在 `CatalogItem` 上方加类型,并给 `CatalogItem` 加两个可选字段:

```ts
export type TriState = "inherit" | "on" | "off";
```

在 `CatalogItem` 内 `enabled: boolean;` 之后追加:

```ts
  // 三态(技能用):存在 = 该行渲染「继承|开|关」分段控件而非单选框。
  state?: TriState;
  globallyEnabled?: boolean; // 全局是否已启用(用于分组/文案)
```

- [ ] **Step 2: 写失败测试**

`frontend/src/components/agentre/capability/__tests__/tri-state-toggle.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { TriStateToggle } from "../tri-state-toggle";

describe("TriStateToggle", () => {
  it("marks the active segment via aria-pressed", () => {
    render(
      <TriStateToggle
        value="off"
        labels={{ inherit: "继承", on: "开", off: "关" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByRole("button", { name: "关" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "继承" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("calls onChange with the clicked state", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TriStateToggle
        value="inherit"
        labels={{ inherit: "继承", on: "开", off: "关" }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "开" }));
    expect(onChange).toHaveBeenCalledWith("on");
  });
});
```

- [ ] **Step 3: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/tri-state-toggle.test.tsx`
Expected: FAIL(`Cannot find module ../tri-state-toggle`)。

- [ ] **Step 4: 实现组件**

`frontend/src/components/agentre/capability/tri-state-toggle.tsx`:

```tsx
import { cn } from "@/lib/utils";

import type { TriState } from "./catalog";

type Props = {
  value: TriState;
  labels: Record<TriState, string>; // 已 t() 解析
  onChange: (next: TriState) => void;
};

const order: TriState[] = ["inherit", "on", "off"];

const activeClass: Record<TriState, string> = {
  inherit: "bg-card text-foreground border border-border",
  on: "bg-primary text-primary-foreground",
  off: "bg-destructive text-white",
};

export function TriStateToggle(props: Props) {
  return (
    <div className="inline-flex shrink-0 items-center gap-0.5 rounded-md bg-secondary p-0.5">
      {order.map((s) => {
        const active = props.value === s;
        return (
          <button
            key={s}
            type="button"
            aria-pressed={active}
            aria-label={props.labels[s]}
            onClick={() => props.onChange(s)}
            className={cn(
              "rounded-[5px] px-2.5 py-1 font-mono text-2xs transition-colors",
              active
                ? activeClass[s]
                : "text-muted-foreground hover:text-foreground",
              active && s !== "inherit" && "font-semibold",
            )}
          >
            {props.labels[s]}
          </button>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 5: 运行测试,确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/tri-state-toggle.test.tsx`
Expected: PASS(2 个用例)。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/capability/catalog.ts frontend/src/components/agentre/capability/tri-state-toggle.tsx frontend/src/components/agentre/capability/__tests__/tri-state-toggle.test.tsx
git commit -m "✨ capability: TriStateToggle 三态分段控件(继承|开|关)+ CatalogItem.state"
```

---

## Task 6: CapabilityPicker 支持三态行

**Files:**
- Modify: `frontend/src/components/agentre/capability/capability-picker.tsx:15-27`, `:52`, `:139-199`, `:210-221`
- Test: `frontend/src/components/agentre/capability/__tests__/capability-picker.test.tsx`

- [ ] **Step 1: 写失败测试 —— 三态行渲染分段控件并回调 onSetState**

`frontend/src/components/agentre/capability/__tests__/capability-picker.test.tsx`,新增一个用例(放在文件内 `describe` 块里):

```tsx
it("renders a tri-state toggle for items with state and calls onSetState", async () => {
  const user = userEvent.setup();
  const onSetState = vi.fn();
  render(
    <CapabilityPicker
      open
      title="管理技能"
      searchPlaceholder="搜索"
      items={[
        {
          id: "sp@m",
          name: "superpowers",
          description: "d",
          group: "继承",
          enabled: true,
          state: "inherit",
          globallyEnabled: true,
          triLabels: { inherit: "继承", on: "开", off: "关" },
        },
      ]}
      onToggle={() => {}}
      onSetState={onSetState}
      onConfirm={() => {}}
      onCancel={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "关" }));
  expect(onSetState).toHaveBeenCalledWith("sp@m", "off");
});
```

> 若文件顶部没 import `userEvent`/`vi`,补上(参考 granted-chips.test.tsx 的 import）。`triLabels` 经 props 传入(见 Step 3),避免组件内再调 i18n。

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/capability-picker.test.tsx`
Expected: FAIL(没有 onSetState / 三态控件,点不到「关」)。

- [ ] **Step 3: 给 Props 加 onSetState / triLabels,并按 item.state 分支渲染行**

`frontend/src/components/agentre/capability/capability-picker.tsx`:

1. import 三态控件与类型(顶部):

```tsx
import { TriStateToggle } from "./tri-state-toggle";
import {
  groupCatalogItems,
  type CatalogBadgeTone,
  type CatalogItem,
  type TriState,
} from "./catalog";
```

2. `Props` 里 `onToggle` 之后加:

```tsx
  onSetState?: (id: string, next: TriState) => void;
  triLabels?: Record<TriState, string>; // 已 t() 解析,三态行用
  footerSummary?: string; // 已 t() 解析,替代默认"已选 N"
```

3. 行渲染(`:139-199` 的 `g.items.map((it) => {...})`)替换为按 `it.state` 分支:

```tsx
{g.items.map((it) => {
  const disabled = Boolean(it.disabledReason);
  if (it.state && props.triLabels) {
    return (
      <div
        key={it.id}
        className={cn(
          "flex items-center gap-2.5 rounded-md border border-border bg-card px-2.5 py-2",
          disabled && "opacity-50",
        )}
      >
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-xs font-semibold">{it.name}</span>
          <span className="truncate font-mono text-2xs text-muted-foreground">
            {it.description}
          </span>
        </span>
        {it.badges && it.badges.length > 0 && (
          <span className="flex shrink-0 items-center gap-1">
            {it.badges.map((b) => (
              <span
                key={b.label}
                className={cn(
                  "rounded px-1.5 py-0.5 font-mono text-2xs font-semibold",
                  badgeToneClass[b.tone],
                )}
              >
                {b.label}
              </span>
            ))}
          </span>
        )}
        {disabled ? (
          <span className="shrink-0 font-mono text-2xs text-muted-foreground">
            {it.disabledReason}
          </span>
        ) : (
          <TriStateToggle
            value={it.state}
            labels={props.triLabels}
            onChange={(next) => props.onSetState?.(it.id, next)}
          />
        )}
      </div>
    );
  }
  return (
    <button
      key={it.id}
      type="button"
      role="checkbox"
      aria-checked={it.enabled}
      aria-label={it.name}
      disabled={disabled}
      onClick={() => !disabled && props.onToggle(it.id)}
      className={cn(
        "flex items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-colors",
        it.enabled
          ? "border-primary/30 bg-primary-soft/40"
          : "border-border bg-card hover:bg-secondary/40",
        disabled && "opacity-50",
      )}
    >
      <span
        className={cn(
          "flex size-4 shrink-0 items-center justify-center rounded-[5px] border",
          it.enabled
            ? "border-primary bg-primary text-primary-foreground"
            : "border-border bg-card",
        )}
        aria-hidden="true"
      >
        {it.enabled && <span className="text-[10px]">✓</span>}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-xs font-semibold">{it.name}</span>
        <span className="truncate font-mono text-2xs text-muted-foreground">
          {it.description}
        </span>
      </span>
      {it.badges && it.badges.length > 0 && (
        <span className="flex shrink-0 items-center gap-1">
          {it.badges.map((b) => (
            <span
              key={b.label}
              className={cn(
                "rounded px-1.5 py-0.5 font-mono text-2xs font-semibold",
                badgeToneClass[b.tone],
              )}
            >
              {b.label}
            </span>
          ))}
        </span>
      )}
      {it.disabledReason && (
        <span className="shrink-0 font-mono text-2xs text-muted-foreground">
          {it.disabledReason}
        </span>
      )}
    </button>
  );
})}
```

4. footer 文案(`:212-214`)支持 `footerSummary`:

```tsx
<span className="flex-1 font-mono text-2xs text-muted-foreground">
  {props.footerSummary ??
    t("capability.picker.selected", { count: selectedCount })}
</span>
```

- [ ] **Step 4: 运行测试,确认通过(含原有用例不回归)**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/capability-picker.test.tsx`
Expected: PASS(新用例 + 原 checkbox 用例都过)。

- [ ] **Step 5: tsc 校验(noUnusedLocals 易踩)**

Run: `cd frontend && pnpm exec tsc -b`
Expected: 0 错误。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/capability/capability-picker.tsx frontend/src/components/agentre/capability/__tests__/capability-picker.test.tsx
git commit -m "✨ capability: CapabilityPicker 支持三态行(技能)+ footerSummary"
```

---

## Task 7: GrantedChips 三态芯片(继承/开/关)

**Files:**
- Modify: `frontend/src/components/agentre/capability/granted-chips.tsx:5-10`, `:48-74`
- Test: `frontend/src/components/agentre/capability/__tests__/granted-chips.test.tsx`

- [ ] **Step 1: 写失败测试 —— 继承芯片无移除按钮、强制关有删除线**

`frontend/src/components/agentre/capability/__tests__/granted-chips.test.tsx`,新增用例:

```tsx
it("inherited chips have no remove button; off chips are struck through", () => {
  render(
    <GrantedChips
      title="技能"
      chipIcon={Boxes}
      chips={[
        { id: "i@m", label: "inherited-pack", tone: "inherit", locked: true },
        { id: "off@m", label: "off-pack", tone: "off" },
      ]}
      addLabel="管理技能"
      removeLabel={(name) => `移除 ${name}`}
      onRemove={() => {}}
      onAdd={() => {}}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "移除 inherited-pack" }),
  ).toBeNull();
  expect(
    screen.getByRole("button", { name: "移除 off-pack" }),
  ).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/granted-chips.test.tsx`
Expected: FAIL(目前每个 chip 都有移除按钮)。

- [ ] **Step 3: 扩展 GrantedChip 类型与渲染**

`frontend/src/components/agentre/capability/granted-chips.tsx`:

1. 顶部 import 加类型(已有 lucide import):无新增 import。`GrantedChip` 类型改为:

```tsx
export type GrantedChip = {
  id: string;
  label: string;
  count?: number;
  badge?: string;
  tone?: "inherit" | "on" | "off"; // 默认 on
  locked?: boolean; // true = 不可移除(继承)
};
```

2. chip 渲染(`:48-74` 的 `props.chips.map`)替换为:

```tsx
{props.chips.map((chip) => {
  const tone = chip.tone ?? "on";
  const toneClass =
    tone === "inherit"
      ? "border-border bg-secondary/60 text-muted-foreground"
      : tone === "off"
        ? "border-destructive/30 bg-destructive/10 text-destructive"
        : "border-border bg-card";
  const iconClass =
    tone === "off" ? "text-destructive" : "text-primary-text";
  return (
    <span
      key={chip.id}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2 py-1",
        toneClass,
      )}
    >
      <Icon className={cn("size-3", iconClass)} aria-hidden="true" />
      <span
        className={cn(
          "font-mono text-2xs font-medium",
          tone === "off" && "line-through",
        )}
      >
        {chip.label}
      </span>
      {typeof chip.count === "number" && (
        <span className="rounded bg-secondary px-1 font-mono text-2xs text-muted-foreground">
          {chip.count}
        </span>
      )}
      {chip.badge && (
        <span className="rounded bg-status-waiting-bg px-1 font-mono text-2xs text-status-waiting">
          {chip.badge}
        </span>
      )}
      {!chip.locked && (
        <button
          type="button"
          aria-label={props.removeLabel(chip.label)}
          onClick={() => props.onRemove(chip.id)}
          className="text-muted-foreground hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      )}
    </span>
  );
})}
```

> 顶部需有 `import { cn } from "@/lib/utils";`(已存在)。

- [ ] **Step 4: 运行测试,确认通过(原用例不回归)**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/granted-chips.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/capability/granted-chips.tsx frontend/src/components/agentre/capability/__tests__/granted-chips.test.tsx
git commit -m "✨ capability: GrantedChips 三态芯片(继承灰锁/强制开/强制关删除线)"
```

---

## Task 8: skill-catalog 按 globallyEnabled 分组(三态目录项)

**Files:**
- Modify: `frontend/src/components/agentre/org/skill-catalog.ts:1-52`
- Test: `frontend/src/components/agentre/org/__tests__/skill-catalog.test.ts`

- [ ] **Step 1: 改写测试 —— 三组 + 三态 control + needInstall**

`frontend/src/components/agentre/org/__tests__/skill-catalog.test.ts`,整体替换 `describe` 内三个用例为:

```ts
describe("skillPacksToCatalog", () => {
  it("globally-enabled pack → inherited group, tri-state, with global-enabled note", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ installed: true, globallyEnabled: true })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.inheritedOn"));
    expect(it0.state).toBe("inherit");
    expect(it0.globallyEnabled).toBe(true);
    expect(it0.description).toContain(t("org.agent.skillCatalog.globalEnabled"));
    expect(it0.disabledReason).toBeUndefined();
  });

  it("installed but globally-off pack → enableable group", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ installed: true, globallyEnabled: false })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.enableable"));
    expect(it0.state).toBe("inherit");
    expect(it0.description).toContain(
      t("org.agent.skillCatalog.globalDisabled"),
    );
    expect(it0.disabledReason).toBeUndefined();
  });

  it("not-installed pack → available group, disabled with needInstall", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ source: "available", installed: false, globallyEnabled: false })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.available"));
    expect(it0.disabledReason).toBe(t("org.agent.skillCatalog.needInstall"));
    expect(it0.state).toBeUndefined(); // 未安装行不给三态(走禁用展示)
  });
});
```

并把顶部 `pack` 工厂加上 `globallyEnabled: false` 默认:

```ts
const pack = (over: Record<string, unknown> = {}) => ({
  id: "x@m",
  name: "x",
  description: "d",
  skills: ["a", "b"],
  source: "installed",
  recommended: false,
  installed: true,
  enabled: false,
  globallyEnabled: false,
  ...over,
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/skill-catalog.test.ts`
Expected: FAIL(旧实现按 recommended/installed 分组,无 state/globallyEnabled)。

- [ ] **Step 3: 重写 skillPacksToCatalog**

`frontend/src/components/agentre/org/skill-catalog.ts` 整体替换为:

```ts
import type { TFunction } from "i18next";

import type { CatalogItem } from "../capability/catalog";
import type { skill_svc } from "../../../../wailsjs/go/models";

// skillPacksToCatalog 把后端 SkillPackDTO[] 适配为 CapabilityPicker 的 CatalogItem[]。
// 分组按"继承(全局已开)/ 可启用(已安装·全局未开)/ 可安装(未装)"。
// 已安装的行给 state（三态由 org-detail 用本地授权 overlay 覆盖），未安装行禁用。
export function skillPacksToCatalog(
  packs: skill_svc.SkillPackDTO[],
  t: TFunction,
): CatalogItem[] {
  return packs.map((p) => {
    const globalNote = p.globallyEnabled
      ? t("org.agent.skillCatalog.globalEnabled")
      : t("org.agent.skillCatalog.globalDisabled");
    const group = !p.installed
      ? t("org.agent.skillCatalog.group.available")
      : p.globallyEnabled
        ? t("org.agent.skillCatalog.group.inheritedOn")
        : t("org.agent.skillCatalog.group.enableable");
    return {
      id: p.id,
      name: p.name,
      description: p.installed
        ? `${p.description} · ${globalNote}`
        : p.description,
      contents: p.skills ?? [],
      group,
      enabled: p.enabled,
      globallyEnabled: p.globallyEnabled,
      state: p.installed ? "inherit" : undefined,
      disabledReason: p.installed
        ? undefined
        : t("org.agent.skillCatalog.needInstall"),
    };
  });
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/skill-catalog.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/org/skill-catalog.ts frontend/src/components/agentre/org/__tests__/skill-catalog.test.ts
git commit -m "✨ org: skillPacksToCatalog 按 globallyEnabled 分组 + 三态目录项"
```

---

## Task 9: useSkillCatalog 挂载即拉

**Files:**
- Modify: `frontend/src/components/agentre/org/use-skill-catalog.ts:9-37`
- Test: `frontend/src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`

- [ ] **Step 1: 写失败测试 —— autoLoad 时 mount 即调一次**

`frontend/src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`,新增用例(沿用该文件已有的 ListAgentSkillPacks mock 方式;若文件用 `vi.mock("@/../wailsjs/go/app/App")`,在其基础上断言调用次数):

```tsx
it("auto-loads once on mount when autoLoad is true", async () => {
  renderHook(() => useSkillCatalog(7, true));
  await waitFor(() => expect(ListAgentSkillPacks).toHaveBeenCalledTimes(1));
  expect(ListAgentSkillPacks).toHaveBeenCalledWith(7, false);
});
```

> import：`renderHook, waitFor` from `@testing-library/react`;`ListAgentSkillPacks` 取自该文件已有的 mock import。

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`
Expected: FAIL(当前不接收 autoLoad,mount 不自动拉)。

- [ ] **Step 3: 加 autoLoad 参数 + mount effect**

`frontend/src/components/agentre/org/use-skill-catalog.ts`,签名与体改为:

```ts
export function useSkillCatalog(agentId: number, autoLoad = false) {
  const { t } = useTranslation();
  const [items, setItems] = React.useState<CatalogItem[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [fetched, setFetched] = React.useState(false);

  const load = React.useCallback(
    async (refresh: boolean) => {
      setLoading(true);
      setError(null);
      try {
        const resp = await ListAgentSkillPacks(agentId, refresh);
        setItems(skillPacksToCatalog(resp?.packs ?? [], t));
        setFetched(true);
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setLoading(false);
      }
    },
    [agentId, t],
  );

  React.useEffect(() => {
    if (autoLoad) void load(false);
  }, [autoLoad, load]);

  return { items, loading, error, fetched, load, reload: () => load(false) };
}
```

并把文件顶注释从"不在 mount 时自动拉"更新为"`autoLoad` 时挂载即拉一次(技能区可见时用)"。

- [ ] **Step 4: 运行测试,确认通过(原懒加载用例不回归)**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/org/use-skill-catalog.ts frontend/src/components/agentre/org/__tests__/use-skill-catalog.test.tsx
git commit -m "✨ org: useSkillCatalog 支持 autoLoad(技能区挂载即拉)"
```

---

## Task 10: org-detail-agent 接线三态(setter / overlay / 芯片 / 计数)

**Files:**
- Modify: `frontend/src/components/agentre/org/org-detail-agent.tsx:136-233`, `:473-531`
- Test: `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`

- [ ] **Step 1: 写失败测试 —— 三态切换 upsert/移除,继承芯片只读**

`frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`,新增用例(沿用该文件已有的 `withCaps([...])` shim 与 `ListAgentSkillPacks` mock;给一个 globally-on 包 + 一个 globally-off 包):

```tsx
it("inherited globally-on pack shows as locked chip; toggling an off pack records enabled:false", async () => {
  const user = userEvent.setup();
  withCaps(["skills"]);
  // mock 目录:superpowers 全局开,ui-ux 全局关
  mockSkillPacks([
    { id: "sp@m", name: "superpowers", description: "d", skills: ["a"], source: "installed", recommended: false, installed: true, enabled: false, globallyEnabled: true },
    { id: "ui@m", name: "ui-ux-pro-max", description: "d", skills: ["b"], source: "installed", recommended: false, installed: true, enabled: false, globallyEnabled: false },
  ]);
  const onUpdate = vi.fn().mockResolvedValue(undefined);
  renderAgentDetail({ onUpdate }); // 该文件已有的渲染辅助
  // 继承芯片可见且不可移除
  await screen.findByText("superpowers");
  expect(screen.queryByRole("button", { name: /移除 superpowers|remove superpowers/i })).toBeNull();
  // 打开弹窗,把 ui-ux 切到「关」
  await user.click(screen.getByRole("button", { name: /管理技能|manage skills/i }));
  await user.click((await screen.findAllByRole("button", { name: /关|off/i }))[0]);
  await user.click(screen.getByRole("button", { name: /完成|done/i }));
  await user.click(screen.getByRole("button", { name: /保存|save/i }));
  const req = onUpdate.mock.calls[0][0];
  expect(req.skills).toContainEqual({ id: "ui@m", enabled: false });
});
```

> 具体辅助函数名(`renderAgentDetail`/`mockSkillPacks`/`withCaps`)以该测试文件现有写法为准;若没有就按文件现有 render/ mock 模式内联。断言核心:继承包无移除键 + 切「关」后 `skills` 含 `{id,enabled:false}`。

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: FAIL(当前弹窗是单选框、无继承芯片、`toggleSkillGrant` 只能开/移除)。

- [ ] **Step 3: 改 org-detail-agent.tsx 接线**

`frontend/src/components/agentre/org/org-detail-agent.tsx`:

1. import 加 `TriState`:

```tsx
import { CapabilityPicker } from "../capability/capability-picker";
import { GrantedChips, type GrantedChip } from "../capability/granted-chips";
import type { TriState } from "../capability/catalog";
```

2. 目录 hook 改挂载即拉(`:137`),autoLoad 跟门控:

```tsx
const skillsCapOn = caps?.has("skills") ?? false;
const skillCatalog = useSkillCatalog(props.agent.id, skillsCapOn);
```

3. 替换三态相关逻辑(原 `:193-233` 的 idToName/skillCountById/skillChips/localEnabled/pickerItems/openSkillPicker/toggleSkillGrant/removeSkill)为:

```tsx
const idToName = (id: string) => id.split("@")[0] || id;
const skillCountById = React.useMemo(() => {
  const m = new Map<string, number>();
  for (const it of skillCatalog.items) m.set(it.id, it.contents?.length ?? 0);
  return m;
}, [skillCatalog.items]);
const globallyOn = React.useMemo(
  () =>
    new Set(
      skillCatalog.items.filter((i) => i.globallyEnabled).map((i) => i.id),
    ),
  [skillCatalog.items],
);

const skillStateOf = (id: string): TriState => {
  const s = skills.find((x) => x.id === id);
  if (!s) return "inherit";
  return s.enabled ? "on" : "off";
};
const setSkillState = (id: string, next: TriState) =>
  setSkills((prev) => {
    const rest = prev.filter((s) => s.id !== id);
    if (next === "inherit") return rest;
    return [
      ...rest,
      department_svc.AgentSkillDTO.createFrom({ id, enabled: next === "on" }),
    ];
  });

const triLabels: Record<TriState, string> = {
  inherit: t("capability.triState.inherit"),
  on: t("capability.triState.on"),
  off: t("capability.triState.off"),
};

// 芯片:继承(全局开且未覆盖,灰锁)+ 强制开(本地 true)+ 强制关(本地 false)
const onSkills = skills.filter((s) => s.enabled);
const offSkills = skills.filter((s) => !s.enabled);
const overriddenIds = new Set(skills.map((s) => s.id));
const inheritedIds = [...globallyOn].filter((id) => !overriddenIds.has(id));
const skillChips: GrantedChip[] = [
  ...inheritedIds.map((id) => ({
    id,
    label: idToName(id),
    count: skillCountById.get(id),
    tone: "inherit" as const,
    locked: true,
  })),
  ...onSkills.map((s) => ({
    id: s.id,
    label: idToName(s.id),
    count: skillCountById.get(s.id),
    tone: "on" as const,
  })),
  ...offSkills.map((s) => ({
    id: s.id,
    label: idToName(s.id),
    count: skillCountById.get(s.id),
    tone: "off" as const,
  })),
];

const pickerItems = skillCatalog.items.map((it) => ({
  ...it,
  state: it.state ? skillStateOf(it.id) : undefined,
}));

const openSkillPicker = () => {
  setSkillPickerOpen(true);
  if (!skillCatalog.fetched) void skillCatalog.load(false);
};
const removeSkillOverride = (id: string) =>
  setSkills((prev) => prev.filter((s) => s.id !== id));
```

4. 技能区 JSX(`:476-502`)的 `GrantedChips` + `CapabilityPicker` 改为:

```tsx
<GrantedChips
  title={t("org.agent.skills.sectionTitle")}
  countLabel={t("org.agent.skills.count", {
    inherit: inheritedIds.length,
    on: onSkills.length,
    off: offSkills.length,
  })}
  chipIcon={Boxes}
  chips={skillChips}
  addLabel={t("org.agent.skills.manage")}
  removeLabel={(name) => t("capability.picker.remove", { name })}
  onRemove={removeSkillOverride}
  onAdd={openSkillPicker}
  emptyLabel={t("org.agent.skills.empty")}
  footerNote={t("org.agent.skills.inheritNote")}
/>
<CapabilityPicker
  open={skillPickerOpen}
  title={t("org.agent.skillPicker.title")}
  subtitle={t("org.agent.skillPicker.subtitle")}
  searchPlaceholder={t("org.agent.skillPicker.searchPlaceholder")}
  items={pickerItems}
  loading={skillCatalog.loading}
  triLabels={triLabels}
  footerSummary={t("org.agent.skills.count", {
    inherit: inheritedIds.length,
    on: onSkills.length,
    off: offSkills.length,
  })}
  footerNote={t("org.agent.skillPicker.personalNote")}
  onToggle={() => {}}
  onSetState={setSkillState}
  onConfirm={() => setSkillPickerOpen(false)}
  onCancel={() => setSkillPickerOpen(false)}
  onRescan={() => void skillCatalog.load(true)}
/>
```

> `onToggle={() => {}}` 占位:技能行走 `onSetState`,不会触发 `onToggle`(仍是必填 prop)。

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: PASS。

- [ ] **Step 5: tsc 校验**

Run: `cd frontend && pnpm exec tsc -b`
Expected: 0 错误(注意删掉不再使用的 `localEnabled`/`toggleSkillGrant`/`removeSkill` 等旧符号,否则 noUnusedLocals 断构建)。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/org/org-detail-agent.tsx frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx
git commit -m "✨ org: agent 技能区接线三态(继承芯片/强制开关 setter/挂载即拉/计数)"
```

---

## Task 11: i18n key

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/__tests__/i18n.test.ts`

- [ ] **Step 1: 加 key(两个 locale 同步)**

在两个 `common.json` 的相应命名空间下加(zh-CN 值如下,en 给对应英文):

```jsonc
// capability.triState
"triState": { "inherit": "继承", "on": "开", "off": "关" }
// org.agent.skills.*
"manage": "管理技能",
"count": "继承 {{inherit}} · 强制开 {{on}} · 强制关 {{off}}",
"inheritNote": "灰=继承全局已启用 · 蓝=仅此 agent 强制开 · 红=仅此 agent 强制关(不动全局)。改动下次起新会话生效。",
// org.agent.skillCatalog.*
"group": { "inheritedOn": "继承(全局已开)", "enableable": "可启用(已安装·全局未开)", "available": "可安装" },
"globalEnabled": "全局已启用",
"globalDisabled": "全局未启用"
```

en 对应:`{ "inherit": "Inherit", "on": "On", "off": "Off" }`、`"manage": "Manage skills"`、`"count": "{{inherit}} inherited · {{on}} on · {{off}} off"`、`group: { inheritedOn: "Inherited (on globally)", enableable: "Available to enable (installed)", available: "Installable" }`、`globalEnabled: "Globally enabled"`、`globalDisabled: "Globally disabled"`,note 同义翻译。

> 复核:旧 `org.agent.skillCatalog.group.recommended`/`installed`、`org.agent.skills.add`/`enabledCount`、`org.agent.skillCatalog.badge.*` 若不再被引用则删除(`grep` 确认无引用再删)。`org.agent.skills.sectionTitle`/`empty`/`skillPicker.*`/`personalNote`/`capability.picker.*` 保留。

- [ ] **Step 2: 运行 i18n 测试 + 全量 vitest**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(zh/en key 覆盖一致、静态 `t()` key 都存在)。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 i18n: 技能三态(继承/开/关)+ 继承分组/全局态文案"
```

---

## Task 12: 全量验证

- [ ] **Step 1: 后端受影响包**

Run: `GOWORK=off go test ./internal/pkg/agentskill/... ./internal/service/skill_svc/... ./internal/service/chat_svc/... ./internal/pkg/agentruntime/runtimes/claudecode/... -count=1`
Expected: 全 PASS。

- [ ] **Step 2: 后端 lint**

Run: `make lint`(或 `GOWORK=off golangci-lint run ./internal/...`)
Expected: 0 issues。

- [ ] **Step 3: 前端全量**

Run: `cd frontend && pnpm exec tsc -b && pnpm test && pnpm lint`
Expected: tsc 0 错;vitest 全绿;eslint 0(注意 `i18next/no-literal-string` 不放过硬编码中文)。

- [ ] **Step 4: 真机冒烟(手动,GUI)**

1. 起 `make dev`;开一个 claudecode agent 详情。
2. 技能区芯片应显示:全局已开的插件(灰·继承·不可移除)。
3. 「管理技能」→ 弹窗:继承组各行「继承」高亮;把某全局开的切「关」、某全局关的切「开」;完成 → 保存。
4. 让该 agent 起**新**会话,后台日志/transcript 确认:被关的插件 skill/command 消失、被开的出现,其余继承(参考 spec §3 的 `claude -p ... stream-json --verbose` 观测法)。
5. codex agent 详情:技能区为灰盒门控(不变)。

---

## Self-Review(已核对)

- **Spec 覆盖**:§4.1→T1、§4.3→T2、§4.2→T3、绑定→T4、§5.2→T5/T6、§5.3→T7、§5.1→T8、§5.3 挂载即拉→T9、§5 接线→T10、§6→T11、§7 测试散落各任务 Step1、§9 验证→T12。✓
- **类型一致**:`TriState`(catalog.ts 定义)在 TriStateToggle/CapabilityPicker/org-detail 一致;`GrantedChip.tone` 取值 `inherit|on|off` 与 chips 构造一致;`SkillPackDTO.globallyEnabled`(后端 json tag `globallyEnabled`)与前端 `p.globallyEnabled` 一致;`EnabledPluginsMap` 用 `a.GetSkills()`(实体已有)。✓
- **无占位**:每个代码步给了完整代码与命令。前端测试里 `renderAgentDetail`/`withCaps`/`mockSkillPacks` 标注"以该测试文件现有写法为准"——执行时先读该文件确认辅助函数实际名/形态再落笔。✓
