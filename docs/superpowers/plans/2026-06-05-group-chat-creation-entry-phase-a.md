# 群聊创建入口 Phase A 实现计划（创建能力 + 弹窗 + 入口）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让用户能从会话侧栏头部 `+` 菜单创建群聊（标题 + 协调者 + 项目 + 初始成员），后端原子建群带成员，候选 agent 按能力门控过滤。

**Architecture:** 后端在 `chat_svc.ChatAgents` 派生 `SupportsGroup`（能力探针，OCP）；`group_svc.CreateGroup` 接受 `MemberAgentIDs[]` 原子加入；前端新增 `group-new-dialog` + 可复用 `agent-multi-picker`，入口挂在 `chat-page` 头部 `+` dropdown 菜单。department 派生留到 Phase C（group_invite 用）。

**Tech Stack:** Go 1.26（cago / goconvey / gomock）、Wails v2 bindings、React 19 / TS / shadcn `@/components/ui/*` / zustand / react-i18next / Vitest。

**关联 spec:** `docs/superpowers/specs/2026-06-05-group-chat-creation-entry-design.md`（§4.1 / §4.2 / §4.3）

---

## 文件结构

- `internal/service/chat_svc/types.go` — `ChatAgentItem` 加 `SupportsGroup` 字段。
- `internal/service/chat_svc/chat.go` — 抽 `backendSupportsGroup(be)` helper + 在 `ChatAgents` 组装处赋值。
- `internal/service/chat_svc/chat_test.go`（或新 `supports_group_test.go`）— helper 单测。
- `internal/service/group_svc/types.go` — `CreateGroupRequest` 加 `MemberAgentIDs`。
- `internal/service/group_svc/group.go` — `CreateGroup` 追加成员循环。
- `internal/service/group_svc/group_test.go` — 新增「建群带成员」用例。
- `internal/app/group.go` — `GroupCreateRequest` 加 `MemberAgentIDs` 透传。
- `frontend/src/components/agentre/group-chat/agent-multi-picker.tsx`（新）+ `.test.tsx`
- `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`（新）+ `.test.tsx`
- `frontend/src/components/agentre/chat-page.tsx` — 头部 `+` dropdown 菜单 + 挂弹窗。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `group.new.*` + `sidebar.add.*`。

---

## Task 1: 后端能力门控字段 `SupportsGroup`

**Files:**
- Modify: `internal/service/chat_svc/types.go`（`ChatAgentItem` 结构体）
- Modify: `internal/service/chat_svc/chat.go`（`ChatAgents` 组装 + 新 helper）
- Test: `internal/service/chat_svc/supports_group_test.go`（新）

- [ ] **Step 1: 写失败测试**

Create `internal/service/chat_svc/supports_group_test.go`：

```go
package chat_svc

import (
	"testing"

	"agentre/internal/model/entity/agent_backend_entity"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBackendSupportsGroup(t *testing.T) {
	Convey("backendSupportsGroup: claudecode 声明 CapMCPTools → true", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
		So(backendSupportsGroup(be), ShouldBeTrue)
	})
	Convey("backendSupportsGroup: codex 不声明 CapMCPTools → false", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex)}
		So(backendSupportsGroup(be), ShouldBeFalse)
	})
	Convey("backendSupportsGroup: nil backend → false", t, func() {
		So(backendSupportsGroup(nil), ShouldBeFalse)
	})
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test -race -run TestBackendSupportsGroup ./internal/service/chat_svc/...`
Expected: FAIL — `undefined: backendSupportsGroup`

- [ ] **Step 3: 实现 helper + 字段**

`internal/service/chat_svc/types.go` 在 `ChatAgentItem` 的 `Pinned` 字段后加：

```go
	Pinned                bool              `json:"pinned"`
	// SupportsGroup 报告该 agent 的后端是否声明 CapMCPTools（可作为群聊协调者/成员）。
	// MVP 仅 claudecode 为 true。前端「新建群聊」picker 用它过滤候选（OCP，不写 backendType 字面量）。
	SupportsGroup         bool              `json:"supportsGroup"`
	ChattableHint         string            `json:"chattableHint"`
```

