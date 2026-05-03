package updater

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"gopkg.in/yaml.v3"
)

const ConfigFile = "config.yaml"

type Config struct {
	App struct {
		Name string `yaml:"name"`
	} `yaml:"app"`
	Python struct {
		Require3_12 bool `yaml:"require_3_12"`
	} `yaml:"python"`
	Git struct {
		RemoteURL  string `yaml:"remote_url"`
		ProjectDir string `yaml:"project_dir"`
	} `yaml:"git"`
}

const PipMirror = "https://mirrors.aliyun.com/pypi/simple/"
const NpmMirror = "https://registry.npmmirror.com/"

type CommitInfo struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Date    string `json:"date"`
}

type UpdateStatus struct {
	HasUpdate    bool        `json:"has_update"`
	RemoteCommit CommitInfo  `json:"remote_commit"`
	LocalCommit  *CommitInfo `json:"local_commit,omitempty"`
}

type Logger interface {
	Logf(format string, args ...interface{})
	Progress(percent int)
}

// getExeDir 获取启动器exe所在目录
func getExeDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

// GetProjectDir 获取项目目录（exe同级/projectDir），目录不存在时自动创建
func GetProjectDir(cfg *Config) string {
	dir := cfg.Git.ProjectDir
	if dir == "" {
		dir = "qingzhu"
	}
	projectDir := filepath.Join(getExeDir(), dir)
	os.MkdirAll(projectDir, 0755)
	return projectDir
}

func configPath() string {
	return filepath.Join(getExeDir(), ConfigFile)
}

// EnsureRipgrep 将启动器同级目录的 rg.exe 复制到项目 bin 目录
func EnsureRipgrep(projectDir string) error {
	exeDir := getExeDir()
	src := filepath.Join(exeDir, "rg.exe")
	dstDir := filepath.Join(projectDir, "bin")
	dst := filepath.Join(dstDir, "rg.exe")

	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("未在启动器同级目录找到 rg.exe: %s", src)
	}

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("创建 bin 目录失败: %w", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取 rg.exe 失败: %w", err)
	}

	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("复制 rg.exe 失败: %w", err)
	}

	return nil
}

// EnsureGit 检查启动器同级目录是否有 PortableGit 安装包，下载后解压到 qingzhu/bin/git/ 下
func EnsureGit(projectDir string, logger Logger) error {
	exeDir := getExeDir()
	dstDir := filepath.Join(projectDir, "bin", "git")
	gitExe := filepath.Join(dstDir, "bin", "git.exe")

	// 已存在则跳过
	if _, err := os.Stat(gitExe); err == nil {
		return nil
	}

	// 检查启动器同级目录是否有安装包
	installerName := "PortableGit-2.54.0-64-bit.7z.exe"
	installerPath := filepath.Join(exeDir, installerName)

	if _, err := os.Stat(installerPath); os.IsNotExist(err) {
		// 下载
		url := "https://registry.npmmirror.com/-/binary/git-for-windows/v2.54.0.windows.1/PortableGit-2.54.0-64-bit.7z.exe"
		if logger != nil {
			logger.Logf("正在下载 Git 便携包 ...")
		}
		if err := downloadFile(url, installerPath, logger); err != nil {
			return fmt.Errorf("下载 Git 便携包失败: %w", err)
		}
	}

	// 解压到目标目录
	if logger != nil {
		logger.Logf("正在解压 Git ...")
	}
	os.MkdirAll(dstDir, 0755)

	// PortableGit-2.54.0-64-bit.7z.exe 是自解压 7z 文件，使用 /S 静默解压到指定目录
	cmd := exec.Command(installerPath, fmt.Sprintf("-o%s", dstDir), "-y")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("解压 Git 便携包失败: %w", err)
	}

	if _, err := os.Stat(gitExe); os.IsNotExist(err) {
		return fmt.Errorf("解压后未找到 git.exe")
	}

	if logger != nil {
		logger.Logf("Git 安装完成: %s", gitExe)
	}
	return nil
}

func downloadFile(url, dest string, logger Logger) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Referer", "https://mirrors.tuna.tsinghua.edu.cn/")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, werr := out.Write(buf[:n])
			if werr != nil {
				return werr
			}
			written += int64(n)
			if logger != nil && total > 0 {
				pct := int(float64(written) / float64(total) * 100)
				logger.Progress(pct)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0644)
}

