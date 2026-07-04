# 组织架构详情表单 —— 去掉取消/保存,改为自动保存

- 日期: 2026-07-04
- 分支: develop/wyz
- 范围: `agentre/`(桌面端 Wails,前端 React/TS)

## 背景与目标

组织架构页(org-chart)选中节点后,右侧滑出详情编辑面板。当前有两个共用同一套「取消 / 保存」页脚的编辑表单:

- `frontend/src/components/agentre/org/org-detail-department.tsx`(部门详情,已有 `dirty` 追踪)
- `frontend/src/components/agentre/org/org-detail-agent.tsx`(智能体详情,**无** `dirty` 追踪,保存按钮恒可点)

目标:**去掉页脚的取消 / 保存按钮,任何字段有变动就直接保存**,两个表单体验一致。

用户已确认的三个决策:

1. **范围**:两个表单都改(部门 + 智能体)。
2. **文本字段保存时机**:防抖 600ms + 失焦(blur)立即存;下拉框 / 图标 / 颜色 / 开关等离散选择即时保存;名称为空时暂不保存。
3. **页脚**:保留一条轻量「自动保存状态」条(保存中 / 已保存 / 未保存 / 保存失败[重试]),不再有取消/保存按钮。头部的 X(关闭)保留。

## 现状事实(实现依据)

- 两个面板由父级 `org-chart-page.tsx` 的 `renderDetail()` 渲染,`key` 为 `dept-${id}` / `agent-${id}`。选中同一节点时不会 remount,因此保存后触发的 reload 不会打断正在输入的本地编辑。
- `use-org-data.ts` 的 `mutate()` 在所有 in-flight 归零后自动 `reload()` 整棵组织树。这意味着每次成功保存后 props(baseline)会刷新为已保存值——正是 baseline 自动对齐的机制,**本次不改这个 hook**。
- 部门 `handleSave`:先 `onUpdate({id,name,description,icon,accentColor,leadAgentId})`,当 `parentId` 变化时再 `onMove({id,newParentId,newSortOrder:0})`。
- 智能体 `handleSave`:`onUpdate` 一次性提交 `{name,description,avatarColor,avatarIcon,agentBackendId,prompt,skills,tools}`。头像 `onUploadAvatar` / `onDeleteAvatar` 已是**独立的即时调用**,不走 `handleSave`。
- 已有 i18n:`common.saving`(保存中…)、`common.saved`(已保存)、`common.unsavedChanges`(未保存的修改)、`common.operationFailed`(操作失败)。顶层 `common` **没有** `saveFailed` / `retry`,需新增。

## 方案:共享 `useAutoSave` hook

在 `org/use-auto-save.ts` 新建一个可复用 hook,被两个详情表单共用,而不是在每个组件里各写一遍防抖 / 状态机(否则计时器逻辑重复两份且会漂移)。

### 契约

```ts
type AutoSaveStatus = "idle" | "saving" | "saved" | "error";

useAutoSave<T>({
  initial: T,
  save: (values: T) => Promise<unknown>,
  debounceMs?: number,          // 默认 600
  isValid?: (values: T) => boolean,
}) => {
  values: T,
  patch: (partial: Partial<T>, opts?: { immediate?: boolean }) => void,
  flush: () => void,
  wrap: <R>(fn: () => Promise<R>) => Promise<R | null>,
  status: AutoSaveStatus,
  pendingInvalid: boolean,
  retry: () => void,
}
```

行为约定:

- `save` 总是发送**完整的合并后 `values`**;`values` 同步写入一个 ref,避免离散控件在 `patch` 之后立即保存时读到过期闭包。
- `patch(partial, {immediate})`:合并进 `values`;
  - `immediate: true` → 取消待触发的防抖并立即保存;
  - 否则 → 以 `debounceMs` 防抖;快速连续 `patch` 合并为一次保存(取最新值)。
  - 若 `isValid(values)` 为 false → **不调度保存**,置 `pendingInvalid = true`;下一次有效 `patch` 再保存。
