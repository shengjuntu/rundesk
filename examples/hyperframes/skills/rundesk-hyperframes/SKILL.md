---
name: rundesk-hyperframes
description: "Use the independent upstream HyperFrames environment in RunDesk to create, inspect, animate, revise or render a video, while using Video App MCP for TTS and image/video generation. Preserve the upstream HyperFrames workflow and native HTML project."
---

# Run upstream HyperFrames with Video App media

Read `/opt/upstreams/hyperframes/skills/hyperframes/SKILL.md` and enter its actual workflow. Use the pinned upstream router, creation workflows and domain skills under that source directory, also available in `/etc/codex/skills`. Keep the upstream brief, storyboard, native HTML/CSS/JavaScript composition, review loop and local render project. Do not load `build-video-artifacts` or replace the native project with Video Builder DSL or Video App HyperFrames document schemas.

## Apply the environment integration

Use the installed `hyperframes` command in upstream examples in place of `npx hyperframes`. The CLI and skills are locked to 0.8.87. All 21 upstream skills are already installed; skip upstream `skills update`, `skills add`, freshness checks and installation steps. Report a missing file instead of silently fetching main or latest. Keep source, installed skills and package locks read-only; work in the task workspace.

Load the brief and assets through the configured `video_app_media` MCP server, or inspect the actual interface with:

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py discovery
python3 /opt/rundesk-profile/tools/video_app_media.py call video_project_get --arguments '{}'
```

Use the provided `VIDEO_APP_PROJECT_ID`, `VIDEO_APP_MCP_URL` and `VIDEO_APP_TOKEN` only at runtime. Never write the token to project files, prompts, logs or source. If binding or service configuration is missing, report the missing value rather than switching project or provider.

## Replace generation providers only

Route **all new narration/TTS, image generation and video generation** to Video App MCP. This integration overrides upstream media-use recommendations for HeyGen voice/video, Kokoro, mflux, LTX, Codex image generation and other provider-specific synthesis. Do not sign into those providers or install local generation models as a fallback. Video App does not supply BGM/SFX generation in this integration; use existing local music/sound when available and report missing material rather than choosing another model. Keep HyperFrames composition, animation, media ingest, analysis, preview, check and render native.

Use the actual discovered MCP schemas. The companion CLI has these equivalent commands:

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py tts --text 'Exact narration' --revision REVISION --request-id REQUEST_ID --output ./media/narration.wav
python3 /opt/rundesk-profile/tools/video_app_media.py image --prompt 'Requested visual' --revision REVISION --request-id REQUEST_ID --output ./media/visual.png
python3 /opt/rundesk-profile/tools/video_app_media.py video --prompt 'Requested motion' --revision REVISION --request-id REQUEST_ID --output ./media/clip.mp4
```

Replace REVISION with the actual current Video App revision and REQUEST_ID with one stable 24-hex request identity per generation. Preserve the original request body and identity on retry. If the service accepts a job and times out, inspect that job with `job-get --job-id ID`; do not create a replacement request or cancel it implicitly. Capability/configuration errors are visible blockers, not permission to try upstream cloud providers.

Use the returned downloaded media files inside the native HyperFrames project. Preserve request/job IDs, asset IDs where returned, hashes and source associations in the project media records. Read measured duration with ffprobe, inspect real images/video and align narration against actual audio; a successful job is not a semantic quality review. Use native media-use `--from`/ingest when useful to record an existing local generated file; do not invoke its generation fallback.

## Finish with native evidence

Run the upstream check/preview/render and review actual frames and sound. Revise the existing native project for follow-up edits and preserve unrelated accepted work. Return the workspace-relative native project and completed MP4 paths plus actual check/render evidence. Use only an available documented archive/import operation for Video App output history; do not invent a Video App render ID or mark an unimported local result as archived there.
