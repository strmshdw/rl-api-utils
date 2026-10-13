<#
.SYNOPSIS
    Wipes rl-sync's local data so the tool starts fresh.

.DESCRIPTION
    Deletes the state store and downloaded replays for each directory rl-sync is
    run from. By default that is the repo root (rl-sync.exe / go run) and
    cmd/rl-sync (the VS Code launch config). For each directory it removes:

      - the state store: rl-sync.db plus its -wal, -shm and -journal sidecars
        (or the .json store and its temp files when db_path ends in .json)
      - <replay_dir>\*.replay, including half-finished .tmp-*.replay downloads

    sync.db_path and sync.replay_dir from config.yaml / config.yml / config.json
    in that directory are honored, as are RL_SYNC_DB_PATH and RL_SYNC_REPLAY_DIR.

    The database also holds saved login tokens (auth_state table), so after a
    reset rl-sync will prompt you to log in again unless config.yaml has
    credentials. config.yaml itself is never touched.

.PARAMETER RunDir
    Extra directories rl-sync is run from (e.g. a Task Scheduler working dir).

.PARAMETER KeepReplays
    Only reset the database; leave .replay files on disk.

.PARAMETER Force
    Skip the confirmation prompt.

.EXAMPLE
    .\scripts\reset-data.ps1
    .\scripts\reset-data.ps1 -WhatIf
    .\scripts\reset-data.ps1 -KeepReplays -Force
#>
[CmdletBinding(SupportsShouldProcess)]
param(
    [string[]]$RunDir = @(),
    [switch]$KeepReplays,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent $PSScriptRoot

function Resolve-RunPath([string]$BaseDir, [string]$Path) {
    if ([System.IO.Path]::IsPathRooted($Path)) {
        return [System.IO.Path]::GetFullPath($Path)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $BaseDir $Path))
}

# Mirrors internal/config: defaults, then the first config file found in the
# working directory, then environment variables.
function Get-DataPaths([string]$Dir) {
    $dbPath = './rl-sync.db'
    $replayDir = './replays'

    foreach ($name in 'config.yaml', 'config.yml', 'config.json') {
        $file = Join-Path $Dir $name
        if (-not (Test-Path -LiteralPath $file)) { continue }
        $text = Get-Content -LiteralPath $file -Raw
        try {
            $json = $text | ConvertFrom-Json
            if ($json.sync.db_path) { $dbPath = $json.sync.db_path }
            if ($json.sync.replay_dir) { $replayDir = $json.sync.replay_dir }
        } catch {
            if ($text -match '(?m)^\s*db_path:\s*["'']?([^"''#\r\n]+?)["'']?\s*(?:#.*)?$') { $dbPath = $Matches[1] }
            if ($text -match '(?m)^\s*replay_dir:\s*["'']?([^"''#\r\n]+?)["'']?\s*(?:#.*)?$') { $replayDir = $Matches[1] }
        }
        break
    }

    if ($env:RL_SYNC_DB_PATH) { $dbPath = $env:RL_SYNC_DB_PATH }
    if ($env:RL_SYNC_REPLAY_DIR) { $replayDir = $env:RL_SYNC_REPLAY_DIR }

    return @{
        DBPath    = Resolve-RunPath $Dir $dbPath
        ReplayDir = Resolve-RunPath $Dir $replayDir
    }
}

# Deleting a SQLite database out from under a running daemon corrupts it, and
# Windows will refuse to delete the locked files anyway.
$running = Get-Process -Name 'rl-sync' -ErrorAction SilentlyContinue
if ($running) {
    Write-Host "rl-sync is running (PID $($running.Id -join ', ')). Stop it first: Stop-Process -Name rl-sync" -ForegroundColor Red
    exit 1
}

$dirs = @($RepoRoot, (Join-Path $RepoRoot 'cmd\rl-sync')) + $RunDir |
    ForEach-Object { Resolve-RunPath (Get-Location).Path $_ } |
    Where-Object { Test-Path -LiteralPath $_ -PathType Container } |
    Select-Object -Unique

$targets = New-Object System.Collections.Generic.List[string]
foreach ($dir in $dirs) {
    $paths = Get-DataPaths $dir

    if ($paths.DBPath -like '*.json') {
        $targets.Add($paths.DBPath)
        $dbDir = Split-Path -Parent $paths.DBPath
        if (Test-Path -LiteralPath $dbDir) {
            Get-ChildItem -LiteralPath $dbDir -Filter '.rl-sync-state-*.tmp' -File -Force |
                ForEach-Object { $targets.Add($_.FullName) }
        }
    } else {
        foreach ($suffix in '', '-wal', '-shm', '-journal') {
            $targets.Add($paths.DBPath + $suffix)
        }
    }

    if (-not $KeepReplays -and (Test-Path -LiteralPath $paths.ReplayDir -PathType Container)) {
        Get-ChildItem -LiteralPath $paths.ReplayDir -Filter '*.replay' -File -Force |
            ForEach-Object { $targets.Add($_.FullName) }
    }
}

$existing = @($targets | Select-Object -Unique | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
if ($existing.Count -eq 0) {
    Write-Host 'Nothing to delete - rl-sync data is already clean.'
    exit 0
}

$replayCount = @($existing | Where-Object { $_ -like '*.replay' }).Count
Write-Host 'The following rl-sync data will be deleted:'
$existing | Where-Object { $_ -notlike '*.replay' } | ForEach-Object { Write-Host "  $_" }
if ($replayCount -gt 0) {
    $existing | Where-Object { $_ -like '*.replay' } | ForEach-Object { Split-Path -Parent $_ } |
        Group-Object | ForEach-Object { Write-Host "  $($_.Count) replay file(s) in $($_.Name)" }
}

if (-not $Force -and -not $WhatIfPreference) {
    $answer = Read-Host 'Continue? [y/N]'
    if ($answer -notmatch '^(y|yes)$') {
        Write-Host 'Aborted.'
        exit 1
    }
}

$failed = 0
foreach ($file in $existing) {
    if ($PSCmdlet.ShouldProcess($file, 'Delete')) {
        try {
            Remove-Item -LiteralPath $file -Force
        } catch {
            Write-Warning "Could not delete ${file}: $($_.Exception.Message)"
            $failed++
        }
    }
}

if ($WhatIfPreference) { exit 0 }
if ($failed -gt 0) {
    Write-Host "$failed file(s) could not be deleted. Close anything using them and re-run." -ForegroundColor Red
    exit 1
}
Write-Host "Deleted $($existing.Count) file(s). rl-sync will create a fresh database on next start."

# If the data files are committed, `git pull` will keep bringing them back.
if (Get-Command git -ErrorAction SilentlyContinue) {
    $relative = @($existing | ForEach-Object {
        if ($_.StartsWith($RepoRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
            $_.Substring($RepoRoot.Length).TrimStart('\', '/').Replace('\', '/')
        }
    })
    $tracked = @()
    if ($relative.Count -gt 0) {
        # Windows PowerShell turns native stderr into an error under 'Stop' (e.g. outside a repo).
        try { $tracked = @(git -C $RepoRoot ls-files -- $relative 2>$null) } catch { }
    }
    if ($tracked.Count -gt 0) {
        Write-Host ''
        Write-Warning "$($tracked.Count) of the deleted files are tracked by git, so git now shows them as deleted and a pull will restore them."
        Write-Host 'To stop committing local data, run once and commit:'
        Write-Host "  git rm -r --cached --ignore-unmatch -- 'rl-sync.db*' 'cmd/rl-sync/rl-sync.db*' replays"
    }
}
