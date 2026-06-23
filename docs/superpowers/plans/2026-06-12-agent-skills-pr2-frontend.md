# Agent 技能（skill-pack）PR2 · 前端实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**实施状态（2026-06-12，subagent-driven 执行完成）：** 9 个 task 全部实现并过审，commit `19b4dff`→`d4d6333`。最终整体审「READY TO MERGE」。全量校验：`vitest` 1481/1481、`tsc -b` 0 错、`eslint`(capability+org+mock) 0、`i18n.test` 13/13。三处有意偏差（保存走底部统一 Save / 芯片数量懒加载 / 个人 skill 静态文案）按计划落地、文案诚实。`tools.list`/`skills.item`/`skills.list` 确认无引用后删除；`skills.summary` 因 `org-list.tsx` 仍用而保留。真机冒烟与合并待用户决定。

**Goal:** 把 PR1 落地的「按 agent 授权 skill-pack」后端能力接到组织架构 agent 详情面板 —— 技能/工具统一为「已授予芯片（GrantedChips）+ 添加目录弹窗（CapabilityPicker）」交互，技能区按 `CapSkills` 门控、工具区按 `CapMCPTools` 门控，全部走 i18n。

**Architecture:** 在 `components/agentre/capability/` 下新增两个**通用、与具体能力无关**的展示组件（`CapabilityPicker` 弹窗 + `GrantedChips` 芯片区），以一个纯类型 `CatalogItem` 为契约。技能侧用 `org/skill-catalog.ts`（纯映射）+ `org/use-skill-catalog.ts`（懒加载 hook，仅弹窗打开时调 `ListAgentSkillPacks`）把后端 `SkillPackDTO[]` 适配成 `CatalogItem[]`；工具侧用纯映射把 `availableTools: string[]` 适配成 `CatalogItem[]`（无新后端调用）。`org-detail-agent.tsx` 用 `useBackendCapabilities(backendType)` 做门控，并复用上述组件重做技能/工具两区。所有 `wailsjs/go/*` 在 vitest 里走全局 mock alias（见下「测试与构建约束」）。

**Tech Stack:** React 19 + TypeScript, Tailwind v4, shadcn `@/components/ui/*`, react-i18next, lucide-react, Vitest + Testing Library。

---

## 背景与既有事实（实现者必读，避免踩坑）

PR1（后端，已 merge-ready，commit `cdfaa8e`→`e5cd8e7`）已经落地：

- Wails binding：`App.ListAgentSkillPacks(agentID number, refresh boolean): Promise<skill_svc.SkillCatalogDTO>`。
- 生成模型：
  - `skill_svc.SkillPackDTO { id, name, description, skills: string[], source, recommended, installed, enabled }`
  - `skill_svc.SkillCatalogDTO { packs: SkillPackDTO[] }`
  - `department_svc.AgentSkillDTO { id, enabled }`（PR1 把字段从 `label` 改成了 **`id`**）
  - `agent_svc.UpdateAgentRequest.skills: department_svc.AgentSkillDTO[]`、`.tools: department_svc.AgentToolDTO[]`（`{key,enabled}`）
- Capability 字符串（后端 `internal/pkg/agentruntime/capability/capability.go`）：
  - `CapSkills = "skills"` —— **仅 claudecode** 声明
  - `CapMCPTools = "mcp_tools"` —— **claudecode 与 codex 都**声明（builtin/remote 不声明）
- 保存复用既有 `UpdateAgent` binding（**没有**新的 skills 保存路径）。技能授予集 = `agents.skills_json` = `[{id, enabled:true}, …]`，注入用 `GetEnabledPackIDs()`（只取 enabled==true）。

> **⚠️ 既有破坏：** 现有前端 `org-detail-agent.tsx` 仍按 `s.label` 读写技能（line 163-167、439、443、452），而 PR1 已把 DTO 改成 `{id}`。`make generate` 后 `models.ts` 里 `AgentSkillDTO` 只有 `id`，**现有面板对 `.label` 的引用会在 tsc/lint 编译失败**，且 `org-detail-agent.test.tsx` 的 `skills:[{label,…}]` fixture + 「toggle skill」测试也已过时。本 PR 必须一并修复（Task 7）。

### UI 设计稿（锁定）

`~/Desktop/agentry.pen` 帧「⑥ Agent 能力区 v2 · 技能/工具」（plugin 粒度版）。关键视觉：

- **GrantedChips（技能）**：小标题 `技能 · SKILL PACKS` + `{n} 已启用`；每个芯片 = boxes 图标 + pack 名（如 `superpowers`）+ skill 数量 badge（如 `14`）+ ✕ 移除；末尾 `+ 添加技能` 按钮（蓝）；底部只读 `lock` 提示「始终可用 · 个人技能（~/.claude/skills）不可单独开关」。
- **GrantedChips（工具）**：小标题 `工具 · TOOLS` + `{n} 已启用`；芯片 = network 图标 + 工具名 + `需审批` badge + ✕；`+ 添加工具` 按钮。
- **CapabilityPicker（弹窗）**：header（boxes 图标 + 标题 `添加技能 · Skill Packs` + 副标题 + ✕）；search 行（`搜索技能包 / 描述…` 输入框 + `重新扫描` refresh 按钮）；body（按来源分组：`推荐 · AGENTRE 精选` / `已安装 · 发现自此后端` / `可安装 · MARKETPLACE`，每组有计数；每行 = checkbox + 图标 + 名 + 描述 + badge(`推荐`蓝/`已装`绿/`需先安装`橙)；`可安装` 行灰显、checkbox 禁用；底部只读个人技能提示）；footer（`已选 N 个技能包 · …生效` + `取消` + `完成`）。
- **门控（非 claudecode）**：技能区换成灰盒 `ban` 图标 + 「当前 backend 不支持技能」+「技能是 Claude Code 原生能力 · 切换到 Claude Code 后端即可启用」；小标题右侧 `backend 不支持` pill。

### 设计与现实的 3 处偏差（实现按本计划，不照抄 pen）

1. **生效语义文案**：pen 写「关闭即生效」，但本 PR **不**让弹窗「完成」直接落库 —— 弹窗只更新面板本地 `skills` 状态，持久化仍走面板底部统一 `保存`（与现有 name/prompt/tools 编辑一致，单一 `UpdateAgent` 路径）。故 footer 文案用「保存后下次该 agent 起新会话生效」。
2. **芯片上的 skill 数量**：agent 的 `skills_json` 只有 id，没有 skill 数。数量来自目录（`SkillPackDTO.skills`）。目录是**懒加载**（仅弹窗打开时拉）。因此：**首次打开弹窗前**，已授予芯片只显示「由 id 派生的展示名」（`superpowers@mk` → `superpowers`），**不显示**数量 badge；弹窗打开拉过目录后，面板缓存目录、芯片再补显数量。这是渐进增强，不违反「懒加载」。
3. **个人裸 skill 列表**：PR1 的 `SkillCatalogDTO` **没有** personalSkills 字段（后端未实现枚举）。故个人技能提示是**静态 i18n 文案**（不含真实 skill 名）。真实枚举属 spec §7 延后项。

### 测试与构建约束（关键，先读再动手）

- **`make generate` 在本 worktree 可用**（`frontend/dist/.gitkeep` 占位已就位）。需要真实 `wailsjs/`（tsc/eslint/build）时跑 `GOWORK=off make generate`。`wailsjs/` 是 gitignore 生成物，**不提交**。
- **vitest 不需要真实 `wailsjs/`**：`frontend/vite.config.ts` 的 `test.alias` 把所有 `wailsjs/go/app/App` → `src/__tests__/mocks/wailsApp.ts`、`wailsjs/go/models` → `src/__tests__/mocks/wailsModels.ts`。所以：
  - 跑前端测试用 `cd frontend && pnpm test -- <file>`（**不要**用 `make test-frontend`，它会强制先 `generate`）。
  - 组件里调的 binding（如 `GetBackendCapabilities`、`ListAgentSkillPacks`）要在 `wailsApp.ts` mock 里有导出。`GetBackendCapabilities` 已有（默认返回 `{capabilities:[]}`）；**`ListAgentSkillPacks` 需新增**（Task 1）。
  - `wailsApp.ts` 的 mock 用 `windowBackedMock`：测试里可设 `window.go.app.App.<Name> = vi.fn()...` 覆盖单个 binding 的返回（见现有 `llm-providers` 测试用法）。
  - `import type { skill_svc } from "@/../wailsjs/go/models"` 是**纯类型**，vitest 下被 esbuild 擦除，无需在 `wailsModels.ts` 里加运行时值。tsc/eslint 用真实生成的 models（已含 `skill_svc`）。
