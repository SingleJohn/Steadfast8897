param(
    [string]$Root = "."
)

$ErrorActionPreference = "Stop"

$rg = Get-Command rg -ErrorAction SilentlyContinue
if (-not $rg) {
    Write-Error "ripgrep (rg) is required to check SQL boundaries."
}

$repoRoot = (Resolve-Path -LiteralPath $Root).Path
$pattern = '\b(state\.DB|pool|tx|db|s\.pool|u\.pool|d\.pool|pb\.pool|e\.pool|li\.pool)\.(Query|QueryRow|Exec|Begin|CopyFrom|SendBatch)\s*\('

$allowedFiles = @(
    "main.go",
    "internal/database/database.go",
    "internal/repository/display_order_repository.go",
    "internal/repository/background_task_repository.go",
    "internal/repository/notify_repository.go",
    "internal/repository/stats_repository.go",
    "internal/repository/library_repository.go",
    "internal/repository/item_helper_repository.go",
    "internal/repository/item_read_repository.go",
    "internal/repository/compat_items_repository.go",
    "internal/repository/item_query_repository.go",
    "internal/repository/platform_repository.go",
    "internal/repository/person_repository.go",
    "internal/gateway/store.go",
    "internal/gateway/source_server.go",
    "internal/handlers/compat_query.go",
    "internal/handlers/system.go",
    "internal/models/item_merge.go",
    "internal/models/library.go",
    "internal/services/coverart/fetch.go",
    "internal/services/gap_scan.go",
    "internal/services/item_delete_plan.go",
    "internal/services/progress_buffer.go",
    "internal/services/redirect_bitrate.go",
    "internal/services/refresh_scheduler.go",
    "internal/services/refresh_worker.go",
    "internal/services/scanner_movie.go",
    "internal/services/scanner_nfo.go",
    "internal/services/scanner_tv.go"
)

$allowed = @{}
foreach ($file in $allowedFiles) {
    $allowed[$file.Replace("\", "/")] = $true
}

Push-Location -LiteralPath $repoRoot
try {
    $rgMatches = & $rg.Source -n --glob "*.go" --glob "!internal/db/gen/**" $pattern internal main.go
    $exit = $LASTEXITCODE
    if ($exit -eq 1) {
        Write-Host "No direct SQL calls found outside generated code."
        exit 0
    }
    if ($exit -ne 0) {
        exit $exit
    }

    $violations = New-Object System.Collections.Generic.List[string]
    foreach ($line in $rgMatches) {
        if ($line -notmatch "^(?<file>[^:]+):(?<rest>.*)$") {
            continue
        }
        $file = $Matches.file.Replace("\", "/")
        if (-not $allowed.ContainsKey($file)) {
            $violations.Add($line)
        }
    }

    if ($violations.Count -gt 0) {
        Write-Host "Direct SQL calls outside allowlist:"
        foreach ($v in $violations) {
            Write-Host "  $v"
        }
        Write-Host ""
        Write-Host "Add a sqlc query + repository method, or update internal/db/SQL_RESIDUALS.md and this allowlist with a reason."
        exit 1
    }

    Write-Host "SQL boundary check passed. Checked $($rgMatches.Count) direct SQL call(s)."
}
finally {
    Pop-Location
}
