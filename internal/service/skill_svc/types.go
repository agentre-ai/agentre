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
}
type SkillCatalogDTO struct {
	Packs []SkillPackDTO `json:"packs"`
}
