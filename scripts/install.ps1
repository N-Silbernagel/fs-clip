# fs-clip install script
# contains code from and inspired by
# https://github.com/twpayne/chezmoi/

<#

.SYNOPSIS
Install fs-clip.

.PARAMETER WatchDir
Specifies the directory to watch. "~/copy-to-clipboard" is the default. Alias: w

.PARAMETER Tag
Specifies the version of fs-clip to install. "latest" is the default. Alias: t

.PARAMETER EnableDebug
If specified, print debug output. Alias: d

.EXAMPLE
PS> iex "&{$(irm 'https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.ps1')}

#>
[CmdletBinding()]
param (
    [Parameter(Mandatory = $false)]
    [Alias('w')]
    [string]
    $WatchDir = (Join-Path -Path (Resolve-Path -Path '.') -ChildPath 'bin'),

    [Parameter(Mandatory = $false)]
    [Alias('t')]
    [string]
    $Tag = 'latest',

    [Parameter(Mandatory = $false)]
    [Alias('d')]
    [switch]
    $EnableDebug,
)

function Write-DebugVariable {
    param (
        [string[]]$variables
    )
    foreach ($variable in $variables) {
        $debugVariable = Get-Variable -Name $variable
        Write-Debug "$( $debugVariable.Name ): $( $debugVariable.Value )"
    }
}

function Invoke-CleanUp ($directory) {
    if (($null -ne $directory) -and (Test-Path -Path $directory)) {
        Write-Debug "removing ${directory}"
        Remove-Item -Path $directory -Recurse -Force
    }
}

function Invoke-FileDownload ($uri, $path) {
    Write-Debug "downloading ${uri}"
    $wc = [System.Net.WebClient]::new()
    $wc.Headers.Add('Accept', 'application/octet-stream')
    $wc.DownloadFile($uri, $path)
    $wc.Dispose()
}

function Invoke-StringDownload ($uri) {
    Write-Debug "downloading ${uri} as string"
    $wc = [System.Net.WebClient]::new()
    $wc.DownloadString($uri)
    $wc.Dispose()
}

function Get-GoOS {
    if ($PSVersionTable.PSEdition -eq 'Desktop') {
        return 'windows'
    }

    $isOSPlatform = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform
    $osPlatform = [System.Runtime.InteropServices.OSPlatform]

    if ($isOSPlatform.Invoke($osPlatform::Windows)) { return 'windows' }
    if ($isOSPlatform.Invoke($osPlatform::Linux)) { return 'linux' }
    if ($isOSPlatform.Invoke($osPlatform::OSX)) { return 'darwin' }

    Write-Error 'unable to determine GOOS'
}

function Get-GoArch {
    if ($PSVersionTable.PSEdition -eq 'Core') {
        $goArch = @{
            'Arm64' = 'arm64'
            'X64'   = 'amd64'
        }
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        $result = $goArch[$arch]
        if (-not $result) {
            throw "Unsupported OS architecture: $arch"
        }
        return $result
    }

    $cpuArch = (Get-CimInstance -ClassName Win32_Processor).Architecture

    if ([System.Environment]::Is64BitOperatingSystem) {
        switch ($cpuArch) {
            9  { return 'amd64' }
            12 { return 'arm64' }
            default {
                throw "Unsupported CPU architecture ($cpuArch) on a 64-bit OS."
            }
        }
    } else {
        switch ($cpuArch) {
            0 { return 'i386' }
            9 { return 'i386' } # AMD64 CPU running 32-bit OS
            5 { return 'arm' }
            12 { return 'arm' } # ARM64 CPU running 32-bit OS
            default {
                throw "Unsupported CPU architecture ($cpuArch) on a 32-bit OS."
            }
        }
    }
}

function Get-RealTag ($tag) {
    Write-Debug "checking GitHub for tag ${tag}"
    $releaseUrl = "${BaseUrl}/${tag}"
    $json = try {
        Invoke-RestMethod -Uri $releaseUrl -Headers @{ 'Accept' = 'application/json' }
    } catch {
        Write-Error "error retrieving GitHub release ${tag}"
    }
    $realTag = $json.tag_name
    Write-Debug "found tag ${realTag} for ${tag}"
    return $realTag
}

