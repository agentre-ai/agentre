// Package claudeskill 用 `claude plugin list --json` 发现该安装的技能包。
package claudeskill

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

func init() {
	agentskill.RegisterDiscoverer(agent_backend_entity.TypeClaudeCode, Discoverer{})
}

// commandRunner 执行 CLI 并返回 stdout。注入接缝:单测替换为假命令,免依赖真实 claude 二进制。
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Discoverer 用 claude CLI 枚举已安装技能包。run 为 nil 时走真实 exec(生产默认)。
type Discoverer struct {
	run commandRunner
}

// runner 取命令执行器:未注入 → 真实 exec.CommandContext().Output()。
func (d Discoverer) runner() commandRunner {
	if d.run != nil {
		return d.run
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
}

// rawPlugin 映射 `claude plugin list --json` 单元素。Enabled = CLI 全局启用态
// (透出到 SkillPack.GloballyEnabled,供"继承"模型判定);Scope 暂不消费。
type rawPlugin struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"` // 用于枚举包内 skill
}

// scanSkills 枚举 plugin 安装目录下 skills/*/SKILL.md,返回 skill 名(目录名,
// os.ReadDir 已按名排序)。installPath 为空 / 无 skills 目录 / 不可读 → nil,不阻断发现。
func scanSkills(installPath string) []string {
	if installPath == "" {
		return nil
	}
	skillsDir := filepath.Join(installPath, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); err != nil {
			continue // 没有 SKILL.md 的子目录不是 skill
		}
		out = append(out, e.Name())
	}
	return out
}

func parsePluginList(b []byte) ([]agentskill.SkillPack, error) {
	out := []agentskill.SkillPack{}
	if len(b) == 0 {
		return out, nil
	}
	var raws []rawPlugin
	if err := json.Unmarshal(b, &raws); err != nil {
		return out, nil // 坏 JSON 视为无发现,不阻断
	}
	for _, r := range raws {
		name := r.ID
		if i := strings.Index(r.ID, "@"); i > 0 {
			name = r.ID[:i]
		}
		out = append(out, agentskill.SkillPack{
			ID:              r.ID,
			Name:            name,
			Skills:          scanSkills(r.InstallPath),
			Source:          agentskill.SourceInstalled,
			Installed:       true,
			GloballyEnabled: r.Enabled,
		})
	}
	return out, nil
}

// Discover 调用 claude plugin list --json 枚举已安装技能包。CLI 不可用时软降级返回空。
func (d Discoverer) Discover(ctx context.Context, q agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	bin := strings.TrimSpace(q.CLIPath)
	if bin == "" {
		bin = "claude"
	}
	b, err := d.runner()(ctx, bin, "plugin", "list", "--json")
	if err != nil {
		return []agentskill.SkillPack{}, nil // CLI 不可用 → 软降级(空发现)
	}
	return parsePluginList(b)
}
