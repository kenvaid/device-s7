# Run isolated contract checks through a Go overlay. The current implementation is expected to fail.
$ErrorActionPreference = 'Stop'
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$reviewRepo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$reviewSource = Join-Path $PSScriptRoot 'review_repro_test.go.txt'
$reviewVirtualTest = Join-Path $reviewRepo 'internal/driver/review_repro_test.go'
$reviewOverlay = Join-Path $env:TEMP ('device-s7-review-' + [guid]::NewGuid().ToString('N') + '.json')
$reviewOriginalCache = $env:GOCACHE
$reviewExitCode = 1
try {
    @{ Replace = @{ $reviewVirtualTest = $reviewSource } } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $reviewOverlay -Encoding utf8
    $env:GOCACHE = Join-Path $env:TEMP 'device-s7-review-go-cache'
    Push-Location -LiteralPath $reviewRepo
    try {
        go test -overlay $reviewOverlay ./internal/driver -run '^TestReviewContracts$' -v -count=1
        $reviewExitCode = $LASTEXITCODE
    } finally {
        Pop-Location
    }
} finally {
    $env:GOCACHE = $reviewOriginalCache
    Remove-Item -LiteralPath $reviewOverlay -ErrorAction SilentlyContinue
}
exit $reviewExitCode
