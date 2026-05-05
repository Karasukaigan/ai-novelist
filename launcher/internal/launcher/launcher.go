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

// PrepareEnvironment 准备环境：检测 Python 版本、下载 Git/Node/rg 等外部工具链
// Python 下载后提醒用户手动安装，不自动安装
func PrepareEnvironment(projectPath string, logger Logger) error {
	if !filepath.IsAbs(projectPath) {
		absPath, err := filepath.Abs(projectPath)
		if err != nil {
			return fmt.Errorf("无法解析项目路径: %w", err)
		}
		projectPath = absPath
	}

	baseDir := projectPath

	logger.Logf("=== 检查 ripgrep ===")
	if err := updater.EnsureRipgrep(baseDir); err != nil {
		return fmt.Errorf("准备 rg.exe 失败: %w", err)
	}

	logger.Logf("=== 检查 Git ===")
	if err := updater.EnsureGit(baseDir, logger); err != nil {
		return fmt.Errorf("准备 Git 失败: %w", err)
	}

	logger.Logf("=== 检查 Node.js 环境 ===")
	nodePath, ok := env.DetectNode(baseDir)
	if !ok {
		logger.Logf("未找到便携版 Node.js，开始下载...")
		if err := env.DownloadNode(baseDir, logger); err != nil {
			return fmt.Errorf("下载便携版 Node.js 失败: %w", err)
		}
		nodePath, ok = env.DetectNode(baseDir)
		if !ok {
			return fmt.Errorf("下载后仍未找到 Node.js")
		}
	}
	logger.Logf("使用 Node.js: %s", nodePath)

	logger.Logf("=== 检查 Python 环境 ===")
	_, ok = env.DetectVenvPython(baseDir)
	if ok {
		logger.Logf("虚拟环境 Python 已存在")
	} else {
		check := env.CheckSystemPython()
		if check.Found && check.Ok {
			logger.Logf("系统 Python 满足要求: %s, 可以点击 [下载启动] 按钮", check.Version)
		} else {
			logger.Logf("%s", check.Message)
			logger.Logf("正在下载 Python 安装包，下载完成后请手动安装...")
			if err := env.DownloadPythonInstaller(baseDir, logger); err != nil {
				return fmt.Errorf("下载 Python 安装包失败: %w", err)
			}
			logger.Logf("Python 安装包已下载到启动器同级目录，并打开安装界面")
			logger.Logf("")
			logger.Logf("======== python安装说明 ========")
			logger.Logf("")
			logger.Logf("1. 请先勾选下方两项")
			logger.Logf("")
			logger.Logf("  [ √ ] Use admin privileges when installing py.exe")
			logger.Logf("  [ √ ] Add python.exe to PATH")
			logger.Logf("")
			logger.Logf("2. 然后点击 Install Now")
			logger.Logf("")
			logger.Logf("   \"是否允许该应用对此设备的更改\" 选择\"是\"")
			logger.Logf("")
			logger.Logf("================================")
			logger.Logf("")
			logger.Logf("手动安装后重新点击「准备环境」")
			logger.Logf("如果显示：")
			logger.Logf("     系统 Python 满足要求: 3.12.9（或其他 3.12.x 版本）, 可以点击 [下载启动] 按钮")
			logger.Logf("则说明安装成功，可以继续点击「下载启动」按钮")
		}
	}
	return nil
}

// DownloadLaunch 下载项目/更新 + pip install + npm install + 启动
func DownloadLaunch(projectPath string, logger Logger) (*LaunchResult, error) {
	if !filepath.IsAbs(projectPath) {
		absPath, err := filepath.Abs(projectPath)
		if err != nil {
			return nil, fmt.Errorf("无法解析项目路径: %w", err)
		}
		projectPath = absPath
	}

	baseDir := projectPath

	// 检查项目仓库是否存在
	if _, err := os.Stat(filepath.Join(projectPath, ".git")); os.IsNotExist(err) {
		return nil, fmt.Errorf("项目仓库不存在: %s，请先点击「检查更新」下载项目", projectPath)
	}

	logger.Logf("=== 检查配置迁移 ===")
	if err := migration.RunAll(projectPath); err != nil {
		return nil, fmt.Errorf("配置迁移失败: %w", err)
	}

	// 检测 Python
	pythonPath, ok := env.DetectVenvPython(baseDir)
	if !ok {
		logger.Logf("未找到虚拟环境，检测系统 Python...")
		check := env.CheckSystemPython()
		if check.Found && check.Ok {
			logger.Logf("系统 Python 满足要求: %s, 可以点击 [下载启动] 按钮", check.Version)
			// 使用 findSystemPython 获取真实路径，绕过 Windows App Execution Alias
			realPythonPath, err := env.FindSystemPython()
			if err != nil {
				return nil, fmt.Errorf("获取系统 Python 真实路径失败: %w", err)
			}
			logger.Logf("使用系统 Python: %s", realPythonPath)
			pythonPath, err = env.EnsureVenv(baseDir, realPythonPath, logger)
			if err != nil {
				return nil, fmt.Errorf("创建虚拟环境失败: %w", err)
			}
		} else {
			return nil, fmt.Errorf("Python 环境未就绪，请先点击「准备环境」")
		}
	}
	logger.Logf("使用 Python: %s", pythonPath)

	// 检测 Node.js
	nodePath, ok := env.DetectNode(baseDir)
	if !ok {
		return nil, fmt.Errorf("Node.js 环境未就绪，请先点击「准备环境」")
	}
	logger.Logf("使用 Node.js: %s", nodePath)

	logger.Logf("=== 部署后端环境 ===")
	if err := backend.PipInstall(projectPath, pythonPath, logger); err != nil {
		return nil, err
	}
	logger.Logf("=== 复制 VC++ 运行时 DLL ===")
	if err := updater.EnsureVcRedist(projectPath); err != nil {
		logger.Logf("复制 VC++ 运行时 DLL 失败（非致命）: %v", err)
	} else {
		logger.Logf("VC++ 运行时 DLL 已就绪")
	}
	logger.Logf("=== 后端依赖部署完成 ===")

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