function Get-Checksums ($tag, $version) {
    $checksumsText = Invoke-StringDownload "${BaseUrl}/download/${tag}/fs-clip_${version}_checksums.txt"

    $checksums = @{}
    $lines = $checksumsText -split '\r?\n' | Where-Object { $_ }
    foreach ($line in $lines) {
        $value, $key = $line -split '\s+'
        $checksums[$key] = $value
    }
    $checksums
}

function Confirm-Checksum ($target, $checksums) {
    $basename = [System.IO.Path]::GetFileName($target)
    if (-not $checksums.ContainsKey($basename)) {
        Write-Error "unable to find checksum for ${target} in checksums"
    }
    $want = $checksums[$basename].ToLower()
    $got = (Get-FileHash -Path $target -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) {
        Write-Error "checksum for ${target} did not verify ${want} vs ${got}"
    }
}

function Expand-ChezmoiArchive ($path) {
    $parent = Split-Path -Path $path -Parent
    Write-Debug "extracting ${path} to ${parent}"
    if ($path.EndsWith('.tar.gz')) {
        & tar --extract --gzip --file $path --directory $parent
    }
    if ($path.EndsWith('.zip')) {
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [System.IO.Compression.ZipFile]::ExtractToDirectory($path, $parent)
    }
}

Set-StrictMode -Version 3.0

[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12

$script:ErrorActionPreference = 'Stop'
$script:InformationPreference = 'Continue'
if ($EnableDebug) {
    $script:DebugPreference = 'Continue'
}

trap {
    Invoke-CleanUp $tempDir
    break
}

$BaseUrl = 'https://github.com/N-Silbernagel/fs-clip/releases'

# convert $WatchDir to an absolute path
$WatchDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($WatchDir)

$tempDir = ''
do {
    $tempDir = Join-Path -Path ([System.IO.Path]::GetTempPath()) -ChildPath ([System.Guid]::NewGuid())
} while (Test-Path -Path $tempDir)
New-Item -ItemType Directory -Path $tempDir | Out-Null

Write-DebugVariable 'WatchDir', 'Tag', 'tempDir'

$goOS = Get-GoOS
$goArch = Get-GoArch
foreach ($variableName in @('goOS', 'goArch')) {
    Write-DebugVariable $variableName
}

$realTag = Get-RealTag $Tag
$version = $realTag.TrimStart('v')
Write-Information "found version ${version} for ${Tag}/${goOS}/${goArch}"

$binarySuffix = ''
$archiveFormat = 'tar.gz'
switch ($goOS) {
    'linux' {
        break
    }
    'windows' {
        $binarySuffix = '.exe'
        $archiveFormat = 'zip'
        break
    }
}
Write-DebugVariable 'binarySuffix', 'archiveFormat'

$archiveFilename = "fs-clip_${version}_${goOS}_${goArch}.${archiveFormat}"
$tempArchivePath = Join-Path -Path $tempDir -ChildPath $archiveFilename
Write-DebugVariable 'archiveFilename', 'tempArchivePath'
Invoke-FileDownload "${BaseUrl}/download/${realTag}/${archiveFilename}" $tempArchivePath

$checksums = Get-Checksums $realTag $version
Confirm-Checksum $tempArchivePath $checksums

Expand-ChezmoiArchive $tempArchivePath

$binaryFilename = "fs-clip${binarySuffix}"
$tempBinaryPath = Join-Path -Path $tempDir -ChildPath $binaryFilename
Write-DebugVariable 'binaryFilename', 'tempBinaryPath'
[System.IO.Directory]::CreateDirectory($WatchDir) | Out-Null
$binary = Join-Path -Path $WatchDir -ChildPath $binaryFilename
Write-DebugVariable 'binary'
Move-Item -Path $tempBinaryPath -Destination $binary -Force
Write-Information "installed ${binary}"

Invoke-CleanUp $tempDir

& $binary
