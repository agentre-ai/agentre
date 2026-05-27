// Package hook_entity contains the rich models for Hook signal sources, routing
// rules, and event log entries.
package hook_entity

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

type SourceKind string

const (
	SourceKindEmail    SourceKind = "email"
	SourceKindGitHub   SourceKind = "github"
	SourceKindSlack    SourceKind = "slack"
	SourceKindSchedule SourceKind = "schedule"
	SourceKindWebhook  SourceKind = "webhook"
	SourceKindSystem   SourceKind = "system"
)

type ConnectionStatus string

const (
	ConnectionConnected ConnectionStatus = "connected"
	ConnectionPending   ConnectionStatus = "pending"
	ConnectionDisabled  ConnectionStatus = "disabled"
	ConnectionError     ConnectionStatus = "error"
)

type EventStatus string

const (
	EventDispatched EventStatus = "dispatched"
	EventUnmatched  EventStatus = "unmatched"
	EventFailed     EventStatus = "failed"
)

var validSourceKinds = map[string]struct{}{
	string(SourceKindEmail):    {},
	string(SourceKindGitHub):   {},
	string(SourceKindSlack):    {},
	string(SourceKindSchedule): {},
	string(SourceKindWebhook):  {},
	string(SourceKindSystem):   {},
}

var validConnectionStatuses = map[string]struct{}{
	string(ConnectionConnected): {},
	string(ConnectionPending):   {},
	string(ConnectionDisabled):  {},
	string(ConnectionError):     {},
}

var validEventStatuses = map[string]struct{}{
	string(EventDispatched): {},
	string(EventUnmatched):  {},
	string(EventFailed):     {},
}

// HookSource is one configured external signal source.
type HookSource struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Kind             string `gorm:"column:kind;type:text;not null"`
	Name             string `gorm:"column:name;type:text;not null"`
	Description      string `gorm:"column:description;type:text;not null;default:''"`
	Identifier       string `gorm:"column:identifier;type:text;not null;default:''"`
	ConfigJSON       string `gorm:"column:config_json;type:text;not null;default:'{}'"`
	Enabled          int    `gorm:"column:enabled;type:int;not null;default:1"`
	ConnectionStatus string `gorm:"column:connection_status;type:text;not null;default:'pending'"`
	LastSyncTime     int64  `gorm:"column:last_sync_time;type:bigint;not null;default:0"`
	TotalCount       int64  `gorm:"column:total_count;type:bigint;not null;default:0"`
	Status           int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime       int64
	Updatetime       int64
}

func (*HookSource) TableName() string { return "hook_sources" }

func (s *HookSource) IsEnabled() bool { return s != nil && s.Enabled == 1 }

func (s *HookSource) Check(ctx context.Context) error {
	if s == nil {
		return i18n.NewError(ctx, code.HookSourceNotFound)
	}
	if strings.TrimSpace(s.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, ok := validSourceKinds[strings.TrimSpace(s.Kind)]; !ok {
		return i18n.NewError(ctx, code.HookInvalidSourceType)
	}
	status := strings.TrimSpace(s.ConnectionStatus)
	if status == "" {
		status = string(ConnectionPending)
		s.ConnectionStatus = status
	}
	if _, ok := validConnectionStatuses[status]; !ok {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if err := validateJSONObject(s.ConfigJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	return nil
}

// HookRule routes matching events from one source to one target Agent.
type HookRule struct {
	ID            int64  `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID      int64  `gorm:"column:source_id;type:bigint;not null"`
	Name          string `gorm:"column:name;type:text;not null"`
	ConditionExpr string `gorm:"column:condition_expr;type:text;not null;default:''"`
	TargetAgentID int64  `gorm:"column:target_agent_id;type:bigint;not null;default:0"`
	Enabled       int    `gorm:"column:enabled;type:int;not null;default:1"`
	IsFallback    int    `gorm:"column:is_fallback;type:int;not null;default:0"`
	SortOrder     int    `gorm:"column:sort_order;type:int;not null;default:0"`
	Status        int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime    int64
	Updatetime    int64
}

func (*HookRule) TableName() string { return "hook_rules" }

func (r *HookRule) IsEnabled() bool { return r != nil && r.Enabled == 1 }

func (r *HookRule) IsFallbackRule() bool { return r != nil && r.IsFallback == 1 }

func (r *HookRule) Check(ctx context.Context) error {
	if r == nil {
		return i18n.NewError(ctx, code.HookRuleNotFound)
	}
	if r.SourceID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if strings.TrimSpace(r.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}

// HookEvent is an immutable-ish event log entry. Service methods may append
// dispatch metadata for manual redelivery, but raw payload fields stay intact.
type HookEvent struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID         int64  `gorm:"column:source_id;type:bigint;not null"`
	Title            string `gorm:"column:title;type:text;not null"`
	SourceRef        string `gorm:"column:source_ref;type:text;not null;default:''"`
	Sender           string `gorm:"column:sender;type:text;not null;default:''"`
	EventType        string `gorm:"column:event_type;type:text;not null;default:''"`
	EventStatus      string `gorm:"column:event_status;type:text;not null"`
	PayloadJSON      string `gorm:"column:payload_json;type:text;not null;default:'{}'"`
	MatchedRulesJSON string `gorm:"column:matched_rules_json;type:text;not null;default:'[]'"`
	DispatchesJSON   string `gorm:"column:dispatches_json;type:text;not null;default:'[]'"`
	ReceivedAt       int64  `gorm:"column:received_at;type:bigint;not null;default:0"`
	Status           int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime       int64
	Updatetime       int64
}

func (*HookEvent) TableName() string { return "hook_events" }

func (e *HookEvent) Check(ctx context.Context) error {
	if e == nil {
		return i18n.NewError(ctx, code.HookEventNotFound)
	}
	if e.SourceID <= 0 || strings.TrimSpace(e.Title) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, ok := validEventStatuses[strings.TrimSpace(e.EventStatus)]; !ok {
		return i18n.NewError(ctx, code.HookInvalidEventStatus)
	}
	if err := validateJSONObject(e.PayloadJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	if err := validateJSONArray(e.MatchedRulesJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	if err := validateJSONArray(e.DispatchesJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	return nil
}

func validateJSONObject(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var out map[string]any
	return json.Unmarshal([]byte(trimmed), &out)
}

func validateJSONArray(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var out []any
	return json.Unmarshal([]byte(trimmed), &out)
}
