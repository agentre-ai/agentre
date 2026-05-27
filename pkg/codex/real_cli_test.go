//go:build codexcli

package codex

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealCodexCLIStartResumeRollbackFork(t *testing.T) {
	if os.Getenv("CODEX_REAL_CLI") != "1" {
		t.Skip("set CODEX_REAL_CLI=1 to run against the local codex CLI")
	}
	binary, err := exec.LookPath("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	client := New(
		WithBinary(binary),
		WithCwd(t.TempDir()),
		WithSandbox(SandboxReadOnly),
		WithApproval(ApprovalNever),
		WithKillGrace(2*time.Second),
	)

	firstText, sourceThreadID := collectRealCodexTurn(t, ctx, client, "Reply exactly with: wrapperpong")
	assert.Equal(t, "wrapperpong", strings.TrimSpace(firstText))
	require.NotEmpty(t, sourceThreadID)

	secondText, resumedThreadID := collectRealCodexTurn(t, ctx, client, "Reply exactly with: wrapperpong2", Resume(sourceThreadID))
	assert.Equal(t, sourceThreadID, resumedThreadID)
	assert.Equal(t, "wrapperpong2", strings.TrimSpace(secondText))

	rolled, err := client.RollbackThread(ctx, sourceThreadID, 1)
	require.NoError(t, err)
	assert.Equal(t, sourceThreadID, rolled.ThreadID)
	rolledText, rolledThreadID := collectRealCodexTurn(t, ctx, client, "Reply exactly with: wrapperrolled", Resume(sourceThreadID))
	assert.Equal(t, sourceThreadID, rolledThreadID)
	assert.Equal(t, "wrapperrolled", strings.TrimSpace(rolledText))

	fork, err := client.ForkThread(ctx, sourceThreadID)
	require.NoError(t, err)
	require.NotEmpty(t, fork.ThreadID)
	assert.Equal(t, sourceThreadID, fork.ForkedFromID)
	assert.NotEqual(t, sourceThreadID, fork.ThreadID)

	thirdText, forkThreadID := collectRealCodexTurn(t, ctx, client, "Reply exactly with: wrapperfork", Resume(fork.ThreadID))
	assert.Equal(t, fork.ThreadID, forkThreadID)
	assert.Equal(t, "wrapperfork", strings.TrimSpace(thirdText))
}

func TestRealCodexCLIPlanModeEmitsPlanText(t *testing.T) {
	if os.Getenv("CODEX_REAL_CLI") != "1" {
		t.Skip("set CODEX_REAL_CLI=1 to run against the local codex CLI")
	}
	binary, err := exec.LookPath("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	client := New(
		WithBinary(binary),
		WithCwd(t.TempDir()),
		WithSandbox(SandboxReadOnly),
		WithApproval(ApprovalNever),
		WithKillGrace(2*time.Second),
		WithConfig(`model_reasoning_effort="low"`),
	)

	stream, err := client.Stream(ctx,
		"Create a concise two step plan for inspecting this empty temp directory, then stop after the plan. Do not run shell commands.",
		RunCollaborationMode(CollaborationPlan),
	)
	require.NoError(t, err)

	var planText string
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventPlanUpdated:
			planText += ev.PlanText
		case EventError:
			require.NoError(t, ev.Err)
		}
	}
	require.NoError(t, stream.Close(ctx))
	trimmed := strings.TrimSpace(planText)
	require.NotEmpty(t, trimmed)
	assert.True(t, strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "-"))
}

func TestRealCodexCLIPlanThenDefaultResumeExecutes(t *testing.T) {
	if os.Getenv("CODEX_REAL_CLI") != "1" {
		t.Skip("set CODEX_REAL_CLI=1 to run against the local codex CLI")
	}
	binary, err := exec.LookPath("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := New(
		WithBinary(binary),
		WithCwd(t.TempDir()),
		WithSandbox(SandboxReadOnly),
		WithApproval(ApprovalNever),
		WithKillGrace(2*time.Second),
		WithConfig(`model_reasoning_effort="low"`),
	)

	stream, err := client.Stream(ctx,
		"Create a one step plan to later reply exactly with PLAN_EXECUTION_PROBE, then stop after the plan. Do not output PLAN_EXECUTION_PROBE in this turn.",
		RunCollaborationMode(CollaborationPlan),
	)
	require.NoError(t, err)

	var planText string
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventPlanUpdated:
			planText += ev.PlanText
		case EventError:
			require.NoError(t, ev.Err)
		}
	}
	require.NoError(t, stream.Close(ctx))
	require.NotEmpty(t, strings.TrimSpace(planText))
	threadID := stream.SessionID()
	require.NotEmpty(t, threadID)

	executed, resumedThreadID := collectRealCodexTurn(t, ctx, client,
		"Implement the plan. Reply exactly with: PLAN_EXECUTION_PROBE",
		Resume(threadID),
		RunCollaborationMode(CollaborationDefault),
	)
	assert.Equal(t, threadID, resumedThreadID)
	assert.Equal(t, "PLAN_EXECUTION_PROBE", strings.TrimSpace(executed))
}

func collectRealCodexTurn(t *testing.T, ctx context.Context, client *Client, prompt string, opts ...RunOption) (string, string) {
	t.Helper()

	stream, err := client.Stream(ctx, prompt, opts...)
	require.NoError(t, err)

	var text strings.Builder
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventTextDelta:
			text.WriteString(ev.Text)
		case EventError:
			require.NoError(t, ev.Err)
		}
	}
	require.NoError(t, stream.Close(ctx))
	return text.String(), stream.SessionID()
}
