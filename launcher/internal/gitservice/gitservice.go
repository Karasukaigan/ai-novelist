package gitservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// GitService 提供Git操作服务
type GitService struct {
	projectDir string
}

// NewGitService 创建Git服务实例
func NewGitService(projectDir string) *GitService {
	return &GitService{projectDir: projectDir}
}

// SetProjectDir 设置项目目录
func (s *GitService) SetProjectDir(projectDir string) {
	s.projectDir = projectDir
}

// openRepo 打开仓库
func (s *GitService) openRepo() (*git.Repository, error) {
	if s.projectDir == "" {
		return nil, fmt.Errorf("项目目录未设置")
	}
	return git.PlainOpen(s.projectDir)
}

// StatusResponse Git状态响应
type StatusResponse struct {
	Branch         string   `json:"branch"`
	Dirty          bool     `json:"dirty"`
	UntrackedFiles []string `json:"untracked_files"`
	Changes        []Change `json:"changes"`
	ModifiedFiles  []string `json:"modified_files"`
}

// Change 变更信息
type Change struct {
	Path       string `json:"path"`
	ChangeType string `json:"change_type"`
}

// GetStatus 获取Git状态
func (s *GitService) GetStatus() (*StatusResponse, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	// 获取当前分支
	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取HEAD失败: %w", err)
	}
	branch := head.Name().Short()

	// 获取工作区状态
	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作区失败: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return nil, fmt.Errorf("获取状态失败: %w", err)
	}

	// 获取未跟踪文件
	var untrackedFiles []string
	var changes []Change
	var modifiedFiles []string

	for file, fileStatus := range status {
		if fileStatus.Staging == git.Untracked && fileStatus.Worktree == git.Untracked {
			untrackedFiles = append(untrackedFiles, file)
		} else {
			// 有更改的文件
			changeType := "M"
			if fileStatus.Staging == git.Added || fileStatus.Worktree == git.Added {
				changeType = "A"
			} else if fileStatus.Staging == git.Deleted || fileStatus.Worktree == git.Deleted {
				changeType = "D"
			}

			// 标准化路径
			filePath := strings.TrimPrefix(file, "./")

			changes = append(changes, Change{
				Path:       filePath,
				ChangeType: changeType,
			})
			modifiedFiles = append(modifiedFiles, filePath)
		}
	}

	return &StatusResponse{
		Branch:         branch,
		Dirty:          !status.IsClean(),
		UntrackedFiles: untrackedFiles,
		Changes:        changes,
		ModifiedFiles:  modifiedFiles,
	}, nil
}

// CheckpointInfo 检查点信息
type CheckpointInfo struct {
	CommitHash string `json:"commit_hash"`
	ShortHash  string `json:"short_hash"`
	Message    string `json:"message"`
}

// ListCheckpoints 列出所有检查点
func (s *GitService) ListCheckpoints() ([]CheckpointInfo, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取日志失败: %w", err)
	}
	defer iter.Close()

	var checkpoints []CheckpointInfo
	for {
		commit, err := iter.Next()
		if err != nil {
			break
		}

		checkpoints = append(checkpoints, CheckpointInfo{
			CommitHash: commit.Hash.String(),
			ShortHash:  commit.Hash.String()[:8],
			Message:    strings.TrimSpace(commit.Message),
		})
	}

	return checkpoints, nil
}

// SaveCheckpointRequest 保存检查点请求
type SaveCheckpointRequest struct {
	Message string `json:"message"`
}

// SaveCheckpointResponse 保存检查点响应
type SaveCheckpointResponse struct {
	Success    bool   `json:"success"`
	CommitHash string `json:"commit_hash,omitempty"`
	ShortHash  string `json:"short_hash,omitempty"`
	Message    string `json:"message,omitempty"`
}

// SaveCheckpoint 保存检查点
func (s *GitService) SaveCheckpoint(req *SaveCheckpointRequest) (*SaveCheckpointResponse, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作区失败: %w", err)
	}

	// 添加所有文件（包括未跟踪的文件）
	err = w.AddWithOptions(&git.AddOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("添加文件失败: %w", err)
	}

	// 获取状态检查是否有更改
	status, err := w.Status()
	if err != nil {
		return nil, fmt.Errorf("获取状态失败: %w", err)
	}

	if status.IsClean() {
		return &SaveCheckpointResponse{
			Success: false,
			Message: "没有更改需要提交",
		}, nil
	}

	// 生成提交消息
	message := req.Message
	if message == "" {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		message = fmt.Sprintf("Checkpoint: %s", timestamp)
	}

	// 创建提交
	commit, err := w.Commit(message, &git.CommitOptions{})
	if err != nil {
		return nil, fmt.Errorf("创建提交失败: %w", err)
	}

	return &SaveCheckpointResponse{
		Success:    true,
		CommitHash: commit.String(),
		ShortHash:  commit.String()[:8],
		Message:    message,
	}, nil
}

