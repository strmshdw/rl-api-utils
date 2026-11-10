param(
    [ValidateSet("production", "prod", "development", "dev", "debug")]
    [string]$Mode = "production",
    [string]$Output = "rl-sync.exe"
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $RepoRoot

try {
    $Version = (Get-Content -Path (Join-Path $RepoRoot "VERSION") -Raw).Trim()
    $Commit = "HEAD"
    try {
        $Commit = (git rev-parse --short HEAD 2>$null).Trim()
    } catch {}

    $NormalizedMode = if ($Mode -in @("development", "dev", "debug")) { "development" } else { "production" }
    $BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    Write-Host "==> Building rl-sync $Version ($NormalizedMode, commit $Commit)" -ForegroundColor Cyan

    if ($NormalizedMode -eq "development") {
        Write-Host "==> Building web frontend in DEVELOPMENT/DEBUG mode (Preview toggle enabled)..." -ForegroundColor Yellow
        npm.cmd --prefix web run build:dev
    } else {
        Write-Host "==> Building web frontend in PRODUCTION mode (Preview disabled)..." -ForegroundColor Green
        npm.cmd --prefix web run build
    }
    if ($LASTEXITCODE -ne 0) {
        throw "Web frontend build failed with exit code $LASTEXITCODE"
    }

    $LdFlags = "-s -w " +
        "-X main.Version=$Version " +
        "-X github.com/dank/rl-api-utils/internal/version.Version=$Version " +
        "-X github.com/dank/rl-api-utils/internal/version.Commit=$Commit " +
        "-X github.com/dank/rl-api-utils/internal/version.BuildMode=$NormalizedMode " +
        "-X github.com/dank/rl-api-utils/internal/version.BuildTime=$BuildTime"

    Write-Host "==> Compiling Go server binary ($Output)..." -ForegroundColor Cyan
    go build -ldflags $LdFlags -o $Output ./cmd/rl-sync
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed with exit code $LASTEXITCODE"
    }

    Write-Host "==> Build complete: $Output (v$Version, mode=$NormalizedMode, commit=$Commit)" -ForegroundColor Green
} finally {
    Pop-Location
}
