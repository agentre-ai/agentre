package app

import (
	"testing"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/service/project_svc"
)

func TestProjectSessionItemsIncludeLastReadAt(t *testing.T) {
	t.Parallel()

	got := toProjectSessionItems([]*chat_entity.Session{
		{
			ID:             11,
			AgentID:        7,
			Title:          "Read state",
			AgentStatus:    "idle",
			LastMessageAt:  2000,
			LastReadAt:     3000,
			NeedsAttention: false,
		},
	})

	if len(got) != 1 {
		t.Fatalf("expected one project session item, got %d", len(got))
	}
	if got[0].LastReadAt != 3000 {
		t.Fatalf("expected lastReadAt to be mapped from chat session, got %d", got[0].LastReadAt)
	}
}

func TestProjectMemberItemsIncludeAgentDisplayFields(t *testing.T) {
	t.Parallel()

	got := toProjectMembers([]*project_svc.ProjectAgentMember{
		{
			AgentID:       5,
			JoinedAt:      100,
			FromProjectID: 1,
			FromName:      "Parent",
			AgentName:     "Builder",
			AvatarColor:   "agent-2",
			AvatarIcon:    "hammer",
			AvatarDataURL: "data:image/png;base64,Yg==",
		},
	}, true)

	if len(got) != 1 {
		t.Fatalf("expected one project member item, got %d", len(got))
	}
	if got[0].AgentName != "Builder" {
		t.Fatalf("expected agentName to be mapped, got %q", got[0].AgentName)
	}
	if got[0].AvatarColor != "agent-2" || got[0].AvatarIcon != "hammer" || got[0].AvatarDataURL == "" {
		t.Fatalf("expected avatar fields to be mapped, got %#v", got[0])
	}
	if !got[0].Inherited {
		t.Fatal("expected inherited flag to be preserved")
	}
}
