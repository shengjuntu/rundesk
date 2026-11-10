#!/usr/bin/env python3
"""Verify and unpack the reviewed source with the Python standard library only."""
import argparse
import gzip
import hashlib
import lzma
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile
import tempfile

MAX_ARCHIVE = 128 << 20
MAX_CONTENT = 512 << 20
MAX_MEMBERS = 100000
MAX_TAR = MAX_CONTENT + (MAX_MEMBERS * 1024) + 10240
XZ_MEMORY_LIMIT = 64 << 20


def ordinary(path, *, directory=False, missing=False):
    path = Path(os.path.abspath(path))
    if any(item.is_symlink() for item in (path, *path.parents)):
        raise ValueError("Source paths cannot contain symbolic links")
    if missing:
        if path.exists():
            raise FileExistsError("Destination already exists: " + str(path))
    elif not (path.is_dir() if directory else path.is_file()):
        raise ValueError("Expected an ordinary source path: " + str(path))
    return path


def decompress(source, target):
    """Validate the complete compressed stream before reading TAR members."""
    written = 0
    with source.open("rb") as raw, target.open("xb") as output:
        magic = raw.read(6)
        raw.seek(0)
        if magic == b"\xfd7zXZ\x00":
            decoder = lzma.LZMADecompressor(format=lzma.FORMAT_XZ, memlimit=XZ_MEMORY_LIMIT)
            while not decoder.eof:
                data = raw.read(1 << 20) if decoder.needs_input else b""
                if not data and decoder.needs_input:
                    raise ValueError("Truncated XZ source archive")
                block = decoder.decompress(data, max_length=1 << 20)
                written += len(block)
                if written > MAX_TAR:
                    raise ValueError("Source TAR exceeds its size limit")
                output.write(block)
            if decoder.unused_data or raw.read(1):
                raise ValueError("Unexpected data after the XZ source archive")
        elif magic.startswith(b"\x1f\x8b"):
            with gzip.GzipFile(fileobj=raw, mode="rb") as decoder:
                while True:
                    block = decoder.read(1 << 20)
                    if not block:
                        break
                    written += len(block)
                    if written > MAX_TAR:
                        raise ValueError("Source TAR exceeds its size limit")
                    output.write(block)
        else:
            raise ValueError("Source archive must use gzip or XZ compression")


def extract(source, expected, destination, root):
    if not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise ValueError("Invalid expected archive SHA256")
    if not root or PurePosixPath(root).parts != (root,) or root in (".", "..") or "\\" in root:
        raise ValueError("Invalid expected source root")
    source = ordinary(source)
    destination = ordinary(destination, missing=True)
    digest, size = hashlib.sha256(), 0
    with source.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            size += len(block)
            if size > MAX_ARCHIVE:
                raise ValueError("Source archive exceeds 128 MiB")
            digest.update(block)
    if digest.hexdigest() != expected:
        raise ValueError("Source archive SHA256 mismatch")
    new_parents = [path for path in (destination.parent, *destination.parent.parents) if not path.exists()]
    destination.parent.mkdir(parents=True, exist_ok=True)
    ordinary(destination.parent, directory=True)
    for path in new_parents:
        path.chmod(0o755)
    temporary = Path(tempfile.mkdtemp(prefix=".openmontage-source-", dir=destination.parent))
    staging = temporary / "tree"
    staging.mkdir()
    try:
        uncompressed = temporary / "source.tar"
        decompress(source, uncompressed)
        seen, aliases, files, total = set(), {}, 0, 0
        with tarfile.open(uncompressed, "r:") as archive:
            for count, member in enumerate(archive, 1):
                name = member.name[:-1] if member.isdir() and member.name.endswith("/") else member.name
                path = PurePosixPath(name)
                if (count > MAX_MEMBERS or not name or path.is_absolute() or path.as_posix() != name
                        or "\\" in name or any(ord(char) < 32 for char in name)
                        or any(part in (".", "..") for part in path.parts)
                        or not path.parts or path.parts[0] != root or name.casefold() in seen):
                    raise ValueError("Unsafe, duplicate or excessive source archive entry")
                seen.add(name.casefold())
                for end in range(1, len(path.parts) + 1):
                    prefix = str(PurePosixPath(*path.parts[:end]))
                    old = aliases.setdefault(prefix.casefold(), prefix)
                    if old != prefix:
                        raise ValueError("Source archive contains case aliases")
                if member.type not in (tarfile.REGTYPE, tarfile.AREGTYPE, tarfile.DIRTYPE):
                    raise ValueError("Source archive only accepts ordinary files and directories")
                if len(path.parts) == 1:
                    if not member.isdir():
                        raise ValueError("Source root must be a directory")
                    continue
                target = staging.joinpath(*path.parts[1:])
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                    continue
                total += member.size
                if member.size < 0 or total > MAX_CONTENT:
                    raise ValueError("Source archive exceeds 512 MiB unpacked")
                target.parent.mkdir(parents=True, exist_ok=True)
                reader = archive.extractfile(member)
                if reader is None:
                    raise ValueError("Cannot read source archive file")
                remaining = member.size
                with target.open("xb") as output:
                    while remaining:
                        block = reader.read(min(1 << 20, remaining))
                        if not block:
                            raise ValueError("Truncated source archive file")
                        output.write(block)
                        remaining -= len(block)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)
                files += 1
        if not files:
            raise ValueError("Source archive contains no files")
        ordinary(destination, missing=True)
        staging.chmod(0o755)
        for path in staging.rglob("*"):
            if path.is_dir():
                path.chmod(0o755)
        staging.rename(destination)
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--root", required=True)
    args = parser.parse_args()
    try:
        extract(args.archive, args.sha256, args.destination, args.root)
    except (OSError, ValueError, tarfile.TarError, EOFError, lzma.LZMAError) as error:
        parser.exit(2, "OpenMontage source extraction failed: " + str(error) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
