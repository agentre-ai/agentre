package agent_entity

import (
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
)

// AgentExecTarget Agent 有序执行目标列表里的一项（R15）。列表的每一项是一个
// backend；backend 自己的 device_id 决定这一档落在哪台机器上（空 = 当前桌面端，
// 非空 = 那台 agentred）。
//
// SkillsJSON 是这一档自己的技能授权（R15e / 决策 33）：每一档各自持有一份，
// 发现从这一档所在的机器发起（本地进程内调 CLI / 远端走 daemon skills.list），
// 不与列表里别的档合并、不做并集。存放位置从 agents.skills_json 下沉到这里；
// GetSkills / SetSkills / GetEnabledPackIDs / SkillPackEnabled 随字段一起搬过来。
type AgentExecTarget struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"`
	AgentID        int64  `gorm:"column:agent_id;type:bigint;not null"`
	AgentBackendID int64  `gorm:"column:agent_backend_id;type:bigint;not null"`
	SortOrder      int    `gorm:"column:sort_order;type:int;not null;default:0"`
	SkillsJSON     string `gorm:"column:skills_json;type:text;not null;default:'[]'"`
	// SyncMeta 账号级同步元数据（R1，366 行）。
	syncmeta_entity.SyncMeta `gorm:"embedded"`
}

func (*AgentExecTarget) TableName() string { return "agent_exec_targets" }

func (t *AgentExecTarget) GetSkills() []AgentSkillItem {
	out := []AgentSkillItem{}
	if t == nil || t.SkillsJSON == "" {
		return out
	}
	_ = json.Unmarshal([]byte(t.SkillsJSON), &out)
	if out == nil {
		out = []AgentSkillItem{}
	}
	return out
}

func (t *AgentExecTarget) SetSkills(items []AgentSkillItem) {
	if items == nil {
		items = []AgentSkillItem{}
	}
	b, _ := json.Marshal(items)
	t.SkillsJSON = string(b)
}

// GetEnabledPackIDs 返回 enabled 的技能包 id。
func (t *AgentExecTarget) GetEnabledPackIDs() []string {
	out := []string{}
	for _, it := range t.GetSkills() {
		if it.Enabled {
			out = append(out, it.ID)
		}
	}
	return out
}

// SkillPackEnabled 报告某技能包是否开启。
func (t *AgentExecTarget) SkillPackEnabled(id string) bool {
	for _, it := range t.GetSkills() {
		if it.ID == id {
			return it.Enabled
		}
	}
	return false
}
