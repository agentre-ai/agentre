# 群聊删除入口设计 (Delete group chat)

- 日期: 2026-06-10
- 范围: `agentre/` (Wails 桌面端,Go 后端 + React 前端)
- 状态: 待实现

## 背景与问题

群聊设置面板当前只有一个 **「归档群」(Archive)** 按钮 (`group-roster.tsx:173`)。该按钮:

1. 直接调用 `GroupArchive(group.id)`,**没有任何确认弹层**;
2. `ArchiveGroup` 无条件软删全群成员的 backing session,并把 `group.Status` 置为 `consts.DELETE`;
3. **不发任何事件、也不触发前端刷新** —— 这正是用户反馈「归档似乎没有看见作用」的根因:DB 里软删了,但侧栏/打开的标签页毫无反应。

用户诉求:给群聊一个**删除入口**,删除时可以**选择是否一并删除关联的会话**;并且**直接去掉归档功能**,只保留删除。

## 目标

- 用「删除群」取代「归档群」,删除时通过确认弹层让用户决定是否删除关联的成员 backing session。
- 删除后 UI **立即可见**:侧栏移除该群、关闭其打开的标签页。

## 非目标 (YAGNI)

- 不做「回收站 / 恢复已删除群」。
- 不在侧栏群行加右键/kebab 菜单(本期入口只在群设置面板;侧栏菜单留待后续)。
- 不改 `RemoveGroupMember` / 单成员 backing session 的清理逻辑。
- 不做数据库 schema 变更(复用现有 `status` 软删 + `chat_sessions.group_id`)。

## 关键决策 (已与用户确认)

| 决策点 | 选择 |
| ------ | ---- |
| 归档 vs 删除 | **删除归档功能**,只保留删除 |
| 入口位置 | 群设置面板(原归档按钮处) |
| 「同时删除关联会话」复选框默认 | **不勾选**(默认保留会话) |
| 不删会话时如何处理 | **保持原样 leave linked**(会话仍带 `group_id`,不 detach) |
| 群行本身 | **软删 `status=DELETE`**(与现有模式一致;app 未发布但保持统一) |

### 已知取舍

「默认保留会话 + leave linked」意味着:删除群后,被保留的 backing session 仍带着指向已删群的 `group_id`,侧栏里可能仍显示一个指向已不存在群的「Group」徽标。此为用户明确选择,接受。

## 设计

### 后端 (`internal/service/group_svc/`, `internal/app/group.go`)

**移除归档:**

- 从 `GroupSvc` 接口删除 `ArchiveGroup(ctx, id) error` (group.go:60)。
- 删除其实现 (group.go:645-669)。
- 删除 wails 绑定 `func (a *App) GroupArchive(id int64) error` (app/group.go:137)。

**新增删除:**

```go
// DeleteGroup 删除群(软删 status=DELETE)。
// deleteSessions=true 时一并软删全群成员的 backing session;false 时保留会话原样。
func (s *groupSvc) DeleteGroup(ctx context.Context, id int64, deleteSessions bool) error
```

行为:

1. `group_repo.Group().Find(ctx, id)`,空 → `i18n.NewError(ctx, code.GroupNotFound)`。
2. `s.stopAll(ctx, id)` —— 停掉在跑的成员轮(沿用归档逻辑)。
3. 若 `deleteSessions`:`group_repo.Member().ListByGroup` 遍历,对 `BackingSessionID > 0` 的成员调用 `s.gw.DeleteSession(ctx, BackingSessionID)`;best-effort,单条失败仅 `logger.Ctx(ctx).Warn` 不阻断(沿用归档的清理代码)。
4. 若 `!deleteSessions`:不动 backing session。
5. `g.Status = consts.DELETE`;`logger.Ctx(ctx).Info("group_svc.DeleteGroup: deleted", ...)`;`group_repo.Group().Update(ctx, g)`。

wails 绑定:

```go
func (a *App) GroupDelete(id int64, deleteSessions bool) error {
	return group_svc.Default().DeleteGroup(a.ctx, id, deleteSessions)
}
```

`make generate` 后 `frontend/wailsjs` 中 `GroupArchive` 消失、`GroupDelete(id, deleteSessions)` 出现。

### 前端 (`frontend/src/components/agentre/group-chat/`)

