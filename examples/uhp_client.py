#!/usr/bin/env python3
r"""Small, dependency-free UHP client for RunDesk (Python 3.10+).

Set UHP_API_KEY in the process environment; never put it in command arguments.
Persist the operation key AND exact request before submit. A lost submission may
be retried with the same credential/key/body; a new task needs a new key. Token
rotation changes the current RunDesk idempotency/object namespace.

Examples:
  python3 examples/uhp_client.py harnesses
  python3 examples/uhp_client.py upload notes.txt
  python3 examples/uhp_client.py submit 'Summarise the attachment' \
    --harness-id chrn_example --file-id file_example --operation-key saved-job-001
  python3 examples/uhp_client.py read resp_example --wait 60
  python3 examples/uhp_client.py cancel resp_example
  python3 examples/uhp_client.py files hsess_example
  python3 examples/uhp_client.py download cntr_example file_example --output report.txt

Polling timeout and a disconnected stream do not cancel the server task. Waiting
for approval returns control to the caller; this client never approves a tool.
"""

import argparse
import http.client
import json
import math
import mimetypes
import os
from pathlib import Path
import re
import secrets
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


PROTOCOL_VERSION = "2026-10-19"
MAX_JSON_BYTES = 16 << 20
MAX_UPLOAD_BYTES = 64 << 20
MAX_DOWNLOAD_BYTES = 512 << 20
TERMINAL = {"completed", "failed", "incomplete", "cancelled"}


class UHPError(RuntimeError):
    """A protocol/local validation error; HTTP status is None for local errors."""

    def __init__(self, code, message, status=None):
        self.code, self.status = code, status
        super().__init__(f"{code}: {message}")


class UHPTransportError(UHPError):
    """The request may have reached the server; submission outcome is unknown."""


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


def _identifier(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,160}", value):
        raise ValueError("Expected an opaque UHP identifier, not a path or URL")
    return value


