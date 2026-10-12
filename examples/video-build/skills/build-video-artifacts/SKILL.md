---
name: build-video-artifacts
description: Build local video artifacts from supplied or generated images, audio and subtitles using FFmpeg and Pillow. Use for short-video assembly, resizing, captions and render verification; does not generate speech, perform neural video generation or publish videos.
---

# Build and verify video artifacts

Read /opt/rundesk-profile/profile.lock.json. Work in the task's project directory. Create a new output folder and preserve input assets and previous renders.

1. Inspect each input with ffprobe or Pillow. Resolve the requested aspect ratio, duration, frame rate, audio and subtitle language from the task. Default to a reviewable 9:16 short-video preview only when no format was specified, and record that assumption.
2. Create render.json listing input paths, hashes, timing, output dimensions, codecs, font selection and exact command arrays. Do not treat a missing narration or image as present.
3. Use installed FFmpeg, ffprobe, Pillow and Noto CJK fonts. Select a real installed font with fc-match before rendering Chinese captions. Pass commands as argument arrays in Python; avoid interpolating untrusted text into shell commands or FFmpeg filter strings.
4. Write subtitles to a separate UTF-8 ASS/SRT file. Validate timings and quote/escape paths using the chosen tool's rules. Keep final output outside the input asset paths, and use non-overwriting output behavior.
5. Render video.mp4 and save stderr logs. Capture ffprobe JSON into probe.json. Check duration, dimensions, frame rate, video/audio streams and file size against the intended render specification.
6. Extract representative frames and inspect them when an image viewer is available. Listen to representative audio when possible. Record any unverified visual/audio properties; ffprobe metadata alone does not prove readable subtitles or correct narration.
7. Return the output paths and checks performed. Preserve partial artifacts and error evidence on failure.

For a dependency smoke test, run `python3 /opt/rundesk-profile/tools/video_smoke.py --out NEW_DIRECTORY`. It generates a two-second 360×640 H.264/AAC test video without external assets; it does not validate a real production render. This profile supplies neither a TTS engine nor publishing credentials.
