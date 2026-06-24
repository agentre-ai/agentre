# Hooks 前端页面重做(Plan 3)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `frontend/src/components/agentre/hooks-page.tsx`(2695 行旧 source/rule/email 连接器 UI)整体替换为**脚本驱动 Hooks 页**,严格按 `.pen` 两帧(`cvWpo` Hooks — 脚本 / `KVqZk` Hooks — 运行日志)与设计 §8.2,只用新 Wails 绑定(LoadHooks/CreateHook/UpdateHook/DeleteHook/ToggleHook/RunHook)。

**Architecture:** 单文件页面(沿用旧文件「页面 + 同文件多个子组件」的组织方式)。左栏 hook 列表(按解释器配色图标 + `<解释器缩写> · cron` + 状态点/「停用」)→ 主面板头部(大图标 + 标题 + kind + 状态 pill + 子行 cron·上次运行·累计事件 + 操作:试运行/启停/⋮)→ 自定义两 tab(脚本 / 运行日志,无 shadcn Tabs 组件)。脚本 tab:**触发**卡(cron + 时区 + 解释器 Select)/ **脚本**卡(深色 Textarea 编辑器)/ **环境变量·密钥**卡(增删行 + secret 脱敏)/ 内联 **RunResult** 卡(试运行后出现:退出码/耗时/不写库 + 摘要 + stdout/stderr/parseError)。运行日志 tab:两栏(事件列表 + 选中事件 payload 详情)。

**Tech Stack:** React 19 + TS,Tailwind v4(tokens 在 `frontend/src/styles/globals.css`:`status-running`/`status-error`/`status-waiting`/`agent-1..16`),shadcn `@/components/ui/*`(Select/Input/Textarea/Switch/Button/Badge/Dialog/DropdownMenu/Tooltip/Alert——**无 Tabs**),react-i18next,vitest + happy-dom。

## Global Constraints

- **不碰后端、不碰其它前端域。** 仅改:`hooks-page.tsx`(重写)、两份 `common.json` 的 `hooks` 块、`App.test.tsx` 的 `mockHooks()` + Hooks 导航测试、新增 `hooks-page.test.tsx`。`index.ts` 的 `export { HooksPage }`(已存在,签名不变)和 `App.tsx:878` 路由(不变)无需改。
- **只用新绑定**:`LoadHooks/CreateHook/UpdateHook/DeleteHook/ToggleHook/RunHook`。删除所有旧 `*HookSource*`/`*HookRule*`/`SyncHookEmailSource`/`TestHookSource`/`RedeliverHookEvent` 调用与相关 UI/文案。
- **所有可见文案走 i18n**(`react-i18next` `t(...)`,zh-CN + en 双份),`i18next/no-literal-string` 把关;`i18n.test.ts` 校 key 覆盖与对称。动态内容(脚本正文、stdout、payload JSON、cron 表达式、env 值)**不进 t()**。
- **shadcn 表单控件**:解释器/时区用 `Select`(禁止原生 `<select>`);name 用 `Input`;脚本正文/值用 `Textarea`;启停可用 `Switch` 或按钮。
- **测试用 wails mock**:渲染本页的测试必须 per-file `vi.mock` runtime + `window.go.app.App` 注桩(见 [[reference_frontend_wails_runtime_test_mock]]:别加全局 alias),跑全量 vitest 不是 focused。
- **worktree**:前端测试需 `node_modules`(`pnpm install`,~7s 走全局 store)+ `wailsjs`(`cp -R <main>/frontend/wailsjs <worktree>/frontend/wailsjs`,或 `make generate` 需 `frontend/dist/index.html` 占位)。见 [[agentre-worktree-build-gotchas]]。

---

## 数据映射(设计帧 → HookItem 字段)

