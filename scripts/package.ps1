# package.ps1 builds and packages hmba-mail into $HOME\.hmba-mail (or custom target)
param(
    [string]$TargetDir = (Join-Path $HOME ".hmba-mail"),
    [string]$Version = "0.1.0"
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$distDir = Join-Path $repoRoot "dist"

# Check if a pre-compiled binary is provided (e.g. from a release zip)
$prebuilt = $null
if (Test-Path (Join-Path $PSScriptRoot "hmba-mail.exe")) {
    $prebuilt = Join-Path $PSScriptRoot "hmba-mail.exe"
} elseif (Test-Path (Join-Path $repoRoot "hmba-mail.exe")) {
    $prebuilt = Join-Path $repoRoot "hmba-mail.exe"
} elseif (Test-Path (Join-Path $distDir "hmba-mail.exe")) {
    $prebuilt = Join-Path $distDir "hmba-mail.exe"
}

if ($prebuilt) {
    Write-Host "==> Using pre-compiled binary: $prebuilt"
    $distExe = $prebuilt
} else {
    Write-Host "==> Verifying Go tests..."
    Push-Location $repoRoot
    try {
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw "Tests failed" }

        Write-Host "==> Compiling hmba-mail.exe..."
        New-Item -ItemType Directory -Force $distDir | Out-Null
        $distExe = Join-Path $distDir "hmba-mail.exe"
        & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $distExe ./cmd/hmba-mail
        if ($LASTEXITCODE -ne 0) { throw "Build failed" }

        $hash = (Get-FileHash $distExe -Algorithm SHA256).Hash.ToLowerInvariant()
        Set-Content -Path (Join-Path $distDir "SHA256SUMS") -Value "$hash  hmba-mail.exe"
        Write-Host "Built: $distExe ($hash)"
    } finally {
        Pop-Location
    }
}


Write-Host "==> Packaging into $TargetDir..."
$binDir = Join-Path $TargetDir "bin"
$stateDir = Join-Path $TargetDir "state"
$logsDir = Join-Path $stateDir "logs"
$origDir = Join-Path $TargetDir "originals"

New-Item -ItemType Directory -Force $binDir | Out-Null
New-Item -ItemType Directory -Force $stateDir | Out-Null
New-Item -ItemType Directory -Force $logsDir | Out-Null
New-Item -ItemType Directory -Force $origDir | Out-Null

$targetExe = Join-Path $binDir "hmba-mail.exe"
Copy-Item -Path $distExe -Destination $targetExe -Force
Write-Host "Installed executable to: $targetExe"

# Generate local default config if not present
$cfgPath = Join-Path $TargetDir "config.json"
if (-not (Test-Path $cfgPath)) {
    $oneDrive = [System.Environment]::GetEnvironmentVariable("OneDriveConsumer")
    if (-not $oneDrive) { $oneDrive = [System.Environment]::GetEnvironmentVariable("OneDriveCommercial") }
    if (-not $oneDrive) { $oneDrive = Join-Path $HOME "OneDrive - UW" }
    
    $defaultInput = Join-Path $oneDrive "HMBA-Emails"
    $defaultShared = Join-Path $defaultInput "Markdown"
    $defaultArchive = Join-Path (Join-Path $HOME "Documents") "HMBA-Archive"

    $configObject = [ordered]@{
        version = 1
        input = $defaultInput
        archive = $defaultArchive
        shared = $defaultShared
        state = $stateDir
        originals = $origDir
        rules = @(
            [ordered]@{ subject = "Recent Canvas Notifications"; category = "canvas-digest" },
            [ordered]@{ subject = "Weekly Announcement"; category = "program-announcement" }
        )
        max_message_bytes = 52428800
        timeout_seconds = 600
        git = [ordered]@{
            enabled = $false
            archive_repo = ""
            remote = "origin"
            branch = "main"
        }
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($cfgPath, ($configObject | ConvertTo-Json -Depth 4), $utf8NoBom)
    Write-Host "Wrote default config to: $cfgPath"
} else {
    Write-Host "Preserved existing config at: $cfgPath"
}

# Write user README
$readmePath = Join-Path $TargetDir "README.md"
$readmeLines = @(
    "# HMBA Mail Automation",
    "",
    "This directory contains the installed standalone executable, active configuration, and local state.",
    "",
    "## Directory Layout",
    "",
    "- `bin/hmba-mail.exe`: Standalone Windows 64-bit executable (version $Version).",
    "- `config.json`: Active runtime configuration.",
    "- `run-task.vbs`: Windowless launcher for Windows Task Scheduler (created on `schedule install`).",
    "- `state/`: Runtime journal, kernel file locks, and rotating sanitized run logs (`state/logs/`).",
    "- `originals/`: Verifiable SHA-256 byte-for-byte local backup of converted EML source emails.",
    "",
    "## OneDrive Hydration Note",
    "",
    "The input directory should be set to **`Always keep on this device`** in Windows File Explorer.",
    "This prevents cloud dehydration from introducing network delays during automated startup runs.",
    "",
    "## CLI Commands",
    "",
    '```powershell',
    '# Check diagnostics and environment prerequisites',
    ('& "{0}" check --config "{1}"' -f $targetExe, $cfgPath),
    '',
    '# Inspect current scheduler status',
    ('& "{0}" schedule status' -f $targetExe),
    '',
    '# Register Windows Task Scheduler logon trigger (2-min delay, battery enabled, silent)',
    ('& "{0}" schedule install --config "{1}"' -f $targetExe, $cfgPath),
    '',
    '# Trigger an immediate manual run via Task Scheduler',
    ('& "{0}" schedule run' -f $targetExe),
    '',
    '# Run manual conversion dry-run',
    ('& "{0}" run --config "{1}" --dry-run' -f $targetExe, $cfgPath),
    '```'
)
[System.IO.File]::WriteAllLines($readmePath, $readmeLines)
Write-Host "Wrote README to: $readmePath"

Write-Host "==> Packaging complete and verified!"
