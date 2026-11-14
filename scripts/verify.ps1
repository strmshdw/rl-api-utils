param(
    [switch]$SkipBuild,
    [switch]$SkipTests,
    [switch]$RequireRace,
    [switch]$AllowLegacyLOC
)

$ErrorActionPreference = 'Stop'

$argsList = @("run", "./cmd/verify-gate")
if ($SkipBuild) { $argsList += "-skip-build" }
if ($SkipTests) { $argsList += "-skip-tests" }
if ($RequireRace) { $argsList += "-require-race" }
if ($AllowLegacyLOC) { $argsList += "-allow-legacy-loc" }

& go @argsList
exit $LASTEXITCODE
