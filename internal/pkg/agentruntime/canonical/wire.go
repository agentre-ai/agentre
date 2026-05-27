package canonical

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MarshalTool flat-encodes a CanonicalTool as `{"kind":"<kind>", ...fields}`.
// Each concrete type's existing JSON tags are inherited via anonymous struct
// embedding — fields keep their wire names, plus an injected `kind`. nil tools
// round-trip as JSON `null`.
//
// 故意没有把 MarshalJSON 挂到具体类型上:chat_svc/view/chat_block.go 的
// CanonicalBlock 已经有自己的 {kind, fileWrite?, fileEdit?, ...} 嵌套包装,
// 如果给 FileWrite 等加 MarshalJSON 会让 kind 同时出现在外层和内层,撞坏 UI
// 的 discriminated union 反序列化。事件 wire 路径走 MarshalTool / UnmarshalTool
// 这两个外置函数,与 ChatBlock 包装路径互不打扰。
func MarshalTool(ct CanonicalTool) ([]byte, error) {
	if ct == nil {
		return []byte("null"), nil
	}
	switch v := ct.(type) {
	case FileWrite:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			FileWrite
		}{KindFileWrite, v})
	case *FileWrite:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case FileEdit:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			FileEdit
		}{KindFileEdit, v})
	case *FileEdit:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case UserAsk:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			UserAsk
		}{KindUserAsk, v})
	case *UserAsk:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case PlanUpdate:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			PlanUpdate
		}{KindPlanUpdate, v})
	case *PlanUpdate:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case PlanApproveRequest:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			PlanApproveRequest
		}{KindPlanApproveRequest, v})
	case *PlanApproveRequest:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case AgentSpawn:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			AgentSpawn
		}{KindAgentSpawn, v})
	case *AgentSpawn:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	case ToolPermission:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
			ToolPermission
		}{KindToolPermission, v})
	case *ToolPermission:
		if v == nil {
			return []byte("null"), nil
		}
		return MarshalTool(*v)
	default:
		return nil, fmt.Errorf("canonical: MarshalTool: unknown concrete type %T", ct)
	}
}

// UnmarshalTool decodes a flat `{"kind":"<kind>", ...fields}` JSON object
// into the matching concrete CanonicalTool value. JSON `null` decodes to nil.
// Missing or unknown `kind` returns an error.
func UnmarshalTool(data []byte) (CanonicalTool, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var head struct {
		Kind Kind `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("canonical: UnmarshalTool: read kind: %w", err)
	}
	if head.Kind == "" {
		return nil, errors.New("canonical: UnmarshalTool: missing kind")
	}
	switch head.Kind {
	case KindFileWrite:
		var v FileWrite
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindFileEdit:
		var v FileEdit
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindUserAsk:
		var v UserAsk
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindPlanUpdate:
		var v PlanUpdate
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindPlanApproveRequest:
		var v PlanApproveRequest
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindAgentSpawn:
		var v AgentSpawn
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case KindToolPermission:
		var v ToolPermission
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, fmt.Errorf("canonical: UnmarshalTool: unknown kind %q", head.Kind)
	}
}
