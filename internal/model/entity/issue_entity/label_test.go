package issue_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelCheck(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, (&Label{Name: "bug", Tone: "bug"}).Check(ctx))
	assert.Error(t, (&Label{Name: "", Tone: "bug"}).Check(ctx))
	assert.Error(t, (&Label{Name: "x", Tone: "rainbow"}).Check(ctx))
}
