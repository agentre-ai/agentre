# 编排流程 · 可编辑提示词模板(Go text/template + {{ DAGPrompt }})

- 日期:2026-07-05
- 仓库:`agentre/`(桌面端)
- 状态:设计定稿,待转 writing-plans
- UI 参考(Pencil `agentre.pen`,「8. Orchestration」板块):
  - `编排 — 编辑流程 · DAG 设计器 · Light`(完整弹窗,编辑态)
  - `提示词模板 — 编辑 ⇄ 预览 · Light`(那块面板的 编辑 / 预览 / 报错 三态对照)

## 1. 背景与问题

编排「流程」当前是 **DAG 为唯一真源**:`workflows.graph`(JSON)经确定性纯函数 `ProjectGraph`(`workflow_svc/projection.go`)投影成散文 `workflows.content` → `CreateRun` 把 `content` 快照进 `orchestration_runs.flow_content` → `turn.go` 的 `BuildTurnExtras` 注入 Leader。

问题:只要流程带 DAG,`content` 就被投影**完全接管、只读**——`workflow_svc/workflow.go` 的 `applyGraph` 每次保存都用 `ProjectGraph` 覆盖 `content`。DAG 设计器右下那块只是**只读预览**。唯一能自由写文本的是「legacy 无图」模式(`workflow-manager-dialog.tsx` 的 `EditorPane`),但那样就**没有 DAG**。

→ 用户要的能力缺失:**在「新建/编辑流程」弹窗里自己编辑提示词**,并可**选择性**地把 DAG 投影嵌进去。

## 2. 目标 / 非目标

**目标**
- 流程带一个**用户可编辑的提示词模板** `template`。
- 模板里可放占位符把 DAG 投影**渲染**进任意位置;不放 = 纯自由文本;左侧不画 DAG = 纯提示词流程。三种情况都可保存。
- 占位符走 **Go `text/template`**(注册函数 `DAGPrompt`),顺带获得 `{{ if }}` 条件、`{{ .FlowName }}` 等能力。
- 模板语法错误要有**报错态**:预览显示错误、保存置灰,绝不写坏 `content`。
- 现有流程**零感知**:默认模板 = 只放 `{{ DAGPrompt }}`,渲染结果与今天逐字节一致。

**非目标(本次不做)**
- 运行时变量(`{{ .Goal }}`、成员列表等)——渲染发生在**保存流程时**,那一刻还没有 Run,这些值不可用;属于更大的改动,以后单独设计。
- 不动系统级 `orchGuidance`(`turn.go` 里那段固定框架提示词);它不在流程库里。
- 不动 `run.FlowContent` 快照链路、`turn.go` 注入、`ProjectGraph` 投影算法、`orch_adapter` 的 `FlowContentByID`。

## 3. 核心模式

`template`(用户写的原始 Go 模板,真源之一)与 `content`(渲染产物)分离:

```
graph(JSON) ──ProjectGraph──▶ dagPrompt(散文)
                                      │  作为注册函数 DAGPrompt 的返回
template(用户编辑) ──text/template 渲染──▶ content ──(既有链路,不动)──▶ run.FlowContent ──▶ turn.go 注入 Leader
```

- **占位符 token**:`{{ DAGPrompt }}`(注册函数,niladic,返回 `ProjectGraph` 的投影文本)。`{{DAGPrompt}}` / `{{ DAGPrompt }}` 皆合法(Go 模板会 trim 内部空格)。
- **默认模板**:新建 / 现有带图流程 = 字面量 `{{ DAGPrompt }}`。它渲染 = `dagPrompt` 本身,与当前 `content` 逐字节一致。
- **渲染时机**:保存流程时(沿用 `applyGraph` 的位置)。预览(`WorkflowPreviewGraph`)必须用**同一渲染函数**,保证「所见即注入」。

## 4. 数据模型与迁移

### 4.1 实体 `workflow_entity.Workflow`(`internal/model/entity/workflow_entity/workflow.go`)

新增字段:

```go
Template string `gorm:"column:template;type:text;not null;default:''"` // 用户编辑的提示词模板(Go text/template 真源)
```

`Content` 语义变更:从「DAG 投影」变为「`template` 的渲染产物」。`Graph`/`Outline`/`Tags`/`IsDefault` 不变。

### 4.2 迁移(追加到 `migrationList()` 末尾,`202607050001_workflow_template.go`)

纯 SQL,无需调 Go 投影(见理由):

```sql
ALTER TABLE workflows ADD COLUMN template TEXT NOT NULL DEFAULT '';
-- 带图流程:模板=只放占位符;其 content 已=投影,render('{{ DAGPrompt }}') 逐字节一致
UPDATE workflows SET template = '{{ DAGPrompt }}' WHERE graph != '';
-- legacy 无图流程:模板=既有自由文本;content 不变(无占位符→原样)
UPDATE workflows SET template = content WHERE graph = '';
```

