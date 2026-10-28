#!/usr/bin/env python3
"""Render and inspect a two-second local test video. No external assets or models."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--out", required=True, help="new output directory")
    args = p.parse_args()
    out = Path(args.out).absolute(); out.mkdir(parents=True, exist_ok=False)
    video = out / "video.mp4"
    command = ["ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-n", "-f", "lavfi", "-i", "color=c=navy:s=360x640:r=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", str(video)]
    receipt = {"schemaVersion": 1, "command": command, "passed": False}
    try:
        subprocess.run(command, capture_output=True, check=True, timeout=60)
        probe = json.loads(subprocess.check_output(["ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", str(video)], timeout=20))
        (out / "probe.json").write_text(json.dumps(probe, indent=2) + "\n")
        v = next(s for s in probe["streams"] if s["codec_type"] == "video")
        a = next(s for s in probe["streams"] if s["codec_type"] == "audio")
        receipt["passed"] = v["width"] == 360 and v["height"] == 640 and v["codec_name"] == "h264" and a["codec_name"] == "aac" and 1.9 <= float(probe["format"]["duration"]) <= 2.2
        receipt["sha256"] = hashlib.sha256(video.read_bytes()).hexdigest()
    except Exception as e:
        receipt["error"] = str(e)
    (out / "render.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt))
    return 0 if receipt["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
