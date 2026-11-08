# SPDX-License-Identifier: AGPL-3.0-only
# RunDesk contributors. Integration source for the separately packaged OpenMontage.
"""Native OpenMontage image provider using Video App MCP."""
from tools.base_tool import BaseTool, ToolRuntime, ToolStability, ToolTier
from lib.rundesk_video_app import configured_status, generate, unknown_price


class VideoAppImage(BaseTool):
    name = "video_app_image"
    version = "1.0.0"
    capability = "image_generation"
    provider = "video-app"
    tier = ToolTier.GENERATE
    runtime = ToolRuntime.API
    stability = ToolStability.BETA
    capabilities = ["text_to_image"]
    supports = {"configured_service": True, "reference_assets": True}
    best_for = ["image generation through the administrator's Video App service"]
    dependencies = []
    input_schema = {"type": "object", "required": ["prompt", "revision", "request_id"], "properties": {"prompt": {"type": "string"}, "revision": {"type": "integer", "minimum": 0}, "request_id": {"type": "string", "pattern": "^[0-9a-f]{24}$"}, "aspect_ratio": {"type": "string"}, "reference_asset_ids": {"type": "array", "items": {"type": "string"}}, "output_path": {"type": "string"}}}
    # The original image selector strips undeclared passthrough controls. Keep
    # unsupported controls visible so generate() can reject rather than ignore.
    input_schema["properties"].update({key: {"description": "Unsupported control; rejected before generation."} for key in ("negative_prompt", "width", "height", "seed", "n", "resolution", "generation_mode", "image_url", "image_path", "image_urls", "image_paths", "image_list", "element_list", "api_family", "model_name", "model", "image_reference", "image_fidelity", "human_fidelity", "result_type", "series_amount", "watermark", "callback_url", "external_task_id", "workflow_json", "workflow_path", "output_node", "workflow_name", "workflow_model", "workflow_model_stack")})
    idempotency_key_fields = ["prompt", "revision", "request_id", "aspect_ratio", "reference_asset_ids"]
    side_effects = ["starts or recovers a durable Video App job", "downloads the verified result to the workspace"]
    get_status = staticmethod(configured_status)
    estimate_cost = staticmethod(unknown_price)

    def execute(self, inputs):
        return generate("image", inputs)