- `flush()`:若存在待触发的防抖保存,立即执行(文本输入框 `onBlur` 调用)。
- `wrap(fn)`:把任意异步 mutation 纳入同一状态机(saving → saved / error),用于部门 `onMove`、智能体头像上传/删除等不在 `values` payload 里的独立保存。失败返回 `null`。
- `retry()`:重跑最近一次失败的保存(payload 保存或 wrap 的 fn)。
- 挂载时不保存:只有用户交互触发 `patch` / `wrap` 才会保存。

### 状态优先级(状态条渲染)

`error` > `saving` > `pendingInvalid`(渲染为 `common.unsavedChanges`)> `saved` / `idle`。

## 各表单接线

### 部门 `org-detail-department.tsx`

- autosave payload = `{name, description, icon, accentColor, leadAgentId}`,`save` 调 `onUpdate`。
- 文本(name / description):`onChange → patch({...})`(防抖),`onBlur → flush()`;`isValid` = `name.trim() !== ""`。
- 图标 / 颜色 / 负责人 Select:`patch({...}, {immediate:true})`。
- **父部门 Select**:保留为**独立的即时 mutation**,`onValueChange → wrap(() => onMove({id,newParentId:v,newSortOrder:0}))`,不进 update payload(结构性变更,和智能体头像的处理方式一致)。
- 删除弹窗、路径面包屑、成员列表、添加成员/子部门按钮:不变。

### 智能体 `org-detail-agent.tsx`

- autosave payload = `{name, description, avatarColor, avatarIcon, agentBackendId, prompt, skills, tools}`,`save` 调 `onUpdate`(沿用 `agent_svc.UpdateAgentRequest.createFrom`,`prompt` 仍按行 split/filter)。
- 文本(name / description / prompt):防抖 + `onBlur` flush;`isValid` = `name.trim() !== ""`。
- 颜色 / 图标 / backend / skills / tools:`immediate`。
- 头像 `onUploadAvatar` / `onDeleteAvatar`:改用 `wrap(...)` 包裹,喂给同一状态条。

## 页脚 → 自动保存状态条

新建一个共享的小组件(如 `org/auto-save-status.tsx` 的 `AutoSaveStatus`),替换两个表单里的 `<footer>` 取消/保存块。保留 `History` 图标 + 一行文案:

- `saving` → `common.saving`(保存中…)
- `saved` / `idle` → `common.saved`(已保存)
- `pendingInvalid` → `common.unsavedChanges`(未保存的修改)
- `error` → `common.saveFailed`(保存失败)+ 一个「重试」按钮(`common.retry`)调 `retry()`

新增 i18n key(zh-CN + en 同时更新):`common.saveFailed`(保存失败 / Save failed)、`common.retry`(重试 / Retry)——若已存在则复用。错误详情不拼进 `t(...)`(状态条只显示「保存失败」)。

## 测试(TDD:Red → Green)

**新 `org/__tests__/use-auto-save.test.tsx`**(renderHook + vitest fake timers):

- 即时保存:`patch(x, {immediate:true})` → `save` 被调用一次且入参为合并后的完整 values,status `saving → saved`。
- 防抖合并:连续多次 `patch`(非 immediate)→ 防抖窗口内不调用;推进计时器后只调一次,入参为最新值。
- `flush()`:存在待触发防抖时立即执行保存。
- 有效性:`isValid` 为 false 时不保存、`pendingInvalid` 为 true;变有效后下一次 `patch` 保存。
- 失败:`save` reject → status `error`;`retry()` 重跑。
- `wrap(fn)`:驱动 status(saving → saved / error),失败返回 null。

**更新 `org/__tests__/org-detail-department.test.tsx` / `org-detail-agent.test.tsx`**:

- 断言页脚不再有「取消 / 保存」按钮。
- 文本字段输入后 blur → `onUpdate` 被调用(带最新值)。
- 颜色 / 图标 / Select 变更 → 立即触发 `onUpdate`;部门父级变更 → `onMove`。
- 状态条在保存后显示「已保存」。

## 不改动 / 范围外

- 后端 Wails 绑定与 service:不变。
- `use-org-data.ts`(mutate/reload 现有行为):不变(baseline 刷新依赖它)。
- 删除确认弹窗、拖拽排序、面包屑、成员列表:不变。
- 不做无关重构 / rename / 格式化扫。diff 只含新 hook + 状态条 + 两个详情表单接线 + 各自测试 + i18n 两个 key。
