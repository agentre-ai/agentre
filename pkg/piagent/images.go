package piagent

import "encoding/base64"

// Image 是一张随 prompt 发送的多模态图片。Data 是原始字节（调用方不必自己
// base64），MimeType 形如 "image/png" / "image/jpeg" / "image/webp"。
type Image struct {
	Data     []byte
	MimeType string
}

// imageWire 是 Pi RPC prompt 帧里的 ImageContent 形态：
// {"type":"image","data":<base64>,"mimeType":"image/png"}。
type imageWire struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// imagesToWire 把对外的 Image 列表转成 Pi RPC 期望的 ImageContent 列表。
// 空字节的图片被跳过，避免给 Pi 发无意义的空附件。
func imagesToWire(images []Image) []imageWire {
	if len(images) == 0 {
		return nil
	}
	out := make([]imageWire, 0, len(images))
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		out = append(out, imageWire{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(img.Data),
			MimeType: img.MimeType,
		})
	}
	return out
}
