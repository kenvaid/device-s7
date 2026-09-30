param([switch]$Race, [switch]$Fuzz)
$ErrorActionPreference = 'Stop'
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$fixDriverRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$fixLibraryRoot = (Resolve-Path -LiteralPath (Join-Path $fixDriverRoot '../gos7')).Path
$previousCache = $env:GOCACHE
try {
    $env:GOCACHE = Join-Path $env:TEMP 's7-fix-validation-cache'
    foreach ($fixRoot in @($fixLibraryRoot, $fixDriverRoot)) {
        Push-Location -LiteralPath $fixRoot
        try {
            Write-Output "Validating $fixRoot"
            go test -count=1 -timeout=90s -cover ./...
            if ($LASTEXITCODE -ne 0) { throw "go test failed in $fixRoot" }
            go vet ./...
            if ($LASTEXITCODE -ne 0) { throw "go vet failed in $fixRoot" }
            go build ./...
            if ($LASTEXITCODE -ne 0) { throw "go build failed in $fixRoot" }
            if ($Race) {
                $fixLinuxRoot = '/mnt/' + $fixRoot.Substring(0,1).ToLower() + $fixRoot.Substring(2).Replace('\','/')
                wsl.exe --cd $fixLinuxRoot -- /usr/local/go/bin/go test -race -count=1 -timeout=90s ./...
                if ($LASTEXITCODE -ne 0) { throw "race check failed in $fixRoot" }
            }
        } finally { Pop-Location }
    }
    if ($Fuzz) {
        Push-Location -LiteralPath $fixLibraryRoot
        try {
            foreach ($fixTarget in @('FuzzHelpers','FuzzProtocolParsers')) {
                go test '-run=^$' "-fuzz=^$fixTarget`$" -fuzztime=5s -parallel=2
                if ($LASTEXITCODE -ne 0) { throw "fuzz check failed: $fixTarget" }
            }
        } finally { Pop-Location }
    }
} finally { $env:GOCACHE = $previousCache }
