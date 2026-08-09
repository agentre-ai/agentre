// exec-target-reorder 是 R15 执行目标有序列表的纯排序辅助——不碰 DOM/dnd-kit，
// 拖拽（DndContext.onDragEnd）与键盘等价的「上移/下移」按钮共用同一个函数，保证
// 两条路径产出同一个结果。镜像 org/reorder.ts 的 computeReorder 写法。

/** moveItem 把下标 from 的元素挪到下标 to；越界或原地不动时原样返回（引用不变，
 * 便于调用方用 `next === list` 判断是不是 no-op）。*/
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (
    from === to ||
    from < 0 ||
    from >= list.length ||
    to < 0 ||
    to >= list.length
  ) {
    return list;
  }
  const next = list.slice();
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}
