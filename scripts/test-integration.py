#!/usr/bin/env python3
"""Exercise a built fs-clip binary against the desktop clipboard."""

import argparse
import base64
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


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
