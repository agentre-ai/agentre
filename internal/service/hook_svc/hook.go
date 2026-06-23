package hook_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
)

const (
	defaultEventLimit = 80
	maskedSecret      = "********"
)

type HookSvc interface {
	Load(ctx context.Context, req *LoadHooksRequest) (*LoadHooksResponse, error)
	CreateSource(ctx context.Context, req *CreateHookSourceRequest) (*CreateHookSourceResponse, error)
	UpdateSource(ctx context.Context, req *UpdateHookSourceRequest) (*UpdateHookSourceResponse, error)
	DeleteSource(ctx context.Context, req *DeleteHookSourceRequest) (*DeleteHookSourceResponse, error)
	CreateRule(ctx context.Context, req *CreateHookRuleRequest) (*CreateHookRuleResponse, error)
	UpdateRule(ctx context.Context, req *UpdateHookRuleRequest) (*UpdateHookRuleResponse, error)
	DeleteRule(ctx context.Context, req *DeleteHookRuleRequest) (*DeleteHookRuleResponse, error)
	TestSource(ctx context.Context, req *TestHookSourceRequest) (*TestHookSourceResponse, error)
	SyncEmailSource(ctx context.Context, req *SyncEmailSourceRequest) (*SyncEmailSourceResponse, error)
	RedeliverEvent(ctx context.Context, req *RedeliverHookEventRequest) (*RedeliverHookEventResponse, error)
	StartEmailPoller(ctx context.Context) context.CancelFunc
}

type hookSvc struct {
	now          func() int64
	mailFetcher  MailFetcher
	emailSyncMu  sync.Mutex
	pollerMu     sync.Mutex
	pollerCancel context.CancelFunc
}

var defaultHook HookSvc = &hookSvc{now: func() int64 { return time.Now().Unix() }}

func Hook() HookSvc { return defaultHook }

func (s *hookSvc) Load(ctx context.Context, req *LoadHooksRequest) (*LoadHooksResponse, error) {
	if req == nil {
		req = &LoadHooksRequest{}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultEventLimit
	}

	sources, err := hook_repo.HookSource().List(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := hook_repo.HookRule().List(ctx)
	if err != nil {
		return nil, err
	}
	var events []*hook_entity.HookEvent
	if req.SourceID > 0 {
		events, err = hook_repo.HookEvent().ListBySource(ctx, req.SourceID, limit)
	} else {
		events, err = hook_repo.HookEvent().ListRecent(ctx, limit)
	}
	if err != nil {
		return nil, err
	}

	agents, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	sourceMap := make(map[int64]string, len(sources))
	sourceItems := make([]*HookSourceItem, 0, len(sources))
	for _, source := range sources {
		sourceMap[source.ID] = source.Name
		sourceItems = append(sourceItems, sourceToItem(source))
	}

	ruleItems := make([]*HookRuleItem, 0, len(rules))
	for _, rule := range rules {
		ruleItems = append(ruleItems, ruleToItem(rule, agentMap))
	}

	eventItems := make([]*HookEventItem, 0, len(events))
	for _, event := range events {
		eventItems = append(eventItems, eventToItem(event, sourceMap, agentMap))
	}

	return &LoadHooksResponse{
		Sources: sourceItems,
		Rules:   ruleItems,
		Events:  eventItems,
		Agents:  agents,
	}, nil
}

func (s *hookSvc) CreateSource(ctx context.Context, req *CreateHookSourceRequest) (*CreateHookSourceResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	now := s.now()
	source := &hook_entity.HookSource{
		Kind:             strings.TrimSpace(req.Kind),
		Name:             strings.TrimSpace(req.Name),
		Description:      strings.TrimSpace(req.Description),
		Identifier:       strings.TrimSpace(req.Identifier),
		ConfigJSON:       serializeConfig(req.Config),
		Enabled:          boolInt(req.Enabled),
		ConnectionStatus: sourceConnectionStatus(req.Enabled),
		Status:           consts.ACTIVE,
		Createtime:       now,
		Updatetime:       now,
	}
	if err := source.Check(ctx); err != nil {
		return nil, err
	}
	dup, err := hook_repo.HookSource().FindByName(ctx, source.Name)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.HookSourceNameDuplicated)
	}
	if err := hook_repo.HookSource().Create(ctx, source); err != nil {
		return nil, err
	}
	if err := s.ensureFallbackRule(ctx, source.ID); err != nil {
		return nil, err
	}
	return &CreateHookSourceResponse{Item: sourceToItem(source)}, nil
}

