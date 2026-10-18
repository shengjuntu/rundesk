#!/usr/bin/env python3
"""Render a script-led EDL with pinned video-use and continuous external narration.

No transcription, cloud model, shell command interpolation or network is required.
See examples/video-build/README.md for the input/EDL/completion contract.
"""
from __future__ import annotations

import argparse
import contextlib
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace

PIN = "43cfc566833548093455c9d4a46bd69913b7e552"
COMMANDS = []
SAFE_FORMATS = "mov,matroska,avi,mpegts,png_pipe,jpeg_pipe,webp_pipe,bmp_pipe,tiff_pipe,gif,ppm_pipe,wav,flac,ogg,aac,mp3"
SAFE_INPUT_OPTIONS = ["-protocol_whitelist", "file,pipe", "-format_whitelist", SAFE_FORMATS]


class RenderError(ValueError):
    pass


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def read_json(path):
    if path.stat().st_size > 4 * 1024 * 1024:
        raise RenderError("JSON file exceeds 4 MiB")
    return json.loads(path.read_text(encoding="utf-8"), parse_constant=lambda v: (_ for _ in ()).throw(RenderError("nonfinite JSON number")))


def write_json(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")


def inside(root, relative, existing=True):
    if not isinstance(relative, str) or not relative or "\\" in relative or "\x00" in relative:
        raise RenderError("Expected relative POSIX path")
    parts = PurePosixPath(relative)
    if parts.is_absolute() or any(p in ("", ".", "..") for p in relative.split("/")):
        raise RenderError("Path must remain inside the production run: " + relative)
    current = root
    for part in parts.parts:
        current = current / part
        if current.is_symlink():
            raise RenderError("Symlinks are not allowed: " + relative)
    if not current.resolve().is_relative_to(root):
        raise RenderError("Path escapes production run")
    if existing and not current.is_file():
        raise RenderError("Missing regular input: " + relative)
    return current


def number(value, name, minimum=0, maximum=600):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not minimum <= value <= maximum:
        raise RenderError("Invalid " + name)
    return float(value)


def run(argv, **kwargs):
    argv = [str(x) for x in argv]
    if Path(argv[0]).name == "ffmpeg":
        # Fixed limits also apply to the upstream helper subprocesses.
        argv[1:1] = ["-nostdin", "-threads", "2", "-filter_threads", "1", "-filter_complex_threads", "1"]
        # Encoder thread count is an output option, before the output filename.
        if argv[-1] != "-" and not argv[-1].startswith("-"):
            argv[-1:-1] = ["-threads", "2"]
    COMMANDS.append(argv)
    kwargs.setdefault("timeout", 1800)
    kwargs.setdefault("check", True)
    if not kwargs.get("capture_output"):
        kwargs.setdefault("stdout", subprocess.PIPE)
        kwargs.setdefault("stderr", subprocess.PIPE)
    try:
        return subprocess.run(argv, **kwargs)
    except subprocess.CalledProcessError as exc:
        error = exc.stderr or b""
        if isinstance(error, bytes):
            error = error.decode("utf-8", "replace")
        raise RenderError("Command failed: " + str(error)[-4000:]) from exc


def probe(path):
    # A hashed local playlist can otherwise load unhashed files or remote URLs.
    # Restrict both probing and decoding to self-contained media containers.
    return json.loads(run(["ffprobe", "-v", "error", *SAFE_INPUT_OPTIONS, "-enable_drefs", "0", "-show_format", "-show_streams", "-of", "json", path]).stdout)


def decode_options(info):
    selected = info.get("format", {}).get("format_name", "").split(",")[0]
    if selected not in SAFE_FORMATS.split(","):
        raise RenderError("Input is not a self-contained supported media container")
    return [*SAFE_INPUT_OPTIONS, "-f", selected] + (["-enable_drefs", "0"] if selected == "mov" else [])


def duration(info):
    return number(float(info.get("format", {}).get("duration", 0)), "media duration", .001, 36000)


def visual_duration(info):
    stream = next((s for s in info.get("streams", []) if s.get("codec_type") == "video"), {})
    try:
        return number(float(stream.get("duration", "nan")), "visual duration", .001, 36000)
    except (ValueError, TypeError):
        pass
    # Matroska/WebM often lacks stream.duration but carries its stream-specific
    # duration tag. Container duration may include a much longer audio track.
    tag = stream.get("tags", {}).get("DURATION", "")
    match = re.fullmatch(r"(\d+):([0-5]\d):([0-5]\d(?:\.\d+)?)", tag)
    if match:
        hours, minutes, seconds = match.groups()
        return number(int(hours) * 3600 + int(minutes) * 60 + float(seconds), "visual duration", .001, 36000)
    raise RenderError("Cannot verify selected video stream duration; normalize the input video first")


def verify_file(root, record, private=None):
    if not isinstance(record, dict):
        raise RenderError("Invalid input record")
    path = inside(root, record.get("path"))
    if private is not None:
        # Traverse with rooted, non-following descriptors and copy verified bytes
        # to a private rendering directory. Later renames/symlinks in the shared
        # workspace cannot change what FFprobe or FFmpeg actually reads.
        expected_hash, expected_size = record.get("sha256"), record.get("size")
        if not isinstance(expected_hash, str) or not re.fullmatch(r"[0-9a-f]{64}", expected_hash) or type(expected_size) is not int or not 0 < expected_size <= 2 * 1024**3:
            raise RenderError("Input requires bounded size and SHA-256")
        directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
        source_fd = None
        target = private / (os.urandom(8).hex() + path.suffix)
        try:
            parts = PurePosixPath(record["path"]).parts
            for part in parts[:-1]:
                following = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
                os.close(directory); directory = following
            source_fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
            details = os.fstat(source_fd)
            if not stat.S_ISREG(details.st_mode) or details.st_size != expected_size:
                raise RenderError("Input size changed: " + record["path"])
            digest, copied = hashlib.sha256(), 0
            with os.fdopen(source_fd, "rb") as source, target.open("xb") as output:
                source_fd = None
                for block in iter(lambda: source.read(1024 * 1024), b""):
                    copied += len(block)
                    if copied > expected_size:
                        raise RenderError("Input grew while copying")
                    output.write(block); digest.update(block)
            if copied != expected_size or digest.hexdigest() != expected_hash:
                raise RenderError("Input digest changed: " + record["path"])
            return target
        finally:
            if source_fd is not None:
                os.close(source_fd)
            os.close(directory)
    if "size" in record and path.stat().st_size != record["size"]:
        raise RenderError("Input size changed: " + record["path"])
    if "sha256" in record and sha256(path) != record["sha256"]:
        raise RenderError("Input digest changed: " + record["path"])
    return path


def upstream_module(path):
    path = path.resolve()
    if not (path / "helpers/render.py").is_file():
        raise RenderError("Pinned video-use helpers are missing")
    pinfile = path / "RUNDESK_PIN.json"
    if not pinfile.is_file() or read_json(pinfile).get("commit") != PIN:
        raise RenderError("video-use pin metadata does not match runtime contract")
    sys.path.insert(0, str(path / "helpers"))
    spec = importlib.util.spec_from_file_location("rundesk_vendor_video_use_render", path / "helpers/render.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.subprocess = SimpleNamespace(run=run, PIPE=subprocess.PIPE, DEVNULL=subprocess.DEVNULL,
                                        CalledProcessError=subprocess.CalledProcessError)
    return module


def srt_check(path, total):
    text = path.read_text(encoding="utf-8-sig")
    if not text.strip() or len(text.encode()) > 1024 * 1024:
        raise RenderError("Subtitles must be nonempty UTF-8 SRT below 1 MiB")
    pattern = r"(\d{2}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2}):(\d{2}):(\d{2}),(\d{3})"
    times = re.findall(pattern, text)
    if not times:
        raise RenderError("No SRT timestamps found")
    last = 0.0
    for fields in times:
        values = [int(v) for v in fields]
        if any(values[i] > 59 for i in (1, 2, 5, 6)):
            raise RenderError("Invalid subtitle timestamp")
        start = values[0] * 3600 + values[1] * 60 + values[2] + values[3] / 1000
        end = values[4] * 3600 + values[5] * 60 + values[6] + values[7] / 1000
        if start < last - .001 or end <= start or end > total + .1:
            raise RenderError("Subtitle timeline is invalid or exceeds narration")
        last = end
    return len(times)


def artifact(root, path, kind, mime, ident):
    return {"id": ident, "kind": kind, "path": path.relative_to(root).as_posix(),
            "sha256": sha256(path), "size": path.stat().st_size, "mimeType": mime}


def extract_visual_segment(upstream, normalized, length, work, index, width, height, fps):
    segment = work / f"segment_{index:03d}.mp4"
    with contextlib.redirect_stdout(sys.stderr):
        upstream.extract_segment(normalized, 0, length, f"scale={width}:{height},setsar=1", segment, rate=str(fps))
    # Temporary AAC is solely for the upstream extraction helper's assumptions.
    # Strip it before concat so codec padding cannot move visual cut points.
    visual = work / f"visual_{index:03d}.mp4"
    run(["ffmpeg", "-n", "-i", segment, "-map", "0:v:0", "-an", "-c:v", "copy", visual])
    return visual


def render_snapshot(args, private):
    COMMANDS.clear()
    manifest_path = Path(args.manifest).absolute()
    root = Path(args.run_root).resolve() if args.run_root else manifest_path.parent.parent.resolve()
    manifest_path = inside(root, manifest_path.relative_to(root).as_posix())
    manifest = read_json(manifest_path)
    if manifest.get("schemaVersion") != 1:
        raise RenderError("Unsupported input schema")
    for key in ("projectId", "productionRunId", "sourceHash", "inputHash"):
        if not isinstance(manifest.get(key), str) or not manifest[key]:
            raise RenderError("Missing input identity: " + key)
    edl_path = Path(args.edl).absolute()
    edl_path = inside(root, edl_path.relative_to(root).as_posix())
    edl = read_json(edl_path)
    if not isinstance(edl, dict) or set(edl) - {"schemaVersion", "canvas", "clips", "subtitles", "notes"} or edl.get("schemaVersion") != 1:
        raise RenderError("Unsupported EDL fields/schema")
    canvas = edl.get("canvas", {})
    if set(canvas) != {"width", "height", "fps"}:
        raise RenderError("Expected canvas width/height/fps")
    width, height = canvas["width"], canvas["height"]
    if any(type(n) is not int or n < 64 or n > 3840 or n % 2 for n in (width, height)):
        raise RenderError("Canvas dimensions must be even integers 64..3840")
    fps = number(canvas["fps"], "fps", 1, 60)
    narration = verify_file(root, manifest.get("narration"), private)
    audio_info = probe(narration)
    if not any(s.get("codec_type") == "audio" for s in audio_info.get("streams", [])):
        raise RenderError("Narration has no audio stream")
    total = duration(audio_info)
    if total > 600:
        raise RenderError("Narration exceeds 600 seconds")
    media = {}
    for record in manifest.get("media", []):
        if record.get("path") in media:
            raise RenderError("Duplicate input media path")
        media[record["path"]] = (record, verify_file(root, record, private))
    clips = edl.get("clips")
    if not isinstance(clips, list) or not 1 <= len(clips) <= 300:
        raise RenderError("Expected 1..300 clips")
    cursor, ids = 0.0, set()
    validated = []
    for clip in clips:
        if not isinstance(clip, dict) or set(clip) - {"id", "assetId", "path", "type", "start", "duration", "sourceIn", "fit", "note"}:
            raise RenderError("Unknown clip fields")
        ident = clip.get("id")
        if not isinstance(ident, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,80}", ident) or ident in ids:
            raise RenderError("Invalid or duplicate clip id")
        ids.add(ident)
        start = number(clip.get("start"), "clip start")
        length = number(clip.get("duration"), "clip duration", 1 / fps)
        offset = number(clip.get("sourceIn", 0), "sourceIn", 0, 36000)
        if abs(start - cursor) > .001:
            raise RenderError("Clips must continuously cover the timeline without gaps/overlaps")
        if clip.get("path") not in media or clip.get("type") not in ("image", "video"):
            raise RenderError("Clip must reference declared image/video input")
        record, source = media[clip["path"]]
        if record.get("type") in ("image", "video") and record["type"] != clip["type"]:
            raise RenderError("Clip type does not match declared input type")
        if clip.get("assetId") and clip["assetId"] != record.get("assetId"):
            raise RenderError("Clip assetId does not match input path")
        if clip.get("fit", "cover") not in ("cover", "contain"):
            raise RenderError("Invalid clip fit")
        info = probe(source)
        if not any(s.get("codec_type") == "video" for s in info.get("streams", [])):
            raise RenderError("Visual input has no video/image stream")
        if clip["type"] == "video" and offset + length > visual_duration(info) + 1 / fps:
            raise RenderError("Selected source range exceeds video duration")
        if clip["type"] == "image" and offset != 0:
            raise RenderError("Still images have no sourceIn offset")
        # Quantize boundaries on the global frame grid, rather than rounding
        # every duration independently and accumulating a frame per cut.
        frames = round((cursor + length) * fps) - round(cursor * fps)
        if frames < 1:
            raise RenderError("Clip has no frame on the output timeline")
        validated.append((clip, source, frames / fps, offset, decode_options(info)))
        cursor += length
    if abs(cursor - total) > 1 / fps + .001:
        raise RenderError("Visual coverage must match actual narration duration within one frame")
    style = edl.get("subtitles", {})
    if not isinstance(style, dict) or set(style) - {"fontName", "fontSize", "marginV"}:
        raise RenderError("Unknown subtitle style fields")
    font = style.get("fontName", "Noto Sans CJK SC")
    if not isinstance(font, str) or not re.fullmatch(r"[A-Za-z0-9 _-]{1,80}", font):
        raise RenderError("Invalid font name")
    font_size = number(style.get("fontSize", 18), "fontSize", 8, 100)
    margin = number(style.get("marginV", 60), "marginV", 0, 200)
    subtitle_record = manifest.get("subtitles")
    subtitles = verify_file(root, subtitle_record, private) if isinstance(subtitle_record, dict) and subtitle_record.get("path") else None
    cue_count = srt_check(subtitles, total) if subtitles else 0
    output = Path(args.out).absolute()
    output = inside(root, output.relative_to(root).as_posix(), existing=False)
    if output.relative_to(root).parts[0] != "artifacts" or output.exists():
        raise RenderError("Output must be a new directory under artifacts/")
    if (root / "completion.json").exists():
        raise RenderError("This production run already has completion.json; prepare a new run for revision")
    upstream = upstream_module(Path(args.video_use_root))
    output.mkdir(parents=True)
    # Archive the exact parsed edit used by this render. The Agent may edit
    # work/edl.json while a long render runs; it must not change the archive.
    frozen_edl = output / "edl.json"
    write_json(frozen_edl, edl)
    edl_hash = sha256(frozen_edl)
    validation = {"schemaVersion": 1, "engine": "video_use", "upstreamCommit": PIN,
                  **{k: manifest[k] for k in ("projectId", "productionRunId", "sourceHash", "inputHash")},
                  "edlHash": edl_hash, "narrationSHA256": sha256(narration),
                  "checks": [], "visualInspected": False, "audioListened": False,
                  "limitations": ["Metadata and decoded sample frames do not prove semantic fit, readable captions or narration quality.",
                                  "Baseline adapter supports sequential image/video cuts; advanced layers, transitions and motion require another edit implementation."]}
    try:
        with tempfile.TemporaryDirectory(prefix="render-work-", dir=private) as temporary:
            work = Path(temporary)
            segments = []
            for i, (clip, source, length, offset, source_options) in enumerate(validated):
                normalized = work / f"normalized_{i:03d}.mp4"
                scale = f"scale={width}:{height}:force_original_aspect_ratio={'increase' if clip.get('fit', 'cover') == 'cover' else 'decrease'}"
                scale += f",crop={width}:{height}" if clip.get("fit", "cover") == "cover" else f",pad={width}:{height}:(ow-iw)/2:(oh-ih)/2:black"
                cmd = ["ffmpeg", "-n"]
                if clip["type"] == "image":
                    cmd += ["-loop", "1", "-framerate", str(fps)]
                else:
                    cmd += ["-ss", str(offset)]
                cmd += [*source_options, "-i", source, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-map", "0:v:0", "-map", "1:a:0",
                        "-t", f"{length:.9f}", "-vf", scale + ",setsar=1,format=yuv420p,tpad=stop_mode=clone:stop_duration=1", "-r", str(fps),
                        "-c:v", "libx264", "-preset", "ultrafast", "-crf", "18", "-c:a", "aac", "-ar", "48000", "-ac", "2", normalized]
                run(cmd)
                # Genuine upstream extraction and lossless concat, with a final scale
                # appended to its grade chain to retain our explicit canvas.
                segments.append(extract_visual_segment(upstream, normalized, length, work, i, width, height, fps))
            base = work / "base.mp4"
            with contextlib.redirect_stdout(sys.stderr):
                upstream.concat_segments(segments, base, work)
            vf = f"scale={width}:{height},setsar=1"
            if subtitles:
                shutil.copyfile(subtitles, work / "captions.srt")
                vf += f",subtitles=filename=captions.srt:force_style='FontName={font},FontSize={font_size:g},MarginV={margin:g},Outline=2,Shadow=0,Alignment=2'"
            final = output / "final.mp4"
            run(["ffmpeg", "-n", "-i", base, *decode_options(audio_info), "-i", narration, "-map", "0:v:0", "-map", "1:a:0", "-vf", vf,
                 "-af", f"apad,atrim=duration={total:.9f},asetpts=PTS-STARTPTS", "-t", f"{total:.9f}", "-r", str(fps),
                 "-c:v", "libx264", "-preset", "fast", "-crf", "20", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ar", "48000", "-b:a", "192k",
                 "-movflags", "+faststart", final], cwd=work)
        info = probe(final)
        write_json(output / "probe.json", info)
        video_stream = next((s for s in info["streams"] if s.get("codec_type") == "video"), {})
        audio_stream = next((s for s in info["streams"] if s.get("codec_type") == "audio"), {})
        actual = duration(info)
        checks = {"dimensions": video_stream.get("width") == width and video_stream.get("height") == height,
                  "videoCodec": video_stream.get("codec_name") == "h264", "audioCodec": audio_stream.get("codec_name") == "aac",
                  "duration": abs(actual - total) <= max(.12, 2 / fps), "fileNonempty": final.stat().st_size > 0}
        try:
            from fractions import Fraction
            checks["frameRate"] = abs(float(Fraction(video_stream["avg_frame_rate"])) - fps) < .001
        except (KeyError, ValueError, ZeroDivisionError):
            checks["frameRate"] = False
        validation["checks"] = [{"name": k, "ok": v} for k, v in checks.items()]
        validation.update(narrationDuration=total, actualDuration=actual, subtitleCues=cue_count)
        frames = []
        for i, t in enumerate(sorted(set([min(.05, total / 2), total / 2, max(0, total - max(.1, 1 / fps))]))):
            frame = output / f"sample_{i:02d}.png"
            run(["ffmpeg", "-n", "-ss", str(t), "-i", final, "-frames:v", "1", "-vf", "scale=480:480:force_original_aspect_ratio=decrease", frame])
            if not frame.is_file() or not frame.stat().st_size:
                raise RenderError("Output sample frame could not be decoded")
            frames.append((frame, t))
        validation["sampleFrames"] = [{"path": p.relative_to(root).as_posix(), "time": t} for p, t in frames]
        validation["commands"] = COMMANDS.copy()
        validation["passed"] = bool(checks) and all(checks.values())
        write_json(output / "validation.json", validation)
        if not validation["passed"]:
            raise RenderError("Rendered output failed metadata checks")
        completion = {"schemaVersion": 1, **{k: manifest[k] for k in ("projectId", "productionRunId", "sourceHash", "inputHash")},
                      "engine": "video_use", "upstreamCommit": PIN,
                      "video": {"duration": actual, "width": width, "height": height, "fps": fps, "videoCodec": "h264", "audioCodec": "aac"},
                      "artifacts": [artifact(root, final, "video", "video/mp4", "video"),
                                    artifact(root, output / "edl.json", "edit", "application/json", "edit"),
                                    artifact(root, output / "validation.json", "validation", "application/json", "validation"),
                                    artifact(root, output / "probe.json", "probe", "application/json", "probe")]
                      + [artifact(root, p, "frame", "image/png", f"frame_{i}") for i, (p, _) in enumerate(frames)]}
        # Publish atomically, without replacing a previously completed run.
        temporary = root / (".completion-" + os.urandom(8).hex() + ".json")
        try:
            write_json(temporary, completion)
            os.link(temporary, root / "completion.json")
        finally:
            temporary.unlink(missing_ok=True)
        return {"ok": True, "completion": str(root / "completion.json"), "video": str(final), "validation": str(output / "validation.json")}
    except Exception as exc:
        write_json(output / "failure.json", {"ok": False, "error": str(exc), "commands": COMMANDS})
        raise


def render(args):
    manifest_path = Path(args.manifest).absolute()
    root = Path(args.run_root).resolve() if args.run_root else manifest_path.parent.parent.resolve()
    # RunDesk's /tmp is a small tmpfs. Keep verified copies/intermediates on the
    # mounted task disk rather than consuming the container's memory budget.
    work = inside(root, "work", existing=False)
    work.mkdir(exist_ok=True)
    if not work.is_dir():
        raise RenderError("work must be a directory")
    with tempfile.TemporaryDirectory(prefix="render-private-", dir=work) as temporary:
        return render_snapshot(args, Path(temporary))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["render"])
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--edl", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--run-root")
    parser.add_argument("--video-use-root", default="/opt/video-use")
    args = parser.parse_args()
    try:
        result = render(args)
    except (RenderError, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as exc:
        print(json.dumps({"ok": False, "error": str(exc)}, ensure_ascii=False), file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
