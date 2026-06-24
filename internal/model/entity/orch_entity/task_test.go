package orch_entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestTask_TableName(t *testing.T) {
	assert.Equal(t, "orch_tasks", (&orch_entity.Task{}).TableName())
}

func TestTask_IsTerminal(t *testing.T) {
	for _, s := range []string{orch_entity.TaskDone, orch_entity.TaskCanceled, orch_entity.TaskError} {
		assert.True(t, (&orch_entity.Task{Status: s}).IsTerminal(), s)
	}
	for _, s := range []string{orch_entity.TaskRunning, orch_entity.TaskAwaitingChildren, orch_entity.TaskAwaitingUser, orch_entity.TaskPaused, orch_entity.TaskPending} {
		assert.False(t, (&orch_entity.Task{Status: s}).IsTerminal(), s)
	}
}

func TestTask_IsWaitingUser(t *testing.T) {
	assert.True(t, (&orch_entity.Task{Status: orch_entity.TaskAwaitingUser}).IsWaitingUser())
	assert.False(t, (&orch_entity.Task{Status: orch_entity.TaskAwaitingChildren}).IsWaitingUser())
}
