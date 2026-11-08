"""Resolve ordinary recipe and shared files without temporary authoring copies."""
import json
import os
from pathlib import Path
import stat


MAX_FILES = 4000
MAX_BYTES = 128 << 20
GENERATED = {"recipe.json", "profile.lock.json", "SHA256SUMS", "upstream/upstream.json"}


def check_budget(count, total):
    if count > MAX_FILES or total > MAX_BYTES:
        raise ValueError("Recipe exceeds build-context limits")


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("Duplicate recipe JSON key: " + key)
        value[key] = item
    return value


def relative(value):
    if (not isinstance(value, str) or not value or value.startswith("/")
            or any(char in value for char in "\\:")
            or any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value)
            or any(part in ("", ".", "..") for part in value.split("/"))):
        raise ValueError("Unsafe shared input relative path")
    return value


def _ordinary(path, directory=False):
    """Check every ancestor before inspecting a file or descending a tree."""
    path = Path(path).absolute()
    for ancestor in reversed(path.parents):
        mode = ancestor.lstat().st_mode
        if not stat.S_ISDIR(mode):
            raise ValueError("Recipe inputs require ordinary directories: " + str(ancestor))
    mode = path.lstat().st_mode
    if directory and not stat.S_ISDIR(mode):
        raise ValueError("Recipe inputs require an ordinary directory: " + str(path))
    if not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
        raise ValueError("Recipe inputs cannot contain symlinks or special files: " + str(path))
    return mode


def _walk(path):
    mode = _ordinary(path)
    if stat.S_ISREG(mode):
        yield "", path, False
        return
    yield "", path, True
    # scandir never follows a linked directory; inspect each entry first.
    stack = [(path, "")]
    while stack:
        parent, prefix = stack.pop()
        with os.scandir(parent) as entries:
            children = sorted(entries, key=lambda item: item.name)
        for entry in children:
            child = Path(entry.path)
            rel = prefix + entry.name
            relative(rel)
            mode = entry.stat(follow_symlinks=False).st_mode
            if not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise ValueError("Recipe inputs cannot contain symlinks or special files: " + str(child))
            directory = stat.S_ISDIR(mode)
            yield rel, child, directory
            if directory:
                stack.append((child, rel + "/"))


def _generated(target):
    return any(target == name or target.startswith(name + "/") for name in GENERATED)


def context_files(root, name, descriptor=None):
    """Return context-relative file -> source Path; local regular files override.

    Sources are validated and budgeted even when a local file overrides them.
    Authoring variants remain in this view so validators and explicit CLI
    selections use the same resolution rules as ordinary exported inputs.
    """
    root = Path(root).absolute()
    relative(name)
    if "/" in name:
        raise ValueError("Invalid recipe name")
    base = root / name
    _ordinary(base, directory=True)
    if descriptor is None:
        metadata = base / "recipe.json"
        _ordinary(metadata)
        descriptor = json.loads(metadata.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    if not isinstance(descriptor, dict):
        raise ValueError("Recipe descriptor must be an object")
    mappings = descriptor.get("sharedFiles", {})
    if not isinstance(mappings, dict):
        raise ValueError("sharedFiles must be an object of relative paths")
    for target in mappings:
        relative(target)
        relative(mappings[target])
        if _generated(target):
            raise ValueError("Shared inputs cannot supply generated metadata: " + target)
    targets = sorted(mappings)
    for index, target in enumerate(targets):
        if any(other.startswith(target + "/") for other in targets[index + 1:]):
            raise ValueError("Shared input targets overlap: " + target)
    files, shared_dirs = {}, set()
    count = total = 0

    def budget(path):
        nonlocal count, total
        count += 1
        total += path.stat().st_size
        check_budget(count, total)

    if mappings:
        common = root / "common"
        _ordinary(common, directory=True)
        for target in targets:
            source = common / mappings[target]
            # Missing source always fails, including when local would override.
            for child, path, directory in _walk(source):
                dest = target + ("/" + child if child else "")
                if _generated(dest):
                    raise ValueError("Shared inputs cannot supply generated metadata: " + dest)
                if directory:
                    shared_dirs.add(dest)
                else:
                    budget(path)
                    files[dest] = path
    shared_names = set(files)
    for child, path, directory in _walk(base):
        if not child:
            continue
        if directory:
            if child in shared_names:
                raise ValueError("Local directory conflicts with shared file: " + child)
            continue
        budget(path)
        if child in shared_dirs or any(name.startswith(child + "/") for name in shared_names):
            raise ValueError("Local file conflicts with shared directory: " + child)
        if any(child.startswith(name + "/") for name in shared_names):
            raise ValueError("Local file conflicts with shared file: " + child)
        files[child] = path
    return dict(sorted(files.items()))


def context_file(root, name, path, descriptor=None):
    relative(path)
    try:
        return context_files(root, name, descriptor)[path]
    except KeyError:
        raise ValueError("Missing recipe input: " + path) from None
