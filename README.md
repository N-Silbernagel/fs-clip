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
sh -c "$(curl -fsLS https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.sh)"
```

On Linux, the installer sets up a systemd user service. Requires a graphical desktop session.

### Windows

Run in PowerShell:

```powershell
& ([scriptblock]::Create((Invoke-RestMethod 'https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.ps1')))
```

The installer adds FSClip to your user PATH and starts it in the foreground.

## Configuration

Use 
```bash
sh -c "$(curl -fsLS https://github.com/N-Silbernagel/fs-clip/releases/latest/download/install.sh)" -- -w YOUR_DIRECTORY
``` 

to configure a different directory to watch during installation (you can just reinstall fs-clip if it is already installed)

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
to build and publish all five release targets. Archives retain their platform-specific
service files and share one checksum manifest.

## Clipboard integration tests

GitHub Actions cross-compiles all five release targets, checks
GoReleaser packaging in snapshot mode without publishing, and runs unit
tests, static checks, and clipboard integration tests on Ubuntu, macOS, and
Windows. Installer tests build the application and verify its release build settings.
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
They then run the clipboard suite against the installed executable. Service-manager
commands are recorded and generated service files are checked; these tests do not
verify service startup at login. macOS and Windows installer coverage is verified
when their CI jobs pass.

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