| 设计元素 | 来源字段 |
| --- | --- |
| 列表项图标配色 | 按 `interpreter` 取 `agent-N` + lucide 图标(见下表) |
| 列表项名 | `name` |
| 列表项副行 `SH · */5 * * * *` | `interpAbbrev(interpreter)` + `" · "` + `scheduleExpr` |
| 列表项状态点 | `enabled` 假→「停用」文字;`lastStatus==="failed"`→`status-error` 红点;否则→`status-running` 绿点 |
| 头部 kind `Shell Hook` | `t("hooks.header.kindLabel", { interp: t("hooks.interp."+interpreter) })` |
| 头部子行 | `scheduleExpr · 上次运行 {{relative}} · 累计 {{totalCount}} 事件` |
| 触发卡 cron | `scheduleExpr`(Input) |
| 触发卡 解释器 | `interpreter`(Select,7 选项) |
| 触发卡 时区 | `timezone`(Select,常用时区 + 默认 Asia/Shanghai) |
| 脚本卡 | `command`(深色 Textarea) |
| env 卡行 | `env[]`:`key` / `value`(secret 时显 `••••••••` 占位)/ `secret` tag |
| RunResult | `RunHookResult`:`exitCode/durationMs/timedOut/stdout/stderr/parseError/events/newCount/dupCount/persisted` |
| 运行日志列表 | `LoadHooksResponse.events`(LoadHooks 带 `hookId` 时返回该 hook 的事件) |
| 日志详情 | 选中 `HookEventItem`:`title/dedupeKey/payloadJson/receivedAt` |

**解释器缩写/图标/配色映射**(组件内常量,不进 i18n——是代号不是文案):
```ts
const INTERP_META: Record<string, { abbrev: string; icon: string; color: string }> = {
  bash:       { abbrev: "SH",  icon: "terminal", color: "agent-8" },
  sh:         { abbrev: "SH",  icon: "terminal", color: "agent-8" },
  node:       { abbrev: "JS",  icon: "braces",   color: "agent-1" },
  python:     { abbrev: "PY",  icon: "terminal", color: "agent-4" },
  pwsh:       { abbrev: "PS",  icon: "terminal", color: "agent-3" },
  powershell: { abbrev: "PS",  icon: "terminal", color: "agent-3" },
  cmd:        { abbrev: "CMD", icon: "terminal", color: "agent-15" },
};
const INTERP_OPTIONS = ["bash", "sh", "node", "python", "pwsh", "powershell", "cmd"];
const TZ_OPTIONS = ["Asia/Shanghai", "UTC", "America/New_York", "Europe/London", "Asia/Tokyo"];
```

> 状态点用现有 token 类:`bg-status-running`(绿)/ `bg-status-error`(红);卡片用 `bg-card border-border`;选中项 `bg-primary/5 border-l-2 border-primary`;深色编辑器 `bg-[#121418] text-[#9aa0ab]`(沿用设计帧硬值,代码块非主题色)。若 `bg-status-running` 类不存在,改用任务执行期 `grep --color-status-running globals.css` 确认的实际类名。

---

## i18n key 树(替换 `common.json` 的整个 `hooks` 块,zh-CN 行 1112–1321)

