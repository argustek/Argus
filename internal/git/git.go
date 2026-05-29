package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"argus/internal/i18n"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

func dbg(msg string) {
	f, _ := os.OpenFile("C:\\argus-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil { f.WriteString(msg + "\n"); f.Close() }
}

type RepoInfo struct {
	IsRepo        bool   `json:"is_repo"`
	CurrentBranch string `json:"current_branch"`
	RemoteURL     string `json:"remote_url"`
	RemoteName    string `json:"remote_name"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	IsClean       bool   `json:"is_clean"`
}

type CommitLogEntry struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Message   string `json:"message"`
	Date      string `json:"date"`
}

type BranchInfo struct {
	Name     string `json:"name"`
	Current  bool   `json:"current"`
	IsRemote bool   `json:"is_remote"`
}

type RemoteInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type FileDiff struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Content   string `json:"content"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type CloneResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func openRepo(dir string) (*git.Repository, error) {
	return git.PlainOpen(dir)
}

type GitCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var credentials = &GitCredentials{}

func SetCredentials(user, pass string) {
	credentials.Username = user
	credentials.Password = pass
}

func GetAuth() (ssh.AuthMethod, *http.BasicAuth) {
	if credentials.Username != "" && credentials.Password != "" {
		return nil, &http.BasicAuth{
			Username: credentials.Username,
			Password: credentials.Password,
		}
	}
	if user, pass := loadSystemCreds(); user != "" {
		return nil, &http.BasicAuth{Username: user, Password: pass}
	}
	auth, _ := sshAuth()
	return auth, nil
}

func loadSystemCreds() (string, string) {
	input := "protocol=https\nhost=gitee.com\n\n"
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", ""
	}
	var user, pass string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "username=") {
			user = strings.TrimPrefix(line, "username=")
		}
		if strings.HasPrefix(line, "password=") {
			pass = strings.TrimPrefix(line, "password=")
		}
	}
	if user != "" && pass != "" {
		credentials.Username = user
		credentials.Password = pass
		fmt.Println("[Git] 已从系统凭据读取认证:", user)
	}
	return user, pass
}

func sshAuth() (ssh.AuthMethod, error) {
	homeDir, _ := os.UserHomeDir()
	keyPath := homeDir + "\\.ssh\\id_rsa"
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		keyPath = homeDir + "\\.ssh\\id_ed25519"
	}
	sshKey, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("SSH key not found (%s): %v", keyPath, err)
	}
	signer, err := cryptossh.ParsePrivateKey(sshKey)
	if err != nil {
		return nil, fmt.Errorf("parse SSH key failed: %v", err)
	}
	return &ssh.PublicKeys{User: "git", Signer: signer}, nil
}

func toSSHURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.Replace(url, "/", ":", 1)
	return "git@" + url
}

func IsGitRepo(dir string) bool {
	_, err := git.PlainOpen(dir)
	return err == nil
}

func GetRepoInfo(dir string) (info *RepoInfo) {
	defer func() {
		if r := recover(); r != nil {
			dbg("📍 [1] PANIC: " + fmt.Sprint(r))
			info = &RepoInfo{IsRepo: false}
		}
	}()
	dbg("📍 [1] GetRepoInfo START")
	repo, err := git.PlainOpen(dir)
	if err != nil {
		dbg("📍 [1] NOT A REPO")
		return &RepoInfo{IsRepo: false}
	}
	info = &RepoInfo{IsRepo: true}

	head, err := repo.Head()
	if err == nil {
		info.CurrentBranch = head.Name().Short()
	}

	cfg, _ := repo.Config()
	if cfg != nil {
		if origin, ok := cfg.Remotes["origin"]; ok && len(origin.URLs) > 0 {
			info.RemoteURL = origin.URLs[0]
			info.RemoteName = "origin"
		}
	}

	wt, err := repo.Worktree()
	if err == nil {
		status, _ := wt.Status()
		info.IsClean = status.IsClean()
		for _, s := range status {
			if s.Staging != git.Unmodified || s.Worktree != git.Unmodified {
				info.IsClean = false
				break
			}
		}
	}

	dbg("📍 [1] GetRepoInfo DONE")
	return info
}