func (s *hookSvc) UpdateSource(ctx context.Context, req *UpdateHookSourceRequest) (*UpdateHookSourceResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	source, err := hook_repo.HookSource().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, i18n.NewError(ctx, code.HookSourceNotFound)
	}

	newName := strings.TrimSpace(req.Name)
	if newName != source.Name {
		dup, err := hook_repo.HookSource().FindByName(ctx, newName)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != source.ID {
			return nil, i18n.NewError(ctx, code.HookSourceNameDuplicated)
		}
	}

	source.Kind = strings.TrimSpace(req.Kind)
	source.Name = newName
	source.Description = strings.TrimSpace(req.Description)
	source.Identifier = strings.TrimSpace(req.Identifier)
	cfg := preserveSourceSecrets(req.Config, parseSourceConfig(source.ConfigJSON))
	source.ConfigJSON = serializeConfig(cfg)
	source.Enabled = boolInt(req.Enabled)
	if !req.Enabled {
		source.ConnectionStatus = string(hook_entity.ConnectionDisabled)
	} else if source.ConnectionStatus == string(hook_entity.ConnectionDisabled) {
		source.ConnectionStatus = string(hook_entity.ConnectionPending)
	}
	source.Updatetime = s.now()
	if err := source.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookSource().Update(ctx, source); err != nil {
		return nil, err
	}
	return &UpdateHookSourceResponse{Item: sourceToItem(source)}, nil
}

func (s *hookSvc) DeleteSource(ctx context.Context, req *DeleteHookSourceRequest) (*DeleteHookSourceResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	source, err := hook_repo.HookSource().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, i18n.NewError(ctx, code.HookSourceNotFound)
	}
	if err := hook_repo.HookSource().Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &DeleteHookSourceResponse{}, nil
}

func (s *hookSvc) CreateRule(ctx context.Context, req *CreateHookRuleRequest) (*CreateHookRuleResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, err := s.requireSource(ctx, req.SourceID); err != nil {
		return nil, err
	}
	if err := s.requireAgentIfSet(ctx, req.TargetAgentID); err != nil {
		return nil, err
	}
	sortOrder, err := hook_repo.HookRule().NextSortOrder(ctx, req.SourceID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rule := &hook_entity.HookRule{
		SourceID:      req.SourceID,
		Name:          strings.TrimSpace(req.Name),
		ConditionExpr: strings.TrimSpace(req.ConditionExpr),
		TargetAgentID: req.TargetAgentID,
		Enabled:       boolInt(req.Enabled),
		SortOrder:     sortOrder,
		Status:        consts.ACTIVE,
		Createtime:    now,
		Updatetime:    now,
	}
	if err := rule.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookRule().Create(ctx, rule); err != nil {
		return nil, err
	}
	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	return &CreateHookRuleResponse{Item: ruleToItem(rule, agentMap)}, nil
}

func (s *hookSvc) UpdateRule(ctx context.Context, req *UpdateHookRuleRequest) (*UpdateHookRuleResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	rule, err := hook_repo.HookRule().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, i18n.NewError(ctx, code.HookRuleNotFound)
	}
	if err := s.requireAgentIfSet(ctx, req.TargetAgentID); err != nil {
		return nil, err
	}
	rule.Name = strings.TrimSpace(req.Name)
	rule.ConditionExpr = strings.TrimSpace(req.ConditionExpr)
	rule.TargetAgentID = req.TargetAgentID
	rule.Enabled = boolInt(req.Enabled)
	if rule.IsFallbackRule() {
		rule.Enabled = 1
	}
	rule.Updatetime = s.now()
	if err := rule.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookRule().Update(ctx, rule); err != nil {
		return nil, err
	}
	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	return &UpdateHookRuleResponse{Item: ruleToItem(rule, agentMap)}, nil
}