- **i18n 测试（`src/__tests__/i18n.test.ts`）强约束**（每个 UI task 必须满足）：
  - zh-CN 与 en `common.json` **key 完全对齐**（多一个少一个都 fail）。
  - 源码里每个**静态** `t("a.b.c")`（不含 `${}`）的 key 必须在两个 locale 都存在。
  - 源码里**不允许**硬编码中文字面量（含 JSX 文本、`aria-label`/`title`/`placeholder` 等可见属性）。所有可见文案走 `t()`。
  - 动态 key（如 `` t(`org.agent.tools.names.${key}`) ``）不被静态扫描，但运行时用到的 key 仍要在 locale 里齐全。
- **mock 规则**（见项目记忆 `reference_frontend_wails_runtime_test_mock`）：`wailsjs/go/*` 走全局 vite alias（即上面的 mock 文件），**不要**在测试里再对它加 per-file `vi.mock`，也**不要**改全局 vite alias；只有 `wailsjs/runtime/runtime` 才需 per-file `vi.mock`。本 PR 不碰 runtime。

---

## 文件结构（先看清边界）

**新增：**
- `frontend/src/components/agentre/capability/catalog.ts` —— `CatalogItem`/`CatalogBadge` 类型 + `groupCatalogItems()` 纯分组函数（通用，技能/工具共用）。
- `frontend/src/components/agentre/capability/capability-picker.tsx` —— `<CapabilityPicker>` 通用目录弹窗（dumb：只渲染传入的 `CatalogItem[]` + 回调）。
- `frontend/src/components/agentre/capability/granted-chips.tsx` —— `<GrantedChips>` 通用已授予芯片区（dumb）。
- `frontend/src/components/agentre/org/skill-catalog.ts` —— `skillPacksToCatalog(packs, t)` 纯映射：`SkillPackDTO[]` → `CatalogItem[]`（来源分组 + badge + 禁用态）。
- `frontend/src/components/agentre/org/use-skill-catalog.ts` —— `useSkillCatalog(agentId)` 懒加载 hook（封装 `ListAgentSkillPacks` + 映射 + rescan）。
- `frontend/src/components/agentre/org/tool-catalog.ts` —— `toolKeysToCatalog(keys, agentTools, t)` 纯映射：`string[]` → `CatalogItem[]`（无后端调用）。
- 对应 `__tests__/*.test.ts(x)`。

**修改：**
- `frontend/src/components/agentre/capability/types.ts` —— `Capability` union += `"skills"` `"mcp_tools"`。
- `frontend/src/__tests__/mocks/wailsApp.ts` —— += `ListAgentSkillPacks` mock。
- `frontend/src/components/agentre/org/org-detail-agent.tsx` —— 重做技能区（Task 7）+ 工具区（Task 8）。
- `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx` —— 改 fixture（`label`→`id`）+ 重写技能/工具断言。
- `frontend/src/i18n/locales/zh-CN/common.json` + `.../en/common.json` —— 新增/调整 key。

**不碰：** 任何 `internal/**`（后端 PR1 已完成）、`use-org-data.ts`（保存链路不变）、其它领域组件。

---

## Task 1: 能力 union + binding mock 接通（enablement）

**Files:**
- Modify: `frontend/src/components/agentre/capability/types.ts:4-16`
- Modify: `frontend/src/__tests__/mocks/wailsApp.ts`（在「Organization bindings」段末追加）
- Create/Modify test: `frontend/src/components/agentre/capability/__tests__/types.test.ts`

- [x] **Step 1: 写失败测试**

`frontend/src/components/agentre/capability/__tests__/types.test.ts`：

```ts
import { describe, expect, it } from "vitest";
import { Capabilities } from "../types";

describe("Capabilities skills/mcp_tools membership", () => {
  it("recognizes the skills and mcp_tools capability strings", () => {
    const caps = new Capabilities(new Set(["skills", "mcp_tools"]), {
      allowedModes: [],
      defaultMode: "",
      switchableDuringTurn: false,
      order: [],
    });
    expect(caps.has("skills")).toBe(true);
    expect(caps.has("mcp_tools")).toBe(true);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/types.test.ts`
Expected: FAIL —— tsc/类型层面 `caps.has("skills")` 报「Argument of type '"skills"' is not assignable to parameter of type 'Capability'」（或运行期通过但类型不符；以 lint/tsc 为准）。

- [x] **Step 3: 改 `types.ts` union**

把 `frontend/src/components/agentre/capability/types.ts` 的 `Capability` union 末尾两项之间补上：

```ts
export type Capability =
  | "steer"
  | "cancel_steer"
  | "drain_steer"
  | "abort"
  | "image_input"
  | "set_permission_mode"
  | "answer_user_ask"
  | "tool_permission_gate"
  | "fork_session"
  | "report_context_window"
  | "compact"
  | "goal"
  | "mcp_tools"
  | "skills";
```

- [x] **Step 4: 给 `wailsApp.ts` mock 加 `ListAgentSkillPacks`**

在 `frontend/src/__tests__/mocks/wailsApp.ts` 的「Organization bindings」段（`DeleteAgentAvatar` 之后）追加：

```ts
export const ListAgentSkillPacks = windowBackedMock("ListAgentSkillPacks", () =>
  Promise.resolve({ packs: [] }),
);
```

- [x] **Step 5: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/types.test.ts`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/capability/types.ts \
        frontend/src/components/agentre/capability/__tests__/types.test.ts \
        frontend/src/__tests__/mocks/wailsApp.ts
git commit -m "✨ capability: 前端 union 加 skills/mcp_tools + ListAgentSkillPacks mock"
```

---

## Task 2: `CatalogItem` 类型 + `groupCatalogItems` 纯分组

**Files:**
- Create: `frontend/src/components/agentre/capability/catalog.ts`
- Test: `frontend/src/components/agentre/capability/__tests__/catalog.test.ts`

- [x] **Step 1: 写失败测试**

```ts
import { describe, expect, it } from "vitest";
import { groupCatalogItems, type CatalogItem } from "../catalog";

const item = (id: string, group: string): CatalogItem => ({
  id,
  name: id,
  description: "",
  group,
  enabled: false,
});

describe("groupCatalogItems", () => {
  it("groups by group label preserving first-seen order", () => {
    const groups = groupCatalogItems([
      item("a", "推荐"),
      item("b", "已安装"),
      item("c", "推荐"),
    ]);
    expect(groups.map((g) => g.group)).toEqual(["推荐", "已安装"]);
    expect(groups[0].items.map((i) => i.id)).toEqual(["a", "c"]);
    expect(groups[1].items.map((i) => i.id)).toEqual(["b"]);
  });

  it("puts items without a group under empty key in trailing order", () => {
    const groups = groupCatalogItems([item("a", ""), item("b", "推荐")]);
    expect(groups.map((g) => g.group)).toEqual(["", "推荐"]);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/catalog.test.ts`
Expected: FAIL —— `Cannot find module '../catalog'`。

- [x] **Step 3: 写实现**

`frontend/src/components/agentre/capability/catalog.ts`：

```ts
// CatalogItem 是「技能/工具添加弹窗」的统一契约（技能 pack 或工具 key）。
// 所有展示文案（name/description/group/badge.label/disabledReason）都应是
// 已本地化的字符串 —— 由消费者侧的映射函数用 t() 解析后填入，
// CapabilityPicker 只负责渲染，不再调 i18n。
export type CatalogBadgeTone =
  | "recommended"
  | "installed"
  | "available"
  | "approval"
  | "needInstall";

export type CatalogBadge = {
  label: string;
  tone: CatalogBadgeTone;
};

export type CatalogItem = {
  id: string; // pack id 或 tool key，全局唯一
  name: string;
  description: string;
  contents?: string[]; // pack 内 skill 名（可选，用于数量/展开）
  group: string; // 已本地化的分组标题（"" = 无分组）
  badges?: CatalogBadge[];
  enabled: boolean; // 当前是否已授予/勾选
  disabledReason?: string; // 非空 = 该行禁用、不可勾选（如「需先安装」）
};

export type CatalogGroup = {
  group: string;
  items: CatalogItem[];
};

// groupCatalogItems 按 group 字段聚合，组顺序 = 组首次出现的顺序，
// 组内顺序 = 原始顺序。纯函数，便于表测。
export function groupCatalogItems(items: CatalogItem[]): CatalogGroup[] {
  const order: string[] = [];
  const byGroup = new Map<string, CatalogItem[]>();
  for (const item of items) {
    const key = item.group ?? "";
    if (!byGroup.has(key)) {
      byGroup.set(key, []);
      order.push(key);
    }
    byGroup.get(key)!.push(item);
  }
  return order.map((group) => ({ group, items: byGroup.get(group)! }));
}
```

