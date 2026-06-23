package project_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectCheck(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		input   *Project
		wantErr bool
	}{
		{"nil receiver", nil, true},
		{"empty name", &Project{Name: "", Path: "/tmp/x", Color: "agent-1"}, true},
		{"empty path", &Project{Name: "x", Path: "", Color: "agent-1"}, true},
		{"negative parent id", &Project{Name: "x", Path: "/tmp/x", ParentID: -1, Color: "agent-1"}, true},
		{"invalid color", &Project{Name: "x", Path: "/tmp/x", Color: "rainbow"}, true},
		{"empty color allowed", &Project{Name: "x", Path: "/tmp/x", Color: ""}, false},
		{"valid agent-3", &Project{Name: "工程", Path: "/tmp/x", Color: "agent-3"}, false},
		{"valid expanded theme color", &Project{Name: "设计", Path: "/tmp/x", Color: "agent-16"}, false},
		{"neutral allowed", &Project{Name: "x", Path: "/tmp/x", Color: "neutral"}, false},
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
