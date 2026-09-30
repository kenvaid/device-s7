param(
    [ValidateSet('upstream','typed')][string]$Mode='typed',
    [ValidateRange(1,10)][int]$Count=3
)
$ErrorActionPreference='Stop'
$OutputEncoding=[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new()
$s7DeviceRoot=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$s7FixtureRoot=Join-Path $PSScriptRoot 'python-snap7'
$s7PythonEnv=Join-Path $env:TEMP 'gos7-python-simulator-venv'
$s7PythonExe=Join-Path $s7PythonEnv 'Scripts/python.exe'
if(-not (Test-Path -LiteralPath $s7PythonExe)) {
    python.exe -m venv $s7PythonEnv
    if($LASTEXITCODE -ne 0) {throw 'Cannot create simulator virtual environment.'}
}
$s7InstalledVersion=& $s7PythonExe -c "import importlib.metadata; print(importlib.metadata.version('python-snap7'))" 2>$null
if($LASTEXITCODE -ne 0 -or $s7InstalledVersion -ne '3.2.0') {
    & $s7PythonExe -m pip install --disable-pip-version-check --no-cache-dir --index-url https://pypi.org/simple 'python-snap7==3.2.0'
    if($LASTEXITCODE -ne 0) {throw 'Cannot install python-snap7 3.2.0.'}
}
if($Mode -eq 'typed') {
    & $s7PythonExe (Join-Path $s7FixtureRoot 'test_typed_server.py') 2>&1 | Tee-Object -FilePath (Join-Path $s7FixtureRoot 'typed-wire-vectors.txt')
    if($LASTEXITCODE -ne 0) {throw 'Typed simulator fixed wire vectors failed.'}
}
if(netstat -ano -p tcp | Select-String '^\s*TCP\s+\S+:(1102|1103)\s+\S+\s+LISTENING\s+') {throw 'Ports 1102/1103 are already occupied.'}
$s7Process=$null
$s7Environment=@{}
foreach($name in @('GOCACHE','S7_PYTHON_ADDRESS','S7_PYTHON_OBSERVER','S7_PYTHON_TRACE')) {$s7Environment[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try {
    $s7ServerScript=Join-Path $s7FixtureRoot 'server.py'
    $s7Process=Start-Process -FilePath $s7PythonExe -ArgumentList @('-u',('"'+$s7ServerScript+'"'),'--mode',$Mode) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $s7FixtureRoot "$Mode-server.log") -RedirectStandardError (Join-Path $s7FixtureRoot "$Mode-server-errors.log")
    $s7Ready=$false
    for($s7Attempt=0;$s7Attempt -lt 50;$s7Attempt++) {
        if($s7Process.HasExited) {throw "Simulator exited. See $Mode-server-errors.log."}
        try {$s7Health=Invoke-RestMethod -Uri http://127.0.0.1:1103/health -TimeoutSec 1; if($s7Health.mode -eq $Mode -and $s7Health.version -eq '3.2.0') {$s7Ready=$true;break}} catch {Start-Sleep -Milliseconds 100}
    }
    if(-not $s7Ready) {throw 'Simulator did not become ready.'}
    $env:GOCACHE=Join-Path $env:TEMP 's7-fix-validation-cache'
    $env:S7_PYTHON_ADDRESS='127.0.0.1:1102'
    $env:S7_PYTHON_OBSERVER='http://127.0.0.1:1103'
    $env:S7_PYTHON_TRACE='0'
    Push-Location -LiteralPath $s7DeviceRoot
    try {
        $s7Module=go list -m -json github.com/kenvaid/gos7 | ConvertFrom-Json
        if($LASTEXITCODE -ne 0 -or -not $s7Module.Dir) {throw 'Cannot resolve the device-s7 gos7 dependency.'}
        $s7LibraryRoot=if($s7Module.Replace) {$s7Module.Replace.Dir} else {$s7Module.Dir}
        Write-Output "python-snap7 3.2.0 mode=$Mode; gos7=$($s7Module.Version); source=$s7LibraryRoot"
        Write-Output 'S7=127.0.0.1:1102; read-only memory observer=http://127.0.0.1:1103'
        go test "-count=$Count" -timeout=90s -v ./doc/review/pythonsim 2>&1 | Tee-Object -FilePath (Join-Path $s7FixtureRoot "$Mode-results.txt")
        if($LASTEXITCODE -ne 0) {throw "Integration tests failed in $Mode mode; see $Mode-results.txt."}
    } finally {Pop-Location}
} finally {
    if($s7Process) {
        if(-not $s7Process.HasExited) {
            try {Invoke-WebRequest -Uri http://127.0.0.1:1103/shutdown -Method Post -TimeoutSec 2 | Out-Null} catch {}
            if(-not $s7Process.WaitForExit(8000)) {Stop-Process -Id $s7Process.Id -Force}
        }
    }
    foreach($name in $s7Environment.Keys) {[Environment]::SetEnvironmentVariable($name,$s7Environment[$name],'Process')}
}
