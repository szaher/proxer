param(
    [string]$Version = "0.1.0",
    [string]$OutputDir = "output/desktop/windows",
    [string]$Manufacturer = "Proxer",
    [string]$ProductName = "Proxer Agent"
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
Set-Location $root

if (-not (Test-Path "internal/nativeagent/static/index.html")) {
    throw "Native desktop UI assets are missing. Run: cd agentweb && npm install && npm run sync-native-static"
}

$numericVersion = ($Version -replace '[^0-9\.]', '.').Trim('.')
$parts = @()
if ($numericVersion -ne "") {
    $parts = $numericVersion.Split('.', [System.StringSplitOptions]::RemoveEmptyEntries)
}
if ($parts.Count -eq 0) {
    $msiVersion = "0.1.0"
} else {
    while ($parts.Count -lt 3) {
        $parts += "0"
    }
    if ($parts.Count -gt 4) {
        $parts = $parts[0..3]
    }
    $msiVersion = ($parts -join '.')
}

$upgradeCode = "{D6F7C60D-6B7B-4E7E-9344-49976F2D6C5C}"
$componentGuid = "{85AA656D-57E2-4DF0-9A06-A9CBAE5201F3}"

$stageDir = Join-Path $OutputDir "stage"
New-Item -ItemType Directory -Force -Path $stageDir | Out-Null
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "Building proxer-agent.exe (wails shell enabled)"
$env:CGO_ENABLED = "1"
go build -trimpath -tags wails -o (Join-Path $stageDir "proxer-agent.exe") ./cmd/agent
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}

$wxsPath = Join-Path $stageDir "installer.wxs"
$exePath = (Join-Path $stageDir "proxer-agent.exe") -replace "\\", "\\\\"

@"
<Wix xmlns="http://wixtoolset.org/schemas/v4/wxs">
  <Package Name="$ProductName" Manufacturer="$Manufacturer" Version="$msiVersion" UpgradeCode="$upgradeCode" InstallerVersion="500" Scope="perMachine">
    <MajorUpgrade DowngradeErrorMessage="A newer version of $ProductName is already installed." />
    <MediaTemplate />
    <Feature Id="MainFeature" Title="$ProductName" Level="1">
      <ComponentGroupRef Id="ProductComponents" />
    </Feature>
  </Package>

  <Fragment>
    <StandardDirectory Id="ProgramFiles64Folder">
      <Directory Id="INSTALLFOLDER" Name="Proxer Agent" />
    </StandardDirectory>
  </Fragment>

  <Fragment>
    <ComponentGroup Id="ProductComponents" Directory="INSTALLFOLDER">
      <Component Guid="$componentGuid">
        <File Source="$exePath" KeyPath="yes" />
      </Component>
    </ComponentGroup>
  </Fragment>
</Wix>
"@ | Set-Content -Path $wxsPath -Encoding UTF8

$wixCmd = Get-Command wix -ErrorAction SilentlyContinue
if (-not $wixCmd) {
    throw "WiX CLI not found. Install WiX v4 (dotnet tool install --global wix)."
}

$msiName = "proxer-agent-$msiVersion-x64.msi"
$msiPath = Join-Path $OutputDir $msiName

Write-Host "Building MSI: $msiPath"
& $wixCmd.Path build $wxsPath -arch x64 -out $msiPath
if ($LASTEXITCODE -ne 0) {
    throw "wix build failed"
}

Write-Host "Built MSI: $msiPath"
