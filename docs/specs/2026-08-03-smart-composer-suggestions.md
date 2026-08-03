# 聊天输入智能候选与本地命令历史

> Status: Approved
> Owner: Frontend
> Last updated: 2026-08-03
> Amendment approved: 2026-08-03 — 使用窄化 Wails 合约解析并返回真实命令执行作用域

**Objective:** 用户在聊天输入框通过 `/`、`$` 或 `@` 打开目录候选时，可以用名称中段、中文拼音、拼音首字母或字符顺序模糊查询找到目标；进入 `!` 本地命令模式时，可以在当前执行设备与项目工作目录下检索并补全持久化的 Shell 命令历史。

**Hard invariant:** 本轮只扩展候选的匹配、排序与 `!` 历史补全，并为准确隔离历史增加一个只读执行作用域解析调用、扩展既有 `TerminalRunCommand` 返回值；不得新增数据库、迁移、后端历史存储或额外命令执行。`/`、`$`、`@` 的触发边界和提交语义不得回退，历史候选不得自动执行 Shell 命令，`!` 命令与输出仍不得发送给 AI，历史持久化或作用域预读取失败不得阻止命令执行，现有本地命令输出卡片仍只保留在当前应用运行期。

## Problem

1. **`/` 与 `$` 候选只能做名称前缀匹配。** `frontend/src/components/agentre/slash-commands/registry.ts:156-167` 明确使用 `startsWith`，对应测试 `frontend/src/components/agentre/slash-commands/__tests__/registry.test.ts:66-78` 也把前缀匹配固化为现有行为。因此用户记得命令中段、插件限定名的一部分或近似字符顺序时，候选会直接消失。用户于 2026-08-03 明确要求这些提示不再局限于前缀匹配。
2. **`@` 候选虽然支持名称包含，但没有相关度排序，也不能按中文拼音或项目路径检索。** `frontend/src/components/agentre/chat-input/mentions/use-mention-menu.ts:21-25` 只对 `label` 做 `includes`；同文件 `:50-57` 固定按 Agents 后 Projects 的源顺序过滤，没有对命中质量排序。候选较多时，精确或更相关的结果不会主动前置。
3. **项目已有智能评分能力，但聊天输入候选没有复用。** `frontend/src/components/agentre/command-palette/score.ts:7-72` 已定义精确、前缀、包含、全拼、拼音首字母、字符顺序模糊和副标题匹配阶梯；当前 `/`、`$`、`@` 各自维护更弱的过滤逻辑，造成同一应用内搜索体验不一致。
4. **`!` 命令模式只能执行当前输入，不能提示过去运行过的 Shell 命令。** `frontend/src/components/agentre/chat-input/index.tsx:276-305` 只检测命令模式并把非空命令交给执行回调；`frontend/src/stores/local-commands-store.ts:35-84` 只在内存中保存当前运行期的命令卡片与输出，没有持久化或候选读取能力。应用重启后，用户无法从 Agentre 找回常用的项目命令。用户于 2026-08-03 明确把“历史命令”限定为 `!` 本地 Shell 命令，并要求跨应用重启持久化。
5. **前端现有会话快照不能可靠代表 Shell 实际执行作用域。** `internal/app/terminal.go` 会在每次命令提交时重新从会话、Agent、Backend 与项目位置解析执行设备和 cwd，而既有 `TerminalRunCommand` 的前端合约只返回 `void`。新建远端项目会话、本地自由会话以及项目位置更新后的已加载会话，都可能没有可用或持有过期的前端 cwd；等待会话详情再启动命令又会改变既有执行时序并在并发提交时丢命令。用户于 2026-08-03 批准增加窄化的执行作用域合约，以保证历史隔离与实际执行目标一致。

## Actors and user stories

