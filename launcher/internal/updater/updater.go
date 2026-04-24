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
		Require3_13_9 bool `yaml:"require_3_13_9"`
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
	if logger != nil {
		logger.Logf("[DEBUG] CheckUpdateStatus: projectDir=%s", projectDir)
	}
	repo, err := git.PlainOpen(projectDir)
	currentBranch := "main"
	if err == nil {
		head, _ := repo.Head()
		if head != nil && head.Name().IsBranch() {
			currentBranch = head.Name().Short()
		}
	}
	if logger != nil {
		logger.Logf("[DEBUG] CheckUpdateStatus: currentBranch=%s", currentBranch)
	}

	remote, err := GetRemoteLatestCommit(cfg.Git.RemoteURL, currentBranch, logger)
	if err != nil {
		if logger != nil {
			logger.Logf("[DEBUG] CheckUpdateStatus: GetRemoteLatestCommit 失败: %v", err)
		}
		return nil, err
	}
	local, _ := GetLocalCommit(projectDir)
	if logger != nil {
		if local != nil {
			logger.Logf("[DEBUG] CheckUpdateStatus: local=%s, remote=%s", local.SHA[:7], remote.SHA[:7])
		} else {
			logger.Logf("[DEBUG] CheckUpdateStatus: local=nil, remote=%s", remote.SHA[:7])
		}
	}

	status := &UpdateStatus{
		HasUpdate:    true,
		RemoteCommit: *remote,
		LocalCommit:  local,
	}
	if local != nil && local.SHA == remote.SHA {
		status.HasUpdate = false
	}
	if logger != nil {
		logger.Logf("[DEBUG] CheckUpdateStatus: HasUpdate=%v", status.HasUpdate)
	}
	return status, nil
}

func createTrackingBranches(repo *git.Repository, logger Logger) error {
	refs, err := repo.References()
	if err != nil {
		return err
	}
	defer refs.Close()

	for {
		ref, err := refs.Next()
		if err != nil {
			break
		}
		if ref.Type() != plumbing.HashReference || !ref.Name().IsRemote() {
			continue
		}

		remoteBranch := ref.Name().Short()
		parts := strings.SplitN(remoteBranch, "/", 2)
		if len(parts) < 2 {
			continue
		}

		localName := parts[1]
		if localName == "HEAD" {
			continue
		}

		localRef := plumbing.NewBranchReferenceName(localName)
		_, err = repo.Reference(localRef, false)
		if err == nil {
			continue
		}

		newRef := plumbing.NewHashReference(localRef, ref.Hash())
		if err := repo.Storer.SetReference(newRef); err != nil {
			logger.Logf("创建本地分支 %s 失败: %v", localName, err)
			continue
		}

		if err := repo.CreateBranch(&config.Branch{
			Name:   localName,
			Remote: parts[0],
			Merge:  localRef,
		}); err != nil {
			logger.Logf("配置分支 %s 跟踪失败: %v", localName, err)
		}

		logger.Logf("创建本地分支: %s", localName)
	}
	logger.Logf("本地跟踪分支创建完成")
	return nil
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
		Progress:   &logWriter{logger: logger},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("获取远程更新失败: %w", err)
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
	if err := createTrackingBranches(repo, logger); err != nil {
		logger.Logf("同步跟踪分支失败: %v", err)
	}

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
	return nil
}

type logWriter struct {
	logger Logger
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.logger.Logf("%s", strings.TrimSpace(string(p)))
	return len(p), nil
}
