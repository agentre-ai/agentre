import * as React from "react";

/**
 * 一行能被键盘触发的动作。容器只从 DOM 读「可见行序」与每行的层级 / 展开态
 * （`aria-level` / `aria-expanded` 就是这两件事的唯一真源），要执行的动作则由
 * 行自己登记进来——容器因此完全不需要知道任何一种模式的数据结构。
 */
export type SidebarRowActions = {
  /** 目录行的展开 ↔ 收起；文件行为 null。 */
  toggle: (() => void) | null;
  /** Enter 对文件行的动作（开预览）；不可预览的行为 null。 */
  activate: (() => void) | null;
  /** `Shift+F10` / 菜单键打开这一行的菜单；没有菜单的行为 null。 */
  openMenu: (() => void) | null;
};

type ListContext = {
  itemRole: "treeitem" | "option";
  register: (
    key: string,
    actions: React.RefObject<SidebarRowActions>,
  ) => () => void;
};

const SidebarListContext = React.createContext<ListContext | null>(null);

/** 行的选择器：`data-row-key` 只由 useSidebarListRow 写，等价于「这是一行」。 */
const ROW_SELECTOR = "[data-row-key]";

type Props = {
  /**
   * 树（「变动」/「目录」两个模式）还是扁平列表（Git 页与搜索结果）。
   *
   * 扁平列表刻意不用 `tree`：它没有层级，也没有可以就地探入的下一层，套上
   * `treeitem` 就得给每行编一个假的 `aria-level`，并让读屏宣告一棵根本不存在的
   * 树。它真正的形状是「一组同级的文件，同时至多有一个被选中打开在预览面板
   * 里」——那正是 `listbox` / `option` + `aria-selected` 的语义。两者共用同一套
   * 上下键 / Home / End / Enter / Shift+F10 的键盘模型，只有左右键（展开收起与
   * 父子跳转）对扁平列表无意义、不响应。
   */
  variant: "tree" | "list";
  /** 列表整体的可访问名。 */
  label: string;
  className?: string;
  children: React.ReactNode;
};

/**
 * SidebarList 是文件列表的键盘外壳（served requirement「键盘与无障碍」）：整个
 * 列表对 Tab 键只是**一个**停靠点，内部靠 roving tabindex 移动——同一时刻只有一
 * 行 `tabindex="0"`，其余全是 `-1`，方向键把焦点命令式地移到目标行上。
 *
 * tabindex 完全由这里写进 DOM、行自己不渲染这个属性：让它成为 React 的受控属性
 * 就得把「当前是哪一行」提升成 state，于是每一次按方向键都要重渲染整棵树（大目
 * 录下几百行）。这里换成 ref + layout effect 校准，按键路径上零重渲染。
 *
 * 两条结构上的取舍：
 * - 层级用 `aria-level` 表达，不套 `role="group"`。子项在 DOM 里是它那一行的
 *   **兄弟**（行是定高的一条 flex 行，装不下子树），这种嵌套关系下 treeitem 并
 *   不「拥有」那个 group，ARIA 给的正解就是显式 `aria-level`。
 * - 加载 / 空 / 截断 / 单层读取失败这些状态文本留在容器内、和行交错着排。它们
 *   是这一层自己的状态，位置本身有信息（哪一层被截断了）；挪到列表外就得复述
 *   一遍「哪一层」，也会让它在视觉上脱离那一段。
 */
