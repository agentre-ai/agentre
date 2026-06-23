# 群聊任务卡编排(Group Task Orchestration)设计

日期:2026-06-11
状态:已与用户对齐方向(见 §2 决策摘要),UI 已落 agentry.pen 设计稿;术语统一用「主持人」(host)
前置:[2026-06-03 群聊编排设计](2026-06-03-group-chat-orchestration-design.md)(@ 寻址 + fan-out 骨架,已实现)

## 1. 背景与目标

群聊 MVP 已经支撑了完整的消息流转:用户 @主持人 派需求、主持人 `group_send` @成员派活、
成员回复自动回到触发来源(`applyFallback`)、多收件人并发起轮。目标场景——

> 用户 @主持人「按设计稿重构 UI/UX」→ 主持人派给开发,开发自测后交付 →
> 主持人并行派 e2e 验证 + 代码审查 → 全部通过后汇总回用户

——在机制层今天就能跑,但有四个缺口:

1. **流程纪律全靠 LLM 自觉**:「开发自测后才交付」「验证通过才算完成」没有结构化痕迹,
   长任务多轮后主持人 context 里全是聊天流水,容易丢任务。
2. **工作空间协同空白**:成员 backing session 全部绑定群的 `project_id` 同目录工作,
   并发写无任何机制(前置设计明确「强制隔离 OUT」)。
3. **组队成本高**:每次手工建 agent、配 prompt、拉群。
4. **主持人编排智能没有抓手**:拆任务/串并行/验收只靠群系统提示里几句引导。

## 2. 决策摘要(已与用户对齐)

| 维度 | 决策 |
| ---- | ---- |
| 任务形态 | **实体级任务卡**:新表 `group_tasks` + task 类 MCP tool,状态流转落库可追溯;软验收门(工具 schema 强制交付物),不做硬门 |
| 工作空间 | **同目录,不互斥**:允许多个任务并行(e2e/审计本质是读者);写冲突由主持人编排判断,不加调度层约束 |
| 组队方式 | **普通建群弹窗手动组队**(「从部门一键预选」已砍):成员可跨部门任选,角色=agent 的 `PromptJSON`;群内随时 `group_invite` 动态招募(池=全部可用 agent) |
| 编排智能 | **内置提示 + 工具自描述**:强化 `buildGroupSystemPrompt` 主持人/成员段落 + task tool description 写明纪律;不加 playbook 实体、不做流程引擎 |
| 共享交付物 | **`.agentre/handoff/<group_id>/` 约定**:过程交付物落此处、卡片传指针;正式产物照常进仓库;纯提示词约定(§6.2) |
| 流程固化 | **流程实体(剧本库)**:`workflows` 表 + 群可选绑定 `workflow_id`,主持人每轮注入最新——与部门/项目正交,跨部门可复用(§6.1);`group_invite` 池随之放宽到全部可用 agent(§7) |
| agent 拉起团队 | **`group_create` 注入单聊**:走现有工具审批门;发起者=主持人;`brief` 进群触发首轮(§7.1) |

## 3. 数据模型

### 3.1 新表 `group_tasks`

migration 追加到 `migrationList()` 末尾,原生 SQL:

```
id                  INTEGER PK AUTOINCREMENT
group_id            BIGINT NOT NULL
task_no             INT NOT NULL          -- 群内自增序号,展示用(#3);在 ingestMu 临界区内分配
title               TEXT NOT NULL         -- 短标题
brief               TEXT NOT NULL         -- 任务说明,验收标准写在这里
creator_member_id   BIGINT NOT NULL       -- 建卡成员(本期任务卡只由成员经 MCP 创建)
assignee_member_id  BIGINT NOT NULL       -- 执行成员
status              TEXT NOT NULL         -- open / done / canceled
result              TEXT NOT NULL DEFAULT '' -- 交付物,done 时必填(改了什么、怎么自测的)
parent_task_no      INT NOT NULL DEFAULT 0 -- 验证类任务回指被验证任务的群内编号(#N,非 DB id),溯源用
createtime / updatetime
UNIQUE(group_id, task_no)
```

状态机刻意小:`open → done / canceled`。

- 不做 `in_progress`:eager 调度下 assignee 立刻起轮,「在跑」由 roster run state 表达。
- 不做 `verified` 状态:验收 = 主持人另建验证任务(`parent_task_no` 回指),软门。
- 打回 = 新建任务;不可重开已 done 的卡。

