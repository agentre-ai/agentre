// Package codexskill 用 `codex plugin list --json` 发现该安装的插件技能包。
package codexskill

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
	agentskill.RegisterDiscoverer(agent_backend_entity.TypeCodex, Discoverer{})
}

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Discoverer 用 codex CLI 枚举已安装插件。run 为 nil 时走真实 exec(生产默认)。
type Discoverer struct {
	run commandRunner
}

func (d Discoverer) runner() commandRunner {
	if d.run != nil {
		return d.run
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
}

type rawPluginList struct {
	Installed []rawPlugin `json:"installed"`
}

type rawPlugin struct {
	PluginID string `json:"pluginId"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Source   struct {
		Path string `json:"path"`
	} `json:"source"`
}

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
			continue
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
	var raws rawPluginList
	if err := json.Unmarshal(b, &raws); err != nil {
		return out, nil
	}
	for _, r := range raws.Installed {
		id := strings.TrimSpace(r.PluginID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = id
			if i := strings.Index(id, "@"); i > 0 {
				name = id[:i]
			}
		}
		out = append(out, agentskill.SkillPack{
			ID:              id,
			Name:            name,
			Skills:          scanSkills(strings.TrimSpace(r.Source.Path)),
			Source:          agentskill.SourceInstalled,
			Installed:       true,
			GloballyEnabled: r.Enabled,
		})
	}
	return out, nil
}

// Discover 调用 codex plugin list --json 枚举已安装插件。CLI 不可用时软降级返回空。
func (d Discoverer) Discover(ctx context.Context, q agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	bin := strings.TrimSpace(q.CLIPath)
	if bin == "" {
		bin = "codex"
	}
	b, err := d.runner()(ctx, bin, "plugin", "list", "--json")
	if err != nil {
		return []agentskill.SkillPack{}, nil
	}
	return parsePluginList(b)
}