1. As an Agentre user, I want to find a command or Skill without remembering its exact prefix, so that I can complete `/` and `$` inputs from partial recollection.
2. As an Agentre user with Chinese-named agents or projects, I want to search `@` candidates by full pinyin or initials, so that switching input methods is unnecessary.
3. As a developer working in a project, I want `!` mode to recall commands previously run in the same execution directory, so that repeated build, test and maintenance commands require less retyping.
4. As a keyboard user, I want the best-ranked candidate highlighted without immediately executing it, so that Enter and Tab remain predictable and safe.
5. As a privacy-conscious user, I want persisted Shell history isolated by execution target and clearable for the current directory, so that commands from unrelated projects or machines do not mix.

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | `/`、`$`、`@` 和 `!` 历史查询共用一套分级智能评分。 | 项目已有经过测试的 Command Palette 评分语义，复用可保持一致并避免算法分叉。Rejected: 只把 `startsWith` 改成 `includes` — 不能解决拼音、近似字符顺序和相关度排序。 |
| 2 | 主字段按精确、前缀、包含、全拼、拼音首字母、字符顺序模糊依次降权；副字段只做更低优先级的大小写不敏感包含匹配。 | 主字段必须主导结果，描述和路径只负责“能找到”，不能压过名称直接命中。Rejected: 对描述和路径应用与名称相同的模糊评分 — 容易把名称无关的候选推到前面。 |
| 3 | `/`、`$` 全局按分数排序；`@` 保留 Agents / Projects 分组，并只在组内排序。 | Slash/Skill 菜单没有分组约束；Mention 菜单的类型分组提供必要上下文。Rejected: `@` 全局混排 — 会破坏现有分组阅读结构并可能重复出现分组标题。 |
| 4 | 同分目录候选保持源顺序；空查询保持完整原始顺序。 | 稳定顺序避免输入过程中候选抖动，也保留用户打开目录菜单时熟悉的初始列表。Rejected: 同分时再按字母排序 — 会无请求地改变当前目录顺序。 |
| 5 | `!` 历史只记录用户提交执行的完整命令文本，按大小写敏感的完整文本去重；每个作用域最多保留 100 条。 | 历史必须能原样补回输入框；限制和去重控制本地存储增长。Rejected: 保存每次执行的重复时间线 — 增加噪声且更快耗尽存储。 |
| 6 | `!` 历史按“稳定执行设备身份 + 当前解析出的工作目录”隔离；空 cwd 使用该设备独立的默认作用域。 | 同一项目不同会话应共享历史，本地与远端或不同目录不得串历史。Rejected: 应用全局历史 — 会混入无关命令、绝对路径和机器专属操作；只按会话隔离 — 无法跨同项目会话复用。 |
| 7 | `!` 非空查询先按智能匹配分排序，同分时最近使用优先；空查询直接按最近使用时间倒序。 | 查询意图应优先于使用历史，而打开空菜单时最近命令最有价值。Rejected: 频率压过匹配分 — 常用但不相关的命令可能挡在准确命中之前。 |
| 8 | 选择 `!` 历史只替换命令正文，不自动执行；Enter 或 Tab 首次只确认候选，用户再次提交才执行。 | Shell 历史包含任意用户授权命令，自动运行会造成不可接受的误操作风险。Rejected: Enter 直接运行高亮历史 — 与现有候选补全语义不一致且危险。 |
| 9 | Shell 历史使用新的版本化本机 `localStorage` 数据，不复用本地命令输出 Store，也不进入数据库。 | 历史是轻量的设备本地输入辅助；输出 Store 含大量流式数据且按既有设计只活到应用退出。Rejected: 持久化整张命令卡片或输出 — 扩大敏感数据和存储体积；新增数据库表 — 对设备本地提示过重。 |
| 10 | 完整命令不做自动敏感词识别或遮盖，并提供“清空当前目录历史”。 | 自动识别 token/password 不可靠，截断后的命令也无法正确复用；用户必须有明确删除入口。Rejected: 静默过滤疑似敏感片段 — 既可能漏掉秘密，也可能破坏普通命令。 |
| 11 | 规范化查询发生变化时，默认高亮回到排名第一的候选。 | 智能排序后首项代表当前最佳结果，继续保留旧下标可能让 Enter/Tab 选中较弱结果。Rejected: 只在下标越界时收缩 — 无法保证默认选择最佳匹配。 |
| 12 | 评分能力作为前端共享纯逻辑，由 Command Palette 与聊天输入候选共同消费，不引入新依赖。 | `pinyin-pro` 已在项目中使用，共享纯函数是最小的一致性边界。Rejected: 从聊天输入模块反向依赖 Command Palette 组件目录或复制算法 — 前者耦合不相干 UI 域，后者会再次漂移。 |
| 13 | 新增只读 `ResolveLocalCommandScope` Wails 合约，为既有 session 或尚未创建 session 的 Agent/项目目标解析当前 `{deviceId, cwd}`，且绝不创建会话。 | 新远端项目、本地自由聊天和位置变更后的会话都不能安全依赖前端快照。Rejected: 使用本地项目路径或旧 session cwd 猜测 — 会把历史写入错误机器或目录；为打开历史菜单而提前创建 session — 会改变会话生命周期。 |
| 14 | `TerminalRunCommand` 在后端只解析一次执行目标，使用该目标启动命令，并把同一 `{deviceId, cwd}` 与可选 `startError` 返回前端；每次提交独立调用一次，不等待异步会话详情，也不共享单条 pending 槽。 | 返回真实目标才能让持久化与执行一致；把启动失败放进响应可在不丢失作用域的前提下沿用失败卡片。Rejected: 先启动后等待 `LoadChatSession` — 连续提交会互相覆盖；只使用预解析结果 — 项目位置变化时存在陈旧目标。 |

