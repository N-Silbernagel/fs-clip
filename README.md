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

Installation uses `~/copy-to-clipboard` by default. Change the folder after
installation:

```bash
fs-clip configure --watch-dir '/path/to/clipboard'
```

On Windows, use a Windows path, for example:

```powershell
fs-clip configure --watch-dir 'C:\Users\YourName\Clipboard'
```

Check whether FSClip is running or uninstall it:

```bash
fs-clip status
fs-clip uninstall
```

Open a new terminal after installation for Windows PATH changes to take effect.
On macOS and Linux, if `fs-clip` is not on your PATH, use `~/.local/bin/fs-clip`
instead, or add `~/.local/bin` to your PATH. The installer prints the executable's
full path when using a custom installation directory.

Reinstalling preserves the watched folder and installation path. Changing the
watched folder restarts FSClip; a failed change restores the previous settings.
Configuration and uninstall work offline.

Uninstall stops FSClip and removes automatic startup and the installed executable.
Watched files, preferences, and logs are preserved. On Windows, the executable
removes itself shortly after the uninstall command exits.

For a custom binary directory, use `-b DIRECTORY` with the Unix installer or
`-InstallDir DIRECTORY` with the PowerShell installer. Uninstall before moving an
existing installation to another directory.

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
CGO_ENABLED=0 go build .
go test -race ./...
go vet ./...
```

Race-enabled tests require a C compiler.

Releases wait for the shared test workflow, then one Ubuntu job runs GoReleaser
to build and publish all six release targets. Each archive contains the executable,
README, and license; releases include a shared checksum manifest.

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
updates, recovery from failed upgrades and configuration changes, and uninstall
without deleting watched files.
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