**zh-CN:**
```json
"hooks": {
  "title": "Hooks",
  "loading": "正在加载 Hooks…",
  "list": {
    "search": "搜索 Hook",
    "groupScheduled": "定时任务",
    "empty": "还没有 Hook",
    "emptyHint": "点击右上角 + 新建一个脚本 Hook",
    "addAria": "新建 Hook",
    "disabled": "停用"
  },
  "header": {
    "kindLabel": "{{interp}} Hook",
    "run": "试运行",
    "runNow": "立即运行",
    "enable": "启用",
    "disable": "停用",
    "more": "更多操作",
    "delete": "删除",
    "lastRun": "上次运行 {{time}}",
    "neverRun": "从未运行",
    "totalEvents": "累计 {{count}} 事件"
  },
  "tabs": { "script": "脚本", "runLog": "运行日志" },
  "trigger": {
    "title": "触发",
    "subtitle": "cron 表达式 · 手动「试运行」随时可跑",
    "cronLabel": "cron 表达式",
    "interpreter": "解释器",
    "timezone": "时区"
  },
  "script": {
    "title": "脚本",
    "subtitle": "env 注入 HOOK_STATE / 密钥 · stdout 输出 JSON:{ events, state }",
    "name": "名称",
    "namePlaceholder": "Hook 名称",
    "commandPlaceholder": "# 在此编写脚本,向 stdout 打印 { events, state }",
    "save": "保存",
    "create": "创建"
  },
  "env": {
    "title": "环境变量 / 密钥",
    "subtitle": "注入为脚本的环境变量 · 密钥落库脱敏,UI 显示 ••••",
    "add": "添加变量",
    "keyPlaceholder": "变量名",
    "valuePlaceholder": "值",
    "secret": "密钥",
    "remove": "删除变量",
    "empty": "暂无环境变量"
  },
  "run": {
    "ok": "试运行 · 退出码 {{code}}",
    "failed": "试运行失败 · 退出码 {{code}}",
    "timedOut": "超时",
    "meta": "耗时 {{ms}}ms · {{persist}}",
    "noPersist": "不写库",
    "persisted": "已入库",
    "summary": "解析到 {{events}} 个事件 · 去重后 {{new}} 个新事件将入库 · {{dup}} 个重复",
    "parseError": "解析失败:{{error}}",
    "stdout": "stdout",
    "stderr": "stderr",
    "running": "运行中…",
    "empty": "点「试运行」执行一次,结果会显示在这里"
  },
  "log": {
    "empty": "还没有产出事件",
    "selectPrompt": "选择左侧事件查看 payload",
    "dedupeKey": "去重键",
    "payload": "Payload",
    "receivedAt": "产出于 {{time}}"
  },
  "interp": {
    "bash": "Bash", "sh": "Shell", "node": "Node", "python": "Python",
    "pwsh": "PowerShell", "powershell": "PowerShell", "cmd": "CMD"
  },
  "status": { "ok": "正常", "failed": "失败", "running": "运行中", "disabled": "停用", "idle": "待运行" },
  "flash": {
    "created": "已创建 Hook {{name}}",
    "updated": "已保存 Hook {{name}}",
    "deleted": "已删除 Hook {{name}}",
    "enabled": "已启用",
    "disabled": "已停用",
    "saveFailed": "保存失败:{{error}}",
    "runFailed": "运行失败:{{error}}"
  },
  "del": {
    "title": "删除 Hook",
    "description": "确定删除「{{name}}」?此操作不可撤销。",
    "confirm": "删除",
    "cancel": "取消"
  },
  "create": { "defaultName": "新 Hook" },
  "time": {
    "never": "从未", "secondsAgo": "{{count}} 秒前", "minutesAgo": "{{count}} 分钟前",
    "hoursAgo": "{{count}} 小时前", "daysAgo": "{{count}} 天前"
  }
}
```

**en:**(同结构,英文文案,例如)
```json
"hooks": {
  "title": "Hooks",
  "loading": "Loading Hooks…",
  "list": { "search": "Search Hooks", "groupScheduled": "Scheduled", "empty": "No Hooks yet", "emptyHint": "Click + at the top right to create a script Hook", "addAria": "New Hook", "disabled": "Off" },
  "header": { "kindLabel": "{{interp}} Hook", "run": "Dry run", "runNow": "Run now", "enable": "Enable", "disable": "Disable", "more": "More actions", "delete": "Delete", "lastRun": "Last run {{time}}", "neverRun": "Never run", "totalEvents": "{{count}} events total" },
  "tabs": { "script": "Script", "runLog": "Run Log" },
  "trigger": { "title": "Trigger", "subtitle": "cron expression · run manually anytime via Dry run", "cronLabel": "cron expression", "interpreter": "Interpreter", "timezone": "Timezone" },
  "script": { "title": "Script", "subtitle": "env injects HOOK_STATE / secrets · stdout emits JSON: { events, state }", "name": "Name", "namePlaceholder": "Hook name", "commandPlaceholder": "# Write your script; print { events, state } to stdout", "save": "Save", "create": "Create" },
  "env": { "title": "Environment / Secrets", "subtitle": "Injected as env vars · secrets are masked in the UI as ••••", "add": "Add variable", "keyPlaceholder": "KEY", "valuePlaceholder": "value", "secret": "Secret", "remove": "Remove variable", "empty": "No environment variables" },
  "run": { "ok": "Dry run · exit {{code}}", "failed": "Dry run failed · exit {{code}}", "timedOut": "Timed out", "meta": "{{ms}}ms · {{persist}}", "noPersist": "not persisted", "persisted": "persisted", "summary": "{{events}} events parsed · {{new}} new after dedupe · {{dup}} duplicates", "parseError": "Parse error: {{error}}", "stdout": "stdout", "stderr": "stderr", "running": "Running…", "empty": "Click Dry run to execute once; the result shows here" },
  "log": { "empty": "No events yet", "selectPrompt": "Select an event to view its payload", "dedupeKey": "Dedupe key", "payload": "Payload", "receivedAt": "Received {{time}}" },
  "interp": { "bash": "Bash", "sh": "Shell", "node": "Node", "python": "Python", "pwsh": "PowerShell", "powershell": "PowerShell", "cmd": "CMD" },
  "status": { "ok": "OK", "failed": "Failed", "running": "Running", "disabled": "Off", "idle": "Idle" },
  "flash": { "created": "Created Hook {{name}}", "updated": "Saved Hook {{name}}", "deleted": "Deleted Hook {{name}}", "enabled": "Enabled", "disabled": "Disabled", "saveFailed": "Save failed: {{error}}", "runFailed": "Run failed: {{error}}" },
  "del": { "title": "Delete Hook", "description": "Delete \"{{name}}\"? This cannot be undone.", "confirm": "Delete", "cancel": "Cancel" },
  "create": { "defaultName": "New Hook" },
  "time": { "never": "never", "secondsAgo": "{{count}}s ago", "minutesAgo": "{{count}}m ago", "hoursAgo": "{{count}}h ago", "daysAgo": "{{count}}d ago" }
}
```

