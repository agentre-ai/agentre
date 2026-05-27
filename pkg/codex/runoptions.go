package codex

type RunOption func(*runSpec)

func Resume(threadID string) RunOption {
	return func(s *runSpec) { s.resumeID = threadID }
}

func RunCollaborationMode(mode CollaborationMode) RunOption {
	return func(s *runSpec) { s.collaborationMode = mode }
}
