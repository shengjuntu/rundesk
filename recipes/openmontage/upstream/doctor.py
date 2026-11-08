#!/usr/bin/env python3
"""Offline install checks; no model, network or production render is executed."""
import json
from pathlib import Path
import shutil
import sys


def main():
    root = Path("/opt/upstreams/openmontage")
    checks = [{"name": name, "ok": (root / name).is_file()} for name in ("LICENSE", "AGENT_GUIDE.md", "tools/audio/video_app_tts.py", "tools/graphics/video_app_image.py", "tools/video/video_app_video.py", "lib/rundesk_video_app.py", "RUNDESK-RUNTIME-PATCHES.json", "RUNDESK-INTEGRATION-LICENSE.txt")]
    checks += [{"name": cmd, "ok": shutil.which(cmd) is not None} for cmd in ("node", "ffmpeg", "ffprobe", "chromium", "hyperframes", "openmontage-python")]
    sys.path.insert(0, str(root))
    try:
        from tools.tool_registry import ToolRegistry
        import tools.audio.video_app_tts as tts
        import tools.graphics.video_app_image as image
        import tools.video.video_app_video as video
        registry = ToolRegistry()
        found = []
        for module in (tts, image, video):
            found.extend(registry.register_module(module))
        checks.append({"name": "native-registry-providers", "ok": sorted(found) == ["video_app_image", "video_app_tts", "video_app_video"]})
    except Exception as exc:
        checks.append({"name": "native-registry-providers", "ok": False, "error": type(exc).__name__})
    for package, expected in (("hyperframes", "0.8.87"), ("gsap", "3.15.0")):
        try:
            actual = json.loads((Path("/opt/hyperframes/node_modules") / package / "package.json").read_text())["version"]
            checks.append({"name": package, "ok": actual == expected, "version": actual})
        except (OSError, ValueError, KeyError):
            checks.append({"name": package, "ok": False})
    try:
        expected = json.loads((root / "remotion-composer/package-lock.json").read_text())["packages"]["node_modules/remotion"]["version"]
        actual = json.loads((root / "remotion-composer/node_modules/remotion/package.json").read_text())["version"]
        checks.append({"name": "locked-remotion", "ok": actual == expected, "version": actual})
    except (OSError, ValueError, KeyError):
        checks.append({"name": "locked-remotion", "ok": False})
    result = {"schemaVersion": 1, "profile": "openmontage", "ok": all(x["ok"] for x in checks), "checks": checks, "modelTested": False, "networkTested": False, "mediaRendered": False}
    print(json.dumps(result))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
