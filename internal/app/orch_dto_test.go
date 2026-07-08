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
