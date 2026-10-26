#!/usr/bin/env python3
"""Real local HTTP and stdio contract tests; no models or Docker required."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / "examples/_shared/video_app_media.py"
spec = importlib.util.spec_from_file_location("video_app_media", MODULE)
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)
PID = "1" * 24
JID = "2" * 24
AID = "3" * 24
RID = "4" * 24
TOKEN = "fixture-private-video-app-token"
MEDIA = b"fixture real HTTP bytes\x00\xff" * 200
SHA = hashlib.sha256(MEDIA).hexdigest()


class Fixture:
    def __init__(self):
        self.calls = []
        self.headers = []
        self.jobs = {}
        self.dispatches = 0
        self.gets = []
        self.mode = "normal"
        self.redirect = None
        self.project_revision = 7
        self.lock = threading.Lock()
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def send(self, status, value):
                raw = json.dumps(value).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def do_GET(self):
                fixture.gets.append((self.path, self.headers.get("Authorization")))
                if fixture.redirect:
                    self.send_response(302)
                    self.send_header("Location", fixture.redirect)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                if self.headers.get("Authorization") != "Bearer " + TOKEN:
                    self.send(401, {})
                    return
                self.send_response(200)
                self.send_header("Content-Length", str(len(MEDIA)))
                self.end_headers()
                self.wfile.write(MEDIA)

            def do_POST(self):
                fixture.headers.append(self.headers.get("Authorization"))
                if self.path != "/api/mcp" or self.headers.get("Authorization") != "Bearer " + TOKEN:
                    self.send(401, {})
                    return
                msg = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                with fixture.lock:
                    fixture.calls.append(msg)
                if fixture.mode in ("malformed-json", "oversized-json"):
                    raw = b"{" if fixture.mode == "malformed-json" else b" " * (bridge.MAX_JSON + 1)
                    self.send_response(200)
                    self.send_header("Content-Length", str(len(raw)))
                    self.end_headers()
                    self.wfile.write(raw)
                    return
                if fixture.mode == "http-error":
                    self.send(503, {"error": "private upstream body " + TOKEN})
                    return
                if msg["method"] == "tools/list":
                    result = {"tools": [{"name": name, "description": name, "inputSchema": {"type": "object", "properties": {"type": {"enum": ["tts", "render"]}}}} for name in sorted(bridge.ALLOWED | {"video_project_create", "video_narration_update", "video_hyperframes_update"})]}
                else:
                    params = msg["params"]
                    name, args = params["name"], params["arguments"]
                    if fixture.mode == "tool-error":
                        self.send(200, {"jsonrpc": "2.0", "id": msg["id"], "result": {"isError": True, "content": [{"type": "text", "text": "echo credential " + TOKEN}]}})
                        return
                    if name == "video_project_get":
                        value = {"id": PID, "revision": fixture.project_revision, "narration": "preserved original narration"}
                    elif name == "video_capabilities_get":
                        value = {"schemaVersion": 1, "generation": True}
                    elif name == "video_job_start":
                        request = args["requestId"]
                        if request not in fixture.jobs:
                            fixture.dispatches += 1
                            fixture.jobs[request] = {"jobId": ("tts:" if args["type"] == "tts" else "generation:") + JID, "projectId": PID, "type": args["type"], "revision": args["revision"], "requestId": request, "state": "running", "artifacts": []}
                        value = fixture.jobs[request].copy()
                        if fixture.mode == "lost-ack":
                            self.connection.shutdown(socket.SHUT_RDWR)
                            self.connection.close()
                            return
                    elif name == "video_job_get":
                        value = next(v.copy() for v in fixture.jobs.values() if v["jobId"] == args["jobId"])
                        if fixture.mode != "pending":
                            value["state"] = "succeeded"
                            if value["type"] == "tts":
                                value["artifacts"] = [{"name": "manifest.json", "url": f"/api/projects/{PID}/speech/{JID}/files/manifest.json", "sha256": SHA}, {"name": "narration.wav", "url": f"/api/projects/{PID}/speech/{JID}/files/narration.wav", "sha256": SHA, "size": len(MEDIA)}]
                            else:
                                value["artifacts"] = [{"name": AID, "url": f"/api/projects/{PID}/assets/{AID}", "sha256": SHA, "size": len(MEDIA)}]
                    elif name == "video_job_list":
                        value = {"jobs": [{"type": "render"}, {"type": "image_generation"}]}
                    elif name == "video_asset_inspect":
                        self.send(200, {"jsonrpc": "2.0", "id": msg["id"], "result": {"content": [{"type": "image", "mimeType": "image/png", "data": "cGl4ZWxz"}, {"type": "text", "text": '{"assetId":"' + AID + '"}'}]}})
                        return
                    else:
                        value = {"ok": True}
                    if fixture.mode == "wrong-job" and name in ("video_job_start", "video_job_get"):
                        value["requestId"] = "9" * 24
                    result = {"content": [{"type": "text", "text": json.dumps(value)}]}
                self.send(200, {"jsonrpc": "2.0", "id": msg["id"] + (1 if fixture.mode == "wrong-id" else 0), "result": result})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = "http://127.0.0.1:" + str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def client(self, root):
        return bridge.Client(self.url + "/api/mcp", TOKEN, PID, timeout=2, output_root=root)

    def env(self, root):
        env = {k: v for k, v in os.environ.items() if not k.startswith("VIDEO_APP_") and k != "CODEX_HOME"}
        env.update(VIDEO_APP_MCP_URL=self.url + "/api/mcp", VIDEO_APP_TOKEN=TOKEN, VIDEO_APP_PROJECT_ID=PID, HOME=str(root), VIDEO_APP_OUTPUT_ROOT=str(root))
        return env


class BridgeTests(unittest.TestCase):
    def test_generation_roundtrip_uses_one_request_and_real_download(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            client = fixture.client(root)
            out = client.generate("image_generation", 7, RID, {"prompt": "a news image"}, "material/image.png", poll_interval=0.01)
            self.assertTrue(out["ok"])
            self.assertEqual(Path(out["files"][0]["path"]).read_bytes(), MEDIA)
            self.assertEqual(out["files"][0]["sha256"], SHA)
            out2 = client.generate("image_generation", 7, RID, {"prompt": "a news image"}, "material/image.png", poll_interval=0.01)
            self.assertEqual(out2["files"][0]["sha256"], SHA)
            self.assertEqual(fixture.dispatches, 1)
            self.assertTrue(all(h == "Bearer " + TOKEN for h in fixture.headers))
            self.assertTrue(all(row["params"]["arguments"]["projectId"] == PID for row in fixture.calls if row["method"] == "tools/call"))

    def test_independent_tts_keeps_native_text_and_selects_audio(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            client = fixture.client(root)
            out = client.generate("tts", 7, RID, {"source": "text", "text": "one native sentence", "voice": "registered-voice"}, "voice.wav", poll_interval=0.01)
            self.assertTrue(out["files"][0]["url"].endswith("/narration.wav"))
            start = fixture.calls[0]["params"]
            self.assertEqual(start["arguments"]["options"]["text"], "one native sentence")
            self.assertFalse(any("narration_update" in row["params"].get("name", "") for row in fixture.calls))
            self.assertEqual(fixture.project_revision, 7)

    def test_stdio_mcp_filters_tools_options_projects_and_secrets(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            client = fixture.client(root)
            requests = [{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-03-26"}}, {"jsonrpc": "2.0", "id": 2, "method": "tools/list"}]
            for index, (name, args) in enumerate([("video_project_create", {}), ("video_project_get", {"projectId": "8" * 24}), ("video_job_start", {"type": "render", "revision": 7, "requestId": RID}), ("video_job_start", {"type": "tts", "revision": 7, "requestId": RID, "options": {"source": "narration"}})], 3):
                requests.append({"jsonrpc": "2.0", "id": index, "method": "tools/call", "params": {"name": name, "arguments": args}})
            dest = io.StringIO()
            bridge.serve(client, io.StringIO("".join(json.dumps(x) + "\n" for x in requests)), dest)
            replies = [json.loads(line) for line in dest.getvalue().splitlines()]
            self.assertEqual(replies[0]["result"]["protocolVersion"], "2025-03-26")
            tools = replies[1]["result"]["tools"]
            self.assertEqual({t["name"] for t in tools}, bridge.ALLOWED | {"video_media_download"})
            self.assertEqual(next(t for t in tools if t["name"] == "video_job_start")["inputSchema"]["properties"]["type"]["enum"], sorted(bridge.JOB_TYPES))
            self.assertTrue(all(row["result"]["isError"] for row in replies[2:]))
            self.assertEqual(fixture.dispatches, 0)
            self.assertNotIn(TOKEN, dest.getvalue())
            evidence = io.StringIO()
            inspect = {"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": {"name": "video_asset_inspect", "arguments": {"assetId": AID}}}
            bridge.serve(client, io.StringIO(json.dumps(inspect) + "\n"), evidence)
            self.assertEqual(json.loads(evidence.getvalue())["result"]["content"][0]["type"], "image")

    def test_redirect_and_wrong_origin_never_receive_the_token(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture, Fixture() as foreign:
            client = fixture.client(root)
            valid = f"/api/projects/{PID}/assets/{AID}"
            for url in [foreign.url + valid, f"/api/projects/{'9' * 24}/assets/{AID}", valid + "?download=1", valid.replace("/assets/", "/%61ssets/"), "/api/health"]:
                with self.assertRaises(bridge.BridgeError):
                    client.download(url, "out.bin")
            fixture.redirect = foreign.url + valid
            with self.assertRaisesRegex(bridge.BridgeError, "HTTP 302"):
                client.download(valid, "out.bin")
            self.assertEqual(foreign.gets, [])
            self.assertFalse((Path(root) / "out.bin").exists())

    def test_download_integrity_size_and_workspace_do_not_overwrite(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside, Fixture() as fixture:
            client = fixture.client(root)
            url = f"/api/projects/{PID}/assets/{AID}"
            for output in ["../escape.bin", str(Path(outside) / "outside.bin")]:
                with self.assertRaisesRegex(bridge.BridgeError, "inside"):
                    client.download(url, output)
            (Path(root) / "link").symlink_to(outside, target_is_directory=True)
            with self.assertRaisesRegex(bridge.BridgeError, "inside"):
                client.download(url, "link/secret")
            for kwargs in [{"sha256": "0" * 64}, {"max_bytes": 8}]:
                with self.assertRaises(bridge.BridgeError):
                    client.download(url, "bad.bin", **kwargs)
                self.assertFalse((Path(root) / "bad.bin").exists())
            target = Path(root) / "saved.bin"
            target.write_bytes(b"already accepted")
            with self.assertRaisesRegex(bridge.BridgeError, "different bytes"):
                client.download(url, "saved.bin", SHA)
            self.assertEqual(target.read_bytes(), b"already accepted")
            self.assertFalse(list(Path(root).glob(".video-app-*")))

    def test_lost_ack_and_pending_jobs_preserve_original_identity(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            client = fixture.client(root)
            fixture.mode = "lost-ack"
            with self.assertRaises(bridge.BridgeError) as lost:
                client.generate("video_generation", 7, RID, {"prompt": "motion"}, poll_interval=0.01)
            self.assertTrue(lost.exception.details["submissionUncertain"])
            self.assertEqual(lost.exception.details["requestId"], RID)
            self.assertEqual(fixture.dispatches, 1)
            fixture.mode = "pending"
            with self.assertRaises(bridge.BridgeError) as pending:
                client.generate("video_generation", 7, RID, {"prompt": "motion"}, wait_timeout=0.02, poll_interval=0.01)
            self.assertEqual(pending.exception.details["jobId"], "generation:" + JID)
            self.assertEqual(fixture.dispatches, 1)
            fixture.mode = "normal"
            self.assertEqual(client.call("video_job_get", {"jobId": "generation:" + JID})["state"], "succeeded")
            self.assertFalse(any(c.get("params", {}).get("name") == "video_job_cancel" for c in fixture.calls))

    def test_errors_malformed_receipts_and_exact_job_scope(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            client = fixture.client(root)
            for mode in ["http-error", "wrong-id", "tool-error", "malformed-json", "oversized-json"]:
                fixture.mode = mode
                with self.assertRaises(bridge.BridgeError) as error:
                    client.call("video_project_get")
                self.assertNotIn(TOKEN, str(error.exception))
            fixture.mode = "wrong-job"
            with self.assertRaisesRegex(bridge.BridgeError, "identity mismatch"):
                client.generate("image_generation", 7, RID, {"prompt": "test"}, poll_interval=0.01)
            for job in ["render:" + JID, "generation:" + "x" * 24]:
                with self.assertRaises(bridge.BridgeError):
                    client.call("video_job_get", {"jobId": job})

    def test_named_codex_env_fallback_respects_explicit_process_values(self):
        with tempfile.TemporaryDirectory() as root:
            home = Path(root) / "codex"; home.mkdir()
            (home / "config.toml").write_text('[mcp_servers.video_app_media.env]\nVIDEO_APP_MCP_URL = "http://127.0.0.1:1234/api/mcp"\nVIDEO_APP_TOKEN = "bridge-only"\nVIDEO_APP_PROJECT_ID = "' + PID + '"\n[mcp_servers.other.env]\nOTHER_SECRET = "not-a-bridge-secret"\n')
            resolved = bridge.resolve_configuration({"CODEX_HOME": str(home), "VIDEO_APP_TOKEN": "explicit"})
            self.assertEqual(resolved["VIDEO_APP_TOKEN"], "explicit")
            self.assertEqual(set(resolved), set(bridge.CONFIG_KEYS))
            self.assertNotIn("not-a-bridge-secret", json.dumps(resolved))
            self.assertEqual(bridge.resolve_configuration({**resolved, "CODEX_HOME": "/missing"}), resolved)
            (home / "config.toml").write_text('mcp_servers="invalid-table"\n')
            with self.assertRaises(bridge.BridgeError):
                bridge.resolve_configuration({"CODEX_HOME": str(home)})

    def test_cli_and_stdio_use_real_http_without_credentials_in_output(self):
        with tempfile.TemporaryDirectory() as root, Fixture() as fixture:
            command = [sys.executable, str(MODULE)]
            result = subprocess.run(command + ["tts", "--revision", "7", "--request-id", RID, "--text", "native text", "--output", "speech.wav", "--poll-interval", "0.01"], cwd=root, env=fixture.env(root), text=True, capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(json.loads(result.stdout)["ok"])
            self.assertEqual((Path(root) / "speech.wav").read_bytes(), MEDIA)
            msg = {"jsonrpc": "2.0", "id": "read", "method": "tools/call", "params": {"name": "video_project_get", "arguments": {}}}
            proxy = subprocess.run(command + ["serve-mcp"], input=json.dumps(msg) + "\n", cwd=root, env=fixture.env(root), text=True, capture_output=True, timeout=10)
            self.assertEqual(proxy.returncode, 0, proxy.stderr)
            self.assertEqual(json.loads(json.loads(proxy.stdout)["result"]["content"][0]["text"])["id"], PID)
            self.assertNotIn(TOKEN, result.stdout + result.stderr + proxy.stdout + proxy.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
