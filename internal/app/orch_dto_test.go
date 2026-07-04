package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestToRunItem_MapsFlowGraph(t *testing.T) {
	dto := toRunItem(&orch_entity.OrchestrationRun{ID: 1, FlowGraph: `{"version":1}`})
	assert.Equal(t, `{"version":1}`, dto.FlowGraph)
}

func TestToTaskDTO_MapsNodeRef(t *testing.T) {
	dto := toTaskDTO(&orch_entity.Task{ID: 1, NodeRef: "FE"})
	assert.Equal(t, "FE", dto.NodeRef)
}
