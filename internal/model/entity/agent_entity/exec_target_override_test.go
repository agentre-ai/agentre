package agent_entity

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
)

func targetsWith(ids ...int64) []*AgentExecTarget {
	out := make([]*AgentExecTarget, 0, len(ids))
	for i, id := range ids {
		out = append(out, &AgentExecTarget{ID: int64(i + 1), AgentID: 7, AgentBackendID: id, SortOrder: i})
	}
	return out
}

func backendIDs(ts []*AgentExecTarget) []int64 {
	out := make([]int64, 0, len(ts))
	for _, t := range ts {
		if t != nil {
			out = append(out, t.AgentBackendID)
		}
	}
	return out
}

// ── R14：本端有覆盖 → 用覆盖顺序 ────────────────────────────────────────────

func TestResolveExecTargetOrder_GivenOverride_ThenUsesOverrideOrder(t *testing.T) {
	convey.Convey("Given 覆盖顺序 [52, 51, 53]", t, func() {
		targets := targetsWith(51, 52, 53)
		override := []int64{52, 51, 53}

		convey.Convey("When 解析（selfBackendID 51 在场）", func() {
			resolved := ResolveExecTargetOrder(targets, override, []int64{51})

			convey.Convey("Then 覆盖顺序生效，self 不提前", func() {
				assert.Equal(t, []int64{52, 51, 53}, backendIDs(resolved))
			})
		})
	})
}

// 覆盖可能因账号级增删档而过期：覆盖里指向已删除 backend 的 id 忽略，没被覆盖
// 列到的档按默认顺序补到尾部 —— 解析必须收敛到当前档集合（R14）。
func TestResolveExecTargetOrder_GivenStaleOverride_ThenReconcilesToCurrentSet(t *testing.T) {
	convey.Convey("Given 覆盖引用已不存在的 backend 99 且漏掉当前档 53", t, func() {
		targets := targetsWith(51, 52, 53)
		override := []int64{52, 99}

		convey.Convey("When 解析", func() {
			resolved := ResolveExecTargetOrder(targets, override, nil)

			convey.Convey("Then 覆盖里的有效档在前，漏掉的档按默认顺序补尾", func() {
				assert.Equal(t, []int64{52, 51, 53}, backendIDs(resolved))
			})
		})
	})
}

// ── R14：无覆盖 + 桌面端把自己提到第一 ──────────────────────────────────────

func TestResolveExecTargetOrder_GivenNoOverride_WhenSelfInList_ThenSelfFirst(t *testing.T) {
	convey.Convey("Given 默认顺序 [51, 52] 且本端（self）是 52", t, func() {
		targets := targetsWith(51, 52)
		override := []int64(nil)

		convey.Convey("When 解析（selfBackendID=52）", func() {
			resolved := ResolveExecTargetOrder(targets, override, []int64{52})

			convey.Convey("Then 自己排到第一，其余保持默认相对顺序", func() {
				assert.Equal(t, []int64{52, 51}, backendIDs(resolved))
			})
		})
	})
}

// ── R14：浏览器语境（没有「自己」）→ 默认顺序原样 ──────────────────────────

func TestResolveExecTargetOrder_GivenNoOverride_WhenNoSelf_ThenDefaultUnchanged(t *testing.T) {
	convey.Convey("Given 默认顺序 [51, 52]（浏览器语境 selfBackendID=0）", t, func() {
		targets := targetsWith(51, 52)
		override := []int64(nil)

		convey.Convey("When 解析", func() {
			resolved := ResolveExecTargetOrder(targets, override, nil)

			convey.Convey("Then 默认顺序原样，不提前", func() {
				assert.Equal(t, []int64{51, 52}, backendIDs(resolved))
			})
		})
	})
}

// 一台机器可以有多档本机 backend（Claude Code / Codex / Pi Agent 各一档，共用一个
// 指纹）：无覆盖时它们全体提前、彼此保持默认相对顺序（「优先我面前这台」）。
func TestResolveExecTargetOrder_GivenNoOverride_WhenMultipleSelf_ThenAllSelfFirst(t *testing.T) {
	convey.Convey("Given 默认顺序 [51(本机), 61(远端), 52(本机)]", t, func() {
		targets := targetsWith(51, 61, 52)
		override := []int64(nil)

		convey.Convey("When 解析（本机档 51、52）", func() {
			resolved := ResolveExecTargetOrder(targets, override, []int64{51, 52})

			convey.Convey("Then 两台本机档在前、保持相对顺序，远端殿后", func() {
				assert.Equal(t, []int64{51, 52, 61}, backendIDs(resolved))
			})
		})
	})
}

// 空列表 / self 不在列表里 → 原样，不崩溃不提前。
func TestResolveExecTargetOrder_GivenEmptyOrSelfAbsent_ThenSafe(t *testing.T) {
	convey.Convey("Given 空列表", t, func() {
		convey.Convey("When 解析", func() {
			assert.Nil(t, ResolveExecTargetOrder(nil, nil, nil))
		})
	})
	convey.Convey("Given self 不在列表里", t, func() {
		targets := targetsWith(51, 52)
		convey.Convey("When 解析（selfBackendID=99）", func() {
			assert.Equal(t, []int64{51, 52}, backendIDs(ResolveExecTargetOrder(targets, nil, []int64{99})))
		})
	})
}