> `i18n.test.ts` 要求 zh/en **key 完全对称**。两份必须逐 key 对齐。旧 `hooks` 块整段删除替换,确保没有遗留旧 key(否则一侧多 key → 测试红)。

---

## File Structure

| 文件 | 动作 |
| --- | --- |
| `frontend/src/components/agentre/hooks-page.tsx` | **整体重写**:`HooksPage` + 同文件子组件(`HookList`/`HookListItem`/`PageHeader`/`ScriptTab`/`TriggerCard`/`ScriptCard`/`EnvCard`/`RunResultCard`/`RunLogTab`/`EventDetail`/`DeleteDialog`)+ 辅助(`relativeTime`/`INTERP_META`) |
| `frontend/src/i18n/locales/zh-CN/common.json` | 替换 `hooks` 块(行 1112–1321) |
| `frontend/src/i18n/locales/en/common.json` | 替换 `hooks` 块(对称) |
| `frontend/src/components/agentre/__tests__/hooks-page.test.tsx` | **新增**:页面级 vitest(per-file wails mock) |
| `frontend/src/__tests__/App.test.tsx` | 更新 `mockHooks()`(返回新 `{hooks,events}` 形状 + CRUD/Run mock)+ Hooks 导航断言改成新页元素 |

---

## Task 1: i18n `hooks` 块替换(zh + en)

**Files:** 两份 `common.json`。

- [ ] **Step 1**: 把 zh-CN `common.json` 行 1112–1321 的整个 `"hooks": { ... }` 块替换为上面的 zh-CN 树(末尾逗号/缩进对齐周边)。
- [ ] **Step 2**: 把 en `common.json` 对应 `"hooks"` 块替换为 en 树。
- [ ] **Step 3**: 校验 JSON 合法 + key 对称:
  Run: `cd frontend && node -e "const z=require('./src/i18n/locales/zh-CN/common.json'),e=require('./src/i18n/locales/en/common.json');const keys=o=>Object.keys(o).flatMap(k=>typeof o[k]==='object'?keys(o[k]).map(s=>k+'.'+s):[k]);const zk=keys(z.hooks).sort(),ek=keys(e.hooks).sort();console.log('zh',zk.length,'en',ek.length,'equal',JSON.stringify(zk)===JSON.stringify(ek));"`
  Expected: `equal true`。
- [ ] **Step 4**: 跑 i18n 测试:`cd frontend && npx vitest run src/__tests__/i18n.test.ts` → PASS。
- [ ] **Step 5**: Commit `📝 hook: 重写 hooks i18n 文案为脚本驱动(zh+en)`。

---

## Task 2: 页面骨架 + 数据加载 + 左栏列表 + 头部(只读视图)

**Files:** 重写 `hooks-page.tsx`(本任务先到「能渲染列表 + 选中 + 头部」);新增 `__tests__/hooks-page.test.tsx`。

**Interfaces:** `export function HooksPage()`(签名不变,`index.ts`/`App.tsx` 已引)。内部状态:`hooks: HookItem[]`、`events: HookEventItem[]`、`selectedId`、`activeTab: "script"|"runLog"`、`loading`、`flash`、`draft`(编辑态,Task 3 用)。

- [ ] **Step 1: 写失败测试** `__tests__/hooks-page.test.tsx`:per-file mock runtime + `window.go.app.App.LoadHooks` 返回 2 个 hook(一个 enabled+lastStatus ok,一个 disabled),断言:渲染出两个 hook 名;副行含解释器缩写 + cron;选中第一个后头部显示其名 + `累计 N 事件`。
  - mock 形状参考 `App.test.tsx` 的 `mockHooks`(改成新 `{hooks:[HookItem...], events:[]}`)。runtime mock 见 [[reference_frontend_wails_runtime_test_mock]]。