> 理由:带图行的 `content` 今天已经等于 `ProjectGraph` 输出;而 `render('{{ DAGPrompt }}', dag)` 恰好输出 `dag` 本身,故 `content` 无需重算。legacy 行 `template = content`,无占位符渲染即原样。二者都可纯 SQL 回填,遵守「迁移不调 Go 投影」。

## 5. 渲染:`workflow_svc`

### 5.1 新增渲染函数(`workflow_svc/render.go`,新文件,SRP)

```go
// RenderTemplate 用 Go text/template 渲染模板;DAGPrompt() 返回 dagPrompt。
// 解析/执行失败返回 error(调用方据此阻止保存、预览显示错误)。
func RenderTemplate(tmpl, name, dagPrompt string) (string, error)
```

- `text/template.New("workflow").Funcs(template.FuncMap{"DAGPrompt": func() string { return dagPrompt }}).Parse(tmpl)`;
- `Execute` 数据 = `struct{ FlowName string }{name}`(暴露 `{{ .FlowName }}`);
- 用 `text/template`(**非** `html/template`),提示词不做 HTML 转义。

### 5.2 `Create` / `Update`(`workflow_svc/workflow.go`)

替换现有 `applyGraph` 的「投影覆盖 content」为「渲染模板 → content」:

1. `w.Name = req.Name`;`w.Template = req.Template`。
2. Graph 赋值保留既有 **空图 no-op 守卫**(Update 不重发 graph 不清空已存 DAG):`if trimmed := strings.TrimSpace(req.Graph); trimmed != "" { w.Graph = trimmed }`。
3. 计算 `dagPrompt`:`if g, ok := ParseFlowGraph(w.Graph); ok { content, outline := ProjectGraph(w.Name, g); dagPrompt = content; w.Outline = encode(outline) } else { dagPrompt = "" }`(`Outline` 仍由 graph 派生,与模板无关)。
4. `rendered, err := RenderTemplate(w.Template, w.Name, dagPrompt)`;`err != nil` → **返回错误,不保存**(`content` 保持旧值);`err == nil` → `w.Content = rendered`。

> 空模板 `template == ""` → 渲染结果 `""` → `content == ""` → Leader 不注入流程(等价「无流程」)。新建默认模板非空,故正常路径不会误入空。

### 5.3 错误契约(LSP / 状态可见性)

- `Create`/`Update` 在模板 parse/execute 失败时返回带说明的 error(含 Go 模板报的 `function "X" not defined` 等),controller/binding 原样上抛,前端展示。
- 绝不把半渲染或坏字符串写进 `content`。

## 6. 预览绑定(`internal/app/workflow.go`)

`WorkflowPreviewGraph` 扩展为「模板感知」,保证预览 = 保存渲染:

- 请求 `WorkflowPreviewRequest` 增 `Template string`。
- 响应 `WorkflowPreviewResponse` 增 `Error string`(parse/execute 失败时填,`Content` 空)。
- 逻辑:`dagPrompt = ParseFlowGraph(req.Graph) ? ProjectGraph(name, g).content : ""`;`content, err := workflow_svc.RenderTemplate(req.Template, req.Name, dagPrompt)`;err → `{Error: err.Error()}`,否则 `{Content, Outline}`。

## 7. DTO / 绑定(`workflow_svc/types.go` + `internal/app/workflow.go`)

- `WorkflowItem` 增 `template`。
- `CreateWorkflowRequest` / `UpdateWorkflowRequest` 增 `template`(create 有默认由前端给 `{{ DAGPrompt }}`)。
- `make generate` 刷新 `frontend/wailsjs/`(生成物,不提交)。

## 8. 前端

### 8.1 `use-workflows.ts`
`WorkflowItem` 类型增 `template`;`create`/`update` 转发 `template`。

### 8.2 `workflow-dag-designer.tsx`(核心改动,对齐 Pencil `编辑流程 · DAG 设计器`)
右下原**只读预览** → **可编辑「提示词模板」pane**:
- 头部:`提示词模板` + `可编辑` 徽标 + `编辑 / 预览` 分段切换。
- 工具条:`插入 {{ DAGPrompt }}` 按钮(在光标处插入 token)+ 说明。
- **编辑态**:`template` 文本域(占位符 pill 高亮),`onChange` → `draftTemplate`。
- **预览态 / 报错态**:250ms 防抖调 `WorkflowPreviewGraph({name, graph, template})`;`resp.error` → 红框错误条(`triangle-alert` + 文案);否则 `MarkdownText` 渲染 `resp.content`。
- 组件对外抛 `templateError: boolean`,供弹窗置灰保存。