## Shared candidate matching

查询先去除首尾空白并做大小写不敏感比较。目录候选的空查询给全部可用候选相同的保留分，并按原始顺序展示。非空查询按以下先命中先得的阶梯评分，较高分排在前面：

1. 主字段与查询完全相等；
2. 主字段以查询开头；
3. 主字段任意位置包含查询；
4. 查询只含 ASCII 字母或数字，且命中主字段中文内容的无声调全拼；
5. 查询只含 ASCII 字母或数字，且命中主字段中文内容的拼音首字母；
6. 查询只含 ASCII 字母或数字，且其字符按顺序出现在主字段的规范化拼音或 ASCII 文本中；
7. 副字段任意位置包含查询。

得分为零的候选不展示。评分只决定可见性与顺序，不修改候选自身的名称、标签、描述、命令文本或提交值。除 `!` 历史明确以最近时间处理同分外，其余排序必须稳定；两个候选得分相同时，相对顺序与候选源一致。

## `/`, `$` and `@` directory candidates

当聊天输入框存在对应候选源，且用户在行首或空白字符后输入 `/`、`$` 或 `@` 时，沿用当前触发检测规则打开相应候选。查询仍是触发字符之后、光标之前且不含空白的文本；一旦查询含空白，当前触发结束。词内的触发字符继续作为普通文本，不打开菜单。

对于 `/` 与 `$`，候选必须先按当前触发字符隔离，再参与评分；`/` 查询绝不能显示 `$` Skill，反之亦然。命令或 Skill 的规范名称是主字段，说明文字是副字段。选中后仍执行现有补全流程：删除触发字符与查询片段，将完整命令文本写回草稿，不自动发送。

对于 `@`，Agent 名称与项目名称分别作为各自候选的主字段；项目路径作为项目候选的副字段，Agent 不新增推导出的副字段。Agents 始终构成第一组，Projects 始终构成第二组；每组只保留得分大于零的候选，并在组内按得分稳定排序。某一组没有命中时，该组不渲染；另一组仍正常展示。

## Local command execution scope contract

`ResolveLocalCommandScope` 接受二选一目标：已有 `sessionId`，或尚未创建会话的 `agentId + projectId`。服务层复用命令启动使用的 Agent、Backend、项目位置和 cwd 解析逻辑；预会话路径使用未持久化的 session 值对象参与解析，禁止调用 session 创建。返回只包含稳定 `deviceId` 与解析后的 `cwd`，不包含命令、输出或凭证。

Composer 在执行目标变化和进入 `!` 模式时异步刷新作用域；刷新期间不展示历史，过期响应不得覆盖较新的目标。解析失败只使历史菜单暂时不可用，不能禁止用户提交命令，也不创建错误的本地路径或默认作用域猜测。

用户提交非空命令时，每次提交拥有独立异步流程：需要时先沿用现有 `EnsureChatSession` 得到 session ID，随后立即调用且只调用一次 `TerminalRunCommand`，不得等待无关的 `LoadChatSession` 详情，也不得把多个提交放进单条可覆盖的 pending 槽。后端为该调用解析一次执行目标，用同一目标调用 `OpenCommand`，并返回 `scope: {deviceId, cwd}`；若目标已解析但命令启动失败，则响应同时返回 `startError` 而不是丢弃 scope。前端先按响应 scope 记录历史，再把 `startError` 交给既有本地命令失败卡片流程。

