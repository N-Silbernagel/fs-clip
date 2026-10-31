#!/usr/bin/env python3
"""Exercise a built fs-clip binary against the desktop clipboard."""

import argparse
import base64
import io
import os
import re
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

from PIL import Image


def run(*args, data=None):
    return subprocess.check_output(args, input=data, timeout=10)


def powershell(script):
    encoded = base64.b64encode(script.encode('utf-16-le')).decode('ascii')
    return run('powershell.exe', '-NoProfile', '-NonInteractive', '-STA',
               '-EncodedCommand', encoded)


def write_clipboard(text):
    if sys.platform == 'darwin':
        run('pbcopy', data=text.encode())
    elif sys.platform == 'win32':
        encoded = base64.b64encode(text.encode()).decode('ascii')
        powershell("$ErrorActionPreference='Stop'; Set-Clipboard -Value "
                   f"([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{encoded}')))")
    else:
        # xclip forks a selection owner; don't let it inherit a captured pipe.
        subprocess.run([XCLIP, '-selection', 'clipboard'], input=text.encode(),
                       stdout=subprocess.DEVNULL, check=True, timeout=10)


def read_clipboard():
    if sys.platform == 'darwin':
        return run('pbpaste').decode('utf-8')
    if sys.platform == 'win32':
        result = powershell("$ErrorActionPreference='Stop'; "
                            "$text=Get-Clipboard -Raw; "
                            "[Console]::Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($text)))")
        return base64.b64decode(result).decode('utf-8')
    return run(XCLIP, '-selection', 'clipboard', '-o').decode('utf-8')


def read_clipboard_image():
    if sys.platform == 'darwin':
        result = run('osascript', '-e', 'get the clipboard as «class PNGf»').decode('utf-8')
        match = re.fullmatch(r'«data PNGf([0-9a-fA-F\s]+)»\s*', result)
        if match is None:
            raise AssertionError(f'Unexpected AppleScript image response: {result[:100]!r}')
        data = bytes.fromhex(match.group(1))
    elif sys.platform == 'win32':
        result = powershell("$ErrorActionPreference='Stop'; "
                            "Add-Type -AssemblyName System.Windows.Forms; "
                            "$image=[Windows.Forms.Clipboard]::GetImage(); "
                            "if ($null -eq $image) { throw 'Clipboard has no image' }; "
                            "$stream=New-Object IO.MemoryStream; "
                            "try { "
                            "$image.Save($stream, [Drawing.Imaging.ImageFormat]::Png); "
                            "[Console]::Write([Convert]::ToBase64String($stream.ToArray())) "
                            "} finally { $stream.Dispose(); $image.Dispose() }")
        data = base64.b64decode(result)
    else:
        data = run(XCLIP, '-selection', 'clipboard', '-o', '-target', 'image/png')
    with Image.open(io.BytesIO(data)) as image:
        return image.convert('RGB')


def wait_for(description, condition, process=None):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if process is not None and process.poll() is not None:
            raise AssertionError(f'fs-clip exited with status {process.returncode}')
        if condition():
            return
        time.sleep(0.02)
    raise AssertionError(f'Timed out waiting for {description}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()

    sentinel = 'fs-clip clipboard smoke check'
    write_clipboard(sentinel)
    wait_for('native clipboard write/read', lambda: read_clipboard() == sentinel)
    print('PASS: native clipboard write/read', flush=True)

    with tempfile.TemporaryDirectory(prefix='fs-clip-integration-') as directory:
        root = Path(directory)
        watched = root / 'watched'
        watched.mkdir()
        log = root / 'fs-clip.log'
        with log.open('wb') as output:
            process = subprocess.Popen([str(binary), '-watch-dir', str(watched),
                                        '-log-level', 'DEBUG'], stdout=output, stderr=output)
            try:
                wait_for('watcher readiness',
                         lambda: 'Watching directory' in log.read_text(), process)
                source = watched / 'plain.txt'
                text = 'Hello clipboard — Grüße, 世界!\nSecond line.\n'
                source.write_text(text, encoding='utf-8', newline='')
                wait_for('plain text source removal', lambda: not source.exists(), process)
                wait_for('Unicode text on clipboard', lambda: read_clipboard() == text, process)
                print('PASS: Unicode text copied and source removed', flush=True)

                source = watched / 'chunked.txt'
                chunks = [f'chunk {i}: complete clipboard content\n' for i in range(15)]
                with source.open('w', encoding='utf-8', newline='') as stream:
                    for chunk in chunks:
                        stream.write(chunk)
                        stream.flush()
                        os.fsync(stream.fileno())
                        if not source.exists():
                            raise AssertionError('Source was deleted before writing finished')
                        time.sleep(0.02)
                wait_for('source removal', lambda: not source.exists(), process)
                expected = ''.join(chunks)
                wait_for('complete clipboard contents', lambda: read_clipboard() == expected, process)
                print('PASS: chunked file copied completely and source removed', flush=True)

                unsupported = watched / 'unsupported.bin'
                content = bytes([0, 1, 2, 3])
                unsupported.write_bytes(content)
                wait_for('unsupported file rejection',
                         lambda: 'unsupported content type' in log.read_text(), process)
                if unsupported.read_bytes() != content:
                    raise AssertionError('Unsupported source contents changed')
                if read_clipboard() != expected:
                    raise AssertionError('Unsupported file changed the clipboard')
                print('PASS: unsupported file preserved without changing clipboard', flush=True)

                source = watched / 'image.png'
                expected_image = Image.new('RGB', (2, 2))
                expected_image.putdata([(255, 0, 0), (0, 255, 0), (0, 0, 255), (255, 255, 255)])
                expected_image.save(source)
                wait_for('image source removal', lambda: not source.exists(), process)
                image = read_clipboard_image()
                if image.size != expected_image.size or image.tobytes() != expected_image.tobytes():
                    raise AssertionError('Clipboard image dimensions or pixels do not match source')
                print('PASS: PNG image pixels copied and source removed', flush=True)

                # Reuse a previously processed path to exercise timer cleanup.
                source = watched / 'plain.txt'
                text = 'Next copy replaces the image clipboard contents.'
                source.write_text(text, encoding='utf-8')
                wait_for('replacement source removal', lambda: not source.exists(), process)
                wait_for('replacement clipboard contents', lambda: read_clipboard() == text, process)
                print('PASS: subsequent file at reused path replaces clipboard contents', flush=True)
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=10)
                diagnostics = log.read_text()
                print(diagnostics, end='', flush=True)
                if 'WARNING: DATA RACE' in diagnostics:
                    raise AssertionError('Go race detector reported a data race')


if __name__ == '__main__':
    XCLIP = shutil.which('xclip')
    main()
