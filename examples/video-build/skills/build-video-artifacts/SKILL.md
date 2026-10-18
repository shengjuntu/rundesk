---
name: build-video-artifacts
description: "Produce script-led videos in three stages: prepare and align material, edit, and refine. Use Video App MCP for TTS and image/video generation, pinned video-use for editing, and the script-media adapter for images, silent clips, continuous narration and subtitles."
---

# Produce a script-led video

Read `/opt/rundesk-profile/profile.lock.json`. Keep the supplied narration text as the content authority. Follow the selected App workflow and task requirements. Use three production stages; stage completion does not require stopping for approval when the user already requested a draft.

## Prepare and align material

Read the full script, inspect actual pictures and multiple timed frames from videos, and associate relevant text spans with asset IDs, selection reasons and video source ranges. Distinguish event evidence from illustrative/generated footage. Preserve uncertain provenance and missing coverage explicitly. Do not impose one sentence per shot or rewrite narration to fit available assets.

Use the configured Video App MCP services for TTS, image generation and video generation when needed. Poll durable jobs; inspect real results before adoption. Use actual narration timing; do not claim estimated sentence boundaries are forced word alignment. Prepare the production-run snapshot only after required assets and narration are ready. Read its `input/manifest.json`, `textPath` and `alignmentPath`. Preserve `input/` files and their hashes; use `work/` for edits.

## Edit

Treat `work/edl.json` as this backend's editing authority. Do not also write the App native edit DSL for the same render. Keep creative explanations in free text or `notes`; use the EDL only for edit timing and layout.

Verify adapter contract 2 with `python3 -I /opt/rundesk-profile/tools/video_use_adapter.py --version`. Associate relevant clips with optional `textRefs:[{"quote":"exact frozen narration","occurrence":1}]`; use several references per clip when appropriate, with at most 32 references per clip and 16384 UTF-8 bytes per quote. Copy exact text from `manifest.textPath`; the adapter verifies its original bytes against `manifest.sourceHash`, including whitespace and CRLF. For repeated quotes, count overlapping occurrences from 1 and specify `occurrence`; omit it only for a unique quote. Allow clips without references, and never impose a fixed scene or sentence partition. Preserve existing clip IDs and unrelated references during revisions.

For sequential images and clips with external continuous narration, create this baseline EDL with paths copied exactly from the input manifest:

```json
{"schemaVersion":1,"canvas":{"width":1080,"height":1920,"fps":30},"clips":[{"id":"opening","assetId":"asset_01","path":"input/media/asset_01.png","type":"image","start":0,"duration":3,"fit":"cover","textRefs":[{"quote":"发布了一款新芯片"}]},{"id":"scene","assetId":"asset_02","path":"input/media/asset_02.mp4","type":"video","start":3,"duration":4,"sourceIn":1,"fit":"contain","textRefs":[{"quote":"计划明年量产","occurrence":1}]}],"subtitles":{"fontName":"Noto Sans CJK SC","fontSize":18,"marginV":60}}
```

Adapt durations to actual narration. Cover its full timeline continuously, without gaps or overlaps; selected video ranges must exist. Image duration is flexible. Generated clips can be silent: source sound is deliberately replaced by the complete external narration. The adapter uses pinned upstream video-use extraction and concat, then applies narration and supplied SRT without cutting narration at each image change. It does not run Scribe or cloud ASR.

```bash
python3 /opt/rundesk-profile/tools/video_use_adapter.py render --manifest RUN_ROOT/input/manifest.json --edl RUN_ROOT/work/edl.json --out RUN_ROOT/artifacts/render-001
```

The adapter emits `final.mp4`, EDL snapshot, `validation.json`, `probe.json`, sample frames and an atomic `RUN_ROOT/completion.json`. Ask Video App to ingest the owned production run. Use its returned archived artifact IDs/URLs for review; mutable shared files are not the archive.

Baseline scope: one sequential visual track, cover/contain, external narration and SRT. It has no camera motion, overlays, transitions or background music. For advanced edits use a supported backend or implement and verify an explicit additional edit path; do not silently drop unsupported fields. Upstream examples live under `/opt/video-use`; its original footage/ASR workflow is reference material, not a requirement to transcribe an authored script or apply its strategy-approval gate.

## Refine

Locate a correction with `video_edit_locate(projectId,quote,occurrence?,renderId?)`, using exact narration text and a repeated quote's occurrence. Read its returned clip IDs and archived edit. Treat unmapped clips as unresolved; do not infer an association merely because they use the same asset. For a historical render inspect `current` and `sourceHash`; do not treat an old source as the current script. Apply user corrections locally and preserve unrelated clip IDs and references. Prepare a new immutable production run for a revised render and preserve previous drafts. Rebuild references from new frozen text when narration changes.

Inspect actual rendered frames at representative moments and cut boundaries. Listen to narration if an audio tool is available. Check text-to-picture relevance, captions, safe areas, pacing and audio. Metadata checks and `textReferences` coverage do not prove these properties. Record which checks were performed and what remains unverified. Final designation follows the App workflow.

Run the offline media check with `python3 /opt/rundesk-profile/tools/video_use_smoke.py --out NEW_DIRECTORY`. It proves the local adapter can render images plus silent clips with WAV and SRT, not that a real Codex production task has passed.
