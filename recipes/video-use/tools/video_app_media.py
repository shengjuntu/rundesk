#!/usr/bin/env python3
"""Generation-only Video App MCP client for independent production environments.

All credentials and project identity are runtime environment values.  This module
never edits narration, creates projects, renders an App editing document, or calls
an inference vendor directly.  It intentionally has no third-party dependencies.
"""
import argparse
import copy
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

VERSION = "1.0.0"
MAX_JSON = 2 * 1024 * 1024
MAX_MEDIA = 512 * 1024 * 1024
IDENTIFIER = re.compile(r"[a-f0-9]{24}\Z")
HASH = re.compile(r"[a-f0-9]{64}\Z")
ALLOWED = frozenset({"video_capabilities_get", "video_project_get", "video_asset_list",
                     "video_asset_inspect", "video_asset_video_inspect", "video_job_start",
                     "video_job_get", "video_job_list", "video_job_cancel"})
JOB_TYPES = frozenset({"tts", "image_generation", "video_generation"})
CONFIG_KEYS = ("VIDEO_APP_MCP_URL", "VIDEO_APP_TOKEN", "VIDEO_APP_PROJECT_ID")


class BridgeError(Exception):
    def __init__(self, message, **details):
        super().__init__(message)
        self.details = details


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def resolve_configuration(environ=None):
    """Read only this bridge's explicit runtime values or named Codex MCP env.

    Shell tools do not inherit an MCP server's configured env.  Access exactly
    mcp_servers.video_app_media.env in the current Codex HOME as a fallback;
    never enumerate other MCP credentials or return them in discovery output.
    """
    environ = os.environ if environ is None else environ
    result = {key: environ[key] for key in CONFIG_KEYS if key in environ}
    if len(result) < len(CONFIG_KEYS):
        path = Path(environ.get("CODEX_HOME") or Path(environ.get("HOME") or Path.home()) / ".codex") / "config.toml"
        if path.is_file():
            try:
                import tomllib
                if path.stat().st_size > MAX_JSON:
                    raise BridgeError("Codex config exceeds the safe bridge configuration size")
                with path.open("rb") as file:
                    config = tomllib.load(file)
                servers = config.get("mcp_servers", {})
                server = servers.get("video_app_media", {}) if isinstance(servers, dict) else None
                if not isinstance(server, dict):
                    raise BridgeError("video_app_media MCP configuration must be a TOML table")
                configured = server.get("env", {})
                if not isinstance(configured, dict):
                    raise BridgeError("video_app_media MCP env must be a TOML table")
                for key in CONFIG_KEYS:
                    if key not in result and key in configured:
                        if not isinstance(configured[key], str):
                            raise BridgeError("video_app_media MCP env values must be strings")
                        result[key] = configured[key]
            except (OSError, ValueError, ImportError) as error:
                raise BridgeError("Cannot read the named video_app_media MCP env from current Codex config (Python 3.11+ required)") from error
    return result


def origin(url):
    parsed = urllib.parse.urlsplit(url)
    try:
        port = parsed.port
    except ValueError:
        raise BridgeError("Invalid Video App URL port")
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username is not None or parsed.password is not None:
        raise BridgeError("Video App URL must be HTTP(S) without embedded credentials")
    return parsed.scheme.lower(), parsed.hostname.lower(), port or (443 if parsed.scheme == "https" else 80)


def compact(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False)


def body_chunks(response, maximum, timeout):
    """Read bounded HTTP data without an unlimited slow-drip read(size) loop."""
    deadline, size = time.monotonic() + timeout, 0
    reader = getattr(response, "read1", response.read)
    while True:
        if time.monotonic() >= deadline:
            raise BridgeError("Video App response exceeded the read deadline")
        chunk = reader(min(1 << 20, maximum - size + 1))
        if not chunk:
            return
        size += len(chunk)
        if size > maximum:
            raise BridgeError("Video App response exceeds the size limit")
        yield chunk


