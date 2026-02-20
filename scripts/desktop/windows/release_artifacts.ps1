param(
    [Parameter(Mandatory = $true)]
    [string]$TargetDir,
    [Parameter(Mandatory = $true)]
    [string[]]$Artifacts
)

$ErrorActionPreference = "Stop"

if ($Artifacts.Count -lt 1) {
    throw "At least one artifact path is required"
}

New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null
$targetFull = (Resolve-Path $TargetDir).Path
Get-ChildItem -Path $targetFull -Force | Remove-Item -Recurse -Force

$copied = @()
foreach ($artifact in $Artifacts) {
    if (-not (Test-Path $artifact)) {
        throw "Artifact not found: $artifact"
    }
    $dest = Join-Path $targetFull (Split-Path $artifact -Leaf)
    Copy-Item -Path $artifact -Destination $dest -Force
    $copied += $dest
}

$checksumLines = @()
$manifestArtifacts = @()
foreach ($item in $copied) {
    $hash = Get-FileHash -Path $item -Algorithm SHA256
    $name = Split-Path $item -Leaf
    $checksumLines += "{0}  {1}" -f $hash.Hash.ToLowerInvariant(), $name
    $manifestArtifacts += [ordered]@{
        name       = $name
        size_bytes = (Get-Item $item).Length
        sha256     = $hash.Hash.ToLowerInvariant()
    }
}
$checksumLines | Set-Content -Path (Join-Path $targetFull "checksums.txt") -Encoding UTF8

$manifest = [ordered]@{
    generated_at = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    commit       = if ([string]::IsNullOrWhiteSpace($env:GITHUB_SHA)) { "unknown" } else { $env:GITHUB_SHA }
    ref          = if ([string]::IsNullOrWhiteSpace($env:GITHUB_REF_NAME)) { "unknown" } else { $env:GITHUB_REF_NAME }
    artifacts    = $manifestArtifacts
}
$manifest | ConvertTo-Json -Depth 5 | Set-Content -Path (Join-Path $targetFull "release-manifest.json") -Encoding UTF8

$skipSbom = @("1", "true", "yes") -contains (($env:PROXER_SKIP_SBOM ?? "").ToLowerInvariant())
if (-not $skipSbom) {
    $gobin = go env GOBIN
    if ([string]::IsNullOrWhiteSpace($gobin)) {
        $gopath = go env GOPATH
        $gobin = Join-Path $gopath "bin"
    }
    if (-not (Test-Path $gobin)) {
        New-Item -ItemType Directory -Force -Path $gobin | Out-Null
    }
    $cyclonedx = Join-Path $gobin "cyclonedx-gomod.exe"
    if (-not (Test-Path $cyclonedx)) {
        go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.8.0
    }
    & $cyclonedx mod -licenses -json -output (Join-Path $targetFull "sbom-go.cdx.json")
    if ($LASTEXITCODE -ne 0) {
        throw "failed to generate go SBOM"
    }

    if (Test-Path "agentweb/package-lock.json") {
        Push-Location agentweb
        npx --yes @cyclonedx/cyclonedx-npm --output-format JSON --output-file (Join-Path $targetFull "sbom-npm.cdx.json")
        if ($LASTEXITCODE -ne 0) {
            Pop-Location
            throw "failed to generate npm SBOM"
        }
        Pop-Location
    }
}

$timestamp = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$releasePath = Join-Path $targetFull "release-notes.md"
$commit = if ([string]::IsNullOrWhiteSpace($env:GITHUB_SHA)) { "unknown" } else { $env:GITHUB_SHA }
$ref = if ([string]::IsNullOrWhiteSpace($env:GITHUB_REF_NAME)) { "unknown" } else { $env:GITHUB_REF_NAME }
$lines = @()
$lines += "# Proxer Desktop Release Notes"
$lines += ""
$lines += "- Generated at: $timestamp"
$lines += "- Commit: $commit"
$lines += "- Ref: $ref"
$lines += ""
$lines += "## Artifacts"
foreach ($item in $copied) {
    $lines += "- $(Split-Path $item -Leaf)"
}
$lines += ""
$lines += "## Included Metadata"
$lines += "- checksums.txt (SHA-256)"
$lines += "- release-manifest.json"
if (Test-Path (Join-Path $targetFull "sbom-go.cdx.json")) {
    $lines += "- sbom-go.cdx.json"
}
if (Test-Path (Join-Path $targetFull "sbom-npm.cdx.json")) {
    $lines += "- sbom-npm.cdx.json"
}
if ($skipSbom) {
    $lines += "- SBOM generation skipped (PROXER_SKIP_SBOM=true)"
}
$lines | Set-Content -Path $releasePath -Encoding UTF8
