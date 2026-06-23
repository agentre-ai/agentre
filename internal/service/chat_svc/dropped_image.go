package chat_svc

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
)

// maxDropImageBytes 与前端 MAX_CHAT_IMAGE_BYTES 对齐。
const maxDropImageBytes = 5 * 1024 * 1024

// dropImageMIMEs 是允许作图片附件的嗅探 MIME 集合(对齐 CHAT_IMAGE_ACCEPT)。
var dropImageMIMEs = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
}

// ReadDroppedImages 按绝对路径读取拖入的图片候选,做 stat/类型/大小校验后归类。
// 永不返回 per-item 错误:任何无法成为图片附件的情况都归为 Kind="path"(降级)。
//
// ctx 当前未使用(纯本地文件 I/O),仍接收以与 Wails binding 签名保持一致并为将来
// (如远端/可取消读取)预留扩展位。
func ReadDroppedImages(_ context.Context, req *ReadDroppedImagesRequest) (*ReadDroppedImagesResponse, error) {
	resp := &ReadDroppedImagesResponse{}
	if req == nil {
		return resp, nil
	}
	resp.Items = make([]DroppedImageItem, 0, len(req.Paths))
	for _, p := range req.Paths {
		resp.Items = append(resp.Items, readDroppedImage(p))
	}
	return resp, nil
}

// readDroppedImage 判定单个路径;任何失败一律降级为 path。
func readDroppedImage(path string) DroppedImageItem {
	degrade := DroppedImageItem{Path: path, Kind: DroppedImageKindPath}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return degrade // 不存在 / 目录 / 非常规文件
	}
	if info.Size() > maxDropImageBytes {
		return degrade // 超限,不读取
	}
	//nolint:gosec // G304: path 来自用户主动拖入的 OS 文件(Wails OnFileDrop),非外部不可信输入
	data, err := os.ReadFile(path)
	if err != nil {
		return degrade
	}
	mime := sniffImageMIME(data)
	if _, ok := dropImageMIMEs[mime]; !ok {
		return degrade // 类型不符
	}
	return DroppedImageItem{
		Path:      path,
		Kind:      DroppedImageKindImage,
		Name:      filepath.Base(path),
		MediaType: mime,
		DataURL:   "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
	}
}

// sniffImageMIME 用 http.DetectContentType 嗅探前 512 字节。Go 标准库的 sniff 表
// (net/http/sniff.go)已覆盖 png/jpeg/webp —— webp 由带掩码的 "RIFF????WEBPVP"
// 模式匹配,因此无需额外的自定义 header 检查。
func sniffImageMIME(data []byte) string {
	if len(data) > 512 {
		data = data[:512]
	}
	return http.DetectContentType(data)
}
