[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [Parameter(Mandatory = $true)]
    [string]$AviUtl2SdkDir,

    [string]$OutputDirectory = "dist"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$sdkPath = (Resolve-Path -LiteralPath $AviUtl2SdkDir).Path
$outputPath = Join-Path $projectRoot $OutputDirectory
$buildPath = Join-Path $projectRoot "build/release"
$stagePath = Join-Path $buildPath "package"
$pluginPath = Join-Path $stagePath "Plugin/AviUtl2-MCP"
$nativeBuildPath = Join-Path $buildPath "plugin"
$visualStudioCMake = "C:\Program Files\Microsoft Visual Studio\2022\Community\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe"
$cmakeCommand = if (Test-Path -LiteralPath $visualStudioCMake) { $visualStudioCMake } else { "cmake" }

if (-not (Test-Path -LiteralPath (Join-Path $sdkPath "plugin2.h"))) {
    throw "AviUtl2SdkDir must directly contain plugin2.h"
}

# Some Unix toolchains add both Path and PATH to a Windows process. MSBuild
# rejects that environment because its variable table is case-insensitive.
$pathVariables = @([Environment]::GetEnvironmentVariables().Keys | Where-Object { $_ -ieq "Path" })
if ($pathVariables.Count -gt 1) {
    [Environment]::SetEnvironmentVariable("PATH", $null, "Process")
}

if (Test-Path -LiteralPath $nativeBuildPath) {
    Remove-Item -LiteralPath $nativeBuildPath -Recurse -Force
}
& $cmakeCommand -S (Join-Path $projectRoot "plugin") -B $nativeBuildPath `
    -DAVIUTL2_SDK_DIR="$sdkPath" -DBUILD_TESTING=OFF
if ($LASTEXITCODE -ne 0) { throw "CMake configure failed" }

& $cmakeCommand --build $nativeBuildPath --config Release
if ($LASTEXITCODE -ne 0) { throw "Native bridge build failed" }

if (Test-Path -LiteralPath $stagePath) {
    Remove-Item -LiteralPath $stagePath -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $pluginPath, $outputPath | Out-Null
$env:GOCACHE = Join-Path $buildPath "go-cache"
go -C $projectRoot build -trimpath -ldflags "-s -w -X main.version=$Version" `
    -o (Join-Path $pluginPath "aviutl2-mcp.exe") ./cmd/aviutl2-mcp
if ($LASTEXITCODE -ne 0) { throw "Go server build failed" }

$bridge = Get-ChildItem -LiteralPath $nativeBuildPath `
    -Filter "aviutl2-mcp-bridge.aux2" -Recurse | Select-Object -First 1
if ($null -eq $bridge) { throw "Built bridge was not found" }

Copy-Item -LiteralPath $bridge.FullName -Destination $pluginPath
Copy-Item -LiteralPath (Join-Path $projectRoot "package/package.ini") -Destination $stagePath
Copy-Item -LiteralPath (Join-Path $projectRoot "package/package.txt") -Destination $stagePath

$packageIni = Join-Path $stagePath "package.ini"
$packageIniContent = (Get-Content -LiteralPath $packageIni -Raw).Replace(
    "information=AviUtl2 MCPブリッジとGo MCPサーバー",
    "information=AviUtl2 MCP $Version by yh2237"
)
[System.IO.File]::WriteAllText(
    $packageIni,
    $packageIniContent,
    [System.Text.UTF8Encoding]::new($false)
)

$archive = Join-Path $outputPath "AviUtl2-MCP-$Version.au2pkg.zip"
if (Test-Path -LiteralPath $archive) {
    Remove-Item -LiteralPath $archive
}
Compress-Archive -Path (Join-Path $stagePath "*") -DestinationPath $archive
Write-Host "Created $archive"