若 RPC 在返回 scope 之前失败，前端可以使用本次提交前最后一次成功预解析的同目标 scope 记录命令；若连合法预解析 scope 也不存在，则不得猜测作用域或串入其它目录，命令错误仍按既有流程展示。无论历史写入是否成功，命令提交与失败展示都继续运行。

## `!` Shell history flow

当普通聊天编辑器按现有规则进入 `!` 命令模式，且当前执行作用域存在历史时，历史菜单在光标附近打开。`!` 后的完整命令正文是查询；与 `/`、`$`、`@` 不同，空格和参数不会结束历史查询，因此用户输入 `!git ch ma` 时菜单仍可继续筛选完整历史命令。没有历史或没有命中时，菜单不渲染，编辑器输入与命令提交保持可用。

非空查询对完整历史命令应用共享评分，匹配分更高者优先，同分时 `lastUsedAt` 更新者优先。Shell 历史的字符顺序模糊匹配额外允许查询中存在 ASCII 空白，并按顺序匹配完整命令，因此 `git ch ma` 可以命中 `git checkout main`；该 Shell 专用选项不得改变 Command Palette 的既有多词查询行为。只有 `!` 或规范化查询为空时，所有当前作用域历史都可见，并直接按最近使用倒序排列。

用户通过鼠标、Enter 或 Tab 选择历史后，编辑器保留开头的 `!`，以保存的完整命令替换 `!` 后全部正文，并把光标移到末尾。选择后菜单关闭，命令不自动执行；用户可以继续修改，也可以再次提交运行。ArrowUp 与 ArrowDown 在菜单打开且有候选时只移动历史高亮，不触发普通聊天消息历史；Escape 只关闭菜单并保留输入。

用户提交非空 `!` 命令时，系统记录提交分支实际交给本地命令执行回调的规范化完整文本，并使用本次 `TerminalRunCommand` 返回的真实执行 scope；RPC 未返回 scope 时只允许回退到该目标最近一次成功预解析的 scope。记录不以 `startError`、退出码或最终状态为条件；命令不存在、启动失败或返回非零退出码仍保留在历史中。仅浏览或选择候选不更新最近时间，真正再次提交该命令才将其移到最近位置。

## Persistent state and data lifecycle

新增一份版本化的本机历史数据。每个作用域由稳定执行设备身份与当前解析出的 cwd 共同确定；本地执行使用固定本机身份，远端执行使用稳定设备身份。cwd 为空时，同一设备的自由会话共享一个独立默认作用域。当前目标、cwd 或执行设备变化后，Composer 重新解析并读取新作用域；解析完成前不展示旧作用域历史，也不混入旧作用域数据。

每个作用域最多保留 100 个大小写敏感的唯一命令。每个历史条目只包含完整 `command` 与 `lastUsedAt`；持久化结构还包含用于隔离的设备/cwd 作用域键和数据版本。再次提交完全相同的命令时只更新时间并移动到最近位置；新增第 101 个唯一命令时淘汰最旧条目。

| Data change | Existing data impact | Rollback and deletion |
|---|---|---|
| 新增版本化 `localStorage` Shell 历史键 | 没有既有键、没有 backfill；首次提交非空 `!` 命令后才产生数据 | 回滚代码后该键成为未使用的本机数据，不影响聊天或命令执行；菜单中的“清空当前目录历史”删除当前作用域条目 |
| 持久化完整 Shell 命令、最近时间与作用域元数据 | 只影响本轮之后用户主动提交的命令；不改写既有聊天消息或命令卡片 | 清理当前作用域是有意且不可恢复的删除；其它作用域不受影响 |

现有本地命令输出 Store 继续维护运行中/完成卡片、输出、退出码和状态，并继续在应用退出后清空。新历史不得持久化输出、退出码、会话 ID、终端 ID、卡片展开状态或运行状态，也不得把历史反向恢复成 transcript 卡片。

历史菜单底部在当前作用域有历史时提供“清空当前目录历史”。触发后只删除当前设备与 cwd 的历史，关闭菜单并保持编辑器内当前命令正文不变；它不停止正在运行的命令，不删除当前运行期命令卡片，也不影响其它设备或目录。

## Selection, UI and accessibility

