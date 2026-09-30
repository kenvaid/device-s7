# Read-only review checks. Failures describe contracts the current implementation violates.
param([switch]$Race)
$ErrorActionPreference = 'Stop'
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$reviewGos7 = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../../../gos7')).Path
$reviewSource = Join-Path $PSScriptRoot 'gos7_review_repro_test.go.txt'
$reviewOverlay = Join-Path $env:TEMP ('gos7-review-' + [guid]::NewGuid().ToString('N') + '.json')
$reviewVirtual = Join-Path $reviewGos7 'zz_review_test.go'
$reviewExit = 1
try {
    if ($Race) {
        $reviewLinuxRepo = '/mnt/' + $reviewGos7.Substring(0,1).ToLower() + $reviewGos7.Substring(2).Replace('\','/')
        $reviewLinuxSource = '/mnt/' + $reviewSource.Substring(0,1).ToLower() + $reviewSource.Substring(2).Replace('\','/')
        $reviewLinuxOverlay = '/mnt/' + $reviewOverlay.Substring(0,1).ToLower() + $reviewOverlay.Substring(2).Replace('\','/')
        @{ Replace = @{ ($reviewLinuxRepo + '/zz_review_test.go') = $reviewLinuxSource } } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $reviewOverlay -Encoding utf8NoBOM
        wsl.exe -- bash -lc "cd '$reviewLinuxRepo' && /usr/local/go/bin/go test -race -overlay '$reviewLinuxOverlay' -run '^TestReviewGos7Concurrent' -v -count=1 -timeout=20s ."
        $reviewExit = $LASTEXITCODE
    } else {
        @{ Replace = @{ $reviewVirtual = $reviewSource } } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $reviewOverlay -Encoding utf8NoBOM
        Push-Location -LiteralPath $reviewGos7
        try {
            go test -overlay $reviewOverlay -run '^TestReviewGos7Contracts$' -v -count=1 -timeout=20s .
            $reviewExit = $LASTEXITCODE
        } finally { Pop-Location }
    }
} finally {
    Remove-Item -LiteralPath $reviewOverlay -ErrorAction SilentlyContinue
}
exit $reviewExit
