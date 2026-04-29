package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"launcher/internal/gitman"
	"launcher/internal/gitservice"
	"launcher/internal/launcher"
	"launcher/internal/updater"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx         context.Context
	config      *updater.Config
	logBuffer   []string
	logMutex    sync.RWMutex
	cmdFrontend *os.Process
	cmdMutex    sync.Mutex
	gitServer   *gitservice.Server
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.Logf("[DEBUG] App startup 被调用")
}

// StartGitServer 启动Git HTTP服务
func (a *App) StartGitServer() error {
	if a.gitServer != nil {
		return nil
	}

	projectDir := a.getProjectDir()
	if projectDir == "" {
		return fmt.Errorf("项目目录未设置")
	}

	a.gitServer = gitservice.NewServer("")
	a.gitServer.SetProjectDir(projectDir)

	if err := a.gitServer.Start(); err != nil {
		a.gitServer = nil
		return fmt.Errorf("启动Git服务失败: %w", err)
	}

	a.Logf("Git服务已启动: %s", a.gitServer.GetAddress())
	return nil
}

// StopGitServer 停止Git HTTP服务
func (a *App) StopGitServer() error {
	if a.gitServer == nil {
		return nil
	}

	if err := a.gitServer.Stop(); err != nil {
		return fmt.Errorf("停止Git服务失败: %w", err)
	}

	a.gitServer = nil
	a.Logf("Git服务已停止")
	return nil
}

// GetGitServerAddress 获取Git服务地址
func (a *App) GetGitServerAddress() string {
	if a.gitServer == nil {
		return ""
	}
	return a.gitServer.GetAddress()
}

func (a *App) LoadConfig() (*updater.Config, error) {
	config, err := updater.LoadConfig()
	if err != nil {
		return nil, err
	}
	a.config = config
	return config, nil
}

// getProjectDir 获取项目目录（exe同级/qingzhu/）
func (a *App) getProjectDir() string {
	if a.config == nil {
		return ""
	}
	return updater.GetProjectDir(a.config)
}

// IsProjectDeployed 检查项目仓库是否存在（通过 .git 目录判断）
func (a *App) IsProjectDeployed() bool {
	projectDir := a.getProjectDir()
	if projectDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(projectDir, ".git"))
	return err == nil
}

func (a *App) GetVersion() string {
	if a.config == nil {
		return ""
	}
	projectDir := a.getProjectDir()
	local, err := updater.GetLocalCommit(projectDir)
	if err != nil {
		return "未安装"
	}
	if len(local.SHA) > 7 {
		return local.SHA[:7]
	}
	return local.SHA
}

func (a *App) CheckUpdate() (*updater.UpdateStatus, error) {
	if a.config == nil {
		return nil, fmt.Errorf("配置未加载")
	}
	return updater.CheckUpdateStatus(a.config, a)
}

func (a *App) PerformUpdate() error {
	if a.config == nil {
		return fmt.Errorf("配置未加载")
	}
	return updater.PullUpdates(a.config, a)
}

func (a *App) PrepareEnvironment() error {
	if a.config == nil {
		return fmt.Errorf("配置未加载")
	}

	projectDir := a.getProjectDir()
	return launcher.PrepareEnvironment(projectDir, a)
}

func (a *App) DownloadLaunch() error {
	if a.config == nil {
		return fmt.Errorf("配置未加载")
	}

	a.cmdMutex.Lock()
	defer a.cmdMutex.Unlock()
	if a.cmdFrontend != nil {
		return fmt.Errorf("主程序已在运行中")
	}

	projectDir := a.getProjectDir()

	go func() {
		result, err := launcher.DownloadLaunch(projectDir, a)
		if err != nil {
			a.Logf("启动失败: %v", err)
			a.emitMainProgramState(false)
			return
		}

		a.cmdMutex.Lock()
		a.cmdFrontend = result.FrontendCmd.Process
		a.cmdMutex.Unlock()

		a.emitMainProgramState(true)

		result.FrontendCmd.Wait()

		a.cmdMutex.Lock()
		a.cmdFrontend = nil
		a.cmdMutex.Unlock()
		a.emitMainProgramState(false)
	}()

	return nil
}

func (a *App) IsMainProgramRunning() bool {
	a.cmdMutex.Lock()
	defer a.cmdMutex.Unlock()
	return a.cmdFrontend != nil
}

func (a *App) emitMainProgramState(running bool) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "main-program-state", running)
	}
}

func (a *App) KillMainProgram() error {
	a.cmdMutex.Lock()
	defer a.cmdMutex.Unlock()

	if a.cmdFrontend != nil {
		a.cmdFrontend.Kill()
		a.cmdFrontend = nil
	}

	// 停止Git服务
	if a.gitServer != nil {
		a.gitServer.Stop()
		a.gitServer = nil
	}

	return nil
}

func (a *App) GetLogs() string {
	a.logMutex.RLock()
	defer a.logMutex.RUnlock()
	var result string
	for _, line := range a.logBuffer {
		result += line
	}
	return result
}

func (a *App) Logf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line += "\n"
	}

	a.logMutex.Lock()
	a.logBuffer = append(a.logBuffer, line)
	a.logMutex.Unlock()

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log", line)
	}
}

func (a *App) Progress(percent int) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "progress", percent)
	}
}

func (a *App) AutoCheckUpdate() {
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.Logf("=== %s 启动器 ===", a.config.App.Name)
		a.Logf("[DEBUG] AutoCheckUpdate: 开始检查更新")

		status, err := updater.CheckUpdateStatus(a.config, a)
		if err != nil {
			a.Logf("检查更新失败: %v", err)
			return
		}
		if status.HasUpdate {
			a.Logf("发现新提交: %s", status.RemoteCommit.SHA[:7])
			a.Logf("提交时间: %s", status.RemoteCommit.Date)
			a.Logf("提交信息:")
			for _, line := range strings.Split(status.RemoteCommit.Message, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					a.Logf("  %s", line)
				}
			}
		} else {
			a.Logf("当前已是最新提交")
		}
	}()
}

func (a *App) GitHistory(limit int) ([]gitman.CommitDetail, error) {
	projectDir := a.getProjectDir()
	return gitman.GetCommitHistory(projectDir, limit)
}

func (a *App) GitFullGraph(limit int) ([]gitman.CommitDetail, error) {
	projectDir := a.getProjectDir()
	return gitman.GetFullCommitGraph(projectDir, limit)
}

func (a *App) GitBranches() ([]gitman.BranchInfo, error) {
	projectDir := a.getProjectDir()
	return gitman.GetBranches(projectDir)
}

func (a *App) GitCheckout(hash string) error {
	projectDir := a.getProjectDir()
	return gitman.CheckoutCommit(projectDir, hash)
}

func (a *App) GitSwitchBranch(name string) error {
	projectDir := a.getProjectDir()
	return gitman.SwitchBranch(projectDir, name)
}

func (a *App) GitCreateBranch(name string) error {
	projectDir := a.getProjectDir()
	return gitman.CreateBranch(projectDir, name)
}

// OpenWebviewTab 打开一个 Webview 标签页，显示指定 URL
func (a *App) OpenWebviewTab(title string, url string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-webview-tab", map[string]string{
			"title": title,
			"url":   url,
		})
	}
}