// RestoreCheckpointRequest 恢复检查点请求
type RestoreCheckpointRequest struct {
	CommitHash string `json:"commit_hash"`
}

// RestoreCheckpointResponse 恢复检查点响应
type RestoreCheckpointResponse struct {
	Success    bool   `json:"success"`
	CommitHash string `json:"commit_hash,omitempty"`
	ShortHash  string `json:"short_hash,omitempty"`
	Message    string `json:"message,omitempty"`
}

// RestoreCheckpoint 恢复检查点
func (s *GitService) RestoreCheckpoint(req *RestoreCheckpointRequest) (*RestoreCheckpointResponse, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作区失败: %w", err)
	}

	hash := plumbing.NewHash(req.CommitHash)

	// 获取提交信息
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("获取提交失败: %w", err)
	}

	// 硬重置到指定提交
	err = w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: hash,
	})
	if err != nil {
		return nil, fmt.Errorf("重置失败: %w", err)
	}

	// 清理未跟踪的文件
	err = w.Clean(&git.CleanOptions{
		Dir: true,
	})
	if err != nil {
		return nil, fmt.Errorf("清理未跟踪文件失败: %w", err)
	}

	return &RestoreCheckpointResponse{
		Success:    true,
		CommitHash: commit.Hash.String(),
		ShortHash:  commit.Hash.String()[:8],
		Message:    strings.TrimSpace(commit.Message),
	}, nil
}

// FileChange 文件变更详情
type FileChange struct {
	Path       string `json:"path"`
	ChangeType string `json:"change_type"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
}

// DiffResponse 差异响应
type DiffResponse struct {
	Success         bool         `json:"success"`
	CommitHash      string       `json:"commit_hash,omitempty"`
	ShortHash       string       `json:"short_hash,omitempty"`
	Changes         []FileChange `json:"changes,omitempty"`
	IsInitialCommit bool         `json:"is_initial_commit,omitempty"`
	Message         string       `json:"message,omitempty"`
}

// GetCheckpointDiff 获取检查点差异
func (s *GitService) GetCheckpointDiff(commitHash string) (*DiffResponse, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	hash := plumbing.NewHash(commitHash)
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("获取提交失败: %w", err)
	}

	// 获取父提交
	parentIter := commit.Parents()
	parentCommit, err := parentIter.Next()
	if err != nil {
		// 初始提交，返回空差异
		return &DiffResponse{
			Success:         true,
			CommitHash:      commitHash,
			ShortHash:       commitHash[:8],
			Changes:         []FileChange{},
			IsInitialCommit: true,
		}, nil
	}

	// 获取差异
	changes, err := parentCommit.Stats()
	if err != nil {
		return nil, fmt.Errorf("获取差异统计失败: %w", err)
	}

	// 获取详细的diff内容
	patch, err := parentCommit.Patch(commit)
	if err != nil {
		return nil, fmt.Errorf("获取patch失败: %w", err)
	}

	var fileChanges []FileChange
	seenPaths := make(map[string]bool)

	for _, filePatch := range patch.FilePatches() {
		if filePatch.IsBinary() {
			continue
		}

		from, to := filePatch.Files()
		var filePath string
		var changeType string

		if from == nil {
			// 新增文件
			filePath = to.Path()
			changeType = "A"
		} else if to == nil {
			// 删除文件
			filePath = from.Path()
			changeType = "D"
		} else {
			// 修改文件
			filePath = to.Path()
			changeType = "M"
		}

		// 标准化路径
		filePath = strings.TrimPrefix(filePath, "./")

		// 去重
		if seenPaths[filePath] {
			continue
		}
		seenPaths[filePath] = true

		// 获取文件内容
		change := FileChange{
			Path:       filePath,
			ChangeType: changeType,
		}

		// 获取旧内容（父提交中的版本）
		if changeType == "M" || changeType == "D" {
			if fromFile, err := parentCommit.File(filePath); err == nil {
				if content, err := fromFile.Contents(); err == nil {
					change.OldContent = content
				}
			}
		}

		// 获取新内容（当前提交中的版本）
		if changeType == "M" || changeType == "A" {
			if toFile, err := commit.File(filePath); err == nil {
				if content, err := toFile.Contents(); err == nil {
					change.NewContent = content
				}
			}
		}

		fileChanges = append(fileChanges, change)
	}

	// 如果没有详细patch，使用stats
	if len(fileChanges) == 0 && len(changes) > 0 {
		for _, change := range changes {
			filePath := strings.TrimPrefix(change.Name, "./")

			if seenPaths[filePath] {
				continue
			}
			seenPaths[filePath] = true

			fileChanges = append(fileChanges, FileChange{
				Path:       filePath,
				ChangeType: "M",
			})
		}
	}

	return &DiffResponse{
		Success:    true,
		CommitHash: commitHash,
		ShortHash:  commitHash[:8],
		Changes:    fileChanges,
	}, nil
}

// WorkingDiffResponse 工作区差异响应
type WorkingDiffResponse struct {
	Success    bool   `json:"success"`
	Path       string `json:"path,omitempty"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Message    string `json:"message,omitempty"`
}