export function SidebarList({ variant, label, className, children }: Props) {
  const rootRef = React.useRef<HTMLDivElement>(null);
  const actionsByKey = React.useRef(
    new Map<string, React.RefObject<SidebarRowActions>>(),
  );
  const rovingKeyRef = React.useRef<string | null>(null);

  const register = React.useCallback(
    (key: string, actions: React.RefObject<SidebarRowActions>) => {
      actionsByKey.current.set(key, actions);
      return () => {
        if (actionsByKey.current.get(key) === actions) {
          actionsByKey.current.delete(key);
        }
      };
    },
    [],
  );

  const itemRole = variant === "tree" ? "treeitem" : "option";
  const context = React.useMemo<ListContext>(
    () => ({ itemRole, register }),
    [itemRole, register],
  );

  // 可见行序 = DOM 顺序。收起的子树根本没渲染，懒加载还没到的层也不在里面，
  // 所以这一个查询就是「上下键要走过的那条序列」的完整定义。
  const rowsOf = React.useCallback(
    () =>
      Array.from(
        rootRef.current?.querySelectorAll<HTMLElement>(ROW_SELECTOR) ?? [],
      ),
    [],
  );

  const setRoving = React.useCallback(
    (key: string | null) => {
      rovingKeyRef.current = key;
      for (const el of rowsOf()) {
        el.setAttribute("tabindex", el.dataset.rowKey === key ? "0" : "-1");
      }
    },
    [rowsOf],
  );

  // 每次容器重渲染后校准停靠点：行会异步长出来（目录懒加载）、也会因为收起而
  // 消失。落点仍在就保持不动，否则退回首行——列表任何时刻都恰好有一个停靠点。
  React.useLayoutEffect(() => {
    const items = rowsOf();
    if (items.length === 0) {
      rovingKeyRef.current = null;
      return;
    }
    const current = rovingKeyRef.current;
    const stillVisible =
      current !== null && items.some((el) => el.dataset.rowKey === current);
    setRoving(stillVisible ? current : (items[0].dataset.rowKey ?? null));
  });

  const focusRow = (el: HTMLElement) => {
    setRoving(el.dataset.rowKey ?? null);
    el.focus();
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    // 行内的 ⋯ 触发器自己吃掉了 ArrowDown / Enter（用来开菜单），这些按键不该
    // 再被当成列表导航。
    if (event.defaultPrevented) return;
    const row = (event.target as HTMLElement).closest<HTMLElement>(
      ROW_SELECTOR,
    );
    if (row === null) return;
    const items = rowsOf();
    const index = items.indexOf(row);
    if (index < 0) return;

    const actions = actionsByKey.current.get(row.dataset.rowKey ?? "")?.current;
    // 展开态与层级都从行自己的 ARIA 属性读：读屏听到的和键盘走到的因此不可能
    // 各说各话。文件行没有 aria-expanded（null），也就没有展开收起可言。
    const expanded = row.getAttribute("aria-expanded");
    const level = Number(row.getAttribute("aria-level") ?? "1");

    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        if (index + 1 < items.length) focusRow(items[index + 1]);
        return;
      case "ArrowUp":
        event.preventDefault();
        if (index > 0) focusRow(items[index - 1]);
        return;
      case "Home":
        event.preventDefault();
        focusRow(items[0]);
        return;
      case "End":
        event.preventDefault();
        focusRow(items[items.length - 1]);
        return;
      case "ArrowRight": {
        if (variant !== "tree") return;
        event.preventDefault();
        // 已收起 → 展开（懒加载的那一层就在 toggle 里被拉起来），焦点留在原地；
        // 已展开 → 移入首个子项，也就是紧跟其后、层级更深的那一行。子项还没到
        // 达时后面跟着的是同级或更浅的行，什么也不做，等它到了再按一次。
        if (expanded === "false") actions?.toggle?.();
        else if (expanded === "true") {
          const next = items[index + 1];
          if (next && Number(next.getAttribute("aria-level") ?? "1") > level) {
            focusRow(next);
          }
        }
        return;
      }
      case "ArrowLeft": {
        if (variant !== "tree") return;
        event.preventDefault();
        if (expanded === "true") {
          actions?.toggle?.();
          return;
        }
        // 已收起的目录行与文件行：回到父行 = 往回找第一个层级更浅的行。
        for (let i = index - 1; i >= 0; i--) {
          if (Number(items[i].getAttribute("aria-level") ?? "1") < level) {
            focusRow(items[i]);
            return;
          }
        }
        return;
      }
      case "Enter":
        // preventDefault 顺带挡掉「焦点落在行内主按钮上（鼠标点过）时 Enter 的
        // 默认激活」：不然那次 click 会和这里各触发一遍，目录行的展开收起互相
        // 抵消，看着像 Enter 没反应。
        event.preventDefault();
        if (actions?.toggle) actions.toggle();
        else actions?.activate?.();
        return;
      case "F10":
        if (!event.shiftKey) return;
        event.preventDefault();
        actions?.openMenu?.();
        return;
      case "ContextMenu":
        event.preventDefault();
        actions?.openMenu?.();
        return;
      default:
        return;
    }
  };

  return (
    <SidebarListContext.Provider value={context}>
      <div
        ref={rootRef}
        role={variant === "tree" ? "tree" : "listbox"}
        aria-label={label}
        className={className}
        onKeyDown={handleKeyDown}
        // 鼠标点过某一行之后，Tab 出去再回来应该落回那一行，而不是跳回首行。
        onFocus={(event) => {
          const row = (event.target as HTMLElement).closest<HTMLElement>(
            ROW_SELECTOR,
          );
          if (row) setRoving(row.dataset.rowKey ?? null);
        }}
      >
        {children}
      </div>
    </SidebarListContext.Provider>
  );
}

/**
 * useSidebarListRow 把一行接进它所在的列表：登记键盘动作，并返回该行要落在 DOM
 * 上的 role 与身份标记。不在 SidebarList 里渲染时返回 null（行照旧是纯 div），
 * 单独渲染一行的测试与调用方因此不受影响。
 */
export function useSidebarListRow(
  rowKey: string,
  actions: SidebarRowActions,
): { role: "treeitem" | "option"; "data-row-key": string } | null {
  const context = React.useContext(SidebarListContext);
  const actionsRef = React.useRef(actions);
  React.useEffect(() => {
    actionsRef.current = actions;
  });

  const register = context?.register;
  React.useEffect(() => {
    if (!register) return;
    return register(rowKey, actionsRef);
  }, [register, rowKey]);

  if (context === null) return null;
  return { role: context.itemRole, "data-row-key": rowKey };
}
