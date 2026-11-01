[CmdletBinding()]
param (
    [Alias('i')][string]$InstallDir = '',
    [Alias('t')][string]$Tag = 'latest',
    [string]$ReleaseBaseUrl = 'https://github.com/N-Silbernagel/fs-clip/releases'
)

Set-StrictMode -Version 3.0
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
if ($env:OS -ne 'Windows_NT') { throw 'Use install.sh on macOS and Linux.' }
$architecture = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($architecture) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $architecture" }
}
$base = $ReleaseBaseUrl.TrimEnd('/')
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
[IO.Directory]::CreateDirectory($tempDir) | Out-Null
try {
    $release = Invoke-RestMethod "$base/$Tag" -Headers @{ Accept = 'application/json' }
    $tagName = $release.tag_name
    if (-not $tagName -or $tagName -notmatch '^[a-zA-Z0-9._-]+$') { throw 'Unable to determine release tag.' }
    $version = $tagName -replace '^v', ''
    $archive = "fs-clip_${version}_windows_${arch}.zip"
    $archivePath = Join-Path $tempDir $archive
    Invoke-WebRequest "$base/download/$tagName/$archive" -UseBasicParsing -OutFile $archivePath
    $manifest = (Invoke-WebRequest "$base/download/$tagName/fs-clip_${version}_checksums.txt" -UseBasicParsing).Content
    if ($manifest -is [byte[]]) { $manifest = [Text.Encoding]::UTF8.GetString($manifest) }
    $want = @($manifest -split '\r?\n' | ForEach-Object {
        if ($_ -match '^([a-fA-F0-9]{64})\s+(.+)$' -and $Matches[2] -eq $archive) { $Matches[1] }
    })
    if ($want.Count -ne 1) { throw 'Missing or invalid archive checksum.' }
    $got = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash
    if ($want[0] -ne $got) { throw 'Archive checksum did not verify.' }
    Expand-Archive -LiteralPath $archivePath -DestinationPath $tempDir
    $binary = Join-Path $tempDir 'fs-clip.exe'
    $installArgs = @('install')
    if ($InstallDir) { $installArgs += @('--bin-dir', $InstallDir) }
    # Piping waits for the GUI-subsystem executable and retains its command output.
    & $binary @installArgs | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "FSClip installation failed (exit $LASTEXITCODE)." }
} finally {
    Remove-Item -LiteralPath $tempDir -Recurse -Force
}
