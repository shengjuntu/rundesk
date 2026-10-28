---
name: edit-with-video-use
description: Edit footage using the independent upstream browser-use/video-use Skill, native EDL, helpers and output review in a RunDesk Codex container. Use Video App MCP for new TTS, images or generated clips while preserving the upstream editing workflow; do not substitute the self-developed Video Builder pipeline.
---

# Edit with upstream Video Use

Read `/opt/upstreams/video-use/SKILL.md` and its `helpers/` scripts. Use their native transcript, EDL, color, overlay, caption, render and self-review workflow. Keep project memory and outputs in the footage directory's `edit/` folder. Preserve original sources and the upstream Skill. This environment is separate from our `video-build` profile.

Apply these deployment overrides before the upstream machine-setup instructions:

- Treat the clone, dependencies, FFmpeg and Skill registration as preinstalled. Do not run `install.md`, `git pull`, package installers or credential setup just because a session starts. Do not write in `/opt/upstreams/video-use`.
- Use the connected `video_app_media` MCP for TTS, generated images and generated video. Read [generation-services.md](references/generation-services.md) for local-file delivery and recovery. Use the source text and requested changes to choose prompts and materials. Keep native editing decisions autonomous and faithful to the task.
- Keep the upstream strategy/confirmation and review rules. Use an already approved task brief as authorization only when the user supplied it; do not invent approval or impose a fixed scene template.
- Transcription is ASR, not TTS. Reuse genuine existing word-level transcripts. The native Scribe helpers require separately configured `ELEVENLABS_API_KEY` if transcription is actually needed; Video App generation does not provide that service. Do not request an ASR key for a task that uses no transcription. Never fabricate word timestamps from the script.
- Keep optional HyperFrames, Remotion and Manim animation choices as upstream options. They are not all preinstalled. Inspect actual availability before using one; no unsupported claims of animation parity.

Run the native renderer, for example:

```bash
python3 /opt/upstreams/video-use/helpers/render.py edit/edl.json -o edit/preview.mp4 --preview
```

Use real local media files returned by generation in the native source/overlay pipeline. Convert a still image into a real clip with FFmpeg when needed; do not invent an image-source field in the upstream EDL. For new voiceover, probe actual duration and compose/mix it with the footage using the upstream FFmpeg workflow; do not claim the EDL has an unsupported narration track. Keep exactly one authoritative native EDL and actual helper outputs; do not mirror decisions into Video App's private DSL or HyperFrames authoring document.

Review the actual output at cut boundaries with the native timeline view, check sound and captions, repair and render again where needed, then report the real `edit/final.mp4` path with its limitations. Preserve earlier accepted renders before replacing local outputs. Record source files, generated asset/job/request IDs, prompts, actual durations and review observations in `edit/project.md`. Do not claim Video App has archived an upstream final merely because a file exists in the RunDesk workspace.
