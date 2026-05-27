// Package agent_entity 维护 Agent 的充血实体。
package agent_entity

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

// SystemBadgeDefault 标记不可删除的 CEO 助手。
const SystemBadgeDefault = "DEFAULT"

// AgentSkillItem Agent 技能开关。
type AgentSkillItem struct {
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

// Agent 一条 Agent 记录。
type Agent struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name           string `gorm:"column:name;type:text;not null"`
	Description    string `gorm:"column:description;type:text;not null;default:''"`
	AvatarColor    string `gorm:"column:avatar_color;type:text;not null;default:''"`
	AvatarIcon     string `gorm:"column:avatar_icon;type:text;not null;default:''"`
	AvatarDataURL  string `gorm:"column:avatar_data_url;type:text;not null;default:''"`
	SystemBadge    string `gorm:"column:system_badge;type:text;not null;default:''"`
	DepartmentID   int64  `gorm:"column:department_id;type:bigint;not null;default:0"`
	ParentAgentID  int64  `gorm:"column:parent_agent_id;type:bigint;not null;default:0"`
	AgentBackendID int64  `gorm:"column:agent_backend_id;type:bigint;not null;default:0"`
	SortOrder      int    `gorm:"column:sort_order;type:int;not null;default:0"`
	PromptJSON     string `gorm:"column:prompt_json;type:text;not null;default:'[]'"`
	SkillsJSON     string `gorm:"column:skills_json;type:text;not null;default:'[]'"`
	Status         int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime     int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime     int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Agent) TableName() string { return "agents" }

func (a *Agent) IsActive() bool { return a != nil && a.Status == consts.ACTIVE }
func (a *Agent) IsSystem() bool { return a != nil && a.SystemBadge == SystemBadgeDefault }

func (a *Agent) GetPrompt() []string {
	out := []string{}
	if a == nil || a.PromptJSON == "" {
		return out
	}
	_ = json.Unmarshal([]byte(a.PromptJSON), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func (a *Agent) SetPrompt(lines []string) {
	if lines == nil {
		lines = []string{}
	}
	b, _ := json.Marshal(lines)
	a.PromptJSON = string(b)
}

func (a *Agent) GetSkills() []AgentSkillItem {
	out := []AgentSkillItem{}
	if a == nil || a.SkillsJSON == "" {
		return out
	}
	_ = json.Unmarshal([]byte(a.SkillsJSON), &out)
	if out == nil {
		out = []AgentSkillItem{}
	}
	return out
}

func (a *Agent) SetSkills(items []AgentSkillItem) {
	if items == nil {
		items = []AgentSkillItem{}
	}
	b, _ := json.Marshal(items)
	a.SkillsJSON = string(b)
}

var allowedAvatarColors = map[string]struct{}{
	"":         {},
	"agent-1":  {},
	"agent-2":  {},
	"agent-3":  {},
	"agent-4":  {},
	"agent-5":  {},
	"agent-6":  {},
	"agent-7":  {},
	"agent-8":  {},
	"agent-9":  {},
	"agent-10": {},
	"neutral":  {},
}

// Check 字段校验。
func (a *Agent) Check(ctx context.Context) error {
	if a == nil {
		return i18n.NewError(ctx, code.AgentNotFound)
	}
	if strings.TrimSpace(a.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, ok := allowedAvatarColors[a.AvatarColor]; !ok {
		return i18n.NewError(ctx, code.AgentInvalidColor)
	}
	if len(a.AvatarIcon) > 32 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}

	if a.IsSystem() {
		if a.DepartmentID != 0 || a.ParentAgentID != 0 {
			return i18n.NewError(ctx, code.AgentSystemImmutable)
		}
	} else {
		hasDepartment := a.DepartmentID > 0
		hasParentAgent := a.ParentAgentID > 0
		if !hasDepartment && !hasParentAgent {
			return i18n.NewError(ctx, code.AgentDepartmentRequired)
		}
		if hasDepartment && hasParentAgent {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if a.AgentBackendID <= 0 {
			return i18n.NewError(ctx, code.AgentBackendRequired)
		}
	}

	if !isValidJSONArray(a.PromptJSON) {
		return i18n.NewError(ctx, code.AgentInvalidPayload)
	}
	if !isValidJSONArray(a.SkillsJSON) {
		return i18n.NewError(ctx, code.AgentInvalidPayload)
	}
	return nil
}

func isValidJSONArray(s string) bool {
	if s == "" {
		return true
	}
	var v []json.RawMessage
	return json.Unmarshal([]byte(s), &v) == nil
}