每当规范化后的查询文本发生变化，候选重新评分后默认高亮第一项。该重置只由查询变化触发；同一查询下使用方向键移动高亮时，不得被编辑器的重复更新事件抢回第一项。候选源异步加载或候选数量收缩时，高亮必须保持在有效范围内。

`/`、`$`、`@` 继续使用现有 ArrowUp、ArrowDown、Enter、Tab、Escape、鼠标悬停和鼠标选择行为。`!` 历史复用同一候选弹层视觉与键鼠高亮模型；历史行以单行命令展示，过长时视觉截断，但完整动态命令必须保留给无障碍名称和选择结果。历史命令本身属于动态用户数据，不进入 i18n；历史菜单的无障碍名称与“清空当前目录历史”属于静态 UI copy，必须同时提供中文和英文资源。

没有候选命中时，各菜单沿用不渲染行为，不新增空状态。除 `!` 新增历史行与清理动作外，本轮不改变候选菜单的尺寸、颜色、动效、列表框角色或分组视觉。最佳候选的高亮继续通过现有选择语义暴露，智能评分不依赖颜色表达额外信息。

## Failure, compatibility, security and privacy

Skill 目录异步加载失败、候选源为空、描述缺失或项目路径缺失时，候选系统继续按现有方式降级为空或只使用已有主字段，不阻断编辑器输入。拼音路径未命中或查询包含不适用于拼音匹配的字符时，继续尝试适用的直接匹配阶梯，最终零分即隐藏，不抛出用户可见错误。

Shell 历史存储不存在或内容损坏时，当前作用域按空历史处理；下一次合法记录可以重建有效数据。浏览器/WebView 拒绝访问 `localStorage`、序列化失败或存储空间不足时，当前运行期仍保留内存中的历史变化，但不承诺跨重启；任何持久化异常都不得阻止命令执行，也不显示打断输入的错误弹窗。

完整 Shell 命令可能含凭证、私有路径或其它敏感参数。本轮按用户确认保存原文，不做不可靠的自动脱敏；数据只保存在 Agentre 本机前端存储，不随聊天消息发送、不进入 AI 上下文。新增后端通信严格限于不携带命令正文的只读作用域解析，以及既有 `TerminalRunCommand` 返回真实 scope/startError；不得新增命令、历史或输出日志。作用域隔离和当前目录清理入口是本轮拥有的隐私边界。

Command Palette 迁移到共享评分逻辑后，其现有评分数值、结果顺序和匹配能力必须保持不变；本轮不借迁移改变 Command Palette 的可见行为。`/`、`$`、`@` 的命令补全、Mention XML、发送和路由协议也保持不变。

## Out of scope

