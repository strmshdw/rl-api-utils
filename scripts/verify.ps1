param(
    [switch]$SkipBuild,
    [switch]$SkipTests,
    [switch]$AllowLegacyLOC
)

$ErrorActionPreference = 'Stop'

$argsList = @("run", "./cmd/verify-gate")
if ($SkipBuild) { $argsList += "-skip-build" }
if ($SkipTests) { $argsList += "-skip-tests" }
if ($AllowLegacyLOC) { $argsList += "-allow-legacy-loc" }

& go @argsList
exit $LASTEXITCODE
