# 编排默认流程库优化 — 设计

- 日期：2026-07-08
- 状态：设计定稿，待评审
- 范围：agentre 桌面端（`internal/**` + `migrations/**` + `frontend/**`）

## 1. 背景与问题

编排（orchestration）目前只有**一个**内置默认流程 `Default Orchestration Flow`，由迁移 `202607040001_workflow_graph_default.go` 一次性 seed（`is_default=1`，`WHERE NOT EXISTS(is_default=1)` 守卫，无重复 seed）。它的 DAG 是：

```
See members → Break down → [Frontend ∥ Backend] → Integrate → Verify → Wrap up
                                                            ↑______bounce_____|
```

现有管线：`graph(JSON)` 是唯一真源 → `ProjectGraph` 确定性投影成 prose → Go `text/template`（默认 `{{ DAGPrompt }}`）渲染成 `content` → `CreateRun` 时快照进 `run.FlowContent` → 每个 Leader turn 由 `turn.go` 注入。

两个问题：

1. **领域硬编码。** 默认流程把 `Frontend / Backend` 写死成两个并行任务，是对"项目类型"的强假设。很多编排任务不是前后端（研究、文档、纯后端、数据处理…），这个默认对它们不贴切。
2. **只有一种拓扑。** 无论多通用，单个流程只能表达一种工作形状（并行分解）。串行依赖、研究综合、质量门控迭代等常见形状没有起手模板。
3. **提示词是机械投影。** 默认 template 只是裸 `{{ DAGPrompt }}`，注入 Leader 的是干巴巴的步骤清单，缺少针对该工作形状的方法论指导。

## 2. 目标

- 把主默认流程改造成**领域无关的通用骨架**，并额外内置 3 个不同**拓扑**的预设，形成一个小而实用的起手库（共 4 个）。
- 每个流程的**提示词按实际场景手写**（不用 `{{ DAGPrompt }}` 机械投影），读起来像一份打磨过的编排指令。
- 保留每个流程的 `graph`，用于设计器可视化 + 运行时进度 overlay 点亮节点。
- 删除 `is_default` 列；新建运行改为 **sticky 上次选择 + 首个兜底** 的预选。
- 用一支迁移一致性测试锁死 `graph ↔ template ↔ content ↔ outline` 的一致性，防漂移。

**非目标**：不改 `ProjectGraph` 投影算法；不改 `RenderTemplate` 渲染机制；不做流程模板运行时变量（`{{ .Goal }}` 等，仍属 prompt-template spec §12 的未来项）；不改设计器 UI；不改后端注入链路。

## 3. 关键设计决策（含取舍）

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 主默认去领域硬编码 vs 补拓扑 | **两者都做**（1 通用 + 3 预设） | 单个流程无论多通用只有一种拓扑；用户按任务选形状更有价值 |
| prompt 来源 | **每个流程手写完整 template**，不含 `{{ DAGPrompt }}` | 提示词质量不该被机械投影束缚；`template` 字段本就支持手写全文（prompt-template spec 的解耦初衷，legacy 无图流程即 `template=手写全文`） |
| graph 去留 | **保留** | 只用于设计器可视化 + 运行时 overlay 点亮；不留会让内置流程在设计器无 DAG、运行时不点亮，且与"刚做的 Phase 2/3a 投入"背离。代价：graph 与 prompt 分开写、可能漂移——内置流程我们手写两者一致，并用一致性测试兜底 |
| 预选默认流程机制 | **删除 `is_default` 列**，改前端 sticky 上次选择 + 首个兜底 | 有 4 个平级预设后，"标记唯一默认"变得牵强；sticky 上次选择比固定默认更贴合习惯，且让 `is_default` 彻底多余 |

## 4. 四个内置流程（graph + 手写 prompt）

所有流程：`content` 与 `template` **逐字节相同**（prompt 无 `{{ }}` 动作，`RenderTemplate` 对无动作字符串原样返回）；`outline` 由 `ProjectGraph(graph)` 产出（实现时跑真实渲染器取 byte-exact）；`tags` 仅展示。graph 里的 `brief` 仅供设计器节点详情展示，**不进注入 prompt**（因不走 DAGPrompt），但仍手写得与 prompt 意图一致。

> 下列 prompt 是**权威注入内容**；graph 是可视化投影。两者手写保持一致，bounce 边只影响 overlay 箭头、不进 prompt。

### 4.1 Parallel Decompose（通用主默认，seed 排第一）

**拓扑**：`See members → Break down → [Subtask A ∥ Subtask B] → Integrate → Verify → Wrap up`，bounce `Verify → Subtask A`。`Subtask A` 标 `sharedFiles:true`。
**tags**：`["General","Parallel"]`

