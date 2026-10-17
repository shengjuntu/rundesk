#!/usr/bin/env python3
"""Exercise real Codex App Server against local, synthetic Responses endpoints.

Uses a fresh temporary Codex home and fake API credentials. Does not read the
user's Codex configuration/authentication or send requests to a model provider.
Run: python3 scripts/codex-observation-smoke.py --codex /path/to/codex
"""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import struct
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class MockResponses(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.headers.get("Content-Encoding"):
            # Fail explicitly instead of mistaking compressed bytes for JSON.
            self.server.captures.put({"path": self.path, "error": "compressed request"})
            self.send_error(415)
            return
        try:
            request = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            self.server.captures.put({"path": self.path, "error": "non-JSON request"})
            self.send_error(400)
            return
        self.capture(request, "http")
        frames = self.response_frames()
        payload = "".join("data: " + json.dumps(frame) + "\n\n" for frame in frames).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("x-request-id", "fixture-request")
        self.end_headers()
        self.wfile.write(payload)

    def capture(self, request, transport):
        self.server.captures.put({
            "path": self.path,
            "request": request,
            "transport": transport,
            "authorization": self.headers.get("Authorization"),
            "fixtureHeader": self.headers.get("X-RunDesk-Fixture"),
        })

    @staticmethod
    def response_frames():
        message = {"id": "msg_fixture", "type": "message", "status": "completed", "role": "assistant",
                   "content": [{"type": "output_text", "text": "OK", "annotations": []}]}
        response = {"id": "resp_fixture", "object": "response", "created_at": 1,
                    "status": "completed", "output": [message],
                    "usage": {"input_tokens": 5, "output_tokens": 1, "total_tokens": 6}}
        return [
            {"type": "response.created", "response": {**response, "status": "in_progress", "output": []}},
            {"type": "response.output_item.added", "output_index": 0,
             "item": {**message, "status": "in_progress", "content": []}},
            {"type": "response.output_text.delta", "item_id": message["id"], "output_index": 0,
             "content_index": 0, "delta": "OK"},
            {"type": "response.output_item.done", "output_index": 0, "item": message},
            {"type": "response.completed", "response": response},
        ]

    def do_GET(self):
        if self.headers.get("Upgrade", "").lower() != "websocket":
            self.send_error(404)
            return
        self.close_connection = True
        key = self.headers.get("Sec-WebSocket-Key", "")
        accepted = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accepted)
        self.end_headers()
        self.wfile.flush()
        self.connection.settimeout(10)
        try:
            while True:
                header = self.rfile.read(2)
                if len(header) != 2:
                    return
                opcode, length = header[0] & 15, header[1] & 127
                if length == 126:
                    length = struct.unpack("!H", self.rfile.read(2))[0]
                elif length == 127:
                    length = struct.unpack("!Q", self.rfile.read(8))[0]
                if length > 16 << 20:
                    raise ValueError("fixture websocket frame too large")
                mask = self.rfile.read(4) if header[1] & 128 else b""
                payload = self.rfile.read(length)
                if mask:
                    payload = bytes(value ^ mask[index % 4] for index, value in enumerate(payload))
                if opcode == 8:
                    self.send_frame(8, payload)
                    return
                if opcode == 9:
                    self.send_frame(10, payload)
                    continue
                if opcode != 1 or not header[0] & 128:
                    raise ValueError("fixture expects complete JSON websocket messages")
                request = json.loads(payload)
                if request.get("generate") is not False:
                    self.capture(request, "websocket")
                for frame in self.response_frames():
                    self.send_frame(1, json.dumps(frame).encode())
        except (TimeoutError, ConnectionError, OSError):
            return

    def send_frame(self, opcode, payload):
        length = len(payload)
        header = bytes([128 | opcode])
        if length < 126:
            header += bytes([length])
        elif length <= 65535:
            header += bytes([126]) + struct.pack("!H", length)
        else:
            header += bytes([127]) + struct.pack("!Q", length)
        self.wfile.write(header + payload)
        self.wfile.flush()


