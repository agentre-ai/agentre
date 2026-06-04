package issue_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueCheck(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, (&Issue{Title: "ok", State: StateOpen}).Check(ctx))

	assert.Error(t, (&Issue{Title: "  ", State: StateOpen}).Check(ctx))
	assert.Error(t, (&Issue{Title: "ok", State: "weird"}).Check(ctx))
}

func TestIssueCloseReopen(t *testing.T) {
	i := &Issue{Title: "x", State: StateOpen}
	i.Close(1234)
	assert.Equal(t, StateClosed, i.State)
	assert.Equal(t, int64(1234), i.ClosedAt)
	assert.True(t, i.IsClosed())

	i.Reopen()
	assert.Equal(t, StateOpen, i.State)
	assert.Equal(t, int64(0), i.ClosedAt)
	assert.True(t, i.IsOpen())
}
