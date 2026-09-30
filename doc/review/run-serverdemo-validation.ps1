param([ValidateRange(1,10)][int]$Count=3)
$ErrorActionPreference='Stop'
$OutputEncoding=[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new()
$s7DeviceRoot=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$s7Listener=netstat -ano -p tcp | Select-String '^\s*TCP\s+(0\.0\.0\.0|127\.0\.0\.1):102\s+\S+\s+LISTENING\s+(\d+)\s*$'
if(-not $s7Listener) {throw 'No IPv4 listener on local TCP port 102.'}
foreach($line in $s7Listener) {
    $s7Process=Get-Process -Id ([int]$line.Matches[0].Groups[2].Value)
    if($s7Process.ProcessName -ne 'serverdemo') {throw "Port 102 is owned by $($s7Process.ProcessName), not ServerDemo."}
}
$s7Environment=@{}
foreach($name in @('GOCACHE','S7_SERVERDEMO_ADDRESS','S7_SERVERDEMO_BACKUP','S7_SERVERDEMO_TRACE')) {$s7Environment[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try {
    $env:GOCACHE=Join-Path $env:TEMP 's7-fix-validation-cache'
    $env:S7_SERVERDEMO_ADDRESS='127.0.0.1:102'
    $env:S7_SERVERDEMO_BACKUP=Join-Path $PSScriptRoot ('serverdemo-original-db-'+(Get-Date -Format 'yyyyMMdd-HHmmss')+'.json')
    $env:S7_SERVERDEMO_TRACE='0'
    Push-Location -LiteralPath $s7DeviceRoot
    try {
        $s7Module=go list -m -json github.com/kenvaid/gos7 | ConvertFrom-Json
        if($LASTEXITCODE -ne 0 -or -not $s7Module.Dir) {throw 'Cannot resolve the device-s7 gos7 dependency.'}
        $s7LibraryRoot=if($s7Module.Replace) {$s7Module.Replace.Dir} else {$s7Module.Dir}
        Write-Output "Testing gos7 $($s7Module.Version): $s7LibraryRoot"
        Write-Output 'ServerDemo: 127.0.0.1:102, rack=0, slot=2; DB contents and CPU status are restored.'
        go test "-count=$Count" -timeout=90s -v ./doc/review/serverdemo 2>&1 | Tee-Object -FilePath (Join-Path $PSScriptRoot 'serverdemo-final-results.txt')
        if($LASTEXITCODE -ne 0) {throw 'ServerDemo integration tests failed. See serverdemo-final-results.txt.'}
    } finally {Pop-Location}
} finally {
    foreach($name in $s7Environment.Keys) {[Environment]::SetEnvironmentVariable($name,$s7Environment[$name],'Process')}
}