func parseGiteeRepo(remoteURL string) (owner, repo string, err error) {
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	if strings.Contains(remoteURL, "gitee.com") {
		parts := strings.Split(remoteURL, "/")
		if len(parts) >= 2 {
			owner = parts[len(parts)-2]
			repo = parts[len(parts)-1]
			return owner, repo, nil
		}
	}
	return "", "", fmt.Errorf("无法解析 gitee 仓库地址: %s", remoteURL)
}

func GetRemoteLatestCommit(remoteURL, branch string, logger Logger) (*CommitInfo, error) {
	if logger != nil {
		logger.Logf("[DEBUG] 开始获取远程提交: remoteURL=%s, branch=%s", remoteURL, branch)
	}
	owner, repo, err := parseGiteeRepo(remoteURL)
	if err != nil {
		if logger != nil {
			logger.Logf("[DEBUG] 解析仓库地址失败: %v", err)
		}
		return nil, err
	}
	if logger != nil {
		logger.Logf("[DEBUG] 解析仓库: owner=%s, repo=%s", owner, repo)
	}
	apiURL := fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s/commits?sha=%s&per_page=1", owner, repo, branch)
	if logger != nil {
		logger.Logf("[DEBUG] API URL: %s", apiURL)
		logger.Logf("[DEBUG] 开始创建 HTTP Client, timeout=30s")
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if logger != nil {
				logger.Logf("[DEBUG] [网络] 开始 TCP 连接: %s", addr)
			}
			start := time.Now()
			conn, err := dialer.DialContext(ctx, network, addr)
			if logger != nil {
				logger.Logf("[DEBUG] [网络] TCP 连接 %s 耗时: %v, err=%v", addr, time.Since(start), err)
			}
			return conn, err
		},
		TLSHandshakeTimeout: 30 * time.Second,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if logger != nil {
				logger.Logf("[DEBUG] [网络] 开始 TLS 握手: %s", addr)
			}
			start := time.Now()
			conn, err := tls.DialWithDialer(dialer, network, addr, &tls.Config{})
			if logger != nil {
				logger.Logf("[DEBUG] [网络] TLS 握手 %s 耗时: %v, err=%v", addr, time.Since(start), err)
			}
			return conn, err
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
	if logger != nil {
		logger.Logf("[DEBUG] HTTP Client 创建完成, 开始发送 GET 请求...")
	}
	start := time.Now()
	resp, err := client.Get(apiURL)
	elapsed := time.Since(start)
	if logger != nil {
		logger.Logf("[DEBUG] 请求总耗时: %v, err=%v", elapsed, err)
	}
	if err != nil {
		return nil, fmt.Errorf("请求 Gitee API 失败: %w", err)
	}
	defer resp.Body.Close()
	if logger != nil {
		logger.Logf("[DEBUG] 收到响应: status=%d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if logger != nil {
			logger.Logf("[DEBUG] 响应错误: body=%s", string(body))
		}
		return nil, fmt.Errorf("Gitee API 返回错误 %d: %s", resp.StatusCode, string(body))
	}

	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		if logger != nil {
			logger.Logf("[DEBUG] 解析响应失败: %v", err)
		}
		return nil, fmt.Errorf("解析 Gitee API 响应失败: %w", err)
	}
	if logger != nil {
		logger.Logf("[DEBUG] 解析成功, commits数量=%d", len(commits))
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("远程仓库没有提交记录")
	}
	c := commits[0]
	if logger != nil {
		logger.Logf("[DEBUG] 获取远程提交成功: SHA=%s", c.SHA[:7])
	}
	return &CommitInfo{
		SHA:     c.SHA,
		Message: strings.TrimSpace(c.Commit.Message),
		Date:    c.Commit.Author.Date,
	}, nil
}

func GetLocalCommit(projectDir string) (*CommitInfo, error) {
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	return &CommitInfo{
		SHA:     head.Hash().String(),
		Message: strings.TrimSpace(commit.Message),
		Date:    commit.Committer.When.Format(time.RFC3339),
	}, nil
}

func CheckUpdateStatus(cfg *Config, logger Logger) (*UpdateStatus, error) {
	projectDir := GetProjectDir(cfg)
	repo, err := git.PlainOpen(projectDir)
	currentBranch := "main"
	if err == nil {
		head, _ := repo.Head()
		if head != nil && head.Name().IsBranch() {
			currentBranch = head.Name().Short()
		}
	}

	remote, err := GetRemoteLatestCommit(cfg.Git.RemoteURL, currentBranch, logger)
	if err != nil {
		return nil, err
	}
	local, _ := GetLocalCommit(projectDir)

	status := &UpdateStatus{
		HasUpdate:    true,
		RemoteCommit: *remote,
		LocalCommit:  local,
	}
	if local != nil && local.SHA == remote.SHA {
		status.HasUpdate = false
	}

	if logger != nil {
		if status.HasUpdate {
			logger.Logf("发现新提交: %s", remote.SHA[:7])
			logger.Logf("提交时间: %s", remote.Date)
			logger.Logf("提交信息:")
			for _, line := range strings.Split(remote.Message, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					logger.Logf("  %s", line)
				}
			}
		} else {
			logger.Logf("当前已是最新提交: %s", remote.SHA[:7])
		}
	}
	return status, nil
}

