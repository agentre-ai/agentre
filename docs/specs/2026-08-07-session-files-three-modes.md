# 会话文件面板三模式：对话变动 / 完整目录树 / Git 变动

> Status: Approved
> Owner: chat experience（前端 + desktop 后端 + agentred daemon）
> Last updated: 2026-08-07

**Objective:** 让会话右侧「文件」面板能在三种视角间切换——本次对话改过的文件、工作目录的完整文件树、当前 git 变动——并且本地会话与远端（agentred）会话行为一致。

**Hard invariants:**

1. **「变动」模式的现有行为零回归。** 它仍然只由前端 `deriveFiles(messages)` 派生、不发起任何后端调用，行点击仍跳转到 `lastTurn` 对应轮次的 user 消息，本地会话仍渲染「打开文件」按钮、远端仍不渲染（`docs/specs/2026-08-06-session-files-tree.md` 的硬不变量 1、2 继续成立）。
2. **本面板绝不修改工作区或 git 状态。** 只执行只读命令；特别地，不得为了给未跟踪文件算行数而执行 `git add -N` 或任何写索引的命令。
3. **目录树不得越出会话工作目录。** 请求路径是相对 cwd 的相对路径，解析后必须仍在 cwd 之内，否则拒绝。
4. **新增可见 UI 文案全部走 i18n**，同时覆盖 `zh-CN` 与 `en`，由 `frontend/src/__tests__/i18n.test.ts` 校验。
5. **不改动既有 `remotefs.*` RPC 的请求/响应形状**，避免新旧 daemon 之间出现静默降级。

## Problem

1. **「文件」面板只能看到对话碰过的文件。** `frontend/src/components/agentre/chat-context-sidebar/derive.ts:131-178` 的 `deriveFiles()` 只扫消息里的编辑/读取工具调用与 `block.canonical`；工作目录里对话没碰过的文件、以及 agent 在会话之外造成的改动，面板里完全不存在。用户要定位一个文件时只能离开应用去文件管理器找。
2. **没有任何工作目录浏览能力。** 桌面端只有远端目录列举 `RemoteFsListDir(deviceID, path)`（`internal/app/remote_fs.go:10`），本机没有对应绑定；面板拿到了 `cwd`（`frontend/src/components/agentre/chat-panel.tsx:2850`）却只用来拼「打开文件」的路径。
3. **git 变动信息只有一个计数，且前端根本没用。** `internal/service/chat_svc/git_state.go:27-65` 的 `runGitState` 产出分支名、dirty 计数、ahead/behind，但没有按文件的变动清单；`GetSessionGitState` 在 `frontend/src` 里零调用（仅 `frontend/src/components/agentre/__tests__/chat-panel.test.tsx:57` 挂了个 mock）。用户看不到「这轮 agent 到底改了哪些文件、各改了多少行」。
4. **远端会话在 git 维度是空白。** `internal/service/chat_svc/git_state.go:122-124` 对 `be.IsRemote()` 直接返回 `notARepo`，daemon 侧没有任何 git RPC（`internal/daemon/daemon.go:578-587` 只注册了 `remotefs.listDir` / `remotefs.mkdir`）。绑了 agentred 的会话拿不到远端工作目录的 git 状态。

## Actors and user stories