`internal/service/chat_svc/chat.go` 文件末尾加 helper（`agentruntime` / `capability` 已被本包导入，见文件顶部 import）：

```go
// backendSupportsGroup 报告某后端 runtime 是否声明 CapMCPTools（群聊资格）。nil → false。
func backendSupportsGroup(be *agent_backend_entity.AgentBackend) bool {
	if be == nil {
		return false
	}
	return agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type)).
		Capabilities().Has(capability.CapMCPTools)
}
```

在 `ChatAgents` 组装处（`if be := backends[a.AgentBackendID]; be != nil {` 块内，紧接 `item.BackendType = be.Type` 之后）加：

```go
				item.BackendType = be.Type
				item.SupportsGroup = backendSupportsGroup(be)
```

- [ ] **Step 4: 跑测试看它通过**

Run: `go test -race -run TestBackendSupportsGroup ./internal/service/chat_svc/...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/chat_svc/types.go internal/service/chat_svc/chat.go internal/service/chat_svc/supports_group_test.go
git commit -m "✨ chat_svc: ChatAgentItem 派生 SupportsGroup 能力门控字段"
```

---

## Task 2: 后端建群带初始成员

**Files:**
- Modify: `internal/service/group_svc/types.go`（`CreateGroupRequest`）
- Modify: `internal/service/group_svc/group.go`（`CreateGroup`）
- Test: `internal/service/group_svc/group_test.go`（新增用例）

> 现有 `TestGroupSvc_CreateGroup_AddsCoordinatorMember` 不传 `MemberAgentIDs` → 成员循环跳过，保持绿。

- [ ] **Step 1: 写失败测试**

在 `internal/service/group_svc/group_test.go` 末尾追加（import 已有 `gomock` / `mock_group_svc` / `mock_group_repo` / `group_repo` / `group_entity` / `capability`）：

```go
func TestGroupSvc_CreateGroup_AddsInitialMembers(t *testing.T) {
	Convey("建群带初始成员：协调者 + 每个成员都建 member", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		// 协调者(1) + 成员(2) 都过能力门控。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		// 协调者 ensureMember
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(1), int64(0), int64(5)).Return(int64(11), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleCoordinator)
				return nil
			})
		// 成员(2) ensureMember
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(2)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(2), int64(0), int64(5)).Return(int64(12), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleMember)
				return nil
			})
		// LoadGroup tail
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "支付小队", CoordinatorAgentID: 1, MemberAgentIDs: []int64{2},
		})
		So(err, ShouldBeNil)
	})

	Convey("成员后端不支持群聊 → GroupBackendUnsupported", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(1), int64(0), int64(5)).Return(int64(11), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		// 成员(7) 门控失败
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(7), capability.CapMCPTools).Return(false, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "X", CoordinatorAgentID: 1, MemberAgentIDs: []int64{7},
		})
		So(err, ShouldNotBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test -race -run TestGroupSvc_CreateGroup_AddsInitialMembers ./internal/service/group_svc/...`
Expected: FAIL — `unknown field 'MemberAgentIDs'`（编译错误）

- [ ] **Step 3: 实现字段 + 成员循环**

`internal/service/group_svc/types.go` 的 `CreateGroupRequest` 加字段：

```go
type CreateGroupRequest struct {
	Title              string
	CoordinatorAgentID int64
	DepartmentID       int64
	ProjectID          int64
	// MemberAgentIDs 建群时一并拉入的初始成员（协调者之外）。每个都经 backendSupportsGroup
	// 门控 + maxMembers 上限；幂等（ensureMember）。
	MemberAgentIDs     []int64
}
```

`internal/service/group_svc/group.go` 的 `CreateGroup`，在 `ensureMember(coordinator)` 成功之后、`logger.Ctx(...)` 日志之前插入：

```go
	if _, err := s.ensureMember(ctx, g, req.CoordinatorAgentID, group_entity.RoleCoordinator); err != nil {
		return nil, err
	}
	for _, agentID := range req.MemberAgentIDs {
		if agentID == req.CoordinatorAgentID {
			continue
		}
		if !s.backendSupportsGroup(ctx, agentID) {
			return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
		}
		if _, err := s.ensureMember(ctx, g, agentID, group_entity.RoleMember); err != nil {
			return nil, err
		}
	}
	logger.Ctx(ctx).Info("group_svc.CreateGroup: created",
```

