#!/usr/bin/env python3
"""Read canonical recipes and export isolated build contexts. Python 3.10+, stdlib only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
import uuid
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parent / "recipe_tools"))
import upstream_profiles
import dockerfile_files
import shared_inputs
import base_images

ROOT = Path(__file__).resolve().parents[1] / "recipes"
VERSION = "1.4.0"
PROFILE_IDS = tuple(sorted(p.parent.name for p in ROOT.glob("*/recipe.json") if re.fullmatch(r"[a-z0-9-]+", p.parent.name)))
EXACT = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?\Z")
IMAGE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._/:@-]{0,240}\Z")


class ProfileError(ValueError):
    pass


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=shared_inputs.unique_object)


def dump(value):
    print(json.dumps(value, ensure_ascii=False, indent=2))


def dependency_pins(name="news2douyin"):
    descriptor = read_json(ROOT / name / "recipe.json")
    return descriptor.get("dependencies", {})


def context_files(root, name, descriptor=None):
    try:
        return shared_inputs.context_files(root, name, descriptor)
    except (ValueError, OSError) as error:
        raise ProfileError(str(error)) from error


def context_file(root, name, path, descriptor=None):
    shared_inputs.relative(path)
    try:
        return context_files(root, name, descriptor)[path]
    except KeyError:
        raise ProfileError("Missing recipe input: " + path) from None


def agent_reach_files(pin, name="news2douyin", files=None):
    """Validate the reviewed local source; never download or sweep a worktree."""
    files = context_files(ROOT, name) if files is None else files
    prefix = "vendor/agent-reach/"
    if pin.get("sourceDirectory") != "vendor/agent-reach":
        raise ProfileError("Agent-Reach requires the reviewed vendored source directory")
    for field, digest in (("filesManifest", "filesManifestSHA256"), ("patchFile", "patchSHA256")):
        rel = pin.get(field, "")
        shared_inputs.relative(rel)
        file = files.get(rel)
        if file is None:
            raise ProfileError("Missing or unsafe Agent-Reach source manifest/patch")
        if hashlib.sha256(file.read_bytes()).hexdigest() != pin.get(digest):
            raise ProfileError("Agent-Reach source manifest/patch digest mismatch")
    expected = {}
    for line in files[pin["filesManifest"]].read_text(encoding="utf-8").splitlines():
        parts = line.split("  ", 1)
        if len(parts) != 2 or not re.fullmatch(r"[0-9a-f]{64}", parts[0]):
            raise ProfileError("Invalid Agent-Reach source manifest")
        digest, name = parts
        rel = Path(name)
        if rel.is_absolute() or ".." in rel.parts or rel.as_posix() != name or name in expected:
            raise ProfileError("Invalid Agent-Reach source manifest path")
        expected[name] = digest
    if not {"pyproject.toml", "LICENSE", "agent_reach/skill/SKILL.md", "agent_reach/guides/setup-exa.md"} <= expected.keys():
        raise ProfileError("Agent-Reach source manifest is incomplete")
    actual = {path[len(prefix):] for path in files if path.startswith(prefix)}
    if actual != expected.keys():
        raise ProfileError("Agent-Reach source file list mismatch")
    for name, digest in expected.items():
        if hashlib.sha256(files[prefix + name].read_bytes()).hexdigest() != digest:
            raise ProfileError("Agent-Reach source digest mismatch: " + name)
    return files[prefix + "pyproject.toml"].parent, sorted(expected)


def load_profile(name):
    if name not in PROFILE_IDS:
        raise ProfileError("Unknown profile: " + name)
    directory = ROOT / name
    if (directory / "profile.lock.json").exists():
        raise ProfileError("recipe.json is the only editable recipe descriptor")
    descriptor = directory / "recipe.json"
    if descriptor.is_symlink() or not descriptor.is_file() or descriptor.stat().st_size > 2 << 20:
        raise ProfileError("Missing, unsafe or oversized recipe descriptor")
    source_metadata = read_json(descriptor)
    files = context_files(ROOT, name, source_metadata)
    if source_metadata.get("upstream") and (directory / "upstream/upstream.json").exists():
        raise ProfileError("upstream/upstream.json is generated from recipe.json")
    value = source_metadata["profile"]
    if value.get("schemaVersion") != 1 or value.get("id") != name:
        raise ProfileError("Invalid profile identity or schema")
    if value.get("runtime") != "codex-docker":
        raise ProfileError("Unsupported runtime")
    if set(value) - {"skillFiles", "upstream"} != {"schemaVersion", "id", "version", "title", "description", "runtime", "searchBackends", "defaultSearchBackend", "aptPackages", "pythonPackages", "skill", "tools", "outputs", "resources", "optionalComponents"}:
        raise ProfileError("Unknown or missing profile fields")
    for key in ("version", "title", "description", "skill", "defaultSearchBackend"):
        if not isinstance(value[key], str) or not value[key]:
            raise ProfileError("Expected nonempty string: " + key)
    for key in ("searchBackends", "aptPackages", "pythonPackages", "tools", "outputs", "optionalComponents"):
        if not isinstance(value[key], list) or not all(isinstance(x, str) and x for x in value[key]) or len(set(value[key])) != len(value[key]):
            raise ProfileError("Expected unique string list: " + key)
    if not set(value["optionalComponents"]) <= {"agent-reach", "video-use"}:
        raise ProfileError("Unknown optional component")
    resource = value["resources"]
    if not isinstance(resource, dict) or set(resource) != {"cpus", "memoryMB", "network"}:
        raise ProfileError("Invalid resource fields")
    for key in ("cpus", "memoryMB"):
        if key in resource and (type(resource[key]) is not int or resource[key] < (1 if key == "cpus" else 256)):
            raise ProfileError("Invalid resource limit: " + key)
    if resource["network"] not in ("bridge", "none"):
        raise ProfileError("Invalid network mode")
    if not EXACT.fullmatch(value["version"]):
        raise ProfileError("Profile version must be exact")
    if not re.fullmatch(r"[a-z0-9-]{1,64}", value["skill"]):
        raise ProfileError("Invalid skill name")
    if not set(value["searchBackends"]) <= {"exa", "none"} or value["defaultSearchBackend"] not in value["searchBackends"]:
        raise ProfileError("Invalid search backends")
    for package in value["aptPackages"]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9+.-]*", package):
            raise ProfileError("Invalid apt package")
    pins = dependency_pins(name)
    for package in value["pythonPackages"]:
        if package not in pins.get("python", {}):
            raise ProfileError("Missing pinned Python package: " + package)
    for package, pin in pins.get("python", {}).items():
        if not re.fullmatch(r"[A-Za-z0-9_.-]+", package) or not re.fullmatch(r"[0-9]+(?:\.[0-9]+)+(?:[a-z0-9.-]+)?", pin):
            raise ProfileError("Invalid Python dependency pin")
    if not all(EXACT.fullmatch(v) for v in pins.get("npm", {}).values()):
        raise ProfileError("Invalid npm tool version")
    if "agent-reach" in value["optionalComponents"]:
        if not re.fullmatch(r"[0-9a-f]{40}", pins["agentReach"]["commit"]):
            raise ProfileError("Agent-Reach needs an exact commit")
        if pins["agentReach"]["repository"] != "https://github.com/silently0801/agent-reach":
            raise ProfileError("Review a new Agent-Reach source before changing repository")
        agent_reach_files(pins["agentReach"], name, files)
    if "video-use" in value["optionalComponents"] and name != "video-build":
        raise ProfileError("video-use compatibility is only offered by video-build")
    if name == "video-build":
        hyper = pins.get("hyperframes", {})
        if hyper.get("repository") != "https://github.com/heygen-com/hyperframes" or hyper.get("nodeMajorMinimum") != 22:
            raise ProfileError("HyperFrames needs the reviewed source and Node 22+ runtime")
        if not isinstance(hyper.get("npm"), dict) or set(hyper["npm"]) != {"hyperframes", "gsap"} or not all(EXACT.fullmatch(v) for v in hyper["npm"].values()):
            raise ProfileError("HyperFrames and GSAP need exact npm versions")
        rel = Path(hyper.get("npmLockFile", ""))
        shared_inputs.relative(hyper.get("npmLockFile", ""))
        path = files.get(rel.as_posix())
        if path is None:
            raise ProfileError("Missing or unsafe HyperFrames npm lock")
        if hashlib.sha256(path.read_bytes()).hexdigest() != hyper.get("npmLockSHA256"):
            raise ProfileError("HyperFrames npm lock digest mismatch")
        lock = read_json(path)
        package = read_json(files[(rel.parent / "package.json").as_posix()])
        if package.get("dependencies") != hyper["npm"] or lock["packages"][""].get("dependencies") != hyper["npm"]:
            raise ProfileError("HyperFrames package/lock does not match dependency metadata")
        for package_name, version in hyper["npm"].items():
            if lock["packages"].get("node_modules/" + package_name, {}).get("version") != version:
                raise ProfileError("HyperFrames npm lock has a mismatched direct version")
        vendor = pins.get("videoUse", {})
        if vendor.get("repository") != "https://github.com/browser-use/video-use" or not re.fullmatch(r"[0-9a-f]{40}", vendor.get("commit", "")):
            raise ProfileError("video-use must use a reviewed exact source commit")
        for filekey, hashkey in (("sourceArchive", "sourceSHA256"), ("pythonLockFile", "pythonLockSHA256")):
            rel = Path(vendor.get(filekey, ""))
            shared_inputs.relative(vendor.get(filekey, ""))
            path = files.get(rel.as_posix())
            if path is None:
                raise ProfileError("Missing or unsafe video-use source/lock file")
            if hashlib.sha256(path.read_bytes()).hexdigest() != vendor.get(hashkey):
                raise ProfileError("video-use source/lock digest mismatch")
        expected_lock = {line.strip() for line in files[vendor["pythonLockFile"]].read_text().splitlines() if line.strip() and not line.startswith("#")}
        for package, pin in vendor.get("python", {}).items():
            if not re.fullmatch(r"[A-Za-z0-9_.-]+", package) or not re.fullmatch(r"[0-9]+(?:\.[0-9]+)+(?:[a-z0-9.-]+)?", pin):
                raise ProfileError("Invalid video-use Python dependency pin")
        if expected_lock != {package + "==" + version for package, version in vendor.get("python", {}).items()}:
            raise ProfileError("video-use Python lock does not match dependency metadata")
    # Copy only declared skill files; never sweep credentials or a working tree.
    skill = Path("skills") / value["skill"]
    skill_files = value.get("skillFiles", ["SKILL.md", "agents/openai.yaml"])
    if not isinstance(skill_files, list) or len(skill_files) > 64 or not all(isinstance(rel, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/-]{0,159}", rel) and ".." not in Path(rel).parts and Path(rel).as_posix() == rel for rel in skill_files) or len(set(skill_files)) != len(skill_files) or not {"SKILL.md", "agents/openai.yaml"} <= set(skill_files):
        raise ProfileError("Invalid declared skill file list")
    for rel in skill_files:
        if (skill / rel).as_posix() not in files:
            raise ProfileError("Missing file in skill: " + str(skill / rel))
    front = files[(skill / "SKILL.md").as_posix()].read_text(encoding="utf-8")
    if not front.startswith("---\nname: " + value["skill"] + "\n") or "\ndescription: " not in front:
        raise ProfileError("Invalid skill frontmatter")
    if "upstream" in value:
        upstream_profiles.load(ROOT, value, files)
    elif name in upstream_profiles.REPOSITORIES:
        raise ProfileError("Independent upstream profile requires upstream.json")
    return value


def resolve(profile, backend=None, agent_reach=None, video_use=False):
    backend = backend or profile["defaultSearchBackend"]
    if backend not in profile["searchBackends"]:
        raise ProfileError("Search backend is not supported by this profile")
    if agent_reach is None:
        agent_reach = "agent-reach" in profile["optionalComponents"]
    if agent_reach and "agent-reach" not in profile["optionalComponents"]:
        raise ProfileError("Agent-Reach is not offered by this profile")
    if video_use and "video-use" not in profile["optionalComponents"]:
        raise ProfileError("video-use compatibility is not offered by this profile")
    result = {"searchBackend": backend, "agentReach": bool(agent_reach), "videoUse": bool(video_use)}
    if profile.get("upstream"):
        result["nativeUpstream"] = profile["id"]
    return result


def write(path, text, executable=False):
    if path.is_symlink() or (path.exists() and not path.is_file()):
        raise ProfileError("Generated file conflicts with a directory or special file: " + str(path))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8", newline="\n")
    path.chmod(0o755 if executable else 0o644)


def seed_config(selection):
    lines = ['cli_auth_credentials_store = "file"', 'check_for_update_on_startup = false', '']
    if selection["searchBackend"] != "none":
        lines += ['[mcp_servers.exa]', 'url = "https://mcp.exa.ai/mcp?tools=web_search_exa,web_fetch_exa,web_search_advanced_exa"',
                  'enabled = ' + str(selection["searchBackend"] == "exa").lower(),
                  'startup_timeout_sec = 30', 'tool_timeout_sec = 90',
                  'enabled_tools = ["web_search_exa", "web_fetch_exa", "web_search_advanced_exa"]', '']
    return "\n".join(lines)


def _copy_context(source, destination, profile):
    """Copy ordinary canonical files, never metadata twins or authoring variants."""
    for filename, path in context_files(ROOT, profile["id"]).items():
        rel = Path(filename)
        if rel.parts[0] == "variants" or filename in {"recipe.json", "SHA256SUMS"}:
            continue
        if rel.parts[0] == "skills":
            declared = profile.get("skillFiles", ["SKILL.md", "agents/openai.yaml"])
            if len(rel.parts) < 3 or rel.parts[1] != profile["skill"] or Path(*rel.parts[2:]).as_posix() not in declared:
                continue
        if rel.as_posix() in {"profile.lock.json", "upstream/upstream.json"}:
            raise ProfileError("Generated metadata must not coexist with recipe.json: " + rel.as_posix())
        target = destination / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, target)
        target.chmod(0o755 if path.stat().st_mode & 0o111 else 0o644)


def default_base_image(profile, selection):
    path = dockerfile_files.source_path(ROOT, profile, selection)
    values = re.findall(r"^ARG RD_BASE_IMAGE=(.+)$", path.read_text(encoding="utf-8"), re.MULTILINE)
    if len(values) != 1:
        raise ProfileError("Recipe requires one RD_BASE_IMAGE default")
    return values[0]


def _snapshot_files(destination):
    """Check the actual generated files, including metadata, against limits."""
    files = []
    total = 0
    for path in sorted(destination.rglob("*")):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ProfileError("Build context contains a link or special file: " + str(path))
        if path.is_file():
            files.append(path)
            total += path.stat().st_size
            shared_inputs.check_budget(len(files), total)
    return files, total


def render(profile, selection, codex_version, base_image, destination):
    if not codex_version or not EXACT.fullmatch(codex_version):
        raise ProfileError("Pass --codex-version X.Y.Z using an exact, deployed Codex release")
    if not IMAGE.fullmatch(base_image):
        raise ProfileError("Invalid base image reference")
    source = ROOT / profile["id"]
    if source.is_symlink() or not source.is_dir():
        raise ProfileError("Missing or unsafe recipe directory")
    resolved = read_json(source / "recipe.json")
    resolved.pop("sharedFiles", None)
    if (source / "profile.lock.json").exists():
        raise ProfileError("recipe.json is the only editable recipe descriptor")
    if (source / "upstream/upstream.json").exists():
        raise ProfileError("upstream/upstream.json is generated from recipe.json")
    if (source / "SHA256SUMS").is_dir():
        raise ProfileError("Generated SHA256SUMS conflicts with a source directory")
    _copy_context(source, destination, profile)
    resolved.update(generatorVersion=VERSION, profile=profile, selection=selection,
                    codexVersion=codex_version, baseImage=base_image,
                    validation={"imageBuilt": False, "modelTested": False, "networkTested": False})
    # Defaults preserve the current file tree, including operator-edited tools,
    # requirements, seed configuration and Dockerfile commands. Only explicit
    # optional selections replace their affected files.
    pins = resolved.get("dependencies", {})
    if "agent-reach" in profile["optionalComponents"] and not selection["agentReach"]:
        for rel in ("agent-tools", "vendor/agent-reach", "vendor/agent-reach.sha256", "vendor/agent-reach-wheel.patch"):
            target = destination / rel
            if target.is_dir(): shutil.rmtree(target)
            elif target.exists(): target.unlink()
        requirements = [line for line in (destination / "requirements.txt").read_text().splitlines()
                        if "/opt/rundesk-profile/vendor/agent-reach" not in line]
        write(destination / "requirements.txt", "\n".join(requirements) + "\n")
        vendor = destination / "vendor"
        if vendor.is_dir() and not any(vendor.iterdir()): vendor.rmdir()
    defaults = read_json(source / "recipe.json").get("selection", {})
    if selection.get("searchBackend") != defaults.get("searchBackend") and not profile.get("upstream"):
        write(destination / "seed/config.toml", seed_config(selection))
    video_use = pins.get("videoUse") if selection.get("videoUse") else None
    if video_use:
        archive = context_file(ROOT, profile["id"], video_use["sourceArchive"])
        target = destination / "vendor/video-use.tar.gz"
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(archive, target)
        write(destination / "requirements.txt", context_file(ROOT, profile["id"], video_use["pythonLockFile"]).read_text())
        write(destination / "vendor/video-use-pin.json", json.dumps({"repository": video_use["repository"], "commit": video_use["commit"], "sourceSHA256": video_use["sourceSHA256"], "license": "MIT"}, indent=2) + "\n")
    if resolved.get("upstream"):
        write(destination / "upstream/upstream.json", json.dumps(resolved["upstream"], ensure_ascii=False, indent=2) + "\n")
    labels = {"org.opencontainers.image.title": "RunDesk " + profile["id"],
              "org.opencontainers.image.version": profile["version"],
              "io.rundesk.profile": profile["id"], "io.rundesk.profile.schema": "1",
              "io.rundesk.codex.version": codex_version,
              "io.rundesk.agent-reach": str(selection["agentReach"]).lower(),
              "io.rundesk.capabilities.tools": ", ".join(profile["tools"] + (["agent-reach"] if selection["agentReach"] else [])),
              "io.rundesk.capabilities.skills": profile["skill"],
              "io.rundesk.capabilities.mcp": selection["searchBackend"]}
    if profile.get("upstream"):
        upstream = resolved["upstream"]
        labels.update({"io.rundesk.upstream.repository": upstream["repository"],
                       "io.rundesk.upstream.commit": upstream["commit"],
                       "io.rundesk.upstream.source.sha256": upstream["sourceSHA256"],
                       "io.rundesk.video-app-media.contract": "1",
                       "io.rundesk.capabilities.skills": ", ".join([profile["skill"]] + [row["name"] for row in upstream["skillDirectories"]]),
                       "io.rundesk.capabilities.mcp": "video_app_media"})
    if profile["id"] == "video-build":
        hyperframes = pins["hyperframes"]
        labels.update({"io.rundesk.hyperframes.version": hyperframes["npm"]["hyperframes"],
                       "io.rundesk.gsap.version": hyperframes["npm"]["gsap"],
                       "io.rundesk.hyperframes.lock": hyperframes["npmLockSHA256"],
                       "io.rundesk.video-use.enabled": str(bool(video_use)).lower()})
    if video_use:
        labels.update({"io.rundesk.video-use.commit": video_use["commit"], "io.rundesk.video-use.adapter": "2"})
    dockerfile_files.render(ROOT, profile, selection, codex_version, base_image, destination, write, labels)
    base_images.plan(destination)
    write(destination / "profile.lock.json", json.dumps(resolved, ensure_ascii=False, indent=2) + "\n")
    files, total = _snapshot_files(destination)
    manifest = "".join(hashlib.sha256(p.read_bytes()).hexdigest() + "  " + p.relative_to(destination).as_posix() + "\n" for p in files)
    shared_inputs.check_budget(len(files) + 1, total + len(manifest.encode("utf-8")))
    write(destination / "SHA256SUMS", manifest)
    _snapshot_files(destination)
    return resolved


def docker_prefix():
    exe = shutil.which("docker")
    if not exe:
        raise ProfileError("Docker CLI is unavailable; prepare/pack work without Docker")
    host = os.environ.get("RUNDESK_DOCKER_HOST")
    return [exe] + (["--host", host] if host else [])


def command(argv, timeout=30):
    # No shell expansion. stdout is reserved for the CLI's JSON result.
    try:
        p = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        return {"ok": p.returncode == 0, "exitCode": p.returncode, "output": p.stdout[-16000:]}
    except (OSError, subprocess.TimeoutExpired) as e:
        return {"ok": False, "error": str(e)}


def doctor(profile, image=None):
    result = {"schemaVersion": 1, "profileId": profile["id"], "checks": [], "modelTested": False, "networkTested": False}
    try:
        prefix = docker_prefix()
        probe = command(prefix + ["info", "--format", "{{json .ServerVersion}}"], 15)
    except ProfileError as e:
        probe = {"ok": False, "error": str(e)}
    result["checks"].append({"name": "docker", **probe})
    if probe["ok"] and image:
        if not IMAGE.fullmatch(image):
            raise ProfileError("Invalid image reference")
        identity = command(prefix + ["image", "inspect", "--format", '{{index .Config.Labels "io.rundesk.profile"}}', image], 15)
        identity["ok"] = identity["ok"] and identity.get("output", "").strip() == profile["id"]
        result["checks"].append({"name": "profile-identity", **identity})
        if not identity["ok"]:
            result.update(ready=False, scope="image identity mismatch; runtime not executed")
            return result
        name = "rundesk-profile-check-" + uuid.uuid4().hex
        # HyperFrames' render preflight checks available temporary disk space.
        # Keep the unmodified upstream check and provide a bounded 2 GiB mount
        # for video-build; other profiles retain their existing probe budget.
        temporary = "/tmp:rw,nosuid,nodev,size=" + ("2g" if profile["id"] == "video-build" else "512m") + ",mode=1777"
        try:
            result["checks"].append({"name": "image-runtime", **command(prefix + ["run", "--name", name, "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "1000:1000", "--tmpfs", temporary, "--env", "HOME=/tmp", "--env", "CODEX_HOME=/tmp/codex", "--entrypoint", "python3", image, "/opt/rundesk-profile/tools/profile_doctor.py", "--json"], 420)})
        finally:
            # Only this invocation's uniquely named probe; also clean up after
            # a client timeout, which alone does not stop a daemon-side container.
            command(prefix + ["rm", "--force", name], 15)
    result["ready"] = all(c["ok"] for c in result["checks"])
    result["scope"] = "host Docker availability" if not image else "offline image runtime; no model, MCP handshake or live search"
    return result


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="command", required=True)
    l = sub.add_parser("list"); l.add_argument("--json", action="store_true")
    for verb in ("inspect", "validate", "prepare", "pack", "build", "doctor"):
        s = sub.add_parser(verb); s.add_argument("profile", choices=PROFILE_IDS); s.add_argument("--json", action="store_true")
        s.add_argument("--search-backend", choices=["exa", "none"])
        component = s.add_mutually_exclusive_group()
        component.add_argument("--with-agent-reach", action="store_true", dest="agent_reach", default=None)
        component.add_argument("--without-agent-reach", action="store_false", dest="agent_reach")
        compatibility = s.add_mutually_exclusive_group()
        compatibility.add_argument("--with-video-use", action="store_true", dest="video_use", default=False)
        compatibility.add_argument("--without-video-use", action="store_false", dest="video_use")
        if verb in ("prepare", "pack", "build"):
            s.add_argument("--codex-version", required=True)
            s.add_argument("--base-image")
        if verb in ("prepare", "pack"):
            s.add_argument("--output", required=True)
        if verb in ("build", "doctor"):
            s.add_argument("--image")
        if verb == "build":
            s.add_argument("--timeout", type=int, default=1800)
    return p


def main(argv=None, default_profile=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if default_profile and argv and argv[0] not in ("list", "-h", "--help"):
        argv.insert(1, default_profile)
    args = parser().parse_args(argv)
    try:
        if args.command == "list":
            dump({"schemaVersion": 1, "generatorVersion": VERSION, "items": [load_profile(x) for x in PROFILE_IDS]}); return 0
        profile = load_profile(args.profile)
        selection = resolve(profile, args.search_backend, args.agent_reach, args.video_use)
        if args.command == "inspect":
            dump({"schemaVersion": 1, "profile": profile, "selection": selection, "dependencies": dependency_pins(args.profile)}); return 0
        if args.command == "validate":
            dump({"schemaVersion": 1, "valid": True, "profileId": profile["id"], "selection": selection, "scope": "catalog and skill files; no Docker or network"}); return 0
        if args.command == "doctor":
            result = doctor(profile, args.image); dump(result); return 0 if result["ready"] else 1
        output = Path(args.output).expanduser().absolute() if hasattr(args, "output") else None
        effective_output = output.resolve(strict=False) if output else None
        canonical_root = ROOT.resolve()
        if output and (effective_output == canonical_root or canonical_root in effective_output.parents or effective_output in canonical_root.parents):
            raise ProfileError("Output must be outside the canonical recipes tree")
        if output and (output.exists() or output.is_symlink()):
            raise ProfileError("Output exists; choose a new path: " + str(output))
        if output:
            output.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="rundesk-profile-", dir=str(output.parent) if output else None) as tmp:
            context = Path(tmp) / "context"; context.mkdir()
            render(profile, selection, args.codex_version, args.base_image or default_base_image(profile, selection), context)
            if args.command == "prepare":
                context.rename(output)
                dump({"schemaVersion": 1, "profileId": profile["id"], "output": str(output), "built": False}); return 0
            if args.command == "pack":
                packed = Path(tmp) / "context.zip"
                with zipfile.ZipFile(packed, "w", zipfile.ZIP_DEFLATED) as z:
                    for f in sorted(p for p in context.rglob("*") if p.is_file()):
                        entry = zipfile.ZipInfo(f.relative_to(context).as_posix(), (2020, 1, 1, 0, 0, 0))
                        entry.compress_type = zipfile.ZIP_DEFLATED
                        entry.create_system = 3
                        entry.external_attr = (0o100755 if f.stat().st_mode & 0o111 else 0o100644) << 16
                        z.writestr(entry, f.read_bytes())
                # Exclusive create: never replace an archive created concurrently.
                with output.open("xb") as dest, packed.open("rb") as source:
                    shutil.copyfileobj(source, dest)
                dump({"schemaVersion": 1, "profileId": profile["id"], "output": str(output), "sha256": hashlib.sha256(output.read_bytes()).hexdigest(), "bytes": output.stat().st_size, "built": False}); return 0
            image = args.image or "rundesk-" + profile["id"] + ":" + profile["version"] + "-codex-" + args.codex_version
            if not IMAGE.fullmatch(image) or not 1 <= args.timeout <= 7200:
                raise ProfileError("Invalid image or build timeout (1..7200 seconds)")
            prefix = docker_prefix()
            if not command(prefix + ["info"], 15)["ok"]:
                raise ProfileError("Docker Engine is unavailable")
            # Build/reuse isolated common layers before the preset in this same invocation.
            # The leaf inherits an immutable ID; standalone `docker build` still has
            # the shared installer fallback when these build arguments are absent.
            base_started = time.monotonic()
            bases, base_args = base_images.prepare(context, prefix, args.timeout)
            remaining = args.timeout - (time.monotonic() - base_started)
            if bases:
                files, _ = _snapshot_files(context)
                write(context / "SHA256SUMS", "".join(hashlib.sha256(p.read_bytes()).hexdigest() + "  " + p.relative_to(context).as_posix() + "\n" for p in files if p != context / "SHA256SUMS"))
            if remaining <= 0:
                raise ProfileError("Build timed out while preparing common bases")
            # Stream build output to stderr and track the immutable image ID.
            iidfile = Path(tmp) / "image.id"
            try:
                p = subprocess.run(prefix + ["build", "--iidfile", str(iidfile), "-t", image] + base_args + [str(context)], stdout=sys.stderr, stderr=sys.stderr, timeout=remaining)
            except subprocess.TimeoutExpired:
                raise ProfileError("Build timed out; Docker daemon may retain work/cache. Inspect before retrying.")
            if p.returncode or not iidfile.exists():
                raise ProfileError("Docker build failed; no image was activated")
            iid = iidfile.read_text().strip()
            if not re.fullmatch(r"sha256:[0-9a-f]{64}", iid):
                raise ProfileError("Docker returned an invalid image ID")
            checks = doctor(profile, iid)
            dump({"schemaVersion": 1, "profileId": profile["id"], "image": image, "imageId": iid, "built": True, "activated": False, "bases": bases, "doctor": checks})
            return 0 if checks["ready"] else 1
    except (ProfileError, OSError, ValueError, KeyError, TypeError) as e:
        print(json.dumps({"schemaVersion": 1, "error": str(e)}, ensure_ascii=False), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
