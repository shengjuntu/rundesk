"""Compiler for independent upstream video environments; no network at prepare time."""
import hashlib
import dockerfile_files
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile


REPOSITORIES = {
    "hyperframes": "https://github.com/heygen-com/hyperframes",
    "video-use": "https://github.com/browser-use/video-use",
    "openmontage": "https://github.com/calesthio/OpenMontage",
}


def relative(value, dot=False):
    if dot and value == ".":
        return value
    if (not isinstance(value, str) or not value or len(value) > 240
            or not re.fullmatch(r"[A-Za-z0-9_.@+-][A-Za-z0-9_.@+/-]*", value)
            or ".." in PurePosixPath(value).parts
            or PurePosixPath(value).as_posix() != value):
        raise ValueError("Unsafe upstream relative path")
    return value


def local(root, value, directory=False):
    relative(value)
    path = root / value
    if path.is_symlink() or any(p.is_symlink() for p in path.parents if p != root.parent):
        raise ValueError("Upstream build inputs cannot contain symlinks")
    if not (path.is_dir() if directory else path.is_file()):
        raise ValueError("Missing upstream build input: " + value)
    return path


def source_digest(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def archive_members(path):
    """Validate the strip-components=1 contract without extracting arbitrary paths."""
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
    if not members or len(members) > 100000:
        raise ValueError("Invalid upstream source archive size")
    prefixes, files = set(), set()
    total = 0
    for member in members:
        name = member.name.rstrip("/")
        if not name or name.startswith("/") or "\\" in name or ".." in PurePosixPath(name).parts:
            raise ValueError("Unsafe upstream source archive path")
        parts = PurePosixPath(name).parts
        prefixes.add(parts[0])
        stripped = PurePosixPath(*parts[1:]).as_posix()
        if not (member.isfile() or member.isdir() or member.issym() or member.islnk()):
            raise ValueError("Unsupported upstream archive entry")
        if member.issym() or member.islnk():
            target = member.linkname
            if target.startswith("/") or "\\" in target:
                raise ValueError("Unsafe upstream archive link")
            stack = list(parts[:-1]) if member.issym() else []
            for part in PurePosixPath(target).parts:
                if part == "..":
                    if len(stack) <= 1:
                        raise ValueError("Upstream archive link escapes its source root")
                    stack.pop()
                elif part != ".":
                    stack.append(part)
            if not stack or stack[0] != parts[0]:
                raise ValueError("Upstream archive link escapes its source root")
        if member.isfile():
            if stripped in files:
                raise ValueError("Duplicate upstream source file")
            files.add(stripped)
            total += member.size
    if len(prefixes) != 1 or total > 512 << 20:
        raise ValueError("Upstream archive must have one bounded source root")
    return files


def load(root, profile):
    name = profile["id"]
    if name not in REPOSITORIES or profile.get("upstream") != "upstream.json":
        raise ValueError("Only reviewed independent upstream profiles may use upstream.json")
    base = root / name
    value = json.loads(local(base, "upstream.json").read_text(encoding="utf-8"))
    required = {"schemaVersion", "repository", "commit", "sourceSHA256", "license", "installScript", "buildFiles", "skillDirectories", "requiredFiles"}
    fields = set(value) - {"environment", "sourceScope"}
    if fields not in (required | {"sourceArchive"}, required | {"sourceURL"}) or value["schemaVersion"] != 1:
        raise ValueError("Invalid upstream source manifest fields")
    if value["repository"] != REPOSITORIES[name] or not re.fullmatch(r"[0-9a-f]{40}", value.get("commit", "")):
        raise ValueError("Upstream source must use its reviewed repository and exact commit")
    if not re.fullmatch(r"[0-9a-f]{64}", value.get("sourceSHA256", "")):
        raise ValueError("Upstream source needs an exact SHA-256")
    if not isinstance(value["license"], str) or not re.fullmatch(r"[A-Za-z0-9.+-]{1,64}", value["license"]):
        raise ValueError("Invalid upstream license identifier")
    if value.get("sourceScope", "complete-repository") not in ("complete-repository", "native-skills-and-metadata"):
        raise ValueError("Unknown upstream source scope")
    members = None
    if "sourceArchive" in value:
        path = local(base, value["sourceArchive"])
        if source_digest(path) != value["sourceSHA256"]:
            raise ValueError("Upstream source archive digest mismatch")
        try:
            members = archive_members(path)
        except (tarfile.TarError, EOFError) as error:
            raise ValueError("Invalid upstream source archive") from error
    else:
        expected = "https://codeload.github.com/" + value["repository"].removeprefix("https://github.com/") + "/tar.gz/" + value["commit"]
        if value["sourceURL"] != expected:
            raise ValueError("Upstream source URL must be the exact reviewed codeload commit")
    build_files = value["buildFiles"]
    if not isinstance(build_files, list) or not 1 <= len(build_files) <= 128 or len(set(build_files)) != len(build_files):
        raise ValueError("Invalid upstream build file list")
    for item in build_files:
        relative(item)
        path = base / item
        if path.is_dir():
            local(base, item, directory=True)
            for child in path.rglob("*"):
                if child.is_symlink() or (not child.is_file() and not child.is_dir()):
                    raise ValueError("Unsafe upstream build directory")
        else:
            local(base, item)
    relative(value["installScript"])
    if value["installScript"] not in build_files or not local(base, value["installScript"]).is_file():
        raise ValueError("Upstream install script must be a declared local file")
    if "doctor.py" not in build_files:
        raise ValueError("Independent upstream profiles must declare doctor.py")
    native = value["skillDirectories"]
    if not isinstance(native, list) or not 0 <= len(native) <= 64:
        raise ValueError("Invalid native upstream skill list")
    names = set()
    for item in native:
        if not isinstance(item, dict) or set(item) != {"source", "name"}:
            raise ValueError("Invalid upstream skill directory")
        relative(item["source"], dot=True)
        skill_name = item["name"]
        if (not isinstance(skill_name, str) or not re.fullmatch(r"[a-z0-9-]{1,64}", skill_name)
                or skill_name == profile["skill"] or skill_name in names):
            raise ValueError("Invalid or duplicate native upstream skill name")
        names.add(skill_name)
        if members is not None:
            expected = "SKILL.md" if item["source"] == "." else item["source"] + "/SKILL.md"
            if expected not in members:
                raise ValueError("Missing upstream native SKILL.md: " + expected)
    files = value["requiredFiles"]
    if not isinstance(files, list) or not 1 <= len(files) <= 128 or len(set(files)) != len(files):
        raise ValueError("Invalid upstream required files")
    for item in files:
        relative(item)
        if members is not None and item not in members:
            raise ValueError("Missing required upstream source file: " + item)
    environment = value.get("environment", {})
    if not isinstance(environment, dict) or len(environment) > 16:
        raise ValueError("Invalid upstream environment constants")
    for key, constant in environment.items():
        if (not isinstance(key, str) or not re.fullmatch(r"(?:HYPERFRAMES_[A-Z0-9_]+|DO_NOT_TRACK|NODE_PATH|PYTHONPATH)", key)
                or any(word in key for word in ("TOKEN", "SECRET", "PASSWORD", "API_KEY"))
                or not isinstance(constant, str) or not 1 <= len(constant) <= 512
                or any(char in constant for char in ("\n", "\r", "\x00", "$"))):
            raise ValueError("Only reviewed non-credential upstream environment constants are allowed")
    return value


def render(root, profile, selection, codex_version, base_image, destination, write, generator_version):
    upstream = load(root, profile)
    name, skill = profile["id"], profile["skill"]
    base, source = root / name, "/opt/upstreams/" + name
    for rel in profile.get("skillFiles", ["SKILL.md", "agents/openai.yaml"]):
        write(destination / "skills" / skill / rel, (base / "skills" / skill / rel).read_text(encoding="utf-8"))
    for rel in upstream["buildFiles"]:
        path, target = base / rel, destination / "upstream" / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        if path.is_dir():
            shutil.copytree(path, target)
        else:
            shutil.copyfile(path, target)
            target.chmod(0o755 if path.stat().st_mode & 0o111 else 0o644)
    write(destination / "upstream/upstream.json", json.dumps(upstream, ensure_ascii=False, indent=2) + "\n")
    write(destination / "tools/configure-apt.sh", (root / "_shared/configure-apt.sh").read_text(), executable=True)
    write(destination / "tools/profile_doctor.py", (root / "_shared/upstream_profile_doctor.py").read_text(), executable=True)
    write(destination / "tools/video_app_media.py", (root / "_shared/video_app_media.py").read_text(), executable=True)
    seed = ('cli_auth_credentials_store = "file"\ncheck_for_update_on_startup = false\n\n'
            '[mcp_servers.video_app_media]\ncommand = "python3"\n'
            'args = ["/opt/rundesk-profile/tools/video_app_media.py", "serve-mcp"]\n'
            'env_vars = ["VIDEO_APP_MCP_URL", "VIDEO_APP_TOKEN", "VIDEO_APP_PROJECT_ID"]\n'
            'enabled = true\nstartup_timeout_sec = 30\ntool_timeout_sec = 1800\n')
    write(destination / "seed/config.toml", seed)
    resolved = {"schemaVersion": 1, "generatorVersion": generator_version, "profile": profile,
                "selection": selection, "codexVersion": codex_version, "baseImage": base_image,
                "upstream": upstream, "validation": {"imageBuilt": False, "modelTested": False, "networkTested": False}}
    write(destination / "profile.lock.json", json.dumps(resolved, ensure_ascii=False, indent=2) + "\n")
    pkgs = sorted(set(["ca-certificates", "git", "curl", "ripgrep", "python3", "python3-venv"] + profile["aptPackages"]))
    labels = {"org.opencontainers.image.title": "RunDesk " + name, "org.opencontainers.image.version": profile["version"],
              "io.rundesk.profile": name, "io.rundesk.profile.schema": "1", "io.rundesk.codex.version": codex_version,
              "io.rundesk.agent-reach": "false", "io.rundesk.upstream.repository": upstream["repository"],
              "io.rundesk.upstream.commit": upstream["commit"], "io.rundesk.upstream.source.sha256": upstream["sourceSHA256"],
              "io.rundesk.video-app-media.contract": "1", "io.rundesk.capabilities.skills": ", ".join([skill] + [row["name"] for row in upstream["skillDirectories"]]),
              "io.rundesk.capabilities.tools": ", ".join(profile["tools"]), "io.rundesk.capabilities.mcp": "video_app_media"}
    if "sourceArchive" in upstream:
        (destination / "vendor").mkdir()
        shutil.copyfile(base / upstream["sourceArchive"], destination / "vendor/source.tar.gz")
    dockerfile_files.render(root, profile, selection, codex_version, base_image, destination, write, labels)
    write(destination / ".dockerignore", "**\n!Dockerfile\n!profile.lock.json\n!THIRD_PARTY.md\n!seed/\n!seed/**\n!skills/\n!skills/**\n!tools/\n!tools/**\n!upstream/\n!upstream/**\n!vendor/\n!vendor/**\n")
    scope = upstream.get("sourceScope", "complete-repository")
    description = ("The pinned native skills and repository metadata (not the complete monorepo) remain in "
                   if scope == "native-skills-and-metadata" else "The complete pinned upstream repository source remains in ")
    write(destination / "THIRD_PARTY.md", "# Independent upstream environment\n\nRepository: " + upstream["repository"] + "\nCommit: " + upstream["commit"] + "\nLicense: " + upstream["license"] + "\nSource scope: " + scope + "\n\n" + description + source + ". Read the upstream LICENSE there. Runtime packages are installed separately from the locked dependencies declared by install.sh. The original source archive and reviewed integration sources remain in /opt/rundesk-upstream. Consult those license files before redistributing an image. No upstream code is imported into RunDesk's Go server or the independent video-build workflow.\n")
    write(destination / "README.md", "# " + profile["title"] + "\n\n" + profile["description"] + "\n\nThis is a build context, not an image or a model acceptance test. Build dependencies and any pinned sourceURL download occur at build time. Use RunDesk to create a dedicated Codex Docker instance/workspace. Configure VIDEO_APP_MCP_URL, VIDEO_APP_TOKEN and VIDEO_APP_PROJECT_ID in this instance's video_app_media MCP runtime environment; no credential is embedded. RunDesk does not automatically forward host exports into a managed Docker container. The pinned upstream sources (scope: " + scope + ") and native skills remain available under " + source + ". Existing mounted HOME configurations are not silently rewritten: explicitly add the video_app_media MCP entry from seed/config.toml to an existing environment.\n\nRun python3 /opt/rundesk-profile/tools/profile_doctor.py --json for offline checks. Native rendering stays in the upstream environment; generated media comes from Video App.\n")
    files = sorted(p for p in destination.rglob("*") if p.is_file())
    write(destination / "SHA256SUMS", "".join(hashlib.sha256(p.read_bytes()).hexdigest() + "  " + p.relative_to(destination).as_posix() + "\n" for p in files))
    return resolved