- [ ] **Step 4: 跑测试看它通过**

Run: `go test -race -run 'TestGroupSvc_CreateGroup' ./internal/service/group_svc/...`
Expected: PASS（新用例 + 原 `AddsCoordinatorMember` 都绿）

- [ ] **Step 5: 提交**

```bash
git add internal/service/group_svc/types.go internal/service/group_svc/group.go internal/service/group_svc/group_test.go
git commit -m "✨ group_svc: CreateGroup 支持初始成员(MemberAgentIDs, 原子加入+能力门控)"
```

---

## Task 3: Wails 绑定透传 + 重新生成

**Files:**
- Modify: `internal/app/group.go`（`GroupCreateRequest` + `GroupCreate`）

- [ ] **Step 1: 加字段 + 透传**

`internal/app/group.go` 的 `GroupCreateRequest` 加 `memberAgentIDs`：

```go
type GroupCreateRequest struct {
	Title              string  `json:"title"`
	CoordinatorAgentID int64   `json:"coordinatorAgentID"`
	DepartmentID       int64   `json:"departmentID"`
	ProjectID          int64   `json:"projectID"`
	MemberAgentIDs     []int64 `json:"memberAgentIDs"`
}
```

`GroupCreate` 方法体里把字段带上：

```go
func (a *App) GroupCreate(req *GroupCreateRequest) (*GroupDetailResponse, error) {
	d, err := group_svc.Default().CreateGroup(a.ctx, &group_svc.CreateGroupRequest{
		Title:              req.Title,
		CoordinatorAgentID: req.CoordinatorAgentID,
		DepartmentID:       req.DepartmentID,
		ProjectID:          req.ProjectID,
		MemberAgentIDs:     req.MemberAgentIDs,
	})
	if err != nil {
		return nil, err
	}
	return toGroupDetail(d), nil
}
```

- [ ] **Step 2: 验证后端编译 + 重新生成绑定**

Run: `go build ./... && make generate`
Expected: 无错误；`frontend/wailsjs/go/models.ts` 的 `GroupCreateRequest` 出现 `memberAgentIDs`，`ChatAgentItem` 出现 `supportsGroup`。

- [ ] **Step 3: 提交**

```bash
git add internal/app/group.go frontend/wailsjs/
git commit -m "✨ app: GroupCreate 透传 memberAgentIDs + 刷新 wails 绑定"
```

---

## Task 4: 前端可复用 `AgentMultiPicker`

**Files:**
- Create: `frontend/src/components/agentre/group-chat/agent-multi-picker.tsx`
- Test: `frontend/src/components/agentre/group-chat/agent-multi-picker.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `frontend/src/components/agentre/group-chat/agent-multi-picker.test.tsx`：

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AgentMultiPicker, type PickableAgent } from "./agent-multi-picker";

const agents: PickableAgent[] = [
  { id: 1, name: "云溪", avatarColor: "agent-1", avatarIcon: "", avatarDataUrl: "" },
  { id: 2, name: "影狼", avatarColor: "agent-2", avatarIcon: "", avatarDataUrl: "" },
  { id: 3, name: "石川", avatarColor: "agent-3", avatarIcon: "", avatarDataUrl: "" },
];

describe("AgentMultiPicker", () => {
  it("点候选把 agent 加入 value（onChange 收到新 id 列表）", () => {
    const onChange = vi.fn();
    render(<AgentMultiPicker agents={agents} value={[]} onChange={onChange} />);
    fireEvent.click(screen.getByText("添加成员"));
    fireEvent.click(screen.getByText("影狼"));
    expect(onChange).toHaveBeenCalledWith([2]);
  });

  it("exclude 的 agent 不出现在候选里", () => {
    render(
      <AgentMultiPicker agents={agents} value={[]} onChange={() => {}} exclude={[1]} />,
    );
    fireEvent.click(screen.getByText("添加成员"));
    expect(screen.queryByText("云溪")).toBeNull();
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/agent-multi-picker.test.tsx`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现组件**

Create `frontend/src/components/agentre/group-chat/agent-multi-picker.tsx`：