- [ ] **Step 2**: 跑测试看失败(旧组件还在/导出形状不符)。
- [ ] **Step 3**: 重写 `hooks-page.tsx`:
  - 顶部 import:React、`useTranslation`、shadcn(`Button`/`Input`/`Badge`/...)、lucide `icons`、wails `LoadHooks` 等(从 `../../../wailsjs/go/app/App`)、`hook_svc` 类型(从 `../../../wailsjs/go/models`)。
  - `HooksPage`:`useEffect` 调 `LoadHooks({hookId:0,limit:50})` → setHooks;默认选中第一个;`loadHookDetail(id)` 调 `LoadHooks({hookId:id,limit:50})` 取该 hook 的 events。
  - `HookList`:标题「Hooks」+ count + `+` 按钮(Task 5 接 create)+ 搜索 Input;分组标签「定时任务」;`HookListItem`:`INTERP_META` 图标/配色 + 名 + 副行 + 状态点/「停用」;点击 setSelectedId。
  - `PageHeader`:大图标 + 名 + `·` + kindLabel + 状态 pill;子行 cron · 上次运行(`relativeTime`)· 累计事件;操作按钮(试运行/启停/⋮——本任务可先渲染按钮,处理在 Task 3/4/5)。
  - 自定义 Tabs(两个按钮,`activeTab` 控制下边框高亮)。
  - `relativeTime(unixSec, t)` 辅助:用 `hooks.time.*`。
- [ ] **Step 4**: 跑测试看通过。
- [ ] **Step 5**: Commit `✨ hook: 脚本驱动 Hooks 页骨架 + 列表 + 头部(只读)`。

---

## Task 3: 脚本 tab — 触发/脚本/env 卡 + 保存(Create/Update)

**Files:** `hooks-page.tsx`(加 `ScriptTab`/`TriggerCard`/`ScriptCard`/`EnvCard` + 编辑态 `draft` + 保存);测试加用例。

**Interfaces:** `draft: CreateHookRequest & {id?: number}`;选中 hook 时从 `HookItem` 投影成 draft(env 原样,secret 值已是 `••••`/`********` 占位)。保存:`id` 有→`UpdateHook`,无→`CreateHook`。

- [ ] **Step 1: 写失败测试**:选中一个 hook → 改 cron Input + 改 command Textarea → 点保存 → 断言 `UpdateHook` 被调且参数含新值;新增 env 行(key/value)+ 标记 secret → 保存参数 env 含该条 `secret:true`。
- [ ] **Step 2**: 跑测试看失败。
- [ ] **Step 3**: 实现:
  - `TriggerCard`:cron `Input`(绑 draft.scheduleExpr);解释器 `Select`(`INTERP_OPTIONS`,label=`t("hooks.interp."+v)`);时区 `Select`(`TZ_OPTIONS`)。
  - `ScriptCard`:name `Input` + 深色 `Textarea`(绑 draft.command,等宽字体 + `bg-[#121418] text-[#e6e6e6]`)。
  - `EnvCard`:`env[]` 行(key `Input` / value `Input`,secret 行 value `type` 视 `secret` 显占位;`Switch`/checkbox 标 secret;删除按钮)+「添加变量」;空态文案。
  - 保存按钮:组 `CreateHookRequest`(secret 值若仍是脱敏占位则原样回传,后端 `preserveSecrets` 保留)→ `id?` 选 Update/Create → 成功后 reload + flash。
- [ ] **Step 4**: 跑测试看通过。
- [ ] **Step 5**: Commit `✨ hook: 脚本 tab(触发/脚本/env 卡)+ 保存`。

---

## Task 4: 试运行(RunHook dryRun)+ 内联 RunResult + 运行日志 tab

**Files:** `hooks-page.tsx`(加 `RunResultCard`/`RunLogTab`/`EventDetail` + 试运行处理);测试加用例。

- [ ] **Step 1: 写失败测试**:
  - 点「试运行」→ `RunHook({id, dryRun:true})` 被调;mock 返回 `{exitCode:0,durationMs:412,events:[{title:"X"}],newCount:1,dupCount:1,persisted:false,stdout:"{...}"}` → 断言出现「退出码 0」「耗时 412ms」「不写库」「X」摘要。
  - 切到「运行日志」tab → 渲染 mock events 的 title;点一条 → 详情区显示其 `payloadJson`。
