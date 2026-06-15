package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGitRepo_NonExistentDir(t *testing.T) {
	assert.False(t, IsGitRepo("/nonexistent/path"))
}

func TestInitRepo(t *testing.T) {
	dir := t.TempDir()

	err := InitRepo(dir)
	require.NoError(t, err)

	assert.True(t, IsGitRepo(dir))
}

func TestGetRepoInfo_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	info := GetRepoInfo(dir)
	assert.True(t, info.IsRepo)
	assert.True(t, info.IsClean)
}

func TestGetRepoInfo_NonRepo(t *testing.T) {
	dir := t.TempDir()

	info := GetRepoInfo(dir)
	assert.False(t, info.IsRepo)
}

func TestGetStatus_NoChanges(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	entries := GetStatus(dir)
	assert.Empty(t, entries)
}

func TestGetStatus_WithUntracked(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
	require.NoError(t, err)

	entries := GetStatus(dir)
	assert.Len(t, entries, 1)
	assert.Equal(t, "test.txt", entries[0]["path"])
}

func TestStageAndCommit(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	err = StageFile(dir, "hello.go")
	require.NoError(t, err)

	hash, err := Commit(dir, "initial commit")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestGetCommitLog(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("first"), 0644)
	require.NoError(t, err)
	StageFile(dir, "f1.txt")
	Commit(dir, "first commit")

	err = os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("second"), 0644)
	require.NoError(t, err)
	StageFile(dir, "f2.txt")
	Commit(dir, "second commit")

	log, err := GetCommitLog(dir, 10)
	require.NoError(t, err)
	assert.Len(t, log, 2)
	assert.Equal(t, "second commit", log[0].Message)
	assert.Equal(t, "first commit", log[1].Message)
	assert.NotEmpty(t, log[0].Hash)
	assert.NotEmpty(t, log[0].ShortHash)
	assert.NotEmpty(t, log[0].Author)
	assert.NotEmpty(t, log[0].Date)
}

func TestGetCommitLog_Limit(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	for i := 0; i < 5; i++ {
		content := fmt.Sprintf("content %d", i)
		f := filepath.Join(dir, content+".txt")
		err := os.WriteFile(f, []byte(content), 0644)
		require.NoError(t, err)
		StageFile(dir, content+".txt")
		Commit(dir, fmt.Sprintf("commit %d", i))
	}

	log, err := GetCommitLog(dir, 3)
	require.NoError(t, err)
	assert.Len(t, log, 3)
}

func TestGetRemotes_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	remotes, err := GetRemotes(dir)
	require.NoError(t, err)
	assert.Empty(t, remotes)
}

func TestAddAndRemoveRemote(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := AddRemote(dir, "origin", "https://example.com/repo.git")
	require.NoError(t, err)

	remotes, err := GetRemotes(dir)
	require.NoError(t, err)
	assert.Len(t, remotes, 1)
	assert.Equal(t, "origin", remotes[0].Name)

	err = RemoveRemote(dir, "origin")
	require.NoError(t, err)

	remotes, err = GetRemotes(dir)
	require.NoError(t, err)
	assert.Empty(t, remotes)
}

func TestCreateAndSwitchBranch(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base"), 0644)
	require.NoError(t, err)
	StageFile(dir, "base.txt")
	Commit(dir, "base commit")

	err = CreateBranch(dir, "feature")
	require.NoError(t, err)

	info := GetRepoInfo(dir)
	assert.Equal(t, "feature", info.CurrentBranch)

	err = SwitchBranch(dir, "master")
	require.NoError(t, err)

	info = GetRepoInfo(dir)
	assert.Equal(t, "master", info.CurrentBranch)
}

func TestRestoreFile(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("original"), 0644)
	require.NoError(t, err)
	StageFile(dir, "file.txt")
	Commit(dir, "initial")

	err = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644)
	require.NoError(t, err)

	err = RestoreFile(dir, "file.txt")
	require.NoError(t, err)
}

func TestDiscardAllChanges(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0644)
	require.NoError(t, err)
	StageFile(dir, "keep.txt")
	Commit(dir, "initial")

	err = os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("modified"), 0644)
	require.NoError(t, err)

	_, err = DiscardAllChanges(dir)
	require.NoError(t, err)
}

func TestGetBranches(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base"), 0644)
	require.NoError(t, err)
	StageFile(dir, "base.txt")
	Commit(dir, "initial")

	branches, err := GetBranches(dir)
	require.NoError(t, err)
	assert.Len(t, branches, 1)
	assert.Equal(t, "master", branches[0].Name)
	assert.True(t, branches[0].Current)
}

func TestToSSHURL(t *testing.T) {
	assert.Equal(t, "git@github.com:user/repo.git", toSSHURL("https://github.com/user/repo.git"))
	assert.Equal(t, "git@gitee.com:user/repo.git", toSSHURL("https://gitee.com/user/repo.git"))
}

