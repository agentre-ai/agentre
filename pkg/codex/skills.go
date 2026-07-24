package codex

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Skill is one command-addressable Codex skill returned by app-server skills/list.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
}

type skillsListResponse struct {
	Data []struct {
		Cwd    string  `json:"cwd"`
		Skills []Skill `json:"skills"`
	} `json:"data"`
}

// ListSkills asks a short-lived app-server to resolve the skills visible from the
// requested working directories. It includes plugin, user, project, and system
// skills according to the installed Codex CLI's own precedence rules.
func (c *Client) ListSkills(ctx context.Context, cwds []string, forceReload bool) ([]Skill, error) {
	app, err := c.startApp(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = app.terminate(context.Background(), c.killGrace) }()

	if err := initializeApp(ctx, app); err != nil {
		return nil, err
	}
	raw, err := app.Call(ctx, appMethodSkillsList, map[string]any{
		"cwds":        cwds,
		"forceReload": forceReload,
	})
	if err != nil {
		return nil, err
	}
	var response skillsListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	var skills []Skill
	for _, item := range response.Data {
		skills = append(skills, item.Skills...)
	}
	return skills, nil
}

// PluginEnabledConfig renders sparse per-agent plugin overrides using Codex's
// TOML command-line config syntax. Unlisted plugins continue to inherit global config.
func PluginEnabledConfig(enabled map[string]bool) []string {
	if len(enabled) == 0 {
		return nil
	}
	keys := make([]string, 0, len(enabled))
	for key := range enabled {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		id := strings.TrimSpace(key)
		out = append(out, "plugins."+strconv.Quote(id)+".enabled="+strconv.FormatBool(enabled[key]))
	}
	return out
}
