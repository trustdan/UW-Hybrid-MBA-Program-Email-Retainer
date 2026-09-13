$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Push-Location $repoRoot
try {
    Write-Host "==> Checking go vet..."
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    Write-Host "==> Running tests..."
    & go test -v ./...
    if ($LASTEXITCODE -ne 0) { throw "tests failed" }

    Write-Host "All tests passed!"
} finally {
    Pop-Location
}
