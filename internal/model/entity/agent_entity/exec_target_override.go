package agent_entity

import (
	"encoding/json"
)

// AgentExecTargetOverride 是本端（这台桌面端安装）对某个 Agent 执行目标顺序的
// 本地覆盖（R14）：只改这一端、不同步、不随账号同步载荷上行、也不进同步队列。
//
// 它只表达**顺序**（backend 的排列），不增删执行目标档 —— 档的集合是账号级的
// （R13 fan-out 之后每台机器各有一档），顺序是机器相关的偏好。
//
// OrderJSON 存该 Agent 执行目标档 backend id 的排列（JSON 数组）。空数组 / 缺失
// = 没有覆盖，解析回落到账号默认顺序。
type AgentExecTargetOverride struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	AgentID    int64  `gorm:"column:agent_id;type:bigint;not null"`
	OrderJSON  string `gorm:"column:order_json;type:text;not null;default:'[]'"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

// TableName 绑定表名。
func (*AgentExecTargetOverride) TableName() string { return "agent_exec_target_overrides" }

// GetOrder 把 OrderJSON 解成 backend id 序列；空/坏 JSON 返回空切片。
func (o *AgentExecTargetOverride) GetOrder() []int64 {
	if o == nil {
		return nil
	}
	out := []int64{}
	if o.OrderJSON == "" {
		return out
	}
	if err := json.Unmarshal([]byte(o.OrderJSON), &out); err != nil {
		return []int64{}
	}
	return out
}

// SetOrder 把 backend id 序列序列化进 OrderJSON。
func (o *AgentExecTargetOverride) SetOrder(order []int64) {
	if order == nil {
		order = []int64{}
	}
	b, _ := json.Marshal(order)
	o.OrderJSON = string(b)
}

// ResolveExecTargetOrder 是 R14 的顺序解析（纯函数，供派发 / 组织架构页共用）：
//
//  1. 本端有覆盖（override 非空）→ 用覆盖顺序；覆盖里没列到的档按默认顺序补到尾部，
//     覆盖里指向已不存在的 backend 的 id 忽略（档集合是账号级的，覆盖可能因增删档
//     而过期，解析必须收敛到当前集合）。
//  2. 无覆盖 → 账号默认顺序；若 selfBackendIDs 非空，把指向本机（自己）的那几档提到
//     最前、保持它们之间的默认相对顺序（「优先我面前这台」，R14 的桌面端落点）。
//  3. selfBackendIDs 为空（浏览器语境没有「自己」可排）→ 原样用默认顺序，不提前。
//
// 不修改入参，返回按解析后顺序排列的新切片。
func ResolveExecTargetOrder(targets []*AgentExecTarget, override []int64, selfBackendIDs []int64) []*AgentExecTarget {
	if len(targets) == 0 {
		return nil
	}
	if len(override) > 0 {
		placed := make(map[int64]bool, len(targets))
		out := make([]*AgentExecTarget, 0, len(targets))
		byBackend := make(map[int64]*AgentExecTarget, len(targets))
		for _, t := range targets {
			if t != nil {
				byBackend[t.AgentBackendID] = t
			}
		}
		for _, id := range override {
			t, ok := byBackend[id]
			if !ok || placed[id] {
				continue
			}
			placed[id] = true
			out = append(out, t)
		}
		for _, t := range targets {
			if t != nil && !placed[t.AgentBackendID] {
				out = append(out, t)
			}
		}
		return out
	}
	if len(selfBackendIDs) > 0 {
		self := make(map[int64]bool, len(selfBackendIDs))
		for _, id := range selfBackendIDs {
			if id > 0 {
				self[id] = true
			}
		}
		out := make([]*AgentExecTarget, 0, len(targets))
		front := make([]*AgentExecTarget, 0, len(self))
		for _, t := range targets {
			if t != nil && self[t.AgentBackendID] {
				front = append(front, t)
				continue
			}
			out = append(out, t)
		}
		if len(front) > 0 {
			return append(front, out...)
		}
	}
	return append([]*AgentExecTarget(nil), targets...)
}
