package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"launcher/internal/backend"
	"launcher/internal/env"
	"launcher/internal/frontend"
	"launcher/internal/migration"
	"launcher/internal/updater"
)

type Logger interface {
	Logf(format string, args ...interface{})
	Progress(percent int)
}

// LaunchResult 保存启动后的进程信息
type LaunchResult struct {
	FrontendCmd *exec.Cmd
}

// getEnvPaths 返回项目所需的 Python 和 Node.js 路径
func getEnvPaths(projectPath string, logger Logger) (pythonPath, nodePath string, err error) {
	if !filepath.IsAbs(projectPath) {
		absPath, err := filepath.Abs(projectPath)
		if err != nil {
			return "", "", fmt.Errorf("无法解析项目路径: %w", err)
		}
		projectPath = absPath
	}

	baseDir := projectPath

	logger.Logf("=== 检查 ripgrep ===")
	if err := updater.EnsureRipgrep(baseDir); err != nil {
		return "", "", fmt.Errorf("准备 rg.exe 失败: %w", err)
	}

	logger.Logf("=== 检查 Git ===")
	if err := updater.EnsureGit(baseDir, logger); err != nil {
		return "", "", fmt.Errorf("准备 Git 失败: %w", err)
	}

	logger.Logf("=== 检查 Python 环境 ===")
	pythonPath, ok := env.DetectVenvPython(baseDir)
	if !ok {
		logger.Logf("未找到虚拟环境，检测系统 Python...")
		check := env.CheckSystemPython()
		if check.Found && check.Ok {
			logger.Logf("系统 Python 满足要求: %s", check.Version)
			pythonPath, err = env.EnsureVenv(baseDir, "python", logger)
			if err != nil {
				return "", "", fmt.Errorf("创建虚拟环境失败: %w", err)
			}
		} else {
			logger.Logf("%s，开始准备项目专用 Python...", check.Message)
			portablePython, err := env.EnsurePython(baseDir, logger)
			if err != nil {
				return "", "", fmt.Errorf("准备项目专用 Python 失败: %w", err)
			}
			pythonPath, err = env.EnsureVenv(baseDir, portablePython, logger)
			if err != nil {
				return "", "", fmt.Errorf("创建虚拟环境失败: %w", err)
			}
		}
	}
	logger.Logf("使用 Python: %s", pythonPath)

	logger.Logf("=== 检查 Node.js 环境 ===")
	nodePath, ok = env.DetectNode(baseDir)
	if !ok {
		logger.Logf("未找到便携版 Node.js，开始下载...")
		if err := env.DownloadNode(baseDir, logger); err != nil {
			return "", "", fmt.Errorf("下载便携版 Node.js 失败: %w", err)
		}
		nodePath, ok = env.DetectNode(baseDir)
		if !ok {
			return "", "", fmt.Errorf("下载后仍未找到 Node.js")
		}
	}
	logger.Logf("使用 Node.js: %s", nodePath)

	if _, err := os.Stat(filepath.Join(projectPath, ".git")); os.IsNotExist(err) {
		return "", "", fmt.Errorf("项目仓库不存在: %s，请先更新下载项目", projectPath)
	}

	logger.Logf("=== 检查配置迁移 ===")
	if err := migration.RunAll(projectPath); err != nil {
		return "", "", fmt.Errorf("配置迁移失败: %w", err)
	}

	return pythonPath, nodePath, nil
}

// LaunchAll 完成环境准备并启动前端（Electron 自管后端）
// projectPath 是项目根目录（即 ai-novelist 目录，包含 .git、main.py、frontend/ 等）
func LaunchAll(projectPath string, logger Logger) (*LaunchResult, error) {
	pythonPath, nodePath, err := getEnvPaths(projectPath, logger)
	if err != nil {
		return nil, err
	}

	logger.Logf("=== 部署后端环境 ===")
	if err := backend.PipInstall(projectPath, pythonPath, logger); err != nil {
		return nil, err
	}
	logger.Logf("=== 后端依赖部署完成 ===")

	// 前端：npm install + 启动（Electron 自管后端）
	logger.Logf("=== 部署前端 ===")
	if err := frontend.NpmInstall(projectPath, nodePath, logger); err != nil {
		return nil, err
	}
	frontendCmd, err := frontend.Start(projectPath, nodePath, logger)
	if err != nil {
		return nil, err
	}

	logger.Logf("=== 启动完成 ===")
	return &LaunchResult{
		FrontendCmd: frontendCmd,
	}, nil
}
