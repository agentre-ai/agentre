package chat_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// peerSessionPublication owns the one ordered notification universe for a
// desktop session. It is deliberately in-memory: persisted transcript seeds
// the initial prefix, and subsequent live canonical frames are retained only
// for the running desktop process so reconnects share one dedup universe.
type peerSessionPublication struct {
	mu          sync.Mutex
	history     []wire.EventFrame
	nextSeq     int64
	initialized bool
	subscribers map[string]*peerSessionSubscription
}

type peerSessionSubscription struct {
	subscriber PeerSessionSubscriber
	highWater  int64
	cursor     int64
	pending    []wire.EventFrame
}

func (s *chatSvc) peerPublication(sessionID int64) *peerSessionPublication {
	value, _ := s.peerPublications.LoadOrStore(sessionID, &peerSessionPublication{
		subscribers: map[string]*peerSessionSubscription{},
	})
	return value.(*peerSessionPublication)
}

// PullPeerSession serves the same runtime.session.pull contract used by
// agentred. The subscriber identifies the account connection whose attach
// handoff cursor advances; it is not a new wire field.
func (s *chatSvc) PullPeerSession(_ context.Context, params wire.SessionPullParams, subscriber PeerSessionSubscriber) (wire.SessionPullResult, error) {
	if params.SessionID <= 0 || subscriber == nil {
		return wire.SessionPullResult{}, ErrPeerSessionNotFound
	}
	publication := s.peerPublication(params.SessionID)
	key := peerSubscriberKey(subscriber)
	publication.mu.Lock()
	defer publication.mu.Unlock()

	subscription := publication.subscribers[key]
	if subscription == nil {
		return wire.SessionPullResult{}, ErrPeerSessionNotFound
	}
	limit := clampPeerPullLimit(params.Limit)
	out := wire.SessionPullResult{Cursor: params.Cursor}
	if subscription.highWater > 0 {
		out.OldestSeq = 1
	}
	for _, frame := range publication.history {
		if frame.Seq <= params.Cursor || frame.Seq > subscription.highWater {
			continue
		}
		if len(out.Notifications) == limit {
			out.HasMore = true
			break
		}
		paramsRaw, err := json.Marshal(frame)
		if err != nil {
			return wire.SessionPullResult{}, fmt.Errorf("marshal peer history frame: %w", err)
		}
		out.Notifications = append(out.Notifications, wire.JournaledNotification{
			Seq: frame.Seq, Method: wire.NotifyEvent, Params: paramsRaw,
		})
		out.Cursor = frame.Seq
	}
	if out.Cursor > subscription.cursor {
		subscription.cursor = out.Cursor
	}
	if subscription.cursor >= subscription.highWater {
		s.drainPeerSubscriptionLocked(publication, key, subscription)
	}
	return out, nil
}

func clampPeerPullLimit(limit int) int {
	if limit <= 0 {
		return wire.DefaultSessionPullLimit
	}
	if limit > wire.MaxSessionPullLimit {
		return wire.MaxSessionPullLimit
	}
	return limit
}

func (s *chatSvc) attachPeerTranscript(ctx context.Context, sessionID int64, subscriber PeerSessionSubscriber) (int64, func(), error) {
	publication := s.peerPublication(sessionID)
	key := peerSubscriberKey(subscriber)
	// Holding this lock across the initial repository read makes the synthesized
	// prefix and registration one publication boundary: a live event is either
	// in 1..H or assigned after H and buffered for this subscriber.
	publication.mu.Lock()
	if !publication.initialized {
		messages, err := chat_repo.Message().List(ctx, sessionID)
		if err != nil {
			publication.mu.Unlock()
			return 0, nil, operationFailedWithCause(ctx, err)
		}
		history, err := synthesizePeerHistory(sessionID, messages)
		if err != nil {
			publication.mu.Unlock()
			return 0, nil, fmt.Errorf("synthesize desktop peer history: %w", err)
		}
		for index := range history {
			history[index].Seq = int64(index + 1)
		}
		publication.history = history
		publication.nextSeq = int64(len(history))
		publication.initialized = true
	}
	highWater := publication.nextSeq
	subscription := &peerSessionSubscription{subscriber: subscriber, highWater: highWater}
	publication.subscribers[key] = subscription
	publication.mu.Unlock()

	var once sync.Once
	detach := func() {
		once.Do(func() {
			publication.mu.Lock()
			if publication.subscribers[key] == subscription {
				delete(publication.subscribers, key)
			}
			publication.mu.Unlock()
		})
	}
	return highWater, detach, nil
}

