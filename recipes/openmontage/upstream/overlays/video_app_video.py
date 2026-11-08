# SPDX-License-Identifier: AGPL-3.0-only
# RunDesk contributors. Integration source for the separately packaged OpenMontage.
"""Native OpenMontage generated-video provider using Video App MCP."""
from tools.base_tool import BaseTool, ToolRuntime, ToolStability, ToolTier
from lib.rundesk_video_app import configured_status, generate, unknown_price


class VideoAppVideo(BaseTool):
    name = "video_app_video"
    version = "1.0.0"
    capability = "video_generation"
    provider = "video-app"
    tier = ToolTier.GENERATE
    runtime = ToolRuntime.API
    stability = ToolStability.BETA
    capabilities = ["text_to_video", "image_to_video", "reference_to_video"]
    supports = {"configured_service": True, "reference_assets": True}
    best_for = ["video generation through the administrator's Video App service"]
    dependencies = []
    input_schema = {"type": "object", "required": ["prompt", "revision", "request_id"], "properties": {"prompt": {"type": "string"}, "revision": {"type": "integer", "minimum": 0}, "request_id": {"type": "string", "pattern": "^[0-9a-f]{24}$"}, "duration": {"type": ["number", "string"]}, "aspect_ratio": {"type": "string"}, "reference_asset_ids": {"type": "array", "items": {"type": "string"}}, "output_path": {"type": "string"}, "operation": {"type": "string", "enum": ["generate", "text_to_video", "image_to_video", "reference_to_video"]}}}
    idempotency_key_fields = ["prompt", "revision", "request_id", "duration", "aspect_ratio", "reference_asset_ids", "operation"]
    side_effects = ["starts or recovers a durable Video App job", "downloads the verified result to the workspace"]
    get_status = staticmethod(configured_status)
    estimate_cost = staticmethod(unknown_price)

    def execute(self, inputs):
        return generate("video", inputs)
