package piagent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureProc 是一个把 stdin 写入捕获下来的假进程，让我们断言发给 Pi 的
// prompt 帧形态（含 images）。stdout 预置一段「prompt 已接受 + agent_end」
// 的脚本让 Stream 自然走完。
type captureProc struct {
	stdin  *lockedBuffer
	stdout io.Reader
	stderr io.Reader
	done   chan error
}

func (p *captureProc) Stdin() io.Writer  { return p.stdin }
func (p *captureProc) Stdout() io.Reader { return p.stdout }
func (p *captureProc) Stderr() io.Reader {
	if p.stderr != nil {
		return p.stderr
	}
	return strings.NewReader("")
}
func (p *captureProc) Wait() error { return <-p.done }
func (p *captureProc) Kill() error { close(p.done); return nil }
func (p *captureProc) Signal(os.Signal) error {
	select {
	case p.done <- nil:
	default:
	}
	return nil
}

type captureRunner struct{ proc *captureProc }

func (r *captureRunner) Start(context.Context, procOptions) (processHandle, error) {
	return r.proc, nil
}

func newCaptureClient(stdout string) (*Client, *captureProc) {
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: strings.NewReader(stdout),
		done:   make(chan error, 1),
	}
	return New(WithRPCProcessRunnerForTesting(&captureRunner{proc: proc})), proc
}

func TestStreamPromptCarriesImages(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[]}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "what color?", WithImages([]Image{
		{Data: []byte{1, 2, 3}, MimeType: "image/png"},
	}))
	require.NoError(t, err)
	for s.Next() {
	}

	var frame struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Images  []struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"images"`
	}
	first := strings.SplitN(strings.TrimSpace(proc.stdin.String()), "\n", 2)[0]
	require.NoError(t, json.Unmarshal([]byte(first), &frame))

	assert.Equal(t, "prompt", frame.Type)
	assert.Equal(t, "what color?", frame.Message)
	require.Len(t, frame.Images, 1)
	assert.Equal(t, "image", frame.Images[0].Type)
	assert.Equal(t, "image/png", frame.Images[0].MimeType)
	assert.Equal(t, "AQID", frame.Images[0].Data) // base64 of {1,2,3}
}

func TestStreamPromptWithoutImagesOmitsField(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[]}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "hi")
	require.NoError(t, err)
	for s.Next() {
	}

	first := strings.SplitN(strings.TrimSpace(proc.stdin.String()), "\n", 2)[0]
	assert.NotContains(t, first, "images")
}
