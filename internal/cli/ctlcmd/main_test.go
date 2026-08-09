package ctlcmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeControl 起一个假的 /ctl/v1/* 控制服务，校验 bearer 并回 canned JSON。
func fakeControl(t *testing.T, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid control token"}`)
			return false
		}
		return true
	}
	mux.HandleFunc("/ctl/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = io.WriteString(w, `{"agents":[{"id":1,"name":"planner","description":"plans"},{"id":2,"name":"coder"}]}`)
	})
	mux.HandleFunc("/ctl/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = io.WriteString(w, `{"projects":[{"id":7,"name":"web","path":"/repo/web"}]}`)
	})
	mux.HandleFunc("/ctl/v1/send", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var body struct {
			Agent string `json:"agent"`
			Text  string `json:"text"`
			Wait  bool   `json:"wait"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Wait {
			_, _ = io.WriteString(w, `{"sessionId":100,"assistantMessageId":200,"text":"the answer","done":true}`)
			return
		}
		_, _ = io.WriteString(w, `{"sessionId":100,"assistantMessageId":200,"done":false}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// envFor 返回一个把控制端点指向 srv 的 lookupEnv。
func envFor(srv *httptest.Server, token string) func(string) (string, bool) {
	m := map[string]string{
		"AGENTRE_CTL_ENDPOINT": srv.URL,
		"AGENTRE_CTL_TOKEN":    token,
	}
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func runCLI(args []string, env func(string) (string, bool)) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

func TestRun_NoArgs(t *testing.T) {
	code, _, _ := runCLI(nil, func(string) (string, bool) { return "", false })
	if code != 2 {
		t.Fatalf("no args: code = %d, want 2", code)
	}
}

func TestRun_GivenHelpWhenPrintedThenUsesAgrctlCommand(t *testing.T) {
	code, out, errs := runCLI([]string{"help"}, func(string) (string, bool) { return "", false })
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%s)", code, errs)
	}
	if !strings.Contains(out, "agrctl ctl") {
		t.Fatalf("stdout = %q, want agrctl ctl command", out)
	}
	if strings.Contains(out, "agentre ctl") {
		t.Fatalf("stdout = %q, must not advertise removed agentre ctl command", out)
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	code, _, errs := runCLI([]string{"bogus"}, func(string) (string, bool) { return "", false })
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errs, "unknown subcommand") {
		t.Fatalf("stderr = %q, want unknown subcommand", errs)
	}
}

func TestRun_Agents(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, out, errs := runCLI([]string{"agents"}, envFor(srv, "tok"))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%s)", code, errs)
	}
	if !strings.Contains(out, "planner") || !strings.Contains(out, "coder") {
		t.Fatalf("stdout = %q, want agent names", out)
	}
}

func TestRun_Projects(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, out, errs := runCLI([]string{"projects"}, envFor(srv, "tok"))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%s)", code, errs)
	}
	if !strings.Contains(out, "/repo/web") {
		t.Fatalf("stdout = %q, want project path", out)
	}
}

func TestRun_SendNoWait(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, out, errs := runCLI([]string{"send", "--agent", "planner", "ship", "it"}, envFor(srv, "tok"))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%s)", code, errs)
	}
	// 即发即返：sessionId 落 stdout 供脚本消费。
	if !strings.Contains(out, "100") {
		t.Fatalf("stdout = %q, want sessionId 100", out)
	}
}

func TestRun_SendWaitPrintsText(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, out, errs := runCLI([]string{"send", "--agent", "planner", "--wait", "do it"}, envFor(srv, "tok"))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%s)", code, errs)
	}
	if strings.TrimSpace(out) != "the answer" {
		t.Fatalf("stdout = %q, want final text", out)
	}
}

func TestRun_SendMissingText(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, _, _ := runCLI([]string{"send", "--agent", "planner"}, envFor(srv, "tok"))
	if code != 2 {
		t.Fatalf("missing text: code = %d, want 2", code)
	}
}

func TestRun_SendMissingAgent(t *testing.T) {
	srv := fakeControl(t, "tok")
	code, _, _ := runCLI([]string{"send", "hello"}, envFor(srv, "tok"))
	if code != 2 {
		t.Fatalf("missing agent: code = %d, want 2", code)
	}
}

func TestRun_NoEndpointConfigured(t *testing.T) {
	// 无 env 端点、AppDataDir 指向空临时目录(无握手文件) → 提示桌面未运行。
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	code, _, errs := runCLI([]string{"agents"}, func(string) (string, bool) { return "", false })
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(strings.ToLower(errs), "not running") && !strings.Contains(strings.ToLower(errs), "endpoint") {
		t.Fatalf("stderr = %q, want desktop-not-running hint", errs)
	}
}