class AppServer:
    def __init__(self, executable, home, workspace):
        # Keep only process-runtime necessities; no real API/auth/proxy env.
        env = {key: value for key, value in os.environ.items()
               if key in {"PATH", "LANG", "LC_ALL", "SYSTEMROOT", "TMPDIR", "TMP", "TEMP"}}
        env.update(CODEX_HOME=str(home), RUNDESK_CAPTURE_TEST_KEY="fake-rundesk-key",
                   OPENAI_API_KEY="fake-rundesk-key", NO_PROXY="127.0.0.1,localhost")
        self.process = subprocess.Popen([executable, "app-server"], cwd=workspace, env=env,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True, start_new_session=os.name == "posix")
        self.messages = queue.Queue()
        self.events = []
        self.stderr = []
        self.sequence = 0
        threading.Thread(target=self._read, daemon=True).start()
        threading.Thread(target=lambda: self.stderr.extend(self.process.stderr), daemon=True).start()

    def _read(self):
        for line in self.process.stdout:
            try:
                self.messages.put(json.loads(line))
            except ValueError:
                self.messages.put({"protocolError": "non-JSON stdout"})

    def send(self, value):
        self.process.stdin.write(json.dumps(value) + "\n")
        self.process.stdin.flush()

    def next(self, timeout=30):
        try:
            message = self.messages.get(timeout=timeout)
        except queue.Empty as error:
            raise RuntimeError("App Server timeout; " + "".join(self.stderr[-5:])) from error
        if "method" in message:
            self.events.append(message)
            # The fixture never approves or executes server-requested tools.
            if "id" in message:
                self.send({"id": message["id"], "error": {"code": -32601, "message": "fixture rejects server requests"}})
        return message

    def call(self, method, params):
        self.sequence += 1
        request_id = self.sequence
        self.send({"id": request_id, "method": method, "params": params})
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                message = self.next(max(0.1, deadline - time.monotonic()))
            except RuntimeError as error:
                raise RuntimeError(method + ": " + str(error)) from error
            if message.get("id") == request_id and "method" not in message:
                if "error" in message:
                    raise RuntimeError(method + ": " + json.dumps(message["error"]))
                return message.get("result")
        raise RuntimeError(method + " timed out")

    def wait_turn(self):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            message = self.next(max(0.1, deadline - time.monotonic()))
            if message.get("method") == "turn/completed":
                turn = message.get("params", {}).get("turn", {})
                if turn.get("status") != "completed":
                    raise RuntimeError("turn failed: " + json.dumps(turn.get("error")))
                return
        raise RuntimeError("turn did not complete")

    def close(self):
        if os.name == "posix":
            try:
                os.killpg(self.process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        else:
            self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            if os.name == "posix":
                os.killpg(self.process.pid, signal.SIGKILL)
            else:
                self.process.kill()
            self.process.wait(timeout=5)


def exercise(executable, root, upstream, provider, override, ephemeral=True, custom_override="dotted"):
    name = provider + ("-override" if override else "-baseline")
    home = root / name
    workspace = home / "workspace"
    workspace.mkdir(parents=True)
    config = ('model = "gpt-5.1-codex"\nmodel_provider = "' + provider + '"\n'
              + ('openai_base_url = "' + upstream + '/baseline/v1"\n' if provider == "openai" else '') +
              '[features]\nplugins = false\napps = false\nremote_plugin = false\n'
              'code_mode_host = false\nbrowser_use = false\nin_app_browser = false\nshell_snapshot = false\n')
    if provider == "fixture":
        config += ('[model_providers.fixture]\nname = "RunDesk fixture"\n'
                   'base_url = "' + upstream + '/baseline/v1"\n'
                   'env_key = "RUNDESK_CAPTURE_TEST_KEY"\nwire_api = "responses"\n'
                   'requires_openai_auth = false\nsupports_websockets = false\n'
                   '[model_providers.fixture.http_headers]\nX-RunDesk-Fixture = "preserve-me"\n')
    (home / "config.toml").write_text(config)
    # Built-in provider uses an entirely synthetic auth file in the temporary
    # home, never the caller's login or environment credentials.
    (home / "auth.json").write_text(json.dumps({"OPENAI_API_KEY": "fake-rundesk-key"}))
    app = AppServer(executable, home, workspace)
    try:
        initialized = app.call("initialize", {"clientInfo": {"name": "rundesk_observation_fixture", "version": "1"},
                                             "capabilities": {"experimentalApi": True}})
        app.send({"method": "initialized"})
        effective = app.call("config/read", {"includeLayers": False, "cwd": str(workspace)})["config"]
        overrides = {}
        if override:
            if provider == "openai":
                overrides["openai_base_url"] = upstream + "/override/v1"
            elif custom_override == "map":
                providers = omit_null_fields(effective.get("model_providers", {}))
                providers[provider]["base_url"] = upstream + "/override/v1"
                overrides["model_providers"] = providers
            else:
                overrides["model_providers." + provider + ".base_url"] = upstream + "/override/v1"
        params = {"cwd": str(workspace), "approvalPolicy": "never", "sandbox": "read-only",
                  "config": overrides, "experimentalRawEvents": False}
        if ephemeral:
            params.update(ephemeral=True, environments=[])
        started = app.call("thread/start", params)
        thread = started["thread"]["id"]
        app.call("turn/start", {"threadId": thread, "input": [{"type": "text", "text": "Reply OK. Do not use tools.", "text_elements": []}]})
        app.wait_turn()
        return {"case": name, "passed": True,
                "configProvider": effective.get("model_provider"),
                "providerEntryPresent": provider in effective.get("model_providers", {}),
                "server": initialized.get("userAgent", "unknown")}
    finally:
        app.close()


def omit_null_fields(value):
    # config/read includes optional nulls; TOML configuration cannot represent
    # null. Keep false, zero and empty strings, which have real semantics.
    if isinstance(value, dict):
        return {key: omit_null_fields(item) for key, item in value.items() if item is not None}
    if isinstance(value, list):
        return [omit_null_fields(item) for item in value]
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", required=True)
    parser.add_argument("--include-openai", action="store_true", help="also probe built-in provider with synthetic API-key auth")
    parser.add_argument("--persistent", action="store_true", help="use normal persisted threads in the temporary home")
    parser.add_argument("--custom-override", choices=["dotted", "map"], default="dotted")
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), MockResponses)
    server.captures = queue.Queue()
    threading.Thread(target=server.serve_forever, daemon=True).start()
    endpoint = "http://127.0.0.1:" + str(server.server_address[1])
    results = []
    try:
        # Codex refuses helper executable setup under /tmp on current builds.
        with tempfile.TemporaryDirectory(prefix="rundesk-codex-capture-", dir=Path.cwd()) as directory:
            cases = [("fixture", False), ("fixture", True)]
            if args.include_openai:
                cases.append(("openai", False))
                cases.append(("openai", True))
            for provider, override in cases:
                try:
                    result = exercise(args.codex, Path(directory), endpoint, provider, override,
                                      not args.persistent, args.custom_override)
                    capture = server.captures.get(timeout=3)
                    request = capture.get("request", {})
                    expected_path = ("/override" if override else "/baseline") + "/v1/responses"
                    assert capture["path"] == expected_path, capture["path"]
                    assert capture["authorization"] == "Bearer fake-rundesk-key", "synthetic authentication not preserved"
                    if provider == "fixture":
                        assert capture["fixtureHeader"] == "preserve-me", "custom provider header lost"
                    assert request.get("model") == "gpt-5.1-codex", "model changed"
                    if capture["transport"] == "http":
                        assert request.get("stream") is True, "streaming disabled"
                    else:
                        assert request.get("type") == "response.create", "unexpected websocket request"
                    assert isinstance(request.get("input"), list), "request input missing"
                    assert isinstance(request.get("tools"), list), "request tool definitions missing"
                    result.update(path=capture["path"], transport=capture["transport"], toolCount=len(request["tools"]),
                                  requestFields=sorted(request), authPreserved=True)
                except Exception as error:
                    result = {"case": provider + ("-override" if override else "-baseline"), "passed": False, "error": str(error)}
                results.append(result)
                print(json.dumps(result, ensure_ascii=False), flush=True)
                if not result["passed"] and not override:
                    # An unusable baseline cannot validate an observation
                    # override; stop instead of repeating a startup timeout.
                    break
    finally:
        server.shutdown()
        server.server_close()
    if any(not result["passed"] for result in results):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
