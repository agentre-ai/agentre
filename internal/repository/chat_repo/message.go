package chat_repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
)

// sessionMutexes 是 per-session 的 read-modify-write 锁。FlipSubagentStatus 与
// AppendSubagentChildren 在同一会话里并发对同一条 launch 消息行做「Find → 改写 →
// Update」时,按会话粒度串行化,避免互相覆盖对方的写入。
// key: sessionID(int64),value: *sync.Mutex。
var sessionMutexes sync.Map

// lockForSession 返回会话 ID 对应的 *sync.Mutex。锁在会话存续期间常驻(不被 GC);
// 会话数实践上有限,不构成问题。
func lockForSession(sessionID int64) *sync.Mutex {
	v, _ := sessionMutexes.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

//go:generate mockgen -source message.go -destination mock_chat_repo/mock_message.go

type MessageRepo interface {
	List(ctx context.Context, sessionID int64) ([]*chat_entity.Message, error)
	Find(ctx context.Context, id int64) (*chat_entity.Message, error)
	NextSeq(ctx context.Context, sessionID int64) (int, error)
	Create(ctx context.Context, m *chat_entity.Message) error
	Update(ctx context.Context, m *chat_entity.Message) error
	// DeleteFromSeq 删除指定 session 下 seq >= fromSeq 的所有消息，返回被删除的行数。
	// 用于「从第 N 条消息开始重新生成」时一次性截断后续记录。
	DeleteFromSeq(ctx context.Context, sessionID int64, fromSeq int) (int64, error)
	// FlipSubagentStatus 定向把本会话里 parent_tool_call_id==toolUseID 的 subagent_state
	// 块状态改成 status(后台 bash 在之后的自主轮才完成,无法走 per-turn accumulator)。
	// summary 非空时同时写入块的 summary 字段。找不到则静默返回 nil(任务可能已 evict / 非本会话)。
	FlipSubagentStatus(ctx context.Context, sessionID int64, toolUseID, status, summary string) error
	// PatchSubagentProgress 定向把 parent_tool_call_id==toolUseID 的 subagent_state 块的
	// 运行时进度字段更新为最新快照。后台 subagent 在会话**空闲态**跑,发起它的那一轮
	// accumulator 早已收尾,进度只能这样落回发起消息(否则重开会话看到的永远是派遣那一刻
	// 的旧数字)。p 的零值字段跳过;全零 / 找不到命中块则静默返回 nil。
	PatchSubagentProgress(ctx context.Context, sessionID int64, toolUseID string, p SubagentProgress) error
	// AppendSubagentChildren 把后台 subagent 内部产生的子块追加进发起消息里对应
	// subagent_state 块的 nested_tool_call_ids,同时把 childBlocksJSON 里的 StoredBlock
	// 追加到该消息 blocks_json 的末尾。childIDs 自动去重(跳过已在数组中的 id)。
	// 找不到命中块则静默返回 nil。
	AppendSubagentChildren(ctx context.Context, sessionID int64, parentToolUseID, childBlocksJSON string, childIDs []string) error
	// FindAssistantBySubagentToolUseID 倒序扫描最近 N 条 assistant 消息,返回第一条 blocks
	// 含 type=="subagent_state" 且 data.parent_tool_call_id==toolUseID 的消息(后台 subagent
	// 的发起卡所在消息)。toolUseID 空 / 无命中 / 该会话没有这类消息时返回 (nil, nil)。
	// 仅读取定位,不改写。
	FindAssistantBySubagentToolUseID(ctx context.Context, sessionID int64, toolUseID string) (*chat_entity.Message, error)
	// LatestAssistant 取某会话 seq 最大的一条 assistant 消息(无 → nil,nil)。
	// 用于 peek 运行中子任务的当前输出（read 工具的 running 分支）。
	LatestAssistant(ctx context.Context, sessionID int64) (*chat_entity.Message, error)
	// FindSubagentState 按 parent_tool_call_id==toolUseID 定位本会话里的 subagent_state
	// 块,返回它记录的 CLI task_id + 当前 status(供 StopBackgroundTask 下发 stop_task)。
	// 定位方式同 FlipSubagentStatus(blocks_json LIKE toolUseID),只读不改。
	// toolUseID 空 / 无命中时返回 (found=false)。
	FindSubagentState(ctx context.Context, sessionID int64, toolUseID string) (taskID, status string, found bool, err error)
}

// flipSubagentScanLimit 是 FlipSubagentStatus 倒序扫描的最近 assistant 消息条数上限。
// 后台 bash 完成的自主轮通常紧跟在发起它的那条消息之后,近窗足以命中。
const flipSubagentScanLimit = 50

var defaultMessage MessageRepo

func Message() MessageRepo             { return defaultMessage }
func RegisterMessage(impl MessageRepo) { defaultMessage = impl }
func NewMessage() MessageRepo          { return &messageRepo{} }

type messageRepo struct{}

// ReplacementRecoveryState is persisted on the hidden recovery marker. Pending
// means Start has not acknowledged the prompt and must restore; acknowledged
// means the active generation is committed and only hidden originals may be removed.
type ReplacementRecoveryState string

const (
	ReplacementRecoveryPending      ReplacementRecoveryState = "pending"
	ReplacementRecoveryAcknowledged ReplacementRecoveryState = "acknowledged"

	replacementRecoveryRole = "__agentre_pi_recovery__"
)

var (
	ErrReplacementNamespaceCollision = errors.New("replacement recovery namespace is already occupied")
	ErrReplacementOwnershipLost      = errors.New("replacement recovery no longer owns the active session")
)

// ReplacementRecovery owns one exact Pi replacement generation. The original
// rows retain their IDs and contents under RecoverySessionID until Start is
// acknowledged; the active IDs prevent stale restoration from deleting a retry.
type ReplacementRecovery struct {
	MarkerID             int64
	RecoverySessionID    int64
	SessionID            int64
	FromSeq              int
	RequestMessageID     int64
	UserMessageID        int64
	AssistantMessageID   int64
	OldProviderSessionID string
	NewProviderSessionID string
	OldAgentStatus       string
	OldLastMessageAt     int64
	State                ReplacementRecoveryState
}

type replacementRecoveryPayload struct {
	SessionID            int64  `json:"sessionId"`
	FromSeq              int    `json:"fromSeq"`
	RequestMessageID     int64  `json:"requestMessageId"`
	UserMessageID        int64  `json:"userMessageId"`
	AssistantMessageID   int64  `json:"assistantMessageId"`
	OldProviderSessionID string `json:"oldProviderSessionId"`
	NewProviderSessionID string `json:"newProviderSessionId"`
	OldAgentStatus       string `json:"oldAgentStatus"`
	OldLastMessageAt     int64  `json:"oldLastMessageAt"`
}

// ReplacementStageSessionID is the legacy private staging namespace. New Pi
// replacements use a generation-owned recovery namespace instead.
func ReplacementStageSessionID(sessionID int64) int64 {
	return -sessionID
}

// ReplacementRecoverySessionID maps a globally unique marker row ID into a
// private negative namespace. The odd mapping keeps the same marker/session ID
// from colliding with the legacy -sessionID stage.
func ReplacementRecoverySessionID(markerID int64) (int64, error) {
	if markerID <= 0 || markerID > (math.MaxInt64-1)/2 {
		return 0, fmt.Errorf("invalid replacement recovery marker ID %d", markerID)
	}
	return -(markerID*2 + 1), nil
}

func NewReplacementRecoveryMarker(recovery *ReplacementRecovery) (*chat_entity.Message, error) {
	if recovery == nil || recovery.SessionID <= 0 || recovery.FromSeq < 0 || recovery.RequestMessageID <= 0 {
		return nil, errors.New("invalid replacement recovery")
	}
	if recovery.State != ReplacementRecoveryPending && recovery.State != ReplacementRecoveryAcknowledged {
		return nil, errors.New("invalid replacement recovery state")
	}
	payload, err := json.Marshal(replacementRecoveryPayload{
		SessionID:            recovery.SessionID,
		FromSeq:              recovery.FromSeq,
		RequestMessageID:     recovery.RequestMessageID,
		UserMessageID:        recovery.UserMessageID,
		AssistantMessageID:   recovery.AssistantMessageID,
		OldProviderSessionID: recovery.OldProviderSessionID,
		NewProviderSessionID: recovery.NewProviderSessionID,
		OldAgentStatus:       recovery.OldAgentStatus,
		OldLastMessageAt:     recovery.OldLastMessageAt,
	})
	if err != nil {
		return nil, err
	}
	return &chat_entity.Message{
		SessionID:  recovery.RecoverySessionID,
		DeviceID:   strconv.FormatInt(recovery.SessionID, 10),
		Role:       replacementRecoveryRole,
		BlocksJSON: string(payload),
		Model:      string(recovery.State),
		Seq:        -1,
	}, nil
}

func ParseReplacementRecoveryMarker(marker *chat_entity.Message) (*ReplacementRecovery, error) {
	if marker == nil || marker.ID <= 0 || marker.SessionID >= 0 || marker.Role != replacementRecoveryRole {
		return nil, errors.New("invalid replacement recovery marker")
	}
	expectedSessionID, err := ReplacementRecoverySessionID(marker.ID)
	if err != nil || marker.SessionID != expectedSessionID {
		return nil, errors.New("replacement recovery namespace does not own marker")
	}
	state := ReplacementRecoveryState(marker.Model)
	if state != ReplacementRecoveryPending && state != ReplacementRecoveryAcknowledged {
		return nil, errors.New("invalid replacement recovery marker state")
	}
	var payload replacementRecoveryPayload
	if err := json.Unmarshal([]byte(marker.BlocksJSON), &payload); err != nil {
		return nil, fmt.Errorf("decode replacement recovery marker: %w", err)
	}
	markerSessionID, err := strconv.ParseInt(marker.DeviceID, 10, 64)
	if err != nil || markerSessionID != payload.SessionID || payload.SessionID <= 0 ||
		payload.FromSeq < 0 || payload.RequestMessageID <= 0 {
		return nil, errors.New("invalid replacement recovery ownership")
	}
	return &ReplacementRecovery{
		MarkerID:             marker.ID,
		RecoverySessionID:    marker.SessionID,
		SessionID:            payload.SessionID,
		FromSeq:              payload.FromSeq,
		RequestMessageID:     payload.RequestMessageID,
		UserMessageID:        payload.UserMessageID,
		AssistantMessageID:   payload.AssistantMessageID,
		OldProviderSessionID: payload.OldProviderSessionID,
		NewProviderSessionID: payload.NewProviderSessionID,
		OldAgentStatus:       payload.OldAgentStatus,
		OldLastMessageAt:     payload.OldLastMessageAt,
		State:                state,
	}, nil
}

func (r *messageRepo) List(ctx context.Context, sessionID int64) ([]*chat_entity.Message, error) {
	var rows []*chat_entity.Message
	err := db.Ctx(ctx).
		Where("session_id = ?", sessionID).
		Order("seq ASC").
		Find(&rows).Error
	return rows, err
}

func (r *messageRepo) NextSeq(ctx context.Context, sessionID int64) (int, error) {
	var next int
	err := db.Ctx(ctx).
		Table("chat_messages").
		Select("COALESCE(MAX(seq), 0) + 1").
		Where("session_id = ?", sessionID).
		Row().Scan(&next)
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (r *messageRepo) Create(ctx context.Context, m *chat_entity.Message) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *messageRepo) Update(ctx context.Context, m *chat_entity.Message) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *messageRepo) Find(ctx context.Context, id int64) (*chat_entity.Message, error) {
	var m chat_entity.Message
	if err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *messageRepo) LatestAssistant(ctx context.Context, sessionID int64) (*chat_entity.Message, error) {
	var m chat_entity.Message
	err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").
		Limit(1).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *messageRepo) FindSubagentState(ctx context.Context, sessionID int64, toolUseID string) (string, string, bool, error) {
	if toolUseID == "" {
		return "", "", false, nil
	}
	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ? AND blocks_json LIKE ?", sessionID, "assistant", "%"+toolUseID+"%").
		Order("seq DESC").
		Find(&rows).Error; err != nil {
		return "", "", false, err
	}
	for _, msg := range rows {
		taskID, status, found, err := FindSubagentStateInBlocksJSON(msg.BlocksJSON, toolUseID)
		if err != nil {
			logger.Ctx(ctx).Warn("chat_repo.FindSubagentState: decode blocks failed; skipping message",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if found {
			return taskID, status, true, nil
		}
	}
	return "", "", false, nil
}

// FindSubagentStateInBlocksJSON 在 blocks_json(StoredBlock 数组)里找 type=="subagent_state"
// 且 data.parent_tool_call_id==toolUseID 的块,返回它的 task_id + status。遵循 FlipSubagentIn
// BlocksJSON 相同的 UseNumber 解码纪律。导出以便直接单测。
func FindSubagentStateInBlocksJSON(blocksJSON, toolUseID string) (string, string, bool, error) {
	if blocksJSON == "" {
		return "", "", false, nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(blocksJSON), &stored); err != nil {
		return "", "", false, err
	}
	for i := range stored {
		if stored[i].Type != "subagent_state" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(stored[i].Data))
		dec.UseNumber()
		var data map[string]any
		if err := dec.Decode(&data); err != nil {
			return "", "", false, err
		}
		if parent, _ := data["parent_tool_call_id"].(string); parent != toolUseID {
			continue
		}
		taskID, _ := data["task_id"].(string)
		status, _ := data["status"].(string)
		return taskID, status, true, nil
	}
	return "", "", false, nil
}

// rewriteSubagentMessage 按 blocks_json LIKE toolUseID 定位后台任务的发起消息,把第一条
// 被 rewrite 改写的行写回库。rewrite 返回 (重写后的 JSON, 是否命中, error)。
//
// 用 LIKE 而非 seq DESC LIMIT N 近因盲扫:后台任务完成是跨轮的,长会话里发起消息可能已
// 滑出近 N 条窗口 → 旧实现漏掉它,DB 永远卡 running(bug #2)。toolUseID 是长且唯一的串,
// LIKE 只会命中发起消息(即便偶有子串误命中,该行不含匹配 subagent_state 时 rewrite 自然
// 返回未命中)。没有任何命中时静默返回 nil:任务可能已 evict / 非本会话。
func (r *messageRepo) rewriteSubagentMessage(
	ctx context.Context, sessionID int64, toolUseID, caller string,
	rewrite func(blocksJSON string) (string, bool, error),
) error {
	// serialize read-modify-write per session to avoid lost-update races between
	// FlipSubagentStatus / PatchSubagentProgress / AppendSubagentChildren.
	mu := lockForSession(sessionID)
	mu.Lock()
	defer mu.Unlock()

	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ? AND blocks_json LIKE ?", sessionID, "assistant", "%"+toolUseID+"%").
		Order("seq DESC").
		Find(&rows).Error; err != nil {
		return err
	}

	for _, msg := range rows {
		rewritten, matched, err := rewrite(msg.BlocksJSON)
		if err != nil {
			// 单条消息 blocks 损坏不应阻断其它消息;跳过继续找。
			logger.Ctx(ctx).Warn(caller+": decode blocks failed; skipping message",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if !matched {
			continue
		}
		msg.BlocksJSON = rewritten
		return r.Update(ctx, msg)
	}
	return nil
}

func (r *messageRepo) FlipSubagentStatus(ctx context.Context, sessionID int64, toolUseID, status, summary string) error {
	if toolUseID == "" || status == "" {
		return nil
	}
	logger.Ctx(ctx).Info("chat_repo.FlipSubagentStatus: flipping subagent_state status",
		zap.Int64("sessionId", sessionID), zap.String("toolUseId", toolUseID), zap.String("status", status))

	return r.rewriteSubagentMessage(ctx, sessionID, toolUseID, "chat_repo.FlipSubagentStatus",
		func(blocksJSON string) (string, bool, error) {
			return FlipSubagentInBlocksJSON(blocksJSON, toolUseID, status, summary)
		})
}

func (r *messageRepo) PatchSubagentProgress(ctx context.Context, sessionID int64, toolUseID string, p SubagentProgress) error {
	if toolUseID == "" || p.IsZero() {
		return nil
	}
	return r.rewriteSubagentMessage(ctx, sessionID, toolUseID, "chat_repo.PatchSubagentProgress",
		func(blocksJSON string) (string, bool, error) {
			return PatchSubagentProgressInBlocksJSON(blocksJSON, toolUseID, p)
		})
}

// patchSubagentStateInBlocksJSON 在 blocks_json(StoredBlock 数组)里遍历每个
// type=="subagent_state" 且 data.parent_tool_call_id==toolUseID 的块,交给 apply 就地
// 改写它的 data(apply 返回 false 表示这块没动),返回重写后的 JSON + 是否有块被改写。
// repo 层不依赖 service 的 block 类型,只按 StoredBlock 信封 + 该块的少数已知字段操作;
// 未被 apply 触碰的字段原样保留。
//
// 解 data 用 json.Decoder + UseNumber():数字字段(total_tokens / duration_ms /
// tool_uses)保持 json.Number,避免经 map[string]any 的 float64 强转把整数重写成
// 科学计数(如 1e+04)。
func patchSubagentStateInBlocksJSON(blocksJSON, toolUseID string, apply func(data map[string]any) bool) (string, bool, error) {
	if blocksJSON == "" {
		return blocksJSON, false, nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(blocksJSON), &stored); err != nil {
		return blocksJSON, false, err
	}
	patched := false
	for i := range stored {
		if stored[i].Type != "subagent_state" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(stored[i].Data))
		dec.UseNumber()
		var data map[string]any
		if err := dec.Decode(&data); err != nil {
			return blocksJSON, false, err
		}
		if parent, _ := data["parent_tool_call_id"].(string); parent != toolUseID {
			continue
		}
		if !apply(data) {
			continue
		}
		buf, err := json.Marshal(data)
		if err != nil {
			return blocksJSON, false, err
		}
		stored[i].Data = buf
		patched = true
	}
	if !patched {
		return blocksJSON, false, nil
	}
	out, err := json.Marshal(stored)
	if err != nil {
		return blocksJSON, false, err
	}
	return string(out), true, nil
}

// FlipSubagentInBlocksJSON 在 blocks_json 里就地翻转 type=="subagent_state" 且
// data.parent_tool_call_id==toolUseID 的块的 status,返回重写后的 JSON + 是否命中。
// summary 非空时同时写入命中块的 summary 字段。只触碰 status / summary 两个字段。
// 导出以便直接单测 JSON 改写逻辑。
func FlipSubagentInBlocksJSON(blocksJSON, toolUseID, status, summary string) (string, bool, error) {
	return patchSubagentStateInBlocksJSON(blocksJSON, toolUseID, func(data map[string]any) bool {
		data["status"] = status
		if summary != "" {
			data["summary"] = summary
		}
		return true
	})
}

// SubagentProgress 是 subagent_state 块的运行时进度快照。零值字段代表「这一帧没带这项」,
// patch 时跳过 —— CLI 的 task_progress 偶尔缺 usage,不该把已经攒起来的工具数 / token
// 抹回 0。
type SubagentProgress struct {
	TotalTokens  int
	ToolUses     int
	DurationMs   int
	LastToolName string
	// Model 是子代理内部帧解析出的实际模型(R2)。first-wins 在上游 SubagentModelHandler
	// 已经保证,这里只是把它跟其它进度字段一起搬进跨轮补丁;空值代表"这一轮没有新模型
	// 可写",patch 时跳过,不抹掉已记录的值。
	Model string
}

// IsZero 表示这份快照没带任何进度信息,调用方可据此跳过一次读-改-写。
func (p SubagentProgress) IsZero() bool {
	return p.TotalTokens == 0 && p.ToolUses == 0 && p.DurationMs == 0 && p.LastToolName == "" && p.Model == ""
}

// PatchSubagentProgressInBlocksJSON 在 blocks_json 里就地更新命中 subagent_state 块的
// 进度字段,返回重写后的 JSON + 是否改写过。零值字段跳过(见 SubagentProgress)。
// 导出以便直接单测 JSON 改写逻辑。
func PatchSubagentProgressInBlocksJSON(blocksJSON, toolUseID string, p SubagentProgress) (string, bool, error) {
	if p.IsZero() {
		return blocksJSON, false, nil
	}
	return patchSubagentStateInBlocksJSON(blocksJSON, toolUseID, func(data map[string]any) bool {
		if p.TotalTokens > 0 {
			data["total_tokens"] = json.Number(strconv.Itoa(p.TotalTokens))
		}
		if p.ToolUses > 0 {
			data["tool_uses"] = json.Number(strconv.Itoa(p.ToolUses))
		}
		if p.DurationMs > 0 {
			data["duration_ms"] = json.Number(strconv.Itoa(p.DurationMs))
		}
		if p.LastToolName != "" {
			data["last_tool_name"] = p.LastToolName
		}
		if p.Model != "" {
			data["model"] = p.Model
		}
		return true
	})
}

func (r *messageRepo) AppendSubagentChildren(ctx context.Context, sessionID int64, parentToolUseID, childBlocksJSON string, childIDs []string) error {
	if parentToolUseID == "" || childBlocksJSON == "" {
		return nil
	}
	// serialize read-modify-write per session to avoid lost-update races with FlipSubagentStatus.
	mu := lockForSession(sessionID)
	mu.Lock()
	defer mu.Unlock()

	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").Limit(flipSubagentScanLimit).Find(&rows).Error; err != nil {
		return err
	}
	for _, msg := range rows {
		rewritten, ok, err := AppendSubagentChildrenInBlocksJSON(msg.BlocksJSON, parentToolUseID, childBlocksJSON, childIDs)
		if err != nil {
			logger.Ctx(ctx).Warn("chat_repo.AppendSubagentChildren: decode blocks failed; skipping",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if !ok {
			continue
		}
		msg.BlocksJSON = rewritten
		return r.Update(ctx, msg)
	}
	return nil
}

// AppendSubagentChildrenInBlocksJSON 在 blocks_json(StoredBlock 数组)里找到
// type=="subagent_state" 且 data.parent_tool_call_id==parentToolUseID 的块,
// 把 childIDs 追加到其 nested_tool_call_ids(去重),并把 childBlocksJSON 里的
// StoredBlock 追加到顶层数组末尾,返回重写后的 JSON + 是否命中。
//
// 遵循和 FlipSubagentInBlocksJSON 相同的 UseNumber 纪律:data map 用
// json.Decoder+UseNumber() 解码,防止整数字段被 float64 强转后重写成科学计数。
func AppendSubagentChildrenInBlocksJSON(blocksJSON, parentToolUseID, childBlocksJSON string, childIDs []string) (string, bool, error) {
	if blocksJSON == "" {
		return blocksJSON, false, nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(blocksJSON), &stored); err != nil {
		return blocksJSON, false, err
	}
	matched := false
	for i := range stored {
		if stored[i].Type != "subagent_state" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(stored[i].Data))
		dec.UseNumber()
		var data map[string]any
		if err := dec.Decode(&data); err != nil {
			return blocksJSON, false, err
		}
		if parent, _ := data["parent_tool_call_id"].(string); parent != parentToolUseID {
			continue
		}
		// 去重追加 childIDs。
		existing := map[string]bool{}
		if arr, ok := data["nested_tool_call_ids"].([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					existing[s] = true
				}
			}
		}
		ids, _ := data["nested_tool_call_ids"].([]any)
		for _, id := range childIDs {
			if !existing[id] {
				ids = append(ids, id)
				existing[id] = true
			}
		}
		data["nested_tool_call_ids"] = ids
		buf, err := json.Marshal(data)
		if err != nil {
			return blocksJSON, false, err
		}
		stored[i].Data = buf
		matched = true
		break
	}
	if !matched {
		return blocksJSON, false, nil
	}
	// 追加子块到顶层数组末尾。
	if childBlocksJSON != "" && childBlocksJSON != "[]" {
		var childBlocks []cagoblocks.StoredBlock
		if err := json.Unmarshal([]byte(childBlocksJSON), &childBlocks); err != nil {
			return blocksJSON, false, err
		}
		stored = append(stored, childBlocks...)
	}
	out, err := json.Marshal(stored)
	if err != nil {
		return blocksJSON, false, err
	}
	return string(out), true, nil
}

func (r *messageRepo) FindAssistantBySubagentToolUseID(ctx context.Context, sessionID int64, toolUseID string) (*chat_entity.Message, error) {
	if toolUseID == "" {
		return nil, nil
	}
	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").Limit(flipSubagentScanLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, msg := range rows {
		matched, err := blocksHaveSubagentState(msg.BlocksJSON, toolUseID)
		if err != nil {
			// 单条消息 blocks 损坏不应阻断其它消息;跳过继续找。
			logger.Ctx(ctx).Warn("chat_repo.FindAssistantBySubagentToolUseID: decode blocks failed; skipping",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if matched {
			return msg, nil
		}
	}
	return nil, nil
}

// blocksHaveSubagentState 只读判定:blocks_json(StoredBlock 数组)里是否存在
// type=="subagent_state" 且 data.parent_tool_call_id==parentToolUseID 的块。
// 与 Flip/AppendSubagentChildrenInBlocksJSON 共用同一 StoredBlock 信封 + parent_tool_call_id
// 匹配口径,但不改写任何字段。空 JSON 返回 (false, nil)。
func blocksHaveSubagentState(blocksJSON, parentToolUseID string) (bool, error) {
	if blocksJSON == "" {
		return false, nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(blocksJSON), &stored); err != nil {
		return false, err
	}
	for i := range stored {
		if stored[i].Type != "subagent_state" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(stored[i].Data))
		dec.UseNumber()
		var data map[string]any
		if err := dec.Decode(&data); err != nil {
			return false, err
		}
		if parent, _ := data["parent_tool_call_id"].(string); parent == parentToolUseID {
			return true, nil
		}
	}
	return false, nil
}

func (r *messageRepo) DeleteFromSeq(ctx context.Context, sessionID int64, fromSeq int) (int64, error) {
	res := db.Ctx(ctx).
		Where("session_id = ? AND seq >= ?", sessionID, fromSeq).
		Delete(&chat_entity.Message{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func EnsureReplacementRecoveryNamespaceAvailable(ctx context.Context, recoverySessionID int64) error {
	if recoverySessionID >= 0 {
		return errors.New("invalid replacement recovery namespace")
	}
	var count int64
	if err := db.Ctx(ctx).
		Raw("SELECT COUNT(*) FROM `chat_messages` WHERE session_id = ?", recoverySessionID).
		Row().
		Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrReplacementNamespaceCollision
	}
	return nil
}

// MoveMessagesFromSeq changes only row ownership. IDs, content, anchors,
// sequence numbers, and timestamps remain byte-for-byte intact for recovery.
func MoveMessagesFromSeq(ctx context.Context, sourceSessionID, targetSessionID int64, fromSeq int) (int64, error) {
	res := db.Ctx(ctx).Exec(
		"UPDATE `chat_messages` SET `session_id`=? WHERE session_id = ? AND seq >= ?",
		targetSessionID,
		sourceSessionID,
		fromSeq,
	)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// DeleteOwnedReplacementMessages removes only the two active rows recorded by
// one recovery generation. A stale restore therefore cannot truncate a retry.
func DeleteOwnedReplacementMessages(ctx context.Context, sessionID, userMessageID, assistantMessageID int64) (int64, error) {
	res := db.Ctx(ctx).Exec(
		"DELETE FROM `chat_messages` WHERE session_id = ? AND id IN (?,?)",
		sessionID,
		userMessageID,
		assistantMessageID,
	)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func FindReplacementRecovery(ctx context.Context, recoverySessionID int64) (*ReplacementRecovery, error) {
	if recoverySessionID >= 0 {
		return nil, nil
	}
	var markers []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", recoverySessionID, replacementRecoveryRole).
		Order("id ASC").
		Find(&markers).Error; err != nil {
		return nil, err
	}
	if len(markers) == 0 {
		return nil, nil
	}
	if len(markers) != 1 {
		return nil, errors.New("replacement recovery namespace has multiple markers")
	}
	return ParseReplacementRecoveryMarker(markers[0])
}

func FindReplacementRecoveryForMessage(ctx context.Context, sessionID, messageID int64) (*ReplacementRecovery, error) {
	if sessionID <= 0 || messageID <= 0 {
		return nil, nil
	}
	var markers []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("role = ? AND device_id = ?", replacementRecoveryRole, strconv.FormatInt(sessionID, 10)).
		Order("id ASC").
		Find(&markers).Error; err != nil {
		return nil, err
	}
	var matched *ReplacementRecovery
	for _, marker := range markers {
		recovery, err := ParseReplacementRecoveryMarker(marker)
		if err != nil {
			return nil, err
		}
		if recovery.SessionID != sessionID ||
			(recovery.UserMessageID != messageID && recovery.AssistantMessageID != messageID) {
			continue
		}
		if matched != nil {
			return nil, errors.New("active message has multiple replacement recovery owners")
		}
		matched = recovery
	}
	return matched, nil
}

// AcknowledgeReplacementRecovery changes only the exact marker row and never
// recreates a marker deleted by a stale finalizer.
func AcknowledgeReplacementRecovery(ctx context.Context, recovery *ReplacementRecovery) error {
	if recovery == nil || recovery.MarkerID <= 0 || recovery.RecoverySessionID >= 0 {
		return errors.New("invalid replacement recovery acknowledgement")
	}
	res := db.Ctx(ctx).Exec(
		"UPDATE `chat_messages` SET `model`=?,`updatetime`=? WHERE id = ? AND session_id = ? AND role = ?",
		string(ReplacementRecoveryAcknowledged),
		time.Now().UnixMilli(),
		recovery.MarkerID,
		recovery.RecoverySessionID,
		replacementRecoveryRole,
	)
	if res.Error != nil {
		return res.Error
	}
	recovery.State = ReplacementRecoveryAcknowledged
	return nil
}

// DeleteReplacementRecovery is idempotent and scoped to one marker-derived
// namespace, so an old acknowledgement cannot delete a newer retry.
func DeleteReplacementRecovery(ctx context.Context, recoverySessionID int64) (int64, error) {
	if recoverySessionID >= 0 {
		return 0, errors.New("invalid replacement recovery namespace")
	}
	res := db.Ctx(ctx).Exec("DELETE FROM `chat_messages` WHERE session_id = ?", recoverySessionID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// RestoreReplacementSession claims the generation by its forked provider
// session ID before any transcript restoration. A stale generation therefore
// rolls its surrounding transaction back instead of restoring over a retry.
func RestoreReplacementSession(ctx context.Context, recovery *ReplacementRecovery) error {
	if recovery == nil || recovery.SessionID <= 0 || recovery.NewProviderSessionID == "" {
		return errors.New("invalid replacement recovery session")
	}
	res := db.Ctx(ctx).Exec(
		"UPDATE `chat_sessions` SET `provider_session_id`=?,`agent_status`=?,`last_message_at`=?,`updatetime`=? "+
			"WHERE id = ? AND provider_session_id = ?",
		recovery.OldProviderSessionID,
		recovery.OldAgentStatus,
		recovery.OldLastMessageAt,
		time.Now().UnixMilli(),
		recovery.SessionID,
		recovery.NewProviderSessionID,
	)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrReplacementOwnershipLost
	}
	return nil
}