- [x] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/catalog.test.ts`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/capability/catalog.ts \
        frontend/src/components/agentre/capability/__tests__/catalog.test.ts
git commit -m "✨ capability: CatalogItem 契约 + groupCatalogItems 纯分组"
```

---

## Task 3: `<CapabilityPicker>` 通用目录弹窗

**Files:**
- Create: `frontend/src/components/agentre/capability/capability-picker.tsx`
- Test: `frontend/src/components/agentre/capability/__tests__/capability-picker.test.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`（新增 `capability.picker.*` 通用 chrome key）

> 该组件是 dumb：所有可变文案（title/subtitle/searchPlaceholder）由 props 传入（消费者已 t() 解析）；只有自身通用 chrome（重新扫描/取消/完成/已选 N/加载中/无匹配）走 `capability.picker.*` 静态 key。

- [x] **Step 1: 先加 i18n key（两 locale 同步）**

在 `zh-CN/common.json` 顶层加（若已有 `capability` 段则并入）：

```json
"capability": {
  "picker": {
    "rescan": "重新扫描",
    "done": "完成",
    "loading": "加载中…",
    "empty": "暂无可用项",
    "searchEmpty": "无匹配项",
    "selected": "已选 {{count}} 项 · 保存后下次起新会话生效",
    "remove": "移除 {{name}}",
    "close": "关闭"
  }
}
```

`en/common.json` 同结构：

```json
"capability": {
  "picker": {
    "rescan": "Rescan",
    "done": "Done",
    "loading": "Loading…",
    "empty": "Nothing available",
    "searchEmpty": "No matches",
    "selected": "{{count}} selected · applies on the agent's next new session after save",
    "remove": "Remove {{name}}",
    "close": "Close"
  }
}
```

- [x] **Step 2: 写失败测试**

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { CapabilityPicker } from "../capability-picker";
import type { CatalogItem } from "../catalog";

const items: CatalogItem[] = [
  {
    id: "sp@m",
    name: "superpowers",
    description: "TDD / 调试 / 计划",
    group: "推荐 · AGENTRE 精选",
    badges: [{ label: "推荐", tone: "recommended" }],
    enabled: true,
  },
  {
    id: "fd@m",
    name: "frontend-design",
    description: "产品级前端设计",
    group: "已安装 · 发现自此后端",
    badges: [{ label: "已装", tone: "installed" }],
    enabled: false,
  },
  {
    id: "by@m",
    name: "baoyu-skills",
    description: "来自 marketplace",
    group: "可安装 · MARKETPLACE",
    enabled: false,
    disabledReason: "需先安装",
  },
];

function setup(extra: Partial<React.ComponentProps<typeof CapabilityPicker>> = {}) {
  const onToggle = vi.fn();
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  const onRescan = vi.fn();
  render(
    <CapabilityPicker
      open
      title="添加技能 · Skill Packs"
      subtitle="一个包 = 一组 skill"
      searchPlaceholder="搜索技能包 / 描述…"
      items={items}
      onToggle={onToggle}
      onConfirm={onConfirm}
      onCancel={onCancel}
      onRescan={onRescan}
      {...extra}
    />,
  );
  return { onToggle, onConfirm, onCancel, onRescan };
}

describe("CapabilityPicker", () => {
  it("renders items grouped by source with group headers", () => {
    setup();
    expect(screen.getByText("推荐 · AGENTRE 精选")).toBeInTheDocument();
    expect(screen.getByText("已安装 · 发现自此后端")).toBeInTheDocument();
    expect(screen.getByText("superpowers")).toBeInTheDocument();
  });

  it("toggles an installed row on click", async () => {
    const user = userEvent.setup();
    const { onToggle } = setup();
    await user.click(screen.getByText("frontend-design"));
    expect(onToggle).toHaveBeenCalledWith("fd@m");
  });

  it("does not toggle a disabled (needs-install) row", async () => {
    const user = userEvent.setup();
    const { onToggle } = setup();
    await user.click(screen.getByText("baoyu-skills"));
    expect(onToggle).not.toHaveBeenCalled();
    expect(screen.getByText("需先安装")).toBeInTheDocument();
  });

  it("filters by name/description via the search box", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText("搜索技能包 / 描述…"), "front");
    expect(screen.getByText("frontend-design")).toBeInTheDocument();
    expect(screen.queryByText("superpowers")).toBeNull();
  });

  it("fires rescan and confirm", async () => {
    const user = userEvent.setup();
    const { onRescan, onConfirm } = setup();
    await user.click(screen.getByText("Rescan"));
    expect(onRescan).toHaveBeenCalled();
    await user.click(screen.getByText("Done"));
    expect(onConfirm).toHaveBeenCalled();
  });

  it("shows loading and empty states", () => {
    const { rerender } = render(<div />) as unknown as { rerender: never };
    void rerender;
    // loading
    render(
      <CapabilityPicker
        open
        title="t"
        searchPlaceholder="p"
        items={[]}
        loading
        onToggle={vi.fn()}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });
});
```

> 注：测试默认语言是 en（i18n 初始化）；上面 `Rescan`/`Done`/`Loading…` 用 en 文案断言，传入的 title/subtitle/placeholder 是裸字符串（非 t() 解析，仅测试方便）。

- [x] **Step 3: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/capability-picker.test.tsx`
Expected: FAIL —— `Cannot find module '../capability-picker'`。

- [x] **Step 4: 写实现**

`frontend/src/components/agentre/capability/capability-picker.tsx`：

