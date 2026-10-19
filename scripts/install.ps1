# Installs a reliab release binary on Windows without a POSIX shell,
# verifying its checksum. The PowerShell twin of scripts/install.sh.
#   -Version  default: the contents of .reliab-version
#   -Dest     default: $HOME\.reliab\bin
# Downloads from GitHub Releases over HTTPS, or from the mirror at
# $env:RELIAB_DOWNLOAD_URL, sending $env:RELIAB_TOKEN as a bearer token when
# set. With RELIAB_DOWNLOAD_URL unset, an installed and authenticated gh is
# used instead; it is optional.
# $env:RELIAB_DOWNLOAD_URL may also be a local directory or a file:// URL
# holding the release assets.
param(
  [string]$Version = $(if (Test-Path .reliab-version) { (Get-Content .reliab-version -Raw).Trim() } else { '' }),
  [string]$Dest = (Join-Path $HOME '.reliab\bin')
)
$ErrorActionPreference = 'Stop'
$repo = 'hbaldwin98/reliab'
if (-not $Version) { throw 'reliab-install: no -Version given and no .reliab-version' }
$Version = 'v' + $Version.TrimStart('v')
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "reliab_$($Version.TrimStart('v'))_windows_$arch.tar.gz"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $gh = Get-Command gh -ErrorAction SilentlyContinue
  $ghReady = $false
  if ($gh -and -not $env:RELIAB_DOWNLOAD_URL) {
    & gh auth status *> $null
    $ghReady = ($LASTEXITCODE -eq 0)
  }
  if ($ghReady) {
    & gh release download $Version -R $repo -p $asset -p checksums.txt -D $tmp
    if ($LASTEXITCODE -ne 0) { throw "reliab-install: gh release download failed" }
  } else {
    $base = if ($env:RELIAB_DOWNLOAD_URL) { $env:RELIAB_DOWNLOAD_URL } else { "https://github.com/$repo/releases/download/$Version" }
    $dir = if ($base -match '^file://') { ([Uri]$base).LocalPath } elseif ($base -notmatch '^[a-z][a-z0-9+.-]*://') { $base } else { '' }
    if ($dir) {
      foreach ($f in @($asset, 'checksums.txt')) {
        if (-not (Test-Path (Join-Path $dir $f))) { throw "reliab-install: $f is not in $dir" }
        Copy-Item (Join-Path $dir $f) $tmp
      }
    } else {
      $headers = @{}
      if ($env:RELIAB_TOKEN) { $headers['Authorization'] = "Bearer $env:RELIAB_TOKEN" }
      Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset) -Headers $headers
      Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -Headers $headers
    }
  }

  $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($asset))$" } | Select-Object -First 1
  if (-not $line) { throw "reliab-install: $asset is not in checksums.txt" }
  $want = ($line -split '\s+')[0].ToLower()
  $have = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
  if ($want -ne $have) { throw "reliab-install: checksum mismatch for $asset" }

  Push-Location $tmp
  try { & tar -xzf $asset; if ($LASTEXITCODE -ne 0) { throw 'reliab-install: extract failed' } } finally { Pop-Location }
  New-Item -ItemType Directory -Force -Path $Dest | Out-Null
  # reliab and the adapters that ship beside it (cmd/adapters).
  Get-ChildItem $tmp -Filter 'reliab*.exe' | Move-Item -Force -Destination $Dest
  Write-Host "reliab-install: installed reliab $Version to $Dest"
} finally {
  Remove-Item -Recurse -Force $tmp
}