充血实体 `group_entity.GroupTask`(任务卡属 group 域,与 GroupMember/GroupMessage 同包):`Check`(title/assignee 必填、status 枚举)、
`IsOpen()`、`CanComplete(memberID)`(仅 assignee)、`CanCancel(memberID, isHost)`(creator 或主持人)。

### 3.2 新表 `workflows`(流程/剧本库)+ `groups` 加一列

```
workflows:
id          INTEGER PK AUTOINCREMENT
name        TEXT NOT NULL              -- 「产品开发流程」「紧急修复流程」…
content     TEXT NOT NULL              -- SOP 正文,见 §6.1
status      INT  NOT NULL DEFAULT 1
createtime / updatetime

groups 加列:
workflow_id BIGINT NOT NULL DEFAULT 0  -- 可选绑定;0 = 不绑定流程
```

流程与部门/项目**正交**:流程是「协作方式」不属于任何组织单元,跨部门协作直接成立。
两个 migration 都随 PR1(注入逻辑要读);流程库 CRUD 与建群下拉 UI 在 PR3。

### 3.3 `group_messages` 加两列

```
task_id     BIGINT NOT NULL DEFAULT 0
task_event  TEXT   NOT NULL DEFAULT ''   -- '' / created / completed / canceled
```

任务事件**以消息形式进群流**,复用全部现有投递管线(seq、recipient、FIFO、kick);
`task_event != ''` 的消息前端渲染为任务卡气泡而非文本。

## 4. MCP tools(group MCP server 扩展)

鉴权复用现有无状态 HMAC token + `memberCanPost`。三个新 tool,**所有成员可见**:

