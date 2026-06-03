package claudecode

import (
	"encoding/base64"
	"encoding/json"
)

// Image 是一条 inline 图片附件:解码后的原始字节 + MIME 类型(image/png 等)。
// 由调用方(runtime 层)从用户 blocks 抽出后透传;buildUserFrame 把它编码成
// Anthropic stream-json user frame 里的 base64 image content block。
type Image struct {
	Data      []byte
	MediaType string
}

// buildUserFrame 构造一条 stream-json user frame 的 JSON 字节(不含尾随换行,交给
// 调用方按 "%s\n" 写 stdin)。
//
// content 顺序按 Anthropic 官方建议:图片在前、文本在后(图片先于文本能获得更好的
// 视觉理解)。规则:
//   - 无 inline 字节的 Image 跳过(URL-only 图片当前不支持,与 piagent 行为一致);
//   - MediaType 缺省回退 image/png;
//   - text 非空才追加 text block;但当一张图片都没有时,即便 text 为空也照发一个
//     空 text block —— 保持与历史 text-only 实现字节级一致(content 永远非空)。
func buildUserFrame(text string, images []Image) ([]byte, error) {
	content := make([]map[string]any, 0, len(images)+1)
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		mediaType := img.MediaType
		if mediaType == "" {
			mediaType = "image/png"
		}
		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	if text != "" || len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	frame := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": content},
	}
	return json.Marshal(frame)
}
