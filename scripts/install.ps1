# fs-clip install script
# contains code from and inspired by
# https://github.com/twpayne/chezmoi/

<#

.SYNOPSIS
Install fs-clip.

.PARAMETER WatchDir
Specifies the directory to watch. "~/copy-to-clipboard" is the default. Alias: w

.PARAMETER InstallDir
Specifies where fs-clip.exe is installed. "%LOCALAPPDATA%\\Programs\\fs-clip" is the default. Alias: i

.PARAMETER Tag
Specifies the version of fs-clip to install. "latest" is the default. Alias: t

.PARAMETER ReleaseBaseUrl
Release server URL. Defaults to the GitHub releases endpoint.

.PARAMETER EnableDebug
If specified, print debug output. Alias: d

.EXAMPLE
PS> & ([scriptblock]::Create((Invoke-RestMethod 'https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.ps1')))

#>
[CmdletBinding()]
param (
    [Parameter(Mandatory = $false)]
    [Alias('w')]
    [string]
    $WatchDir = (Join-Path -Path $HOME -ChildPath 'copy-to-clipboard'),

    [Parameter(Mandatory = $false)]
    [Alias('i')]
    [string]
    $InstallDir = $(if ($env:LOCALAPPDATA) { Join-Path -Path $env:LOCALAPPDATA -ChildPath 'Programs\fs-clip' } else { Join-Path -Path $HOME -ChildPath '.local/bin/fs-clip' }),

    [Parameter(Mandatory = $false)]
    [Alias('t')]
    [string]
    $Tag = 'latest',

    [Parameter(Mandatory = $false)]
    [Alias('d')]
    [switch]
    $EnableDebug,

    [string]
    $ReleaseBaseUrl = 'https://github.com/N-Silbernagel/fs-clip/releases'
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

function Add-PathEntryIfMissing ([string]$entry) {
    $userPath = [System.Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($userPath) {
        $parts = $userPath -split ';' | Where-Object { $_ }
    }
    if (-not ($parts -contains $entry)) {
        $newParts = @($parts + $entry)
        $newUserPath = ($newParts -join ';')
        [System.Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    }

    $processParts = @()
    if ($env:Path) {
        $processParts = $env:Path -split ';' | Where-Object { $_ }
    }
    if (-not ($processParts -contains $entry)) {
        $env:Path = (($processParts + $entry) -join ';')
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

function Expand-Archive ($path) {
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

$tempDir = ''
$BaseUrl = $ReleaseBaseUrl.TrimEnd('/')

try {

    # convert $WatchDir to an absolute path
    $WatchDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($WatchDir)
    $InstallDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($InstallDir)

    $tempDir = ''
    do {
        $tempDir = Join-Path -Path ([System.IO.Path]::GetTempPath()) -ChildPath ([System.Guid]::NewGuid())
    } while (Test-Path -Path $tempDir)
    New-Item -ItemType Directory -Path $tempDir | Out-Null

    Write-DebugVariable 'WatchDir', 'InstallDir', 'Tag', 'tempDir'

    $goOS = Get-GoOS
    if ($goOS -ne 'windows') {
        throw 'Use install.sh on macOS and Linux.'
    }
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

    Expand-Archive $tempArchivePath

    $binaryFilename = "fs-clip${binarySuffix}"
    $tempBinaryPath = Join-Path -Path $tempDir -ChildPath $binaryFilename
    Write-DebugVariable 'binaryFilename', 'tempBinaryPath'
    [System.IO.Directory]::CreateDirectory($WatchDir) | Out-Null
    [System.IO.Directory]::CreateDirectory($InstallDir) | Out-Null
    $binary = Join-Path -Path $InstallDir -ChildPath $binaryFilename
    Write-DebugVariable 'binary'

    $userId = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $taskName = "dev.nils-silbernagel.fs-clip-${userId}"
    $existingTask = Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\' -and $_.TaskName -eq $taskName }
    if ($existingTask) {
        Stop-ScheduledTask -TaskName $taskName
        $deadline = [DateTime]::UtcNow.AddSeconds(30)
        while ((Get-ScheduledTask -TaskName $taskName).State -eq 'Running') {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw 'Timed out stopping the existing FSClip task.'
            }
            Start-Sleep -Milliseconds 100
        }
    }

    # Migrate instances started by the previous foreground installer.
    $sessionId = [System.Diagnostics.Process]::GetCurrentProcess().SessionId
    foreach ($process in @(Get-Process -Name fs-clip -ErrorAction SilentlyContinue)) {
        if ($process.SessionId -eq $sessionId -and $process.Path -eq $binary) {
            Stop-Process -InputObject $process -Force
            if (-not $process.WaitForExit(30000)) {
                throw 'Timed out stopping the existing FSClip process.'
            }
        }
    }

    Move-Item -Path $tempBinaryPath -Destination $binary -Force
    Write-Information "installed ${binary}"

    Add-PathEntryIfMissing $InstallDir
    Write-Information "ensured $InstallDir is in user PATH"

    # Windows command-line quoting doubles trailing backslashes before a quote.
    $quotedWatchDir = '"' + ($WatchDir -replace '(\\*)"', '$1$1\"' -replace '(\\+)$', '$1$1') + '"'
    $action = New-ScheduledTaskAction -Execute $binary -Argument "--watch-dir $quotedWatchDir"
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $userId
    $principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) `
        -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger `
        -Principal $principal -Settings $settings -Force | Out-Null
    Write-Information "registered login task ${taskName}"

    Start-ScheduledTask -TaskName $taskName
    Write-Information 'started FSClip in the background'

} finally {
    Invoke-CleanUp $tempDir
}