// syncBranches 用已打开的 repo 同步远程分支：
//   - 远程新分支 → 自动创建本地跟踪分支
//   - 远程已删除的分支 → 删除对应的本地分支（当前分支除外）
func syncBranches(repo *git.Repository, logger Logger) {
	head, err := repo.Head()
	if err != nil {
		logger.Logf("[syncBranches] 获取 HEAD 失败: %v", err)
		return
	}
	currentBranch := head.Name().Short()
	logger.Logf("[syncBranches] 当前分支: %s", currentBranch)

	// 收集所有远程分支名（去掉 origin/ 前缀）
	remoteBranches := make(map[string]plumbing.Hash)
	rIter, err := repo.References()
	if err != nil {
		logger.Logf("[syncBranches] 遍历引用失败: %v", err)
		return
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
		remoteShort := ref.Name().Short()
		localName := remoteShort
		if idx := strings.Index(remoteShort, "/"); idx >= 0 {
			localName = remoteShort[idx+1:]
		}
		logger.Logf("[syncBranches] 发现远程跟踪引用: %s -> 本地分支名: %s hash: %s", remoteShort, localName, ref.Hash().String()[:7])
		remoteBranches[localName] = ref.Hash()
	}
	logger.Logf("[syncBranches] 共发现 %d 个远程跟踪引用", len(remoteBranches))

	// 遍历本地分支，做双向同步
	bIter, err := repo.Branches()
	if err != nil {
		logger.Logf("[syncBranches] 遍历分支失败: %v", err)
		return
	}
	defer bIter.Close()

	for {
		ref, err := bIter.Next()
		if err != nil {
			break
		}
		localName := ref.Name().Short()
		localHash := ref.Hash().String()[:7]

		remoteHash, existsOnRemote := remoteBranches[localName]
		if existsOnRemote {
			remoteHashShort := remoteHash.String()[:7]
			// 远程存在且本地指向不同 commit → 更新本地指向
			if ref.Hash() != remoteHash {
				logger.Logf("[syncBranches] 更新本地分支 %s: %s -> %s", localName, localHash, remoteHashShort)
				localRef := plumbing.NewBranchReferenceName(localName)
				newRef := plumbing.NewHashReference(localRef, remoteHash)
				if err := repo.Storer.SetReference(newRef); err != nil {
					logger.Logf("[syncBranches] 更新分支 %s 失败: %v", localName, err)
				}
			} else {
				logger.Logf("[syncBranches] 本地分支 %s 已是最新: %s", localName, localHash)
			}
			delete(remoteBranches, localName)
		} else {
			// 远程已删除该分支，删除本地分支（当前分支除外）
			if localName != currentBranch {
				logger.Logf("[syncBranches] 删除本地分支: %s (%s) - 远程已删除", localName, localHash)
				localRef := plumbing.NewBranchReferenceName(localName)
				err1 := repo.Storer.RemoveReference(localRef)
				err2 := repo.DeleteBranch(localName)
				if err1 != nil {
					logger.Logf("[syncBranches] 删除引用 %s 失败: %v", localName, err1)
				}
				if err2 != nil {
					logger.Logf("[syncBranches] 删除分支 %s 失败: %v", localName, err2)
				}
			} else {
				logger.Logf("[syncBranches] 跳过当前分支 %s（远程已删除但保留本地）", localName)
			}
		}
	}

	// 剩余在 remoteBranches 中的是远程有但本地没有的分支 → 创建本地跟踪分支
	logger.Logf("[syncBranches] 剩余 %d 个远程分支需要创建本地跟踪", len(remoteBranches))
	for localName, hash := range remoteBranches {
		logger.Logf("[syncBranches] 创建本地跟踪分支: %s -> %s", localName, hash.String()[:7])
		localRef := plumbing.NewBranchReferenceName(localName)
		newRef := plumbing.NewHashReference(localRef, hash)
		if err := repo.Storer.SetReference(newRef); err != nil {
			logger.Logf("[syncBranches] 创建分支引用 %s 失败: %v", localName, err)
			continue
		}
		if err := repo.CreateBranch(&config.Branch{
			Name:   localName,
			Remote: "origin",
			Merge:  localRef,
		}); err != nil {
			logger.Logf("[syncBranches] 配置跟踪分支 %s 失败: %v", localName, err)
		}
	}
	logger.Logf("[syncBranches] 分支同步完成")
}

