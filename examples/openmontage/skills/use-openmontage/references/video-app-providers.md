# Video App native-provider contract

Configure the `video_app_media` MCP server in this RunDesk instance with `VIDEO_APP_MCP_URL` (complete `/api/mcp` endpoint), `VIDEO_APP_TOKEN` and `VIDEO_APP_PROJECT_ID`. Keep credentials in runtime configuration, never images or prompts. The common bridge reads process variables first, then only this MCP server's env from the selected Codex configuration; shell tools need not echo secrets.

Inspect capabilities and the actual project:

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py call video_capabilities_get --arguments '{"probe":false}'
python3 /opt/rundesk-profile/tools/video_app_media.py call video_project_get --arguments '{}'
```

Save current revision, a fresh 24-hex lowercase request_id, exact text/prompt, controls and output path in native project artifacts before generation. Missing revision/request_id is rejected before dispatch. On retries preserve both values and original body.

```python
from tools.tool_registry import registry
registry.discover()
result = registry.get("tts_selector").execute({
    "preferred_tool": "video_app_tts",
    "allowed_providers": ["video-app"],
    "text": "The approved narration for this section.",
    "revision": 12,  # Replace with the actual current revision.
    "request_id": "0123456789abcdef01234567",  # Replace once, save and reuse.
    "output_path": "projects/story/assets/narration.wav"
})
if not result.success:
    raise RuntimeError(result.error)
print(result.artifacts)
```

For image_selector use preferred_tool="video_app_image", prompt, optional aspect_ratio and reference_asset_ids. For video_selector use preferred_tool="video_app_video", prompt, optional duration, aspect_ratio, reference_asset_ids and a supported operation. Always retain allowed_providers=["video-app"]. Actual provider support comes from Video App capabilities; ComfyUI H3 can reject reference-conditioned requests. Import references into the bound App project first: native URLs and filesystem paths are not App asset IDs.

TTS can pass voice_id/voice, language and speed only if the configured service supports them. Section audio does not overwrite the project's authoritative narration. Forced word timestamps are not supplied; use real duration/transcription/alignment tools where needed.

Model selection stays in Video App. Unsupported native controls such as seed, negative prompt, width/height, vendor options and unregistered references fail rather than being silently dropped. The provider does not know the configured backend price; do not equate local MCP with free generation or bypass upstream budget/review.

Failures retain request_id in ToolResult.data; common bridge errors preserve jobId/requestId/submissionUncertain where available. Use job-get with the original job and inspect its receipt; do not switch provider or dispatch with a new ID after timeout. Outputs are limited to cwd or explicit administrator VIDEO_APP_OUTPUT_ROOT; existing different bytes are not overwritten. Record artifacts, data.output and data.video_app in the native asset manifest and continue OpenMontage Remotion/HyperFrames composition.