def file_sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def unpack(result):
    if not isinstance(result, dict):
        raise BridgeError("Malformed MCP tool result")
    if result.get("isError"):
        text = " ".join(str(item.get("text", "")) for item in result.get("content", []) if isinstance(item, dict))
        raise BridgeError("Video App tool failed: " + text[:2000])
    if isinstance(result.get("structuredContent"), (dict, list)):
        return result["structuredContent"]
    if any(isinstance(item, dict) and item.get("type") != "text" for item in result.get("content", [])):
        return result  # Preserve image/audio evidence for MCP consumers.
    texts = [item.get("text") for item in result.get("content", []) if isinstance(item, dict) and item.get("type") == "text"]
    if len(texts) == 1 and isinstance(texts[0], str):
        try:
            return json.loads(texts[0])
        except ValueError:
            pass
    return result


class Client:
    def __init__(self, endpoint=None, token=None, project_id=None, timeout=120, output_root=None):
        config = resolve_configuration() if any(value is None for value in (endpoint, token, project_id)) else {}
        self.endpoint = endpoint if endpoint is not None else config.get("VIDEO_APP_MCP_URL", "")
        self.token = token if token is not None else config.get("VIDEO_APP_TOKEN", "")
        self.project_id = project_id if project_id is not None else config.get("VIDEO_APP_PROJECT_ID", "")
        parsed = urllib.parse.urlsplit(self.endpoint)
        self.origin = origin(self.endpoint)
        if parsed.query or parsed.fragment or not parsed.path.endswith("/api/mcp"):
            raise BridgeError("VIDEO_APP_MCP_URL must be the complete /api/mcp URL without query or fragment")
        if not self.token or "\r" in self.token or "\n" in self.token:
            raise BridgeError("VIDEO_APP_TOKEN is required at runtime")
        if self.project_id and not IDENTIFIER.fullmatch(self.project_id):
            raise BridgeError("VIDEO_APP_PROJECT_ID must be a 24-character hexadecimal project ID")
        if not 0 < timeout <= 300:
            raise BridgeError("HTTP timeout must be greater than zero and at most 300 seconds")
        self.timeout = timeout
        self.output_root = Path(output_root or os.environ.get("VIDEO_APP_OUTPUT_ROOT") or Path.cwd()).resolve()
        if not self.output_root.is_dir():
            raise BridgeError("Output root must be an existing workspace directory")
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        self.sequence = 0

    def safe_error(self, error):
        return str(error).replace(self.token, "[redacted]")

    def request(self, method, params=None):
        self.sequence += 1
        identity = self.sequence
        data = compact({"jsonrpc": "2.0", "id": identity, "method": method, "params": params or {}}).encode()
        if len(data) > MAX_JSON:
            raise BridgeError("MCP request exceeds 2 MiB")
        req = urllib.request.Request(self.endpoint, data=data, method="POST", headers={"Content-Type": "application/json", "Accept": "application/json", "Authorization": "Bearer " + self.token})
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                if response.status != 200:
                    raise BridgeError("Video App MCP returned HTTP " + str(response.status))
                raw = b"".join(body_chunks(response, MAX_JSON, self.timeout))
        except urllib.error.HTTPError as error:
            raise BridgeError("Video App MCP returned HTTP " + str(error.code))
        except (OSError, urllib.error.URLError, http.client.HTTPException) as error:
            raise BridgeError("Video App MCP transport failed: " + self.safe_error(error))
        try:
            body = json.loads(raw)
        except (ValueError, UnicodeError):
            raise BridgeError("Video App MCP returned invalid JSON")
        if not isinstance(body, dict) or body.get("jsonrpc") != "2.0" or body.get("id") != identity:
            raise BridgeError("Video App MCP response identity mismatch")
        if "error" in body:
            raise BridgeError("Video App MCP rejected the request: " + compact(body["error"])[:2000])
        if "result" not in body:
            raise BridgeError("Video App MCP response has no result")
        return body["result"]

    def arguments(self, name, arguments):
        if name not in ALLOWED:
            raise BridgeError("Tool is outside the generation-only bridge: " + name)
        if not isinstance(arguments, dict):
            raise BridgeError("Tool arguments must be an object")
        args = copy.deepcopy(arguments)
        if name != "video_capabilities_get":
            if not self.project_id:
                raise BridgeError("VIDEO_APP_PROJECT_ID is required for project tools")
            if args.get("projectId", self.project_id) != self.project_id:
                raise BridgeError("Tool projectId differs from the configured project")
            args["projectId"] = self.project_id
        if name == "video_job_start":
            if args.get("type") not in JOB_TYPES:
                raise BridgeError("Only tts, image_generation and video_generation jobs are allowed")
            if not IDENTIFIER.fullmatch(str(args.get("requestId", ""))):
                raise BridgeError("A caller-owned 24-hex requestId is required; reuse it for identical retries")
            if type(args.get("revision")) is not int or args["revision"] < 1:
                raise BridgeError("A fixed positive project revision is required")
            options = args.get("options", {})
            if not isinstance(options, dict):
                raise BridgeError("Generation options must be an object")
            if args["type"] == "tts" and (options.get("source") != "text" or not isinstance(options.get("text"), str) or not options["text"].strip()):
                raise BridgeError("Independent TTS requires options.source=text and nonempty options.text")
        if name in ("video_job_get", "video_job_cancel"):
            if not re.fullmatch(r"(?:tts|generation):[a-f0-9]{24}", str(args.get("jobId", ""))):
                raise BridgeError("Only a TTS or generation job ID is allowed")
        return args

    def raw_call(self, name, arguments):
        return self.request("tools/call", {"name": name, "arguments": self.arguments(name, arguments)})

    def call(self, name, arguments=None):
        try:
            value = unpack(self.raw_call(name, {} if arguments is None else arguments))
        except BridgeError as error:
            raise BridgeError(self.safe_error(error), **error.details) from error
        if name == "video_job_list" and isinstance(value, dict) and isinstance(value.get("jobs"), list):
            value["jobs"] = [row for row in value["jobs"] if row.get("type") in JOB_TYPES]
        return value

    def tools(self):
        result = self.request("tools/list")
        if not isinstance(result, dict) or not isinstance(result.get("tools"), list):
            raise BridgeError("Malformed MCP tools/list response")
        tools = []
        for item in result["tools"]:
            if not isinstance(item, dict) or item.get("name") not in ALLOWED:
                continue
            item = copy.deepcopy(item)
            if item["name"] == "video_job_start":
                item["description"] = "Generate independent TTS, images or video through Video App. TTS requires options.source=text and options.text. Caller fixes revision/requestId; never edits narration or starts rendering."
                item.setdefault("inputSchema", {}).setdefault("properties", {})["type"] = {"type": "string", "enum": sorted(JOB_TYPES)}
            if item["name"] != "video_capabilities_get" and self.project_id:
                item.setdefault("inputSchema", {}).setdefault("properties", {})["projectId"] = {"type": "string", "const": self.project_id, "default": self.project_id}
            tools.append(item)
        tools.append({"name": "video_media_download", "description": "Download an owned Video App asset or TTS artifact into the current workspace. Same-origin only; no redirects or overwriting different existing bytes.", "inputSchema": {"type": "object", "properties": {"url": {"type": "string"}, "output": {"type": "string"}, "sha256": {"type": "string", "pattern": "^[a-f0-9]{64}$"}}, "required": ["url", "output"], "additionalProperties": False}, "annotations": {"readOnlyHint": False, "destructiveHint": False, "openWorldHint": True}})
        return tools

    def artifact_url(self, raw):
        if not isinstance(raw, str) or not raw or "\\" in raw or any(ord(c) < 32 for c in raw):
            raise BridgeError("Invalid artifact URL")
        url = urllib.parse.urljoin(self.endpoint, raw)
        parsed = urllib.parse.urlsplit(url)
        if origin(url) != self.origin or parsed.query or parsed.fragment:
            raise BridgeError("Artifact URL must remain on the configured Video App origin without query or fragment")
        if not self.project_id or urllib.parse.unquote(parsed.path) != parsed.path:
            raise BridgeError("Artifact URL must identify the configured project without encoded path components")
        route = r"/api/projects/" + re.escape(self.project_id) + r"/(?:assets/[a-f0-9]{24}|speech/[a-f0-9]{24}/files/[A-Za-z0-9][A-Za-z0-9._-]{0,127})"
        if not re.fullmatch(route, parsed.path):
            raise BridgeError("Artifact URL is outside configured-project assets and TTS files")
        return url

    def download(self, url, output, sha256=None, max_bytes=MAX_MEDIA):
        url = self.artifact_url(url)
        if sha256 is not None and not HASH.fullmatch(sha256):
            raise BridgeError("Expected SHA-256 must contain 64 lowercase hexadecimal characters")
        if type(max_bytes) is not int or not 0 < max_bytes <= MAX_MEDIA:
            raise BridgeError("Download limit must be between 1 byte and 512 MiB")
        target = Path(output)
        target = (self.output_root / target if not target.is_absolute() else target).resolve()
        if target == self.output_root or not target.is_relative_to(self.output_root):
            raise BridgeError("Output must remain inside the current workspace or VIDEO_APP_OUTPUT_ROOT")
        if target.exists() and not target.is_file():
            raise BridgeError("Output already exists and is not a regular file")
        target.parent.mkdir(parents=True, exist_ok=True)
        req = urllib.request.Request(url, headers={"Authorization": "Bearer " + self.token}, method="GET")
        temporary = None
        digest, size = hashlib.sha256(), 0
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                if response.status != 200:
                    raise BridgeError("Artifact download returned HTTP " + str(response.status))
                if response.headers.get("Content-Length"):
                    try:
                        if int(response.headers["Content-Length"]) > max_bytes:
                            raise BridgeError("Artifact exceeds the download size limit")
                    except ValueError:
                        raise BridgeError("Artifact returned an invalid Content-Length")
                with tempfile.NamedTemporaryFile(prefix=".video-app-", dir=target.parent, delete=False) as f:
                    temporary = Path(f.name)
                    for chunk in body_chunks(response, max_bytes, self.timeout):
                        size += len(chunk)
                        digest.update(chunk)
                        f.write(chunk)
                    f.flush()
                    os.fsync(f.fileno())
            actual = digest.hexdigest()
            if not size:
                raise BridgeError("Artifact is empty")
            if sha256 is not None and actual != sha256:
                raise BridgeError("Artifact SHA-256 does not match the completed job")
            if target.exists():
                if file_sha256(target) != actual:
                    raise BridgeError("Output already contains different bytes; choose another output path")
            else:
                # Exclusive linking preserves concurrent-created outputs instead of replacing them.
                try:
                    os.link(temporary, target)
                except FileExistsError:
                    if file_sha256(target) != actual:
                        raise BridgeError("Output was concurrently created with different bytes")
            return {"path": str(target), "sha256": actual, "size": size, "url": url}
        except urllib.error.HTTPError as error:
            raise BridgeError("Artifact download returned HTTP " + str(error.code))
        except (OSError, urllib.error.URLError, http.client.HTTPException) as error:
            raise BridgeError("Artifact download failed: " + self.safe_error(error))
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)

    def generate(self, kind, revision, request_id, options, output=None, wait_timeout=1800, poll_interval=1):
        if not 0 < wait_timeout <= 7200 or not 0 < poll_interval <= 60:
            raise BridgeError("Wait timeout must be 0..7200 seconds and poll interval 0..60 seconds")
        arguments = self.arguments("video_job_start", {"type": kind, "revision": revision, "requestId": request_id, "options": options})
        try:
            job = self.call("video_job_start", arguments)
        except BridgeError as error:
            raise BridgeError(str(error), requestId=request_id, submissionUncertain=True)
        if not isinstance(job, dict) or job.get("projectId") != self.project_id or job.get("requestId") != request_id or job.get("revision") != revision or job.get("type") != kind or not re.fullmatch(r"(?:tts|generation):[a-f0-9]{24}", str(job.get("jobId", ""))):
            raise BridgeError("Job receipt identity mismatch; inspect the original request before retrying", requestId=request_id, submissionUncertain=True)
        job_id = job["jobId"]
        deadline = time.monotonic() + wait_timeout
        while job.get("state") not in ("succeeded", "failed", "interrupted"):
            if time.monotonic() >= deadline:
                raise BridgeError("Job is still pending; resume with job-get or repeat the identical request, never use a fresh requestId", jobId=job_id, requestId=request_id)
            time.sleep(min(poll_interval, max(0, deadline - time.monotonic())))
            try:
                job = self.call("video_job_get", {"jobId": job_id})
            except BridgeError as error:
                raise BridgeError(str(error), jobId=job_id, requestId=request_id)
            if not isinstance(job, dict) or job.get("projectId") != self.project_id or job.get("jobId") != job_id or job.get("requestId") != request_id or job.get("revision") != revision or job.get("type") != kind:
                raise BridgeError("Polled job receipt identity mismatch", jobId=job_id, requestId=request_id)
        if job.get("state") != "succeeded":
            raise BridgeError("Generation " + str(job.get("state")) + ": " + str(job.get("error", ""))[:2000], jobId=job_id, requestId=request_id)
        files = []
        if output is not None:
            artifacts = job.get("artifacts")
            if not isinstance(artifacts, list) or not artifacts or not isinstance(artifacts[0], dict):
                raise BridgeError("Succeeded job has no artifact receipt", jobId=job_id, requestId=request_id)
            # TTS may return JSON/SRT before WAV; choose the audio artifact explicitly.
            artifact = next((x for x in artifacts if kind == "tts" and str(x.get("name", "")).lower().endswith((".wav", ".mp3", ".flac", ".ogg"))), artifacts[0])
            try:
                saved = self.download(artifact["url"], output, artifact.get("sha256") or None)
            except (BridgeError, KeyError) as error:
                raise BridgeError(str(error), jobId=job_id, requestId=request_id)
            saved["name"] = artifact.get("name", "")
            files.append(saved)
        return {"schemaVersion": 1, "ok": True, "projectId": self.project_id, "requestId": request_id, "job": job, "files": files}