1. 作为使用中的用户，我希望在同一个面板里切到「目录」，浏览会话工作目录的完整文件树，以便找到对话没提到过的文件。
2. 作为使用中的用户，我希望切到「Git」看到当前工作区的未提交改动及每个文件的 +/− 行数，以便在提交前核对 agent 到底动了什么。
3. 作为在特性分支上工作的用户，我希望把 Git 模式切到「本分支」，看到相对某个基线分支的总变动，并且在基线猜错时能自己改。
4. 作为远端（agentred）会话的用户，我希望目录树与 git 变动与本地会话表现一致，而不是看到一句「不支持」。
5. 作为大仓库用户，我希望目录树不因为 `node_modules` 这类被忽略的目录而卡住或被淹没，同时在需要时仍能看到它们。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 顶层 tab 保持「大纲 / 文件」两个不变，三种模式做成「文件」页内的分段控件；模式选择持久化 | mockup 方案 A/B 对比（`.dev-kit/artifacts/2026-08-07-session-files-three-modes/mockups/`，本地产物、不在 Git 里）：240px 下四个顶层 tab 必须砍掉计数角标，且 Git 子档与基线仍要另占一行，并没省掉 chrome。语义上三种模式是同一批文件的三种视角，与「大纲」不同级。拒绝：顶层四 tab |
| 2 | 后端三个方法的入参是 `sessionID`（加相对路径 / scope / baseRef），不是 `cwd` 路径 | 会话的工作目录解析已经是后端职责且本地/远端规则不同（`internal/service/chat_svc/cwd.go:40-65`：本地走 `AgentCwd`，远端查 `project_location_repo`）。让前端传路径会把这套规则复制一份到前端，并让路径成为可被伪造的入参。拒绝：前端传 `cwd` |
| 3 | `workspace_fs_svc` 自己声明窄接口 `SessionWorkspaceResolver`（`sessionID → {deviceID, cwd}`），由 `chat_svc` 实现、在 composition root 用 `RegisterSessionWorkspaceResolver` 注入 | DIP + ISP，且与既有 `chat_svc.RegisterCwdResolver(project_svc.Default().ResolveSessionCwd)`（`internal/bootstrap/cago.go:127`）是同一种函数值注入模式；服务单测因此可以只用 mock、不碰 DB。拒绝：`workspace_fs_svc` 直接调 `chat_repo` / `agent_backend_repo`——跨域读别人的表，且逼出 DB 测试 |
| 4 | fs 与 git 的核心逻辑放叶子包 `internal/pkg/workspacefs`，host 的本机分支与 daemon handler 共用同一份实现 | 两端行为一致必须靠共享代码保证，不能靠两边各写一遍再靠测试对齐；`internal/pkg` 是既有的跨切面叶子层（不反向 import service/repository）。拒绝：daemon 与 host 各自实现 |
| 5 | daemon 侧新开 `workspacefs.*` 方法族（`listDir` / `gitChanges` / `gitBranches`），不给 `remotefs.*` 加字段 | 版本偏斜必须可见：给 `remotefs.listDir` 加 `respectGitignore` 字段的话，旧 daemon 会静默忽略它并返回一棵含 `node_modules` 的树，桌面端无从察觉；新方法族在旧 daemon 上直接回 `-32601`（`internal/daemon/rpc/registry.go:53`），可以明确提示升级。拒绝：扩展现有方法 |
| 6 | 目录树按目录懒加载，每次只列一层 | 大仓一次性递归会遍历几十万文件、卡住 UI 且吃内存；懒加载对任意仓库规模都是常数代价。拒绝：一次性递归 + 硬编码黑名单（黑名单永远盖不全 `.venv`/`vendor`/构建产物）；拒绝：只列 `git ls-files`（非 git 目录不可用，且看不到 agent 刚创建的未跟踪文件——恰恰是最想看的） |
| 7 | 忽略判定用 `git check-ignore --stdin -z` 喂当层条目，非 git 目录不判定 | 自动尊重嵌套 `.gitignore`、`.git/info/exclude` 与全局 excludes，语义与用户的 git 完全一致；`go.mod` 中没有 gitignore 库，引入新依赖仅为此不划算。拒绝：第三方 gitignore 库；拒绝：硬编码黑名单 |
| 8 | Git 模式内含「未提交 / 本分支」两档 | 用户决定（2026-08-07）。「未提交」回答「现在还有什么没提交」，「本分支」回答「这个分支相对基线一共改了什么」，两者都常用且不能互相替代 |
| 9 | 「本分支」的基线默认取 `origin/HEAD` → 回退 `main` → `master`，且在上下文条常驻显示、可点开改，选择按会话持久化 | 用户决定（2026-08-07）。本仓库有基线猜错的前例（`.claude/settings.local.json` 里 worktree 的 `baseRef` 因 `origin/main` 猜错而改成 `head`），所以必须既有默认又可改，且当前基线必须一直看得见。拒绝：只自动推断不可改；拒绝：取 `@{u}`——对长期特性分支只会显示未推送的少数提交，不是分支的全部工作量 |
| 10 | 未跟踪文件的新增行数由 Go 侧读文件计行得出（>1 MiB 或含 NUL 字节判为二进制，不显示角标） | `--numstat` 覆盖不到未跟踪文件；唯一能让 git 覆盖它们的办法是 `git add -N`，那会写用户的索引，违反硬不变量 2。拒绝：`git add -N`；拒绝：未跟踪文件一律不显示行数——agent 新建的文件是最需要看行数的 |
| 11 | Git 模式的文件行是扁平列表（basename 主显 + 右侧灰色目录后缀，从头截断），另两个模式仍是目录树 | mockup G1/G2 对比：git 变动天然跨目录且路径深，树形下 `internal/service/chat_svc/turn.go` 一个文件就吃掉 4 行，而单子目录链压缩已列入不做。扁平形态下文件名永不被截断，与 VS Code 源代码面板默认形态一致。拒绝：三模式统一树形 |
| 12 | 目录展开状态与已加载数据仅存组件内、切换会话时重置；「显示忽略项」开关与模式选择全局持久化 | 沿用上一轮 spec 的决策 1（展开状态不持久化）；开关与模式是用户偏好而非会话数据，持久化到既有的 `chat-sidebar-store` persist 里代价为零 |
| 13 | 目录与 Git 模式的数据是快照，在「该模式可见且无缓存」与「当前会话轮次结束」两个时机自动重新拉取，无手动刷新按钮 | 轮次结束是「文件可能变了」的唯一强信号，自动重拉覆盖了绝大多数需求；额外的刷新按钮会在 240px 上下文条里与基线 chip 争位置。拒绝：手动刷新按钮；拒绝：定时轮询——远端会话下等于持续占用 daemon 租借 |

