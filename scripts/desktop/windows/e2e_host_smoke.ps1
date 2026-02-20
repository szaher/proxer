param(
    [string]$OutputDir = "output/desktop/windows",
    [string]$MsiPath = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($MsiPath)) {
    $msi = Get-ChildItem (Join-Path $OutputDir "*.msi") | Select-Object -First 1
    if (-not $msi) {
        throw "MSI not found under $OutputDir"
    }
    $MsiPath = $msi.FullName
}

if (-not (Test-Path $MsiPath)) {
    throw "MSI not found: $MsiPath"
}

$exe = Join-Path $env:ProgramFiles "Proxer Agent\proxer-agent.exe"
$smokeOut = Join-Path $OutputDir "smoke-help.txt"
$statusOut = Join-Path $OutputDir "smoke-status.json"

$install = Start-Process msiexec.exe -ArgumentList "/i `"$MsiPath`" /qn /norestart" -Wait -PassThru
if ($install.ExitCode -ne 0 -and $install.ExitCode -ne 3010) {
    throw "MSI install failed with exit code $($install.ExitCode)"
}
if (-not (Test-Path $exe)) {
    throw "Installed executable missing: $exe"
}

& $exe help | Tee-Object -FilePath $smokeOut | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Executable help command failed with code $LASTEXITCODE"
}
if (-not (Select-String -Path $smokeOut -Pattern "proxer-agent run" -SimpleMatch -Quiet)) {
    throw "Help output did not contain expected command list"
}

& $exe status --json | Tee-Object -FilePath $statusOut | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Executable status command failed with code $LASTEXITCODE"
}

$uninstall = Start-Process msiexec.exe -ArgumentList "/x `"$MsiPath`" /qn /norestart" -Wait -PassThru
if ($uninstall.ExitCode -ne 0 -and $uninstall.ExitCode -ne 3010) {
    throw "MSI uninstall failed with exit code $($uninstall.ExitCode)"
}
Start-Sleep -Seconds 2
if (Test-Path $exe) {
    throw "Executable still present after uninstall: $exe"
}

Write-Host "MSI smoke passed"
