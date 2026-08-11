package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/agentre-ai/agentre/internal/buildinfo"
	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/cago-frame/cago/configs"
)

const ipcSocketName = "agentred.sock"

// BuildIdentity returns the version and commit injected into the agentred binary.
func BuildIdentity() string {
	return configs.Version + " (" + buildinfo.ShortCommitID() + ")"
}

// startIPC binds a unix-domain socket under <DataDir>/agentred.sock and
// serves a minimal HTTP API for the local CLI (status / pair / llm).
// Returns the server so daemon.Run can call Shutdown on ctx cancel.
//
// Windows is not yet wired (would need named pipe); returns an error so the
// daemon refuses to start on Windows until we add platform support.
func (d *Daemon) startIPC(ctx context.Context) (*http.Server, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("ipc: windows named pipe path not yet wired")
	}
	path := d.SocketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/local/pair", d.ipcPair)
	mux.HandleFunc("/local/status", d.ipcStatus)
	mux.HandleFunc("/local/llm", d.ipcLLM)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	//nolint:gosec // G118: shutdown must use a fresh ctx since the request ctx is already canceled
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		_ = os.Remove(path)
	}()
	go func() { _ = srv.Serve(ln) }()
	return srv, nil
}

// SocketPath returns the absolute path of the daemon's IPC unix socket.
// Tests use this to dial without going through paths.AgentredDataDir().
func (d *Daemon) SocketPath() string {
	return filepath.Join(d.opts.DataDir, ipcSocketName)
}

func (d *Daemon) ipcPair(w http.ResponseWriter, r *http.Request) {
	code, err := d.pairing.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"code":       code,
		"ttlSeconds": d.state.Snapshot().Preferences.PairingCodeTTLSeconds,
		"listenURLs": lanURLs(d),
	})
}

func (d *Daemon) ipcStatus(w http.ResponseWriter, r *http.Request) {
	snap := d.state.Snapshot()
	// dbPath / dbSizeBytes:远端盒子上的 transcript 是永久档案,规格要求库文件的位置与
	// 体量在状态查询里看得见,用户据此自行判断何时该清理(见 Daemon.DBStat)。
	dbStat := d.DBStat()
	writeJSON(w, map[string]any{
		"pid":              os.Getpid(),
		"version":          BuildIdentity(),
		"daemonUUID":       snap.DaemonInstanceUUID,
		"listenURLs":       lanURLs(d),
		"socketPath":       d.SocketPath(),
		"pairedPeers":      summarizePeers(snap.PairedPeers),
		"activeSessions":   d.activeSessionCount(r.Context()),
		"llmProviderCount": len(snap.LLMProviders),
		"dbPath":           dbStat.Path,
		"dbSizeBytes":      dbStat.SizeBytes,
	})
}

// activeSessionCount 是「这台 daemon 此刻在为几条会话干活」,取自它自己记着的生命周期
// (一轮起手 running、轮末 idle、进程重启把非终态一律标 interrupted)—— 那是 daemon
// 上唯一知道这件事的地方。
//
// 读不出来返 0 并留一行日志:状态查询不该因为库忙而整个失败,但也不能让一个读失败
// 冒充「没有会话在跑」而不留痕迹。
func (d *Daemon) activeSessionCount(ctx context.Context) int64 {
	n, err := d.sessionStore.CountRunning(ctx)
	if err != nil {
		log.Printf("daemon.ipcStatus: count running sessions failed: %v", err)
		return 0
	}
	return n
}

func (d *Daemon) ipcLLM(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		llmH := handlers.NewLLMHandlers(d.state)
		res, _ := llmH.List(r.Context())
		writeJSON(w, res)
	case http.MethodPost:
		var p handlers.LLMUpsertParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		llmH := handlers.NewLLMHandlers(d.state)
		res, err := llmH.Upsert(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, res)
	case http.MethodDelete:
		var p struct {
			ProviderKey string `json:"providerKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		llmH := handlers.NewLLMHandlers(d.state)
		res, err := llmH.Delete(r.Context(), handlers.LLMDeleteParams{ProviderKey: p.ProviderKey})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, res)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func lanURLs(d *Daemon) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.lan == nil {
		return nil
	}
	return []string{d.lan.URL()}
}

func summarizePeers(m map[string]state.PairedPeer) []map[string]any {
	out := make([]map[string]any, 0, len(m))
	for fp, p := range m {
		out = append(out, map[string]any{
			"fingerprint": fp,
			"deviceName":  p.DeviceName,
			"pairedAt":    p.PairedAt,
			"lastSeenAt":  p.LastSeenAt,
		})
	}
	return out
}
