package department_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDepartmentCheck(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		input   *Department
		wantErr bool
	}{
		{"nil receiver", nil, true},
		{"empty name", &Department{Name: "", AccentColor: "agent-1"}, true},
		{"negative parent id", &Department{Name: "x", ParentID: -1, AccentColor: "agent-1"}, true},
		{"invalid color", &Department{Name: "x", AccentColor: "rainbow"}, true},
		{"empty color allowed", &Department{Name: "x", AccentColor: ""}, false},
		{"valid agent-3", &Department{Name: "工程部", AccentColor: "agent-3"}, false},
		{"valid extended color", &Department{Name: "设计部", AccentColor: "agent-16"}, false},
		{"neutral allowed", &Department{Name: "x", AccentColor: "neutral"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Check(ctx)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDepartmentHelpers(t *testing.T) {
	assert.False(t, (*Department)(nil).IsActive())
	assert.True(t, (&Department{Status: 1}).IsActive())
	assert.True(t, (&Department{ParentID: 0}).IsRoot())
	assert.False(t, (&Department{ParentID: 5}).IsRoot())
}