def serve(client, source=sys.stdin, destination=sys.stdout):
    while True:
        line = source.readline(MAX_JSON + 2)
        if not line:
            return
        response_id = None
        try:
            if len(line.encode()) > MAX_JSON or not line.endswith("\n"):
                raise BridgeError("MCP input line exceeds 2 MiB or is incomplete")
            msg = json.loads(line)
            if not isinstance(msg, dict) or msg.get("jsonrpc") != "2.0":
                raise BridgeError("Invalid JSON-RPC request")
            if "id" not in msg:
                continue
            response_id = msg["id"]
            method, params = msg.get("method"), msg.get("params", {})
            if not isinstance(params, dict):
                raise BridgeError("MCP params must be an object")
            if method == "initialize":
                result = {"protocolVersion": params.get("protocolVersion", "2024-11-05"), "capabilities": {"tools": {}}, "serverInfo": {"name": "video-app-media", "version": VERSION}}
            elif method == "ping":
                result = {}
            elif method == "tools/list":
                result = {"tools": client.tools()}
            elif method == "tools/call":
                try:
                    name, arguments = params.get("name"), params.get("arguments", {})
                    if name == "video_media_download":
                        if not isinstance(arguments, dict) or set(arguments) - {"url", "output", "sha256"}:
                            raise BridgeError("Invalid download arguments")
                        value = client.download(**arguments)
                        result = {"content": [{"type": "text", "text": compact(value)}]}
                    else:
                        if name == "video_job_list":
                            value = client.call(name, arguments)
                            result = {"content": [{"type": "text", "text": compact(value)}]}
                        else:
                            result = client.raw_call(name, arguments)
                            unpack(result)  # Detect errors without discarding image/audio blocks.
                except (BridgeError, TypeError, KeyError) as error:
                    result = {"isError": True, "content": [{"type": "text", "text": compact({"ok": False, "error": client.safe_error(error)})}]}
            else:
                destination.write(compact({"jsonrpc": "2.0", "id": response_id, "error": {"code": -32601, "message": "Method not found"}}) + "\n")
                destination.flush()
                continue
            response = {"jsonrpc": "2.0", "id": response_id, "result": result}
        except (ValueError, BridgeError, TypeError) as error:
            response = {"jsonrpc": "2.0", "id": response_id, "error": {"code": -32600, "message": client.safe_error(error)}}
        destination.write(compact(response) + "\n")
        destination.flush()


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--version", action="version", version=VERSION)
    p.add_argument("--http-timeout", type=float, default=120)
    sub = p.add_subparsers(dest="command", required=True)
    sub.add_parser("discovery")
    sub.add_parser("serve-mcp")
    call = sub.add_parser("call"); call.add_argument("tool"); call.add_argument("--arguments", default="{}")
    job = sub.add_parser("job-get"); job.add_argument("--job-id", required=True)
    download = sub.add_parser("download"); download.add_argument("--url", required=True); download.add_argument("--output", required=True); download.add_argument("--sha256"); download.add_argument("--max-bytes", type=int, default=MAX_MEDIA)
    for name in ("tts", "image", "video"):
        item = sub.add_parser(name)
        item.add_argument("--revision", type=int, required=True)
        item.add_argument("--request-id", required=True)
        item.add_argument("--output")
        item.add_argument("--wait-timeout", type=float, default=1800)
        item.add_argument("--poll-interval", type=float, default=1)
        if name == "tts":
            item.add_argument("--text", required=True)
            for field in ("voice", "language", "backend"):
                item.add_argument("--" + field)
            item.add_argument("--speed", type=float)
        else:
            item.add_argument("--prompt", required=True)
            item.add_argument("--duration", type=float)
            item.add_argument("--aspect-ratio")
            item.add_argument("--reference-asset-id", action="append", default=[])
    return p


