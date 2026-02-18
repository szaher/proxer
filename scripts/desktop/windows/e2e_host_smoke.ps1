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

Start-Process msiexec.exe -ArgumentList "/i `"$MsiPath`" /qn /norestart" -Wait -PassThru | Out-Null
if (-not (Test-Path $exe)) {
    throw "Installed executable missing: $exe"
}

& $exe help | Tee-Object -FilePath $smokeOut | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Executable help command failed with code $LASTEXITCODE"
}

Start-Process msiexec.exe -ArgumentList "/x `"$MsiPath`" /qn /norestart" -Wait -PassThru | Out-Null
Start-Sleep -Seconds 2
if (Test-Path $exe) {
    throw "Executable still present after uninstall: $exe"
}

Write-Host "MSI smoke passed"
