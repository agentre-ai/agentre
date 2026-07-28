package agentruntime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

// PiAgentSessionsDir returns Agentre's dedicated Pi session store:
//
//	<AppDataDir>/piagent/sessions/
//
// It is intentionally separate from the agent cwd so Pi session JSONL files do
// not leak into user projects.
func PiAgentSessionsDir() (string, error) {
	root, err := paths.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "piagent", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// PiAgentSessionFilePath maps an Agentre chat session id to the deterministic
// Pi session JSONL path used for cross-turn resume. sessionID<=0 or empty dir
// means no resume path is available.
func PiAgentSessionFilePath(dir string, sessionID int64) string {
	if dir == "" || sessionID <= 0 {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("agentre-%d.jsonl", sessionID))
}