// GetWorkingDiff 获取工作区差异
func (s *GitService) GetWorkingDiff(filePath string) (*WorkingDiffResponse, error) {
	repo, err := s.openRepo()
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	// 读取当前工作区的文件内容（新内容）
	fullPath := filepath.Join(s.projectDir, filePath)
	var newContent string
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件被删除，新内容为空
			newContent = ""
		} else {
			return nil, fmt.Errorf("读取文件失败: %w", err)
		}
	} else {
		newContent = string(content)
	}

	// 获取最新提交中的文件内容（旧内容）
	var oldContent string
	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取HEAD失败: %w", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("获取提交失败: %w", err)
	}

	// 尝试获取文件在最新提交中的内容
	file, err := commit.File(filePath)
	if err == nil {
		oldContent, _ = file.Contents()
	} else {
		// 文件在最新提交中不存在（新文件）
		oldContent = ""
	}

	return &WorkingDiffResponse{
		Success:    true,
		Path:       filePath,
		OldContent: oldContent,
		NewContent: newContent,
	}, nil
}

// InitRepoResponse 初始化仓库响应
type InitRepoResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// InitRepo 初始化Git仓库
func (s *GitService) InitRepo() (*InitRepoResponse, error) {
	if s.projectDir == "" {
		return nil, fmt.Errorf("项目目录未设置")
	}

	gitDir := filepath.Join(s.projectDir, ".git")

	// 如果Git仓库已存在，跳过初始化
	if _, err := os.Stat(gitDir); err == nil {
		return &InitRepoResponse{
			Success: true,
			Message: "Git仓库已存在，跳过初始化",
		}, nil
	}

	// 初始化Git仓库
	repo, err := git.PlainInit(s.projectDir, false)
	if err != nil {
		return nil, fmt.Errorf("初始化仓库失败: %w", err)
	}

	// 配置Git用户信息
	cfg, err := repo.Config()
	if err != nil {
		return nil, fmt.Errorf("获取配置失败: %w", err)
	}
	cfg.User.Name = "AI Novelist"
	cfg.User.Email = "noreply@ai-novelist.local"
	if err := repo.SetConfig(cfg); err != nil {
		return nil, fmt.Errorf("设置配置失败: %w", err)
	}

	// 获取工作区
	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作区失败: %w", err)
	}

	// 第一步：创建完全空的初始提交
	now := time.Now()
	emptyCommit, err := w.Commit("Initial commit (empty)", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AI Novelist",
			Email: "noreply@ai-novelist.local",
			When:  now,
		},
		Committer: &object.Signature{
			Name:  "AI Novelist",
			Email: "noreply@ai-novelist.local",
			When:  now,
		},
		AllowEmptyCommits: true,
	})
	if err != nil {
		return nil, fmt.Errorf("创建空初始提交失败: %w", err)
	}

	// 第二步：添加所有文件并提交
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return nil, fmt.Errorf("添加文件失败: %w", err)
	}

	_, err = w.Commit("Initial checkpoint", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AI Novelist",
			Email: "noreply@ai-novelist.local",
			When:  now,
		},
		Committer: &object.Signature{
			Name:  "AI Novelist",
			Email: "noreply@ai-novelist.local",
			When:  now,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建初始存档点失败: %w", err)
	}

	return &InitRepoResponse{
		Success: true,
		Message: fmt.Sprintf("Git仓库初始化成功, 空提交: %s", emptyCommit.String()[:8]),
	}, nil
}
