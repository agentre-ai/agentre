//go:build snapshot

package chat_svc

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/stretchr/testify/require"
)

var updateSnapshots = flag.Bool("update-snapshots", false,
	"重新录 chat_messages.blocks_json snapshot 基线")

// assertBlocksSnapshot 把一组 ContentBlock 编码成 blocks_json (与 Message.SetBlocks 同路径)
// 并与 testdata/snapshots/<name>.json 比对。`go test -tags=snapshot -update-snapshots`
// 重新录入；CI 必须空白参数跑。
func assertBlocksSnapshot(t *testing.T, name string, bs []blocks.ContentBlock) {
	t.Helper()
	got, err := encodeBlocksForSnapshot(bs)
	require.NoError(t, err)

	path := filepath.Join("testdata", "snapshots", name+".json")
	if *updateSnapshots {
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "snapshot file missing — run `go test -tags=snapshot -update-snapshots` to record")
	require.JSONEq(t, string(want), string(got), "snapshot drift for %s", name)
}

// encodeBlocksForSnapshot 复用 chat_entity.Message.SetBlocks 的 EncodeAll → MarshalIndent 路径。
// 返回带终止换行的字节切片,便于人类 diff。
func encodeBlocksForSnapshot(bs []blocks.ContentBlock) ([]byte, error) {
	stored, err := blocks.EncodeAll(bs)
	if err != nil {
		return nil, err
	}
	buf, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}
