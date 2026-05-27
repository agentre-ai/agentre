package buildinfo

// CommitID is injected at build time via -ldflags.
var CommitID string

// ShortCommitID returns the first 7 characters of CommitID.
func ShortCommitID() string {
	if len(CommitID) > 7 {
		return CommitID[:7]
	}
	return CommitID
}
