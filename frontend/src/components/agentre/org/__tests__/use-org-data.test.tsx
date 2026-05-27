import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useOrgData } from "../use-org-data";

// App.js delegates to window.go.app.App at runtime, so we patch that directly.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).window = globalThis;

type AnyFn = (...args: unknown[]) => unknown;

type AppMockShape = {
  LoadOrg: AnyFn;
  ListAgentBackends: AnyFn;
  CreateDepartment: AnyFn;
  UpdateDepartment: AnyFn;
  MoveDepartment: AnyFn;
  DeleteDepartment: AnyFn;
  CreateAgent: AnyFn;
  UpdateAgent: AnyFn;
  MoveAgent: AnyFn;
  DeleteAgent: AnyFn;
};

let appMock: AppMockShape;

function installAppMock(overrides: Partial<AppMockShape> = {}) {
  const base: AppMockShape = {
    LoadOrg: vi.fn(() => Promise.resolve({ departments: [], agents: [] })),
    ListAgentBackends: vi.fn(() => Promise.resolve({ items: [] })),
    CreateDepartment: vi.fn(() => Promise.resolve({ item: {} })),
    UpdateDepartment: vi.fn(() => Promise.resolve({ item: {} })),
    MoveDepartment: vi.fn(() => Promise.resolve({ item: {} })),
    DeleteDepartment: vi.fn(() => Promise.resolve({})),
    CreateAgent: vi.fn(() => Promise.resolve({ item: {} })),
    UpdateAgent: vi.fn(() => Promise.resolve({ item: {} })),
    MoveAgent: vi.fn(() => Promise.resolve({ item: {} })),
    DeleteAgent: vi.fn(() => Promise.resolve({})),
  };
  appMock = { ...base, ...overrides };
  Object.defineProperty(window, "go", {
    configurable: true,
    value: { app: { App: appMock } },
  });
  return appMock;
}

beforeEach(() => {
  installAppMock();
});

afterEach(() => {
  Reflect.deleteProperty(window, "go");
  vi.resetAllMocks();
});

describe("useOrgData", () => {
  it("loads org tree on mount", async () => {
    installAppMock({
      LoadOrg: vi.fn(() =>
        Promise.resolve({
          departments: [{ id: 1, name: "x", parentId: 0 } as never],
          agents: [{ id: 1, name: "CEO" } as never],
        }),
      ),
    });

    const { result } = renderHook(() => useOrgData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.departments).toHaveLength(1);
    expect(result.current.agents).toHaveLength(1);
  });

  it("captures error and stops loading", async () => {
    installAppMock({
      LoadOrg: vi.fn(() => Promise.reject(new Error("boom"))),
    });

    const { result } = renderHook(() => useOrgData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe("boom");
  });

  it("reloads after a mutation", async () => {
    installAppMock({
      LoadOrg: vi.fn(() => Promise.resolve({ departments: [], agents: [] })),
      MoveAgent: vi.fn(() => Promise.resolve({ item: { id: 1 } as never })),
    });

    const { result } = renderHook(() => useOrgData());
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.moveAgent({
        id: 1,
        newDepartmentId: 2,
        newParentAgentId: 0,
        newSortOrder: 0,
      });
    });

    expect(vi.mocked(appMock.LoadOrg)).toHaveBeenCalledTimes(2);
  });
});