## 模式与入口

「文件」页顶部新增一条分段控件，三段依次为「变动 / 目录 / Git」，任一时刻恰有一段选中，选中段以强调背景 + 加粗区分，`role="tablist"` / `aria-selected` 表达状态，可用 Tab 聚焦且焦点环可见。当前选中的模式写入既有的 sidebar store 并随其持久化；持久化值非法或缺失时回落到「变动」。

分段控件上的计数角标：「变动」恒显示 `deriveFiles()` 的文件数（零成本）；「Git」在其数据已加载时显示变动文件数，未加载或加载中不显示；「目录」不显示计数（树是按需展开的，总数没有意义）。

分段控件下方是一条**随模式变化的上下文条**，仅在需要时出现：「变动」模式无上下文条；「目录」模式的上下文条右侧是「显示忽略项」开关；「Git」模式的上下文条左侧是「未提交 / 本分支」两档切换，选中「本分支」时右侧再出现基线分支按钮。侧栏被拖窄到 190px 时分段控件与上下文条均不换行，基线分支名以省略号截断（见 mockup B5）。

「变动」模式的内容区与现状完全一致，不做任何改动。

## 服务边界与会话解析

新增服务 `workspace_fs_svc`，对外三个方法，第一个入参一律是 `sessionID`：

- **列目录**：入参 `sessionID`、相对 cwd 的相对路径（空串表示 cwd 本身）、是否包含被忽略项；返回解析后的绝对路径、条目列表（名称、是否目录、大小、修改时间、是否符号链接、是否被 git 忽略）、以及是否因超过单层上限而截断。
- **取 git 变动**：入参 `sessionID`、范围（未提交 / 本分支）、基线 ref（可空，表示用默认推断）；返回是否为 git 仓库、实际使用的基线 ref、变动文件列表（仓库相对路径、状态、新增行数、删除行数、是否二进制）、是否截断。
- **取分支清单**：入参 `sessionID`；返回是否为 git 仓库、当前分支、推断出的默认基线、可选分支列表（本地分支 + 远程跟踪分支）。

