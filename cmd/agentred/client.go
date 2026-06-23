package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"

	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

// localClient dials the daemon's unix socket so CLI subcommands can talk to a
// locally running agentred without re-implementing the JSON-RPC transport.
func localClient() *http.Client {
	dir, _ := paths.AgentredDataDir()
	sock := filepath.Join(dir, "agentred.sock")
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sock)
			},
		},
	}
}

func localGET(path string) ([]byte, error) {
	resp, err := localClient().Get("http://daemon" + path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
