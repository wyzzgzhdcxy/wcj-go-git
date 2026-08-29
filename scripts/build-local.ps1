#requires -Version 5.1
<#
.SYNOPSIS
    本地打包 Windows UI 程序 (Wails) 和 sync/ 下的命令行程序，统一输出到 build/bin/，并部署到工具箱目录。

.DESCRIPTION
    用法:
        .\scripts\build-local.ps1                 # 默认 release 构建，输出到 build/bin/ 并部署
        .\scripts\build-local.ps1 -Clean          # 构建前清理 build/bin/
        .\scripts\build-local.ps1 -SkipUI         # 只打包 sync/ 命令行程序
        .\scripts\build-local.ps1 -SkipCLI        # 只打包 Wails UI 程序
        .\scripts\build-local.ps1 -OutputDir dist  # 自定义输出目录
        .\scripts\build-local.ps1 -NoDeploy       # 只构建不部署

    行为:
        - 打包前若 sync.exe 进程正在运行，先强制停止（避免文件占用导致构建/清理失败）
        - 打包完成后把 Git同步工具.exe 和 sync.exe 一起部署到 -DeployDir（默认 E:\application\我的工具箱），
          部署前若对应 exe 正在运行也会先停止

    产出:
        build/bin/Git同步工具.exe   <- Wails UI
        build/bin/sync.exe           <- sync/ 命令行程序
#>

[CmdletBinding()]
param(
    [switch]$Clean,
    [switch]$SkipUI,
    [switch]$SkipCLI,
    [string]$OutputDir = "build\bin",
    [string]$DeployDir = "E:\application\我的工具箱",
    [switch]$NoDeploy,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Write-Step($t) { Write-Host "`n==> $t" -ForegroundColor Cyan }
function Write-Ok($t)   { Write-Host $t -ForegroundColor Green }
function Write-Warn($t) { Write-Host $t -ForegroundColor Yellow }

# 停止指定名称（不带 .exe 后缀）的进程，不存在时静默跳过
function Stop-ExeProcess([string]$name) {
    $procs = Get-Process -Name $name -ErrorAction SilentlyContinue
    if ($procs) {
        Write-Host "  停止进程: $name (PID: $(($procs.Id) -join ', '))" -ForegroundColor Yellow
        $procs | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 500
    }
}

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

# --- stop running sync.exe ----------------------------------------------------
# 正在运行的 sync.exe 会占用 build/bin 或部署目录中的 exe，打包前先停掉
if (-not $SkipCLI -and -not $DryRun) {
    Write-Step "检查并停止运行中的 sync.exe"
    Stop-ExeProcess $cliName
}

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
    $sameFile = $false
    try {
        $srcFull = (Get-Item -LiteralPath $src -ErrorAction Stop).FullName.TrimEnd('\')
        $dstFull = $dst.TrimEnd('\')
        if ([string]::Equals($srcFull, $dstFull, [System.StringComparison]::OrdinalIgnoreCase)) {
            $sameFile = $true
        }
    } catch {}

    if ($sameFile) {
        Write-Host "  (源与目标相同，跳过 copy): $dst"
    } elseif ($DryRun) {
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
            & go build -trimpath -ldflags "-s -w -H windowsgui" -o $cliOut .
            if ($LASTEXITCODE -ne 0) { throw "go build sync 失败 (exit=$LASTEXITCODE)" }
        } finally {
            Pop-Location
        }
        Write-Ok "  ✓ 已生成: $cliOut"
    }
} else {
    Write-Warn "[2/2] 已跳过 sync CLI"
}

# --- deploy ------------------------------------------------------------------
if ($NoDeploy) {
    Write-Warn "[3/3] 已跳过部署 (-NoDeploy)"
} else {
    Write-Step "[3/3] 部署程序到: $DeployDir"
    if (-not (Test-Path -LiteralPath $DeployDir)) {
        New-Item -ItemType Directory -Path $DeployDir -Force | Out-Null
    }

    # 把本次构建产出的 exe 都部署到目标目录
    $artifacts = @()
    if (-not $SkipUI)  { $artifacts += $uiName }
    if (-not $SkipCLI) { $artifacts += $cliName }

    foreach ($name in $artifacts) {
        $src = Join-Path $binDir "$name.exe"
        if (-not (Test-Path -LiteralPath $src)) {
            Write-Warn "  未找到 $src，跳过部署"
            continue
        }
        $dst = Join-Path $DeployDir "$name.exe"
        if ($DryRun) {
            Write-Host "  (DRYRUN) copy: $src -> $dst"
            continue
        }
        # 部署目录中的 exe 若正在运行会占用文件，先停掉再覆盖
        Stop-ExeProcess $name
        Copy-Item -LiteralPath $src -Destination $dst -Force
        Write-Ok "  ✓ 已部署: $dst"
    }
}

# --- summary -----------------------------------------------------------------
Write-Step "构建完成"
if (Test-Path $binDir) {
    Get-ChildItem -LiteralPath $binDir -File | Sort-Object Name |
        Select-Object Name, @{n='Size(MB)';e={[math]::Round($_.Length/1MB,2)}} |
        Format-Table -AutoSize | Out-String | Write-Host
}

Write-Host "最终输出目录: $binDir" -ForegroundColor Green