func (s *hookSvc) DeleteRule(ctx context.Context, req *DeleteHookRuleRequest) (*DeleteHookRuleResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	rule, err := hook_repo.HookRule().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, i18n.NewError(ctx, code.HookRuleNotFound)
	}
	if rule.IsFallbackRule() {
		return nil, i18n.NewError(ctx, code.HookRuleFallbackImmutable)
	}
	if err := hook_repo.HookRule().Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &DeleteHookRuleResponse{}, nil
}

func (s *hookSvc) TestSource(ctx context.Context, req *TestHookSourceRequest) (*TestHookSourceResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	source, err := s.requireSource(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	source.ConnectionStatus = string(hook_entity.ConnectionConnected)
	source.LastSyncTime = now
	source.TotalCount++
	source.Updatetime = now
	if err := hook_repo.HookSource().Update(ctx, source); err != nil {
		return nil, err
	}

	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := hook_repo.HookRule().ListBySource(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"type":       "connection_test",
		"sourceId":   source.ID,
		"sourceName": source.Name,
		"kind":       source.Kind,
	}
	matches, dispatches := evaluateRules(source, rules, payload, agentMap)
	eventStatus := string(hook_entity.EventUnmatched)
	if len(dispatches) > 0 {
		eventStatus = string(hook_entity.EventDispatched)
	}
	payloadRaw, _ := json.MarshalIndent(payload, "", "  ")
	matchesRaw, _ := json.Marshal(matches)
	dispatchRaw, _ := json.Marshal(dispatches)
	event := &hook_entity.HookEvent{
		SourceID:         source.ID,
		Title:            fmt.Sprintf("连接测试 · %s", source.Name),
		SourceRef:        source.Identifier,
		Sender:           "Agentre",
		EventType:        "connection_test",
		EventStatus:      eventStatus,
		PayloadJSON:      string(payloadRaw),
		MatchedRulesJSON: string(matchesRaw),
		DispatchesJSON:   string(dispatchRaw),
		ReceivedAt:       now,
		Status:           consts.ACTIVE,
		Createtime:       now,
		Updatetime:       now,
	}
	if err := event.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookEvent().Create(ctx, event); err != nil {
		return nil, err
	}
	sourceMap := map[int64]string{source.ID: source.Name}
	return &TestHookSourceResponse{
		Item:  sourceToItem(source),
		Event: eventToItem(event, sourceMap, agentMap),
	}, nil
}

func (s *hookSvc) RedeliverEvent(ctx context.Context, req *RedeliverHookEventRequest) (*RedeliverHookEventResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	event, err := hook_repo.HookEvent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, i18n.NewError(ctx, code.HookEventNotFound)
	}
	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	targetID := req.TargetAgentID
	if targetID <= 0 {
		targetID = defaultAgentID(agentMap)
	}
	if targetID > 0 {
		if _, ok := agentMap[targetID]; !ok {
			return nil, i18n.NewError(ctx, code.HookRuleTargetAgentNotFound)
		}
	}
	dispatches := parseDispatches(event.DispatchesJSON)
	if targetID > 0 {
		dispatches = append(dispatches, HookDispatchItem{
			AgentID:   targetID,
			AgentName: agentMap[targetID].Name,
			SessionID: fmt.Sprintf("pending-%d-%d", event.ID, s.now()),
			Status:    "queued",
			Message:   "Agent runtime dispatch is not enabled yet.",
		})
	}
	raw, _ := json.Marshal(dispatches)
	event.DispatchesJSON = string(raw)
	event.EventStatus = string(hook_entity.EventDispatched)
	event.Updatetime = s.now()
	if err := event.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookEvent().Update(ctx, event); err != nil {
		return nil, err
	}
	sourceName := ""
	if source, err := hook_repo.HookSource().Find(ctx, event.SourceID); err == nil && source != nil {
		sourceName = source.Name
	}
	return &RedeliverHookEventResponse{
		Item: eventToItem(event, map[int64]string{event.SourceID: sourceName}, agentMap),
	}, nil
}

