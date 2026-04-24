# 编译 launcher 启动器，输出到项目根目录
$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$launcherDir = Join-Path $projectRoot "launcher"
$embeddedDir = Join-Path $launcherDir "internal\env\embedded"

Write-Host "Copying rg.exe to embedded directory..."
New-Item -ItemType Directory -Path $embeddedDir -Force | Out-Null
Copy-Item (Join-Path $projectRoot "bin\rg.exe") (Join-Path $embeddedDir "rg.exe") -Force

Write-Host "Building launcher..."

Push-Location $launcherDir

# 设置国内 Go 代理，避免网络超时
$env:GOPROXY = "https://goproxy.cn,direct"

# 使用 wails 编译
wails build -platform windows/amd64 -o ../qingzhu-launcher.exe

if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed"
    Pop-Location
    exit 1
}

$src = Join-Path (Join-Path $launcherDir "build") "qingzhu-launcher.exe"
$dst = Join-Path $projectRoot "qingzhu-launcher.exe"

if (Test-Path $src) {
    Copy-Item $src $dst -Force
    Write-Host "Launcher built and copied to: $dst"
} else {
    Write-Error "Build output not found: $src"
    Pop-Location
    exit 1
}

Pop-Location
Write-Host "Done."
