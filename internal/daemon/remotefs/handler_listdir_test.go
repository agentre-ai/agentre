package remotefs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/remotefs"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"
)

func setupHandlers(t *testing.T) (*remotefs.Handlers, string) {
	t.Helper()
	home := t.TempDir()
	h := remotefs.NewHandlers(remotefs.Options{
		HomeFn:     func() (string, error) { return home, nil },
		MaxEntries: 2000,
	})
	return h, home
}

func TestListDir_Happy(t *testing.T) {
	h, home := setupHandlers(t)
	require.NoError(t, os.Mkdir(filepath.Join(home, "Work"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "readme.txt"), []byte("hi"), 0o644))

	resp, err := h.ListDir(context.Background(), wire.ListDirReq{Path: home})
	require.NoError(t, err)
	assert.Equal(t, home, resp.Path)
	assert.False(t, resp.Truncated)
	names := map[string]wire.Entry{}
	for _, e := range resp.Entries {
		names[e.Name] = e
	}
	assert.True(t, names["Work"].IsDir)
	assert.Equal(t, int64(0), names["Work"].Size)
	assert.False(t, names["readme.txt"].IsDir)
	assert.Equal(t, int64(2), names["readme.txt"].Size)
}

func TestListDir_EmptyPathUsesHome(t *testing.T) {
	h, home := setupHandlers(t)
	resp, err := h.ListDir(context.Background(), wire.ListDirReq{Path: ""})
	require.NoError(t, err)
	assert.Equal(t, home, resp.Path)
}

func TestListDir_NonAbsolute(t *testing.T) {
	h, _ := setupHandlers(t)
	_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: "relative"})
	assert.True(t, errors.Is(err, wire.ErrPathRefused))
}

func TestListDir_DotDot(t *testing.T) {
	h, _ := setupHandlers(t)
	_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: "/../etc"})
	assert.True(t, errors.Is(err, wire.ErrPathRefused))
}

func TestListDir_Blacklist(t *testing.T) {
	h, _ := setupHandlers(t)
	for _, p := range []string{"/proc", "/proc/cpuinfo", "/sys", "/dev"} {
		_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: p})
		assert.Truef(t, errors.Is(err, wire.ErrPathRefused), "path=%s", p)
	}
}

func TestListDir_NotFound(t *testing.T) {
	h, home := setupHandlers(t)
	_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: filepath.Join(home, "nope")})
	assert.True(t, errors.Is(err, wire.ErrNotFound))
}

func TestListDir_NotDir(t *testing.T) {
	h, home := setupHandlers(t)
	file := filepath.Join(home, "f.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: file})
	assert.True(t, errors.Is(err, wire.ErrNotDir))
}

func TestListDir_PermDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only perm test")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	h, home := setupHandlers(t)
	sub := filepath.Join(home, "locked")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.Chmod(sub, 0))
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	_, err := h.ListDir(context.Background(), wire.ListDirReq{Path: sub})
	assert.True(t, errors.Is(err, wire.ErrPermDenied))
}

func TestListDir_SymlinkFlag(t *testing.T) {
	h, home := setupHandlers(t)
	target := filepath.Join(home, "target")
	require.NoError(t, os.Mkdir(target, 0o755))
	link := filepath.Join(home, "link")
	require.NoError(t, os.Symlink(target, link))

	resp, err := h.ListDir(context.Background(), wire.ListDirReq{Path: home})
	require.NoError(t, err)
	for _, e := range resp.Entries {
		if e.Name == "link" {
			assert.True(t, e.Symlink, "symlink flag")
			return
		}
	}
	t.Fatalf("entry 'link' not found")
}

func TestListDir_Truncated(t *testing.T) {
	home := t.TempDir()
	h := remotefs.NewHandlers(remotefs.Options{
		HomeFn:     func() (string, error) { return home, nil },
		MaxEntries: 3,
	})
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(home, "f"+strconv.Itoa(i)), nil, 0o644))
	}
	resp, err := h.ListDir(context.Background(), wire.ListDirReq{Path: home})
	require.NoError(t, err)
	assert.True(t, resp.Truncated)
	assert.Len(t, resp.Entries, 3)
}

func TestListDir_HiddenNotFiltered(t *testing.T) {
	h, home := setupHandlers(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".secret"), nil, 0o644))
	resp, err := h.ListDir(context.Background(), wire.ListDirReq{Path: home})
	require.NoError(t, err)
	found := false
	for _, e := range resp.Entries {
		if e.Name == ".secret" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestRegister_TranslatesSentinel(t *testing.T) {
	reg := rpc.NewRegistry()
	home := t.TempDir()
	h := remotefs.NewHandlers(remotefs.Options{
		HomeFn:     func() (string, error) { return home, nil },
		MaxEntries: 2000,
	})
	// wrap 透传 = 等价于不做 auth 检查,方便单测;生产由 daemon 传 requireAuth 闭包。
	remotefs.Register(reg, h, func(fn rpc.HandlerFunc) rpc.HandlerFunc { return fn })

	// 通过 dispatch 触发一次 PathRefused,验证客户端能拿到 *rpc.Error 而不是裸 sentinel
	raw, _ := json.Marshal(wire.ListDirReq{Path: "/proc"})
	_, err := reg.Dispatch(context.Background(), wire.MethodListDir, raw)
	var rpcErr *rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodePathRefused, rpcErr.Code)
}