func (s *chatSvc) publishPeerEvent(sessionID int64, event agentruntime.Event) {
	if sessionID <= 0 || event == nil {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.publishPeerEventRaw(sessionID, raw)
}

func (s *chatSvc) publishPeerEventRaw(sessionID int64, raw json.RawMessage) {
	if sessionID <= 0 || len(raw) == 0 {
		return
	}
	value, ok := s.peerPublications.Load(sessionID)
	if !ok {
		return
	}
	publication := value.(*peerSessionPublication)
	publication.mu.Lock()
	defer publication.mu.Unlock()
	publication.nextSeq++
	frame := wire.EventFrame{SessionID: sessionID, Event: append(json.RawMessage(nil), raw...), Seq: publication.nextSeq}
	publication.history = append(publication.history, frame)
	for key, subscription := range publication.subscribers {
		if subscription.cursor < subscription.highWater {
			subscription.pending = append(subscription.pending, frame)
			continue
		}
		if err := subscription.subscriber.Notify(wire.NotifyEvent, frame); err != nil {
			delete(publication.subscribers, key)
		}
	}
}

func (s *chatSvc) drainPeerSubscriptionLocked(publication *peerSessionPublication, key string, subscription *peerSessionSubscription) {
	for _, frame := range subscription.pending {
		if err := subscription.subscriber.Notify(wire.NotifyEvent, frame); err != nil {
			delete(publication.subscribers, key)
			return
		}
	}
	subscription.pending = nil
}

func peerSubscriberKey(subscriber PeerSessionSubscriber) string {
	if keyer, ok := subscriber.(PeerSessionSubscriberKeyer); ok && keyer.PeerSessionSubscriberKey() != "" {
		return keyer.PeerSessionSubscriberKey()
	}
	value := reflect.ValueOf(subscriber)
	if value.IsValid() && value.Kind() == reflect.Pointer && !value.IsNil() {
		return fmt.Sprintf("%T:%x", subscriber, value.Pointer())
	}
	return fmt.Sprintf("%T:%v", subscriber, subscriber)
}

func synthesizePeerHistory(sessionID int64, messages []*chat_entity.Message) ([]wire.EventFrame, error) {
	sorted := append([]*chat_entity.Message(nil), messages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i] == nil {
			return false
		}
		if sorted[j] == nil {
			return true
		}
		return sorted[i].Seq < sorted[j].Seq
	})
	frames := make([]wire.EventFrame, 0)
	appendEvent := func(event agentruntime.Event) error {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		frames = append(frames, wire.EventFrame{SessionID: sessionID, Event: raw})
		return nil
	}
	appendRaw := func(raw any) error {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		frames = append(frames, wire.EventFrame{SessionID: sessionID, Event: encoded})
		return nil
	}
	for _, message := range sorted {
		if message == nil {
			continue
		}
		var stored []cagoblocks.StoredBlock
		if err := json.Unmarshal([]byte(message.BlocksJSON), &stored); err != nil {
			return nil, fmt.Errorf("message %d blocks: %w", message.ID, err)
		}
		for _, block := range stored {
			if message.Role == "assistant" && block.Type == "user_ask" {
				var data chatblocks.UserAskBlock
				if err := json.Unmarshal(block.Data, &data); err != nil {
					return nil, err
				}
				if err := appendEvent(agentruntime.UserAskRequest{RequestID: data.RequestID, ToolCallID: data.ToolCallID, Questions: peerQuestions(data.Questions)}); err != nil {
					return nil, err
				}
				if data.Answered || data.Skipped {
					if err := appendEvent(agentruntime.UserAskResolved{RequestID: data.RequestID, Answers: peerAnswers(data.Answers), Skipped: data.Skipped}); err != nil {
						return nil, err
					}
				}
				continue
			}
			if message.Role == "assistant" && block.Type == "subagent_state" {
				var data chatblocks.SubagentStateBlock
				if err := json.Unmarshal(block.Data, &data); err != nil {
					return nil, err
				}
				if err := appendEvent(agentruntime.SubagentDone{ToolCallID: data.ParentToolCallID, Info: agentruntime.SubagentInfo{
					TaskID: data.TaskID, Kind: data.Kind, TaskDescription: data.Description, LastToolName: data.LastToolName,
					ToolUses: data.ToolUses, TotalTokens: data.TotalTokens, DurationMs: data.DurationMs, Status: data.Status,
					Mode: data.Mode, Runs: data.Runs,
				}}); err != nil {
					return nil, err
				}
				if data.Model != "" {
					if err := appendEvent(agentruntime.SubagentModel{ToolCallID: data.ParentToolCallID, Model: data.Model}); err != nil {
						return nil, err
					}
				}
				continue
			}
			if message.Role == "assistant" && block.Type == "tool_permission" {
				var data chatblocks.ToolPermissionBlock
				if err := json.Unmarshal(block.Data, &data); err != nil {
					return nil, err
				}
				input, err := json.Marshal(data.ToolInput)
				if err != nil {
					return nil, err
				}
				if err := appendEvent(agentruntime.ToolPermissionRequest{RequestID: data.RequestID, ToolCallID: data.ToolCallID, ToolName: data.ToolName, Input: input}); err != nil {
					return nil, err
				}
				if data.Resolved {
					if err := appendEvent(agentruntime.ToolPermissionResolved{RequestID: data.RequestID, Allowed: data.Allowed, AlwaysAllow: data.AlwaysAllow, DenyReason: data.DenyReason}); err != nil {
						return nil, err
					}
				}
				continue
			}
			if event, ok, err := peerEventForStoredBlock(message, block); err != nil {
				return nil, err
			} else if ok {
				if err := appendEvent(event); err != nil {
					return nil, err
				}
			} else if err := appendRaw(struct {
				Kind  string                 `json:"kind"`
				Block cagoblocks.StoredBlock `json:"block"`
			}{Kind: "unrecognized_block", Block: block}); err != nil {
				return nil, err
			}
		}
		if message.Role == "assistant" {
			if message.PromptTokens != 0 || message.CompletionTokens != 0 || message.CachedTokens != 0 || message.CacheCreationTokens != 0 || message.ReasoningTokens != 0 || message.TotalInputTokens != 0 {
				if err := appendEvent(agentruntime.UsageUpdate{Usage: &provider.Usage{
					PromptTokens: message.PromptTokens, CompletionTokens: message.CompletionTokens,
					CachedTokens: message.CachedTokens, CacheCreationTokens: message.CacheCreationTokens,
					ReasoningTokens: message.ReasoningTokens,
				}, TotalInputTokens: message.TotalInputTokens}); err != nil {
					return nil, err
				}
			}
			if message.ErrorText != "" {
				if err := appendEvent(agentruntime.ErrorEvent{Err: errors.New(message.ErrorText)}); err != nil {
					return nil, err
				}
			}
			if err := appendEvent(agentruntime.Done{}); err != nil {
				return nil, err
			}
		}
	}
	return frames, nil
}

