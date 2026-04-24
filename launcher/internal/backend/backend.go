package backend

import (
	"bufio"
	"fmt"
	"launcher/internal/updater"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Logger interface {
	Logf(format string, args ...interface{})
}

func PipInstall(projectPath, pythonPath string, logger Logger) error {
	reqFile := filepath.Join(projectPath, "backend", "requirements.txt")
	if _, err := os.Stat(reqFile); os.IsNotExist(err) {
		return fmt.Errorf("requirements.txt 不存在: %s", reqFile)
	}

	logger.Logf("正在安装后端依赖（可能需要几分钟）...")
	installArgs := []string{"-m", "pip", "install", "-r", reqFile, "-i", updater.PipMirror}
	cmd := exec.Command(pythonPath, installArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pip install 启动失败: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Logf("[pip] %s", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logger.Logf("[pip ERR] %s", scanner.Text())
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pip install 失败: %w", err)
	}

	logger.Logf("后端依赖安装完成")
	return nil
}

func Start(projectPath, pythonPath string, logger Logger) (*exec.Cmd, error) {
	mainPy := filepath.Join(projectPath, "main.py")
	if _, err := os.Stat(mainPy); os.IsNotExist(err) {
		return nil, fmt.Errorf("main.py 不存在: %s", mainPy)
	}

	cmd := exec.Command(pythonPath, mainPy)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Dir = projectPath

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动后端失败: %w", err)
	}

	logger.Logf("后端启动成功 (PID: %d)", cmd.Process.Pid)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Logf("[Backend] %s", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logger.Logf("[Backend ERR] %s", scanner.Text())
		}
	}()

	return cmd, nil
}

func WaitForHealthy(port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(800 * time.Millisecond)
	}
	return fmt.Errorf("后端健康检查超时 (%ds)", int(timeout.Seconds()))
}
