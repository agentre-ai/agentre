// Package hook_svc exposes Hook signal source, routing rule, and event log
// service contracts to the Wails binding layer.
package hook_svc

type SourceConfig struct {
	WebhookURL       string   `json:"webhookUrl"`
	Secret           string   `json:"secret"`
	VerifySignature  bool     `json:"verifySignature"`
	Events           []string `json:"events"`
	IMAPServer       string   `json:"imapServer"`
	IMAPPort         int      `json:"imapPort"`
	IMAPMailbox      string   `json:"imapMailbox"`
	UseTLS           *bool    `json:"useTls,omitempty"`
	EmailAddress     string   `json:"emailAddress"`
	AppPassword      string   `json:"appPassword"`
	PollingInterval  string   `json:"pollingInterval"`
	LastUID          uint32   `json:"lastUid"`
	UIDValidity      uint32   `json:"uidValidity"`
	BotToken         string   `json:"botToken"`
	Channel          string   `json:"channel"`
	CronExpr         string   `json:"cronExpr"`
	Timezone         string   `json:"timezone"`
	SystemPermission string   `json:"systemPermission"`
}

type HookSourceItem struct {
	ID               int64        `json:"id"`
	Kind             string       `json:"kind"`
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	Identifier       string       `json:"identifier"`
	Config           SourceConfig `json:"config"`
	Enabled          bool         `json:"enabled"`
	ConnectionStatus string       `json:"connectionStatus"`
	LastSyncTime     int64        `json:"lastSyncTime"`
	TotalCount       int64        `json:"totalCount"`
	Createtime       int64        `json:"createtime"`
	Updatetime       int64        `json:"updatetime"`
}

type AgentOption struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	AvatarColor  string `json:"avatarColor"`
	SystemBadge  string `json:"systemBadge"`
	DepartmentID int64  `json:"departmentId"`
}

type HookRuleItem struct {
	ID              int64  `json:"id"`
	SourceID        int64  `json:"sourceId"`
	Name            string `json:"name"`
	ConditionExpr   string `json:"conditionExpr"`
	TargetAgentID   int64  `json:"targetAgentId"`
	TargetAgentName string `json:"targetAgentName"`
	Enabled         bool   `json:"enabled"`
	IsFallback      bool   `json:"isFallback"`
	SortOrder       int    `json:"sortOrder"`
	Createtime      int64  `json:"createtime"`
	Updatetime      int64  `json:"updatetime"`
}

type RuleMatchResult struct {
	RuleID    int64  `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Matched   bool   `json:"matched"`
	Reason    string `json:"reason"`
	AgentID   int64  `json:"agentId"`
	AgentName string `json:"agentName"`
}

type HookDispatchItem struct {
	AgentID   int64  `json:"agentId"`
	AgentName string `json:"agentName"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

type HookEventItem struct {
	ID               int64              `json:"id"`
	SourceID         int64              `json:"sourceId"`
	SourceName       string             `json:"sourceName"`
	Title            string             `json:"title"`
	SourceRef        string             `json:"sourceRef"`
	Sender           string             `json:"sender"`
	EventType        string             `json:"eventType"`
	EventStatus      string             `json:"eventStatus"`
	PayloadJSON      string             `json:"payloadJson"`
	MatchedRules     []RuleMatchResult  `json:"matchedRules"`
	Dispatches       []HookDispatchItem `json:"dispatches"`
	MatchedRuleNames []string           `json:"matchedRuleNames"`
	TargetAgentNames []string           `json:"targetAgentNames"`
	ReceivedAt       int64              `json:"receivedAt"`
	Createtime       int64              `json:"createtime"`
	Updatetime       int64              `json:"updatetime"`
}

type LoadHooksRequest struct {
	SourceID int64 `json:"sourceId"`
	Limit    int   `json:"limit"`
}

type LoadHooksResponse struct {
	Sources []*HookSourceItem `json:"sources"`
	Rules   []*HookRuleItem   `json:"rules"`
	Events  []*HookEventItem  `json:"events"`
	Agents  []*AgentOption    `json:"agents"`
}

type CreateHookSourceRequest struct {
	Kind        string       `json:"kind" binding:"required"`
	Name        string       `json:"name" binding:"required"`
	Description string       `json:"description"`
	Identifier  string       `json:"identifier"`
	Config      SourceConfig `json:"config"`
	Enabled     bool         `json:"enabled"`
}

type CreateHookSourceResponse struct {
	Item *HookSourceItem `json:"item"`
}

type UpdateHookSourceRequest struct {
	ID          int64        `json:"id" binding:"required"`
	Kind        string       `json:"kind" binding:"required"`
	Name        string       `json:"name" binding:"required"`
	Description string       `json:"description"`
	Identifier  string       `json:"identifier"`
	Config      SourceConfig `json:"config"`
	Enabled     bool         `json:"enabled"`
}

type UpdateHookSourceResponse struct {
	Item *HookSourceItem `json:"item"`
}

type DeleteHookSourceRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteHookSourceResponse struct{}

type CreateHookRuleRequest struct {
	SourceID      int64  `json:"sourceId" binding:"required"`
	Name          string `json:"name" binding:"required"`
	ConditionExpr string `json:"conditionExpr"`
	TargetAgentID int64  `json:"targetAgentId"`
	Enabled       bool   `json:"enabled"`
}

type CreateHookRuleResponse struct {
	Item *HookRuleItem `json:"item"`
}

type UpdateHookRuleRequest struct {
	ID            int64  `json:"id" binding:"required"`
	Name          string `json:"name" binding:"required"`
	ConditionExpr string `json:"conditionExpr"`
	TargetAgentID int64  `json:"targetAgentId"`
	Enabled       bool   `json:"enabled"`
}

type UpdateHookRuleResponse struct {
	Item *HookRuleItem `json:"item"`
}

type DeleteHookRuleRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteHookRuleResponse struct{}

type TestHookSourceRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type TestHookSourceResponse struct {
	Item  *HookSourceItem `json:"item"`
	Event *HookEventItem  `json:"event"`
}

type SyncEmailSourceRequest struct {
	ID    int64 `json:"id" binding:"required"`
	Limit int   `json:"limit"`
}

type SyncEmailSourceResponse struct {
	Item    *HookSourceItem  `json:"item"`
	Events  []*HookEventItem `json:"events"`
	Created int              `json:"created"`
	Skipped int              `json:"skipped"`
}

type RedeliverHookEventRequest struct {
	ID            int64 `json:"id" binding:"required"`
	TargetAgentID int64 `json:"targetAgentId"`
}

type RedeliverHookEventResponse struct {
	Item *HookEventItem `json:"item"`
}