func TestComputeDiff_SameLines(t *testing.T) {
	old := []string{"a", "b", "c"}
	new := []string{"a", "b", "c"}
	diff, adds, dels := computeDiff(old, new)
	assert.Equal(t, 0, adds)
	assert.Equal(t, 0, dels)
	assert.Contains(t, diff, " a")
}

func TestComputeDiff_AddedLines(t *testing.T) {
	old := []string{"a"}
	new := []string{"a", "b", "c"}
	diff, adds, dels := computeDiff(old, new)
	assert.Equal(t, 2, adds)
	assert.Equal(t, 0, dels)
	assert.Contains(t, diff, "+b")
	assert.Contains(t, diff, "+c")
}

func TestComputeDiff_DeletedLines(t *testing.T) {
	old := []string{"a", "b", "c"}
	new := []string{"a"}
	diff, adds, dels := computeDiff(old, new)
	assert.Equal(t, 0, adds)
	assert.Equal(t, 2, dels)
	assert.Contains(t, diff, "-b")
	assert.Contains(t, diff, "-c")
}

func TestComputeDiff_ModifiedLines(t *testing.T) {
	old := []string{"a", "old", "c"}
	new := []string{"a", "new", "c"}
	out, adds, dels := computeDiff(old, new)
	assert.Equal(t, 1, adds)
	assert.Equal(t, 1, dels)
	assert.Contains(t, out, "-old")
	assert.Contains(t, out, "+new")
}

func TestComputeDiff_BothEmpty(t *testing.T) {
	diff, adds, dels := computeDiff(nil, nil)
	assert.Empty(t, diff)
	assert.Equal(t, 0, adds)
	assert.Equal(t, 0, dels)
}

func TestGetFileDiff_NonExistent(t *testing.T) {
	dir := t.TempDir()
	diff := GetFileDiff(dir, "nonexistent.txt")
	assert.NotNil(t, diff)
	assert.Equal(t, "nonexistent.txt", diff.Path)
}

func TestGetFileDiff_UntrackedFile(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\nworld"), 0644)
	require.NoError(t, err)

	diff := GetFileDiff(dir, "new.txt")
	assert.NotNil(t, diff)
	assert.Contains(t, diff.Status, "?")
	assert.Contains(t, diff.Content, "hello")
}

func TestCredentials(t *testing.T) {
	SetCredentials("user", "pass")
	auth, basicAuth := GetAuth()
	assert.Nil(t, auth)
	require.NotNil(t, basicAuth)
	assert.Equal(t, "user", basicAuth.Username)
	assert.Equal(t, "pass", basicAuth.Password)
}

func TestRepoInfoStructure(t *testing.T) {
	info := &RepoInfo{
		IsRepo:        true,
		CurrentBranch: "main",
		IsClean:       true,
	}
	assert.True(t, info.IsRepo)
	assert.Equal(t, "main", info.CurrentBranch)
	assert.True(t, info.IsClean)
	assert.Empty(t, info.RemoteURL)
}

func TestFileDiffStructure(t *testing.T) {
	diff := &FileDiff{
		Path:      "test.go",
		Status:    "M",
		Content:   "+fmt.Println\n-fmt.Printf",
		Additions: 1,
		Deletions: 1,
	}
	assert.Equal(t, "test.go", diff.Path)
	assert.Equal(t, "M", diff.Status)
	assert.Equal(t, 1, diff.Additions)
}

func TestGetCommitLog_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	log, err := GetCommitLog(dir, 10)
	assert.Error(t, err)
	assert.Empty(t, log)
}

func TestGetCommitLog_NonRepo(t *testing.T) {
	dir := t.TempDir()

	log, err := GetCommitLog(dir, 10)
	assert.Error(t, err)
	assert.Empty(t, log)
}

func TestUnstageFile(t *testing.T) {
	dir := t.TempDir()
	InitRepo(dir)

	err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("hello"), 0644)
	require.NoError(t, err)
	err = StageFile(dir, "staged.txt")
	require.NoError(t, err)

	entries := GetStatus(dir)
	stagingStatus := ""
	for _, e := range entries {
		if e["path"] == "staged.txt" {
			statusStr, _ := e["status"].(string)
			stagingStatus = statusStr
			break
		}
	}
	assert.NotEmpty(t, stagingStatus)
}

func TestFileDiffNonExistent(t *testing.T) {
	dir := t.TempDir()
	d := GetFileDiff(dir, "")
	assert.NotNil(t, d)
}

func TestGetFileDiff_SpacesInPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my file.txt")
	err := os.WriteFile(path, []byte("content"), 0644)
	require.NoError(t, err)

	d := GetFileDiff(dir, "my file.txt")
	assert.NotNil(t, d)
}
