import * as React from "react";

// useVisibleMessageId 在 transcript 滚动容器上挂 IntersectionObserver，
// 实时算出"当前用户视野焦点对应的消息 id"，供右侧 outline 高亮联动。
//
// 契约：
//   - root = containerRef.current；rootMargin 把命中区裁到容器顶部约 1/3 范围，
//     模拟"读到这条"的直觉位置；
//   - 同一时刻多条命中 → 取 DOM 顺序最靠前的那条（最贴近顶部）；
//   - 容器内 article 增删 → MutationObserver 兜底重订阅；
//   - 容器 ref 为 null 或环境不支持 IntersectionObserver → 返回 null，
//     不报错也不订阅，便于 SSR / 测试。
export function useVisibleMessageId(
  containerRef: React.RefObject<HTMLElement | null>,
): number | null {
  const [active, setActive] = React.useState<number | null>(null);

  React.useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    if (typeof IntersectionObserver === "undefined") return;

    const intersecting = new Set<Element>();

    const recompute = () => {
      const rows = container.querySelectorAll<HTMLElement>("[data-message-id]");
      let next: number | null = null;
      for (const row of rows) {
        if (!intersecting.has(row)) continue;
        const v = Number(row.getAttribute("data-message-id"));
        if (Number.isFinite(v) && v > 0) {
          next = v;
          break;
        }
      }
      setActive((prev) => (prev === next ? prev : next));
    };

    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) intersecting.add(e.target);
          else intersecting.delete(e.target);
        }
        recompute();
      },
      {
        root: container,
        rootMargin: "0px 0px -66% 0px",
        threshold: [0, 0.01],
      },
    );

    const observed = new WeakSet<Element>();
    const sync = () => {
      const rows = container.querySelectorAll<HTMLElement>("[data-message-id]");
      for (const row of rows) {
        if (!observed.has(row)) {
          observer.observe(row);
          observed.add(row);
        }
      }
      for (const el of intersecting) {
        if (!container.contains(el)) intersecting.delete(el);
      }
      recompute();
    };
    sync();

    const mut = new MutationObserver(() => sync());
    mut.observe(container, { childList: true, subtree: true });

    return () => {
      observer.disconnect();
      mut.disconnect();
    };
  }, [containerRef]);

  return active;
}