func GetStatus(dir string) (entries []map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			dbg("📍 [2] PANIC: " + fmt.Sprint(r))
			entries = []map[string]interface{}{}
		}
	}()
	dbg("📍 [2] GetStatus START")
	repo, err := git.PlainOpen(dir)
	if err != nil {
		dbg("📍 [2] FAIL: open repo")
		return []map[string]interface{}{}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return []map[string]interface{}{}
	}
	status, err := wt.Status()
	if err != nil {
		return []map[string]interface{}{}
	}

	entries = []map[string]interface{}{}
	for path, s := range status {
		statusStr := ""
		switch s.Staging {
		case git.Added:
			statusStr += "A"
		case git.Deleted:
			statusStr += "D"
		case git.Modified:
			statusStr += "M"
		case git.Renamed:
			statusStr += "R"
		case git.Copied:
			statusStr += "C"
		default:
			statusStr += " "
		}
		switch s.Worktree {
		case git.Added:
			statusStr += "A"
		case git.Deleted:
			statusStr += "D"
		case git.Modified:
			statusStr += "M"
		case git.Untracked:
			statusStr += "?"
		case git.Renamed:
			statusStr += "R"
		case git.Copied:
			statusStr += "C"
		default:
			statusStr += " "
		}
		entries = append(entries, map[string]interface{}{
			"status": statusStr,
			"path":   path,
		})
	}
	dbg("📍 [2] GetStatus DONE")
	return entries
}

func GetCommitLog(dir string, limit int) ([]CommitLogEntry, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return []CommitLogEntry{}, fmt.Errorf("%s", i18n.T("err.git_log_failed", err))
	}
	head, err := repo.Head()
	if err != nil {
		return []CommitLogEntry{}, fmt.Errorf("%s", i18n.T("err.git_head_failed", err))
	}
	logIter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return []CommitLogEntry{}, fmt.Errorf("%s", i18n.T("err.git_log_failed", err))
	}
	defer logIter.Close()

	entries := []CommitLogEntry{}
	err = logIter.ForEach(func(c *object.Commit) error {
		dateStr := c.Author.When.Format("2006-01-02 15:04")
		entries = append(entries, CommitLogEntry{
			Hash:      c.Hash.String(),
			ShortHash: c.Hash.String()[:7],
			Author:    c.Author.Name,
			Message:   strings.SplitN(c.Message, "\n", 1)[0],
			Date:      dateStr,
		})
		if len(entries) >= limit {
			return fmt.Errorf("limit reached")
		}
		return nil
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func GetBranches(dir string) ([]BranchInfo, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return []BranchInfo{}, fmt.Errorf("%s", i18n.T("err.git_branch_failed", err))
	}
	branches := []BranchInfo{}

	head, _ := repo.Head()
	currentName := ""
	if head != nil {
		currentName = head.Name().Short()
	}

	localIter, _ := repo.Branches()
	if localIter != nil {
		localIter.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().Short()
			branches = append(branches, BranchInfo{
				Name:     name,
				Current:  name == currentName,
				IsRemote: false,
			})
			return nil
		})
	}

	remoteIter, _ := repo.Remotes()
	if remoteIter != nil {
		for _, r := range remoteIter {
			refList, _ := r.List(&git.ListOptions{})
			for _, ref := range refList {
				name := ref.Name().Short()
				if strings.Contains(name, "->") || strings.HasPrefix(name, "HEAD") {
					continue
				}
				branches = append(branches, BranchInfo{
					Name:     name,
					Current:  false,
					IsRemote: true,
				})
			}
		}
	}

	return branches, nil
}

func SwitchBranch(dir, branch string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	err = wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_checkout_failed", err))
	}
	return nil
}

func CreateBranch(dir, name string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	err = wt.Checkout(&git.CheckoutOptions{
		Create: true,
		Branch: plumbing.NewBranchReferenceName(name),
	})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_create_branch", err))
	}
	return nil
}

func GetRemotes(dir string) ([]RemoteInfo, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return []RemoteInfo{}, fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	remotes, err := repo.Remotes()
	if err != nil {
		return []RemoteInfo{}, nil
	}
	result := []RemoteInfo{}
	for _, r := range remotes {
		urls := r.Config().URLs
		if len(urls) > 0 {
			result = append(result, RemoteInfo{Name: r.Config().Name, URL: urls[0]})
		}
	}
	return result, nil
}

func AddRemote(dir, name, url string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	repo.DeleteRemote(name)
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_add_remote", err))
	}
	return nil
}

func RemoveRemote(dir, name string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	err = repo.DeleteRemote(name)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_remove_remote", err))
	}
	return nil
}

func Clone(url, dir, branch string) *CloneResult {
	sshAuth, httpAuth := GetAuth()
	opts := &git.CloneOptions{
		URL:  url,
		Auth: sshAuth,
	}
	if httpAuth != nil {
		opts.Auth = httpAuth
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}
	_, err := git.PlainClone(dir, false, opts)
	result := &CloneResult{}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
		result.Output = fmt.Sprintf("克隆成功: %s -> %s", url, dir)
	}
	return result
}