### 8.3 `workflow-manager-dialog.tsx`
- 草稿态增 `draftTemplate`;新建默认 `{{ DAGPrompt }}`;编辑载入 `w.template`(迁移已回填,老数据必有值)。
- **两条现有路径都改成写 `template`(最小改动,保留 mode 切换)**:
  - 有图 → 设计器 `DesignerPane`,右下模板 pane 编辑 `template`(可含 `{{ DAGPrompt }}`)。
  - 无图 → `EditorPane` 现在编辑 `template`(纯自由文本;无 DAG 时 `{{ DAGPrompt }}` 渲染为空)。`content` 不再由用户直接写,统一是 `template` 的渲染产物。
  - `转 DAG` 按钮语义不变(切到设计器加节点)。**不强拆/不合并两个 pane**——避免无关重构;彻底统一成单一模板编辑器是以后的可选项。
- `canSave` 增 `!templateError`;`submit` 串 `template`(不再传 `content`,由后端渲染)。

### 8.4 i18n(强制)
新增可见文案(`提示词模板`/`可编辑`/`编辑`/`预览`/`插入 {{ DAGPrompt }}`/占位符说明/报错文案等)进 `zh-CN` + `en` 两份 `common.json`;token `{{ DAGPrompt }}` 是数据不翻译。跑 `i18n.test.ts`。

## 9. 不变量(回归护栏)
- `orch_svc/turn.go` `BuildTurnExtras`、`orch_svc/create.go` `CreateRun` 快照、`orch_adapter.go` `FlowContentByID` **零改动**;`turn_test.go`(tags/outline 永不注入)保持绿。
- `ProjectGraph` 算法不动。

## 10. 测试(TDD:Red → Green → Refactor)

**后端**
- `workflow_svc/render_test.go`:`{{ DAGPrompt }}` → 恰为 dagPrompt;前后包裹文本正确拼接;`{{ .FlowName }}` 生效;`{{ if }}` 分支;空模板 → `""`;**parse 错误**(未定义函数 / 坏语法)→ 返回 error 且不改 content。
- `workflow_svc/workflow_test.go`:Create/Update 渲染进 content;渲染失败不落库;空图 + 占位符 → content 空;legacy 无占位符文本 → 原样;Update 空图 no-op 不清 graph。
- 迁移测试:回填后带图行 `template='{{ DAGPrompt }}'` 且 content 不变、legacy 行 `template=content`。
- repo sqlmock:新增 `template` 列的 Create/Update WithArgs 补齐(参照 Phase 1 `202607040001` 曾漏更 sqlmock 的坑)。
- `app/workflow_test.go`:`WorkflowPreviewGraph` 带 template 返回渲染 content;坏模板返回 `Error`。
- 护栏:`orch_svc` `turn_test` 保持绿。

**前端**(Vitest)
- 设计器模板 pane:编辑改 `draftTemplate`;插入按钮插 token;预览态渲染 content;报错态显红条并置灰保存。
- `use-workflows` 转发 template;`workflow-manager-dialog` 默认 `{{ DAGPrompt }}`、载入回填、`canSave` 联动 `templateError`。
- `i18n.test.ts` 覆盖新 key。
- 全量 `pnpm test` + `tsc` + `eslint`(收尾看真 exit code,勿只信 focused)。

## 11. 落地缝一览(已核对代码)
| 关注点 | 位置 |
|---|---|
| 实体加字段 | `internal/model/entity/workflow_entity/workflow.go` |
| 迁移 | `migrations/202607050001_workflow_template.go`(追加末尾) |
| 渲染函数 | `internal/service/workflow_svc/render.go`(新) |
| 渲染接入 | `internal/service/workflow_svc/workflow.go`(`Create`/`Update`,替换 `applyGraph`) |
| DTO | `internal/service/workflow_svc/types.go` |
| 预览 + 绑定 | `internal/app/workflow.go`(`WorkflowPreview*`) |
| 前端 hook | `frontend/src/hooks/use-workflows.ts` |
| 设计器 pane | `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx` |
| 弹窗草稿 / 两路径写 template | `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx` |
| i18n | `frontend/src/i18n/locales/{zh-CN,en}/common.json` |

## 12. 未决 / 以后
- 运行时变量(`{{ .Goal }}`、成员、Leader agent):需把渲染时机从「保存流程」下沉到 `CreateRun`/`turn.go`,`content` 改存原始模板 —— 较大架构改动,本次不做。
- 模板编辑器语法高亮 / 自动补全 —— 增强项,非必需。