func (s *hookSvc) ensureFallbackRule(ctx context.Context, sourceID int64) error {
	rules, err := hook_repo.HookRule().ListBySource(ctx, sourceID)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.IsFallbackRule() {
			return nil
		}
	}
	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	rule := &hook_entity.HookRule{
		SourceID:      sourceID,
		Name:          "兜底规则",
		ConditionExpr: "未命中任何规则",
		TargetAgentID: defaultAgentID(agentMap),
		Enabled:       1,
		IsFallback:    1,
		SortOrder:     9999,
		Status:        consts.ACTIVE,
		Createtime:    now,
		Updatetime:    now,
	}
	if err := rule.Check(ctx); err != nil {
		return err
	}
	return hook_repo.HookRule().Create(ctx, rule)
}

func (s *hookSvc) requireSource(ctx context.Context, id int64) (*hook_entity.HookSource, error) {
	if id <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	source, err := hook_repo.HookSource().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, i18n.NewError(ctx, code.HookSourceNotFound)
	}
	return source, nil
}

func (s *hookSvc) requireAgentIfSet(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	repo := agent_repo.Agent()
	if repo == nil {
		return nil
	}
	agent, err := repo.Find(ctx, id)
	if err != nil {
		return err
	}
	if agent == nil || !agent.IsActive() {
		return i18n.NewError(ctx, code.HookRuleTargetAgentNotFound)
	}
	return nil
}

func (s *hookSvc) agentOptions(ctx context.Context) ([]*AgentOption, map[int64]*AgentOption, error) {
	repo := agent_repo.Agent()
	if repo == nil {
		return []*AgentOption{}, map[int64]*AgentOption{}, nil
	}
	rows, err := repo.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	items := make([]*AgentOption, 0, len(rows))
	byID := make(map[int64]*AgentOption, len(rows))
	for _, row := range rows {
		if row == nil || !row.IsActive() {
			continue
		}
		color := row.AvatarColor
		if color == "" {
			color = "agent-1"
		}
		item := &AgentOption{
			ID:           row.ID,
			Name:         row.Name,
			AvatarColor:  color,
			SystemBadge:  row.SystemBadge,
			DepartmentID: row.DepartmentID,
		}
		items = append(items, item)
		byID[item.ID] = item
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SystemBadge == "DEFAULT" && items[j].SystemBadge != "DEFAULT" {
			return true
		}
		if items[i].SystemBadge != "DEFAULT" && items[j].SystemBadge == "DEFAULT" {
			return false
		}
		return items[i].ID < items[j].ID
	})
	return items, byID, nil
}

