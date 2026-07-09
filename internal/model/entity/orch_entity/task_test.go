package orch_entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestTask_TableName(t *testing.T) {
	assert.Equal(t, "orch_tasks", (&orch_entity.Task{}).TableName())
}

func TestValidTaskStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{orch_entity.TaskStatusPending, true},
		{orch_entity.TaskStatusInProgress, true},
		{orch_entity.TaskStatusDone, true},
		{"bogus", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, orch_entity.ValidTaskStatus(c.status), c.status)
	}
}
