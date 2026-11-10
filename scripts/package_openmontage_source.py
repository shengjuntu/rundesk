#!/usr/bin/env python3
"""Repack the reviewed local OpenMontage archive without three demo videos.

No network access is performed. Every retained path, byte and executable bit is
preserved; reproducible gzip/tar metadata is independent of local file times.
"""
from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import stat
import tarfile
import tempfile

REPOSITORY = "https://github.com/calesthio/OpenMontage"
COMMIT = "c8066452325cb57c8fc1ba8c2a71b6e8842e6b65"
SOURCE_URL = "https://codeload.github.com/calesthio/OpenMontage/tar.gz/" + COMMIT
SOURCE_SHA256 = "dff7cd1cdf2cb0522398e00b3f0b75f32336b695a8be34ac8e6b6913055aab25"
PREFIX = "OpenMontage-" + COMMIT
MAX_ARCHIVE = 128 << 20
MAX_CONTENT = 512 << 20
OMITTED = (
    "assets/signal-from-tomorrow-demo.mp4",
    ".agents/skills/hyperframes-animation/examples/assets/hyperframes-showcase-hypecard.mp4",
    ".agents/skills/hyperframes-animation/examples/assets/background-tech-data-flow.mp4",
)
OMISSION_REASON = "Upstream demonstration video; no retained UTF-8 source references its basename."


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def ordinary_path(path: Path, *, existing: bool) -> Path:
    path = Path(os.path.abspath(path.expanduser()))
    for item in (path, *path.parents):
        if item.is_symlink():
            raise ValueError("Symbolic links are not accepted: " + str(item))
    if existing and (not path.is_file() or not stat.S_ISREG(path.stat().st_mode)):
        raise ValueError("Source must be an ordinary local file")
    if not existing and path.exists():
        raise FileExistsError("Destination already exists: " + str(path))
    return path


def source_files(payload: bytes) -> dict[str, tuple[bytes, int]]:
    """Validate the fixed archive root and preserve regular files only."""
    files: dict[str, tuple[bytes, int]] = {}
    seen: set[str] = set()
    total = 0
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as archive:
        members = archive.getmembers()
        if not members or len(members) > 100000:
            raise ValueError("Invalid or excessive upstream archive entries")
        for member in members:
            name = member.name.rstrip("/")
            path = PurePosixPath(name)
            if (not name or path.is_absolute() or "\\" in name or ".." in path.parts
                    or path.as_posix() != name or path.parts[0] != PREFIX):
                raise ValueError("Unsafe or unexpected upstream path: " + name)
            if name.casefold() in seen:
                raise ValueError("Duplicate upstream archive path: " + name)
            seen.add(name.casefold())
            if member.isdir():
                continue
            if not member.isfile() or len(path.parts) < 2:
                raise ValueError("Only directories and regular upstream files are accepted")
            total += member.size
            if member.size < 0 or total > MAX_CONTENT:
                raise ValueError("Upstream source exceeds its content limit")
            relative = str(PurePosixPath(*path.parts[1:]))
            reader = archive.extractfile(member)
            if reader is None:
                raise ValueError("Cannot read upstream source: " + relative)
            data = reader.read(member.size + 1)
            if len(data) != member.size:
                raise ValueError("Upstream source length changed: " + relative)
            files[relative] = (data, 0o755 if member.mode & 0o111 else 0o644)
    missing = set(OMITTED) - files.keys()
    if missing:
        raise ValueError("Reviewed demo files are missing: " + ", ".join(sorted(missing)))
    # This check documents the reviewed omission boundary. A referenced sample
    # cannot be removed even if this tool is explicitly adapted to another pin.
    names = tuple(PurePosixPath(name).name for name in OMITTED)
    for relative, (data, _) in files.items():
        if relative in OMITTED:
            continue
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            continue
        if any(name in text for name in names):
            raise ValueError("Retained source references a removed demo: " + relative)
    return files