```tsx
import * as React from "react";
import { Boxes, RefreshCw, Search, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

import { groupCatalogItems, type CatalogBadgeTone, type CatalogItem } from "./catalog";

type Props = {
  open: boolean;
  title: string; // 已 t() 解析
  subtitle?: string; // 已 t() 解析
  searchPlaceholder: string; // 已 t() 解析
  items: CatalogItem[];
  loading?: boolean;
  footerNote?: string; // 已 t() 解析（如个人技能只读提示）
  onToggle: (id: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
  onRescan?: () => void;
};

const badgeToneClass: Record<CatalogBadgeTone, string> = {
  recommended: "bg-primary-soft text-primary-text",
  installed: "bg-status-running-bg text-status-running",
  available: "bg-secondary text-muted-foreground",
  approval: "bg-status-warning-bg text-status-warning",
  needInstall: "bg-status-warning-bg text-status-warning",
};

export function CapabilityPicker(props: Props) {
  const { t } = useTranslation();
  const [query, setQuery] = React.useState("");

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return props.items;
    return props.items.filter(
      (it) =>
        it.name.toLowerCase().includes(q) ||
        it.description.toLowerCase().includes(q),
    );
  }, [props.items, query]);

  const groups = React.useMemo(() => groupCatalogItems(filtered), [filtered]);
  const selectedCount = props.items.filter((it) => it.enabled).length;

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onCancel()}>
      {props.open && (
        <DialogContent
          className="max-w-[460px] gap-0 overflow-hidden p-0"
          showCloseButton={false}
        >
          {/* header */}
          <div className="flex flex-col gap-1.5 border-b border-border px-[18px] py-3.5">
            <div className="flex items-center gap-2">
              <Boxes className="size-4 text-primary-text" aria-hidden="true" />
              <span className="text-[15px] font-semibold">{props.title}</span>
              <div className="flex-1" />
              <button
                type="button"
                aria-label={t("capability.picker.close")}
                onClick={props.onCancel}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="size-4" />
              </button>
            </div>
            {props.subtitle && (
              <span className="font-mono text-2xs text-muted-foreground">
                {props.subtitle}
              </span>
            )}
          </div>

          {/* search + rescan */}
          <div className="flex items-center gap-2.5 border-b border-border/60 px-[18px] py-3">
            <div className="flex flex-1 items-center gap-2 rounded-md border border-border px-2.5 py-2">
              <Search className="size-3.5 text-muted-foreground" aria-hidden="true" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={props.searchPlaceholder}
                aria-label={props.searchPlaceholder}
                className="w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground"
              />
            </div>
            {props.onRescan && (
              <Button
                variant="outline"
                size="sm"
                className="h-auto gap-1.5 px-2.5 py-2"
                onClick={props.onRescan}
              >
                <RefreshCw className="size-3" aria-hidden="true" />
                {t("capability.picker.rescan")}
              </Button>
            )}
          </div>

          {/* body */}
          <div className="flex max-h-[420px] flex-col gap-2 overflow-y-auto px-4 py-3">
            {props.loading ? (
              <p className="py-6 text-center text-2xs text-muted-foreground">
                {t("capability.picker.loading")}
              </p>
            ) : props.items.length === 0 ? (
              <p className="py-6 text-center text-2xs text-muted-foreground">
                {t("capability.picker.empty")}
              </p>
            ) : groups.length === 0 ? (
              <p className="py-6 text-center text-2xs text-muted-foreground">
                {t("capability.picker.searchEmpty")}
              </p>
            ) : (
              groups.map((g) => (
                <div key={g.group} className="flex flex-col gap-1.5">
                  {g.group && (
                    <div className="flex items-center gap-2 pt-1">
                      <span className="font-mono text-3xs font-semibold uppercase text-muted-foreground">
                        {g.group}
                      </span>
                      <div className="flex-1" />
                      <span className="font-mono text-3xs text-muted-foreground">
                        {g.items.length}
                      </span>
                    </div>
                  )}
                  {g.items.map((it) => {
                    const disabled = Boolean(it.disabledReason);
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
                          <span className="truncate text-xs font-semibold">
                            {it.name}
                          </span>
                          <span className="truncate font-mono text-3xs text-muted-foreground">
                            {it.description}
                          </span>
                        </span>
                        {it.badges && it.badges.length > 0 && (
                          <span className="flex shrink-0 items-center gap-1">
                            {it.badges.map((b) => (
                              <span
                                key={b.label}
                                className={cn(
                                  "rounded px-1.5 py-0.5 font-mono text-3xs font-semibold",
                                  badgeToneClass[b.tone],
                                )}
                              >
                                {b.label}
                              </span>
                            ))}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              ))
            )}
            {props.footerNote && (
              <p className="mt-1 rounded-md bg-secondary/40 px-2.5 py-2 font-mono text-3xs text-muted-foreground">
                {props.footerNote}
              </p>
            )}
          </div>

          {/* footer */}
          <div className="flex items-center gap-2.5 border-t border-border bg-secondary/30 px-[18px] py-3">
            <span className="flex-1 font-mono text-3xs text-muted-foreground">
              {t("capability.picker.selected", { count: selectedCount })}
            </span>
            <Button variant="outline" size="sm" onClick={props.onCancel}>
              {t("common.cancel")}
            </Button>
            <Button size="sm" onClick={props.onConfirm}>
              {t("capability.picker.done")}
            </Button>
          </div>
        </DialogContent>
      )}
    </Dialog>
  );
}
```

> 实现者注意：用现有 `@/components/ui/dialog` 的 `DialogContent`。若该组件无 `showCloseButton` prop，则去掉该 prop 并保留自绘 ✕（先 `Read` dialog.tsx 确认 API）。`text-3xs`/`bg-primary-soft`/`status-*` 等 token 若不存在，改用就近的现有 Tailwind class（先在别的组件里 grep 确认 token 名）。`✓` 用现有 `Check` lucide 图标替换更佳。

- [x] **Step 5: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/capability-picker.test.tsx`
Expected: PASS（如断言文案因语言不符，调整为实际初始化语言的文案）。

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/capability/capability-picker.tsx \
        frontend/src/components/agentre/capability/__tests__/capability-picker.test.tsx \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ capability: CapabilityPicker 通用目录弹窗(分组/搜索/勾选/重扫)"
```

---

## Task 4: `<GrantedChips>` 通用已授予芯片区

**Files:**
- Create: `frontend/src/components/agentre/capability/granted-chips.tsx`
- Test: `frontend/src/components/agentre/capability/__tests__/granted-chips.test.tsx`

> 同样 dumb：标题/addLabel/footerNote/每芯片 label 由 props 传入（已 t()）。

- [x] **Step 1: 写失败测试**

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Boxes } from "lucide-react";
import { describe, expect, it, vi } from "vitest";

import { GrantedChips } from "../granted-chips";

function setup(extra = {}) {
  const onRemove = vi.fn();
  const onAdd = vi.fn();
  render(
    <GrantedChips
      title="技能 · SKILL PACKS"
      countLabel="2 已启用"
      chipIcon={Boxes}
      chips={[
        { id: "sp@m", label: "superpowers", count: 14 },
        { id: "fd@m", label: "frontend-design", count: 1 },
      ]}
      addLabel="添加技能"
      removeLabel={(name) => `移除 ${name}`}
      onRemove={onRemove}
      onAdd={onAdd}
      footerNote="始终可用 · 个人技能不可单独开关"
      {...extra}
    />,
  );
  return { onRemove, onAdd };
}

