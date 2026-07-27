import { describe, expect, it } from "vitest";

import {
  findProjectColorToken,
  findProjectPath,
  projectChain,
} from "../project-chain";
import type { app } from "../../../wailsjs/go/models";

type Node = app.ProjectTreeNode;

function makeNode(
  id: number,
  name: string,
  color: string,
  children: Node[] = [],
  path = "",
): Node {
  return {
    project: {
      id,
      name,
      color,
      parentID: 0,
      icon: "",
      description: "",
      path,
      isGitRepo: false,
      createtime: 0,
      updatetime: 0,
    } as app.ProjectItem,
    children,
  } as Node;
}

describe("projectChain", () => {
  const tree: Node[] = [
    makeNode(1, "Agentre", "agent-1", [
      makeNode(2, "backend", "agent-2", [
        makeNode(3, "db migrations", "agent-3"),
      ]),
      makeNode(4, "frontend", "agent-4"),
    ]),
    makeNode(5, "Other", "agent-5"),
  ];

  it("找到节点时返回 root → target 的 name 链", () => {
    expect(projectChain(tree, 3)).toEqual([
      "Agentre",
      "backend",
      "db migrations",
    ]);
    expect(projectChain(tree, 4)).toEqual(["Agentre", "frontend"]);
    expect(projectChain(tree, 5)).toEqual(["Other"]);
  });

  it("找不到返回空数组", () => {
    expect(projectChain(tree, 999)).toEqual([]);
  });

  it("空树返回空数组", () => {
    expect(projectChain([], 1)).toEqual([]);
  });
});

describe("findProjectColorToken", () => {
  const tree: Node[] = [
    makeNode(1, "Agentre", "agent-1", [makeNode(2, "backend", "agent-2")]),
    makeNode(3, "NoColor", ""),
  ];

  it("返回目标项目的 color token", () => {
    expect(findProjectColorToken(tree, 1)).toBe("agent-1");
    expect(findProjectColorToken(tree, 2)).toBe("agent-2");
  });

  it("color 为空时返回 null", () => {
    expect(findProjectColorToken(tree, 3)).toBeNull();
  });

  it("找不到返回 null", () => {
    expect(findProjectColorToken(tree, 999)).toBeNull();
  });
});

describe("findProjectPath", () => {
  const tree: Node[] = [
    makeNode(1, "Agentre", "agent-1", [
      makeNode(2, "frontend", "agent-2", [], "/tmp/agentre/frontend"),
    ]),
  ];

  it("Given a nested project, When resolving its composer cwd, Then its configured path is returned", () => {
    expect(findProjectPath(tree, 2)).toBe("/tmp/agentre/frontend");
  });

  it("Given an unknown project or an empty path, When resolving cwd, Then discovery falls back to the global scope", () => {
    expect(findProjectPath(tree, 1)).toBe("");
    expect(findProjectPath(tree, 999)).toBe("");
  });
});
