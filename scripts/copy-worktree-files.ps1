param(
    [Parameter(Mandatory=$true)]
    [string]$TargetPath
)

$SourcePath = Split-Path -Parent $PSScriptRoot

# 检查目标路径是否存在
if (-not (Test-Path $TargetPath)) {
    Write-Host "错误: 目标路径不存在: $TargetPath" -ForegroundColor Red
    exit 1
}

# 读取 .worktreeinclude
$includeFile = Join-Path $SourcePath ".worktreeinclude"
if (-not (Test-Path $includeFile)) {
    Write-Host "错误: 找不到 .worktreeinclude 文件" -ForegroundColor Red
    exit 1
}

$patterns = Get-Content $includeFile | Where-Object { $_ -and $_ -notmatch '^\s*#' }

$copied = 0
$failed = 0

foreach ($pattern in $patterns) {
    $pattern = $pattern.Trim()
    if (-not $pattern) { continue }

    $src = Join-Path $SourcePath $pattern
    $dst = Join-Path $TargetPath $pattern

    if (Test-Path $src) {
        # 创建目标目录
        $dstDir = Split-Path $dst -Parent
        if (-not (Test-Path $dstDir)) {
            New-Item -ItemType Directory -Path $dstDir -Force | Out-Null
        }

        try {
            if (Test-Path -Path $src -PathType Container) {
                # 目录: 使用 robocopy 复制
                robocopy $src $dst /E /COPY:DAT /R:0 /W:0 /NFL /NDL /NJH /NJS > $null
                Write-Host "  [目录] $pattern" -ForegroundColor Green
            } else {
                # 文件: 直接复制
                Copy-Item $src $dst -Force
                Write-Host "  [文件] $pattern" -ForegroundColor Green
            }
            $copied++
        } catch {
            Write-Host "  [失败] $pattern : $_" -ForegroundColor Red
            $failed++
        }
    } else {
        Write-Host "  [跳过] $pattern (源不存在)" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "完成! 已复制 $copied 项, 失败 $failed 项" -ForegroundColor Cyan