def write_bundle(path: Path, files: dict[str, tuple[bytes, int]]) -> None:
    with path.open("wb") as output:
        with gzip.GzipFile(filename="", mode="wb", fileobj=output, compresslevel=9, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w|", format=tarfile.PAX_FORMAT) as archive:
                for relative in sorted(files):
                    if relative in OMITTED:
                        continue
                    content, mode = files[relative]
                    member = tarfile.TarInfo(PREFIX + "/" + relative)
                    member.mode = mode
                    member.uid = member.gid = member.mtime = 0
                    member.uname = member.gname = ""
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))


def package(source: Path, output: Path, manifest: Path) -> dict:
    source = ordinary_path(source, existing=True)
    output = ordinary_path(output, existing=False)
    manifest = ordinary_path(manifest, existing=False)
    if len({source, output, manifest}) != 3:
        raise ValueError("Source, bundle and manifest paths must be distinct")
    with source.open("rb") as stream:
        payload = stream.read(MAX_ARCHIVE + 1)
    if len(payload) > MAX_ARCHIVE or digest(payload) != SOURCE_SHA256:
        raise ValueError("Original OpenMontage archive SHA256 mismatch; expected pin was not changed")
    files = source_files(payload)
    output.parent.mkdir(parents=True, exist_ok=True)
    manifest.parent.mkdir(parents=True, exist_ok=True)
    bundle_fd, bundle_name = tempfile.mkstemp(prefix=".openmontage-bundle-", dir=output.parent)
    os.close(bundle_fd)
    record_fd, record_name = tempfile.mkstemp(prefix=".openmontage-record-", dir=manifest.parent)
    os.close(record_fd)
    temporary_bundle, temporary_record = Path(bundle_name), Path(record_name)
    try:
        write_bundle(temporary_bundle, files)
        content = temporary_bundle.read_bytes()
        kept = {name: {"sha256": digest(data), "bytes": len(data), "mode": mode}
                for name, (data, mode) in sorted(files.items()) if name not in OMITTED}
        record = {
            "schemaVersion": 1,
            "scope": "runtime-without-demo-media",
            "repository": REPOSITORY,
            "commit": COMMIT,
            "sourceURL": SOURCE_URL,
            "sourceSHA256": SOURCE_SHA256,
            "sourceBytes": len(payload),
            "sourceFiles": len(files),
            "sourceUncompressedBytes": sum(len(data) for data, _ in files.values()),
            "bundlePath": "vendor/source.tar.gz",
            "bundleSHA256": digest(content),
            "bundleBytes": len(content),
            "bundleFiles": len(kept),
            "bundleUncompressedBytes": sum(item["bytes"] for item in kept.values()),
            "omittedFiles": [{"path": name, "bytes": len(files[name][0]), "sha256": digest(files[name][0]),
                              "reason": OMISSION_REASON} for name in OMITTED],
            "preservedFiles": kept,
        }
        temporary_record.write_text(json.dumps(record, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        temporary_bundle.chmod(0o644)
        temporary_record.chmod(0o644)
        # Refuse to overwrite a file that appeared while packaging was running.
        ordinary_path(output, existing=False)
        ordinary_path(manifest, existing=False)
        os.replace(temporary_bundle, output)
        try:
            os.replace(temporary_record, manifest)
        except BaseException:
            output.unlink()
            raise
        return record
    finally:
        temporary_bundle.unlink(missing_ok=True)
        temporary_record.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True, help="Original local codeload tar.gz; exact SHA256 is required")
    parser.add_argument("--output", type=Path, required=True, help="New bundle filename, usually vendor/source.tar.gz")
    parser.add_argument("--manifest", type=Path, required=True, help="New provenance filename, usually upstream/source-bundle.json")
    args = parser.parse_args()
    try:
        record = package(args.source, args.output, args.manifest)
    except (OSError, ValueError, tarfile.TarError, EOFError) as error:
        parser.exit(2, str(error) + "\n")
    print(json.dumps({key: record[key] for key in ("bundleBytes", "bundleFiles", "bundleSHA256", "sourceBytes")}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
