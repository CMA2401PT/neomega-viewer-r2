# Build and package with Windows PowerShell 5.1 or PowerShell 7.
[CmdletBinding()]
param(
    [string]$GoCommand = 'go',
    [ValidatePattern('^[A-Za-z0-9._+-]+$')]
    [string]$Version
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$appRoot = $PSScriptRoot
$workspace = (Resolve-Path (Join-Path $appRoot '../..')).Path
$dist = [IO.Path]::GetFullPath((Join-Path $appRoot 'dist'))
$name = 'neomega-viewer-r2-windows-amd64'

# Accept an explicit toolchain path, then try PATH and Go's per-user installation.
$go = Get-Command $GoCommand -ErrorAction SilentlyContinue
if (-not $go -and $GoCommand -eq 'go') {
    $go = Get-Command "$env:LOCALAPPDATA/Programs/go/bin/go.exe" -ErrorAction SilentlyContinue
}
if (-not $go) { throw 'Go not found. Install Go or pass -GoCommand path/to/go.exe.' }
if (-not (Test-Path -LiteralPath (Join-Path $workspace 'go.work'))) {
    throw 'Run this script from the complete mymc workspace containing go.work.'
}

function Get-SourceRevision([string]$Path) {
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { return 'unknown' }
    $revision = & git -C $Path describe --always --dirty 2>$null
    if ($LASTEXITCODE -ne 0) { return 'unknown' }
    return "$revision".Trim()
}
$viewerRevision = Get-SourceRevision $appRoot
$rendererRevision = Get-SourceRevision (Join-Path $workspace 'neomega-r2')
if (-not $Version) { $Version = "$viewerRevision+r2.$rendererRevision" }

New-Item -ItemType Directory -Path $dist -Force | Out-Null
$staging = Join-Path $dist ('.package-' + [guid]::NewGuid().ToString('N'))
$bundle = Join-Path $staging $name
$binaryDirectory = Join-Path $bundle 'dist'
New-Item -ItemType Directory -Path $binaryDirectory -Force | Out-Null
$binary = Join-Path $binaryDirectory 'neomega-viewer-r2.exe'
$savedEnvironment = @{}
foreach ($key in @('GOOS', 'GOARCH', 'CGO_ENABLED', 'GOAMD64')) {
    $savedEnvironment[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}
Push-Location $workspace
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOAMD64 = 'v1'
    $linkFlags = "-s -w -X main.version=$Version"
    & $go.Source build -trimpath "-ldflags=$linkFlags" -o $binary ./apps/neomega-viewer-r2/cmd/viewer
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed; no package published.' }

    # Verify the release actually runs and carries the requested version.
    $actualVersion = & $binary -version
    if ($LASTEXITCODE -ne 0 -or "$actualVersion".Trim() -ne $Version) {
        throw 'Release executable version check failed.'
    }
    $buildInfo = & $go.Source version -m $binary
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read Go build information.' }
    $buildInfo[0] = $buildInfo[0].Replace($binary, 'neomega-viewer-r2.exe')
    foreach ($setting in @('-trimpath=true', 'GOOS=windows', 'GOARCH=amd64', 'CGO_ENABLED=0')) {
        if (-not (($buildInfo -join "`n").Contains($setting))) { throw "Missing build setting: $setting" }
    }

    # Inspect PE headers: AMD64, no COFF symbols and no DWARF debug sections.
    $bytes = [IO.File]::ReadAllBytes($binary)
    $pe = [BitConverter]::ToInt32($bytes, 0x3c)
    if ([BitConverter]::ToUInt32($bytes, $pe) -ne 0x4550 -or
        [BitConverter]::ToUInt16($bytes, $pe + 4) -ne 0x8664 -or
        [BitConverter]::ToUInt32($bytes, $pe + 16) -ne 0) {
        throw 'Unexpected PE architecture or unstripped symbol table.'
    }
    $sectionCount = [BitConverter]::ToUInt16($bytes, $pe + 6)
    $sectionTable = $pe + 24 + [BitConverter]::ToUInt16($bytes, $pe + 20)
    for ($i = 0; $i -lt $sectionCount; $i++) {
        $section = [Text.Encoding]::ASCII.GetString($bytes, $sectionTable + 40 * $i, 8).TrimEnd([char]0)
        if ($section -match '^\.(z?debug)') { throw "Debug section remains: $section" }
    }
    $binaryText = [Text.Encoding]::UTF8.GetString($bytes)
    foreach ($path in @($workspace, $workspace.Replace('\', '/'))) {
        if ($binaryText.Contains($path)) { throw "Local build path remains in executable: $path" }
    }
    $binaryText = $null
    $bytes = $null

    foreach ($file in @('start-windows.cmd', 'start-windows.ps1', 'LICENSE')) {
        Copy-Item -LiteralPath (Join-Path $appRoot $file) -Destination $bundle
    }
    @"
neOmega Viewer R2 - Windows amd64
Version: $Version

Extract the entire archive, then double-click start-windows.cmd.
Keep the dist directory beside the launcher. Chrome or Edge is required.
The launcher uses a separate OpenGL browser profile; default port is 2412.
For another port: start-windows.cmd -Port 2413
PowerShell execution policy is overridden only for the launcher process.
No permanent system execution-policy setting is changed.

The standalone executable is dist/neomega-viewer-r2.exe.
Direct3D shader compilation can still be slow; use the compatibility launcher.

Build: CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=v1
Flags: -trimpath -ldflags="$linkFlags"
COFF symbols, DWARF information and local workspace paths are stripped.
Go runtime metadata is retained.
"@ | Set-Content -LiteralPath (Join-Path $bundle 'README.txt') -Encoding UTF8
    [ordered]@{
        version = $Version
        viewerRevision = $viewerRevision
        rendererRevision = $rendererRevision
        buildInfo = $buildInfo
    } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $bundle 'build-info.json') -Encoding UTF8

    $archive = Join-Path $staging "$name.zip"
    Compress-Archive -LiteralPath $bundle -DestinationPath $archive
    Copy-Item -LiteralPath $binary -Destination (Join-Path $dist "$name.exe") -Force
    Copy-Item -LiteralPath $archive -Destination (Join-Path $dist "$name.zip") -Force
    $checksums = foreach ($extension in @('exe', 'zip')) {
        $fileName = "$name.$extension"
        $hash = Get-FileHash -LiteralPath (Join-Path $dist $fileName) -Algorithm SHA256
        "$($hash.Hash.ToLowerInvariant())  $fileName"
    }
    $checksums | Set-Content -LiteralPath (Join-Path $dist 'SHA256SUMS.txt') -Encoding ASCII
    Write-Output "Built and verified: $Version"
    Write-Output (Join-Path $dist "$name.zip")
} finally {
    Pop-Location
    foreach ($key in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($key, $savedEnvironment[$key], 'Process')
    }
    # Delete only this invocation's generated staging directory, directly under dist.
    $resolvedStaging = (Resolve-Path -LiteralPath $staging).Path
    if ([IO.Path]::GetDirectoryName($resolvedStaging) -ne $dist -or
        [IO.Path]::GetFileName($resolvedStaging) -notmatch '^\.package-[0-9a-f]{32}$') {
        throw "Refusing to remove unexpected staging path: $resolvedStaging"
    }
    Remove-Item -LiteralPath $resolvedStaging -Recurse -Force
}
