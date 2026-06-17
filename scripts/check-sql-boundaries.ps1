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
    "internal/gateway/store.go",
    "internal/gateway/source_server.go",
    "internal/handlers/backfill.go",
    "internal/handlers/compat_item_enrich.go",
    "internal/handlers/compat_items.go",
    "internal/handlers/compat_media.go",
    "internal/handlers/compat_query.go",
    "internal/handlers/compat_show.go",
    "internal/handlers/emby_compat.go",
    "internal/handlers/library_detail.go",
    "internal/handlers/library_manage.go",
    "internal/handlers/library_platform.go",
    "internal/handlers/library_query.go",
    "internal/handlers/library_refresh.go",
    "internal/handlers/library_scrape.go",
    "internal/handlers/playback.go",
    "internal/handlers/stats.go",
    "internal/handlers/system.go",
    "internal/handlers/videos.go",
    "internal/models/item_counts.go",
    "internal/models/item_lookup.go",
    "internal/models/item_merge.go",
    "internal/models/item_nextup.go",
    "internal/models/item_query.go",
    "internal/models/library.go",
    "internal/models/person.go",
    "internal/models/person_admin.go",
    "internal/models/platform.go",
    "internal/services/auto_scrape.go",
    "internal/services/backfill_actor_images.go",
    "internal/services/backfill_episode_image.go",
    "internal/services/backfill_episode_name.go",
    "internal/services/backfill_media_quality.go",
    "internal/services/coverart/fetch.go",
    "internal/services/episode_fetch.go",
    "internal/services/gap_scan.go",
    "internal/services/incremental_scan.go",
    "internal/services/ingest_match.go",
    "internal/services/item_delete_plan.go",
    "internal/services/notify.go",
    "internal/services/notify_sweeper.go",
    "internal/services/probe_on_play.go",
    "internal/services/probe_task.go",
    "internal/services/progress_buffer.go",
    "internal/services/redirect_bitrate.go",
    "internal/services/refresh_scheduler.go",
    "internal/services/refresh_worker.go",
    "internal/services/scanner_cleanup.go",
    "internal/services/scanner_dir.go",
    "internal/services/scanner_mixed.go",
    "internal/services/scanner_movie.go",
    "internal/services/scanner_nfo.go",
    "internal/services/scanner_tv.go",
    "internal/services/scrape_config.go",
    "internal/services/tmdb_identify.go",
    "internal/services/tmdb_utils.go",
    "internal/services/unmatched.go"
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
