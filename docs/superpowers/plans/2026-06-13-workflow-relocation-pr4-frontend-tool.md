# Part B PR4 · 流程工具前端接入(能力 picker + 审批文案)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **依赖:** PR2(前端审批已改名 `toolApproval.*` + 通用 `ToolApprovalCard`)、PR3(后端注册 `KeyWorkflow`,运行时 `availableTools` 含 `"workflow"`)。本 PR 只补前端文案/审批徽标,**无新组件**(能力 picker 与审批卡都已通用)。

**Goal:** 让 `workflow` 工具在「组织架构 → agent 详情 → 工具」能力 picker 里以「流程库」出现(带「需审批」徽标),并给审批卡补 `workflow_create/update/delete` 的中英文标签。

**Architecture:** 能力 picker 按后端 `availableTools: string[]` 自动渲染(`tool-catalog.ts` 的 `toolKeysToCatalog` 映射 `org.agent.tools.names/descriptions.<key>`),所以只需:① `APPROVAL_TOOLS` 加 `"workflow"`(写需审批徽标);② i18n 加 `org.agent.tools.{names,descriptions}.workflow` + `toolApproval.tools.workflow_*`。审批卡(PR2 后通用)按 `toolName` 取 `toolApproval.tools.<toolName>` 标签,无需改组件。

**Tech Stack:** React 19 + TS + react-i18next + Vitest。

---

## 背景与既有事实

- `frontend/src/components/agentre/org/tool-catalog.ts`:
  ```ts
  const APPROVAL_TOOLS = new Set(["org"]);
  export function toolKeysToCatalog(keys, agentTools, t): CatalogItem[] {
    return keys.map((key) => ({
      id: key, name: t(`org.agent.tools.names.${key}`), description: t(`org.agent.tools.descriptions.${key}`),
      group: "", badges: APPROVAL_TOOLS.has(key) ? [{ label: t("org.agent.tools.approval"), tone: "approval" }] : undefined,
      enabled: enabledByKey.get(key) ?? false,
    }));
  }
  ```