```tsx
import * as React from "react";
import { Plus, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "../primitives";
import type { AgentColor } from "../types";

export type PickableAgent = {
  id: number;
  name: string;
  avatarColor: string;
  avatarIcon: string;
  avatarDataUrl: string;
};

export type AgentMultiPickerProps = {
  agents: PickableAgent[];
  value: number[];
  onChange: (next: number[]) => void;
  exclude?: number[];
};

function AgentMultiPicker({ agents, value, onChange, exclude = [] }: AgentMultiPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);
  const selected = agents.filter((a) => value.includes(a.id));
  const candidates = agents.filter(
    (a) => !value.includes(a.id) && !exclude.includes(a.id),
  );

  const add = (id: number) => {
    onChange([...value, id]);
    setOpen(false);
  };
  const remove = (id: number) => onChange(value.filter((v) => v !== id));

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {selected.map((a) => (
        <span
          key={a.id}
          className="inline-flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-xs"
        >
          <AgentAvatar
            name={a.name}
            initials={a.name.charAt(0)}
            color={(a.avatarColor as AgentColor) || "agent-1"}
            avatarIcon={a.avatarIcon || undefined}
            avatarDataUrl={a.avatarDataUrl || undefined}
            size="md"
            className="size-4 rounded-sm text-[8px]"
          />
          <span className="font-medium">{a.name}</span>
          <button
            type="button"
            aria-label={t("group.new.removeMember", { name: a.name })}
            onClick={() => remove(a.id)}
            className="text-muted-foreground hover:text-foreground"
          >
            <X className="size-3" aria-hidden="true" />
          </button>
        </span>
      ))}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <Plus className="size-3" aria-hidden="true" />
            {t("group.new.addMember")}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 p-1">
          {candidates.length === 0 ? (
            <div className="px-2 py-3 text-center text-2xs text-muted-foreground">
              {t("group.new.noCandidates")}
            </div>
          ) : (
            candidates.map((a) => (
              <button
                key={a.id}
                type="button"
                onClick={() => add(a.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs",
                  "hover:bg-accent",
                )}
              >
                <AgentAvatar
                  name={a.name}
                  initials={a.name.charAt(0)}
                  color={(a.avatarColor as AgentColor) || "agent-1"}
                  avatarIcon={a.avatarIcon || undefined}
                  avatarDataUrl={a.avatarDataUrl || undefined}
                  size="md"
                  className="size-5 rounded-sm text-[10px]"
                />
                <span className="font-medium">{a.name}</span>
              </button>
            ))
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}

export { AgentMultiPicker };
```

> 已核对：`AgentAvatar` 由 `frontend/src/components/agentre/primitives.tsx` 导出；本文件在 `group-chat/` 下，相对路径 `../primitives` 正确（同 `command-palette/sources/new-chat-source.tsx` 用法）。

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/agent-multi-picker.test.tsx`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/group-chat/agent-multi-picker.tsx frontend/src/components/agentre/group-chat/agent-multi-picker.test.tsx
git commit -m "✨ group(fe): 可复用 AgentMultiPicker(avatar chip + 候选 + exclude)"
```

---

## Task 5: 前端 New Group 弹窗

**Files:**
- Create: `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`
- Test: `frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx`：

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GroupNewDialog } from "./group-new-dialog";

const groupCreate = vi.fn();
const groupListReload = vi.fn();
const openGroup = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupCreate: (...a: unknown[]) => groupCreate(...a),
}));
vi.mock("@/hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [
      { id: 1, name: "云溪", avatarColor: "agent-1", avatarIcon: "", avatarDataUrl: "", chattable: true, supportsGroup: true },
      { id: 9, name: "Codex君", avatarColor: "agent-2", avatarIcon: "", avatarDataUrl: "", chattable: true, supportsGroup: false },
    ],
    loading: false,
  }),
}));
vi.mock("@/hooks/use-project-list", () => ({
  useProjectList: () => ({ projects: [{ id: 3, name: "Agentre" }], reload: vi.fn() }),
}));
vi.mock("@/stores/group-list-store", () => ({
  useGroupListStore: { getState: () => ({ reload: groupListReload }) },
}));
vi.mock("@/stores/chat-tabs-store", () => ({
  useChatTabsStore: { getState: () => ({ openGroup }) },
}));