class UHPClient:
    def __init__(self, base_url=None, *, timeout=30):
        base_url = base_url or os.environ.get("UHP_URL", "http://127.0.0.1:3210")
        parsed = urllib.parse.urlsplit(base_url)
        try:
            port = parsed.port
        except ValueError as error:
            raise ValueError("Invalid server port") from error
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname
                or parsed.username is not None or parsed.password is not None
                or parsed.query or parsed.fragment or port == 0
                or any(part in {".", ".."} for part in parsed.path.split("/"))):
            raise ValueError("Use an http(s) server URL without credentials, query or fragment")
        if not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("timeout must be positive and finite")
        token = os.environ.get("UHP_API_KEY", "")
        if not token or any(char.isspace() for char in token) or not token.isascii():
            raise ValueError("Set a non-empty UHP_API_KEY in the process environment")
        self.base_url, self.timeout = base_url.rstrip("/"), timeout
        self._token = token
        self._opener = urllib.request.build_opener(_NoRedirect())

    def redact(self, value):
        """Remove the configured token even if a server accidentally echoes it."""
        if isinstance(value, str):
            return value.replace(self._token, "[redacted]")
        if isinstance(value, dict):
            return {self.redact(key): self.redact(item) for key, item in value.items()}
        if isinstance(value, list):
            return [self.redact(item) for item in value]
        return value

    def _open(self, method, path, data=None, headers=None):
        request_headers = {
            "Authorization": "Bearer " + self._token,
            "UHP-Version": PROTOCOL_VERSION,
            "Accept": "application/json",
        }
        request_headers.update(headers or {})
        request = urllib.request.Request(self.base_url + "/v1" + path,
                                         method=method, data=data, headers=request_headers)
        try:
            return self._opener.open(request, timeout=self.timeout)
        except urllib.error.HTTPError as error:
            with error:
                if 300 <= error.code < 400:
                    raise UHPError("redirect_refused", "Authenticated redirects are disabled", error.code) from None
                raw = error.read(MAX_JSON_BYTES + 1)
            code, message = "http_error", f"Server returned HTTP {error.code}"
            try:
                body = json.loads(raw)
                detail = body.get("error", {}) if isinstance(body, dict) else {}
                if isinstance(detail, dict):
                    code = str(detail.get("code") or code)
                    message = str(detail.get("message") or message)
            except (ValueError, UnicodeError):
                pass
            raise UHPError(self.redact(code), self.redact(message), error.code) from None
        except (urllib.error.URLError, http.client.HTTPException, OSError):
            raise UHPTransportError("transport_error", "Connection failed; the server may have accepted the request") from None

    def _json(self, method, path, body=None, *, key=None, encoded=None):
        headers = {}
        data = encoded
        if body is not None:
            data = json.dumps(body, ensure_ascii=False, allow_nan=False).encode("utf-8")
        if data is not None:
            headers["Content-Type"] = "application/json"
        if key is not None:
            headers["Idempotency-Key"] = key
        try:
            with self._open(method, path, data, headers) as response:
                raw = response.read(MAX_JSON_BYTES + 1)
        except (http.client.HTTPException, OSError):
            raise UHPTransportError("transport_error", "Response was interrupted; the server may have accepted the request") from None
        if len(raw) > MAX_JSON_BYTES:
            raise UHPError("response_too_large", "JSON response exceeds the client limit")
        try:
            result = json.loads(raw)
        except (ValueError, UnicodeError):
            raise UHPError("invalid_response", "Expected a JSON response") from None
        if not isinstance(result, dict):
            raise UHPError("invalid_response", "Expected a JSON object")
        return self.redact(result)

    def discovery(self):
        return self._json("GET", "/uhp")

    def harnesses(self):
        return self._json("GET", "/harnesses")

    def harness(self, harness_id):
        return self._json("GET", "/harnesses/" + _identifier(harness_id))

    def models(self, harness_id):
        return self._json("GET", "/harnesses/" + _identifier(harness_id) + "/models")

    def require_capabilities(self, harness_id, names):
        """Read-only preflight. It does not prove model/account readiness."""
        harness = self.harness(harness_id)
        capabilities = harness.get("rundesk_capabilities", {})
        missing = [name for name in names if capabilities.get(name) is not True]
        if missing:
            raise UHPError("capability_unavailable", "Harness does not advertise: " + ", ".join(missing))
        return harness

    def submit(self, request, *, operation_key, transport_retries=0, required_capabilities=()):
        """Background submission; transport retries are explicitly opt-in.

        Every attempt sends exactly the same encoded body and operation key.
        Required capabilities are checked locally and sent in rundesk_require
        for an authoritative server-side check before admission.
        HTTP errors are never retried automatically. Persist request/key before
        this method; do not upload a replacement attachment on submission retry.
        """
        if (not isinstance(operation_key, str) or not 1 <= len(operation_key) <= 255
                or any(ord(char) < 33 or ord(char) > 126 for char in operation_key)):
            raise ValueError("operation_key must be 1–255 visible ASCII characters, persisted by the caller")
        if not isinstance(transport_retries, int) or not 0 <= transport_retries <= 3:
            raise ValueError("transport_retries must be between 0 and 3")
        if not isinstance(request, dict) or not isinstance(request.get("metadata", {}), dict):
            raise ValueError("request and metadata must be JSON objects")
        body = dict(request)
        if body.get("stream"):
            raise ValueError("Submit in background, then use events(response_id) for streaming")
        body["background"] = True
        body["stream"] = False
        if required_capabilities:
            harness_id = body.get("metadata", {}).get("harness_id")
            if not harness_id:
                raise ValueError("Capability preflight requires an explicit metadata.harness_id")
            names = list(required_capabilities)
            if not all(isinstance(name, str) and name for name in names):
                raise ValueError("required_capabilities must contain capability names")
            metadata = dict(body.get("metadata", {}))
            existing = metadata.get("rundesk_require", [])
            if not isinstance(existing, list) or not all(isinstance(name, str) for name in existing):
                raise ValueError("metadata.rundesk_require must be a list of names")
            names = list(dict.fromkeys(existing + names))
            self.require_capabilities(harness_id, names)
            # The server checks again immediately before admission. Preflight is
            # only a helpful early error; it does not authorize execution.
            metadata["rundesk_require"] = names
            body["metadata"] = metadata
        encoded = json.dumps(body, ensure_ascii=False, allow_nan=False).encode("utf-8")
        for attempt in range(transport_retries + 1):
            try:
                return self._json("POST", "/responses", key=operation_key, encoded=encoded)
            except UHPTransportError:
                if attempt == transport_retries:
                    raise
        raise AssertionError("unreachable")

    def read(self, response_id):
        return self._json("GET", "/responses/" + _identifier(response_id))

    def cancel(self, response_id):
        return self._json("POST", "/responses/" + _identifier(response_id) + "/cancel", {})

    def wait(self, response_id, *, wait_seconds=60, poll_interval=1, on_update=None, stop_for_action=True):
        """Return on completion, required human action, or observation timeout.

        The returned response is authoritative; no cancellation or approval is
        sent. An application may choose stop_for_action=False and keep polling.
        """
        if not math.isfinite(wait_seconds) or wait_seconds < 0:
            raise ValueError("wait_seconds must be non-negative and finite")
        if not math.isfinite(poll_interval) or poll_interval <= 0:
            raise ValueError("poll_interval must be positive and finite")
        deadline = time.monotonic() + wait_seconds
        while True:
            response = self.read(response_id)
            if on_update:
                on_update(response)
            progress = response.get("metadata", {}).get("rundesk_progress", {})
            if (response.get("status") in TERMINAL or time.monotonic() >= deadline
                    or stop_for_action and progress.get("required_action")):
                return response
            time.sleep(min(poll_interval, max(0, deadline - time.monotonic())))

    def events(self, response_id, *, last_event_id=None):
        """Yield (event_id, JSON) from RunDesk's resumable SSE extension.

        Save each event id after processing; a caller may reconnect with that id.
        No automatic reconnect or task cancellation occurs. Use read() to inspect
        progress if the connection closes before a terminal event.
        """
        headers = {"Accept": "text/event-stream"}
        if last_event_id is not None:
            if not isinstance(last_event_id, int) or last_event_id < 0:
                raise ValueError("last_event_id must be a non-negative integer")
            headers["Last-Event-ID"] = str(last_event_id)
        try:
            with self._open("GET", "/responses/" + _identifier(response_id) + "/events", headers=headers) as response:
                if response.headers.get_content_type() != "text/event-stream":
                    raise UHPError("invalid_response", "Expected an SSE response")
                parts, event_id, size = [], None, 0
                while True:
                    raw = response.readline(MAX_JSON_BYTES + 1)
                    if not raw:
                        return
                    size += len(raw)
                    if size > MAX_JSON_BYTES:
                        raise UHPError("response_too_large", "SSE event exceeds the client limit")
                    line = raw.decode("utf-8").rstrip("\r\n")
                    if not line:
                        if parts:
                            yield event_id, self.redact(json.loads("\n".join(parts)))
                        parts, event_id, size = [], None, 0
                    elif line.startswith("id:"):
                        event_id = line[3:].strip()
                    elif line.startswith("data:"):
                        parts.append(line[5:].lstrip(" "))
        except (ValueError, UnicodeError):
            raise UHPError("invalid_response", "Invalid SSE JSON or UTF-8") from None
        except (http.client.HTTPException, OSError):
            raise UHPTransportError("transport_error", "Event stream was interrupted; the task was not cancelled") from None

    def upload(self, path):
        """Upload once, persist the returned file id, then reuse it on retries."""
        source = Path(path)
        with source.open("rb") as stream:
            content = stream.read(MAX_UPLOAD_BYTES + 1)
        if len(content) > MAX_UPLOAD_BYTES:
            raise ValueError("Upload exceeds the 64 MiB server limit")
        filename = source.name.replace("\\", "_").replace('"', "_").replace("\r", "_").replace("\n", "_")
        boundary = "rundesk-" + secrets.token_hex(24)
        mime = mimetypes.guess_type(filename)[0] or "application/octet-stream"
        data = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
                f'Content-Type: {mime}\r\n\r\n').encode("utf-8") + content + f"\r\n--{boundary}--\r\n".encode()
        try:
            with self._open("POST", "/files", data, {"Content-Type": "multipart/form-data; boundary=" + boundary}) as response:
                raw = response.read(MAX_JSON_BYTES + 1)
        except (http.client.HTTPException, OSError):
            raise UHPTransportError("transport_error", "Upload response was interrupted; do not silently upload again") from None
        if len(raw) > MAX_JSON_BYTES:
            raise UHPError("response_too_large", "Upload response exceeds the client limit")
        try:
            result = json.loads(raw)
            if not isinstance(result, dict) or not isinstance(result.get("id"), str):
                raise ValueError()
        except (ValueError, UnicodeError):
            raise UHPError("invalid_response", "Expected a file object") from None
        return self.redact(result)

    def files(self, session_id):
        return self._json("GET", "/sessions/" + _identifier(session_id) + "/files")

    def download(self, container_id, file_id, destination):
        """Write original bytes to an explicit new path; ignore server filenames.

        The temporary file is on the same filesystem. Publication uses a hard
        link to avoid overwriting an existing path, including a symlink.
        """
        destination = Path(destination)
        path = "/containers/" + _identifier(container_id) + "/files/" + _identifier(file_id) + "/content"
        temp_name = None
        try:
            with self._open("GET", path, headers={"Accept": "application/octet-stream"}) as response:
                if response.status != 200:
                    raise UHPError("invalid_response", "Expected a complete artifact download")
                with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=".uhp-download-", delete=False) as output:
                    temp_name = output.name
                    total = 0
                    while block := response.read(1 << 20):
                        total += len(block)
                        if total > MAX_DOWNLOAD_BYTES:
                            raise UHPError("response_too_large", "Artifact exceeds the 512 MiB client limit")
                        output.write(block)
                declared = response.headers.get("Content-Length")
                if declared is not None and total != int(declared):
                    raise UHPError("incomplete_download", "Artifact byte count differs from Content-Length")
            os.link(temp_name, destination)
            return destination
        except (http.client.HTTPException, urllib.error.URLError):
            raise UHPTransportError("transport_error", "Artifact download was interrupted") from None
        finally:
            if temp_name is not None:
                Path(temp_name).unlink(missing_ok=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--url", default=None, help="server root; defaults to UHP_URL or localhost:3210")
    parser.add_argument("--timeout", type=float, default=30, help="per-request network timeout")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("discover")
    commands.add_parser("harnesses")
    upload = commands.add_parser("upload")
    upload.add_argument("path")
    submit = commands.add_parser("submit")
    submit.add_argument("text")
    submit.add_argument("--operation-key", required=True, help="persist this key and request before submission")
    submit.add_argument("--harness-id")
    submit.add_argument("--model")
    submit.add_argument("--previous-response-id")
    submit.add_argument("--file-id", action="append", default=[])
    submit.add_argument("--instructions")
    submit.add_argument("--max-step", type=int)
    submit.add_argument("--max-output-tokens", type=int)
    submit.add_argument("--task-timeout", type=int)
    submit.add_argument("--require-capability", action="append", default=[])
    submit.add_argument("--transport-retries", type=int, default=0)
    read = commands.add_parser("read")
    read.add_argument("response_id")
    read.add_argument("--wait", type=float, default=0)
    read.add_argument("--poll-interval", type=float, default=1)
    cancel = commands.add_parser("cancel")
    cancel.add_argument("response_id")
    files = commands.add_parser("files")
    files.add_argument("session_id")
    download = commands.add_parser("download")
    download.add_argument("container_id")
    download.add_argument("file_id")
    download.add_argument("--output", required=True)
    args = parser.parse_args(argv)
    try:
        client = UHPClient(args.url, timeout=args.timeout)
        if args.command in {"discover", "harnesses"}:
            result = client.discovery() if args.command == "discover" else client.harnesses()
        elif args.command == "upload":
            result = client.upload(args.path)
        elif args.command == "submit":
            request = {"input": args.text}
            if args.file_id:
                request["input"] = [{"role": "user", "content": [{"type": "input_text", "text": args.text}]
                                     + [{"type": "input_file", "file_id": _identifier(value)} for value in args.file_id]}]
            if args.harness_id:
                request["metadata"] = {"harness_id": _identifier(args.harness_id)}
            for name, value in {"model": args.model, "previous_response_id": args.previous_response_id,
                                "instructions": args.instructions, "max_step": args.max_step,
                                "max_output_tokens": args.max_output_tokens, "timeout_seconds": args.task_timeout}.items():
                if value is not None:
                    request[name] = value
            result = client.submit(request, operation_key=args.operation_key,
                                   transport_retries=args.transport_retries, required_capabilities=args.require_capability)
        elif args.command == "read":
            result = client.wait(args.response_id, wait_seconds=args.wait, poll_interval=args.poll_interval)
        elif args.command == "cancel":
            result = client.cancel(args.response_id)
        elif args.command == "files":
            result = client.files(args.session_id)
        else:
            result = {"saved": str(client.download(args.container_id, args.file_id, args.output))}
        print(json.dumps(client.redact(result), ensure_ascii=False, indent=2), flush=True)
        if args.command == "read" and result.get("status") != "completed":
            return 2  # Pending/human action and unsuccessful terminal outcomes stay visible.
        return 0
    except (UHPError, ValueError, OSError) as error:
        message = str(error)
        token = os.environ.get("UHP_API_KEY", "")
        print(message.replace(token, "[redacted]") if token else message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
