package chat_svc

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func TestReadDroppedImages_ClassifiesEachPath(t *testing.T) {
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "shot.png")
	writeTestPNG(t, pngPath)

	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakePNG := filepath.Join(dir, "fake.png") // 扩展名像图片,内容是文本
	if err := os.WriteFile(fakePNG, []byte("not really an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "folder")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "ghost.png")

	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{
		Paths: []string{pngPath, txtPath, fakePNG, subdir, missing},
	})
	if err != nil {
		t.Fatalf("ReadDroppedImages: %v", err)
	}
	if len(resp.Items) != 5 {
		t.Fatalf("items=%d want 5", len(resp.Items))
	}

	got := resp.Items[0]
	if got.Kind != "image" || got.MediaType != "image/png" || got.Name != "shot.png" ||
		!strings.HasPrefix(got.DataURL, "data:image/png;base64,") {
		t.Fatalf("png item = %+v", got)
	}
	for i, p := range []string{txtPath, fakePNG, subdir, missing} {
		item := resp.Items[i+1]
		if item.Kind != "path" || item.Path != p {
			t.Fatalf("item[%d] = %+v want kind=path path=%s", i+1, item, p)
		}
	}
}

func TestReadDroppedImages_OversizeDegradesToPath(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")

	// 文件以合法 PNG 头开始(本可通过 MIME 嗅探),但总大小超过 maxDropImageBytes。
	// 这样断言 Kind=path 才能证明降级是由大小闸门触发,而非 MIME 不符。
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	oversize := append(pngBuf.Bytes(), make([]byte, maxDropImageBytes)...)
	if err := os.WriteFile(big, oversize, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{Paths: []string{big}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Items[0].Kind != "path" {
		t.Fatalf("oversize item = %+v want kind=path", resp.Items[0])
	}
}

func TestReadDroppedImages_WebPClassifiedAsImage(t *testing.T) {
	dir := t.TempDir()
	webpPath := filepath.Join(dir, "anim.webp")
	// http.DetectContentType 用带掩码的 "RIFF????WEBPVP" 匹配 webp(中间 4 字节为大小,被掩码忽略)。
	if err := os.WriteFile(webpPath, []byte("RIFF\x00\x00\x00\x00WEBPVP8 \x00\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{Paths: []string{webpPath}})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.Items[0]
	if got.Kind != "image" || got.MediaType != "image/webp" ||
		!strings.HasPrefix(got.DataURL, "data:image/webp;base64,") {
		t.Fatalf("webp item = %+v", got)
	}
}

func TestReadDroppedImages_EmptyPaths(t *testing.T) {
	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{Paths: []string{}})
	if err != nil {
		t.Fatalf("empty paths: err=%v", err)
	}
	if resp.Items == nil || len(resp.Items) != 0 {
		t.Fatalf("empty paths: items=%+v want non-nil len 0", resp.Items)
	}
}

func TestReadDroppedImages_NilRequest(t *testing.T) {
	resp, err := ReadDroppedImages(context.Background(), nil)
	if err != nil || resp == nil || len(resp.Items) != 0 {
		t.Fatalf("nil req: resp=%+v err=%v", resp, err)
	}
}