- `org-detail-agent.tsx` 的 `toolChips` 还有一处硬编码 `badge: tl.key === "org" ? t("org.agent.tools.approval") : undefined`(`~line 180`)—— 需改成 `APPROVAL_TOOLS.has(tl.key)` 以让 workflow 也带徽标(否则已授予芯片不显「需审批」)。
- i18n 现有(en）`org.agent.tools.names.org="Org Structure"`、`descriptions.org="Allow this agent to read and manage departments and members (writes require your approval)"`;`org.agent.tools.approval="Approval"`。
- 审批卡标签(PR2 后)`toolApproval.tools.*` 现有 `org_*` + `group_create`;**缺** `workflow_*`。
- 能力区门控:工具区需 backend `CapMCPTools`(claudecode/codex 声明)。workflow 工具与 org 同区,门控不变。

### 测试/构建
- `cd frontend && pnpm test`;i18n 强约束(zh/en 对齐 + 静态 key 存在 + 无硬编码中文)。
- 运行时要真看到 `workflow` 进 picker 需 PR3 后端跑起来(`availableTools` 来自 `LoadOrg`)。单测里直接传 `["org","workflow"]` 即可,不依赖后端。

### 文件结构(改)
- `frontend/src/components/agentre/org/tool-catalog.ts`(APPROVAL_TOOLS 加 workflow)
- `frontend/src/components/agentre/org/org-detail-agent.tsx`(toolChips 徽标改用 APPROVAL_TOOLS)
- `frontend/src/i18n/locales/{zh-CN,en}/common.json`(names/descriptions.workflow + toolApproval.tools.workflow_*)
- 相关测试:`org/__tests__/org-detail-agent.test.tsx`、`tool-approval/card.test.tsx`(PR2 改名后)、(可选)`tool-catalog` 单测

---

## Task 1: tool-catalog APPROVAL_TOOLS 加 workflow + 徽标统一

**Files:**
- Modify: `frontend/src/components/agentre/org/tool-catalog.ts`、`org-detail-agent.tsx`
- Test: `frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts`(新建,若无)

- [ ] **Step 1: 写失败测试**(`tool-catalog.test.ts`)
```ts
import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { toolKeysToCatalog } from "../tool-catalog";

describe("toolKeysToCatalog", () => {
  it("workflow 带审批徽标 + 名称来自 i18n", () => {
    const items = toolKeysToCatalog(["org", "workflow"], [{ key: "workflow", enabled: true }], i18n.t.bind(i18n));
    const wf = items.find((i) => i.id === "workflow")!;
    expect(wf.name).toBe(i18n.t("org.agent.tools.names.workflow"));
    expect(wf.enabled).toBe(true);
    expect(wf.badges?.[0]?.tone).toBe("approval");
  });
});
```

- [ ] **Step 2: 跑红** `cd frontend && pnpm test -- src/components/agentre/org/__tests__/tool-catalog.test.ts`(`names.workflow` 缺失会让 `t` 回 key;徽标断言因 APPROVAL_TOOLS 不含 workflow 而失败)。

- [ ] **Step 3: 改 tool-catalog.ts**
```ts
const APPROVAL_TOOLS = new Set(["org", "workflow"]);
```

- [ ] **Step 4: 改 org-detail-agent.tsx 的 toolChips 徽标**(import APPROVAL_TOOLS 或本地判断)。最小改:把
  ```tsx
  badge: tl.key === "org" ? t("org.agent.tools.approval") : undefined,
  ```
  改为复用集合。把 `APPROVAL_TOOLS` 从 tool-catalog 导出后:
  ```ts
  // tool-catalog.ts
  export const APPROVAL_TOOLS = new Set(["org", "workflow"]);
  ```
  ```tsx
  // org-detail-agent.tsx
  import { toolKeysToCatalog, APPROVAL_TOOLS } from "./tool-catalog";
  // ...
  badge: APPROVAL_TOOLS.has(tl.key) ? t("org.agent.tools.approval") : undefined,
  ```

- [ ] **Step 5: 跑绿(需 Task 2 的 i18n 才全绿;先确认徽标逻辑通过,names 断言待 i18n)**

> 顺序提示:Task 1 的测试断言 `names.workflow`,依赖 Task 2 的 i18n。可先做 Task 2 再回跑 Task 1,或本步与 Task 2 合并提交。推荐先做 Task 2 的 i18n,再回 Task 1 Step 2/5。

---

## Task 2: i18n 加 workflow 工具名/描述 + 审批标签

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`en/common.json`

- [ ] **Step 1: `org.agent.tools` 加 workflow**

zh-CN `org.agent.tools.names`：加 `"workflow": "流程库"`;`org.agent.tools.descriptions`：加 `"workflow": "允许该 Agent 查询并管理协作流程（写操作需你审批）"`。
en `names`：`"workflow": "Workflow Library"`;`descriptions`：`"workflow": "Allow this agent to read and manage collaboration workflows (writes require your approval)"`。

- [ ] **Step 2: `toolApproval.tools` 加 workflow_***(PR2 已把 `orgApproval`→`toolApproval`)

zh-CN `toolApproval.tools`：
```json
"workflow_create": "新建流程",
"workflow_update": "更新流程",
"workflow_delete": "删除流程"
```
en `toolApproval.tools`：
```json
"workflow_create": "Create workflow",
"workflow_update": "Update workflow",
"workflow_delete": "Delete workflow"
```

- [ ] **Step 3: i18n 测试 + 回跑 Task 1**
```bash
cd frontend && pnpm test -- src/__tests__/i18n.test.ts
cd frontend && pnpm test -- src/components/agentre/org/__tests__/tool-catalog.test.ts
```
Expected: i18n PASS(zh/en 对齐);tool-catalog 测试 PASS。

- [ ] **Step 4: 提交 Task 1+2**
```bash
git add frontend/src/components/agentre/org/tool-catalog.ts \
        frontend/src/components/agentre/org/org-detail-agent.tsx \
        frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 workflowtool(web): 能力 picker 出现「流程库」(需审批)+ 审批卡 workflow_* 标签"
```

---

## Task 3: 能力 picker + 审批卡覆盖测试

**Files:**
- Modify: `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`、`frontend/src/components/agentre/tool-approval/card.test.tsx`

- [ ] **Step 1: org-detail-agent 测试加 workflow**

在现有「授予 org 工具」用例旁,补一条:`availableTools` 传 `["org","workflow"]`(测试 props),打开工具 picker → 勾选「Workflow Library」→ 保存 → 断言 `UpdateAgent` 的 `tools` 含 `{ key: "workflow", enabled: true }`;并断言已授予芯片区出现「Workflow Library」+「Approval」徽标。

> 参照同文件「授予 org 工具」用例(`~line 295-333`)的写法,key 换 `workflow`、label 换 en `Workflow Library`。

- [ ] **Step 2: 审批卡测试加 workflow_create**

在 `tool-approval/card.test.tsx`(PR2 改名后)补:`approval = { toolKey: "workflow", toolName: "workflow_create", toolInput: {name:"评审流程"}, status: "pending", requestId: "r1" }` → 渲染断言标签「Create workflow」、Approve 点击调 `AnswerToolApproval({ sessionId, requestId: "r1", allow: true })`(PR2 后统一 binding,无 group_create 分支)。

- [ ] **Step 2: 跑绿**
```bash
cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx src/components/agentre/tool-approval/card.test.tsx 2>&1 | grep -E "Test Files|Tests "
```

- [ ] **Step 3: 提交**
```bash
git add frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx \
        frontend/src/components/agentre/tool-approval/card.test.tsx
git commit -m "✅ workflowtool(web): 能力 picker 授予 + 审批卡 workflow 标签/统一 Answer 测试"
```

---

## Task 4: 全量校验

**Files:** 无(验证)

- [ ] **Step 1: 全量 vitest + i18n + tsc + lint**
```bash
cd frontend && pnpm test 2>&1 | grep -E "Test Files|Tests "
pnpm exec tsc -b && echo TSC_OK
pnpm lint 2>&1 | tail -5
```
Expected: 全绿;i18n 无 `names.workflow`/`descriptions.workflow`/`toolApproval.tools.workflow_*` 缺失;无硬编码中文。

- [ ] **Step 2: 真机冒烟(可选)** `make dev`:给某 claudecode/codex agent(声明 CapMCPTools)在工具区授予「流程库」并保存;新起一轮让它调 `workflow_create`,确认审批卡弹出(标题/标签正确)、批准后流程真建、`workflow_list` 能读到。org/group 审批无回归。

---

## Self-Review(对照 spec Part B「能力门控 + 前端开关」)

- **APPROVAL_TOOLS 加 workflow + 徽标统一(芯片也带徽标)**:Task 1 ✓
- **i18n names/descriptions.workflow + toolApproval.tools.workflow_***:Task 2 ✓
- **能力 picker 自动渲染(无新组件)**:复用现有 `toolKeysToCatalog` + picker ✓
- **审批卡通用(PR2)渲染 workflow 标签 + 统一 AnswerToolApproval**:Task 2 标签 + Task 3 测试 ✓
- **测试覆盖:picker 授予 + 审批卡 workflow**:Task 3 ✓
- **依赖**:PR2(toolApproval 命名/通用卡)、PR3(KeyWorkflow 让 availableTools 含 workflow)—— 已在头部声明 ✓
