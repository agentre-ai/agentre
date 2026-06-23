package agentruntime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

// AgentCwd 给需要文件系统工具的后端拼一个稳定的 Agent 工作目录：
//
//	<AppDataDir>/agents/<agentID>/
//
// 同一 Agent 的所有聊天会话复用同一目录，便于内置工具和 CLI 后端累积用户文件。
// 会话软删除不清理该目录；它是 Agent 级工作区。
func AgentCwd(agentID int64) (string, error) {
	if agentID <= 0 {
		return "", fmt.Errorf("agentruntime: AgentCwd needs agentID > 0")
	}
	root, err := paths.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "agents", fmt.Sprintf("%d", agentID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
