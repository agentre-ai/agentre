package agent_backend_svc

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/utils/httputils"

	"agentre/internal/pkg/cliprober"
	"agentre/internal/pkg/code"
)

// defaultNameForType 按 CLI 后端类型返回自动扫描时的默认名称。
func defaultNameForType(backendType string) string {
	switch backendType {
	case "claudecode":
		return "Claude Code CLI"
	case "codex":
		return "Codex CLI"
	case "piagent":
		return "Pi Agent CLI"
	default:
		return backendType + " CLI"
	}
}

// isNameDuplicated 判断 err 是否为 i18n 名称重复错误(AgentBackendNameDuplicated)。
func isNameDuplicated(err error) bool {
	var httpErr *httputils.Error
	if errors.As(err, &httpErr) {
		return httpErr.Code == int(code.AgentBackendNameDuplicated)
	}
	return false
}

// ScanAndCreateAgentBackends 扫描系统 PATH 中的 Claude Code / Codex / Pi Agent CLI，
// 命中时自动创建对应的 Agent 后端配置。
func (s *agentBackendSvc) ScanAndCreateAgentBackends(ctx context.Context, _ *ScanAndCreateAgentBackendsRequest) (*ScanAndCreateAgentBackendsResponse, error) {
	results := cliprober.ScanAllCLIs()
	items := make([]*ScanResultItem, 0, len(results))
	for _, r := range results {
		item := &ScanResultItem{
			Type:    r.BackendType,
			Name:    defaultNameForType(r.BackendType),
			CLIPath: r.Path,
			Found:   r.Found,
		}
		if !r.Found {
			item.Error = "binary not found in system PATH"
			items = append(items, item)
			continue
		}
		resp, err := s.Create(ctx, &CreateBackendRequest{
			Type:    r.BackendType,
			Name:    item.Name,
			CLIPath: r.Path,
		})
		if err != nil {
			if isNameDuplicated(err) {
				item.Skipped = true
				item.Error = "name already exists"
			} else {
				item.Error = err.Error()
			}
		} else {
			item.Created = true
			item.BackendID = resp.Item.ID
		}
		items = append(items, item)
	}
	return &ScanAndCreateAgentBackendsResponse{Results: items}, nil
}
