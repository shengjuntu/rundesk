# Shared Video App generation services

Use the runtime `video_app_media` MCP for capability/project reads, generation jobs, polling, cancellation and local media download. The service is scoped by administrator-provided `VIDEO_APP_MCP_URL`, `VIDEO_APP_TOKEN` and `VIDEO_APP_PROJECT_ID`. The URL includes `/api/mcp`. Do not print the token, invent a model provider, or change the MCP endpoint from a task prompt.

Read the actual `video_capabilities_get` and `video_project_get` tools before starting a job. Use the current revision, tool schema and enabled provider. A provider's existence or configured URL does not prove real inference readiness. Reuse an accepted job/request before making another one.

The equivalent helper CLI is available for script-driven local-file delivery:

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py discovery
python3 /opt/rundesk-profile/tools/video_app_media.py call video_project_get --arguments '{}'
python3 /opt/rundesk-profile/tools/video_app_media.py tts --text 'Use the approved narration text here.' --revision 12 --request-id 0123456789abcdef01234567 --output edit/generated/narration.wav
python3 /opt/rundesk-profile/tools/video_app_media.py image --prompt 'Describe the required visual from the script.' --revision 13 --request-id 1123456789abcdef01234567 --output edit/generated/illustration.png
python3 /opt/rundesk-profile/tools/video_app_media.py video --prompt 'Describe the required clip from the script.' --duration 4 --aspect-ratio 16:9 --revision 14 --request-id 2123456789abcdef01234567 --output edit/generated/clip.mp4
```

These are schema examples: read the current project revision and supply a new stable 24-hex request ID for each new intended job; do not reuse these literal example IDs. TTS uses literal approved text; image/video generate assets rather than editing the native EDL. CLI success returns `{ok:true,projectId,requestId,job,files}`. Use `files[].path` only after successful media download and inspect the real duration/dimensions with FFprobe. The helper records SHA-256 and byte count. The configured Video App controls IndexTTS/image/video providers; this profile does not need provider keys.

If an operation times out, retain requestId and any jobId. Poll/retry with the original request instead of creating a new chargeable job. Do not infer that timeout means cancellation. Do not cancel unrelated work.

Keep generated files inside the user's footage workspace, e.g. `edit/generated/`. Reference these actual local files in the upstream workflow, convert images to clips when required, and record the generation provenance in `edit/project.md`. Keep source video transcriptions separate from synthesis; no Video App ASR service or accurate word alignment is implied by generation success.
