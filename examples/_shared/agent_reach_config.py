#!/usr/bin/env python3
"""Seed only a missing mcporter home config. No network, installs or credentials."""
import fcntl
import json
import os
from pathlib import Path
import tempfile


def initialize(home):
    directory = Path(home) / '.mcporter'
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (directory / '.rundesk-init.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        for name in ('mcporter.json', 'mcporter.jsonc'):
            path = directory / name
            if path.exists() or path.is_symlink():
                return path  # Respect custom/disabled servers, credentials and JSONC.
        path = directory / 'mcporter.json'
        fd, temporary = tempfile.mkstemp(prefix='.seed-', dir=directory)
        try:
            with os.fdopen(fd, 'w') as stream:
                json.dump({'mcpServers': {'exa': {'baseUrl': 'https://mcp.exa.ai/mcp'}}}, stream, indent=2)
                stream.write('\n')
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, path)
        finally:
            Path(temporary).unlink(missing_ok=True)
        return path


if __name__ == '__main__':
    print(initialize(os.environ['HOME']))
