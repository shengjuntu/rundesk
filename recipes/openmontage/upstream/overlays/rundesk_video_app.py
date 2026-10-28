# SPDX-License-Identifier: AGPL-3.0-only
# RunDesk contributors. Integration source for the separately packaged OpenMontage.
"""Translate native provider calls into the installed Video App MCP bridge."""
from __future__ import annotations

import functools
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import time

from tools.base_tool import ToolResult, ToolStatus
from tools.provider_pricing import PriceQuoteRequired

BRIDGE = Path("/opt/rundesk-profile/tools/video_app_media.py")
CONFIG_KEYS = ("VIDEO_APP_MCP_URL", "VIDEO_APP_TOKEN", "VIDEO_APP_PROJECT_ID")


@functools.lru_cache(maxsize=1)
def _bridge_module():
    spec = importlib.util.spec_from_file_location("rundesk_video_app_media_bridge", BRIDGE)
    if spec is None or spec.loader is None:
        raise RuntimeError("Video App MCP bridge is not installed")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def configured_status() -> ToolStatus:
    """Configuration-only status; no discovery, model call, or key is printed."""
    try:
        cfg = _bridge_module().resolve_configuration()
        return ToolStatus.AVAILABLE if all(cfg.get(k) for k in CONFIG_KEYS) else ToolStatus.UNAVAILABLE
    except Exception:
        return ToolStatus.UNAVAILABLE


def unknown_price(_inputs):
    raise PriceQuoteRequired("Video App controls the actual provider and pricing; do not infer zero cost from a local MCP endpoint.")


def generate(kind: str, inputs: dict) -> ToolResult:
    started = time.monotonic()
    rid = inputs.get("request_id")
    revision = inputs.get("revision")
    if not isinstance(rid, str) or re.fullmatch(r"[0-9a-f]{24}", rid) is None:
        return ToolResult(False, error="Pass a saved 24-hex request_id; retries must retain it.", cost_usd=None)
    if type(revision) is not int or revision < 0:
        return ToolResult(False, error="Pass the current Video App project revision explicitly.", cost_usd=None)
    key = "text" if kind == "tts" else "prompt"
    text = inputs.get(key)
    if not isinstance(text, str) or not text.strip():
        return ToolResult(False, error=f"A nonempty {key} is required.", cost_usd=None)
    # These native provider controls have no declared Video App equivalent.
    # Reject them before dispatch rather than quietly changing the request.
    unsupported = ("model", "model_id", "model_name", "pitch", "instructions", "stability", "similarity_boost", "style", "seed", "negative_prompt", "width", "height", "image_path", "image_paths", "image_url", "image_urls", "reference_image_path", "reference_image_paths", "reference_image_url", "reference_image_urls", "reference_video_path", "reference_video_url", "workflow_json", "workflow_path", "language_code", "output_format", "speaking_rate", "voice_speed", "voice_language", "apply_text_normalization", "voice_performance", "sample_mode", "speaker_id", "length_scale")
    rejected = [k for k in unsupported if inputs.get(k) is not None]
    supported = {"text", "prompt", "revision", "request_id", "output_path", "preferred_tool", "preferred_provider", "allowed_providers", "hosting_provider", "task_context", "project_dir", "scene_id", "operation", "n", "timestamps", "input_type", "reference_asset_ids", "voice", "voice_id", "language", "speed", "duration", "aspect_ratio"}
    rejected += sorted(set(inputs) - supported - set(unsupported))
    if rejected:
        return ToolResult(False, error="Unsupported Video App controls: " + ", ".join(rejected) + ". Use configured service settings and App asset IDs.", cost_usd=None)
    if inputs.get("n", 1) != 1 or inputs.get("timestamps", False) or inputs.get("input_type", "text") != "text":
        return ToolResult(False, error="Only one output and plain text TTS are supported; this provider does not return forced word timestamps.", cost_usd=None)
    allowed_operations = {"tts": {"generate"}, "image": {"generate", "text_to_image"}, "video": {"generate", "text_to_video", "image_to_video", "reference_to_video"}}
    if inputs.get("operation", "generate") not in allowed_operations[kind]:
        return ToolResult(False, error="This operation is not supported by the Video App media provider.", cost_usd=None)
    refs = inputs.get("reference_asset_ids", [])
    if not isinstance(refs, list) or not all(isinstance(x, str) and x for x in refs):
        return ToolResult(False, error="reference_asset_ids must contain real Video App asset IDs.", cost_usd=None)
    if kind == "video" and inputs.get("operation") in {"image_to_video", "reference_to_video"} and not refs:
        return ToolResult(False, error="Reference-conditioned video requires reference_asset_ids; no text-only fallback was dispatched.", cost_usd=None)
    suffix = {"tts": "wav", "image": "png", "video": "mp4"}[kind]
    output = inputs.get("output_path", f"assets/video-app/{kind}-{rid}.{suffix}")
    if not isinstance(output, str) or not output:
        return ToolResult(False, error="output_path must be a local workspace path.", cost_usd=None)
    argv = [sys.executable, str(BRIDGE), kind, "--" + key, text, "--revision", str(revision), "--request-id", rid, "--output", output]
    for source, flag in (("voice_id", "--voice"), ("language", "--language"), ("speed", "--speed")) if kind == "tts" else (("duration", "--duration"), ("aspect_ratio", "--aspect-ratio")):
        value = inputs.get(source)
        if source == "voice_id" and value is None:
            value = inputs.get("voice")
        if value is not None:
            argv += [flag, str(value)]
    for ref in refs:
        argv += ["--reference-asset-id", ref]
    try:
        proc = subprocess.run(argv, capture_output=True, text=True, timeout=1860, check=False)
    except subprocess.TimeoutExpired:
        return ToolResult(False, data={"request_id": rid, "submissionUncertain": True}, error="Bridge deadline exceeded. Recover the same request; do not create a new request ID.", cost_usd=None)
    except OSError:
        return ToolResult(False, data={"request_id": rid}, error="Video App MCP bridge could not be started.", cost_usd=None)
    if proc.returncode:
        try:
            error = json.loads(proc.stderr)
        except (ValueError, TypeError):
            error = {"requestId": rid, "submissionUncertain": True}
        return ToolResult(False, data={"request_id": rid, "video_app_error": error}, error="Video App MCP generation failed. Inspect the preserved job/request receipt before retrying; no provider fallback was attempted.", cost_usd=None)
    try:
        receipt = json.loads(proc.stdout)
        files = receipt["files"]
        if receipt.get("ok") is not True or receipt.get("requestId") != rid or not isinstance(files, list) or not 1 <= len(files) <= 8:
            raise ValueError("Unexpected bridge receipt")
        paths = []
        for file in files:
            path = Path(file["path"])
            with path.open("rb") as data:
                digest = hashlib.file_digest(data, "sha256").hexdigest()
            if not path.is_file() or digest != file["sha256"]:
                raise ValueError("Downloaded bytes differ from receipt")
            paths.append(str(path))
    except (OSError, ValueError, TypeError, KeyError):
        return ToolResult(False, data={"request_id": rid, "submissionUncertain": True}, error="Invalid bridge receipt or artifact bytes. Recover the original request; do not dispatch a replacement.", cost_usd=None)
    data = {"provider": "video-app", "output": paths[0], "outputs": paths, "video_app": receipt, "request_id": rid}
    if kind == "video":
        data["video_path"] = paths[0]
    if kind == "image":
        data["image_paths"] = paths
    return ToolResult(True, data=data, artifacts=paths, cost_usd=None, duration_seconds=round(time.monotonic() - started, 3))
