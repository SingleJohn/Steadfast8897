Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$jdk = "D:\services\jdks\ms-21.0.7"

if (-not $env:JAVA_HOME -and (Test-Path $jdk)) {
    $env:JAVA_HOME = $jdk
}
if ($env:JAVA_HOME) {
    $env:Path = "$env:JAVA_HOME\bin;$env:Path"
}

Push-Location $root
try {
    & .\gradlew.bat --no-daemon clean shadowJar
    $jar = Join-Path $root "build\libs\fyms-csp-sidecar-all.jar"
    if (-not (Test-Path $jar)) {
        throw "CSP sidecar fat jar 未生成: $jar"
    }
    Write-Host "CSP sidecar fat jar 已生成: $jar"
} finally {
    Pop-Location
}
