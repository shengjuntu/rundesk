# SPDX-License-Identifier: AGPL-3.0-only
# RunDesk contributors. Integration source for the separately packaged OpenMontage.
"""Native OpenMontage TTS provider using Video App's configured service."""
from tools.base_tool import BaseTool, ToolRuntime, ToolStability, ToolTier
from lib.rundesk_video_app import configured_status, generate, unknown_price


class VideoAppTTS(BaseTool):
    name = "video_app_tts"
    version = "1.0.0"
    capability = "tts"
    provider = "video-app"
    tier = ToolTier.VOICE
    runtime = ToolRuntime.API
    stability = ToolStability.BETA
    capabilities = ["text_to_speech"]
    supports = {"configured_service": True, "native_audio": True, "forced_word_timestamps": False}
    best_for = ["narration through the administrator's Video App TTS service"]
    dependencies = []
    input_schema = {"type": "object", "required": ["text", "revision", "request_id"], "properties": {"text": {"type": "string"}, "revision": {"type": "integer", "minimum": 0}, "request_id": {"type": "string", "pattern": "^[0-9a-f]{24}$"}, "voice_id": {"type": "string"}, "voice": {"type": "string"}, "language": {"type": "string"}, "speed": {"type": "number"}, "output_path": {"type": "string"}}}
    idempotency_key_fields = ["text", "revision", "request_id", "voice_id", "voice", "language", "speed"]
    side_effects = ["starts or recovers a durable Video App job", "downloads the verified result to the workspace"]
    get_status = staticmethod(configured_status)
    estimate_cost = staticmethod(unknown_price)

    def execute(self, inputs):
        return generate("tts", inputs)
