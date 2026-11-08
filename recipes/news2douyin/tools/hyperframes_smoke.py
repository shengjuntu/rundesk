#!/usr/bin/env python3
"""Render a local HTML/GSAP animation with the installed HyperFrames CLI; no models or downloads."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="new output directory")
    parser.add_argument("--runtime", default=os.environ.get("RUNDESK_HYPERFRAMES_ROOT", "/opt/hyperframes"))
    parser.add_argument("--font", help="installed font file; defaults to fontconfig Noto Sans CJK SC")
    args = parser.parse_args()
    out = Path(args.out).absolute(); out.mkdir(parents=True, exist_ok=False)
    runtime = Path(args.runtime).absolute()
    browser = os.environ.get("HYPERFRAMES_BROWSER_PATH", "/usr/bin/chromium")
    receipt = {"schemaVersion": 1, "engine": "hyperframes", "passed": False, "modelTested": False,
               "networkRequired": False, "browserPath": browser, "runtime": str(runtime), "commands": []}
    env = dict(os.environ, HYPERFRAMES_BROWSER_PATH=browser, HYPERFRAMES_NO_TELEMETRY="1", DO_NOT_TRACK="1")
    def invoke(argv, timeout=120):
        completed = subprocess.run(argv, env=env, capture_output=True, text=True, timeout=timeout)
        receipt["commands"].append({"argv": argv, "exitCode": completed.returncode, "output": (completed.stdout + completed.stderr)[-8000:]})
        if completed.returncode:
            raise RuntimeError("command failed: " + " ".join(argv))
        return completed.stdout
    try:
        if not Path(browser).is_file():
            raise RuntimeError("No installed browser at HYPERFRAMES_BROWSER_PATH; refusing an implicit browser download")
        cli = runtime / "node_modules/hyperframes/bin/hyperframes.mjs"
        gsap = runtime / "node_modules/gsap/dist/gsap.min.js"
        for package in ("hyperframes", "gsap"):
            receipt.setdefault("versions", {})[package] = json.loads((runtime / "node_modules" / package / "package.json").read_text())["version"]
        project = out / "project"; project.mkdir()
        shutil.copyfile(gsap, project / "gsap.min.js")
        if args.font:
            font = Path(args.font).absolute()
        else:
            font = Path(invoke(["fc-match", "-f", "%{file}", "Noto Sans CJK SC"]).strip())
            family = invoke(["fc-match", "-f", "%{family}", "Noto Sans CJK SC"]).strip()
            if "Noto Sans CJK" not in family:
                raise RuntimeError("Noto CJK font missing; fontconfig resolved " + family)
        shutil.copyfile(font, project / ("font" + font.suffix))
        receipt["fontFile"] = str(font)
        (project / "index.html").write_text('''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
@font-face{font-family:SmokeFont;src:url("SMOKE_FONT")}*{box-sizing:border-box}body{margin:0;background:#142036}#root{position:relative;width:320px;height:240px;background:#142036;overflow:hidden;color:white;font-family:SmokeFont,sans-serif}h1{position:absolute;left:30px;top:35px;margin:0;font-size:24px}#bar{position:absolute;left:30px;top:105px;width:60px;height:32px;background:#6fa8ff}
</style><script src="gsap.min.js"></script></head><body><div id="root" data-composition-id="smoke" data-start="0" data-duration="1" data-width="320" data-height="240" data-fps="10"><h1>新闻动画验证</h1><div id="bar"></div></div><script>
const timeline=gsap.timeline({paused:true});timeline.to("#bar",{x:160,duration:1,ease:"none"},0);window.__timelines={smoke:timeline};
</script></body></html>'''.replace("SMOKE_FONT", "font" + font.suffix), encoding="utf-8")
        (project / "hyperframes.json").write_text(json.dumps({"name": "rundesk-offline-smoke", "version": "1.0.0"}) + "\n")
        invoke(["node", str(cli), "--version"])
        # No strict-all: nonfatal layout recommendations are retained in the report.
        invoke(["node", str(cli), "check", str(project), "--json", "--samples", "2"])
        video = out / "animation.mp4"
        invoke(["node", str(cli), "render", str(project), "--output", str(video), "--fps", "10", "--workers", "1", "--quality", "draft", "--no-browser-gpu", "--no-best-effort", "--strict"], timeout=180)
        probe = json.loads(invoke(["ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", str(video)]))
        (out / "probe.json").write_text(json.dumps(probe, indent=2) + "\n")
        stream = next(s for s in probe["streams"] if s["codec_type"] == "video")
        # Distinct decoded frames prove an animation, rather than just a valid static MP4.
        hashes = invoke(["ffmpeg", "-nostdin", "-v", "error", "-i", str(video), "-map", "0:v:0", "-f", "framemd5", "-"])
        rows = [line.rsplit(",", 1)[-1].strip() for line in hashes.splitlines() if line and not line.startswith("#")]
        receipt.update(duration=float(probe["format"]["duration"]), width=stream["width"], height=stream["height"],
                       videoCodec=stream["codec_name"], frameCount=len(rows), distinctFrames=len(set(rows)), sha256=hashlib.sha256(video.read_bytes()).hexdigest())
        receipt["passed"] = (receipt["width"], receipt["height"], receipt["videoCodec"]) == (320, 240, "h264") and .9 <= receipt["duration"] <= 1.2 and receipt["distinctFrames"] > 1
    except (OSError, ValueError, KeyError, StopIteration, RuntimeError, subprocess.TimeoutExpired) as exc:
        receipt["error"] = str(exc)
    (out / "render.json").write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(receipt, ensure_ascii=False))
    return 0 if receipt["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
