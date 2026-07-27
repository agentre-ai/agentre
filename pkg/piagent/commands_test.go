package piagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCommands(t *testing.T) {
	runner := &fakeRunner{process: newFakeProcess(t)}
	runner.process.stdout = strings.NewReader(`{"type":"response","command":"get_commands","success":true,"data":{"commands":[{"name":"skill:review","description":"Review changes","source":"skill","location":"project","path":"/work/.pi/skills/review/SKILL.md"},{"name":"session-name","source":"extension"}]}}` + "\n")
	runner.process.finishOnSignal(interruptExitError(t))
	client := New(
		WithRPCProcessRunnerForTesting(runner),
		WithKillGrace(time.Second),
	)

	commands, err := client.ListCommands(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []Command{
		{Name: "skill:review", Description: "Review changes", Source: "skill", Location: "project", Path: "/work/.pi/skills/review/SKILL.md"},
		{Name: "session-name", Source: "extension"},
	}, commands)
	assert.True(t, runner.process.signaled)
}

func TestListCommandsRejectsFailureResponse(t *testing.T) {
	runner := &fakeRunner{process: newFakeProcess(t)}
	runner.process.stdout = strings.NewReader(`{"type":"response","command":"get_commands","success":false,"error":"unavailable"}` + "\n")
	runner.process.finishOnSignal(interruptExitError(t))
	client := New(
		WithRPCProcessRunnerForTesting(runner),
		WithKillGrace(time.Second),
	)

	_, err := client.ListCommands(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unavailable")
}
