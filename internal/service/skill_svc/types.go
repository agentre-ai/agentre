package skill_svc

type SkillPackDTO struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Skills          []string `json:"skills"`
	Source          string   `json:"source"`
	Recommended     bool     `json:"recommended"`
	Installed       bool     `json:"installed"`
	Enabled         bool     `json:"enabled"`
	GloballyEnabled bool     `json:"globallyEnabled"` // CLI 全局启用态(继承判定用)
	// EffectiveEnabled 是最终传给 CLI 的生效态:显式覆盖优先,否则继承全局态;
	// 未安装的推荐包永远为 false。输入框命令提示以此为唯一真相。
	EffectiveEnabled bool `json:"effectiveEnabled"`
}
type SkillCatalogDTO struct {
	Packs []SkillPackDTO `json:"packs"`
}

// SkillCommandDTO 是输入框可直接补全的 backend-native Skill 名称。
// Name 不包含 Codex `$` / Claude Code `/` 前缀，前端按 backend 统一添加。
type SkillCommandDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SkillCommandCatalogDTO struct {
	Commands []SkillCommandDTO `json:"commands"`
}
