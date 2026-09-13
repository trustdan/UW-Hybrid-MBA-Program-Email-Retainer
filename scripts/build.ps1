param([string]$Version = "0.1.0")
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Push-Location $repoRoot
try {
    Write-Host "==> Compiling hmba-mail..."
    New-Item -ItemType Directory -Force "dist" | Out-Null
    & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o "dist/hmba-mail.exe" ./cmd/hmba-mail
    if ($LASTEXITCODE -ne 0) { throw "Build failed" }
    Write-Host "Success: dist/hmba-mail.exe"
} finally {
    Pop-Location
}
