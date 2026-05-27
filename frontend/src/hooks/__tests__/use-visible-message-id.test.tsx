import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useVisibleMessageId } from "../use-visible-message-id";

type IOCallback = (
  entries: IntersectionObserverEntry[],
  observer: IntersectionObserver,
) => void;

interface FakeIO {
  callback: IOCallback;
  observed: Set<Element>;
  root: Element | null;
  observe(el: Element): void;
  unobserve(el: Element): void;
  disconnect(): void;
}

let instances: FakeIO[] = [];

class IOMock {
  callback: IOCallback;
  observed = new Set<Element>();
  root: Element | null;
  constructor(cb: IOCallback, opts?: IntersectionObserverInit) {
    this.callback = cb;
    this.root = (opts?.root as Element | undefined) ?? null;
    instances.push(this);
  }
  observe(el: Element) {
    this.observed.add(el);
  }
  unobserve(el: Element) {
    this.observed.delete(el);
  }
  disconnect() {
    this.observed.clear();
  }
  takeRecords() {
    return [];
  }
}

function makeEntry(
  target: Element,
  isIntersecting: boolean,
): IntersectionObserverEntry {
  return { target, isIntersecting } as unknown as IntersectionObserverEntry;
}

function setupContainer(ids: number[]) {
  const container = document.createElement("section");
  document.body.appendChild(container);
  for (const id of ids) {
    const a = document.createElement("article");
    a.setAttribute("data-message-id", String(id));
    container.appendChild(a);
  }
  return container;
}

describe("useVisibleMessageId", () => {
  let originalIO: typeof IntersectionObserver | undefined;

  beforeEach(() => {
    instances = [];
    originalIO = globalThis.IntersectionObserver;
    (
      globalThis as unknown as { IntersectionObserver: unknown }
    ).IntersectionObserver = IOMock;
  });

  afterEach(() => {
    document.body.innerHTML = "";
    if (originalIO) {
      (
        globalThis as unknown as { IntersectionObserver: unknown }
      ).IntersectionObserver = originalIO;
    } else {
      delete (globalThis as unknown as { IntersectionObserver?: unknown })
        .IntersectionObserver;
    }
  });

  it("returns null when no row is intersecting", () => {
    const container = setupContainer([1, 2, 3]);
    const ref = { current: container };
    const { result } = renderHook(() => useVisibleMessageId(ref));
    expect(result.current).toBeNull();
  });

  it("returns the id of the intersecting row", () => {
    const container = setupContainer([10, 20, 30]);
    const ref = { current: container };
    const { result } = renderHook(() => useVisibleMessageId(ref));
    const io = instances[0];
    act(() => {
      io.callback(
        [makeEntry(container.children[1], true)],
        io as unknown as IntersectionObserver,
      );
    });
    expect(result.current).toBe(20);
  });

  it("picks the earliest intersecting row when several intersect", () => {
    const container = setupContainer([10, 20, 30]);
    const ref = { current: container };
    const { result } = renderHook(() => useVisibleMessageId(ref));
    const io = instances[0];
    act(() => {
      io.callback(
        [
          makeEntry(container.children[2], true),
          makeEntry(container.children[0], true),
        ],
        io as unknown as IntersectionObserver,
      );
    });
    expect(result.current).toBe(10);
  });

  it("updates when previously visible row leaves and another enters", () => {
    const container = setupContainer([10, 20]);
    const ref = { current: container };
    const { result } = renderHook(() => useVisibleMessageId(ref));
    const io = instances[0];
    act(() => {
      io.callback(
        [makeEntry(container.children[0], true)],
        io as unknown as IntersectionObserver,
      );
    });
    expect(result.current).toBe(10);
    act(() => {
      io.callback(
        [
          makeEntry(container.children[0], false),
          makeEntry(container.children[1], true),
        ],
        io as unknown as IntersectionObserver,
      );
    });
    expect(result.current).toBe(20);
  });

  it("returns null when container ref is null", () => {
    const ref = { current: null } as React.RefObject<HTMLElement | null>;
    const { result } = renderHook(() => useVisibleMessageId(ref));
    expect(result.current).toBeNull();
    expect(instances).toHaveLength(0);
  });

  it("uses container as observer root", () => {
    const container = setupContainer([1]);
    const ref = { current: container };
    renderHook(() => useVisibleMessageId(ref));
    expect(instances[0].root).toBe(container);
  });
});