**graph（节点）**：`see`(leader "See members") · `break`(leader "Break down") · `t1`(task "Subtask A", sharedFiles) · `t2`(task "Subtask B") · `int`(leader "Integrate") · `ver`(task "Verify") · `wrap`(leader "Wrap up")
**edges**：`see→break`, `break→t1`, `break→t2`, `t1→int`, `t2→int`, `int→ver`, `ver→wrap`, `ver→t1`(bounce)

**template / content**：

```markdown
# Parallel Decompose

You are the **Leader** of this run. You do not do the work yourself — you split it, dispatch each piece to the right member, and integrate what comes back. Every result returns to you; you decide the next move.

## Flow
1. **See members.** Call the roster before planning — never assume who is available or what they can do.
2. **Break down.** Split the goal into independent subtasks that can run in parallel without blocking each other. If the work is inherently sequential, use the Pipeline flow instead.
3. **Dispatch in parallel.** Send each subtask to the best-matched member. Every dispatch is one concrete task plus explicit acceptance criteria — vague briefs produce vague work. If two subtasks touch the same files, dispatch with isolate=true so they do not clobber each other.
4. **Integrate.** Pull the results together and resolve conflicts yourself; do not hand integration off to a subtask.
5. **Verify.** Dispatch a review/test pass with a clear bar: all checks green, no regressions. On failure, send the work back to the responsible subtask — do not patch over it yourself.
6. **Wrap up.** Finish with a concise summary @user: what was built, what was verified, and anything still open.
```

### 4.2 Sequential Pipeline（串行流水线）

**拓扑**：`Investigate → Design → Implement → Verify → Wrap up`（线性链，每步依赖上一步产出），bounce `Verify → Implement`。
**tags**：`["Sequential","Pipeline"]`

**graph（节点）**：`inv`(task "Investigate") · `des`(task "Design") · `impl`(task "Implement") · `ver`(task "Verify") · `wrap`(leader "Wrap up")
**edges**：`inv→des`, `des→impl`, `impl→ver`, `ver→wrap`, `ver→impl`(bounce)

**template / content**：

```markdown
# Sequential Pipeline

You are the **Leader** of a staged pipeline. Each stage consumes the previous stage's output, so order matters: never open a stage until its predecessor has met its acceptance criteria. You dispatch each stage, review the handoff, then pass it forward.

## Flow
1. **Investigate.** Dispatch a member to gather the facts, constraints, and unknowns. Acceptance: the questions the next stage needs answered are answered.
2. **Design.** Dispatch the plan/approach based on the investigation. Acceptance: a concrete design the implementer can follow without guessing.
3. **Implement.** Dispatch the build against the approved design. Acceptance: the design is realized, with tests where they apply.
4. **Verify.** Dispatch review/tests. Acceptance: all pass, no regressions. On failure, send it back to Implement — fix the stage that broke, do not bolt on a new one.
5. **Wrap up.** Finish with a concise summary @user: what each stage produced and what was verified.

Between every stage, confirm the handoff is solid before moving on. A weak earlier stage poisons everything downstream.
```

### 4.3 Research → Synthesize（研究综合）

**拓扑**：`Frame questions → [Angle A ∥ Angle B ∥ Angle C] → Synthesize → Wrap up(report)`。**无 Verify、无 bounce**——知识工作，收敛综合。
**tags**：`["Research","Knowledge"]`

**graph（节点）**：`frame`(leader "Frame questions") · `a1`(task "Angle A") · `a2`(task "Angle B") · `a3`(task "Angle C") · `syn`(leader "Synthesize") · `wrap`(leader "Wrap up")
**edges**：`frame→a1`, `frame→a2`, `frame→a3`, `a1→syn`, `a2→syn`, `a3→syn`, `syn→wrap`

**template / content**：

```markdown
# Research → Synthesize

You are the **Leader** of a research effort. This flow produces understanding, not code: you frame the questions, fan out independent investigations, then converge the findings into one coherent answer. Every finding returns to you.

## Flow
1. **Frame the questions.** State what you actually need to know and the distinct angles worth investigating. Good framing keeps the parallel work non-overlapping.
2. **Investigate in parallel.** Dispatch one member per angle. Each returns findings with sources/evidence, not opinions — an unsourced claim is a lead, not a conclusion. Angles are independent; they do not wait on each other.
3. **Synthesize.** Pull every angle together yourself. Reconcile conflicts, note where sources disagree, and separate what is well-supported from what is uncertain. Do not just concatenate the reports.
4. **Wrap up.** Deliver a concise report @user: the answer, the confidence behind it, the key evidence, and what remains open.

Do not write or verify code in this flow — if the task turns into building something, switch to Parallel Decompose or Pipeline.
```

### 4.4 Generate → Review → Iterate（生成评审迭代）

**拓扑**：`Produce → Review → Wrap up`，bounce `Review → Produce`。强调评审者独立/对抗。
**tags**：`["Quality","Loop"]`

