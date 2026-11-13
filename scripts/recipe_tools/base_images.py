"""Build isolated, content-keyed common bases for the standalone recipe CLI.

The server freezes its own build jobs. This CLI pins parent and dependency image
IDs for one invocation, with a small persistent ID cache across invocations.
Dockerfile FROM receives a checked, content-named local tag: a bare content ID
is not a resolvable image reference in BuildKit. No mutable cache tag is trusted.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

import shared_inputs


class BaseImageError(ValueError):
    pass


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def _regular(path):
    # shared_inputs also rejects symbolic directory ancestors.
    if not path.is_file() or path.is_symlink():
        raise BaseImageError("Missing or unsafe base input: " + str(path))
    shared_inputs._ordinary(path)
    return path.read_bytes()


def _instructions(text):
    return "\n".join(line for line in text.splitlines() if not line.lstrip().startswith("#"))


def _from_contract(text):
    instructions = _instructions(text)
    lines = re.findall(r"(?mi)^\s*FROM\s+[^\r\n]+$", instructions)
    return len(lines) == 1 and re.fullmatch(r"\s*FROM\s+\$\{RD_BASE_IMAGE\}(?:\s+AS\s+[a-z0-9_-]+)?\s*", lines[0], re.IGNORECASE) is not None


def _uses(text, name):
    instructions = "\n".join(line for line in _instructions(text).splitlines() if not re.match(r"^\s*ARG\s", line, re.IGNORECASE))
    return re.search(r"\$(?:" + re.escape(name) + r"\b|\{" + re.escape(name) + r"(?:\}|[:?+%-]))", instructions) is not None


def plan(context):
    filename = context / "bases/plan.json"
    if not filename.exists() and not filename.is_symlink():
        return None, []
    leaf = _regular(context / "Dockerfile").decode()
    if not re.search(r"(?m)^ARG RD_TOOL_BASE_READY(?:=|\s*$)", leaf) or not _uses(leaf, "RD_TOOL_BASE_READY") or not _from_contract(leaf):
        raise BaseImageError("Common bases require the RD_TOOL_BASE_READY/RD_BASE_IMAGE contract")
    raw = _regular(filename)
    if len(raw) > 64 << 10:
        raise BaseImageError("Common base plan exceeds 64 KiB")
    value = json.loads(raw, object_pairs_hook=shared_inputs.unique_object)
    if not isinstance(value, dict) or set(value) != {"schemaVersion", "target", "stages"} or type(value["schemaVersion"]) is not int or value["schemaVersion"] != 1 or not isinstance(value["target"], str) or not isinstance(value["stages"], list) or not 1 <= len(value["stages"]) <= 3:
        raise BaseImageError("Invalid common base plan")
    stages = {}
    previous = ""
    for stage in value["stages"]:
        if not isinstance(stage, dict) or set(stage) != {"id", "dockerfile", "parent", "inputs", "arguments"}:
            raise BaseImageError("Invalid common base stage")
        if not isinstance(stage["id"], str) or not re.fullmatch(r"[a-z][a-z0-9-]{0,79}", stage["id"]) or stage["id"] in stages:
            raise BaseImageError("Invalid or duplicate common base stage ID")
        if not isinstance(stage["parent"], str) or stage["parent"] != previous:
            raise BaseImageError("Invalid common base parent")
        if not isinstance(stage["inputs"], list) or not isinstance(stage["arguments"], list) or len(stage["inputs"]) > 32 or len(stage["arguments"]) > 16:
            raise BaseImageError("Invalid common base inputs")
        shared_inputs.relative(stage["dockerfile"])
        dockerfile = _regular(context / stage["dockerfile"]).decode()
        if len(dockerfile.encode()) > 1 << 20 or not re.search(r"(?m)^ARG RD_BASE_IMAGE(?:=|\s*$)", dockerfile) or not _from_contract(dockerfile):
            raise BaseImageError("Common base Dockerfile requires RD_BASE_IMAGE inheritance")
        for filename in stage["inputs"]:
            shared_inputs.relative(filename)
            _regular(context / filename)
        all_files = [stage["dockerfile"]] + stage["inputs"]
        if len({name.lower() for name in all_files}) != len(all_files) or len(set(stage["arguments"])) != len(stage["arguments"]):
            raise BaseImageError("Duplicate common base input or argument")
        for name in stage["arguments"]:
            if not isinstance(name, str) or not re.fullmatch(r"RD_[A-Z][A-Z0-9_]{0,75}", name) or name in {"RD_BASE_IMAGE", "RD_TOOL_BASE_READY", "RD_CODEX_MODE"} or not _uses(dockerfile, name) or not re.search(r"(?m)^ARG " + re.escape(name) + r"(?:=|\s*$)", dockerfile):
                raise BaseImageError("Undeclared common base argument")
        stages[stage["id"]] = stage
        previous = stage["id"]
    chain, seen, current = [], set(), value["target"]
    while current:
        if current not in stages or current in seen:
            raise BaseImageError("Missing or cyclic common base dependency")
        seen.add(current)
        chain.append(stages[current])
        current = stages[current]["parent"]
    if not chain:
        raise BaseImageError("Missing common base target")
    return value, list(reversed(chain))


def prepare(context, prefix, timeout, cache_root=None, runner=None):
    """Return frozen dependency IDs and build arguments; no recipe activation."""
    value, chain = plan(context)
    if value is None:
        return [], []
    leaf = _regular(context / "Dockerfile").decode()
    defaults = {key: value.strip().strip("\"'") for key, value in re.findall(r"(?m)^ARG ([A-Z][A-Z0-9_]*)=([^\r\n]*)$", leaf)}
    root = defaults.get("RD_BASE_IMAGE", "")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/:@-]{0,240}", root):
        raise BaseImageError("Invalid common base root image")
    deadline = time.monotonic() + timeout

    def invoke(args, capture=True, optional=False):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise BaseImageError("Common base preparation exceeded the build timeout")
        if runner:
            result = runner(prefix + args, remaining, capture)
        else:
            try:
                result = subprocess.run(prefix + args, stdout=subprocess.PIPE if capture else sys.stderr,
                                        stderr=subprocess.STDOUT if capture else sys.stderr,
                                        text=True, timeout=remaining)
            except subprocess.TimeoutExpired as error:
                raise BaseImageError("Common base preparation timed out; Docker may retain cache/work") from error
        if result.returncode:
            message = (result.stdout or "").lower()
            if optional and any(marker in message for marker in ("no such image", "no such object", "image not found")):
                return None
            raise BaseImageError("Common base command failed: " + " ".join(args[:3]) + "\n" + (result.stdout or "")[-16000:])
        return result.stdout or ""

    def inspect(reference, optional=False, expected_key=None):
        output = invoke(["image", "inspect", "--format", '{{.Id}}|{{.Os}}|{{.Architecture}}|{{index .Config.Labels "io.rundesk.base.key"}}', reference], optional=optional)
        if output is None:
            return None
        fields = output.strip().split("|")
        if len(fields) != 4 or not re.fullmatch(r"sha256:[0-9a-f]{64}", fields[0]) or fields[1] != "linux" or not re.fullmatch(r"[a-z0-9_]+", fields[2]):
            raise BaseImageError("Docker returned an invalid common base identity/platform")
        if re.fullmatch(r"sha256:[0-9a-f]{64}", reference) and fields[0] != reference:
            raise BaseImageError("Docker returned a different fixed common base image ID")
        if expected_key is not None and fields[3] != expected_key:
            raise BaseImageError("Common base image cache label changed")
        return fields[:3]

    def from_reference(image):
        # FROM sha256:<content-id> is parsed as a registry image by BuildKit.
        # Create our own content-named local alias from the frozen ID instead
        # of trusting a user tag or changing cache/snapshot identity to a tag.
        reference = "rundesk-base/image:sha256-" + image[0].removeprefix("sha256:")
        invoke(["image", "tag", image[0], reference], capture=False)
        if inspect(reference) != image:
            raise BaseImageError("Local common base reference changed identity/platform")
        return reference

    parent = inspect(root, optional=True)
    if parent is None:
        if re.fullmatch(r"sha256:[0-9a-f]{64}", root):
            raise BaseImageError("Fixed common base root image is missing locally; restore the pinned image before rebuilding")
        invoke(["pull", root], capture=False)
        parent = inspect(root)
    platform = parent[1] + "/" + parent[2]
    cache = Path(cache_root or os.environ.get("RUNDESK_BUILD_BASE_CACHE") or
                 Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache")) / "rundesk/build-bases").expanduser().absolute()
    cache.mkdir(parents=True, exist_ok=True)
    shared_inputs._ordinary(cache, directory=True)
    snapshots = []
    for stage in chain:
        stage_text = _regular(context / stage["dockerfile"]).decode()
        stage_defaults = {key: value.strip().strip("\"'") for key, value in re.findall(r"(?m)^ARG ([A-Z][A-Z0-9_]*)=([^\r\n]*)$", stage_text)}
        arguments = {key: stage_defaults.get(key, "") for key in stage["arguments"]}
        if any(key not in stage_defaults or "$" in arguments[key] for key in arguments):
            raise BaseImageError("Common base arguments require explicit defaults")
        files = {name: {"sha256": hashlib.sha256(_regular(context / name)).hexdigest(), "executable": (context / name).stat().st_mode & 0o111}
                 for name in sorted(set(stage["inputs"] + [stage["dockerfile"]]))}
        identity = {"schemaVersion": 1, "stage": stage["id"], "parentImageId": parent[0],
                    "platform": platform, "files": files, "arguments": arguments}
        key = hashlib.sha256(_canonical(identity)).hexdigest()
        record = cache / (key + ".json")
        snapshot, cached = None, None
        if record.exists() or record.is_symlink():
            snapshot = json.loads(_regular(record), object_pairs_hook=shared_inputs.unique_object)
            if not isinstance(snapshot, dict) or snapshot.get("identity") != identity or snapshot.get("key") != key or not re.fullmatch(r"sha256:[0-9a-f]{64}", str(snapshot.get("imageId", ""))):
                raise BaseImageError("Common base cache metadata changed: " + str(record))
            cached = inspect(snapshot.get("imageId", ""), optional=True, expected_key=key)
            if cached is None:
                print("Cached common base " + stage["id"] + " is missing; preparing it from this invocation\'s pinned parent and inputs", file=sys.stderr)
            elif cached[0] != snapshot.get("imageId") or cached[1:] != parent[1:]:
                raise BaseImageError("Cached common base image identity/platform changed")
        if cached is not None:
            print("Reusing common base " + stage["id"] + " " + cached[0], file=sys.stderr)
            parent = cached
        else:
            print("Preparing common base " + stage["id"], file=sys.stderr)
            with tempfile.TemporaryDirectory(prefix="rundesk-base-") as temporary:
                isolated = Path(temporary) / "context"; isolated.mkdir()
                for filename in files:
                    target = isolated / filename; target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(_regular(context / filename))
                    target.chmod(0o644 | files[filename]["executable"])
                iidfile = Path(temporary) / "image.id"
                args = ["build", "--pull=false", "--platform", platform, "--iidfile", str(iidfile), "--build-arg", "RD_BASE_IMAGE=" + from_reference(parent),
                        "--label", "io.rundesk.base.key=" + key]
                for name in sorted(arguments):
                    args += ["--build-arg", name + "=" + arguments[name]]
                args += ["-t", "rundesk-base/" + stage["id"] + ":" + key,
                         "-f", str(isolated / stage["dockerfile"]), str(isolated)]
                invoke(args, capture=False)
                iid = _regular(iidfile).decode().strip()
                built = inspect(iid, expected_key=key)
                if built[0] != iid or built[1:] != parent[1:]:
                    raise BaseImageError("Common base build returned a different identity/platform")
                snapshot = {"key": key, "imageId": iid, "identity": identity}
                descriptor, temporary_name = tempfile.mkstemp(prefix=key + ".", suffix=".tmp", dir=cache)
                temporary_record = Path(temporary_name)
                try:
                    with os.fdopen(descriptor, "wb") as output:
                        output.write(_canonical(snapshot))
                    os.replace(temporary_record, record)
                finally:
                    temporary_record.unlink(missing_ok=True)
                parent = built
        snapshots.append({"stage": stage["id"], "key": key, "imageId": parent[0], "parentImageId": identity["parentImageId"], "platform": platform})
    leaf_reference = from_reference(parent)
    lock = {"schemaVersion": 1, "target": value["target"], "stages": snapshots}
    (context / "build-bases.lock.json").write_bytes(_canonical(lock) + b"\n")
    return snapshots, ["--build-arg", "RD_BASE_IMAGE=" + leaf_reference, "--build-arg", "RD_TOOL_BASE_READY=1"]