- 不从普通聊天消息、终端输出、Shell 自身 history 文件或现有 transcript 卡片反向导入历史。
- 不持久化本地命令输出卡片、输出、退出码、运行状态或终端进程。
- 不跨设备同步、云备份或共享 Shell 历史。
- 不提供所有目录历史的集中管理页、导入/导出或全局清空入口。
- 不新增触发字符，也不改变 `/`、`$`、`@` 与 `!` 的既有模式触发边界。
- 除只读 scope 解析和既有 `TerminalRunCommand` 的返回结构外，不改变命令执行次数、自动发送、Mention XML、Agent 路由或项目引用语义。
- 不在后端、数据库或设备间存储/同步 Shell 历史；后端只解析并回传执行作用域。
- 不为候选搜索增加服务端索引、历史频率学习或个性化模型。
- 不给描述和项目路径增加拼音或字符顺序模糊匹配。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| 共享评分纯函数 | 精确、前缀、包含、全拼、拼音首字母、字符顺序模糊、副字段、零分与空查询的评分阶梯保持确定 | `frontend/src/components/agentre/command-palette/score.test.ts` |
| `/`、`$` 候选纯逻辑 | 非前缀命中、相关度排序、描述低优先级命中、同分稳定、空查询原序以及触发字符隔离 | `frontend/src/components/agentre/slash-commands/__tests__/registry.test.ts` |
| `@` 候选纯逻辑 | 中文拼音与首字母、项目路径、Agents / Projects 分组内排序、无命中组消失和稳定顺序 | `frontend/src/components/agentre/chat-input/mentions/__tests__/build-sources.test.ts`；当前过滤逻辑没有独立纯函数测试 |
| Shell 历史持久化纯逻辑 | 设备/cwd 作用域隔离、大小写敏感去重、再次提交移到最近、每作用域 100 条淘汰、损坏数据、清空当前作用域和写入失败的内存降级 | 现有本地命令 Store 测试只覆盖运行期卡片状态，没有持久化先例 |
| Shell 历史排序纯逻辑 | 空查询按最近，非空查询匹配分优先且同分按最近，`git ch ma` 一类含空格顺序查询可命中完整命令且不改变 Command Palette 行为 | 共享评分测试；无现成 Shell 历史测试 |
| 聊天输入组件集成 | `/`、`$`、`@` 的非前缀查询；`!` 打开历史、含空格查询、替换全部命令正文、首次 Enter/Tab 不执行、再次提交才执行、方向键优先级及清空后保留正文 | `frontend/src/components/agentre/slash-commands/__tests__/integration.test.tsx`；`frontend/src/components/agentre/chat-input/mentions/__tests__/integration.test.tsx`；`frontend/src/components/agentre/chat-input/__tests__/command-mode.test.tsx` |
| 执行作用域服务单元测试 | 已有 session、预会话本地/远端项目、自由会话、项目位置变化与无效目标都复用同一解析器；预解析绝不创建 session | `internal/service/chat_svc/exec_target_test.go` 及现有 service mock 注入模式 |
| Wails 命令合约测试 | `ResolveLocalCommandScope` 只返回设备/cwd；`TerminalRunCommand` 使用并返回同一 scope，`OpenCommand` 失败进入 `startError` 响应且每次提交只启动一次 | `internal/app/terminal_test.go`；Wails binding 保持 parse → service → return |
| 前端执行编排集成 | 新远端项目和本地自由聊天在首条命令前可读取正确历史；连续提交不覆盖；位置更新不使用旧 session cwd；scope 解析失败不阻止命令；异步拒绝无未处理 Promise | `frontend/src/components/agentre/__tests__/chat-panel.test.tsx`；`frontend/src/components/agentre/chat-input/__tests__/command-mode.test.tsx` |
| 持久化数据形状 | 新键只含版本、作用域、命令和最近时间，不含输出、退出码、会话/终端 ID 或状态；回滚时旧功能不读取该键 | `frontend/src/stores/__tests__/chat-tabs-persistence.test.ts` 提供本地存储损坏与版本化测试先例 |
| Command Palette 既有测试 | 评分逻辑共享化不改变命令面板现有匹配分数与结果 | `frontend/src/components/agentre/command-palette/score.test.ts` 及各 source 测试 |
| i18n 与可访问性组件测试 | 新菜单无障碍名称、清理动作中英文覆盖、长命令完整可访问且视觉可截断 | `frontend/src/__tests__/i18n.test.ts`；`frontend/src/components/agentre/slash-commands/__tests__/slash-popover.test.tsx`；`frontend/src/components/agentre/chat-input/mentions/__tests__/mention-popover.test.tsx` |

该行为可以在纯函数、本地存储适配器、chat_svc/App 单元测试和 TipTap 组件集成边界完整自动化；新增 Wails 合约由 Go 单元测试与生成后的 TypeScript 类型覆盖，不需要真实 GUI IPC、数据库、平台专属 PTY 或像素级布局，因此本轮仍不新增 GUI E2E。持久化验证需要从空存储开始记录、重新构造读取方后确认恢复，再清理当前作用域并确认其它作用域未变；静态类型检查、后端与前端全量测试、生成检查和 lint 覆盖共享模块迁移、类型、i18n 与格式风险。

## Relevant links

- [`../frontend.md`](../frontend.md) — 前端结构、pnpm、i18n 与 lint 约束。
- [`../testing.md`](../testing.md) — 测试边界与 Red → Green → Refactor 规则。
- [`../develop.md`](../develop.md#when-touching-persistent-data) — 新 `localStorage` 数据的持久化变更流程。
- [`../design.md`](../design.md) — 候选菜单保持一致的视觉与可访问性约束。
- [`../superpowers/specs/2026-06-25-bang-command-execution-design.md`](../superpowers/specs/2026-06-25-bang-command-execution-design.md) — 已归档的 `!` 本地命令原始设计快照；其“无独立命令历史、输出与命令均不持久化”决策由本规格仅针对命令历史作废，输出卡片临时语义继续有效。

## Open questions

None.
