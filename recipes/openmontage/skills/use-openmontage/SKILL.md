---
name: use-openmontage
description: Produce or revise videos with the separately installed upstream OpenMontage pipelines, stage directors, native artifacts and renderers. Use for OpenMontage production, comparison with the independent Video Builder, and pipeline media generated through Video App MCP.
---

# Use OpenMontage

Keep this profile independent of the self-developed Video Builder. Use original OpenMontage pipelines, directors, checkpoints and composition tools. Do not translate production into Video App's editing DSL or require Video Builder's project schema.

Read `/opt/upstreams/openmontage/AGENT_GUIDE.md`, then the selected `pipeline_defs/*.yaml` and named stage directors. Resolve upstream-relative references against that source and read its Layer 3 skills when relevant. Preserve original review and approval behavior; do not silently choose another composition engine.

Run native Python with `openmontage-python` from the RunDesk working directory. The wrapper prepares a writable pinned native source/composer under the working directory and selects its imports. Keep assets, checkpoints and renders under the persistent working directory. Do not attempt production writes in read-only `/opt`.

Discover Video App MCP capabilities and read the bound project/revision before generation. Use Video App as the media service, not as the editing engine. Keep vendor credentials and model configuration in Video App rather than requesting them in the OpenMontage `.env`.

Read [Video App provider contract](references/video-app-providers.md) before the first generation. Route TTS through `video_app_tts`, images through `video_app_image`, and generated clips through `video_app_video`. These native BaseTool providers are discovered by the original registry. With selectors, set exact `preferred_tool` and `allowed_providers=["video-app"]`; never silently fall back to Piper or another generator. Upstream prompting guides for another vendor do not override this routing.

Save explicit request_id and original revision with every generation; recover that request on uncertainty. Record verified local media paths and the Video App receipt in the native asset manifest, then continue native OpenMontage editing/rendering. Preserve previous completed versions for comparisons.

Inspect the actual rendered video with the selected upstream renderer. Distinguish configuration, generated media and completed video. Automatic ingestion of native final videos into Video App is not provided here: retain the local MP4 and native export/archive unless an explicit supported import tool is available.
