package env

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

type Logger interface {
	Logf(format string, args ...interface{})
	Progress(percent int)
}

func getBinDir(baseDir string) string {
	return filepath.Join(baseDir, "bin")
}

// DetectVenvPython 检测 .venv 中的 Python
func DetectVenvPython(baseDir string) (string, bool) {
	p := filepath.Join(baseDir, ".venv", "Scripts", "python.exe")
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// DetectNode 检测便携版 Node.js
func DetectNode(baseDir string) (string, bool) {
	p := filepath.Join(getBinDir(baseDir), "node", "node.exe")
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// findSystemPython 查找系统中真实安装的 Python 路径
// 绕过 Windows App Execution Alias（WindowsApps 下的假 python.exe）
func findSystemPython() (string, error) {
	// 1. 尝试 exec.LookPath，如果找到的路径不在 WindowsApps 下则直接使用
	if pyPath, err := exec.LookPath("python"); err == nil {
		if !isWindowsAppsPath(pyPath) {
			if _, err := exec.Command(pyPath, "--version").Output(); err == nil {
				return pyPath, nil
			}
		}
	}
	if pyPath, err := exec.LookPath("python3"); err == nil {
		if !isWindowsAppsPath(pyPath) {
			if _, err := exec.Command(pyPath, "--version").Output(); err == nil {
				return pyPath, nil
			}
		}
	}

	// 2. 查注册表 HKLM\SOFTWARE\Python\PythonCore\{version}\InstallPath
	//    以及 HKCU\SOFTWARE\Python\PythonCore\{version}\InstallPath
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		baseKey := `SOFTWARE\Python\PythonCore`
		k, err := registry.OpenKey(root, baseKey, registry.READ)
		if err != nil {
			continue
		}
		subKeys, err := k.ReadSubKeyNames(0)
		k.Close()
		if err != nil {
			continue
		}
		for _, ver := range subKeys {
			installKey := baseKey + `\` + ver + `\InstallPath`
			ik, err := registry.OpenKey(root, installKey, registry.READ)
			if err != nil {
				continue
			}
			installPath, _, err := ik.GetStringValue("")
			ik.Close()
			if err != nil || installPath == "" {
				continue
			}
			pyPath := filepath.Join(installPath, "python.exe")
			if _, err := os.Stat(pyPath); err != nil {
				continue
			}
			if _, err := exec.Command(pyPath, "--version").Output(); err == nil {
				return pyPath, nil
			}
		}
	}

	return "", fmt.Errorf("未找到系统 Python")
}

// isWindowsAppsPath 判断路径是否在 Windows App Execution Alias 目录下
func isWindowsAppsPath(path string) bool {
	return strings.Contains(strings.ToLower(path), `microsoft\windowsapps`)
}

// DownloadPythonInstaller 下载 Python 安装包到启动器同级目录，不自动安装
// 下载完成后提醒用户手动安装
func DownloadPythonInstaller(baseDir string, logger Logger) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取启动器路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// 检查是否已有安装包
	entries, err := os.ReadDir(exeDir)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "python-") && strings.HasSuffix(name, ".exe") {
			logger.Logf("Python 安装包已存在: %s", entry.Name())
			logger.Logf("请手动运行该安装包进行安装，安装完成后重新点击「准备环境」")
			return nil
		}
	}

	installerName := "python-3.12.9-amd64.exe"
	installerPath := filepath.Join(exeDir, installerName)
	url := "https://mirrors.aliyun.com/python-release/windows/" + installerName
	logger.Logf("正在从镜像下载 %s ...", installerName)
	if err := downloadFile(url, installerPath, logger); err != nil {
		return fmt.Errorf("下载 Python 安装包失败: %w", err)
	}
	logger.Logf("Python 安装包下载完成: %s", installerPath)
	logger.Logf("请手动运行该安装包进行安装，安装完成后重新点击「准备环境」")
	return nil
}

// EnsureVenv 确保 .venv 虚拟环境已创建
func EnsureVenv(baseDir string, pythonExe string, logger Logger) (string, error) {
	venvPython := filepath.Join(baseDir, ".venv", "Scripts", "python.exe")
	if _, err := os.Stat(venvPython); err == nil {
		logger.Logf("虚拟环境已存在: %s", venvPython)
		return venvPython, nil
	}

	logger.Logf("正在创建虚拟环境 .venv ...")
	cmd := exec.Command(pythonExe, "-m", "venv", filepath.Join(baseDir, ".venv"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("创建虚拟环境失败: %w\n%s", err, string(out))
	}

	logger.Logf("虚拟环境创建完成: %s", venvPython)
	return venvPython, nil
}

func DownloadNode(baseDir string, logger Logger) error {
	binDir := getBinDir(baseDir)
	os.MkdirAll(binDir, os.ModePerm)

	nodeDir := filepath.Join(binDir, "node")

	url := "https://npmmirror.com/mirrors/node/v24.15.0/node-v24.15.0-win-x64.zip"
	zipPath := filepath.Join(binDir, "node.zip")

	logger.Logf("正在下载 Node.js 24.15.0 ...")
	if err := downloadFile(url, zipPath, logger); err != nil {
		return fmt.Errorf("下载 Node 失败: %w", err)
	}

	logger.Logf("正在解压 Node.js ...")
	tmpDir := filepath.Join(binDir, "node_tmp")
	os.RemoveAll(tmpDir)
	os.MkdirAll(tmpDir, os.ModePerm)
	if err := unzip(zipPath, tmpDir); err != nil {
		return fmt.Errorf("解压 Node 失败: %w", err)
	}
	os.Remove(zipPath)

	// 移动内部目录到 node/
	entries, _ := os.ReadDir(tmpDir)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "node-") {
			src := filepath.Join(tmpDir, entry.Name())
			os.RemoveAll(nodeDir)
			if err := os.Rename(src, nodeDir); err != nil {
				return fmt.Errorf("移动 Node 目录失败: %w", err)
			}
			break
		}
	}
	os.RemoveAll(tmpDir)

	logger.Logf("Node.js 安装完成")
	return nil
}

// PythonVersionCheck 系统 Python 版本检测结果
type PythonVersionCheck struct {
	Found   bool   `json:"found"`
	Version string `json:"version"`
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
}

// CheckSystemPython 检测系统 Python 版本是否 >= 3.12.0
// 绕过 Windows App Execution Alias，通过注册表查找真实安装路径
func CheckSystemPython() PythonVersionCheck {
	pyPath, err := findSystemPython()
	if err != nil {
		return PythonVersionCheck{
			Found:   false,
			Version: "",
			Ok:      false,
			Message: "未检测到系统 Python，请先安装 Python 3.12 或更高版本",
		}
	}

	// 获取版本
	cmd := exec.Command(pyPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return PythonVersionCheck{
			Found:   true,
			Version: "",
			Ok:      false,
			Message: "无法获取 Python 版本信息",
		}
	}

	verStr := strings.TrimSpace(string(out))
	version := parsePythonVersion(verStr)
	if version == "" {
		return PythonVersionCheck{
			Found:   true,
			Version: verStr,
			Ok:      false,
			Message: "无法解析 Python 版本: " + verStr,
		}
	}

	// 比较版本是否 >= 3.12.0
	ok := !versionLessThan(version, "3.12.0")
	msg := fmt.Sprintf("当前 Python 版本: %s", version)
	if !ok {
		msg = fmt.Sprintf("当前 Python 版本 %s 过低，建议安装 3.12 或更高版本以避免兼容性问题", version)
	}

	return PythonVersionCheck{
		Found:   true,
		Version: version,
		Ok:      ok,
		Message: msg,
	}
}

// parsePythonVersion 从 "Python 3.13.9" 中提取 "3.13.9"
func parsePythonVersion(output string) string {
	re := regexp.MustCompile(`Python\s+([\d.]+)`)
	m := re.FindStringSubmatch(output)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// versionLessThan 比较两个版本号字符串 a < b
func versionLessThan(a, b string) bool {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, _ := strconv.Atoi(pa[i])
		nb, _ := strconv.Atoi(pb[i])
		if na < nb {
			return true
		}
		if na > nb {
			return false
		}
	}
	return len(pa) < len(pb)
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

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(dest, os.ModePerm)

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