func Push(dir, remote, branch string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_not_repo"))
	}
	if remote == "" {
		remote = "origin"
	}
	if branch == "" {
		head, _ := repo.Head()
		if head != nil {
			branch = head.Name().Short()
		} else {
			return "", fmt.Errorf("%s", i18n.T("err.git_no_branch"))
		}
	}
	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	sshAuth, httpAuth := GetAuth()
	pushOpts := &git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{refSpec},
	}
	if sshAuth != nil {
		pushOpts.Auth = sshAuth
	}
	if httpAuth != nil {
		pushOpts.Auth = httpAuth
	}
	err = repo.Push(pushOpts)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_push_failed", err))
	}
	return fmt.Sprintf("推送到 %s 成功 (%s)", remote, branch), nil
}

func Pull(dir, remote, branch string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_not_repo"))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	if remote == "" {
		remote = "origin"
	}
	if branch == "" {
		head, _ := repo.Head()
		if head != nil {
			branch = head.Name().Short()
		} else {
			return "", fmt.Errorf("%s", i18n.T("err.git_no_branch"))
		}
	}

	gitRemote, err := repo.Remote(remote)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_remote_not_exist", remote, err))
	}

	sshAuth, httpAuth := GetAuth()
	fetchOpts := &git.FetchOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch))},
	}
	if sshAuth != nil {
		fetchOpts.Auth = sshAuth
	}
	if httpAuth != nil {
		fetchOpts.Auth = httpAuth
	}

	err = gitRemote.Fetch(fetchOpts)
	if err == git.NoErrAlreadyUpToDate {
		return "已经是最新的", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_pull_failed", err))
	}

	remoteRefName := plumbing.NewRemoteReferenceName(remote, branch)
	remoteRef, err := repo.Reference(remoteRefName, true)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_fetch_remote", err))
	}

	err = wt.Reset(&git.ResetOptions{
		Mode:   git.MergeReset,
		Commit: remoteRef.Hash(),
	})
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_merge_failed", err))
	}

	return fmt.Sprintf("从 %s/%s 拉取成功", remote, branch), nil
}

func StageFile(dir, path string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	_, err = wt.Add(path)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_stage_failed", err))
	}
	return nil
}

func UnstageFile(dir, path string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	err = wt.Reset(&git.ResetOptions{
		Mode:   git.MixedReset,
		Files:  []string{path},
	})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_unstage_failed", err))
	}
	return nil
}

func Commit(dir, message string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	hash, err := wt.Commit(message, &git.CommitOptions{
		All: true,
	})
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_commit_failed", err))
	}
	return hash.String(), nil
}

func RestoreFile(dir, path string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	err = wt.Checkout(&git.CheckoutOptions{
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_restore_failed", err))
	}
	return nil
}

func DiscardAllChanges(dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_open_repo", err))
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_worktree", err))
	}
	err = wt.Checkout(&git.CheckoutOptions{Force: true})
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("err.git_discard_failed", err))
	}
	return "已丢弃所有更改", nil
}

func GetFileDiff(dir, path string) (result *FileDiff) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[Git] GetFileDiff panic: %v, path=%s\n", r, path)
			result = &FileDiff{Path: path, Status: "?", Content: fmt.Sprintf("(diff 错误: %v)", r)}
		}
	}()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return &FileDiff{Path: path, Status: "!", Content: err.Error()}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return &FileDiff{Path: path, Status: "!", Content: err.Error()}
	}
	status, _ := wt.Status()

	statusStr := "?"
	fileStatus, ok := status[path]
	if ok {
		switch fileStatus.Worktree {
		case git.Added:
			statusStr = "A"
		case git.Deleted:
			statusStr = "D"
		case git.Modified:
			statusStr = "M"
		case git.Untracked:
			statusStr = "?"
		case git.Renamed:
			statusStr = "R"
		}
	}

	headRef, _ := repo.Head()
	if headRef == nil {
		content, _ := os.ReadFile(filepath.Join(dir, path))
		return &FileDiff{
			Path:      path,
			Status:    statusStr,
			Content:   string(content),
			Additions: len(strings.Split(string(content), "\n")),
		}
	}

	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return &FileDiff{Path: path, Status: statusStr}
	}

	adds, dels := 0, 0
	diffText := ""

	switch statusStr {
	case "A", "?":
		content, readErr := os.ReadFile(filepath.Join(dir, path))
		if readErr == nil {
			lines := strings.Split(string(content), "\n")
			for _, l := range lines {
				diffText += "+" + l + "\n"
				adds++
			}
		}
	case "D":
		tree, _ := headCommit.Tree()
		if tree != nil {
			oldBlob, fErr := tree.File(path)
			if fErr == nil && oldBlob != nil {
				oldContent, _ := oldBlob.Contents()
				for _, l := range strings.Split(oldContent, "\n") {
					diffText += "-" + l + "\n"
					dels++
				}
			}
		}
	default:
		tree, _ := headCommit.Tree()
		var oldLines []string
		if tree != nil {
			oldBlob, fErr := tree.File(path)
			if fErr == nil && oldBlob != nil {
				oldContent, _ := oldBlob.Contents()
				oldLines = strings.Split(oldContent, "\n")
			}
		}
		newContent, readErr := os.ReadFile(filepath.Join(dir, path))
		newLines := []string{}
		if readErr == nil {
			newLines = strings.Split(string(newContent), "\n")
		}
		diffText, adds, dels = computeDiff(oldLines, newLines)
	}

	if diffText == "" && statusStr == "M" {
		diffText = fmt.Sprintf("(文件: %s 状�? %s 无可见差�?", path, statusStr)
	}

	return &FileDiff{
		Path:      path,
		Status:    statusStr,
		Content:   diffText,
		Additions: adds,
		Deletions: dels,
	}
}