| tool | 参数 | 权限 | 行为 |
| ---- | ---- | ---- | ---- |
| `group_task_create` | `assignee, title, brief, parentTaskId?` | 任意成员(支持「开发直接派测试」);assignee 不能是自己(防自循环,返回专用错误 `GroupTaskSelfAssign` 提示模型直接执行) | 分配 task_no 落卡 + 落一条 `task_event=created` 消息投递给 assignee(天然触发其轮次),返回任务编号(#N) |
| `group_task_complete` | `taskId, result` | 仅 assignee;`result` 必填 = 软验收门 | 卡置 done + 落 `completed` 消息投递给 creator |
| `group_task_cancel` | `taskId, reason` | creator 或主持人 | 卡置 canceled + 落 `canceled` 消息投递给 assignee 与 creator |

`taskId` / `parentTaskId` 参数语义均为**群内任务编号(#N)**,不是 `group_tasks.id`。

**不做 `group_task_list` tool**:每次起轮在 system prompt suffix 注入快照——成员看到
「你名下未完成任务」,主持人额外看到「全群未完成任务」。`buildGroupSystemPrompt`
每轮重建,零成本,且 LLM 不会忘记查。

错误码:`GroupTaskNotFound` / `GroupTaskForbidden` / `GroupTaskClosed` / `GroupTaskResultRequired` / `GroupTaskSelfAssign`。

## 5. 消息联动与调度语义

- **created** → `persistMessage(recipient=[assignee], task_id, task_event)`,正文抬头
  `(来自 X 的任务 #N)` + title + brief → 进 assignee FIFO → `kick()`。
- **completed** → 投递给 creator,creator 收到后自主决定下一步(派验证/打回/汇总)。
  creator 已离群时走既有 fallback 链(回触发来源/最近发言者/用户)。
- **无互斥**:多任务可并行 open;同目录写冲突由主持人判断,提示词写明
  「可能改同一片代码的任务请串行派」。
- **成员离群** → 其名下 open 任务自动 canceled + 落 system 消息(`RemoveGroupMember` 级联)。
- **StopGroup/PauseGroup 不动任务状态**:停的是轮次,卡还在;恢复后快照仍在提示里。
- **round_count 语义不变**;task 消息与普通消息同样计轮。
- 与 `IngestAgentMessage` 共用 per-group `ingestMu` 串行化「task_no 分配 + seq + 落库 + 入队」。

## 6. 编排提示强化(`buildGroupSystemPrompt`)

- **主持人段**:标准动作环——理解需求 → 拆解 → `group_task_create` 派活(可能冲突的写任务
  串行派) → 收 completed → 派验证任务(测试/审计可并行,`parentTaskId` 回指) →
  全部通过后汇总 @用户;发现问题 → 新任务打回。
- **成员段**:收到任务 → 在项目目录执行 → **自测** → `group_task_complete`
  (result 写清改动文件 + 自测情况);需要协作可自己 `group_task_create` 或 `group_send`。
- task tool 的 description 同步写明上述纪律(双保险)。

### 6.1 流程(SOP)的放置:agent 只定义技能,流程放主持人层

多角色流水线(如产品开发小组:产品 → UI → 开发 → 测试)的流程知识**不写进成员 agent**——
成员的 `PromptJSON` 只写各自技能(产品:需求→PRD;UI:PRD→设计稿;开发:实现+自测;测试:e2e),
这样成员可跨小组复用、换流程不动成员。流程按固化程度三级,全部只对主持人生效:

1. **用户首条消息**(一次性流程,最高优先);
2. **主持人 agent 的 `PromptJSON`**(该主持人的个人编排习惯);
3. **流程实体(剧本库)= 固化级**:流程是「协作方式」,不属于任何部门——跨部门协作
   (产品部 → 设计部 → 研发部 → 测试部)下钉在部门字段上会没有归属。`workflows` 表
   (§3.2)集中维护 SOP,建群时可选绑定,`buildGroupSystemPrompt` 主持人段**每轮注入
   绑定流程的最新内容**(改流程对进行中的群即时生效)。「产品开发流程」**写一次即固化:
   日常用户只需 @主持人 描述需求,不必每次口述流程**;首条消息那一级仅用于临时覆盖。
   (表 + 注入随 PR1;流程库管理 + 建群下拉随 PR3。)

内置提示只教通用动作环(拆卡/派活/验收/并行),**不含任何领域流程**。

配套约定:

- **交接物经同目录文件系统流转**:卡片 `brief`/`result` 只传指针(文件路径 + 摘要),
  下一棒从上一棒的交付物继续——这是「同目录工作区」决策的直接红利;落点约定见 §6.2。
- **中心化为默认**:每棒 completed 回到 creator(主持人),由它派下一棒,流程始终在主持人
  手里。若团队 SOP 偏好接力直派(开发完成直接派测试),任意成员可 `group_task_create`
  已天然支持,无需新机制;但 completed 会回建卡人而非主持人,汇总多绕一圈,提示词不主动引导。

### 6.2 共享交付物目录 `.agentre/`

- 约定 `<工作目录>/.agentre/handoff/<group_id>/` 存放**过程交付物**——不打算进版本库的
  群内交接物(PRD 草稿、设计说明、评审意见、测试报告等);正式产物(代码、要进 repo 的文档)
  照常放仓库正常位置,不进 `.agentre/`。
- 文件命名建议 `task-<task_no>-<slug>.md`;卡片 `brief`/`result` 引用相对路径。
- 纯提示词约定(主持人/成员段都注入),agent 自行 `mkdir -p`;首次写入前把 `.agentre/`
  追加进 `.git/info/exclude`(仅本地忽略,不污染用户仓库的 `.gitignore`)。
  零应用代码侵入,属 PR1 提示强化范围;`handoff/` 之外的 `.agentre/` 命名空间留给未来
  项目级配置,不在本期使用。

### 6.3 workflow 正文怎么写(作者指南)

正文是**写给主持人读的自由 Markdown**,不是 DSL——没有解析器,没有强制 schema,
LLM 直接消费。约定:

- **长度控制在几十行内**:每轮注入主持人提示词,长度即 token 成本。
- **推荐骨架四段**(编辑器「插入骨架模板」一键插入,模板文案进 i18n 两语言):
  1. `适用`:什么场景用这个流程;
  2. `角色`:用**抽象角色**(产品/UI/开发/测试),**不写 agent 真名**——主持人按
     roster 成员的名字与职责对号入座,同一流程在不同班子间复用;缺角色时先
     `group_invite` 招募或询问用户;
  3. `步骤`:顺序 + 每棒的交付物落点(`.agentre/handoff/<群>/` 路径约定,§6.2)+
     验收标准;明确标注**可并行的环节**与验证任务的回指关系;
  4. `纪律`:每步用任务卡交接、交付物写明路径、可能改同一片代码的任务不并行等。
- 示例(即设计稿编辑弹窗内的正文):

```markdown
# 产品开发流程

适用:新功能从需求到验收的完整交付。

## 角色
- 产品:需求分析,产出 PRD
- UI:界面设计,产出设计稿说明
- 开发:实现与自测
- 测试:e2e 验证
(按群成员职责对号入座;缺角色先 group_invite 招募或询问用户)

## 步骤
1. 产品:需求 → PRD,交付 handoff/task-N-prd.md,含验收标准
2. UI:依据 PRD 出设计稿说明,引用上一棒交付物
3. 开发:依据 1+2 实现并自测,result 写明改动文件与自测结论
4. 并行验证:测试跑 e2e;涉核心逻辑另派代码审查(回指 3)
5. 全部通过 → 汇总 @用户;有问题 → 打回对应环节(新任务卡)

## 纪律
- 每一步都用任务卡交接,交付物写明路径
- 可能改同一片代码的任务不要并行
- 验收标准在派活时写进 brief
```

## 7. 组队方式

建群走**普通建群弹窗手动组队**(「从部门一键预选」入口已砍,部门仅作组织结构):

- 建群弹窗:群标题 + 主持人(经 `CapMCPTools` 门控)+ 项目 + **「协作流程」下拉(可选,
  绑定 workflows)** + 初始成员(可跨部门任选,逐个经能力门控 + 8 人上限)。
- 角色设定即 agent 的 `PromptJSON`:用户维护「产品/UI/开发/测试」等角色 agent,
  不新增角色实体。
- **`group_invite` 招募池放宽**(修订 2026-06-03 spec 的部门池决策):从「群绑定部门」
  放宽为**全部 active 且 backend 支持群聊的 agent**——部门只是组织单位,跨部门流程
  中途招人(走到测试环节才拉 QA)直接成立。招募池含子 agent(挂在 agent 汇报线下的)——
  与建群弹窗/CreateGroup 同口径;是否将子 agent 排除出群聊为开放问题,留后续产品决策。
  tool description 引导「优先同部门,跨部门
  说明理由(`reason` 参数已有)」——注:roster 不携带部门信息,模型实际无法区分同/跨部门,
  该提示为软引导(或后续在 roster 行加部门);8 人上限、能力门控、system 消息可见性不变。(随 PR1。)
  动态招募是组队的主路径:建群只需拉主持人 + 起步成员,缺谁让主持人按流程招。

### 7.1 Agent 自主拉起团队(`group_create`)

动机:大任务先到了单聊里的某个 agent(如部门负责人),由它判断「需要团队」并自己建群组队,
而不是用户手工组队。

- 新 MCP tool **`group_create(title, memberNames[], brief)`**,注入**普通单聊**轮。
  注入条件:该 agent 的 backend 声明 `CapMCPTools`;群成员轮(backing session)
  **不注入**,防止群中拉群套娃。
- 鉴权:session 维度 HMAC token(payload `create:<agentID>:<sessionID>`,复用现有 secret
  与验签思路),调用时按 DB 现状校验会话/agent 仍有效。
- **走现有工具审批门,不进 allowedTools**:自主建群会启动多成员消耗,必须用户在聊天里
  批准后才执行(复用现有审批 UX,无新 UI 形态)。
- 行为:`CreateGroup`(**发起者 = 主持人**——需求上下文在它手里,不做 host 移交;
  成员按名字从**全部 active 可用 agent** 解析(与 invite 池同口径,可跨部门),
  逐个过 `backendSupportsGroup` + 8 人上限) → 落 system 消息
  标注「由 <agent> 自会话拉起」 → **`brief` 作为首条群消息投递给主持人**,触发其群内首轮
  (读 SOP → 拆卡 → 派活)。发起 agent 的单聊上下文**不会**自动带进群的 backing session,
  故 tool description 要求 `brief` 完整转述需求与验收标准。
- tool result 返回 `group_id`;前端在单聊 transcript 渲染「已创建群聊 →」跳转卡,
  侧栏出现新群。
- 测试:svc(审批通过建群/成员门控/套娃拒绝/无效成员名报错)+ e2e(fake runtime
  调 `group_create` 走全链路)。

## 8. 前端 UI/UX

设计稿已落 `~/Desktop/agentry.pen`(pencil MCP 维护,非仓库文件):

| 帧 | 内容 |
| ---- | ---- |
| `Group Chat — 任务卡编排 · Light` | 完整故事线:需求 → 派活卡 #1 → 交付卡 #1 → 并行验证卡 #2/#3 → QA 运行中;右侧「任务」tab 激活态 |
| `② 新建群聊弹窗`(原帧更新) | 群标题 + 主持人 + 项目 + **「协作流程」下拉(可选,绑定 workflows)** + 初始成员;文案统一「主持人」(「从部门预选」变体帧已删) |
| `Group 任务卡组件 — Dark` | 任务卡三态 + 任务面板行的暗色验证(全变量驱动) |
| `Workflows — 流程库 · Light` | 流程管理页:列表(名称 + 摘要首行 + 使用中群数 + 更新时间 + 编辑/删除)+ 右侧选中流程正文预览面板(底部「编辑流程」) |
| `⑤ 流程编辑弹窗` | 名称 + Markdown 正文编辑器(label 行带「插入骨架模板」链接)+ 保存;正文区内容即 §6.3 的骨架示例 |

要点:

- **任务卡气泡**(`task_event != ''` 的消息):保留发送者头像/名字行;卡体 = 头部条
  (clipboard-list 图标 + `#N` mono 序号 + 标题 + 状态 pill:进行中=amber/已完成=green/已取消=gray)
  + 体部(brief 或 result + 「指派给/交付给 @成员」chip + 创建者/时间 meta)。
  并行派活时两张紧凑卡并排(`↳ 验证 #1` 回指链接)。@chip 点击跳 assignee backing session(复用现有 mention chip 行为)。
- **状态实时回写**:历史 created 卡的状态 pill 随任务状态翻转;前端 store 维护
  `taskId → status`,由 group event 新增 `task_updated` 事件驱动。
- **任务 tab**:roster 面板 Members/Settings 之外第三个 tab,标题带 open 计数 badge;
  列表「进行中」置顶、「已完成」分组在下;行 = 状态点/勾 + `#N` + 标题 + 副行
  (assignee · 回指 · 时间) + assignee 小头像 + chevron;点行 = transcript 锚定到对应任务卡
  (复用行级 anchor),行尾跳 assignee 会话。
- **群头部不加新元素**(open 计数已在 tab badge 表达)。
- **流程库**:独立管理页(与组织页同级入口);列表选中 → 右侧预览面板展示正文
  (所见即注入,头部标注「修改即时生效」);新建/编辑走弹窗,「插入骨架模板」降低首次
  书写门槛;删除前确认并提示使用中的群数——已删流程的群按「不绑定」处理
  (注入时查不到即跳过,不报错)。
- 全部静态文案走 i18n(zh-CN/en);控件用 shadcn `@/components/ui/*`。

## 9. 测试策略(严格 TDD)

- entity:`Check` / `CanComplete` / `CanCancel` 行为测试。
- repo:sqlmock(Create/Update/FindByGroupAndNo/ListByGroup/NextTaskNo)。
- svc(mockgen 注入 repo mock):建卡即投递、complete 鉴权(非 assignee 拒绝)、
  result 必填、cancel 权限、assignee 离群级联取消、关单后操作拒绝。
- scheduler:task 消息走 FIFO/kick 与普通消息一致(现有测试模式)。
- 前端 Vitest:任务卡气泡渲染三态、状态回写、任务 tab 排序/锚定、i18n key 覆盖。
- e2e(fake runtime 已是 group MCP HTTP 客户端):扩展 fake 调 `group_task_create/complete`;
  spec 走「用户@主持人 → 建任务给成员 → 成员 complete → 主持人收到」全链路,
  `node:sqlite` oracle 验 `group_tasks` 行。注意 gateway 端口须 `AGENTRE_PROXY_PORT=0`。

## 10. 分期(5 个 PR)

1. **后端任务域**:migration(`group_tasks` + `group_messages` 两列 + `workflows` 表 +
   `groups.workflow_id`)+ entity/repo/svc + 3 个 task MCP tool + 消息联动 + 提示强化
   (含流程注入、`.agentre/handoff` 约定)+ `group_invite` 池放宽(后端测试齐)。
2. **前端**:任务卡气泡 + 任务 tab + `task_updated` 事件 + i18n。
3. **流程库**(CRUD 管理页 + 建群弹窗「协作流程」下拉)。
4. **e2e seam + spec**。
5. **agent 拉起团队**:`group_create` tool + 单聊注入 + 审批门 + 跳转卡
   (只依赖现有群域,可与 1–4 并行)。

## 11. Out of scope(本期明确不做)

硬验收门(未验收不能汇报)、worktree/分支隔离、任务依赖图/DAG、用户直接建任务卡
(用户走 @主持人)、跨群任务、任务卡编辑(打回=新卡)、`group_task_list` tool、
主持人(host)移交、`.agentre/` 的应用层自动管理(纯提示词约定)、
从部门一键建群入口(已砍,部门仅作组织结构)、
流程 AI 生成草稿(后续再做;本期靠「插入骨架模板」+手写)。

## 12. 关键不变量

- 任务事件即消息:不引入第二条投递管线;task 消息的 seq/round/FIFO 语义与普通消息完全一致。
- `task_no` 在 per-group 锁内分配,与 seq 同纪律,不重号。
- complete 仅 assignee、cancel 仅 creator/主持人,按 DB 现状实时鉴权(与 `memberCanPost` 同思路)。
- 软门不挡路:任何任务状态都不阻塞 `group_send`/轮次调度——LLM 永远有逃生通道,卡死只能由用户 Stop。
