"""Compiler for independent upstream video environments; no network at prepare time."""
import hashlib
import dockerfile_files
import json
import lzma
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile
import shared_inputs


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
    with tarfile.open(path, "r:*") as archive:
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


def load(root, profile, inputs=None):
    name = profile["id"]
    if name not in REPOSITORIES or profile.get("upstream") != "upstream.json":
        raise ValueError("Only reviewed independent upstream profiles may use upstream.json")
    inputs = shared_inputs.context_files(root, name) if inputs is None else inputs

    def file(value):
        relative(value)
        if value not in inputs:
            raise ValueError("Missing upstream build input: " + value)
        return inputs[value]

    value = json.loads(file("recipe.json").read_text(encoding="utf-8"), object_pairs_hook=shared_inputs.unique_object)["upstream"]
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
    if value.get("sourceScope", "complete-repository") not in ("complete-repository", "native-skills-and-metadata", "runtime-without-demo-media"):
        raise ValueError("Unknown upstream source scope")
    if value.get("sourceScope") == "runtime-without-demo-media" and (name != "openmontage" or "sourceArchive" not in value):
        raise ValueError("Reviewed runtime source bundle requires local OpenMontage archive")
    members = None
    if "sourceArchive" in value:
        path = file(value["sourceArchive"])
        if source_digest(path) != value["sourceSHA256"]:
            raise ValueError("Upstream source archive digest mismatch")
        try:
            members = archive_members(path)
        except (tarfile.TarError, EOFError, lzma.LZMAError) as error:
            raise ValueError("Invalid upstream source archive") from error
    else:
        expected = "https://codeload.github.com/" + value["repository"].removeprefix("https://github.com/") + "/tar.gz/" + value["commit"]
        if value["sourceURL"] != expected:
            raise ValueError("Upstream source URL must be the exact reviewed codeload commit")
    if value.get("sourceScope") == "runtime-without-demo-media":
        provenance = json.loads(file("upstream/source-bundle.json").read_text(encoding="utf-8"), object_pairs_hook=shared_inputs.unique_object)
        if (provenance.get("schemaVersion") != 1 or provenance.get("scope") != value["sourceScope"]
                or provenance.get("repository") != value["repository"] or provenance.get("commit") != value["commit"]
                or provenance.get("bundlePath") != value["sourceArchive"] or provenance.get("bundleSHA256") != value["sourceSHA256"]
                or provenance.get("bundleBytes") != file(value["sourceArchive"]).stat().st_size
                or provenance.get("bundleFiles") != len(members)
                or not isinstance(provenance.get("preservedFiles"), dict) or set(provenance["preservedFiles"]) != members
                or not re.fullmatch(r"[0-9a-f]{64}", str(provenance.get("sourceSHA256", "")))
                or "source-bundle.json" not in value["buildFiles"]):
            raise ValueError("OpenMontage source bundle provenance does not match its pinned archive")
    build_files = value["buildFiles"]
    if not isinstance(build_files, list) or not 1 <= len(build_files) <= 128 or len(set(build_files)) != len(build_files):
        raise ValueError("Invalid upstream build file list")
    for item in build_files:
        relative(item)
        target = "upstream/" + item
        if target not in inputs and not any(path.startswith(target + "/") for path in inputs):
            raise ValueError("Missing upstream build input: " + item)
    relative(value["installScript"])
    if value["installScript"] not in build_files or "upstream/" + value["installScript"] not in inputs:
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