**graph（节点）**：`prod`(task "Produce") · `rev`(task "Review") · `wrap`(leader "Wrap up")
**edges**：`prod→rev`, `rev→wrap`, `rev→prod`(bounce)

**template / content**：

```markdown
# Generate → Review → Iterate

You are the **Leader** of a quality-gated loop. One member produces, a *different* member reviews adversarially, and nothing ships until it passes. The whole point is the separation: the reviewer must not be the producer.

## Flow
1. **Produce.** Dispatch the work with a concrete task and explicit acceptance criteria. Acceptance is what Review will hold it to, so make it testable.
2. **Review.** Dispatch a *separate* member to review adversarially — actively try to break it, find the gaps, check the claims. A rubber-stamp review is worse than none. Acceptance: the reviewer signs off that the criteria are genuinely met.
3. **Iterate.** On any real issue, send it back to Produce with the specific defects — reuse the same node, do not spawn a new one. Loop until Review passes clean.
4. **Wrap up.** Finish with a concise summary @user: what was produced, what Review caught, and how it was resolved.

Keep producer and reviewer distinct across iterations so the review stays honest.
```

## 5. 数据模型影响：删除 `is_default`

`is_default` 目前的两个作用：①标记内置默认流程；②驱动新建运行弹窗预选。删列后 ① 不再需要（4 个平级预设），② 由前端 sticky 逻辑接管（§7）。

> **强耦合**：`ALTER TABLE ... DROP COLUMN is_default`（§6）与删除 `workflow_entity.IsDefault` 字段必须**同批落地**。GORM 按 struct 字段 SELECT，若列已 DROP 而字段还在，启动后任何 `workflows` 查询都会因 `no such column: is_default` 报错。因此本 spec 的迁移 + entity/DTO 清理是一个不可拆分的原子改动。

需要清理的读写点（实现时以 `grep -rn 'is_default\|IsDefault\|isDefault'` 为准，至少含）：

- `internal/model/entity/workflow_entity/workflow.go:24` — 删 `IsDefault int` 字段。
- `internal/service/workflow_svc/types.go:16` — 删 `WorkflowItem.IsDefault`。
- `internal/service/workflow_svc/*.go` — Create/Update/List 里对 `IsDefault` 的映射（若有）。
- `internal/app/orch_adapter.go` 及任何读 `w.IsDefault` 的适配器。
- `internal/repository/workflow_repo/**` — 若查询/排序引用了 `is_default`。
- 前端 `frontend/src/components/agentre/orchestration/run-new-dialog.tsx:79,191,194-195` — 删 `WorkflowOption.isDefault` 与 `items.find(w => w.isDefault)` 预选。
- `frontend/wailsjs/**` 生成物 — `make generate` 重新生成即随 DTO 去掉 `isDefault`。

## 6. 迁移策略

新增迁移 `migrations/202607080001_workflow_default_presets.go`，追加到 `migrationList()` 末尾（**不改旧迁移**）。一支迁移内按序做三件事：

1. **删旧内置默认行**（趁 `is_default` 列仍在）：`DELETE FROM workflows WHERE is_default = 1`。这会去掉 dev/新装机上 040001 seed 的旧 `Default Orchestration Flow`。
2. **插 4 个新流程**（每个 `WHERE NOT EXISTS(SELECT 1 FROM workflows WHERE name = ?)` 守卫幂等，`status=1`）：
   - 顺序 `Parallel Decompose → Sequential Pipeline → Research → Synthesize → Generate → Review → Iterate`。
   - `content` 与 `template` 写同一手写全文；`graph` / `tags` / `outline` 手写；`outline` 实现时跑 `workflow_svc.RenderWorkflowContent` 取 byte-exact。
   - **`updatetime`/`createtime` 带递减偏移**，保证 repo 的 `updatetime DESC` 排序稳定产出 `Parallel Decompose > Pipeline > Research > Review`（前端"首个兜底"= `items[0]` 因此确定性命中 Parallel Decompose）。例如 base = `strftime('%s','now')*1000`，四行分别 `base+3 / base+2 / base+1 / base+0`。
3. **DROP `is_default` 列**：`ALTER TABLE workflows DROP COLUMN is_default`（本仓 SQLite 支持原生 DROP COLUMN，见 040001 的 rollback 已用）。

**Rollback**：反向——`ALTER TABLE workflows ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0` → `DELETE` 四行 → 重插旧 `Default Orchestration Flow`（`is_default=1`）。（rollback 主要为对称，实际不常用。）

迁移码保持**纯原生 SQL、不 import service**（沿用现有迁移自包含风格；一致性由 §8 的测试保证，而非迁移时调用渲染器——避免 migrations→service 反向依赖，且避免"迁移时按当时代码渲染"导致新旧安装内容漂移）。

