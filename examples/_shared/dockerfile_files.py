"""Read editable Dockerfiles; only resolve build identity defaults and labels."""
import json
from pathlib import Path
import re


def render(root, profile, selection, codex_version, base_image, destination, write, labels):
    filename = "Dockerfile"
    if "agent-reach" in profile["optionalComponents"] and not selection["agentReach"]:
        filename += ".lite"
    elif profile["id"] == "video-build" and selection.get("videoUse"):
        filename += ".video-use"
    path = root / profile["id"] / filename
    if not path.is_file() or any(p.is_symlink() for p in (path, *path.parents)):
        raise ValueError("Missing or unsafe external Dockerfile: " + str(path))
    text = path.read_text(encoding="utf-8")
    for name, value in (("RD_BASE_IMAGE", base_image), ("CODEX_VERSION", codex_version)):
        pattern = re.compile(r"^ARG " + name + r"=.*$", re.MULTILINE)
        if len(pattern.findall(text)) != 1:
            raise ValueError("External Dockerfile requires one exact ARG default: " + name)
        text = pattern.sub(lambda _: "ARG " + name + "=" + value, text)
    for name, value in labels.items():
        pattern = re.compile(r"^LABEL " + re.escape(name) + r"=.*$", re.MULTILINE)
        if len(pattern.findall(text)) != 1:
            raise ValueError("External Dockerfile requires one identity label: " + name)
        text = pattern.sub(lambda _: "LABEL " + name + "=" + json.dumps(value, ensure_ascii=False), text)
    write(destination / "Dockerfile", text)
