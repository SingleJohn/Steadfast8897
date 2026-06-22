Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$src = Join-Path $root "src"
$out = Join-Path $root "classes"

if (-not (Get-Command javac -ErrorAction SilentlyContinue)) {
    throw "未找到 javac，无法编译 CSP sidecar classes"
}

New-Item -ItemType Directory -Force -Path $out | Out-Null
$sources = Get-ChildItem -Path $src -Recurse -Filter *.java | ForEach-Object { $_.FullName }
if (-not $sources -or $sources.Count -eq 0) {
    throw "CSP sidecar 源码为空"
}

& javac -encoding UTF-8 -d $out @sources
Write-Host "CSP sidecar classes 已生成: $out"