def main(argv=None):
    args = parser().parse_args(argv)
    client = None
    try:
        client = Client(timeout=args.http_timeout)
        if args.command == "serve-mcp":
            serve(client); return 0
        if args.command == "discovery":
            result = {"schemaVersion": 1, "tools": client.tools(), "capabilities": client.call("video_capabilities_get", {"probe": False}), "project": client.call("video_project_get")}
        elif args.command == "call":
            result = client.call(args.tool, json.loads(args.arguments))
        elif args.command == "job-get":
            result = client.call("video_job_get", {"jobId": args.job_id})
        elif args.command == "download":
            result = client.download(args.url, args.output, args.sha256, args.max_bytes)
        else:
            options = {"source": "text", "text": args.text} if args.command == "tts" else {"prompt": args.prompt}
            mapping = {"voice": "voice", "language": "language", "backend": "backend", "speed": "speed", "duration": "duration", "aspect_ratio": "aspectRatio", "reference_asset_id": "referenceAssetIds"}
            options.update({key: getattr(args, field) for field, key in mapping.items() if getattr(args, field, None) not in (None, [])})
            result = client.generate({"tts": "tts", "image": "image_generation", "video": "video_generation"}[args.command], args.revision, args.request_id, options, args.output, args.wait_timeout, args.poll_interval)
        print(compact(result))
        return 0
    except (BridgeError, ValueError, OSError, TypeError) as error:
        message = client.safe_error(error) if client else str(error)
        result = {"schemaVersion": 1, "ok": False, "error": message, **(error.details if isinstance(error, BridgeError) else {})}
        print(compact(result), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
