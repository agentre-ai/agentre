package chat_repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
}

// flipSubagentScanLimit 是 FlipSubagentStatus 倒序扫描的最近 assistant 消息条数上限。
// 后台 bash 完成的自主轮通常紧跟在发起它的那条消息之后,近窗足以命中。
const flipSubagentScanLimit = 50

var defaultMessage MessageRepo

func Message() MessageRepo             { return defaultMessage }
func RegisterMessage(impl MessageRepo) { defaultMessage = impl }
func NewMessage() MessageRepo          { return &messageRepo{} }

type messageRepo struct{}

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

func (r *messageRepo) FlipSubagentStatus(ctx context.Context, sessionID int64, toolUseID, status, summary string) error {
	if toolUseID == "" || status == "" {
		return nil
	}
	// serialize read-modify-write per session to avoid lost-update races with AppendSubagentChildren.
	mu := lockForSession(sessionID)
	mu.Lock()
	defer mu.Unlock()

	logger.Ctx(ctx).Info("chat_repo.FlipSubagentStatus: flipping subagent_state status",
		zap.Int64("sessionId", sessionID), zap.String("toolUseId", toolUseID), zap.String("status", status))

	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").
		Limit(flipSubagentScanLimit).
		Find(&rows).Error; err != nil {
		return err
	}

	for _, msg := range rows {
		rewritten, flipped, err := FlipSubagentInBlocksJSON(msg.BlocksJSON, toolUseID, status, summary)
		if err != nil {
			// 单条消息 blocks 损坏不应阻断其它消息;跳过继续找。
			logger.Ctx(ctx).Warn("chat_repo.FlipSubagentStatus: decode blocks failed; skipping message",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if !flipped {
			continue
		}
		msg.BlocksJSON = rewritten
		return r.Update(ctx, msg)
	}

	// 没命中:任务可能已 evict / 非本会话,静默返回 nil。
	return nil
}

// FlipSubagentInBlocksJSON 在 blocks_json(StoredBlock 数组)里就地翻转 type=="subagent_state"
// 且 data.parent_tool_call_id==toolUseID 的块的 status,返回重写后的 JSON + 是否命中。
// summary 非空时同时写入命中块的 summary 字段。
// 只触碰命中块的 status / summary 字段,其余 data 原样保留;repo 层不依赖 service 的 block 类型,
// 只按 StoredBlock 信封 + 该块的少数已知字段操作。
//
// 解 data 用 json.Decoder + UseNumber():数字字段(total_tokens / duration_ms /
// tool_uses)保持 json.Number,避免经 map[string]any 的 float64 强转把整数重写成
// 科学计数(如 1e+04)。导出以便直接单测 JSON 改写逻辑。
func FlipSubagentInBlocksJSON(blocksJSON, toolUseID, status, summary string) (string, bool, error) {
	if blocksJSON == "" {
		return blocksJSON, false, nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(blocksJSON), &stored); err != nil {
		return blocksJSON, false, err
	}
	flipped := false
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
		data["status"] = status
		if summary != "" {
			data["summary"] = summary
		}
		buf, err := json.Marshal(data)
		if err != nil {
			return blocksJSON, false, err
		}
		stored[i].Data = buf
		flipped = true
	}
	if !flipped {
		return blocksJSON, false, nil
	}
	out, err := json.Marshal(stored)
	if err != nil {
		return blocksJSON, false, err
	}
	return string(out), true, nil
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
