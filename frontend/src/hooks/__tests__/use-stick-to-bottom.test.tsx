import { act, render } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it } from "vitest";

import { isAtBottom, useStickToBottom } from "../use-stick-to-bottom";

describe("isAtBottom", () => {
  it("贴底(在容差内)判为 true", () => {
    expect(
      isAtBottom({ scrollTop: 480, scrollHeight: 1000, clientHeight: 500 }),
    ).toBe(true); // 1000-480-500 = 20 <= 32
  });

  it("远离底部判为 false", () => {
    expect(
      isAtBottom({ scrollTop: 0, scrollHeight: 1000, clientHeight: 500 }),
    ).toBe(false); // 1000-0-500 = 500 > 32
  });
});

// 测试组件：把 hook 的 ref/onScroll 挂到一个 div，并暴露 atBottom / scrollToBottom。
function Harness({ dep }: { dep: number }) {
  const { ref, atBottom, scrollToBottom, onScroll } = useStickToBottom(dep);
  return (
    <div>
      <div
        data-testid="scroller"
        ref={ref as React.Ref<HTMLDivElement>}
        onScroll={onScroll}
      />
      <span data-testid="at-bottom">{String(atBottom)}</span>
      <button type="button" onClick={scrollToBottom}>
        to-bottom
      </button>
    </div>
  );
}

function setDims(
  el: HTMLElement,
  dims: { scrollTop: number; scrollHeight: number; clientHeight: number },
) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    value: dims.scrollHeight,
  });
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    value: dims.clientHeight,
  });
  el.scrollTop = dims.scrollTop;
}

describe("useStickToBottom", () => {
  it("用户上滚后 atBottom 变 false；scrollToBottom 把它拉回底部", () => {
    const { getByTestId, getByText } = render(<Harness dep={0} />);
    const scroller = getByTestId("scroller");
    setDims(scroller, { scrollTop: 0, scrollHeight: 1000, clientHeight: 500 });

    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });
    expect(getByTestId("at-bottom").textContent).toBe("false");

    act(() => {
      getByText("to-bottom").click();
    });
    expect(getByTestId("at-bottom").textContent).toBe("true");
    expect(scroller.scrollTop).toBe(1000); // 被拉到 scrollHeight
  });

  it("贴底时 dep 变化(新消息追加)自动滚到底", () => {
    const { getByTestId, rerender } = render(<Harness dep={0} />);
    const scroller = getByTestId("scroller");
    setDims(scroller, {
      scrollTop: 500,
      scrollHeight: 1000,
      clientHeight: 500,
    });

    // 内容增高后 dep 翻新
    setDims(scroller, {
      scrollTop: 500,
      scrollHeight: 2000,
      clientHeight: 500,
    });
    act(() => {
      rerender(<Harness dep={1} />);
    });
    expect(scroller.scrollTop).toBe(2000);
  });
});
