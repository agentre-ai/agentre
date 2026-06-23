import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { GroupRoster } from "./group-roster";

import type { app } from "../../../../wailsjs/go/models";

// 两个都设成 member 角色,保证渲染顺序 = 数组顺序(host 会被分到上面一组)。
function members(): app.GroupMemberItem[] {
  return [
    {
      id: 1,
      agentID: 2,
      role: "member",
      status: "active",
      backingSessionID: 11,
      runState: "running",
    },
    {
      id: 2,
      agentID: 3,
      role: "member",
      status: "active",
      backingSessionID: 12,
      runState: "idle",
    },
  ] as unknown as app.GroupMemberItem[];
}

describe("GroupRoster status dot", () => {
  it("colors the dot by runState, not membership status", () => {
    const { container } = render(
      <GroupRoster
        members={members()}
        memberName={(id) => `M${id}`}
        onOpenMember={() => {}}
        onInvite={() => {}}
        onDelete={() => {}}
        tasks={[]}
        onAnchorTask={vi.fn()}
        onOpenMemberById={vi.fn()}
      />,
    );
    // 状态点是 size-1.5 的圆点(头像是更大的圆,不会命中)。
    const dots = container.querySelectorAll(".size-1\\.5");
    expect(dots).toHaveLength(2);
    // 在跑成员 → running 色;空转成员 → idle 色(即便 membership 都是 active)。
    expect(dots[0].classList.contains("bg-status-running")).toBe(true);
    expect(dots[1].classList.contains("bg-status-idle")).toBe(true);
  });
});

describe("GroupRoster project", () => {
  function openSettings() {
    fireEvent.click(screen.getByText(/Settings|设置/));
  }

  it("renders the bound project name as a button that jumps via onOpenProject(projectId)", () => {
    const onOpenProject = vi.fn();
    render(
      <GroupRoster
        members={members()}
        memberName={(id) => `M${id}`}
        projectID={7}
        projectName="Agentre-desktop"
        onOpenProject={onOpenProject}
        onOpenMember={() => {}}
        onInvite={() => {}}
        onDelete={() => {}}
        tasks={[]}
        onAnchorTask={vi.fn()}
        onOpenMemberById={vi.fn()}
      />,
    );
    openSettings();
    const link = screen.getByRole("button", { name: "Agentre-desktop" });
    fireEvent.click(link);
    expect(onOpenProject).toHaveBeenCalledWith(7);
  });

  it("shows the no-project fallback (not clickable) when the group has no project", () => {
    const onOpenProject = vi.fn();
    render(
      <GroupRoster
        members={members()}
        memberName={(id) => `M${id}`}
        projectID={0}
        onOpenProject={onOpenProject}
        onOpenMember={() => {}}
        onInvite={() => {}}
        onDelete={() => {}}
        tasks={[]}
        onAnchorTask={vi.fn()}
        onOpenMemberById={vi.fn()}
      />,
    );
    openSettings();
    expect(screen.getByText(/No project|未绑定项目/)).toBeInTheDocument();
    // 无项目时不渲染可点击的项目按钮(只剩删除按钮)。
    expect(
      screen.queryByRole("button", { name: "Agentre-desktop" }),
    ).not.toBeInTheDocument();
  });
});

describe("GroupRoster nickname editor", () => {
  function renderRoster(onSetMemberNickname = vi.fn()) {
    render(
      <GroupRoster
        members={members()}
        // member 1 的有效名=前端工程师(已设昵称),member 2 的有效名=Codex。
        memberName={(id) => (id === 1 ? "前端工程师" : "Codex")}
        agentNameOf={(m) => (m.agentID === 3 ? "Codex" : "Claude Code")}
        onSetMemberNickname={onSetMemberNickname}
        onOpenMember={() => {}}
        onInvite={() => {}}
        onDelete={() => {}}
        tasks={[]}
        onAnchorTask={vi.fn()}
        onOpenMemberById={vi.fn()}
      />,
    );
    return onSetMemberNickname;
  }

  it("saves a trimmed nickname via onSetMemberNickname(memberId, value)", () => {
    const onSet = renderRoster();
    // 打开 member 2(Codex)的群昵称编辑器。
    fireEvent.click(screen.getAllByTestId("member-nickname-edit")[1]);
    fireEvent.change(screen.getByTestId("member-nickname-input"), {
      target: { value: "  后端工程师  " },
    });
    fireEvent.click(screen.getByTestId("member-nickname-save"));
    expect(onSet).toHaveBeenCalledWith(2, "后端工程师");
  });

  it("blocks a nickname colliding with another member's effective name", () => {
    const onSet = renderRoster();
    fireEvent.click(screen.getAllByTestId("member-nickname-edit")[1]);
    fireEvent.change(screen.getByTestId("member-nickname-input"), {
      target: { value: "前端工程师" }, // = member 1 的有效名
    });
    expect(screen.getByTestId("member-nickname-error")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("member-nickname-save"));
    expect(onSet).not.toHaveBeenCalled();
  });
});

describe("GroupRoster delete", () => {
  function openDeleteDialog(onDelete: (deleteSessions: boolean) => void) {
    render(
      <GroupRoster
        members={members()}
        memberName={(id) => `M${id}`}
        onOpenMember={() => {}}
        onInvite={() => {}}
        onDelete={onDelete}
        tasks={[]}
        onAnchorTask={vi.fn()}
        onOpenMemberById={vi.fn()}
      />,
    );
    // 删除入口在 Settings tab。
    fireEvent.click(screen.getByText(/Settings|设置/));
    fireEvent.click(
      screen.getByRole("button", { name: /Delete group|删除群/ }),
    );
  }

  it("confirming without checking the box keeps sessions → onDelete(false)", () => {
    const onDelete = vi.fn();
    openDeleteDialog(onDelete);
    // 不勾选直接确认。
    fireEvent.click(screen.getByRole("button", { name: /^Delete$|^删除$/ }));
    expect(onDelete).toHaveBeenCalledWith(false);
  });

  it("checking the box then confirming → onDelete(true)", () => {
    const onDelete = vi.fn();
    openDeleteDialog(onDelete);
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /^Delete$|^删除$/ }));
    expect(onDelete).toHaveBeenCalledWith(true);
  });
});