func computeDiff(oldLines, newLines []string) (string, int, int) {
	var sb strings.Builder
	adds, dels := 0, 0
	oi, ni := 0, 0
	for oi < len(oldLines) || ni < len(newLines) {
		if oi < len(oldLines) && ni < len(newLines) && oldLines[oi] == newLines[ni] {
			sb.WriteString(" " + oldLines[oi] + "\n")
			oi++
			ni++
		} else if oi >= len(oldLines) || (ni < len(newLines) && (oi > 0 && oldLines[oi-1] == newLines[ni])) {
			sb.WriteString("+" + newLines[ni] + "\n")
			adds++
			ni++
		} else if ni >= len(newLines) || (oi < len(oldLines) && (ni > 0 && newLines[ni-1] == oldLines[oi])) {
			sb.WriteString("-" + oldLines[oi] + "\n")
			dels++
			oi++
		} else {
			sb.WriteString("-" + oldLines[oi] + "\n")
			sb.WriteString("+" + newLines[ni] + "\n")
			dels++
			adds++
			oi++
			ni++
		}
	}
	return sb.String(), adds, dels
}

func GetCommitDiff(dir, hash string) (result *FileDiff) {
	defer func() {
		if r := recover(); r != nil {
			result = &FileDiff{Path: hash, Status: "!", Content: fmt.Sprintf("(diff 错误: %v)", r)}
		}
	}()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return &FileDiff{Path: hash, Status: "!", Content: err.Error()}
	}
	h := plumbing.NewHash(hash)
	commit, err := repo.CommitObject(h)
	if err != nil {
		return &FileDiff{Path: hash, Status: "!", Content: "找不到该提交: " + err.Error()}
	}
	parent, err := commit.Parent(0)
	if err != nil {
		tree, _ := commit.Tree()
		if tree != nil {
			var sb strings.Builder
			adds := 0
			tree.Files().ForEach(func(f *object.File) error {
				content, _ := f.Contents()
				for _, line := range strings.Split(content, "\n") {
					sb.WriteString("+" + line + "\n")
					adds++
				}
				return nil
			})
			return &FileDiff{
				Path:      commit.Hash.String()[:8],
				Status:    "A",
				Content:   sb.String(),
				Additions: adds,
			}
		}
		return &FileDiff{Path: hash, Status: "?", Content: "(初始提交，无父节点)"}
	}
	patch, err := commit.Patch(parent)
	if err != nil {
		return &FileDiff{Path: hash, Status: "!", Content: "生成 diff 失败: " + err.Error()}
	}
	var sb strings.Builder
	adds, dels := 0, 0
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		fromPath := "/dev/null"
		toPath := "/dev/null"
		if from != nil {
			fromPath = from.Path()
		}
		if to != nil {
			toPath = to.Path()
		}
		sb.WriteString(fmt.Sprintf("--- %s\n+++ %s\n", fromPath, toPath))
		for _, chunk := range fp.Chunks() {
			content := chunk.Content()
			switch chunk.Type() {
			case 0:
				sb.WriteString(" " + content)
			case 1:
				sb.WriteString("+" + content)
				adds++
			case 2:
				sb.WriteString("-" + content)
				dels++
			}
		}
	}
	return &FileDiff{
		Path:      commit.Hash.String()[:8],
		Status:    "M",
		Content:   sb.String(),
		Additions: adds,
		Deletions: dels,
	}
}

func InitRepo(dir string) error {
	_, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.git_init_failed", err))
	}
	return nil
}
