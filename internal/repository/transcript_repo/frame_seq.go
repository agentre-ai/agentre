package transcript_repo

import (
	"context"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"
)

// FrameKey 定位一个持久帧在转录里的位置：它由哪条消息的哪个块的第几帧投影而来。
//
// BlockIdx = -1 表示消息级派生帧（UsageUpdate / ErrorEvent / Done）；Ordinal 是该块
// （或该消息尾部）投影出的第几帧，一个块投影出不止一帧时它才非零。
//
// 这三格合起来是**内容的身份**：它不随「这是转录里的第几帧」漂移，所以原地修补往
// 中间插入一帧时，后面那些帧的身份、进而它们的编号都不变。
type FrameKey struct {
	MessageID int64
	BlockIdx  int
	Ordinal   int
}

// FrameSeqRow 是台账里的一行：一次编号分配。
type FrameSeqRow struct {
	SessionID int64 `gorm:"column:session_id;type:bigint;not null"`
	MessageID int64 `gorm:"column:message_id;type:bigint;not null"`
	BlockIdx  int   `gorm:"column:block_idx;type:int;not null"`
	Ordinal   int   `gorm:"column:ordinal;type:int;not null"`
	Seq       int64 `gorm:"column:seq;type:bigint;not null"`
}

// TableName 绑定表名。
func (*FrameSeqRow) TableName() string { return "chat_frame_seqs" }

//go:generate mockgen -source frame_seq.go -destination mock_transcript_repo/mock_frame_seq.go

// FrameSeqRepo 是对端帧编号的台账（决策 3）。
//
// 分配与落库不可分：Allocate 是唯一的取号入口，它在一个事务里读出当前末尾、写下新
// 的分配行；写失败即没有分配，调用方据此**不发布**这一帧 —— 否则对端会持有一个宿主
// 认不回来的号。
type FrameSeqRepo interface {
	// Load 取回这条对话已分配过的全部编号，按帧位置归并。
	//
	// 同一个位置被原地修补过多次时留下多行，交回 seq 最大的那一行：它才是该位置**当前
	// 内容**的号，也正是对端最后一次实时收到的那个号。
	Load(ctx context.Context, sessionID int64) (map[FrameKey]int64, error)
	// Allocate 依次为 keys 取下一个号并落库，返回与 keys 一一对应的编号。
	// keys 为空时不发查询。
	Allocate(ctx context.Context, sessionID int64, keys []FrameKey) ([]int64, error)
}

var defaultFrameSeq FrameSeqRepo = NewFrameSeq()

func FrameSeq() FrameSeqRepo             { return defaultFrameSeq }
func RegisterFrameSeq(impl FrameSeqRepo) { defaultFrameSeq = impl }
func NewFrameSeq() FrameSeqRepo          { return &frameSeqRepo{} }

type frameSeqRepo struct{}

func (r *frameSeqRepo) Load(ctx context.Context, sessionID int64) (map[FrameKey]int64, error) {
	var rows []*FrameSeqRow
	if err := db.Ctx(ctx).Model(&FrameSeqRow{}).
		Where("session_id = ?", sessionID).
		Order("seq ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[FrameKey]int64, len(rows))
	for _, row := range rows {
		// 按 seq 升序扫描,后写的覆盖先写的 —— 落在同一位置的多次分配里,最后一次胜出。
		out[FrameKey{MessageID: row.MessageID, BlockIdx: row.BlockIdx, Ordinal: row.Ordinal}] = row.Seq
	}
	return out, nil
}

func (r *frameSeqRepo) Allocate(ctx context.Context, sessionID int64, keys []FrameKey) ([]int64, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	seqs := make([]int64, 0, len(keys))
	err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		seqs = seqs[:0]
		var latest int64
		// 计数器没有单独的列:本会话台账的 MAX(seq) 就是它。被顶掉的旧分配行留在表里,
		// 所以它只增不减 —— 一个号一旦发出去就永不重用。
		if err := tx.Model(&FrameSeqRow{}).
			Where("session_id = ?", sessionID).
			Select("COALESCE(MAX(seq), 0)").
			Scan(&latest).Error; err != nil {
			return err
		}
		rows := make([]*FrameSeqRow, 0, len(keys))
		for i, key := range keys {
			seq := latest + int64(i) + 1
			rows = append(rows, &FrameSeqRow{
				SessionID: sessionID, MessageID: key.MessageID,
				BlockIdx: key.BlockIdx, Ordinal: key.Ordinal, Seq: seq,
			})
			seqs = append(seqs, seq)
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return seqs, nil
}