func sourceConnectionStatus(enabled bool) string {
	if !enabled {
		return string(hook_entity.ConnectionDisabled)
	}
	return string(hook_entity.ConnectionPending)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func serializeConfig(cfg SourceConfig) string {
	cfg = normalizeSourceConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func parseSourceConfig(raw string) SourceConfig {
	cfg := SourceConfig{Events: []string{}}
	_ = json.Unmarshal([]byte(raw), &cfg)
	return normalizeSourceConfig(cfg)
}

func normalizeSourceConfig(cfg SourceConfig) SourceConfig {
	if cfg.Events == nil {
		cfg.Events = []string{}
	}
	if cfg.IMAPMailbox == "" {
		cfg.IMAPMailbox = "INBOX"
	}
	if cfg.UseTLS == nil {
		cfg.UseTLS = boolPtr(true)
	}
	return cfg
}

func boolPtr(value bool) *bool {
	return &value
}

func sourceToItem(source *hook_entity.HookSource) *HookSourceItem {
	if source == nil {
		return nil
	}
	cfg := parseSourceConfig(source.ConfigJSON)
	return &HookSourceItem{
		ID:               source.ID,
		Kind:             source.Kind,
		Name:             source.Name,
		Description:      source.Description,
		Identifier:       source.Identifier,
		Config:           sanitizeSourceConfig(cfg),
		Enabled:          source.IsEnabled(),
		ConnectionStatus: source.ConnectionStatus,
		LastSyncTime:     source.LastSyncTime,
		TotalCount:       source.TotalCount,
		Createtime:       source.Createtime,
		Updatetime:       source.Updatetime,
	}
}

func sanitizeSourceConfig(cfg SourceConfig) SourceConfig {
	if strings.TrimSpace(cfg.AppPassword) != "" {
		cfg.AppPassword = maskedSecret
	}
	return cfg
}

func preserveSourceSecrets(next SourceConfig, current SourceConfig) SourceConfig {
	if shouldPreserveSecret(next.AppPassword) {
		next.AppPassword = current.AppPassword
	}
	return next
}

func shouldPreserveSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == maskedSecret
}

func ruleToItem(rule *hook_entity.HookRule, agentMap map[int64]*AgentOption) *HookRuleItem {
	if rule == nil {
		return nil
	}
	targetName := ""
	if agent := agentMap[rule.TargetAgentID]; agent != nil {
		targetName = agent.Name
	}
	return &HookRuleItem{
		ID:              rule.ID,
		SourceID:        rule.SourceID,
		Name:            rule.Name,
		ConditionExpr:   rule.ConditionExpr,
		TargetAgentID:   rule.TargetAgentID,
		TargetAgentName: targetName,
		Enabled:         rule.IsEnabled(),
		IsFallback:      rule.IsFallbackRule(),
		SortOrder:       rule.SortOrder,
		Createtime:      rule.Createtime,
		Updatetime:      rule.Updatetime,
	}
}

func eventToItem(event *hook_entity.HookEvent, sourceMap map[int64]string, agentMap map[int64]*AgentOption) *HookEventItem {
	if event == nil {
		return nil
	}
	matches := parseMatches(event.MatchedRulesJSON)
	dispatches := parseDispatches(event.DispatchesJSON)
	for i := range matches {
		if matches[i].AgentName == "" {
			if agent := agentMap[matches[i].AgentID]; agent != nil {
				matches[i].AgentName = agent.Name
			}
		}
	}
	for i := range dispatches {
		if dispatches[i].AgentName == "" {
			if agent := agentMap[dispatches[i].AgentID]; agent != nil {
				dispatches[i].AgentName = agent.Name
			}
		}
	}
	return &HookEventItem{
		ID:               event.ID,
		SourceID:         event.SourceID,
		SourceName:       sourceMap[event.SourceID],
		Title:            event.Title,
		SourceRef:        event.SourceRef,
		Sender:           event.Sender,
		EventType:        event.EventType,
		EventStatus:      event.EventStatus,
		PayloadJSON:      event.PayloadJSON,
		MatchedRules:     matches,
		Dispatches:       dispatches,
		MatchedRuleNames: matchedRuleNames(matches),
		TargetAgentNames: dispatchTargetNames(dispatches),
		ReceivedAt:       event.ReceivedAt,
		Createtime:       event.Createtime,
		Updatetime:       event.Updatetime,
	}
}

func parseMatches(raw string) []RuleMatchResult {
	var out []RuleMatchResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return []RuleMatchResult{}
	}
	return out
}

func parseDispatches(raw string) []HookDispatchItem {
	var out []HookDispatchItem
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return []HookDispatchItem{}
	}
	return out
}

func matchedRuleNames(matches []RuleMatchResult) []string {
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if !m.Matched || m.RuleName == "" {
			continue
		}
		if _, ok := seen[m.RuleName]; ok {
			continue
		}
		seen[m.RuleName] = struct{}{}
		out = append(out, m.RuleName)
	}
	return out
}

func dispatchTargetNames(dispatches []HookDispatchItem) []string {
	out := make([]string, 0, len(dispatches))
	seen := map[string]struct{}{}
	for _, d := range dispatches {
		if d.AgentName == "" {
			continue
		}
		if _, ok := seen[d.AgentName]; ok {
			continue
		}
		seen[d.AgentName] = struct{}{}
		out = append(out, d.AgentName)
	}
	return out
}

func defaultAgentID(agentMap map[int64]*AgentOption) int64 {
	var first int64
	for id, agent := range agentMap {
		if first == 0 || id < first {
			first = id
		}
		if agent.SystemBadge == "DEFAULT" {
			return id
		}
	}
	return first
}

