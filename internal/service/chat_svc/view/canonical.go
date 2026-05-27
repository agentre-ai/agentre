package view

import (
	"agentre/internal/pkg/agentruntime/canonical"
)

// FromCanonical 把 agentruntime.canonical 类型转成 wire CanonicalDTO。
// nil-safe(返 nil)。
//
// Live emit 路径:ToolCall handler 拿 ev.Canonical 直接调这里;
// Replay 路径:view/project.go 在重建 ChatBlock 时按需从 raw ToolUseBlock.Input
// 重算 canonical 后再调这里。
func FromCanonical(c canonical.CanonicalTool) *CanonicalDTO {
	if c == nil {
		return nil
	}
	dto := &CanonicalDTO{Kind: canonical.KindOf(c)}
	switch t := c.(type) {
	case canonical.FileWrite:
		dto.FileWrite = &t
	case *canonical.FileWrite:
		dto.FileWrite = t
	case canonical.FileEdit:
		dto.FileEdit = &t
	case *canonical.FileEdit:
		dto.FileEdit = t
	case canonical.UserAsk:
		dto.UserAsk = &t
	case *canonical.UserAsk:
		dto.UserAsk = t
	case canonical.PlanUpdate:
		dto.PlanUpdate = &t
	case *canonical.PlanUpdate:
		dto.PlanUpdate = t
	case canonical.PlanApproveRequest:
		dto.PlanApprove = &t
	case *canonical.PlanApproveRequest:
		dto.PlanApprove = t
	case canonical.AgentSpawn:
		dto.AgentSpawn = &t
	case *canonical.AgentSpawn:
		dto.AgentSpawn = t
	case canonical.ToolPermission:
		dto.ToolPermission = &t
	case *canonical.ToolPermission:
		dto.ToolPermission = t
	}
	return dto
}
