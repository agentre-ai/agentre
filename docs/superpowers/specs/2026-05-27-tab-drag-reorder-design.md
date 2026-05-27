# Tab 拖拽排序 — 设计文档

- **Date**: 2026-05-27
- **Status**: Design approved, pending implementation plan
- **Scope**: 前端 `chat-tabs/` 模块 + `chat-tabs-store`

## 背景

当前 tab 列表按"三区自动排序"渲染:`pinned → attention → idle`,每个区内还有各自的子排序规则(pinAt / lastMessageAt / active 浮顶)。`selectSortedTabs` 把 `ChatTab[]` 重新排出来,UI 拿到的是计算结果而不是用户控制的顺序。

用户希望"tab 支持拖拽排序"——这不是简单加一个 DnD 那么简单,而是要从"系统自动排序"切换到"用户全手动排序"模型,这会动到 store / selector / view / 测试多个层。

## 目标

- tab-strip 内可水平拖拽 tab 改变顺序。
- 顺序由用户控制,持久化跟随既有 tabs 数组 schema(无需迁移)。
- 保留 `isPinned` 概念,但 "pinned 区域" 由列表**连续前缀**隐式定义,不再显式分组。
- attention 状态仍提供视觉提示,且**首次进入 attention** 时自动 bump 到 pinned 前缀之后;持续 / 重复 attention 不再扰动用户拖出的位置。

## 非目标

- 不做跨窗口拖拽。
- 不做 tab 分组 / 分栏 / split view。
- 不引入后端 schema 改动。

## 模型变更

### `ChatTab` (store)

字段保持不变。`isPinned` / `pinAt` / `isPreview` 都还在:

- `isPinned`: 仍是用户/系统标记的 pin 状态(影响 pin 图标和 §2 的 normalize 规则)。
- `pinAt`: 不再用于排序,降级为审计字段。保留是为了少改 store 的 set 逻辑,不主动删。
- `isPreview`: 不变,promote 仍是用户行为。

### `TabView` (`use-tabs-view.ts`)

删除 `zone` 字段及 `TabZone` 类型。`tab-strip.tsx` 内不再渲染 `tab-zone-divider`,该 testid / 视觉元素一并移除。

### `tabs` 数组的语义

`tabs` 从"未排序的池子(由 selector 重排)"升级为"用户控制的有序列表"。所有写入操作(open / close / pin / move / bump)都直接维护这个数组的顺序。

### Store 新增 action

```ts
// 用户拖拽产生的位置变动。会调用 normalize() 调整 isPinned (见 §2)。
moveTab: (fromIndex: number, toIndex: number) => void;

// 系统行为:把 id 对应的 tab 搬到 lastPinnedPrefixIndex + 1 位置。
// 不触发 normalize() —— 这是 attention bump 专用,绝不应改 isPinned。
bumpToAfterPinned: (id: string) => void;
```

`closeTab` / `openSession` / `openNewSession` / `togglePin` 行为基本不变,但要做的小调整:

- `togglePin(id)` 将 tab 标为 pinned 时,additionally 把它**搬到 pinned 前缀的末端**(即新 last pinned index),避免出现"pinned 但卡在列表中间"的不一致状态。取消 pin 时位置不动。
- 其他 open*/close 行为保持现状(append 到末尾 / 替换 preview slot)。

## §2 pinned 自动调整规则(`normalize`)

"pinned 区域"定义:从 index 0 起的、`isPinned=true` 的**连续前缀**。设 `lastPinnedPrefixIndex` 为该前缀的最后一个 index;空前缀时为 `-1`。

`moveTab(from, to)` 先执行 splice,**然后**基于新数组计算 `lastPinnedPrefixIndex`,再对被搬动的 tab(记其新 index 为 `final`)做一次检查:

1. 如果 `tab.isPinned` 且 `final > lastPinnedPrefixIndex`(自己已不在 pinned 前缀里) → 设 `isPinned = false, pinAt = 0`。
2. 如果 `!tab.isPinned` 且 `final <= lastPinnedPrefixIndex` → **不自动加 pin**(用户只指定了反向 unpin)。这会临时打破"pinned 都在前缀里"的不变式——原本紧跟在 `final` 之后的 pinned tab 现在被"踢出"前缀。我们容忍这种状态:用户可以拖回去或右键重新 pin。后续 `bumpToAfterPinned` 用的就是"当前 prefix 末端 + 1",自然会落在新的(变短的)prefix 之后。

`bumpToAfterPinned(id)` **不**调用 normalize(它是系统行为,不应改 isPinned;且搬到 lastPinnedPrefixIndex + 1 的位置天然在前缀之外)。

## §3 Attention 一次性 bump

### 触发时机

边沿触发:某个 tab 从 `attention=false` → `attention=true` 的那一帧执行一次 bump。持续 attention(状态保持 true)不触发。从 true → false → true 的二次进入算新一次 bump。

### 实现位置

新建 `frontend/src/components/agentre/chat-tabs/use-attention-bump.ts`:

```ts
export function useAttentionBump(attentionTabIds: Set<string>) {
  const prev = React.useRef<Set<string>>(new Set());
  React.useEffect(() => {
    const bumped = useChatTabsStore.getState().bumpToAfterPinned;
    for (const id of attentionTabIds) {
      if (!prev.current.has(id)) bumped(id);
    }
    prev.current = attentionTabIds;
  }, [attentionTabIds]);
}
```

在 `TabStrip` 或 `use-tabs-view` 内调用一次。注意 effect 依赖必须是稳定引用(`attentionTabIds` 需要 useMemo,这一点 `use-tabs-view.ts` 已经做了)。

