#!/usr/bin/env python3
"""RunDesk profile catalog and build-context compiler. Python 3.10+, stdlib only."""
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
import uuid
import zipfile

ROOT = Path(__file__).resolve().parent
VERSION = "1.3.0"
PROFILE_IDS = tuple(sorted(p.parent.name for p in ROOT.glob("*/profile.json") if re.fullmatch(r"[a-z0-9-]+", p.parent.name)))
EXACT = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?\Z")
IMAGE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._/:@-]{0,240}\Z")


class ProfileError(Exception):
    pass


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def dump(value):
    print(json.dumps(value, ensure_ascii=False, indent=2))


def load_profile(name):
    if name not in PROFILE_IDS:
        raise ProfileError("Unknown profile: " + name)
    value = read_json(ROOT / name / "profile.json")
    if value.get("schemaVersion") != 1 or value.get("id") != name:
        raise ProfileError("Invalid profile identity or schema")
    if value.get("runtime") != "codex-docker":
        raise ProfileError("Unsupported runtime")
    if set(value) - {"skillFiles"} != {"schemaVersion", "id", "version", "title", "description", "runtime", "searchBackends", "defaultSearchBackend", "aptPackages", "pythonPackages", "skill", "tools", "outputs", "resources", "optionalComponents"}:
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
    pins = read_json(ROOT / "_shared" / "dependencies.json")
    for package in value["pythonPackages"]:
        if package not in pins["python"]:
            raise ProfileError("Missing pinned Python package: " + package)
    for package, pin in pins["python"].items():
        if not re.fullmatch(r"[A-Za-z0-9_.-]+", package) or not re.fullmatch(r"[0-9]+(?:\.[0-9]+)+(?:[a-z0-9.-]+)?", pin):
            raise ProfileError("Invalid Python dependency pin")
    if not all(EXACT.fullmatch(v) for v in pins["npm"].values()):
        raise ProfileError("Invalid npm tool version")
    if not re.fullmatch(r"[0-9a-f]{40}", pins["agentReach"]["commit"]):
        raise ProfileError("Agent-Reach needs an exact commit")
    if pins["agentReach"]["repository"] != "https://github.com/silently0801/agent-reach":
        raise ProfileError("Review a new Agent-Reach source before changing repository")
    if "video-use" in value["optionalComponents"] and name != "video-build":
        raise ProfileError("video-use compatibility is only offered by video-build")
    if name == "video-build":
        hyper = pins.get("hyperframes", {})
        if hyper.get("repository") != "https://github.com/heygen-com/hyperframes" or hyper.get("nodeMajorMinimum") != 22:
            raise ProfileError("HyperFrames needs the reviewed source and Node 22+ runtime")
        if not isinstance(hyper.get("npm"), dict) or set(hyper["npm"]) != {"hyperframes", "gsap"} or not all(EXACT.fullmatch(v) for v in hyper["npm"].values()):
            raise ProfileError("HyperFrames and GSAP need exact npm versions")
        rel = Path(hyper.get("npmLockFile", ""))
        path = ROOT / "_shared" / rel
        if rel.is_absolute() or ".." in rel.parts or not path.is_file() or path.is_symlink() or path.parent.is_symlink():
            raise ProfileError("Missing or unsafe HyperFrames npm lock")
        if hashlib.sha256(path.read_bytes()).hexdigest() != hyper.get("npmLockSHA256"):
            raise ProfileError("HyperFrames npm lock digest mismatch")
        lock = read_json(path)
        package = read_json(path.with_name("package.json"))
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
            path = ROOT / name / rel
            if rel.is_absolute() or ".." in rel.parts or not path.is_file() or path.is_symlink() or any(p.is_symlink() for p in path.parents if p != ROOT.parent):
                raise ProfileError("Missing or unsafe video-use source/lock file")
            if hashlib.sha256(path.read_bytes()).hexdigest() != vendor.get(hashkey):
                raise ProfileError("video-use source/lock digest mismatch")
        expected_lock = {line.strip() for line in (ROOT / name / vendor["pythonLockFile"]).read_text().splitlines() if line.strip() and not line.startswith("#")}
        for package, pin in vendor.get("python", {}).items():
            if not re.fullmatch(r"[A-Za-z0-9_.-]+", package) or not re.fullmatch(r"[0-9]+(?:\.[0-9]+)+(?:[a-z0-9.-]+)?", pin):
                raise ProfileError("Invalid video-use Python dependency pin")
        if expected_lock != {package + "==" + version for package, version in vendor.get("python", {}).items()}:
            raise ProfileError("video-use Python lock does not match dependency metadata")
    # Copy only declared skill files; never sweep credentials or a working tree.
    skill = ROOT / name / "skills" / value["skill"]
    skill_files = value.get("skillFiles", ["SKILL.md", "agents/openai.yaml"])
    if not isinstance(skill_files, list) or len(skill_files) > 64 or not all(isinstance(rel, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/-]{0,159}", rel) and ".." not in Path(rel).parts and Path(rel).as_posix() == rel for rel in skill_files) or len(set(skill_files)) != len(skill_files) or not {"SKILL.md", "agents/openai.yaml"} <= set(skill_files):
        raise ProfileError("Invalid declared skill file list")
    for rel in skill_files:
        path = skill / rel
        if not path.is_file() or path.is_symlink() or any(parent.is_symlink() for parent in [skill.parent, skill, *[ancestor for ancestor in path.parents if skill in ancestor.parents]]):
            raise ProfileError("Missing file or symlink in skill: " + str(path))
    front = (skill / "SKILL.md").read_text(encoding="utf-8")
    if not front.startswith("---\nname: " + value["skill"] + "\n") or "\ndescription: " not in front:
        raise ProfileError("Invalid skill frontmatter")
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
    return {"searchBackend": backend, "agentReach": bool(agent_reach), "videoUse": bool(video_use)}


def write(path, text, executable=False):
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


def render(profile, selection, codex_version, base_image, destination):
    if not codex_version or not EXACT.fullmatch(codex_version):
        raise ProfileError("Pass --codex-version X.Y.Z using an exact, deployed Codex release")
    if not IMAGE.fullmatch(base_image):
        raise ProfileError("Invalid base image reference")
    pins = read_json(ROOT / "_shared" / "dependencies.json")
    pkgs = sorted(set(["ca-certificates", "git", "curl", "ripgrep", "python3", "python3-venv"] + profile["aptPackages"]))
    if selection["agentReach"]:
        pkgs += ["gh"]
    requirements = [p + "==" + pins["python"][p] for p in profile["pythonPackages"]]
    hyperframes = pins.get("hyperframes") if profile["id"] == "video-build" else None
    video_use = pins.get("videoUse") if selection.get("videoUse") else None
    if video_use:
        requirements = [p + "==" + v for p, v in sorted(video_use["python"].items(), key=lambda row: row[0].lower())]
    if selection["agentReach"]:
        a = pins["agentReach"]
        requirements += ["agent-reach @ " + a["repository"] + "/archive/" + a["commit"] + ".zip"]
    write(destination / "requirements.txt", "\n".join(requirements) + "\n")
    write(destination / "seed/config.toml", seed_config(selection))
    skill = profile["skill"]
    for rel in profile.get("skillFiles", ["SKILL.md", "agents/openai.yaml"]):
        write(destination / "skills" / skill / rel, (ROOT / profile["id"] / "skills" / skill / rel).read_text(encoding="utf-8"))
    for filename in ("profile_doctor.py", "read_web.py", "video_smoke.py", "video_use_adapter.py", "video_use_smoke.py", "hyperframes_smoke.py", "mcporter", "agent_reach_config.py"):
        source = ROOT / "_shared" / filename
        write(destination / "tools" / filename, source.read_text(encoding="utf-8"), executable=True)
    resolved = {"schemaVersion": 1, "generatorVersion": VERSION, "profile": profile,
                "selection": selection, "codexVersion": codex_version, "baseImage": base_image,
                "dependencies": pins, "validation": {"imageBuilt": False, "modelTested": False, "networkTested": False}}
    write(destination / "profile.lock.json", json.dumps(resolved, ensure_ascii=False, indent=2) + "\n")
    if selection["agentReach"]:
        for filename in ("package.json", "package-lock.json"):
            write(destination / "agent-tools" / filename, (ROOT / "_shared/agent-tools" / filename).read_text())
    if hyperframes:
        for filename in ("package.json", "package-lock.json"):
            write(destination / "hyperframes" / filename, (ROOT / "_shared/hyperframes" / filename).read_text())
        write(destination / "hyperframes/LICENSES/Apache-2.0.txt", (ROOT / "_shared/hyperframes/LICENSES/Apache-2.0.txt").read_text())
    labels = {"org.opencontainers.image.title": "RunDesk " + profile["id"],
              "org.opencontainers.image.version": profile["version"],
              "io.rundesk.profile": profile["id"], "io.rundesk.profile.schema": "1",
              "io.rundesk.codex.version": codex_version,
              "io.rundesk.agent-reach": str(selection["agentReach"]).lower(),
              "io.rundesk.capabilities.tools": ", ".join(profile["tools"] + (["agent-reach"] if selection["agentReach"] else [])),
              "io.rundesk.capabilities.skills": skill,
              "io.rundesk.capabilities.mcp": selection["searchBackend"]}
    if hyperframes:
        labels.update({"io.rundesk.hyperframes.version": hyperframes["npm"]["hyperframes"], "io.rundesk.gsap.version": hyperframes["npm"]["gsap"], "io.rundesk.hyperframes.lock": hyperframes["npmLockSHA256"], "io.rundesk.video-use.enabled": str(bool(video_use)).lower()})
    if video_use:
        labels.update({"io.rundesk.video-use.commit": video_use["commit"], "io.rundesk.video-use.adapter": "2"})
        archive = ROOT / profile["id"] / video_use["sourceArchive"]
        destination.joinpath("vendor").mkdir()
        shutil.copyfile(archive, destination / "vendor/video-use.tar.gz")
        write(destination / "vendor/video-use-pin.json", json.dumps({"repository": video_use["repository"], "commit": video_use["commit"], "sourceSHA256": video_use["sourceSHA256"], "license": "MIT"}, indent=2) + "\n")
    # Keep Codex and changing metadata after the expensive shared dependency layers.
    lines = ["# Generated by examples/profiles.py; inspect profile.lock.json before building.",
             "FROM " + base_image,
              "RUN apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 update && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 install -y --no-install-recommends " + " ".join(pkgs) + " && rm -rf /var/lib/apt/lists/*",
              "ENV npm_config_fetch_retries=3 npm_config_fetch_timeout=120000",
              "RUN python3 -m venv /opt/rundesk-python",
              "ENV PATH=\"/opt/rundesk-python/bin:${PATH}\" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1",
              "COPY requirements.txt /opt/rundesk-profile/requirements.txt"]
    if requirements:
        lines += ["RUN python3 -m pip install --retries 3 --timeout 60 --no-cache-dir " + ("--no-deps " if video_use else "") + "-r /opt/rundesk-profile/requirements.txt && python3 -m pip check"]
    if video_use:
        lines += ["COPY vendor/video-use.tar.gz /opt/rundesk-profile/video-use.tar.gz",
                  "RUN echo '" + video_use["sourceSHA256"] + "  /opt/rundesk-profile/video-use.tar.gz' | sha256sum -c - && mkdir -p /opt/video-use && tar -xzf /opt/rundesk-profile/video-use.tar.gz --strip-components=1 -C /opt/video-use && rm /opt/rundesk-profile/video-use.tar.gz",
                  "COPY vendor/video-use-pin.json /opt/video-use/RUNDESK_PIN.json",
                  "RUN chmod -R a+rX /opt/video-use && test -f /opt/video-use/LICENSE && test -f /opt/video-use/helpers/render.py"]
    if hyperframes:
        lines += ["COPY hyperframes/ /opt/hyperframes/",
                  "RUN npm ci --prefix /opt/hyperframes --omit=dev --no-audit --no-fund && npm cache clean --force",
                  'ENV PATH="/opt/hyperframes/node_modules/.bin:${PATH}" HYPERFRAMES_BROWSER_PATH=/usr/bin/chromium HYPERFRAMES_NO_TELEMETRY=1 DO_NOT_TRACK=1',
                  "RUN hyperframes --version && chromium --version && node -e \"if (Number(process.versions.node.split('.')[0]) < 22) process.exit(1)\""]
    if selection["agentReach"]:
        lines += ["COPY agent-tools/ /opt/agent-tools/",
                  "RUN npm ci --prefix /opt/agent-tools --omit=dev --no-audit --no-fund && npm cache clean --force",
                  'ENV PATH="/opt/agent-tools/node_modules/.bin:${PATH}" NODE_PATH=/opt/agent-tools/node_modules',
                  "RUN mkdir -p /etc/codex/skills/agent-reach && python3 -c \"from importlib.resources import files; from pathlib import Path; Path('/etc/codex/skills/agent-reach/SKILL.md').write_text(files('agent_reach').joinpath('skill/SKILL.md').read_text())\"",
                  "RUN agent-reach --version && gh --version && bird --version && mcporter --version"]
    lines += ["ARG CODEX_VERSION=" + codex_version,
              'RUN npm install --global "@openai/codex@${CODEX_VERSION}" && npm cache clean --force && codex --version && codex app-server --help >/dev/null']
    lines += ["LABEL " + k + "=" + json.dumps(v, ensure_ascii=False) for k, v in labels.items()]
    # The admin skill path survives RunDesk's runtime HOME mount; no duplicate skills.
    lines += ["COPY skills/ /etc/codex/skills/", "COPY seed/ /opt/rundesk-seed/", "COPY tools/ /opt/rundesk-profile/tools/",
              "COPY profile.lock.json /opt/rundesk-profile/profile.lock.json",
              "COPY THIRD_PARTY.md /opt/rundesk-profile/THIRD_PARTY.md",
              "RUN mkdir -p /opt/rundesk/bin && chmod -R a+rX /opt/rundesk-profile /etc/codex/skills /opt/rundesk-seed",
              # Resolve mcporter's config from the runtime HOME, never /root or a workspace.
              *( ["COPY tools/mcporter /usr/local/bin/mcporter", "ENV PATH=\"/usr/local/bin:${PATH}\""] if selection["agentReach"] else [] ),
              "USER node", "WORKDIR /home/node", 'CMD ["codex", "app-server"]', ""]
    write(destination / "Dockerfile", "\n".join(lines))
    write(destination / ".dockerignore", "**\n!Dockerfile\n!requirements.txt\n!profile.lock.json\n!THIRD_PARTY.md\n!seed/\n!seed/**\n!skills/\n!skills/**\n!tools/\n!tools/**\n!agent-tools/\n!agent-tools/**\n!vendor/\n!vendor/**\n!hyperframes/\n!hyperframes/**\n")
    write(destination / "THIRD_PARTY.md", (ROOT / "_shared/THIRD_PARTY.md").read_text(encoding="utf-8"))
    write(destination / "README.md", "# " + profile["title"] + "\n\n" + profile["description"] + "\n\n"
          "This is a generated Docker build context, not a built image. Dependencies are downloaded during build.\n"
          "The Codex version is fixed in Dockerfile; no build arguments are needed for RunDesk WebUI.\n"
          "Upload this ZIP with Dockerfile at its root, build, then explicitly select the image for the application.\n"
          "Skills live in /etc/codex/skills; seed/config.toml initializes missing config only. Existing config is not merged.\n"
          "Use a new environment or apply the selected MCP configuration explicitly. Configure Codex authentication at runtime.\n"
          "Run python3 /opt/rundesk-profile/tools/profile_doctor.py --json inside the deployed container.\n"
          "Agent-Reach includes gh, mcporter, bird, undici and its Codex skill; mcporter seeds Exa into the runtime HOME on first use.\n"
          "Cookies, proxies and optional external platform MCP services are configured at runtime; no Docker socket is bundled. video-build includes system Chromium for local HyperFrames checks.\n"
          "Run agent-reach doctor after startup to check live channels; it is not a model or research acceptance test.\n")
    files = sorted(p for p in destination.rglob("*") if p.is_file())
    write(destination / "SHA256SUMS", "".join(hashlib.sha256(p.read_bytes()).hexdigest() + "  " + p.relative_to(destination).as_posix() + "\n" for p in files))
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
        try:
            result["checks"].append({"name": "image-runtime", **command(prefix + ["run", "--name", name, "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "1000:1000", "--tmpfs", "/tmp:rw,nosuid,nodev,size=512m,mode=1777", "--env", "HOME=/tmp", "--env", "CODEX_HOME=/tmp/codex", "--entrypoint", "python3", image, "/opt/rundesk-profile/tools/profile_doctor.py", "--json"], 420)})
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
            s.add_argument("--base-image", default="node:22-bookworm-slim")
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
            dump({"schemaVersion": 1, "profile": profile, "selection": selection, "dependencies": read_json(ROOT / "_shared/dependencies.json")}); return 0
        if args.command == "validate":
            dump({"schemaVersion": 1, "valid": True, "profileId": profile["id"], "selection": selection, "scope": "catalog and skill files; no Docker or network"}); return 0
        if args.command == "doctor":
            result = doctor(profile, args.image); dump(result); return 0 if result["ready"] else 1
        output = Path(args.output).expanduser().absolute() if hasattr(args, "output") else None
        if output and (output.exists() or output.is_symlink()):
            raise ProfileError("Output exists; choose a new path: " + str(output))
        if output:
            output.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="rundesk-profile-", dir=str(output.parent) if output else None) as tmp:
            context = Path(tmp) / "context"; context.mkdir()
            render(profile, selection, args.codex_version, args.base_image, context)
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
            # Stream build output to stderr and track the immutable image ID.
            iidfile = Path(tmp) / "image.id"
            try:
                p = subprocess.run(prefix + ["build", "--iidfile", str(iidfile), "-t", image, str(context)], stdout=sys.stderr, stderr=sys.stderr, timeout=args.timeout)
            except subprocess.TimeoutExpired:
                raise ProfileError("Build timed out; Docker daemon may retain work/cache. Inspect before retrying.")
            if p.returncode or not iidfile.exists():
                raise ProfileError("Docker build failed; no image was activated")
            iid = iidfile.read_text().strip()
            if not re.fullmatch(r"sha256:[0-9a-f]{64}", iid):
                raise ProfileError("Docker returned an invalid image ID")
            checks = doctor(profile, iid)
            dump({"schemaVersion": 1, "profileId": profile["id"], "image": image, "imageId": iid, "built": True, "activated": False, "doctor": checks})
            return 0 if checks["ready"] else 1
    except (ProfileError, OSError, ValueError, KeyError, TypeError) as e:
        print(json.dumps({"schemaVersion": 1, "error": str(e)}, ensure_ascii=False), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
