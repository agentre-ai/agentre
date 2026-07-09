package orch_entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestDispatch_TableName(t *testing.T) {
	assert.Equal(t, "orch_dispatches", (&orch_entity.Dispatch{}).TableName())
}

func TestDispatch_IsTerminal(t *testing.T) {
	for _, s := range []string{orch_entity.DispatchDone, orch_entity.DispatchCanceled, orch_entity.DispatchError} {
		assert.True(t, (&orch_entity.Dispatch{Status: s}).IsTerminal(), s)
	}
	for _, s := range []string{orch_entity.DispatchRunning, orch_entity.DispatchAwaitingChildren, orch_entity.DispatchAwaitingUser, orch_entity.DispatchPaused, orch_entity.DispatchPending} {
		assert.False(t, (&orch_entity.Dispatch{Status: s}).IsTerminal(), s)
	}
}

func TestDispatch_IsWaitingUser(t *testing.T) {
	assert.True(t, (&orch_entity.Dispatch{Status: orch_entity.DispatchAwaitingUser}).IsWaitingUser())
	assert.False(t, (&orch_entity.Dispatch{Status: orch_entity.DispatchAwaitingChildren}).IsWaitingUser())
}