## 7. 前端：新建运行 sticky 预选

`run-new-dialog.tsx` 的 `WorkflowList().then` 分支改为：

```ts
const items = (resp?.items ?? []).map((w) => ({
  id: w.id, name: w.name, tags: w.tags ?? [],
  outline: w.outline ?? [], graph: w.graph ?? "",
})); // 去掉 isDefault
setWorkflows(items);
const lastId = Number(localStorage.getItem(LAST_FLOW_KEY) || 0);
const picked = items.find((w) => w.id === lastId) ?? items[0];
if (picked) setFlowId(picked.id);
```

- `LAST_FLOW_KEY`：一个模块级常量，如 `"agentre.orchestration.lastFlowId"`（localStorage 是本仓已有的前端持久化套路，后台任务面板即用它）。
- **持久化时机**：`submit()` 成功创建后、library 模式下写回 `localStorage.setItem(LAST_FLOW_KEY, String(flowId))`（记录"上次实际使用的流程"）。adhoc 模式不写。
- **兜底**：`items[0]`（因 §6 排序 = Parallel Decompose）。列表空则不预选（维持 `flowId=0`）。
- **无新增可见文案** → 无 i18n 改动；ESLint `no-literal-string` 不触发。

## 8. 测试（TDD：先红后绿）

### 8.1 迁移一致性测试（核心，走真实 DB）

`migrations/202607080001_workflow_default_presets_test.go`（迁移自测例外，允许真实 DB；**仅测试文件** import `workflow_svc`，生产迁移码不 import——无 import cycle，`workflow_svc` 不 import migrations）。

跑完全部迁移后读出 `workflows` 全部行，断言：

1. 恰好存在 4 个预期内置流程，`name` 命中 `{Parallel Decompose, Sequential Pipeline, Research → Synthesize, Generate → Review → Iterate}`，各自 `tags` 非空。
2. 旧 `Default Orchestration Flow` 已不存在；`is_default` 列已被 DROP（查 `PRAGMA table_info(workflows)` 无该列 / 或 `SELECT is_default` 报错）。
3. 排序：按 `updatetime DESC` 取首行 = `Parallel Decompose`。
4. **一致性**：对每行，`RenderWorkflowContent(name, graph, template)` 的返回 `content` == 存储 `content`、返回 `outline`(JSON) == 存储 `outline`。这把 graph↔template↔content↔outline 永久锁死、防手写漂移。

先写该测试（红：迁移未写 / 旧数据在），再写迁移（绿）。

### 8.2 前端预选测试

`run-new-dialog` 对应 vitest（更新既有测试或新增）：

- 首次（localStorage 空）→ 预选 `items[0]`（Parallel Decompose）。
- localStorage 有有效 `lastFlowId` → 预选该流程。
- localStorage 的 `lastFlowId` 已不在列表 → 回退 `items[0]`。
- library 模式提交成功后 → `localStorage` 写入所选 `flowId`。

（wails runtime mock 依既有 per-file `vi.mock` 规则，见项目约定。）

## 9. 收尾门控

- `make test-backend` + `make lint` 看真 exit code；补跑 `gofmt -l`。
- 全量 `pnpm test`（预选逻辑改动可能牵动跨文件）+ `tsc` + `eslint`。
- `make generate` 刷 wailsjs 绑定（DTO 去 `isDefault`）。
- 手动确认：新建运行弹窗四个流程正确出现、sticky 预选生效、四个流程在设计器里图/prompt 一致。

## 10. 涉及文件一览

**后端**
- 新增 `migrations/202607080001_workflow_default_presets.go`（+ `_test.go`）
- 改 `migrations/migrations.go`（追加到 `migrationList()`）
- 改 `internal/model/entity/workflow_entity/workflow.go`（删字段）
- 改 `internal/service/workflow_svc/types.go`（删 DTO 字段）+ 相关映射 `.go`
- 改 `internal/app/orch_adapter.go` / `internal/repository/workflow_repo/**`（若引用 `is_default`）

**前端**
- 改 `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`（去 `isDefault` + sticky 预选）
- 改对应 `*.test.tsx`
- `frontend/wailsjs/**`（`make generate` 重生成）

## 11. 风险与缓解

- **手写 content/outline 漂移** → §8.1 一致性测试锁死。
- **`items[0]` 排序不确定** → §6 updatetime 偏移保证确定性。
- **`is_default` 清理遗漏** → §5 grep 全量清点 + `make lint`/`tsc` 兜底。
- **graph 与 prompt 分离导致设计器"改图不改词"困惑** → 属 prompt-template 既有 UX（template 与 graph 本就解耦），非本次回归；内置流程两者手写一致。
- **DROP COLUMN 兼容性** → 本仓 040001 rollback 已用原生 DROP COLUMN，验证可行；若某环境不支持则表重建（plan 阶段决定）。