describe("GrantedChips", () => {
  it("renders chips with labels and counts", () => {
    setup();
    expect(screen.getByText("superpowers")).toBeInTheDocument();
    expect(screen.getByText("14")).toBeInTheDocument();
    expect(screen.getByText("2 已启用")).toBeInTheDocument();
    expect(screen.getByText("始终可用 · 个人技能不可单独开关")).toBeInTheDocument();
  });

  it("calls onRemove with chip id", async () => {
    const user = userEvent.setup();
    const { onRemove } = setup();
    await user.click(screen.getByRole("button", { name: "移除 superpowers" }));
    expect(onRemove).toHaveBeenCalledWith("sp@m");
  });

  it("calls onAdd", async () => {
    const user = userEvent.setup();
    const { onAdd } = setup();
    await user.click(screen.getByRole("button", { name: "添加技能" }));
    expect(onAdd).toHaveBeenCalled();
  });

  it("renders an empty hint when there are no chips", () => {
    setup({ chips: [], emptyLabel: "未授予技能包" });
    expect(screen.getByText("未授予技能包")).toBeInTheDocument();
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/granted-chips.test.tsx`
Expected: FAIL —— 模块不存在。

- [x] **Step 3: 写实现**

`frontend/src/components/agentre/capability/granted-chips.tsx`：

```tsx
import * as React from "react";
import { Plus, X, type LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

export type GrantedChip = {
  id: string;
  label: string;
  count?: number;
  badge?: string; // 已 t() 解析（如「需审批」）
};

type Props = {
  title: string;
  countLabel?: string;
  chipIcon: LucideIcon;
  chips: GrantedChip[];
  addLabel: string;
  removeLabel: (name: string) => string;
  onRemove: (id: string) => void;
  onAdd: () => void;
  emptyLabel?: string;
  footerNote?: string;
  className?: string;
};

export function GrantedChips(props: Props) {
  const Icon = props.chipIcon;
  return (
    <div className={cn("flex flex-col gap-2", props.className)}>
      <div className="flex items-center gap-1.5">
        <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
          {props.title}
        </h3>
        <div className="flex-1" />
        {props.countLabel && (
          <span className="font-mono text-2xs text-muted-foreground">
            {props.countLabel}
          </span>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        {props.chips.length === 0 && props.emptyLabel && (
          <span className="text-2xs text-muted-foreground">
            {props.emptyLabel}
          </span>
        )}
        {props.chips.map((chip) => (
          <span
            key={chip.id}
            className="inline-flex items-center gap-1.5 rounded-md border border-border bg-card px-2 py-1"
          >
            <Icon className="size-3 text-primary-text" aria-hidden="true" />
            <span className="font-mono text-2xs font-medium">{chip.label}</span>
            {typeof chip.count === "number" && (
              <span className="rounded bg-secondary px-1 font-mono text-3xs text-muted-foreground">
                {chip.count}
              </span>
            )}
            {chip.badge && (
              <span className="rounded bg-status-warning-bg px-1 font-mono text-3xs text-status-warning">
                {chip.badge}
              </span>
            )}
            <button
              type="button"
              aria-label={props.removeLabel(chip.label)}
              onClick={() => props.onRemove(chip.id)}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        <button
          type="button"
          onClick={props.onAdd}
          className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary-soft px-2 py-1 font-mono text-2xs font-semibold text-primary-text hover:bg-primary-soft/70"
        >
          <Plus className="size-3" aria-hidden="true" />
          {props.addLabel}
        </button>
      </div>

      {props.footerNote && (
        <p className="font-mono text-3xs text-muted-foreground">
          {props.footerNote}
        </p>
      )}
    </div>
  );
}
```

> token 同 Task 3 备注：`status-warning-bg`/`primary-soft`/`text-3xs` 不存在就换就近现有 class。

- [x] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/capability/__tests__/granted-chips.test.tsx`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/capability/granted-chips.tsx \
        frontend/src/components/agentre/capability/__tests__/granted-chips.test.tsx
git commit -m "✨ capability: GrantedChips 通用已授予芯片区(增删/数量/badge)"
```

---

## Task 5: 技能目录映射 `skillPacksToCatalog`

**Files:**
- Create: `frontend/src/components/agentre/org/skill-catalog.ts`
- Test: `frontend/src/components/agentre/org/__tests__/skill-catalog.test.ts`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`（`org.agent.skillCatalog.group.*` + `badge.*`）

映射规则（对齐设计稿分组，**用 flag 而非 raw source**，因为后端把「既推荐又安装」标成 source=installed）：
- `recommended === true` → 组「推荐 · AGENTRE 精选」；
- 否则 `installed === true` → 组「已安装 · 发现自此后端」；
- 否则（未安装）→ 组「可安装 · MARKETPLACE」，**禁用**（`disabledReason = 需先安装`）。
- badges：`recommended` → `推荐`(recommended)；`installed` → `已装`(installed)；`!installed` → `需先安装`(needInstall)。
- `enabled` = `pack.enabled`；`contents = pack.skills`。

- [x] **Step 1: 写失败测试**

```ts
import { describe, expect, it } from "vitest";
import i18n from "@/i18n";

import { skillPacksToCatalog } from "../skill-catalog";

const t = i18n.getFixedT("en");
const pack = (over: Record<string, unknown> = {}) => ({
  id: "x@m",
  name: "x",
  description: "d",
  skills: ["a", "b"],
  source: "installed",
  recommended: false,
  installed: true,
  enabled: false,
  ...over,
});

describe("skillPacksToCatalog", () => {
  it("maps a recommended+installed pack into the recommended group with both badges", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ recommended: true, installed: true, enabled: true })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.recommended"));
    expect(it0.enabled).toBe(true);
    expect(it0.contents).toEqual(["a", "b"]);
    expect(it0.badges?.map((b) => b.tone).sort()).toEqual([
      "installed",
      "recommended",
    ]);
    expect(it0.disabledReason).toBeUndefined();
  });

  it("maps an installed-only pack into the installed group", () => {
    const [it0] = skillPacksToCatalog([pack()], t);
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.installed"));
    expect(it0.badges?.map((b) => b.tone)).toEqual(["installed"]);
  });

  it("maps an available (not installed) pack as disabled with needInstall badge", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ source: "available", installed: false })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.available"));
    expect(it0.disabledReason).toBe(t("org.agent.skillCatalog.needInstall"));
    expect(it0.badges?.map((b) => b.tone)).toEqual(["needInstall"]);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/skill-catalog.test.ts`
Expected: FAIL —— 模块不存在 + i18n key 缺失。

- [x] **Step 3: 加 i18n key（两 locale）**

`zh-CN/common.json` 的 `org.agent` 下加：

```json
"skillCatalog": {
  "group": {
    "recommended": "推荐 · AGENTRE 精选",
    "installed": "已安装 · 发现自此后端",
    "available": "可安装 · MARKETPLACE"
  },
  "badge": {
    "recommended": "推荐",
    "installed": "已装",
    "needInstall": "需先安装"
  },
  "needInstall": "需先安装"
}
```

`en/common.json` 同结构：

```json
"skillCatalog": {
  "group": {
    "recommended": "Recommended · Agentre picks",
    "installed": "Installed · discovered on this backend",
    "available": "Available · Marketplace"
  },
  "badge": {
    "recommended": "Recommended",
    "installed": "Installed",
    "needInstall": "Install first"
  },
  "needInstall": "Install first"
}
```

- [x] **Step 4: 写实现**

`frontend/src/components/agentre/org/skill-catalog.ts`：

```ts
import type { TFunction } from "i18next";

import type { CatalogBadge, CatalogItem } from "../capability/catalog";
import type { skill_svc } from "../../../../wailsjs/go/models";

// skillPacksToCatalog 把后端 SkillPackDTO[] 适配为 CapabilityPicker 的 CatalogItem[]。
// 分组按 recommended/installed flag（不是 raw source）—— 既推荐又安装的包
// 后端标 source=installed，但设计稿要它进「推荐」组。
export function skillPacksToCatalog(
  packs: skill_svc.SkillPackDTO[],
  t: TFunction,
): CatalogItem[] {
  return packs.map((p) => {
    const badges: CatalogBadge[] = [];
    if (p.recommended) {
      badges.push({
        label: t("org.agent.skillCatalog.badge.recommended"),
        tone: "recommended",
      });
    }
    if (p.installed) {
      badges.push({
        label: t("org.agent.skillCatalog.badge.installed"),
        tone: "installed",
      });
    } else {
      badges.push({
        label: t("org.agent.skillCatalog.badge.needInstall"),
        tone: "needInstall",
      });
    }

    const group = p.recommended
      ? t("org.agent.skillCatalog.group.recommended")
      : p.installed
        ? t("org.agent.skillCatalog.group.installed")
        : t("org.agent.skillCatalog.group.available");

    return {
      id: p.id,
      name: p.name,
      description: p.description,
      contents: p.skills ?? [],
      group,
      badges,
      enabled: p.enabled,
      disabledReason: p.installed
        ? undefined
        : t("org.agent.skillCatalog.needInstall"),
    };
  });
}
```

- [x] **Step 5: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/skill-catalog.test.ts`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/org/skill-catalog.ts \
        frontend/src/components/agentre/org/__tests__/skill-catalog.test.ts \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ org: skillPacksToCatalog 映射(来源分组+badge+禁用态)"
```

---

## Task 6: 懒加载 hook `useSkillCatalog`

**Files:**
- Create: `frontend/src/components/agentre/org/use-skill-catalog.ts`
- Test: `frontend/src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`

行为：初始不拉数据；`load(refresh)` 调 `ListAgentSkillPacks(agentId, refresh)` → `skillPacksToCatalog` → `items`；暴露 `loading`/`error`/`load`/`reload`/`fetched`。

- [x] **Step 1: 写失败测试**

```tsx
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useSkillCatalog } from "../use-skill-catalog";

function stubBinding(packs: unknown[]) {
  const fn = vi.fn().mockResolvedValue({ packs });
  // @ts-expect-error test shim onto window-backed mock
  window.go = { app: { App: { ListAgentSkillPacks: fn } } };
  return fn;
}

afterEach(() => {
  // @ts-expect-error cleanup
  delete window.go;
});

describe("useSkillCatalog", () => {
  it("does not fetch until load() is called", () => {
    const fn = stubBinding([]);
    const { result } = renderHook(() => useSkillCatalog(7));
    expect(fn).not.toHaveBeenCalled();
    expect(result.current.items).toEqual([]);
    expect(result.current.fetched).toBe(false);
  });

  it("loads and maps packs on load(false)", async () => {
    const fn = stubBinding([
      {
        id: "sp@m",
        name: "superpowers",
        description: "d",
        skills: ["a"],
        source: "installed",
        recommended: false,
        installed: true,
        enabled: true,
      },
    ]);
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(false);
    });
    expect(fn).toHaveBeenCalledWith(7, false);
    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.items[0].name).toBe("superpowers");
    expect(result.current.fetched).toBe(true);
  });

  it("rescan calls the binding with refresh=true", async () => {
    const fn = stubBinding([]);
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(true);
    });
    expect(fn).toHaveBeenCalledWith(7, true);
  });

  it("captures errors", async () => {
    const fn = vi.fn().mockRejectedValue(new Error("boom"));
    // @ts-expect-error shim
    window.go = { app: { App: { ListAgentSkillPacks: fn } } };
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(false);
    });
    await waitFor(() => expect(result.current.error).toBeTruthy());
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`
Expected: FAIL —— 模块不存在。

- [x] **Step 3: 写实现**

`frontend/src/components/agentre/org/use-skill-catalog.ts`：

```ts
import * as React from "react";
import { useTranslation } from "react-i18next";

import { ListAgentSkillPacks } from "@/../wailsjs/go/app/App";

import type { CatalogItem } from "../capability/catalog";
import { skillPacksToCatalog } from "./skill-catalog";

// useSkillCatalog 懒加载某 agent 的技能目录。组件应在「打开添加弹窗」时
// 调一次 load(false)，「重新扫描」时 load(true)。不在 mount 时自动拉，
// 避免每次打开面板都跑后端 CLI(plugin list)。
export function useSkillCatalog(agentId: number) {
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

  return { items, loading, error, fetched, load, reload: () => load(false) };
}
```

- [x] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-skill-catalog.test.tsx`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/org/use-skill-catalog.ts \
        frontend/src/components/agentre/org/__tests__/use-skill-catalog.test.tsx
git commit -m "✨ org: useSkillCatalog 懒加载 hook(ListAgentSkillPacks→CatalogItem)"
```

---

## Task 7: 面板技能区重做（门控 + GrantedChips + 弹窗 + 个人提示）

**Files:**
- Modify: `frontend/src/components/agentre/org/org-detail-agent.tsx`
- Modify: `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`（`org.agent.skills.*` 调整 + `org.agent.skillsGate.*` + `org.agent.skillPicker.*`）

要点：
1. **修 `.label`→`.id`**：删 `toggleSkill`；`skills` 状态/读取改用 `.id`。
2. 引入 `useBackendCapabilities(selectedBackend?.type)`；`caps?.has("skills")` 决定技能区 vs 门控盒。
3. 引入 `useSkillCatalog(props.agent.id)` + 本地 `skillPickerOpen` 状态；打开弹窗时 `load(false)`，缓存 `catalog.items` 供芯片显数量。
4. 技能区 = `<GrantedChips>`（chips 来自本地 `skills`，label 由 id 派生，count 来自缓存目录）+ `<CapabilityPicker>`（items = 目录 overlay 本地选择态）。
5. 弹窗「完成」只更新本地 `skills`；保存仍走底部 `保存`（`handleSave` 不变，`skills` 已是 `{id,enabled:true}[]`）。

- [x] **Step 1: 先加/调 i18n key（两 locale）**

`org.agent.skills.*`（替换旧的 summary/item/list 用法，保留/新增）：

zh-CN：
```json
"skills": {
  "sectionTitle": "技能 · SKILL PACKS",
  "enabledCount": "{{count}} 已启用",
  "add": "添加技能",
  "empty": "未授予技能包",
  "personalNote": "始终可用 · 个人技能（~/.claude/skills）不可单独开关",
  "title": "技能"
},
"skillsGate": {
  "pill": "backend 不支持",
  "title": "当前 backend 不支持技能",
  "description": "技能是 Claude Code 原生能力 · 切换到 Claude Code 后端即可启用并配置"
},
"skillPicker": {
  "title": "添加技能 · Skill Packs",
  "subtitle": "一个包 = 一组 skill，按 agent 授权",
  "searchPlaceholder": "搜索技能包 / 描述…",
  "personalNote": "个人技能（~/.claude/skills）始终可用，CLI 不支持单独开关"
}
```

en：
```json
"skills": {
  "sectionTitle": "Skills · Skill Packs",
  "enabledCount": "{{count}} enabled",
  "add": "Add Skill",
  "empty": "No skill packs granted",
  "personalNote": "Always available · personal skills (~/.claude/skills) can't be toggled individually",
  "title": "Skills"
},
"skillsGate": {
  "pill": "Unsupported",
  "title": "This backend doesn't support skills",
  "description": "Skills are a Claude Code native capability. Switch to a Claude Code backend to enable and configure them."
},
"skillPicker": {
  "title": "Add Skills · Skill Packs",
  "subtitle": "One pack = a set of skills, granted per agent",
  "searchPlaceholder": "Search skill packs / descriptions…",
  "personalNote": "Personal skills (~/.claude/skills) are always available; the CLI can't toggle them individually"
}
```

> 删除旧 key `org.agent.skills.summary` / `.item` / `.list`（不再被引用）。**两 locale 同步删**，否则 i18n 对齐测试 fail。

- [x] **Step 2: 写/改失败测试**

改 `org-detail-agent.test.tsx`：

(a) fixture 改 id（顶部 `agent()` 里）：
```ts
skills: [
  { id: "superpowers@m", enabled: true },
  { id: "frontend-design@m", enabled: false },
],
```

(b) 删除旧「toggles skill enabled state and updates counter」用例。

(c) 新增技能门控用例（claudecode 显示技能区、codex 显示门控盒）。先建一个能注入 caps 的 helper：

```tsx
function withCaps(caps: string[]) {
  // wailsApp mock 的 GetBackendCapabilities 是 windowBackedMock，
  // 测试覆盖其返回。
  // @ts-expect-error test shim
  window.go = {
    app: {
      App: {
        GetBackendCapabilities: vi.fn().mockResolvedValue({
          capabilities: caps,
          permissionModeMeta: null,
        }),
        ListAgentSkillPacks: vi.fn().mockResolvedValue({ packs: [] }),
      },
    },
  };
}
afterEach(() => {
  // @ts-expect-error cleanup
  delete window.go;
});

it("shows the skills section for a claudecode backend (CapSkills)", async () => {
  withCaps(["skills", "mcp_tools"]);
  renderPanel({ agentBackendId: 5 }, [backend({ id: 5, type: "claudecode" })]);
  expect(
    await screen.findByText("Skills · Skill Packs"),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add Skill" })).toBeInTheDocument();
});

it("shows the gating box for a non-claudecode backend", async () => {
  withCaps(["mcp_tools"]); // codex: no skills cap
  renderPanel({ agentBackendId: 6 }, [backend({ id: 6, type: "codex" })]);
  expect(
    await screen.findByText("This backend doesn't support skills"),
  ).toBeInTheDocument();
});

it("granted skill chips render derived names from pack ids", async () => {
  withCaps(["skills", "mcp_tools"]);
  renderPanel({ agentBackendId: 5 }, [backend({ id: 5, type: "claudecode" })]);
  expect(await screen.findByText("superpowers")).toBeInTheDocument();
  expect(screen.getByText("frontend-design")).toBeInTheDocument();
});
```

> 这些用例需要 `vi` 已 import（已在文件顶部）。`backend()` helper 接受 `id`/`type` override（已支持）。

- [x] **Step 3: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: FAIL —— 新断言找不到「Skills · Skill Packs」「Add Skill」「This backend doesn't support skills」；旧 `.label` 渲染已不匹配 id fixture。

- [x] **Step 4: 改 `org-detail-agent.tsx`**

(a) 顶部 import 增补：
```tsx
import { Ban, Boxes } from "lucide-react";
import { useBackendCapabilities } from "../capability/use-backend-capabilities";
import { CapabilityPicker } from "../capability/capability-picker";
import { GrantedChips, type GrantedChip } from "../capability/granted-chips";
import { useSkillCatalog } from "./use-skill-catalog";
```

(b) 删除 `toggleSkill`（line 163-167）、`enabledCount`/`disabledCount`（line 175-176）。

(c) 组件体内（`selectedBackend` 之后）加：
```tsx
const { caps } = useBackendCapabilities(selectedBackend?.type);
const skillCatalog = useSkillCatalog(props.agent.id);
const [skillPickerOpen, setSkillPickerOpen] = React.useState(false);

// 已授予芯片：label 由 id 派生(strip @marketplace)，count 来自已拉过的目录缓存。
const idToName = (id: string) => id.split("@")[0] || id;
const skillCountById = React.useMemo(() => {
  const m = new Map<string, number>();
  for (const it of skillCatalog.items) m.set(it.id, it.contents?.length ?? 0);
  return m;
}, [skillCatalog.items]);
const skillChips: GrantedChip[] = skills
  .filter((s) => s.enabled)
  .map((s) => ({
    id: s.id,
    label: idToName(s.id),
    count: skillCountById.get(s.id),
  }));

// 弹窗 items：目录 overlay 本地选择态(本地优先于后端 enabled)
const localEnabled = React.useMemo(
  () => new Set(skills.filter((s) => s.enabled).map((s) => s.id)),
  [skills],
);
const pickerItems = skillCatalog.items.map((it) => ({
  ...it,
  enabled: localEnabled.has(it.id),
}));

const openSkillPicker = () => {
  setSkillPickerOpen(true);
  if (!skillCatalog.fetched) void skillCatalog.load(false);
};
const toggleSkillGrant = (id: string) => {
  setSkills((prev) => {
    const idx = prev.findIndex((s) => s.id === id);
    if (idx >= 0) return prev.filter((s) => s.id !== id); // 取消授予 = 移除
    return [...prev, department_svc.AgentSkillDTO.createFrom({ id, enabled: true })];
  });
};
const removeSkill = (id: string) =>
  setSkills((prev) => prev.filter((s) => s.id !== id));
```

(d) 用下面整段替换原「技能区」`<section data-slot="agent-section-skills">…</section>`（line 415-457）：

```tsx
<section className="space-y-2.5" data-slot="agent-section-skills">
  {caps?.has("skills") ? (
    <>
      <GrantedChips
        title={t("org.agent.skills.sectionTitle")}
        countLabel={t("org.agent.skills.enabledCount", {
          count: skillChips.length,
        })}
        chipIcon={Boxes}
        chips={skillChips}
        addLabel={t("org.agent.skills.add")}
        removeLabel={(name) => t("capability.picker.remove", { name })}
        onRemove={removeSkill}
        onAdd={openSkillPicker}
        emptyLabel={t("org.agent.skills.empty")}
        footerNote={t("org.agent.skills.personalNote")}
      />
      <CapabilityPicker
        open={skillPickerOpen}
        title={t("org.agent.skillPicker.title")}
        subtitle={t("org.agent.skillPicker.subtitle")}
        searchPlaceholder={t("org.agent.skillPicker.searchPlaceholder")}
        items={pickerItems}
        loading={skillCatalog.loading}
        footerNote={t("org.agent.skillPicker.personalNote")}
        onToggle={toggleSkillGrant}
        onConfirm={() => setSkillPickerOpen(false)}
        onCancel={() => setSkillPickerOpen(false)}
        onRescan={() => void skillCatalog.load(true)}
      />
    </>
  ) : (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5">
        <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
          {t("org.agent.skills.title")}
        </h3>
        <div className="flex-1" />
        <span className="rounded bg-secondary px-1.5 py-0.5 font-mono text-3xs text-muted-foreground">
          {t("org.agent.skillsGate.pill")}
        </span>
      </div>
      <div className="flex items-start gap-2.5 rounded-md border border-border bg-secondary/30 px-3 py-2.5">
        <Ban className="mt-0.5 size-3.5 text-muted-foreground" aria-hidden="true" />
        <div className="space-y-0.5">
          <p className="text-2xs font-semibold text-foreground">
            {t("org.agent.skillsGate.title")}
          </p>
          <p className="font-mono text-3xs text-muted-foreground">
            {t("org.agent.skillsGate.description")}
          </p>
        </div>
      </div>
    </div>
  )}
</section>
```

> 若 `caps === null`（加载中/无后端），上面 `caps?.has("skills")` 为 false → 暂显门控盒。如需更精细可加 `caps === null ? null : …`，但 v1 接受加载态短暂显门控盒（claudecode 拉到 caps 后即切回）。实现者可按 review 决定是否加 loading 占位。

- [x] **Step 5: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: PASS（工具相关用例此时可能仍依赖旧渲染 —— 若该文件里工具用例因缺 caps 失败，先在工具用例的 `renderPanel` 前加 `withCaps(["mcp_tools"])`，完整工具改造在 Task 8）。

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/org/org-detail-agent.tsx \
        frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ org: 技能区重做(CapSkills 门控+GrantedChips+CapabilityPicker+个人提示) 并修 .label→.id"
```

---

## Task 8: 面板工具区重做（复用组件 + CapMCPTools 门控）

**Files:**
- Create: `frontend/src/components/agentre/org/tool-catalog.ts`
- Test: `frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts`
- Modify: `frontend/src/components/agentre/org/org-detail-agent.tsx`
- Modify: `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`（`org.agent.tools.*` 调整 + `org.agent.toolPicker.*`）

要点：工具目录来自 `availableTools: string[]`（无后端调用）；`{key,enabled}` 保存形状不变；按 `caps.has("mcp_tools")` 门控（claudecode+codex 有 → 显示；builtin/remote 无 → 隐藏整区）。`org` 工具带「需审批」badge（前端已知集合）。

- [x] **Step 1: 写失败测试（tool-catalog 映射）**

`frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts`：

```ts
import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { toolKeysToCatalog } from "../tool-catalog";

const t = i18n.getFixedT("en");

describe("toolKeysToCatalog", () => {
  it("maps tool keys with localized names + approval badge for org", () => {
    const items = toolKeysToCatalog(["org"], [{ key: "org", enabled: true }], t);
    expect(items[0].id).toBe("org");
    expect(items[0].name).toBe(t("org.agent.tools.names.org"));
    expect(items[0].enabled).toBe(true);
    expect(items[0].badges?.[0]?.tone).toBe("approval");
  });

  it("marks unknown agent tools as not enabled", () => {
    const items = toolKeysToCatalog(["org"], [], t);
    expect(items[0].enabled).toBe(false);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/tool-catalog.test.ts`
Expected: FAIL —— 模块不存在。

- [x] **Step 3: 加 i18n key + 写 `tool-catalog.ts`**

i18n（`org.agent.tools.*` 增补；保留既有 `names.org`/`descriptions.org`）：

zh-CN：
```json
"tools": {
  "title": "工具",
  "sectionTitle": "工具 · TOOLS",
  "enabledCount": "{{count}} 已启用",
  "add": "添加工具",
  "empty": "暂无可用工具",
  "approval": "需审批",
  "names": { "org": "组织架构" },
  "descriptions": { "org": "允许该 Agent 查询并管理部门与成员（写操作需你审批）" }
},
"toolPicker": {
  "title": "添加工具",
  "subtitle": "工具调用经 MCP 注入 · 写操作需审批",
  "searchPlaceholder": "搜索工具 / 描述…"
}
```

en：
```json
"tools": {
  "title": "Tools",
  "sectionTitle": "Tools · TOOLS",
  "enabledCount": "{{count}} enabled",
  "add": "Add Tool",
  "empty": "No tools available",
  "approval": "Approval",
  "names": { "org": "Org Structure" },
  "descriptions": { "org": "Lets this agent query and manage departments and members (writes need your approval)" }
},
"toolPicker": {
  "title": "Add Tools",
  "subtitle": "Tool calls are injected over MCP; writes need approval",
  "searchPlaceholder": "Search tools / descriptions…"
}
```

> 删除旧 `org.agent.tools.list`（不再用，两 locale 同步删）。

`frontend/src/components/agentre/org/tool-catalog.ts`：

```ts
import type { TFunction } from "i18next";

import type { CatalogItem } from "../capability/catalog";
import type { department_svc } from "../../../../wailsjs/go/models";

// 已知需审批的工具(后端写操作走审批门)。新增工具时在此登记，
// 真正的 per-tool 审批元数据未来应由后端提供(本期前端已知集合)。
const APPROVAL_TOOLS = new Set(["org"]);

export function toolKeysToCatalog(
  keys: string[],
  agentTools: department_svc.AgentToolDTO[],
  t: TFunction,
): CatalogItem[] {
  const enabledByKey = new Map(agentTools.map((tl) => [tl.key, tl.enabled]));
  return keys.map((key) => ({
    id: key,
    name: t(`org.agent.tools.names.${key}`),
    description: t(`org.agent.tools.descriptions.${key}`),
    group: "",
    badges: APPROVAL_TOOLS.has(key)
      ? [{ label: t("org.agent.tools.approval"), tone: "approval" as const }]
      : undefined,
    enabled: enabledByKey.get(key) ?? false,
  }));
}
```

- [x] **Step 4: tool-catalog 测试转绿**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/tool-catalog.test.ts`
Expected: PASS

- [x] **Step 5: 改面板工具区 + 改面板工具测试**

(a) `org-detail-agent.tsx` import 增补 `Network`（lucide）+ `toolKeysToCatalog`。

(b) 删除 `toggleTool`（line 169-173）。组件体内加（沿用 Task 7 的 caps）：
```tsx
const [toolPickerOpen, setToolPickerOpen] = React.useState(false);
const toolChips: GrantedChip[] = tools
  .filter((tl) => tl.enabled)
  .map((tl) => ({
    id: tl.key,
    label: t(`org.agent.tools.names.${tl.key}`),
    badge: tl.key === "org" ? t("org.agent.tools.approval") : undefined,
  }));
const toolItems = toolKeysToCatalog(props.availableTools ?? [], tools, t);
const toggleToolGrant = (key: string) =>
  setTools((prev) =>
    prev.map((tl) => (tl.key === key ? { ...tl, enabled: !tl.enabled } : tl)),
  );
const removeTool = (key: string) =>
  setTools((prev) =>
    prev.map((tl) => (tl.key === key ? { ...tl, enabled: false } : tl)),
  );
```

(c) 用下段替换原「工具区」`<section data-slot="agent-section-tools">…</section>`（line 459-496）。整区按 `caps?.has("mcp_tools")` 门控（无则不渲染工具区）：

```tsx
{caps?.has("mcp_tools") && (
  <section className="space-y-2.5" data-slot="agent-section-tools">
    <GrantedChips
      title={t("org.agent.tools.sectionTitle")}
      countLabel={t("org.agent.tools.enabledCount", {
        count: toolChips.length,
      })}
      chipIcon={Network}
      chips={toolChips}
      addLabel={t("org.agent.tools.add")}
      removeLabel={(name) => t("capability.picker.remove", { name })}
      onRemove={removeTool}
      onAdd={() => setToolPickerOpen(true)}
      emptyLabel={t("org.agent.tools.empty")}
    />
    <CapabilityPicker
      open={toolPickerOpen}
      title={t("org.agent.toolPicker.title")}
      subtitle={t("org.agent.toolPicker.subtitle")}
      searchPlaceholder={t("org.agent.toolPicker.searchPlaceholder")}
      items={toolItems}
      onToggle={toggleToolGrant}
      onConfirm={() => setToolPickerOpen(false)}
      onCancel={() => setToolPickerOpen(false)}
    />
  </section>
)}
```

(d) 改 `org-detail-agent.test.tsx` 的工具用例（保存形状仍 `{key,enabled}`）。三个旧工具用例改造：
- 「renders Tools section with org switch…」→ 改成断言 `Tools · TOOLS` 标题 + `Add Tool` 按钮存在（不再有 switch）。需 `withCaps(["skills","mcp_tools"])` + claudecode backend。
- 「saves tools with org enabled after toggling…」→ 改成：打开工具弹窗 → 勾选 `Org Structure` → 关弹窗 → 点 `Save`，断言 `onUpdate` 收到 `tools: [{key:"org", enabled:true}]`。
- 「renders org switch in on state…」→ 改成断言已启用时芯片区出现 `Org Structure` 芯片。

示例（其一）：
```tsx
it("grants the org tool via the tool picker and saves it", async () => {
  withCaps(["skills", "mcp_tools"]);
  const user = userEvent.setup();
  const { onUpdate } = renderPanel(
    { tools: [], agentBackendId: 5 },
    [backend({ id: 5, type: "claudecode" })],
    ["org"],
  );
  await user.click(await screen.findByRole("button", { name: "Add Tool" }));
  await user.click(await screen.findByRole("checkbox", { name: "Org Structure" }));
  await user.click(screen.getByText("Done"));
  const saveBtn = screen
    .getAllByRole("button")
    .find((b) => b.textContent?.trim() === "Save")!;
  await user.click(saveBtn);
  await waitFor(() => expect(onUpdate).toHaveBeenCalled());
  expect(onUpdate).toHaveBeenCalledWith(
    expect.objectContaining({
      tools: expect.arrayContaining([
        expect.objectContaining({ key: "org", enabled: true }),
      ]),
    }),
  );
});
```

- [x] **Step 6: 跑全面板测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx src/components/agentre/org/__tests__/tool-catalog.test.ts`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add frontend/src/components/agentre/org/tool-catalog.ts \
        frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts \
        frontend/src/components/agentre/org/org-detail-agent.tsx \
        frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ org: 工具区重做(复用 GrantedChips+CapabilityPicker, CapMCPTools 门控)"
```

---

## Task 9: 全量校验（i18n 对齐 + 完整前端测试 + lint/typecheck）

**Files:** 无新增；只跑校验、按结果回补。

- [x] **Step 1: i18n 对齐 + 静态 key 覆盖 + 无硬编码中文**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS。若失败：
- 「same keys」fail → 某 key 只加进一个 locale，补齐另一个（或两边同步删旧 key）。
- 「provide every key」fail → 源码里某静态 `t("x")` 在 locale 缺失，补上。
- 「no Chinese hardcoded」fail → 某可见字面量没走 `t()`，改成 `t()` + locale。

- [x] **Step 2: 跑全部前端单测**

Run: `cd frontend && pnpm test`
Expected: 全绿（含 capability/org 新测试 + 既有 App.test/foundation.test 不被破坏）。

- [x] **Step 3: 生成真实 binding 后 typecheck + lint**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre/.claude/worktrees/agent-skills
GOWORK=off make generate
cd frontend && pnpm exec eslint src --max-warnings=0
```
Expected: 0 error。重点确认：
- `org-detail-agent.tsx` 不再有 `.label` 引用（真实 `AgentSkillDTO` 只有 `id`，否则 tsc 报错）；
- `i18next/no-literal-string` 无新增硬编码中文；
- 未引入未使用 import。

> 若 worktree 里 `make generate` 偶发失败，可改跑 `GOWORK=off "$(go env GOPATH)/bin/wails" generate module` 后重试 eslint。

- [x] **Step 4: 勾选本计划 + 收尾 commit**

把本文件所有 `- [ ]` 勾成 `- [x]`，并补一条计划状态（仿 PR1 plan 收尾）。
```bash
git add docs/superpowers/plans/2026-06-12-agent-skills-pr2-frontend.md
git commit -m "📝 plan: 勾选 PR2 全部任务(前端实施完成)"
```

---

## 完成后（控制者执行，勿在 task 间停顿）

1. **最终整体 code review**：派一个 reviewer subagent 审整条前端链路（catalog/picker/chips/mapping/hook/panel/i18n），对照本计划 + spec §4.8 + pen 帧⑥；尤其核对「弹窗 false 项语义」「保存仍走统一 UpdateAgent」「门控正确（claudecode 显技能、codex 隐技能显工具、builtin 两者皆隐）」。
2. **真机冒烟（人工，可选）**：claudecode agent 详情 → 技能区显示 → 添加弹窗拉到 plugin list → 勾选/取消 → 保存 → 重开会话确认注入；codex agent → 技能门控盒 + 工具区仍在。
3. **Use superpowers:finishing-a-development-branch**（合并/收尾由用户拍板，勿擅自 merge）。

## 自检（writing-plans 要求，已核对）

- **Spec 覆盖**：CapabilityPicker(Task3)/GrantedChips(Task4)/技能区门控(Task7)/工具区(Task8)/个人 skill 只读组(Task7 静态提示)/i18n(各 UI task + Task9) —— 对齐 spec §8.5「前端」整条。
- **类型一致**：`CatalogItem`(Task2) 在 Task3/5/8 一致使用；`GrantedChip`(Task4) 在 Task7/8 一致；`skill_svc.SkillPackDTO` 字段名与生成 models 一致（`id/name/description/skills/source/recommended/installed/enabled`）；`AgentSkillDTO{id,enabled}` 与 `UpdateAgentRequest.skills` 一致。
- **无占位**：每个改码步骤给了完整代码或精确替换段。
- **已知偏差**已在「设计与现实的 3 处偏差」显式说明（生效语义/芯片数量懒加载/个人 skill 静态文案），均为有意取舍、非缺陷。
