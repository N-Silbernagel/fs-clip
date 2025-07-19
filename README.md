# FSClip

A tiny filesystem “clipboard bridge” that watches a directory and automatically places any dropped file's contents on your system clipboard — then cleans up after itself.


FSClip ("FileSystem Clipboard") gives you a friction‑free way to get data into the clipboard when the producing application can only save to disk. Drop (or programmatically write) a file into a special watched folder (default: ~/copy-to-clipboard/) and the file's contents are copied to your clipboard and the file is removed. Ideal for export‑only GUI apps.

## Why?

Some tools can only emit output as files. If your next step is to paste that output into chat, an editor, an email, or a web form, manually opening the file and copying its contents gets repetitive fast.

## Key Features
* Zero friction workflow – Just write or drag files into the watched directory
* Auto copy & prune – Contents are copied; the original file is deleted
* Text & Image modes - Either copies files as text or as image
* Minimal footprint – Single small executable

## Installation

```bash
git clone git@github.com:N-Silbernagel/fs-clip.git
```

```bash
cd fs-clip
```

```bash
make install
```


## Configuration

Use make install WATCH_DIR=YOUR_DIRECTORY to configure a directory which should be watched

## How It Works

At its core FSClip watches a directory for new files
1. Detect new file
2. Read file contents
3. Write to system clipboard
4. Delete the source file

## Roadmap
* Cross-platform packaged binaries
* Ignore patterns (e.g. .DS_Store)
* Tests + CI badges
* Release automation (GitHub Actions)
* Add configuration for file deletion

## Contributing

PRs, issues, and discussions welcome!

## Security & Privacy

FSClip reads files you intentionally place into the watched directory. It does not traverse elsewhere.

## License

Distributed under the MIT License. See LICENSE for details.

## Need to Customize Further?

Let me know which implementation language, packaging method, or features you actually have and I can tailor the README precisely (removing placeholders and planned items).
