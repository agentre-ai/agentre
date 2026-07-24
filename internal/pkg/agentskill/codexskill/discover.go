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
	"github.com/agentre-ai/agentre/pkg/codex"
)

func init() {
	agentskill.RegisterDiscoverer(agent_backend_entity.TypeCodex, Discoverer{})
}

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
type skillLister func(ctx context.Context, binary, cwd string, config []string) ([]codex.Skill, error)

// Discoverer 用 codex CLI 枚举已安装插件。run 为 nil 时走真实 exec(生产默认)。
type Discoverer struct {
	run        commandRunner
	listSkills skillLister
}

func (d Discoverer) skills(ctx context.Context, binary, cwd string, config []string) ([]codex.Skill, error) {
	if d.listSkills != nil {
		return d.listSkills(ctx, binary, cwd, config)
	}
	opts := []codex.Option{codex.WithBinary(binary), codex.WithCwd(cwd)}
	for _, item := range config {
		opts = append(opts, codex.WithConfig(item))
	}
	return codex.New(opts...).ListSkills(ctx, []string{cwd}, true)
}

func (d Discoverer) runner() commandRunner {
	if d.run != nil {
		return d.run
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // name is the user-configured Codex CLI path, not message input.
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
		return out, nil //nolint:nilerr // Malformed optional discovery output degrades to an empty catalog.
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
		return []agentskill.SkillPack{}, nil //nolint:nilerr // An unavailable optional CLI must not block the composer.
	}
	return parsePluginList(b)
}

// DiscoverCommands delegates to Codex app-server skills/list so plugin, user,
// project, and system skills follow the current CLI's own resolution rules.
func (d Discoverer) DiscoverCommands(ctx context.Context, q agentskill.CommandDiscoverQuery) ([]agentskill.SkillCommand, error) {
	bin := strings.TrimSpace(q.CLIPath)
	if bin == "" {
		bin = "codex"
	}
	cwd := strings.TrimSpace(q.Cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	skills, err := d.skills(ctx, bin, cwd, codex.PluginEnabledConfig(q.EnabledPlugins))
	if err != nil {
		return []agentskill.SkillCommand{}, nil //nolint:nilerr // Skill suggestions are optional and fail open to normal message input.
	}
	seen := map[string]struct{}{}
	commands := []agentskill.SkillCommand{}
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if !skill.Enabled || name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		commands = append(commands, agentskill.SkillCommand{
			Name:        name,
			Description: strings.TrimSpace(skill.Description),
		})
	}
	return commands, nil
}