- [ ] **Step 2**: 跑测试看失败。
- [ ] **Step 3**: 实现:
  - 试运行:`setRunning(true)` → `RunHook({id, dryRun:true})` → setRunResult;失败 flash。`RunResultCard`:绿/红边框(看 exitCode/parseError/timedOut),头部 `hooks.run.ok/failed` + meta(耗时 + 不写库/已入库),摘要 `hooks.run.summary`,parseError 行,stdout/stderr 深色块(动态内容不进 t())。
  - `RunLogTab`:左事件列表(`events`,title + `relativeTime(receivedAt)`)+ 右 `EventDetail`(title/dedupeKey/payloadJson 高亮块);空态 `hooks.log.empty`/`selectPrompt`。
- [ ] **Step 4**: 跑测试看通过。
- [ ] **Step 5**: Commit `✨ hook: 试运行内联结果 + 运行日志两栏`。

---

## Task 5: 新建 / 启停 / 删除 + 收尾(App.test 更新 + 全量绿)

**Files:** `hooks-page.tsx`(+ 按钮处理 + `DeleteDialog`);`App.test.tsx`;全量验证。

- [ ] **Step 1: 写失败测试**(hooks-page.test.tsx):
  - `+` 新建 → 进入空 draft(name=默认),保存调 `CreateHook`。
  - 启停按钮 → `ToggleHook(id, !enabled)`。
  - ⋮ → 删除 → 确认 Dialog → `DeleteHook(id)`,列表移除。
- [ ] **Step 2**: 跑测试看失败 → 实现:
  - 新建:`+` → `draft = {name:t("hooks.create.defaultName"),interpreter:"bash",command:"",scheduleExpr:"",timezone:"Asia/Shanghai",env:[],enabled:true}`,`selectedId=null`,切到脚本 tab。
  - 启停:`ToggleHook` → reload + flash。
  - 删除:`DropdownMenu`「删除」→ `AlertDialog`/`Dialog` 确认(`hooks.del.*`)→ `DeleteHook` → 从 hooks 去除 + 选中切换 + flash。
- [ ] **Step 3: 更新 `App.test.tsx`**:把 `mockHooks()` 改成新形状(`LoadHooks`→`{hooks:[新HookItem],events:[]}`,补 `CreateHook/UpdateHook/DeleteHook/ToggleHook/RunHook` mock);把「opens the implemented Hooks workspace」测试的断言改成新页元素(如 hook 名 / 「脚本」tab)。
- [ ] **Step 4: 全量验证**:
  Run: `cd frontend && npx vitest run` → 全绿(含 i18n / App / hooks-page)。
  Run: `cd frontend && npx eslint src/components/agentre/hooks-page.tsx src/i18n` → 0 error(尤其 `i18next/no-literal-string`)。
  Run(可选,确认 TS):`cd frontend && npx tsc --noEmit`(若项目惯例跑;失败若仅 wailsjs 类型缺失则 `make generate`/cp 后再跑)。
- [ ] **Step 5**: Commit `✨ hook: 新建/启停/删除 + App 测试对齐脚本驱动 Hooks 页`。

---

## 完成后

- superpowers:finishing-a-development-branch:`cd frontend && npx vitest run` 全绿 → 呈现 4 选项 → 执行。沿用 Plan 1/2 收尾(rebase develop/wyz 后 `--ff-only`,线性历史;ExitWorktree remove)。

## Self-Review

- **Spec 覆盖**:§8.2 左栏列表(T2)/ 两 tab(T2)/ 触发+解释器 Select+脚本编辑器+env(T3)/ 内联试运行(T4)/ 运行日志两栏(T4)/ 删旧 source-rule-IMAP UI 与文案(T1+T2 重写)/ i18n 双份(T1)。
- **绑定**:仅用 6 个新绑定,旧绑定全移除。
- **类型一致**:draft 投影对齐 `CreateHookRequest`/`UpdateHookRequest`;RunResult 字段对齐 `RunHookResult`;events 来自 `LoadHooksResponse.events`。
- **测试**:每任务先红后绿;`App.test.tsx` 旧 Hooks 断言同步更新(否则全量红)。
- **风险**:`hooks-page.tsx` 是 2695 行重写——保持单文件 + 同文件子组件,逐 tab 增量提交;`bg-status-running` 等类名执行期用 grep 校验实际可用类。
