$ErrorActionPreference = "Stop"

param(
  [string]$Version = "",
  [string]$BaseUrl = "",
  [string]$Prefix = "",
  [switch]$AddToPath = $true
)

if (-not $Version) {
  $Version = if ($env:XMLUI_VERSION) { $env:XMLUI_VERSION } else { "latest" }
}
if (-not $BaseUrl -and $env:XMLUI_BASE_URL) {
  $BaseUrl = $env:XMLUI_BASE_URL
}

$Repo = "xmlui-org/xmlui-cli"

if ([System.Environment]::OSVersion.Platform -ne [System.PlatformID]::Win32NT) {
  throw "xmlui install: install.ps1 is only supported on Windows."
}

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "X64" { $Artifact = "xmlui-windows-amd64.zip" }
  default {
    throw "xmlui install: unsupported Windows architecture $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)."
  }
}

if ($BaseUrl) {
  $ResolvedBaseUrl = $BaseUrl
} elseif ($Version -eq "latest") {
  $ResolvedBaseUrl = "https://github.com/$Repo/releases/latest/download"
} else {
  $ResolvedBaseUrl = "https://github.com/$Repo/releases/download/$Version"
}

function Download-File {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$Path
  )

  $client = New-Object System.Net.WebClient
  try {
    $client.DownloadFile($Url, $Path)
  } finally {
    $client.Dispose()
  }
}

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("xmlui-install-" + [System.Guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $TempDir | Out-Null

try {
  $ArtifactPath = Join-Path $TempDir $Artifact
  $SumsPath = Join-Path $TempDir "SHA256SUMS"

  Write-Host "Downloading $Artifact..."
  Download-File -Url "$ResolvedBaseUrl/$Artifact" -Path $ArtifactPath

  Write-Host "Downloading SHA256SUMS..."
  Download-File -Url "$ResolvedBaseUrl/SHA256SUMS" -Path $SumsPath

  $Expected = $null
  foreach ($line in Get-Content -Path $SumsPath) {
    if ($line -match '^\s*([0-9a-fA-F]{64})\s+\*?(.+?)\s*$' -and $matches[2] -eq $Artifact) {
      $Expected = $matches[1].ToLowerInvariant()
      break
    }
  }
  if (-not $Expected) {
    throw "xmlui install: $Artifact not found in SHA256SUMS."
  }

  $Actual = (Get-FileHash -Algorithm SHA256 -Path $ArtifactPath).Hash.ToLowerInvariant()
  if ($Actual -ne $Expected) {
    throw "xmlui install: SHA256 mismatch for $Artifact. Expected $Expected, got $Actual."
  }
  Write-Host "SHA256 verified."

  Write-Host "Extracting..."
  Expand-Archive -LiteralPath $ArtifactPath -DestinationPath $TempDir -Force

  $Binary = Get-ChildItem -Path $TempDir -Filter "xmlui.exe" -File -Recurse | Select-Object -First 1
  if (-not $Binary) {
    throw "xmlui install: xmlui.exe not found in archive."
  }

  $Args = @("install")
  if ($Prefix) {
    $Args += @("--prefix", $Prefix)
  }
  if ($AddToPath) {
    $Args += "--add-to-path"
  }

  & $Binary.FullName @Args
} finally {
  Remove-Item -LiteralPath $TempDir -Recurse -Force -ErrorAction SilentlyContinue
}