服务用自己声明的窄接口 `SessionWorkspaceResolver` 把 `sessionID` 解析成 `{deviceID, cwd}`；实现方是 `chat_svc`（它已持有 `resolveSessionCwd` 与 backend 查询），在 composition root 注入。`deviceID` 为空走本机分支，直接调 `internal/pkg/workspacefs`；非空走远端分支，借 `remote_device_svc` 的租借调用 `workspacefs.*` RPC。前端不感知本地/远端差异，也不参与工作目录解析。

Wails 绑定新增一个按域划分的文件，三个方法均只做 parse → `workspace_fs_svc.Default().Xxx` → return，不含业务逻辑。

**路径约束**：列目录的相对路径经解析后必须仍在 cwd 之内；含 `..` 越界、绝对路径、或解析后逃出 cwd 的请求一律拒绝并返回「路径被拒绝」类错误，错误文案不回显被拒的具体原因，避免成为路径探测信道（与 `internal/pkg/remotefs/pathguard` 既有的取舍一致）。

## 目录模式

树根是会话工作目录本身，初次进入时列出根下一层。点击目录行展开/收起；首次展开时才请求该目录的内容，展开期间该行的 chevron 位置显示加载指示器（mockup B2）。已加载的层与展开集合缓存在组件内，切换会话时清空。

`.git` 目录恒不显示，且不可展开。被 git 忽略的条目默认不显示；打开「显示忽略项」后它们以半透明呈现（mockup B2b），开关状态全局持久化。目录不是 git 仓库时不做忽略判定，全部条目正常显示，开关仍可点但不改变结果。

单层条目上限沿用远端目录列举既有的 2000（`internal/daemon/remotefs/handler.go` 的 `defaultMaxEntries`），超出时截断并在该层末尾显示一行说明，说明里带上实际上限数字——不做静默截断。

目录行按「目录在前、各自名称字母序」排列。文件行显示文件名与「打开文件」按钮，按钮的显示条件与「变动」模式一致：仅本地会话且 cwd 非空时渲染。文件行不显示 diff 角标（目录模式不承载改动信息）。

## Git 模式

**未提交档**：以 `git status --porcelain=v1 -z` 取得路径与状态（修改 / 新增 / 删除 / 重命名 / 未跟踪五类，重命名显示新路径），以 `git diff --numstat -z HEAD` 取得已跟踪文件的 +/− 行数。被忽略的文件不出现在此列表。

**本分支档**：先求基线与 HEAD 的 merge-base，再以工作区对 merge-base 做比较取得状态与 +/− 行数——即「相对基线的总变动」，已提交与未提交的改动一并计入。未跟踪文件同样计入。

两档共用的规则：未跟踪文件的新增行数由读取文件内容计行得出，文件超过 1 MiB 或含 NUL 字节时判为二进制，只列出该文件、不显示角标；`+N` 用绿色、`−N` 用红色，与「变动」模式的 diff 角标同色；两者皆为 0 时不显示角标。变动文件数超过上限时截断并显示说明。

**基线的确定**：默认依次尝试 `origin/HEAD` 指向的分支、`main`、`master`，每一步都验证该 ref 确实存在，取第一个命中者。用户在基线按钮里选择的值按会话持久化，持久化值优先于默认推断；若持久化的 ref 已不存在则回落到默认推断，并把持久化值清掉。三者都不可得时基线为空，「本分支」档显示「推断不出默认分支」空态并引导用户点基线按钮选一个（mockup C5）。当前生效的基线名在上下文条上常驻可见，无需展开即可确认算的是什么。