### preview tab 不再有"自动浮顶"副作用

`promoteCurrent` 行为不变,只切 `isPreview=false`,不动位置。新开 preview / 替换 preview slot 也不动顺序,仍走现有的"replace existing preview / push to end"路径。

## §4 拖拽交互 (UI)

技术栈:`@dnd-kit/core` + `@dnd-kit/sortable`(均已安装,见 `frontend/package.json`)。

### Tab-strip 改造

```
TabStrip
└── DndContext (sensors, onDragEnd → moveTab)
    └── SortableContext (items=tabIds, strategy=horizontalListSortingStrategy)
        └── TabWithContextMenu (useSortable → setNodeRef, transform, listeners)
```

- 拖拽方向:水平。
- 拖拽 handle:整个 tab 都是 handle。
- 不使用 `DragOverlay`——`useSortable` 默认的 `transform` 位移真 DOM 即可。
- `autoScroll`: 默认开启,跨可视区拖拽时 tab-strip(已有 `overflow-x-auto`)自动滚动。

### 与 ContextMenu / Click 的解耦

`PointerSensor` 配置 `activationConstraint: { distance: 4 }`:
- 移动距离 < 4px:不进入拖拽模式,click 和 contextmenu 正常分发。
- 移动距离 ≥ 4px:进入拖拽,click 被吞掉。

这避免了"右键想出菜单结果触发了 drag"以及"想点 tab 切换结果手抖触发 drag"。

### 键盘可用性

附加 `KeyboardSensor` + `sortableKeyboardCoordinates`(参考 `org-tree.tsx` 用法),Tab 聚焦后 Space 抓起、Arrow 移动、Space 落下、Esc 取消。

### onDragEnd

```ts
function onDragEnd(e: DragEndEvent) {
  const { active, over } = e;
  if (!over || active.id === over.id) return;
  const ids = sortedTabs.map(t => t.id);
  const from = ids.indexOf(String(active.id));
  const to = ids.indexOf(String(over.id));
  if (from < 0 || to < 0) return;
  moveTab(from, to);
}
```

## §5 测试 & 持久化

### 持久化

`chat-tabs-persistence.ts` 已完整 serialize `tabs` 数组。顺序天然跟着数组走,**不需要 schema / 版本号变更**。

### 测试改动

| 文件 | 操作 |
|---|---|
| `stores/__tests__/chat-tabs-store.test.ts` | **新增** `moveTab` 用例(simple reorder / 拖出 pinned 触发 unpin / 拖进 pinned 前缀不自动 pin)、`bumpToAfterPinned` 用例(不改 isPinned、移到 lastPinnedPrefixIndex+1) |
| `stores/__tests__/chat-tabs-store-selectors.test.ts` | **删除**:整个文件随 `selectSortedTabs` 一并删除。如果还有其他 selector(例如 `selectActiveTab`),保留对应 case 并拆到合适文件 |
| `stores/chat-tabs-store-selectors.ts` | **删除整个文件**(或只删 `selectSortedTabs` 函数,若文件还有其他 selector 则保留壳子) |
| `chat-tabs/__tests__/tab-strip.test.tsx` | **删除** zone-divider 渲染断言;**新增** 拖拽用例(用 `@dnd-kit` 提供的 keyboard sensor 走 Space+Arrow+Space 路径,或 pointer 事件序列) |
| `chat-tabs/__tests__/use-attention-bump.test.ts` | **新增**:tab 由非 attention → attention 时调用一次 `bumpToAfterPinned`;状态持续 true 不重复触发;true→false→true 重新触发 |
| `chat-tabs/__tests__/tab-overflow-menu.test.tsx` | 如果它依赖 zone 字段排序或显示,调整为按数组顺序 |
| `chat-tabs/use-tabs-view.ts` | 移除 `selectSortedTabs` 调用、`zone` 字段、`TabZone` 类型导出。如有 view 层单测,调整 |

### 后端影响

无。无 Wails binding、protobuf、SQL、Go 代码改动。

## 实现顺序(给后续 plan 用)

1. store: `moveTab` + `bumpToAfterPinned` action + `togglePin` 微调,先红后绿。
2. 删 `selectSortedTabs` + 调整 `useTabsView`(去掉 zone 字段),用 view 单测(如有)护栏。
3. `useAttentionBump` hook + 单测。
4. `TabStrip` 接 `DndContext` / `SortableContext`,删 zone-divider,拖拽 + a11y 用例。
5. `togglePin` 自动归位到 pinned 前缀末端,补用例。
6. `make test` + `make lint` 整体跑过。

## 风险 / 边界

- **PointerSensor distance=4 与现有 click handler 冲突**:`tab.tsx` 上还有 double-click promote 等逻辑,要确认 dnd-kit 的 listeners 不会吞掉 dblclick。如果吞了,要么改用 `MouseSensor` 加 delay 区分,要么把 dblclick 处理移到 dnd-kit 不接管的层。实现时先验证。
- **触发 bump 期间用户正在拖拽**:dnd-kit 拖拽中外部 reorder 会让索引错乱。`bumpToAfterPinned` 需要在拖拽进行时短暂禁用——可以用一个 `isDragging` ref 跳过 bump,等 onDragEnd 后再补一次 diff。这一点在 plan 阶段细化。
- **多 attention tab 同帧 bump 的稳定顺序**:遍历 `attentionTabIds` 时如果一帧内同时新出现两个,bump 顺序由 Set 遍历顺序决定。可以接受(用户随后能拖),不专门处理。