func evaluateRules(source *hook_entity.HookSource, rules []*hook_entity.HookRule, payload map[string]any, agentMap map[int64]*AgentOption) ([]RuleMatchResult, []HookDispatchItem) {
	fields := map[string]string{
		"source":     source.Name,
		"source_ref": source.Identifier,
		"event_type": stringFromMap(payload, "type"),
		"title":      stringFromMap(payload, "title"),
		"subject":    stringFromMap(payload, "subject"),
		"sender":     stringFromMap(payload, "sender"),
		"from":       stringFromMap(payload, "from"),
		"payload":    compactJSON(payload),
	}
	results := make([]RuleMatchResult, 0, len(rules))
	dispatches := []HookDispatchItem{}
	var fallback *hook_entity.HookRule
	matchedAny := false

	for _, rule := range rules {
		if rule == nil || !rule.IsEnabled() {
			continue
		}
		if rule.IsFallbackRule() {
			fallback = rule
			continue
		}
		matched, reason := matchCondition(rule.ConditionExpr, fields)
		result := RuleMatchResult{
			RuleID:    rule.ID,
			RuleName:  rule.Name,
			Matched:   matched,
			Reason:    reason,
			AgentID:   rule.TargetAgentID,
			AgentName: agentName(agentMap, rule.TargetAgentID),
		}
		results = append(results, result)
		if matched {
			matchedAny = true
			if rule.TargetAgentID > 0 {
				dispatches = append(dispatches, buildQueuedDispatch(rule.TargetAgentID, agentMap))
			}
		}
	}

	if !matchedAny && fallback != nil {
		results = append(results, RuleMatchResult{
			RuleID:    fallback.ID,
			RuleName:  fallback.Name,
			Matched:   true,
			Reason:    "no routing rule matched; fallback selected",
			AgentID:   fallback.TargetAgentID,
			AgentName: agentName(agentMap, fallback.TargetAgentID),
		})
		if fallback.TargetAgentID > 0 {
			dispatches = append(dispatches, buildQueuedDispatch(fallback.TargetAgentID, agentMap))
		}
	}

	return results, dispatches
}

func buildQueuedDispatch(agentID int64, agentMap map[int64]*AgentOption) HookDispatchItem {
	return HookDispatchItem{
		AgentID:   agentID,
		AgentName: agentName(agentMap, agentID),
		Status:    "queued",
		Message:   "Agent runtime dispatch is not enabled yet.",
	}
}

func agentName(agentMap map[int64]*AgentOption, id int64) string {
	if agent := agentMap[id]; agent != nil {
		return agent.Name
	}
	if id > 0 {
		return fmt.Sprintf("Agent #%d", id)
	}
	return ""
}

func stringFromMap(payload map[string]any, key string) string {
	if value, ok := payload[key]; ok {
		return fmt.Sprint(value)
	}
	return ""
}

func compactJSON(payload map[string]any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func matchCondition(expr string, fields map[string]string) (bool, string) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" || strings.EqualFold(trimmed, "always") {
		return true, "empty condition"
	}
	lower := strings.ToLower(trimmed)
	if idx := strings.Index(lower, " contains "); idx > 0 {
		fieldName := strings.TrimSpace(lower[:idx])
		field := strings.ToLower(fields[fieldName])
		rawNeedles := strings.TrimSpace(trimmed[idx+len(" contains "):])
		for _, token := range splitOR(rawNeedles) {
			needle := strings.ToLower(strings.Trim(strings.TrimSpace(token), `"'`))
			if needle == "" {
				continue
			}
			if strings.Contains(field, needle) {
				return true, fmt.Sprintf("%s contains %q", fieldName, needle)
			}
		}
		return false, fmt.Sprintf("%s did not contain requested text", fieldName)
	}
	if idx := strings.Index(lower, "="); idx > 0 {
		fieldName := strings.TrimSpace(lower[:idx])
		want := strings.ToLower(strings.Trim(strings.TrimSpace(trimmed[idx+1:]), `"'`))
		got := strings.ToLower(fields[fieldName])
		if got == want {
			return true, fmt.Sprintf("%s = %q", fieldName, want)
		}
		if _, err := strconv.ParseBool(want); err == nil && got == want {
			return true, fmt.Sprintf("%s = %s", fieldName, want)
		}
		return false, fmt.Sprintf("%s was %q", fieldName, got)
	}
	return false, "condition syntax not supported"
}

func splitOR(raw string) []string {
	parts := strings.Split(raw, " OR ")
	if len(parts) == 1 {
		parts = strings.Split(raw, " or ")
	}
	return parts
}