describe("GroupNewDialog", () => {
  beforeEach(() => {
    groupCreate.mockReset().mockResolvedValue({ group: { id: 5, title: "新群" } });
    groupListReload.mockReset();
    openGroup.mockReset();
  });

  it("不支持群聊的 agent 不在协调者候选里", () => {
    render(<GroupNewDialog open onOpenChange={() => {}} />);
    fireEvent.click(screen.getByText("选择协调者"));
    expect(screen.queryByText("Codex君")).toBeNull();
    expect(screen.getByText("云溪")).toBeTruthy();
  });

  it("填标题 + 选协调者 → 提交调 GroupCreate 并打开群 tab", async () => {
    render(<GroupNewDialog open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByLabelText("群标题"), { target: { value: "支付小队" } });
    fireEvent.click(screen.getByText("选择协调者"));
    fireEvent.click(screen.getByText("云溪"));
    fireEvent.click(screen.getByText("创建群聊"));
    await waitFor(() => expect(groupCreate).toHaveBeenCalled());
    expect(groupCreate.mock.calls[0][0]).toMatchObject({
      title: "支付小队",
      coordinatorAgentID: 1,
    });
    await waitFor(() => expect(openGroup).toHaveBeenCalledWith(5, "新群"));
    expect(groupListReload).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现弹窗**

Create `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`：

```tsx
import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useProjectList } from "@/hooks/use-project-list";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useGroupListStore } from "@/stores/group-list-store";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";

import { AgentMultiPicker, type PickableAgent } from "./agent-multi-picker";
import { GroupCreate } from "../../../../wailsjs/go/app/App";

export type GroupNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

function GroupNewDialog({ open, onOpenChange }: GroupNewDialogProps) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { projects } = useProjectList();
  const projectContext = useNewChatContextStore((s) => s.projectContext);

  const eligible: PickableAgent[] = React.useMemo(
    () =>
      agents
        .filter((a) => a.supportsGroup && a.chattable)
        .map((a) => ({
          id: a.id,
          name: a.name,
          avatarColor: a.avatarColor,
          avatarIcon: a.avatarIcon,
          avatarDataUrl: a.avatarDataUrl,
        })),
    [agents],
  );

  const [title, setTitle] = React.useState("");
  const [coordinatorID, setCoordinatorID] = React.useState(0);
  const [projectID, setProjectID] = React.useState(0);
  const [memberIDs, setMemberIDs] = React.useState<number[]>([]);
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // 每次打开重置 + 预填当前项目上下文。
  React.useEffect(() => {
    if (open) {
      setTitle("");
      setCoordinatorID(0);
      setProjectID(projectContext?.projectID ?? 0);
      setMemberIDs([]);
      setError(null);
    }
  }, [open, projectContext]);

  const coordinator = eligible.find((a) => a.id === coordinatorID);
  const canSubmit = title.trim().length > 0 && coordinatorID > 0 && !submitting;

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      const detail = await GroupCreate({
        title: title.trim(),
        coordinatorAgentID: coordinatorID,
        departmentID: 0,
        projectID,
        memberAgentIDs: memberIDs,
      });
      await useGroupListStore.getState().reload();
      const g = detail.group;
      if (g) useChatTabsStore.getState().openGroup(g.id, g.title);
      onOpenChange(false);
    } catch (err) {
      setError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{t("group.new.title")}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3.5">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.groupTitle")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              aria-label={t("group.new.groupTitle")}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("group.new.groupTitlePlaceholder")}
              className="h-9 text-xs"
            />
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.coordinator")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Select
              value={coordinatorID ? String(coordinatorID) : ""}
              onValueChange={(v) => setCoordinatorID(Number(v))}
            >
              <SelectTrigger className="h-9 text-xs">
                <SelectValue placeholder={t("group.new.coordinatorPlaceholder")}>
                  {coordinator?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {eligible.map((a) => (
                  <SelectItem key={a.id} value={String(a.id)}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className="text-2xs text-muted-foreground">
              {t("group.new.coordinatorHint")}
            </span>
          </label>

          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.project")}
            </span>
            <Select
              value={String(projectID)}
              onValueChange={(v) => setProjectID(Number(v))}
            >
              <SelectTrigger className="h-9 text-xs">
                <SelectValue placeholder={t("group.new.projectNone")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">{t("group.new.projectNone")}</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>

          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("group.new.members")}
            </span>
            <AgentMultiPicker
              agents={eligible}
              value={memberIDs}
              onChange={setMemberIDs}
              exclude={coordinatorID ? [coordinatorID] : []}
            />
            <span className="text-2xs text-muted-foreground">
              {t("group.new.membersHint")}
            </span>
          </div>

          {error ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
              {error}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t("common.cancel")}
          </Button>
          <Button type="button" disabled={!canSubmit} onClick={() => void submit()}>
            {submitting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            {t("group.new.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { GroupNewDialog };
```

> 已核对：`useChatAgents` 返回的 `ChatAgentItem` 在 Task 3 `make generate` 后含 `supportsGroup`；`new-chat-context-store.ProjectContext` 字段为 `{ projectID, projectName }`，故 `projectContext?.projectID ?? 0` 正确。

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/group-chat/group-new-dialog.tsx frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx
git commit -m "✨ group(fe): 新建群聊弹窗(标题+协调者+项目预填+初始成员)"
```

---

## Task 6: i18n 文案

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

- [ ] **Step 1: zh-CN 加 `group.new.*`**

`frontend/src/i18n/locales/zh-CN/common.json` 的 `"group"` 对象内追加（与现有 `group.*` 键并列）：

```json
    "new": {
      "title": "新建群聊",
      "groupTitle": "群标题",
      "groupTitlePlaceholder": "前端重构攻坚组",
      "coordinator": "协调者",
      "coordinatorPlaceholder": "选择协调者",
      "coordinatorHint": "仅支持 group_send 的 Agent（claudecode）可作为协调者",
      "project": "项目",
      "projectNone": "（不绑定项目）",
      "members": "初始成员",
      "membersHint": "协调者可在群里随时邀请更多成员加入",
      "addMember": "添加成员",
      "removeMember": "移除 {{name}}",
      "noCandidates": "没有可加入的 Agent",
      "create": "创建群聊"
    }
```

- [ ] **Step 2: en 加对应键**

`frontend/src/i18n/locales/en/common.json` 的 `"group"` 对象内追加：

```json
    "new": {
      "title": "New group",
      "groupTitle": "Group title",
      "groupTitlePlaceholder": "Frontend refactor squad",
      "coordinator": "Coordinator",
      "coordinatorPlaceholder": "Select coordinator",
      "coordinatorHint": "Only group_send-capable agents (claudecode) can coordinate",
      "project": "Project",
      "projectNone": "(No project)",
      "members": "Initial members",
      "membersHint": "The coordinator can invite more members anytime",
      "addMember": "Add member",
      "removeMember": "Remove {{name}}",
      "noCandidates": "No agents available to add",
      "create": "Create group"
    }
```

- [ ] **Step 3: 跑 i18n 测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS（zh/en key 覆盖一致）

- [ ] **Step 4: 提交**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 group(fe): 新建群聊弹窗 i18n(zh/en)"
```

---

## Task 7: 侧栏头部 `+` 菜单接入弹窗

**Files:**
- Modify: `frontend/src/components/agentre/chat-page.tsx`
- Modify: `frontend/src/components/agentre/__tests__/chat-page.test.tsx`

> Phase A 把 `+` 菜单挂在**现有**侧栏头部（`chat-page.tsx` 头部 `chatPage.agents` 标题行右侧）。Phase B 重构搜索行时由新头部承载同一菜单。

- [ ] **Step 1: 写失败测试**

在 `frontend/src/components/agentre/__tests__/chat-page.test.tsx` 增用例（沿用该文件已有的 render 工具/mock；按文件现有风格补 mock）：

```tsx
it("点头部 + → 菜单含「新建群聊」→ 打开弹窗", async () => {
  renderChatPage();
  fireEvent.click(screen.getByLabelText("新建"));
  const item = await screen.findByText("新建群聊");
  fireEvent.click(item);
  expect(screen.getByText("创建群聊")).toBeTruthy(); // 弹窗 footer 按钮
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/chat-page.test.tsx -t "新建群聊"`
Expected: FAIL — 找不到「新建」按钮。

- [ ] **Step 3: 实现 `+` dropdown + 挂弹窗**

`chat-page.tsx` 顶部加导入：

```tsx
import { Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { GroupNewDialog } from "./group-chat/group-new-dialog";
```

`ChatPage` 组件内加 state：

```tsx
  const [newGroupOpen, setNewGroupOpen] = React.useState(false);
```

在头部标题行（`{t("chatPage.agents")}` 那个 `<div className="flex items-center gap-2">`）的 `<div className="min-w-0 flex-1" />` 之后、容器闭合前插入 `+` 菜单：

```tsx
            <div className="min-w-0 flex-1" />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  aria-label={t("sidebar.add.aria")}
                  className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
                >
                  <Plus className="size-4" aria-hidden="true" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setNewGroupOpen(true)}>
                  {t("sidebar.add.newGroup")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
```

在 `ChatPage` 返回的最外层 fragment 末尾（侧栏 `</ResizableSidebar>` 之后、`</>` 之前）挂弹窗：

```tsx
        <GroupNewDialog open={newGroupOpen} onOpenChange={setNewGroupOpen} />
```

> Phase A 菜单先只放「新建群聊」一项（`新建 Agent 会话` 等 Phase B 随搜索行重构补；现有 per-agent `+新会话` 仍可用）。

- [ ] **Step 4: 加 i18n `sidebar.add.*`**

`zh-CN/common.json` 顶层加：

```json
  "sidebar": {
    "add": { "aria": "新建", "newGroup": "新建群聊" }
  },
```

`en/common.json` 顶层加：

```json
  "sidebar": {
    "add": { "aria": "New", "newGroup": "New group" }
  },
```

> 若顶层已有 `sidebar` 键，则合并 `add` 子对象，勿覆盖。

- [ ] **Step 5: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/chat-page.test.tsx`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/agentre/chat-page.tsx frontend/src/components/agentre/__tests__/chat-page.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ group(fe): 侧栏头部 + 菜单接入新建群聊弹窗"
```

---

## Task 8: 全量校验

- [ ] **Step 1: 后端**

Run: `make test-backend && make lint`
Expected: race 测试全绿；golangci-lint `0 issues`。

- [ ] **Step 2: 前端**

Run: `cd frontend && pnpm test && pnpm exec tsc --noEmit && pnpm lint`
Expected: Vitest 全绿；`tsc` 0 错；eslint 0 错（含 `i18next/no-literal-string`）。

- [ ] **Step 3: 手动冒烟（可选）**

Run: `make dev`，点侧栏头部 `+` → 新建群聊 → 选协调者（claudecode 后端 agent）+ 可选成员 → 创建 → 群 tab 打开、左侧群列表出现。

---

## Self-Review 备注（写计划时已核对）

- **Spec 覆盖**：§4.1（Task 1）、§4.2（Task 2/3，department 派生明确移至 Phase C）、§4.3（Task 4/5/7）、i18n（Task 6）。侧栏混排/筛选/置顶（§4.4）= Phase B 计划；group_invite（§4.5）= Phase C 计划。
- **类型一致**：`SupportsGroup`(Go) ↔ `supportsGroup`(TS)；`MemberAgentIDs`(Go) ↔ `memberAgentIDs`(TS)；`AgentMultiPicker` 的 `PickableAgent`/props 在 Task 4 定义、Task 5 复用一致。
- **环境依赖已核对**：①`AgentAvatar` 相对路径 `../primitives` 正确（导出自 `primitives.tsx`）；②`ProjectContext` = `{ projectID, projectName }`，预填用 `projectContext?.projectID`。
- **`+` 菜单的 aria-label**：Task 7 测试用 `getByLabelText("新建")`，对应 `sidebar.add.aria`=「新建」；实现与文案一致。
- **执行顺序约束**：Task 3 的 `make generate` 必须在前端 Task 4/5/7 之前完成（前端依赖 `supportsGroup` / `memberAgentIDs` 绑定）。
