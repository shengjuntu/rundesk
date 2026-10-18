#!/usr/bin/env python3
"""Offline real-media check: image + silent generated-style clip + WAV + SRT."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import struct
import subprocess
import sys
import wave


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True)
    parser.add_argument("--video-use-root", default="/opt/video-use")
    args = parser.parse_args()
    root = Path(args.out).absolute()
    root.mkdir(parents=True, exist_ok=False)
    (root / "input").mkdir(); (root / "work").mkdir()
    (root / "input/image.ppm").write_bytes(b"P6\n180 320\n255\n" + bytes([24, 70, 140]) * 180 * 320)
    subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-n", "-f", "lavfi", "-i", "testsrc2=size=180x320:rate=12", "-t", "1.5", "-an", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", str(root / "input/generated-silent.mp4")], check=True, timeout=30)
    with wave.open(str(root / "input/narration.wav"), "wb") as f:
        f.setparams((1, 2, 24000, 0, "NONE", "not compressed"))
        f.writeframes(b"".join(struct.pack("<h", int(1500 * math.sin(2 * math.pi * 440 * i / 24000))) for i in range(48000)))
    (root / "input/subtitles.srt").write_text("1\n00:00:00,000 --> 00:00:01,000\nScript drives the picture.\n\n2\n00:00:01,000 --> 00:00:02,000\nContinuous narration, silent source clip.\n", encoding="utf-8")
    def entry(rel, **kwargs):
        path = root / rel
        return {"path": rel, "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size, **kwargs}
    manifest = {"schemaVersion": 1, "projectId": "offline-smoke", "productionRunId": "offline-smoke-run", "sourceHash": "a" * 64, "inputHash": "b" * 64,
                "media": [entry("input/image.ppm", assetId="image", type="image"), entry("input/generated-silent.mp4", assetId="generated", type="video")],
                "narration": entry("input/narration.wav", duration=2), "subtitles": entry("input/subtitles.srt")}
    edl = {"schemaVersion": 1, "canvas": {"width": 180, "height": 320, "fps": 12},
           "clips": [{"id": "opening", "assetId": "image", "path": "input/image.ppm", "type": "image", "start": 0, "duration": 1},
                     {"id": "explain", "assetId": "generated", "path": "input/generated-silent.mp4", "type": "video", "start": 1, "duration": 1, "sourceIn": .25}]}
    for path, value in ((root / "input/manifest.json", manifest), (root / "work/edl.json", edl)):
        path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")
    command = [sys.executable, str(Path(__file__).with_name("video_use_adapter.py")), "render", "--manifest", str(root / "input/manifest.json"), "--edl", str(root / "work/edl.json"), "--out", str(root / "artifacts/render-smoke"), "--video-use-root", args.video_use_root]
    p = subprocess.run(command, capture_output=True, text=True, timeout=120)
    result = {"ok": p.returncode == 0, "output": p.stdout[-3000:], "error": p.stderr[-3000:], "modelTested": False, "networkTested": False}
    print(json.dumps(result, ensure_ascii=False))
    return p.returncode


if __name__ == "__main__":
    raise SystemExit(main())
