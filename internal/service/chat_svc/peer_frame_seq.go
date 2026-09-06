package chat_svc

import (
	"github.com/agentre-hub/agentre/internal/pkg/transcript"
)

// settledPeerFrames 挑出轮内 checkpoint 此刻可以定稿的帧。
//
// 落在结尾的 text / thinking 块可能只是累加器里还在长的那一段(acc.Snapshot 把未
// flush 的缓冲当块交出来),现在给它取号,下一次 checkpoint 内容变长就会变成第二个号、
// 第二帧,对端的转录里同一段话出现两次。它们等收口那一次(final)再发。
// 消息级派生帧同理:usage / done 要等这一轮真的结束。
//
// 「一条消息摊成带位置的持久帧」本身不在这里:那是两个宿主共用的
// transcript.ProjectKeyedMessage(规格「复用边界」)。这里只有桌面端自己的取舍
// —— 哪些帧现在还不该发。
func settledPeerFrames(frames []transcript.KeyedFrame) []transcript.KeyedFrame {
	end := len(frames)
	for end > 0 {
		last := frames[end-1]
		if last.Key.BlockIdx == transcript.MessageDerivedBlockIdx || isGrowingTextBlock(last.BlockType) {
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