**确认弹层** —— 复用现有 shadcn `@/components/ui/dialog.tsx` + `@/components/ui/checkbox.tsx`(均已存在)。新建一个小组件(或内联于 roster),包含:

- 标题:`t("group.delete.title")` —— 「删除群聊？」
- 描述:`t("group.delete.description")` —— 说明会移除该群,默认保留成员会话。
- 复选框:`t("group.delete.alsoSessions")` —— 「同时删除关联的会话」,默认 **不勾选**。
- 按钮:Cancel(`t("common.cancel")` 或现有取消键)+ 删除(`variant="destructive"`)。

**`group-roster.tsx`:**

- 把 Settings tab 里的 Archive `<Button>`(175-181)换成 destructive 的 **Delete** 按钮,点击打开弹层。
- prop `onArchive: () => void` → `onDelete: (deleteSessions: boolean) => void`(由弹层确认时回传复选框值)。

**`index.tsx` (`GroupChat`) → `chat-page.tsx`:**

- 移除 `GroupArchive` import 与 `onArchive={() => void GroupArchive(group.id)}`(19, 251)。
- 新增 `onDeleted`(或在 `GroupChat` 内直接驱动 store):确认删除时
  1. `await GroupDelete(group.id, deleteSessions)`;
  2. `await useGroupListStore.getState().reload()`(侧栏移除该群 —— 对应 `toggleGroupPin` 在 `chat-page.tsx:712` 的写法);
  3. 关闭该群的标签页(`chat-tabs-store` 的 close,按 `kind==="group" && groupId` 匹配)。

> 第 2、3 步正是归档当前缺失、导致「没有看见作用」的部分。

### i18n (`frontend/src/i18n/locales/{zh-CN,en}/common.json`)

新增 `group.delete`:

```jsonc
"delete": {
  "button": "删除群" / "Delete group",
  "title": "删除群聊？" / "Delete group?",
  "description": "将移除该群。成员会话默认保留,除非你勾选下面的选项。" / "This removes the group. Member conversations are kept unless you opt in below.",
  "alsoSessions": "同时删除关联的会话" / "Also delete associated sessions",
  "confirm": "删除" / "Delete"
}
```

删除已失效的 `group.settings.archive` 键(两个 locale 都删)。`group.settings` 下仍保留 `workdir`。

## 测试 (TDD: Red → Green → Refactor)

### 后端 (`internal/service/group_svc/control_test.go`)

把现有两个 `TestArchiveGroup_*` 改写为 `TestDeleteGroup_*`:

1. `TestDeleteGroup_DeleteSessionsTrue_StopsAndDeletes`:`deleteSessions=true` → `stopAll` + 对每个 `BackingSessionID>0` 调 `gw.DeleteSession`(跳过 0)+ `status=DELETE` + `Update`。
2. `TestDeleteGroup_DeleteSessionsFalse_KeepsSessions`:`deleteSessions=false` → **不**调 `gw.DeleteSession`,但仍 `status=DELETE` + `Update`。
3. (可选)`TestDeleteGroup_NotFound`:群不存在返回 `GroupNotFound`。

仓储单测用 sqlmock,service 单测用 mockgen 注入 repo / gateway mock(沿用现有 `control_test.go` 的搭法),不连真库。

### 前端

- 更新 `group-chat.test.tsx`(把 `GroupArchive: vi.fn()` 换成 `GroupDelete: vi.fn()`)与 `group-roster.test.tsx`(prop 改名)。
- 新增:点 Delete 打开弹层;勾选/不勾选复选框后确认,断言 `GroupDelete` 以正确的 `deleteSessions` 入参被调用。
- `i18n.test.ts` 覆盖新键、确认旧 `group.settings.archive` 移除后无悬空引用。

## 实施顺序

1. 后端:改写 `control_test.go` 的两个测试为 DeleteGroup(Red)。
2. 后端:接口/实现移除 Archive、加 `DeleteGroup`;加 `GroupDelete` 绑定(Green)。
3. `make generate` 刷新 wailsjs 绑定。
4. 前端:更新 mock/测试(Red)→ Delete 按钮 + 弹层 + checkbox + 接线 reload/closeTab(Green)。
5. i18n:加 `group.delete.*`、删 `group.settings.archive`;跑 i18n 测试。
6. `make lint` + `make test-backend` + 相关前端 Vitest 全绿。