**行形态**：扁平列表，每行依次是状态字母（五类各有稳定配色与可读的无障碍标签）、文件名（basename，永不截断）、灰色的目录后缀（从头截断，根目录下的文件此处为空）、diff 角标（mockup G2）。点击行的行为与「变动」模式一致：本地会话用系统默认应用打开该文件，远端会话不提供打开。

## 远端会话与版本兼容

远端会话的两个新模式与本地走同一套前端代码与同一份核心实现，差别只在服务层选择了 RPC 传输。daemon 侧新增的三个方法与既有 `remotefs.*` 一样要求已鉴权。

远端设备离线、租借失败或调用超时时，内容区显示错误态与「重试」按钮（mockup C4），重试重新发起当前模式的请求，不影响另外两个模式已缓存的数据。

远端 daemon 版本过旧（不认识新方法族，返回方法不存在）时，显示一条明确的「远端 agentred 版本过旧，请升级后再用这个视图」提示，而不是空列表或通用错误——这正是决策 5 选择新方法族而非扩展旧方法的原因。

## 失败与恢复

会话没有工作目录（cwd 为空、或远端会话在该设备上没有配置项目路径）时，「目录」与「Git」两个模式显示对应空态，「变动」模式不受影响。

工作目录不是 git 仓库时，「Git」模式显示「这个目录不是 git 仓库」，并提示可切到「变动」看本次对话改过的文件（mockup C2）；「目录」模式正常工作，只是不做忽略判定。

git 仓库干净、当前档没有任何变动时显示对应的空态文案，两档文案不同（未提交档说工作区干净，本分支档说相对某基线没有改动，mockup C3）。

单条 git 子命令失败时，对应字段留空而不是让整个请求失败——沿用 `internal/service/chat_svc/git_state.go:23-26` 已确立的容错约定；唯一的例外是「是否在工作树内」的判定失败，它直接意味着不是 git 仓库。

单个目录读取失败（权限不足等）时，只让该目录节点显示一行错误提示，树的其余部分照常可用。

所有 fs 与 git 调用都受上下文超时约束；远端调用沿用租借既有的超时设置。

## 无障碍与文案

分段控件与两档切换用 tablist/tab 语义并表达选中状态；「显示忽略项」开关表达按下状态；基线按钮用完整的「对比基线：xxx」作为无障碍标签（可见文本只有 ref 名）；git 状态字母对读屏提供文字标签（已修改 / 新增 / 已删除 / 已重命名 / 未跟踪），视觉上的字母本身对读屏隐藏；加载中、截断、空态、错误态均有文本表达，不依赖颜色单独传达信息。

新增可见文案覆盖：三个模式名与其分组标签、显示/隐藏忽略项、单层截断说明、目录为空、无工作目录、目录读取失败、两档名称与其分组标签、基线标签与「选择基线」、非 git 仓库及其引导、两档各自的干净空态、推断不出基线及其引导、五类 git 状态的无障碍标签、远端离线、远端版本过旧、重试。全部同时写入 `zh-CN` 与 `en`。

## Out of scope

