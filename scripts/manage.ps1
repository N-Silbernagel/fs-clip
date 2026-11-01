# Manages script-installed FSClip. The worker itself has no installation state.
[CmdletBinding()]
param (
    [Parameter(Position = 0, Mandatory = $true)]
    [ValidateSet('Setup', 'Stop', 'Configure', 'Status', 'Uninstall')]
    [string]$Command,
    [string]$WatchDir,
    [string]$InstallDir,
    [switch]$InstallLockHeld
)

Set-StrictMode -Version 3.0
$ErrorActionPreference = 'Stop'
$InformationPreference = 'Continue'
$stateDir = Join-Path $env:LOCALAPPDATA 'fs-clip'
$settingsPath = Join-Path $stateDir 'settings.json'
$userId = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$taskName = "dev.nils-silbernagel.fs-clip-${userId}"

function Add-PathEntryIfMissing ([string]$entry) {
    $userPath = [System.Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($userPath) {
        $parts = @($userPath -split ';' | Where-Object { $_ })
    }
    if (-not ($parts -contains $entry)) {
        $newParts = @($parts + $entry)
        $newUserPath = ($newParts -join ';')
        [System.Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    }

    $processParts = @()
    if ($env:Path) {
        $processParts = @($env:Path -split ';' | Where-Object { $_ })
    }
    if (-not ($processParts -contains $entry)) {
        $env:Path = (($processParts + $entry) -join ';')
    }
}

function Stop-Worker {
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
}

function Start-Worker {
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
}

function Save-Settings {
    $settings | ConvertTo-Json | Set-Content -LiteralPath "$settingsPath.new" -Encoding UTF8
    Move-Item -LiteralPath "$settingsPath.new" -Destination $settingsPath -Force
}

[System.IO.Directory]::CreateDirectory($stateDir) | Out-Null
# A file handle releases the lock even if PowerShell is terminated.
$lock = $null
if (-not $InstallLockHeld) {
    $lock = [System.IO.File]::Open((Join-Path $stateDir 'management.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
}
try {
    $settings = $null
    if (Test-Path -LiteralPath $settingsPath) {
        $settings = Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
    }
    if ($Command -eq 'Stop') {
        if (-not $InstallDir) { throw 'Stop requires -InstallDir.' }
        $binary = Join-Path $InstallDir 'fs-clip.exe'
        Stop-Worker
        return
    }
    if ($Command -eq 'Setup') {
        if (-not $InstallDir -or -not $WatchDir) { throw 'Setup requires installation and watch directories.' }
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $pathAdded = -not (@($userPath -split ';') -contains $InstallDir)
        if ($settings -and $settings.InstallDir -eq $InstallDir) {
            $pathAdded = $settings.PathAdded
        }
        $settings = [pscustomobject]@{ InstallDir = $InstallDir; WatchDir = $WatchDir; PathAdded = $pathAdded; Installed = $true }
        $binary = Join-Path $InstallDir 'fs-clip.exe'
        Stop-Worker
        [System.IO.Directory]::CreateDirectory($WatchDir) | Out-Null
        Start-Worker
        Add-PathEntryIfMissing $InstallDir
        Save-Settings
        return
    }
    if (-not $settings -or -not $settings.Installed) {
        if ($Command -eq 'Uninstall') { Write-Information 'FSClip is already uninstalled.'; return }
        throw 'No script-managed installation found. Run the installer first.'
    }
    $InstallDir = $settings.InstallDir
    $binary = Join-Path $InstallDir 'fs-clip.exe'
    switch ($Command) {
        'Status' {
            Write-Output "Executable: $binary"
            Write-Output "Watching: $($settings.WatchDir)"
            Get-ScheduledTask -TaskName $taskName | Format-List TaskName, State
        }
        'Configure' {
            if (-not $WatchDir) { throw 'Configure requires -WatchDir.' }
            $WatchDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($WatchDir)
            [System.IO.Directory]::CreateDirectory($WatchDir) | Out-Null
            $previous = $settings.WatchDir
            Stop-Worker
            try {
                Start-Worker
                $settings.WatchDir = $WatchDir
                Save-Settings
            } catch {
                $failure = $_
                $WatchDir = $previous
                Stop-Worker
                Start-Worker
                throw $failure
            }
        }
        'Uninstall' {
            Stop-Worker
            if (Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\' -and $_.TaskName -eq $taskName }) {
                Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
            }
            if (Test-Path -LiteralPath $binary) { Remove-Item -LiteralPath $binary -Force }
            if ($settings.PathAdded) {
                $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
                $remaining = @($userPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';'
                [Environment]::SetEnvironmentVariable('Path', $remaining, 'User')
                $env:Path = @($env:Path -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';'
            }
            $settings.Installed = $false
            Save-Settings
            Write-Information 'FSClip uninstalled. Watched files, preferences, and logs were preserved.'
        }
    }
} finally {
    if ($lock) { $lock.Dispose() }
}
