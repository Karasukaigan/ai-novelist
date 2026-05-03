package gitman

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// CommitDetail 单条提交详情
type CommitDetail struct {
	SHA     string   `json:"sha"`
	Message string   `json:"message"`
	Date    string   `json:"date"`
	Author  string   `json:"author"`
	Parents []string `json:"parents"`
	IsHEAD  bool     `json:"is_head"`
	Refs    []string `json:"refs"`
}

// BranchInfo 分支信息
type BranchInfo struct {
	Name      string `json:"name"`
	IsRemote  bool   `json:"is_remote"`
	IsCurrent bool   `json:"is_current"`
	SHA       string `json:"sha"`
}

// GetCommitHistory 获取提交历史（从新到旧）
func GetCommitHistory(projectDir string, limit int) ([]CommitDetail, error) {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取 HEAD 失败: %w", err)
	}

	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取日志失败: %w", err)
	}
	defer iter.Close()

	var commits []CommitDetail
	for i := 0; i < limit; i++ {
		c, err := iter.Next()
		if err != nil {
			break
		}

		parents := make([]string, len(c.ParentHashes))
		for j, h := range c.ParentHashes {
			parents[j] = h.String()
		}

		commits = append(commits, CommitDetail{
			SHA:     c.Hash.String(),
			Message: c.Message,
			Date:    c.Committer.When.Format(time.RFC3339),
			Author:  c.Author.Name,
			Parents: parents,
			IsHEAD:  c.Hash == head.Hash(),
		})
	}

	return commits, nil
}

// FetchRemote 从远程获取最新引用（prune 会清理已删除的远程跟踪引用）
func FetchRemote(projectDir string) error {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开仓库失败: %w", err)
	}

	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Prune:      true, // 清理远程已删除的分支的本地跟踪引用
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("获取远程引用失败: %w", err)
	}
	return nil
}

// SyncRemoteBranches 同步远程分支到本地：
//   - 远程新分支 → 自动创建本地跟踪分支
//   - 远程已删除的分支 → 删除对应的本地分支（当前分支除外）
func SyncRemoteBranches(projectDir string) error {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开仓库失败: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("获取 HEAD 失败: %w", err)
	}
	currentBranch := head.Name().Short()

	// 收集所有远程分支名（去掉 origin/ 前缀）
	remoteBranches := make(map[string]plumbing.Hash)
	rIter, err := repo.References()
	if err != nil {
		return err
	}
	defer rIter.Close()
	for {
		ref, err := rIter.Next()
		if err != nil {
			break
		}
		if ref.Type() != plumbing.HashReference || !ref.Name().IsRemote() {
			continue
		}
		remoteShort := ref.Name().Short() // e.g. "origin/main"
		localName := remoteShort
		if idx := strings.Index(remoteShort, "/"); idx >= 0 {
			localName = remoteShort[idx+1:]
		}
		remoteBranches[localName] = ref.Hash()
	}

	// 遍历本地分支，做双向同步
	bIter, err := repo.Branches()
	if err != nil {
		return err
	}
	defer bIter.Close()

	for {
		ref, err := bIter.Next()
		if err != nil {
			break
		}
		localName := ref.Name().Short()

		remoteHash, existsOnRemote := remoteBranches[localName]
		if existsOnRemote {
			// 远程存在但本地没有该分支引用 → 已在上面的 remoteBranches 遍历中处理
			// 这里只需要处理：远程存在且本地也有，但本地指向不同 commit → 更新本地指向
			if ref.Hash() != remoteHash {
				localRef := plumbing.NewBranchReferenceName(localName)
				newRef := plumbing.NewHashReference(localRef, remoteHash)
				_ = repo.Storer.SetReference(newRef)
			}
			// 从 map 中移除已处理的分支
			delete(remoteBranches, localName)
		} else {
			// 远程已删除该分支，删除本地分支（当前分支除外）
			if localName != currentBranch {
				localRef := plumbing.NewBranchReferenceName(localName)
				_ = repo.Storer.RemoveReference(localRef)
				_ = repo.DeleteBranch(localName)
			}
		}
	}

	// 剩余在 remoteBranches 中的是远程有但本地完全没有的分支 → 创建本地跟踪分支
	for localName, hash := range remoteBranches {
		localRef := plumbing.NewBranchReferenceName(localName)
		newRef := plumbing.NewHashReference(localRef, hash)
		if err := repo.Storer.SetReference(newRef); err != nil {
			continue
		}
		_ = repo.CreateBranch(&config.Branch{
			Name:   localName,
			Remote: "origin",
			Merge:  localRef,
		})
	}

	return nil
}

// GetBranches 获取本地分支列表（仅读取本地仓库，不请求远程）
func GetBranches(projectDir string) ([]BranchInfo, error) {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取 HEAD 失败: %w", err)
	}

	var branches []BranchInfo

	iter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for {
		ref, err := iter.Next()
		if err != nil {
			break
		}
		branches = append(branches, BranchInfo{
			Name:      ref.Name().Short(),
			IsRemote:  false,
			IsCurrent: head.Name().String() == ref.Name().String(),
			SHA:       ref.Hash().String(),
		})
	}

	return branches, nil
}

