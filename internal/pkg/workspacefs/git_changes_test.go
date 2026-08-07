package workspacefs_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/workspacefs"
)

func changesByPath(changes []workspacefs.Change) map[string]workspacefs.Change {
	m := make(map[string]workspacefs.Change, len(changes))
	for _, c := range changes {
		m[c.Path] = c
	}
	return m
}

func TestGitChanges_UncommittedScope_FiveStatuses(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "c.txt"), []byte("nested\n"), 0o644))
	runGit(t, dir, "add", "b.txt", "sub/c.txt")
	runGit(t, dir, "commit", "-q", "-m", "seed")

	// modified
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world\nmodified line\n"), 0o644))
	// renamed (staged)
	runGit(t, dir, "mv", "sub/c.txt", "sub/d.txt")
	// added (staged, binary)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.bin"), []byte("bin\x00data"), 0o644))
	runGit(t, dir, "add", "f.bin")
	// deleted
	require.NoError(t, os.Remove(filepath.Join(dir, "b.txt")))
	// untracked
	require.NoError(t, os.WriteFile(filepath.Join(dir, "e.txt"), []byte("untracked content\n"), 0o644))

	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	assert.False(t, res.NotARepo)
	assert.False(t, res.Truncated)

	byPath := changesByPath(res.Changes)

	require.Contains(t, byPath, "b.txt")
	assert.Equal(t, workspacefs.ChangeDeleted, byPath["b.txt"].Status)

	require.Contains(t, byPath, "sub/d.txt")
	assert.Equal(t, workspacefs.ChangeRenamed, byPath["sub/d.txt"].Status)
	assert.Equal(t, "sub/c.txt", byPath["sub/d.txt"].OldPath)

	require.Contains(t, byPath, "f.bin")
	assert.Equal(t, workspacefs.ChangeAdded, byPath["f.bin"].Status)
	assert.True(t, byPath["f.bin"].Binary, "tracked binary file: numstat reports '-' for both counts")
	assert.Equal(t, 0, byPath["f.bin"].Added)

	require.Contains(t, byPath, "e.txt")
	assert.Equal(t, workspacefs.ChangeUntracked, byPath["e.txt"].Status)
	assert.Equal(t, 1, byPath["e.txt"].Added, "one line, go-side count for untracked file")
	assert.False(t, byPath["e.txt"].Binary)
}

func TestGitChanges_UncommittedScope_ModifiedLineCounts(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "seed")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nmodified\n"), 0o644))

	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	byPath := changesByPath(res.Changes)
	require.Contains(t, byPath, "a.txt")
	assert.Equal(t, workspacefs.ChangeModified, byPath["a.txt"].Status)
	assert.Equal(t, 1, byPath["a.txt"].Added)
	assert.Equal(t, 0, byPath["a.txt"].Deleted)
}

func TestGitChanges_UntrackedBinary_NULByte(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("a\x00b"), 0o644))

	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	byPath := changesByPath(res.Changes)
	require.Contains(t, byPath, "bin.dat")
	assert.True(t, byPath["bin.dat"].Binary)
	assert.Equal(t, 0, byPath["bin.dat"].Added)
}

func TestGitChanges_UntrackedOversized_TreatedAsBinary(t *testing.T) {
	dir := initRepo(t)
	big := bytes.Repeat([]byte("a"), (1<<20)+1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644))

	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	byPath := changesByPath(res.Changes)
	require.Contains(t, byPath, "big.txt")
	assert.True(t, byPath["big.txt"].Binary, ">1MiB must be treated as binary without reading full content")
	assert.Equal(t, 0, byPath["big.txt"].Added)
}