- **把 `chat_svc.GetSessionGitState` 迁进新包，或给它补远端实现。** 它至今在前端零调用，是独立于本轮的问题；本轮不动它，也不删它。
- **单子目录链压缩**（把 `internal/service/chat_svc` 折成一行显示）。它同时影响「变动」模式的现有渲染，超出本轮范围。
- **文件内容 diff 预览 / 行级 diff 视图。** 本轮只到文件粒度的 +/− 行数。
- **远端文件的下载与打开。** 「打开文件」按钮仍只在本地会话渲染，与上一轮 spec 一致。
- **手动刷新按钮与定时轮询**（见决策 13）。
- **在面板内执行任何 git 写操作**（暂存、提交、丢弃、切分支）。
- **「变动」模式自身的任何改动**，包括其排序、路径提取与树形渲染。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `internal/pkg/workspacefs` 的列目录（真实临时目录 + 真实临时 git 仓库） | 单层列举、`.git` 恒隐藏、忽略项标记与嵌套 `.gitignore`、非 git 目录不判定、超上限截断、相对路径越界被拒 | `internal/daemon/remotefs/handler_listdir_test.go:21-45`（真 `t.TempDir()`）+ `internal/service/chat_svc/git_state_test.go:16-24`（`runGit` helper 建真仓库） |
| `internal/pkg/workspacefs` 的 git 变动（真实临时 git 仓库） | 未提交档的五类状态与 +/− 行数、本分支档的 merge-base 语义（已提交+未提交一并计入）、未跟踪文件计行、二进制与超大文件判定、非 git 仓库、基线默认推断的三级回退与 ref 存在性校验、分支清单 | 同上 `git_state_test.go` |
| `workspace_fs_svc`（mock 注入，不连 DB） | `deviceID` 空/非空的路由分叉、会话解析失败与 cwd 为空的降级、远端错误码到 i18n 错误的翻译、方法不存在被识别为版本过旧 | `internal/service/remote_fs_svc/svc_test.go`（mock `remote_device_svc`） |
| daemon `workspacefs` handler（真实临时目录/仓库） | 三个方法的请求解析、sentinel 到 JSON-RPC 错误的映射 | `internal/daemon/remotefs/handler_listdir_test.go` |
| `workspacefs` wire 的错误往返 | sentinel ↔ JSON-RPC code 双向翻译不丢失 | `internal/pkg/remotefs/wire/wire.go:49-89` 的既有翻译对 |
| daemon 集成测试 | `workspacefs.*` 未鉴权被拒、鉴权后端到端一跳可用 | `internal/daemon/integration_test.go:402-404`（`remotefs.listDir` 的鉴权断言） |
| 前端 git 变动行模型的纯函数 | 变动文件列表 → 行模型（basename / 目录后缀 / 状态 / 角标）的拆分与排序 | `chat-context-sidebar/__tests__/derive.test.ts` |
| `FilesView` 三模式渲染 | 模式切换、上下文条按模式出现/消失、目录懒加载与展开收起、忽略项默认隐藏与开关生效、Git 两档切换与基线选择、五类状态与角标、各空态/加载态/错误态、远端版本过旧提示、「变动」模式行为不回归 | `chat-context-sidebar/__tests__/files-view.test.tsx` 现有用例 |
| sidebar store | 模式与忽略开关的持久化、非法持久化值回落、基线选择按会话存取 | `frontend/src/stores/chat-sidebar-store.ts` 现有 `VALID_TABS` 回落逻辑 |
| i18n key 覆盖 | 新增 key 的 `zh-CN` / `en` 双语覆盖 | `frontend/src/__tests__/i18n.test.ts` |

**不可自动化的部分**：绑定真实 agentred 设备的远端会话下，目录树懒加载、忽略项过滤与 git 两档的实际表现，以及旧版 daemon 的版本过旧提示，都需要真机手动验证。按 `docs/verification.md` 在 `e2e/scratch/2026-08-07-session-files-three-modes/` 下先建 `report.md` 再执行，留下每个场景的证据。`chat-panel` 到 sidebar 的接线（会话切换时清空缓存）为大型集成组件，由运行时观察覆盖。

## Links

- 前置：`docs/specs/2026-08-06-session-files-tree.md`（本面板上一轮：目录树 + 打开文件按钮 + diff 角标）
- 布局取证（本地产物，不在 Git 里）：`.dev-kit/artifacts/2026-08-07-session-files-three-modes/mockups/`

## Open questions

无。决策 1（方案 B 入口层级）、8（两档）、9（基线可选）、11（Git 行扁平）、以及远端全支持与不拆轮次，均已获用户确认（2026-08-07）。