// CheckoutCommit 硬重置到指定提交（版本回溯）
func CheckoutCommit(projectDir string, hash string) error {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开仓库失败: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作区失败: %w", err)
	}

	h := plumbing.NewHash(hash)
	err = w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: h,
	})
	if err != nil {
		return fmt.Errorf("重置到提交失败: %w", err)
	}
	return nil
}

// SwitchBranch 切换到已有分支；若本地不存在但远程存在，则自动创建本地跟踪分支
// name 可以是 "main"（本地分支名）或 "origin/main"（远程分支名）
func SwitchBranch(projectDir string, name string) error {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开仓库失败: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作区失败: %w", err)
	}

	// 解析分支名：如果是 origin/xxx 格式，提取 xxx
	localName := name
	remoteName := "origin"
	if strings.HasPrefix(name, "origin/") {
		localName = name[len("origin/"):]
	}

	localRef := plumbing.NewBranchReferenceName(localName)
	_, err = repo.Reference(localRef, false)
	if err != nil {
		// 本地没有该分支，尝试从远程创建
		remoteRefName := plumbing.NewRemoteReferenceName(remoteName, localName)
		remoteRef, err := repo.Reference(remoteRefName, false)
		if err != nil {
			return fmt.Errorf("分支 %s 不存在（本地及远程）: %w", name, err)
		}

		// 创建本地分支引用指向远程的 commit
		newRef := plumbing.NewHashReference(localRef, remoteRef.Hash())
		if err := repo.Storer.SetReference(newRef); err != nil {
			return fmt.Errorf("创建本地分支引用失败: %w", err)
		}

		// 配置跟踪分支关系
		if err := repo.CreateBranch(&config.Branch{
			Name:   localName,
			Remote: remoteName,
			Merge:  localRef,
		}); err != nil {
			return fmt.Errorf("创建分支配置失败: %w", err)
		}
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: localRef,
		Force:  true,
	})
	if err != nil {
		return fmt.Errorf("切换分支失败: %w", err)
	}
	return nil
}

// CreateBranch 基于当前 HEAD 创建新分支
func CreateBranch(projectDir string, name string) error {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开仓库失败: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("获取 HEAD 失败: %w", err)
	}

	// 创建分支配置
	err = repo.CreateBranch(&config.Branch{
		Name:   name,
		Remote: "origin",
	})
	if err != nil {
		return fmt.Errorf("创建分支配置失败: %w", err)
	}

	// 创建引用指向当前 HEAD
	newRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), head.Hash())
	err = repo.Storer.SetReference(newRef)
	if err != nil {
		return fmt.Errorf("创建分支引用失败: %w", err)
	}

	return nil
}

// GetFullCommitGraph 获取全仓库的提交图（包含所有分支可达的 commit）
func GetFullCommitGraph(projectDir string, limit int) ([]CommitDetail, error) {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取 HEAD 失败: %w", err)
	}

	// 收集所有分支引用
	refsMap := make(map[string][]string) // hash -> [refName, ...]

	// HEAD
	refsMap[head.Hash().String()] = append(refsMap[head.Hash().String()], "HEAD")

	// 本地分支
	bIter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	defer bIter.Close()
	for {
		ref, err := bIter.Next()
		if err != nil {
			break
		}
		refsMap[ref.Hash().String()] = append(refsMap[ref.Hash().String()], ref.Name().Short())
	}

	// 远程分支
	rIter, err := repo.References()
	if err != nil {
		return nil, err
	}
	defer rIter.Close()
	for {
		ref, err := rIter.Next()
		if err != nil {
			break
		}
		if ref.Type() == plumbing.HashReference && ref.Name().IsRemote() {
			refsMap[ref.Hash().String()] = append(refsMap[ref.Hash().String()], ref.Name().Short())
		}
	}

	// BFS 遍历所有分支可达的 commit
	seen := make(map[string]bool)
	var queue []plumbing.Hash

	// 将所有分支 tip 加入队列
	for h := range refsMap {
		queue = append(queue, plumbing.NewHash(h))
	}

	var rawCommits []*object.Commit

	for len(queue) > 0 && len(rawCommits) < limit {
		cur := queue[0]
		queue = queue[1:]

		if seen[cur.String()] {
			continue
		}
		seen[cur.String()] = true

		c, err := repo.CommitObject(cur)
		if err != nil {
			continue
		}
		rawCommits = append(rawCommits, c)

		for _, p := range c.ParentHashes {
			queue = append(queue, p)
		}
	}

	// 按时间从新到旧排序（与 git log 一致）
	sort.Slice(rawCommits, func(i, j int) bool {
		return rawCommits[i].Committer.When.After(rawCommits[j].Committer.When)
	})

	var commits []CommitDetail
	for _, c := range rawCommits {
		parents := make([]string, len(c.ParentHashes))
		for j, h := range c.ParentHashes {
			parents[j] = h.String()
		}

		commits = append(commits, CommitDetail{
			SHA:     c.Hash.String(),
			Message: c.Message,
			Date:    c.Committer.When.Format(time.RFC3339),
			Author:  c.Author.Name,
			Parents: parents,
			IsHEAD:  c.Hash == head.Hash(),
			Refs:    refsMap[c.Hash.String()],
		})
	}

	return commits, nil
}
