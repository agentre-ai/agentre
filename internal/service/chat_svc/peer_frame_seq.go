package chat_svc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/agentre-hub/agentre/internal/model/entity/transcript_entity"
	"github.com/agentre-hub/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-hub/agentre/internal/pkg/transcript"
	"github.com/agentre-hub/agentre/internal/repository/transcript_repo"
)

// keyedFrame 是一个持久帧连同它在转录里的位置与内容指纹。
//
// 位置(key)是编号挂靠的那一格:它不随「这是第几帧」漂移,所以原地修补往中间插一帧
// 时,后面那些帧的位置、进而它们已发布的编号都不动。
type keyedFrame struct {
	key   transcript_repo.FrameKey
	frame wire.EventFrame
	// blockType 是产生这一帧的块类型;消息级派生帧(UsageUpdate / ErrorEvent / Done)
	// 留空。轮内 checkpoint 靠它认出结尾那些**还会继续长**的正文块。
	blockType string
	// fingerprint 是帧内容的指纹。同一个位置指纹变了 = 这个块被原地修补过,
	// 修补后的那一帧要取一个新的末尾号(spec「帧编号」)。
	fingerprint string
	createtime  int64
}

// projectMessageFrames 把一条消息摊成带位置的持久帧。
//
// 位置不是本文件自己数出来的:它把每个块**单独**装进一条同角色的消息交给共享投影器
// (internal/pkg/transcript),于是「这一帧来自哪个块、是该块投影出的第几帧」由投影器
// 自己说了算 —— 本文件因此不持有第二份「块 → 帧」的分派表。两条路必须逐帧相等,
// 由 TestProjectMessageListFrames_MatchesSharedProjection 守住。
func projectMessageFrames(conversationID string, msg *transcript_entity.Message) ([]keyedFrame, error) {
	if msg == nil {
		return nil, nil
	}
	blocksJSON := msg.BlocksJSON
	if blocksJSON == "" {
		blocksJSON = "[]"
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(blocksJSON), &raw); err != nil {
		return nil, fmt.Errorf("message %d blocks: %w", msg.ID, err)
	}
	out := make([]keyedFrame, 0, len(raw)+1)
	for idx, block := range raw {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(block, &head); err != nil {
			return nil, fmt.Errorf("message %d block %d: %w", msg.ID, idx, err)
		}
		single := *msg
		single.BlocksJSON = "[" + string(block) + "]"
		clearMessageDerivedFields(&single)
		frames, createtimes, err := transcript.ProjectMessages(conversationID, []*transcript_entity.Message{&single})
		if err != nil {
			return nil, err
		}
		// 清空派生那几格之后,单块消息的尾巴是固定的:assistant 恒剩一帧 Done,user 没有。
		frames = frames[:len(frames)-messageDerivedFrameCount(&single)]
		for ordinal := range frames {
			out = append(out, keyedFrame{
				key:         transcript_repo.FrameKey{MessageID: msg.ID, BlockIdx: idx, Ordinal: ordinal},
				frame:       frames[ordinal],
				blockType:   head.Type,
				fingerprint: frameFingerprint(frames[ordinal]),
				createtime:  createtimes[ordinal],
			})
		}
	}
	// 消息级派生帧:同一条消息去掉全部块之后剩下的就是它们,顺序仍由投影器决定。
	derived := *msg
	derived.BlocksJSON = "[]"
	frames, createtimes, err := transcript.ProjectMessages(conversationID, []*transcript_entity.Message{&derived})
	if err != nil {
		return nil, err
	}
	for ordinal := range frames {
		out = append(out, keyedFrame{
			key:         transcript_repo.FrameKey{MessageID: msg.ID, BlockIdx: messageDerivedBlockIdx, Ordinal: ordinal},
			frame:       frames[ordinal],
			fingerprint: frameFingerprint(frames[ordinal]),
			createtime:  createtimes[ordinal],
		})
	}
	return out, nil
}

// messageDerivedBlockIdx 是消息级派生帧的块下标 —— 它们不属于任何块。
const messageDerivedBlockIdx = -1

// projectMessageListFrames 把整条转录摊成带位置的持久帧,顺序与共享投影器一致。
func projectMessageListFrames(conversationID string, messages []*transcript_entity.Message) ([]keyedFrame, error) {
	// 与共享投影器同一口径的排序(按消息 seq,稳定)。两边的顺序必须逐帧一致,
	// 由 TestProjectMessageListFrames_MatchesSharedProjection 守住。
	sorted := append([]*transcript_entity.Message(nil), messages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i] == nil {
			return false
		}
		if sorted[j] == nil {
			return true
		}
		return sorted[i].Seq < sorted[j].Seq
	})
	out := make([]keyedFrame, 0, len(sorted))
	for _, message := range sorted {
		if message == nil {
			continue
		}
		frames, err := projectMessageFrames(conversationID, message)
		if err != nil {
			return nil, err
		}
		out = append(out, frames...)
	}
	return out, nil
}

// clearMessageDerivedFields 清掉「消息级派生帧」读的那几格,只留 Done 那一帧。
func clearMessageDerivedFields(m *transcript_entity.Message) {
	m.PromptTokens, m.CompletionTokens, m.CachedTokens = 0, 0, 0
	m.CacheCreationTokens, m.ReasoningTokens, m.TotalInputTokens = 0, 0, 0
	m.ErrorText = ""
}

// messageDerivedFrameCount 是被 clearMessageDerivedFields 清空后的消息尾部帧数。
func messageDerivedFrameCount(m *transcript_entity.Message) int {
	if m.Role == "assistant" {
		return 1 // Done
	}
	return 0
}

// frameFingerprint 是一帧内容的指纹。编不出来时交回空串,调用方把它当作「与任何东西
// 都不同」—— 宁可多发一帧,也不要把一次真实的原地修补当成没变过。
func frameFingerprint(frame wire.EventFrame) string {
	payload, err := json.Marshal(frame.Event)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// settledPeerFrames 挑出轮内 checkpoint 此刻可以定稿的帧。
//
// 落在结尾的 text / thinking 块可能只是累加器里还在长的那一段(acc.Snapshot 把未
// flush 的缓冲当块交出来),现在给它取号,下一次 checkpoint 内容变长就会变成第二个号、
// 第二帧,对端的转录里同一段话出现两次。它们等收口那一次(final)再发。
// 消息级派生帧同理:usage / done 要等这一轮真的结束。
func settledPeerFrames(frames []keyedFrame) []keyedFrame {
	end := len(frames)
	for end > 0 {
		last := frames[end-1]
		if last.key.BlockIdx == messageDerivedBlockIdx || isGrowingTextBlock(last.blockType) {
			end--
			continue
		}
		break
	}
	return frames[:end]
}

func isGrowingTextBlock(blockType string) bool {
	switch blockType {
	case "text", "display_text", "thinking":
		return true
	default:
		return false
	}
}