// SyncBranchesFromRemot
func SyncBranchesFromRemote(cfg *Config, logger Logger) {
	projectDir := GetProjectDir(cfg)
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); os.IsNotExist(err) {
		logger.Logf("项目未部署，跳过分支同步")
		return
	}
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		logger.Logf("打开仓库失败，跳过分支同步: %v", err)
		return
	}
	w, err := repo.Worktree()
	if err != nil {
		logger.Logf("获取工作区失败: %v", err)
		return
	}
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Prune:      true,
		Progress:   &logWriter{logger: logger},
	})
	if err == git.NoErrAlreadyUpToDate {
		logger.Logf("远程已是最新")
	} else if err != nil {
		logger.Logf("fetch 远程失败: %v", err)
		return
	}
	// 强行 hard reset 当前分支到远程最新
	head, err := repo.Head()
	if err == nil && head.Name().IsBranch() {
		branchName := head.Name().Short()
		refName := plumbing.NewRemoteReferenceName("origin", branchName)
		ref, err := repo.Reference(refName, true)
		if err == nil {
			_ = w.Reset(&git.ResetOptions{
				Mode:   git.HardReset,
				Commit: ref.Hash(),
			})
			logger.Logf("强制重置 %s 到远程最新: %s", branchName, ref.Hash().String()[:7])
		}
	}
	syncBranches(repo, logger)
}

// PullUpdates 根据项目目录是否存在，执行克隆或拉取更新
func PullUpdates(cfg *Config, logger Logger) error {
	projectDir := GetProjectDir(cfg)

	// 检查项目仓库是否存在（通过 .git 目录判断）
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); os.IsNotExist(err) {
		logger.Logf("项目未部署，开始克隆到 %s ...", projectDir)
		return cloneProject(cfg, logger)
	}

	logger.Logf("开始拉取更新...")
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		return fmt.Errorf("打开本地仓库失败: %w", err)
	}
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作区失败: %w", err)
	}
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Prune:      true,
		Progress:   &logWriter{logger: logger},
	})
	if err == git.NoErrAlreadyUpToDate {
		logger.Logf("远程已是最新（NoErrAlreadyUpToDate），继续同步分支")
	} else if err != nil {
		return fmt.Errorf("获取远程更新失败: %w", err)
	} else {
		logger.Logf("远程 fetch 成功（Prune=true），已清理已删除分支的远程跟踪引用")
	}
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("获取 HEAD 失败: %w", err)
	}
	if head.Name().IsBranch() {
		branchName := head.Name().Short()
		refName := plumbing.NewRemoteReferenceName("origin", branchName)
		ref, err := repo.Reference(refName, true)
		if err != nil {
			return fmt.Errorf("获取远程分支引用失败: %w", err)
		}
		err = w.Reset(&git.ResetOptions{
			Mode:   git.HardReset,
			Commit: ref.Hash(),
		})
		if err != nil {
			return fmt.Errorf("重置到最新提交失败: %w", err)
		}
	}
	logger.Logf("正在同步本地跟踪分支...")
	syncBranches(repo, logger)

	logger.Logf("更新完成")
	if err := EnsureRipgrep(projectDir); err != nil {
		return fmt.Errorf("复制 rg.exe 失败: %w", err)
	}
	return nil
}

func cloneProject(cfg *Config, logger Logger) error {
	projectDir := GetProjectDir(cfg)

	_, err := git.PlainClone(projectDir, &git.CloneOptions{
		URL:      cfg.Git.RemoteURL,
		Progress: &logWriter{logger: logger},
	})
	if err != nil {
		return fmt.Errorf("克隆项目失败: %w", err)
	}

	logger.Logf("项目克隆完成")

	// 克隆后打开仓库，同步远程分支
	repo, err := git.PlainOpen(projectDir)
	if err != nil {
		logger.Logf("打开仓库失败: %v", err)
	} else {
		logger.Logf("正在同步远程分支...")
		syncBranches(repo, logger)
	}

	logger.Logf("接下来点击「准备环境」按钮，将会检测系统环境，下载需要的安装包/便携包")
	return nil
}

type logWriter struct {
	logger Logger
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.logger.Logf("%s", strings.TrimSpace(string(p)))
	return len(p), nil
}
