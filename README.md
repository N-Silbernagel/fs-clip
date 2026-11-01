![FSClip headline image](assets/readme.png)

A tiny filesystem “clipboard bridge” that watches a directory and automatically places any dropped file's contents on your system clipboard — then cleans up after itself.


FSClip ("FileSystem Clipboard") gives you a friction‑free way to get data into the clipboard when the producing application can only save to disk. Drop (or programmatically write) a file into a special watched directory (default: ~/copy-to-clipboard/) and the file's contents are copied to your clipboard and the file is removed. Ideal for export‑only GUI apps.

## Why?

Some tools can only emit output as files. If your next step is to paste that output into chat, an editor, an email, or a web form, manually opening the file and copying its contents gets repetitive fast.

## Key Features
* Zero friction workflow – Just write or drag files into the watched directory
* Auto copy & prune – Contents are copied; the original file is deleted
* Text & Image modes - Copies text files and PNG, JPEG, GIF, and WebP images. Images become PNG on the clipboard; animated images use their first frame.
* Minimal footprint – Single small executable

## Installation
### macOS / Linux

```bash
sh -c 'installer=$(curl -fsSL https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.sh) && sh -c "$installer"'
```

On Linux, the installer sets up a systemd user service. Requires a graphical desktop session.

### Windows

Run in PowerShell:

```powershell
& ([scriptblock]::Create((Invoke-RestMethod 'https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.ps1')))
```

The installer adds FSClip to your user PATH and starts it in the background. A scheduled task starts it automatically when you log in, without administrator privileges.

## Configuration and management

Installation uses `~/copy-to-clipboard` by default. The installer saves a local
management script, so configuration, status, and uninstall work without a network
connection. Reinstalling preserves the saved watched folder and installation path.

### macOS

```bash
manager="$HOME/Library/Application Support/fs-clip/manage.sh"
sh "$manager" configure --watch-dir '/path/to/clipboard'
sh "$manager" status
# When you want to remove FSClip:
sh "$manager" uninstall
```

### Linux

```bash
manager="${XDG_CONFIG_HOME:-$HOME/.config}/fs-clip/manage.sh"
sh "$manager" configure --watch-dir '/path/to/clipboard'
sh "$manager" status
# When you want to remove FSClip:
sh "$manager" uninstall
```

### Windows

```powershell
$manager = Join-Path $env:LOCALAPPDATA 'fs-clip\manage.ps1'
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$manager" Configure -WatchDir 'C:\Users\YourName\Clipboard'
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$manager" Status
# When you want to remove FSClip:
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$manager" Uninstall
```

The Windows commands set execution policy for that process only, so local scripts
can run without changing your saved PowerShell execution policy.

Changing the watched folder restarts the background worker. If activation fails,
the manager attempts to restore the previous configuration. Status reports the OS
service or task state; it does not test clipboard access.

Uninstall stops FSClip, removes automatic startup and the executable, and on
Windows removes the user PATH entry only if the manager added it. Watched files,
preferences, logs, and the management script are retained. Repeating uninstall is
safe. Installers created before saved preferences were introduced cannot preserve
a custom folder automatically; configure the folder again after the first upgrade.

The installers still accept explicit binary and watch-directory overrides for
existing automation (`-b` / `-w` on Unix, `-InstallDir` / `-WatchDir` on Windows).
Prefer the local management script for subsequent configuration changes. Uninstall
before moving an existing installation to a different binary directory.

## How It Works

At its core FSClip watches a directory for new files
1. Detect new file
2. Read file contents
3. Write to system clipboard
4. Delete the source file after a successful copy

Files that cannot be read or copied, and files with unsupported content types, are kept in the watched directory.

## Roadmap
* Evaluate GoReleaser's free Homebrew cask, Scoop, and `.deb`/`.rpm` support, including service setup and uninstall cleanup.
* Tests + CI badges

## Building from source

Requires Go 1.27.3 or newer and a graphical desktop session for clipboard access.

```bash
make build
go test -race ./...
go vet ./...
```

Race-enabled tests require a C compiler.

Releases wait for the shared test workflow, then one Ubuntu job runs GoReleaser
to build and publish all six release targets. Archives include the platform
management script and, on Unix, service templates; they share one checksum manifest. The archive checksum covers the management script
as well as the executable.

## Clipboard integration tests

GitHub Actions cross-compiles all six release targets, checks
GoReleaser packaging in snapshot mode without publishing, and runs unit
tests, static checks, and clipboard integration tests on Linux AMD64, macOS ARM64,
and Windows AMD64. Installer tests build the application and verify its release build settings.
The test harness retains race detection. Each integration job first checks native clipboard
write/read access, then starts the built application and verifies:

- Unicode text is copied completely and its source is removed.
- Complete contents of a file written in chunks reach the clipboard.
- Unsupported files retain their contents and leave the clipboard unchanged.
- PNG, JPEG, GIF, and WebP images become valid PNG clipboard data with matching dimensions.
- A subsequent file at a previously used path replaces the clipboard contents.

Clipboard access failures fail the job rather than skipping the tests. Image
assertions use Go's PNG decoder to validate image data read through native clipboard tools.

Unit tests verify the debounce timer with Go's deterministic test clock. FSClip
currently infers readiness from 100 ms without write events; a producer that pauses
longer can be processed before it finishes. To publish a completed file explicitly,
write it outside the watched directory and then move it into that directory on
the same filesystem.

The same jobs exercise the installers against a local release server: latest and
explicit tags, repeat installs, custom paths, and invalid or missing checksums.
Management coverage checks offline configuration and status, saved settings across
updates, and uninstall without deleting watched files.
They then run the clipboard suite against the installed executable. On macOS and
Linux, service-manager commands are recorded and generated service files are checked.
On Windows, tests register the real login task and verify its settings, repeat
installs, and text and image clipboard access. Tests remove the
task afterward. These tests do not simulate a fresh login; macOS and Windows
installer coverage is verified when their CI jobs pass.

## Contributing

PRs, issues, and discussions welcome!

## Security & Privacy

FSClip reads files you intentionally place into the watched directory. It does not traverse elsewhere.

## Acknowledgments

FSClip builds on the excellent work of these open-source projects:

- **[golang-design/clipboard](https://github.com/golang-design/clipboard)** – cross-platform clipboard support for Go
- **[fsnotify/fsnotify](https://github.com/fsnotify/fsnotify)** – filesystem event notifications for Go

## License

Distributed under the MIT License. See LICENSE.txt for details.
