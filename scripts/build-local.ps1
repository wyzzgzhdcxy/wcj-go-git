#requires -Version 5.1
<#
.SYNOPSIS
    本地打包 Windows UI 程序 (Wails) 和 sync/ 下的命令行程序，统一输出到 build/bin/。

.DESCRIPTION
    用法:
        .\scripts\build-local.ps1                 # 默认 release 构建，输出到 build/bin/
        .\scripts\build-local.ps1 -Clean          # 构建前清理 build/bin/
        .\scripts\build-local.ps1 -SkipUI         # 只打包 sync/ 命令行程序
        .\scripts\build-local.ps1 -SkipCLI        # 只打包 Wails UI 程序
        .\scripts\build-local.ps1 -OutputDir dist  # 自定义输出目录

    产出:
        build/bin/Git同步工具.exe   <- Wails UI
        build/bin/sync.exe           <- sync/ 命令行程序
        build/bin/README.txt         <- 版本/构建信息 (可选)
#>

[CmdletBinding()]
param(
    [switch]$Clean,
    [switch]$SkipUI,
    [switch]$SkipCLI,
    [string]$OutputDir = "build\bin",
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Write-Step($t) { Write-Host "`n==> $t" -ForegroundColor Cyan }
function Write-Ok($t)   { Write-Host $t -ForegroundColor Green }
function Write-Warn($t) { Write-Host $t -ForegroundColor Yellow }

$root   = (Resolve-Path ".").Path
$uiName = "Git同步工具"
$cliName = "sync"

# --- preflight ---------------------------------------------------------------
function Require-Command($cmd) {
    $ok = Get-Command $cmd -ErrorAction SilentlyContinue
    if (-not $ok) { throw "未找到命令: $cmd，请先安装并加入 PATH" }
}

Write-Step "检查构建依赖"
Require-Command go
if (-not $SkipUI) {
    Require-Command wails
}
Require-Command node
Require-Command npm

$goVer    = (& go version).Trim()
$wailsVer = if (-not $SkipUI) { (& wails version 2>$null | Select-Object -First 1).Trim() } else { "(skipped)" }
$nodeVer  = (& node --version).Trim()
Write-Host "  $goVer"
Write-Host "  wails: $wailsVer"
Write-Host "  $nodeVer"

# --- output dir --------------------------------------------------------------
$binDir = Join-Path $root $OutputDir
if ($Clean -and (Test-Path $binDir)) {
    Write-Step "清理输出目录"
    Remove-Item -LiteralPath $binDir -Recurse -Force
}
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}
Write-Host "输出目录: $binDir"

# --- Wails UI ----------------------------------------------------------------
if (-not $SkipUI) {
    Write-Step "[1/2] 打包 Wails UI 程序: $uiName"
    if (-not (Test-Path (Join-Path $root 'wails.json'))) {
        throw "未找到 wails.json，请在仓库根目录执行此脚本"
    }

    Push-Location $root
    try {
        & wails build -platform windows/amd64 -trimpath -clean
        if ($LASTEXITCODE -ne 0) { throw "wails build 失败 (exit=$LASTEXITCODE)" }
    } finally {
        Pop-Location
    }

    $src = Join-Path $root "build\bin\$uiName.exe"
    if (-not (Test-Path $src)) {
        # 兼容某些构建产物名差异
        $candidates = Get-ChildItem -LiteralPath (Join-Path $root "build\bin") -Filter "*.exe" -ErrorAction SilentlyContinue
        if ($candidates) {
            $src = $candidates[0].FullName
            Write-Warn "未找到预期的 '$uiName.exe'，改用: $($candidates[0].Name)"
        } else {
            throw "wails build 未产出 exe 文件"
        }
    }

    $dst = Join-Path $binDir "$uiName.exe"
    if ($DryRun) {
        Write-Host "  (DRYRUN) copy: $src -> $dst"
    } else {
        Copy-Item -LiteralPath $src -Destination $dst -Force
        Write-Ok "  ✓ 已生成: $dst"
    }
} else {
    Write-Warn "[1/2] 已跳过 Wails UI"
}

# --- sync CLI ---------------------------------------------------------------
if (-not $SkipCLI) {
    Write-Step "[2/2] 打包 sync/ 命令行程序: $cliName"
    $syncDir = Join-Path $root "sync"
    if (-not (Test-Path $syncDir)) { throw "未找到目录: $syncDir" }

    $cliOut = Join-Path $binDir "$cliName.exe"
    if ($DryRun) {
        Write-Host "  (DRYRUN) go build -> $cliOut"
    } else {
        Push-Location $syncDir
        try {
            $env:GOOS   = "windows"
            $env:GOARCH = "amd64"
            $env:CGO_ENABLED = "0"
            & go build -trimpath -ldflags "-s -w" -o $cliOut .
            if ($LASTEXITCODE -ne 0) { throw "go build sync 失败 (exit=$LASTEXITCODE)" }
        } finally {
            Pop-Location
        }
        Write-Ok "  ✓ 已生成: $cliOut"
    }
} else {
    Write-Warn "[2/2] 已跳过 sync CLI"
}

# --- summary -----------------------------------------------------------------
Write-Step "构建完成"
if (Test-Path $binDir) {
    Get-ChildItem -LiteralPath $binDir -File | Sort-Object Name |
        Select-Object Name, @{n='Size(MB)';e={[math]::Round($_.Length/1MB,2)}} |
        Format-Table -AutoSize | Out-String | Write-Host
}

Write-Host "最终输出目录: $binDir" -ForegroundColor Green