func peerEventForStoredBlock(message *chat_entity.Message, block cagoblocks.StoredBlock) (agentruntime.Event, bool, error) {
	if message.Role == "user" && (block.Type == "text" || block.Type == "display_text") {
		var data struct {
			Text             string `json:"text"`
			SourceDevice     string `json:"sourceDevice"`
			SourceDeviceName string `json:"sourceDeviceName"`
		}
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.UserMessageEvent{
			Text: data.Text, SourceDevice: data.SourceDevice, SourceDeviceName: data.SourceDeviceName,
		}, true, nil
	}
	if message.Role != "assistant" {
		return nil, false, nil
	}
	switch block.Type {
	case "text", "display_text":
		var data struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.TextDelta{Text: data.Text}, true, nil
	case "thinking":
		var data struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.ThinkingDelta{Text: data.Text}, true, nil
	case "tool_use":
		var data struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.ToolCall{ID: data.ID, Name: data.Name, Input: data.Input}, true, nil
	case "tool_result":
		var data struct {
			ToolUseID string                   `json:"tool_use_id"`
			Content   []cagoblocks.StoredBlock `json:"content"`
			IsError   bool                     `json:"is_error"`
		}
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.ToolResult{ToolCallID: data.ToolUseID, Content: peerTextFromStoredBlocks(data.Content), IsError: data.IsError}, true, nil
	case "permission_mode_change":
		var data chatblocks.PermissionModeChangeBlock
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.PermissionModeChanged{Mode: data.To}, true, nil
	case "plan":
		var data PlanBlock
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		steps := make([]canonical.PlanStep, 0, len(data.Steps))
		for _, step := range data.Steps {
			steps = append(steps, canonical.PlanStep{Step: step.Step, Status: canonical.PlanStepStatus(step.Status)})
		}
		return agentruntime.PlanUpdated{Plan: canonical.PlanUpdate{Steps: steps, Text: data.Text, Actions: data.Actions}}, true, nil
	case "compact_boundary":
		var data chatblocks.CompactBoundaryBlock
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		return agentruntime.CompactBoundary{PreTokens: data.PreTokens, Trigger: data.Trigger}, true, nil
	case "exec_approval":
		var data chatblocks.ExecApprovalBlock
		if err := json.Unmarshal(block.Data, &data); err != nil {
			return nil, false, err
		}
		if data.Status == "resolved" || data.Status == "expired" {
			return agentruntime.ExecApprovalResolved{ID: data.ID, Status: data.Status, Decision: data.Decision, ResolvedBy: data.ResolvedBy, ResolvedAtMs: data.ResolvedAtMs}, true, nil
		}
		return agentruntime.ExecApprovalRequested{ID: data.ID, CommandText: data.CommandText, CommandPreview: data.CommandPreview, AllowedDecisions: data.AllowedDecisions, Host: data.Host, NodeID: data.NodeID, AgentID: data.AgentID, CreatedAtMs: data.CreatedAtMs, ExpiresAtMs: data.ExpiresAtMs}, true, nil
	default:
		return nil, false, nil
	}
}