// setupDivergedBranches builds: main (init commit) -> feature branch checked
// out with one committed change on top of main, plus a further uncommitted
// worktree edit to the same file, a staged new file, and an untracked file.
// Returns the repo dir; caller passes baseline="main".
func setupDivergedBranches(t *testing.T) string {
	t.Helper()
	dir := initRepo(t) // main branch, "init" commit (empty)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "seed on main")

	runGit(t, dir, "checkout", "-q", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nmore\n"), 0o644))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "feature commit")

	// further uncommitted edit on top of the feature commit
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nmore\nwip\n"), 0o644))
	// staged new tracked file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("new tracked file\n"), 0o644))
	runGit(t, dir, "add", "newfile.txt")
	// untracked file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked here\n"), 0o644))

	return dir
}

func TestGitChanges_BranchScope_MergeBaseIncludesCommittedAndUncommitted(t *testing.T) {
	dir := setupDivergedBranches(t)

	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeBranch, "main", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	assert.False(t, res.NotARepo)

	byPath := changesByPath(res.Changes)

	require.Contains(t, byPath, "a.txt", "modified relative to merge-base, combining the feature commit and the wip edit")
	assert.Equal(t, workspacefs.ChangeModified, byPath["a.txt"].Status)
	assert.Equal(t, 2, byPath["a.txt"].Added, "both the committed 'more' line and the uncommitted 'wip' line count")

	require.Contains(t, byPath, "newfile.txt")
	assert.Equal(t, workspacefs.ChangeAdded, byPath["newfile.txt"].Status)
	assert.Equal(t, 1, byPath["newfile.txt"].Added)

	require.Contains(t, byPath, "untracked.txt", "untracked files must still be listed in branch scope")
	assert.Equal(t, workspacefs.ChangeUntracked, byPath["untracked.txt"].Status)
	assert.Equal(t, 1, byPath["untracked.txt"].Added)
}

func TestGitChanges_BranchScope_RequiresBaseline(t *testing.T) {
	dir := initRepo(t)
	_, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeBranch, "", workspacefs.DefaultMaxEntries)
	assert.Truef(t, errors.Is(err, workspacefs.ErrBaselineRequired), "err=%v", err)
}

func TestGitChanges_NonRepo_Degrades(t *testing.T) {
	dir := t.TempDir()
	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", workspacefs.DefaultMaxEntries)
	require.NoError(t, err)
	assert.True(t, res.NotARepo)
	assert.Empty(t, res.Changes)
}

func TestGitChanges_Truncated(t *testing.T) {
	dir := initRepo(t)
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "u"+string(rune('a'+i))+".txt"), []byte("x"), 0o644))
	}
	res, err := workspacefs.GitChanges(context.Background(), dir, workspacefs.ScopeUncommitted, "", 3)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Len(t, res.Changes, 3)
}

func TestDefaultBaseline_PrefersOriginHEAD(t *testing.T) {
	dir := initRepo(t)
	// simulate a remote-tracking origin/main + symbolic origin/HEAD without a
	// real network remote, mirroring what a clone leaves behind.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "refs", "remotes", "origin"), 0o755))
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	// initRepo already leaves a local "main" branch checked out, so this also
	// proves origin/HEAD wins over the local branch of the same name.

	got := workspacefs.DefaultBaseline(context.Background(), dir)
	assert.Equal(t, "origin/main", got)
}

func TestDefaultBaseline_FallsBackToMain(t *testing.T) {
	dir := initRepo(t) // initRepo creates branch "main" already, no origin
	got := workspacefs.DefaultBaseline(context.Background(), dir)
	assert.Equal(t, "main", got)
}

func TestDefaultBaseline_FallsBackToMaster(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "trunk")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	runGit(t, dir, "branch", "master")

	got := workspacefs.DefaultBaseline(context.Background(), dir)
	assert.Equal(t, "master", got)
}

func TestDefaultBaseline_NoneFound(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "trunk")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "init")

	got := workspacefs.DefaultBaseline(context.Background(), dir)
	assert.Equal(t, "", got)
}

func TestRefExists(t *testing.T) {
	dir := initRepo(t)
	assert.True(t, workspacefs.RefExists(context.Background(), dir, "main"))
	assert.False(t, workspacefs.RefExists(context.Background(), dir, "does-not-exist"))
	assert.False(t, workspacefs.RefExists(context.Background(), dir, ""))
}

func TestGitBranches_ListsLocalAndRemote(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "refs", "remotes", "origin"), 0o755))
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	runGit(t, dir, "branch", "develop")

	res, err := workspacefs.GitBranches(context.Background(), dir)
	require.NoError(t, err)
	assert.False(t, res.NotARepo)

	names := make([]string, 0, len(res.Branches))
	byName := map[string]workspacefs.Branch{}
	for _, b := range res.Branches {
		names = append(names, b.Name)
		byName[b.Name] = b
	}
	assert.Contains(t, names, "main")
	assert.False(t, byName["main"].Remote)
	assert.Contains(t, names, "develop")
	assert.Contains(t, names, "origin/main")
	assert.True(t, byName["origin/main"].Remote)
	assert.NotContains(t, strings.Join(names, ","), "HEAD", "the symbolic origin/HEAD pointer must not be listed as a branch")
}

func TestGitBranches_NonRepo_Degrades(t *testing.T) {
	dir := t.TempDir()
	res, err := workspacefs.GitBranches(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, res.NotARepo)
	assert.Empty(t, res.Branches)
}
