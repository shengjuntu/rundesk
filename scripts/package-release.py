#!/usr/bin/env python3
"""Stage a self-contained Linux or Windows RunDesk package outside the checkout.

Python is a packaging-time tool only. The resulting launchers use the supplied
native executable, external recipes and configuration without Python or Node.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import tempfile

ROOT = Path(__file__).resolve().parents[1]
PROFILES = ("news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage")


def executable_target(path: Path) -> tuple[str, str]:
    """Identify the target without executing a possibly cross-compiled binary."""
    if not path.is_file():
        raise ValueError(f"Executable is not an ordinary file: {path}")
    with path.open("rb") as stream:
        header = stream.read(64)
        if header[:4] == b"\x7fELF" and len(header) >= 20:
            if header[4] not in (1, 2) or header[5] not in (1, 2) or header[7] not in (0, 3):
                raise ValueError("Only Linux ELF or Windows PE executables can be packaged")
            machine = struct.unpack(("<" if header[5] == 1 else ">") + "H", header[18:20])[0]
            return "linux", {3: "386", 40: "arm", 62: "amd64", 183: "arm64"}.get(machine, f"elf-{machine}")
        if header[:2] == b"MZ" and len(header) == 64:
            offset = struct.unpack("<I", header[60:64])[0]
            if 64 <= offset <= 1 << 20:
                stream.seek(offset)
                pe = stream.read(6)
                if pe[:4] == b"PE\0\0" and len(pe) == 6:
                    machine = struct.unpack("<H", pe[4:])[0]
                    return "windows", {0x14C: "386", 0x8664: "amd64", 0xAA64: "arm64"}.get(machine, f"pe-{machine}")
    raise ValueError("Only Linux ELF or Windows PE executables can be packaged")


def resource_files(directory: Path) -> list[Path]:
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError(f"Missing or linked resource directory: {directory}")
    files = []
    for path in sorted(directory.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"Resource symlinks are not allowed: {path}")
        if path.is_dir():
            continue
        if not stat.S_ISREG(path.stat().st_mode):
            raise ValueError(f"Resource special files are not allowed: {path}")
        if "__pycache__" not in path.relative_to(directory).parts and path.suffix not in (".pyc", ".pyo"):
            files.append(path)
    return files


def source_version(root: Path) -> str:
    text = (root / "internal/app/api_v1.go").read_text(encoding="utf-8")
    match = re.search(r'const Version = "([^"]+)"', text)
    if not match:
        raise ValueError("RunDesk source version was not found")
    return match.group(1)


def stage_release(binary: Path, output: Path, kun: Path | None = None, *, resource_root: Path = ROOT) -> dict:
    resource_root = resource_root.resolve()
    requested = Path(os.path.abspath(output.expanduser()))
    output = requested.resolve()
    if requested == resource_root or resource_root in requested.parents or output == resource_root or resource_root in output.parents:
        raise ValueError("Package output must be outside the source checkout")
    if requested.exists() or requested.is_symlink():
        raise FileExistsError(f"Package destination already exists: {requested}")
    if output.exists() or output.is_symlink():
        raise FileExistsError(f"Package destination already exists: {output}")
    binary = binary.expanduser().resolve(strict=True)
    target = executable_target(binary)
    if kun is not None:
        kun = kun.expanduser().resolve(strict=True)
        if executable_target(kun) != target:
            raise ValueError("Kun and RunDesk must target the same operating system and architecture")

    recipes, config = resource_root / "recipes", resource_root / "config"
    resources = [(directory, resource_files(directory)) for directory in (recipes, config, resource_root / "LICENSES")]
    notices = [(resource_root / name, name) for name in ("LICENSE", "THIRD_PARTY_NOTICES.md")]
    notices.append((resource_root / "third_party/kun-sdk/LICENSE", "LICENSES/kun-sdk-LICENSE.txt"))
    for source, _ in notices:
        if not source.is_file() or any(path.is_symlink() for path in (source, *source.parents) if path != resource_root.parent):
            raise ValueError(f"Missing or linked license/notice: {source}")
    for profile in PROFILES:
        directory = recipes / profile
        if not (directory / "Dockerfile").is_file() or not (directory / "recipe.json").is_file():
            raise ValueError(f"Incomplete canonical recipe: {profile}")
        if (directory / "profile.lock.json").exists():
            raise ValueError(f"Recipe has two editable manifests: {profile}")
        json.loads((directory / "recipe.json").read_text(encoding="utf-8"))
    for name in ("Dockerfile", "seed/config.toml"):
        if not (recipes / "generic" / name).is_file():
            raise ValueError(f"Incomplete generic recipe: {name}")
    for name in ("build-sources.json", "public-images.json"):
        json.loads((config / name).read_text(encoding="utf-8"))
    version = source_version(resource_root)
    platform, arch = target
    suffix = ".exe" if platform == "windows" else ""

    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".rundesk-package-", dir=output.parent) as tmp:
        stage = Path(tmp)
        executables = [(binary, "rundesk" + suffix)]
        if kun:
            executables.append((kun, "kun" + suffix))
        for source, name in executables:
            destination = stage / name
            shutil.copyfile(source, destination)
            destination.chmod(0o755)
        for directory, files in resources:
            for source in files:
                destination = stage / directory.name / source.relative_to(directory)
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, destination)
        for source, name in notices:
            destination = stage / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            if destination.exists():
                if destination.read_bytes() != source.read_bytes():
                    raise ValueError(f"Conflicting license/notice destination: {name}")
            else:
                shutil.copy2(source, destination)
        if platform == "linux":
            launcher = stage / "start-rundesk.sh"
            launcher.write_text('#!/bin/sh\nset -eu\nrun_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexec "$run_dir/rundesk" --data "$run_dir/data" "$@"\n', encoding="utf-8", newline="\n")
            launcher.chmod(0o755)
        else:
            (stage / "start-rundesk.cmd").write_bytes(b'@echo off\r\nsetlocal\r\n"%~dp0rundesk.exe" --data "%~dp0data" %*\r\nexit /b %errorlevel%\r\n')
        (stage / "README.txt").write_text(
            f"RunDesk portable package / RunDesk 可运行包\nSource version / 源码版本: {version}\nTarget / 目标平台: {platform}/{arch}\n\n"
            "Launch with start-rundesk.sh (Linux) or start-rundesk.cmd (Windows).\n"
            "使用随包启动文件运行；启动文件以自身目录为准，不要求特定工作目录。\n"
            "No Go, Python, Node.js or npm is required to start this package.\n"
            "启动可运行包无需 Go、Python、Node.js 或 npm。Docker Engine is required only for Docker features.\n"
            "Keep recipes/ and config/ beside the executable. The server reads external recipe files.\n"
            "保留程序旁的 recipes/ 和 config/，构建配方仍是可编辑的外置文件。\n"
            "License and attribution files are included in LICENSE, THIRD_PARTY_NOTICES.md and LICENSES/.\n"
            "许可证与来源说明保存在 LICENSE、THIRD_PARTY_NOTICES.md 和 LICENSES/。\n"
            "New installs keep mutable state in data/. No existing data or credentials are included.\n"
            "新安装的数据写入 data/；本包不含已有数据或凭据。\n"
            "For upgrades pass --data /absolute/path/to/existing-data to the launcher.\n"
            "升级时请通过 --data 明确指定原数据目录，避免改用新空目录。\n"
            "Check rundesk --version before deployment; --binary must match the source checkout used to prepare this package.\n"
            "部署前核对 rundesk --version；打包所选程序须与打包源码版本一致。\n"
            + ("Kun is included beside RunDesk.\n已包含独立 Kun 程序。\n" if kun else "Kun is optional and is not included. Supply it beside RunDesk or configure it during setup.\n未包含可选的独立 Kun；可放到 RunDesk 旁，或在初始化中配置。\n"),
            encoding="utf-8", newline="\n")
        entries = {}
        for path in sorted(stage.rglob("*")):
            if path.is_file():
                payload = path.read_bytes()
                entries[path.relative_to(stage).as_posix()] = {"bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest()}
        manifest = {"schemaVersion": 1, "sourceVersion": version, "platform": platform, "arch": arch, "kunIncluded": kun is not None, "files": entries}
        (stage / "package-manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        # Claim the destination exclusively after all inputs have been copied.
        # A failed commit leaves an explicit marker rather than deleting files.
        output.mkdir()
        marker = output / "PACKAGE-INCOMPLETE.txt"
        marker.write_text("Package staging did not finish. Do not deploy this directory.\n", encoding="utf-8")
        for child in stage.iterdir():
            destination = output / child.name
            if destination.exists() or destination.is_symlink():
                raise FileExistsError(f"Package destination changed during staging: {destination}")
            child.rename(destination)
        marker.unlink()
    return {"output": str(output), "platform": platform, "arch": arch, "files": len(entries), "kunIncluded": kun is not None}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path, help="Compiled RunDesk binary matching this source checkout")
    parser.add_argument("--output-dir", required=True, type=Path, help="New destination outside the checkout; never overwritten")
    parser.add_argument("--kun", type=Path, help="Optional matching Kun executable")
    args = parser.parse_args()
    try:
        print(json.dumps(stage_release(args.binary, args.output_dir, args.kun), ensure_ascii=False))
    except (OSError, ValueError) as error:
        parser.exit(1, f"Package creation failed: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