func peerTextFromStoredBlocks(blocks []cagoblocks.StoredBlock) string {
	var out strings.Builder
	for _, block := range blocks {
		if block.Type != "text" && block.Type != "display_text" {
			continue
		}
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(block.Data, &data) == nil {
			out.WriteString(data.Text)
		}
	}
	return out.String()
}

func peerQuestions(in []chatblocks.AskQuestionDTO) []agentruntime.AskQuestion {
	out := make([]agentruntime.AskQuestion, 0, len(in))
	for _, question := range in {
		options := make([]agentruntime.AskOption, 0, len(question.Options))
		for _, option := range question.Options {
			options = append(options, agentruntime.AskOption{Label: option.Label, Description: option.Description, Preview: option.Preview})
		}
		out = append(out, agentruntime.AskQuestion{ID: question.ID, Question: question.Question, Header: question.Header, MultiSelect: question.MultiSelect, IsOther: question.IsOther, IsSecret: question.IsSecret, Options: options})
	}
	return out
}

func peerAnswers(in []chatblocks.AskAnswerDTO) []agentruntime.AskAnswer {
	out := make([]agentruntime.AskAnswer, 0, len(in))
	for _, answer := range in {
		out = append(out, agentruntime.AskAnswer{QuestionIndex: answer.QuestionIndex, Labels: answer.Labels, OtherText: answer.OtherText})
	}
	return out
}
