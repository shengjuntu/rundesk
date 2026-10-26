#!/usr/bin/env python3
"""HTTP fixture checks for the stdlib UHP client; no provider or real agent calls."""

from contextlib import contextmanager
from email.parser import BytesParser
from email.policy import default
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import tempfile
import threading
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("uhp_client", ROOT / "examples/uhp_client.py")
uhp = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(uhp)
TOKEN = "uhp-fixture-secret-never-real"


class Fixture:
    def __init__(self):
        self.requests = []
        self.routes = {}
        self.lock = threading.Lock()

    @staticmethod
    def reply(handler, body, status=200, **headers):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        handler.send_response(status)
        handler.send_header("Content-Type", "application/json")
        handler.send_header("Content-Length", str(len(data)))
        for key, value in headers.items():
            handler.send_header(key.replace("_", "-"), value)
        handler.end_headers()
        handler.wfile.write(data)


@contextmanager
def server():
    fixture = Fixture()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def handle_request(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            request = {"method": self.command, "path": self.path, "body": body,
                       "headers": dict(self.headers)}
            with fixture.lock:
                fixture.requests.append(request)
            route = fixture.routes.get((self.command, self.path))
            if route:
                route(self, request)
            else:
                fixture.reply(self, {"error": {"code": "fixture_missing", "message": "Unknown fixture route"}}, 404)

        do_GET = do_POST = handle_request

    http = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=http.serve_forever, daemon=True)
    thread.start()
    fixture.url = f"http://127.0.0.1:{http.server_port}"
    try:
        yield fixture
    finally:
        http.shutdown()
        http.server_close()
        thread.join(timeout=2)


class UHPClientTests(unittest.TestCase):
    def setUp(self):
        self.env = mock.patch.dict(os.environ, {"UHP_API_KEY": TOKEN})
        self.env.start()
        self.addCleanup(self.env.stop)

    def client(self, fixture):
        return uhp.UHPClient(fixture.url, timeout=2)

    def test_lost_submit_reuses_exact_key_and_body_and_only_executes_once(self):
        with server() as fixture:
            accepted = {}

            def submit(handler, request):
                key = request["headers"]["Idempotency-Key"]
                if key not in accepted:
                    accepted[key] = request["body"]
                    handler.close_connection = True
                    handler.connection.shutdown(socket.SHUT_RDWR)
                    handler.connection.close()
                    return
                self.assertEqual(request["body"], accepted[key])
                fixture.reply(handler, {"id": "resp_original", "status": "completed"})

            fixture.routes["POST", "/v1/responses"] = submit
            request = {"input": "研究材料", "previous_response_id": "resp_previous",
                       "metadata": {"harness_id": "chrn_saved"}}
            result = self.client(fixture).submit(request, operation_key="persisted-operation-1", transport_retries=1)
            self.assertEqual(result["id"], "resp_original")
            self.assertEqual(len(accepted), 1)
            self.assertEqual(len(fixture.requests), 2)
            payload = json.loads(fixture.requests[0]["body"])
            self.assertTrue(payload["background"])
            self.assertFalse(payload["stream"])
            self.assertEqual(payload["previous_response_id"], "resp_previous")
            self.assertNotIn("background", request, "Caller request must remain unchanged")

    def test_transport_failure_is_not_retried_by_default(self):
        with server() as fixture:
            def drop(handler, _request):
                handler.close_connection = True
                handler.connection.shutdown(socket.SHUT_RDWR)
                handler.connection.close()

            fixture.routes["POST", "/v1/responses"] = drop
            with self.assertRaises(uhp.UHPTransportError):
                self.client(fixture).submit({"input": "one task"}, operation_key="saved-one")
            self.assertEqual(len(fixture.requests), 1)

    def test_http_error_is_reported_without_retry_or_secret(self):
        with server() as fixture:
            fixture.routes["POST", "/v1/responses"] = lambda h, r: fixture.reply(
                h, {"error": {"code": "session_busy", "message": "busy " + TOKEN}}, 409)
            with self.assertRaises(uhp.UHPError) as raised:
                self.client(fixture).submit({"input": "one task"}, operation_key="saved-one", transport_retries=3)
            self.assertEqual(raised.exception.status, 409)
            self.assertEqual(raised.exception.code, "session_busy")
            self.assertNotIn(TOKEN, str(raised.exception))
            self.assertEqual(len(fixture.requests), 1)

    def test_capability_preflight_refuses_before_submit(self):
        with server() as fixture:
            fixture.routes["GET", "/v1/harnesses/chrn_1"] = lambda h, r: fixture.reply(
                h, {"id": "chrn_1", "rundesk_capabilities": {"input_text": True, "max_step": False}})
            client = self.client(fixture)
            with self.assertRaisesRegex(uhp.UHPError, "max_step"):
                client.submit({"input": "bounded task", "metadata": {"harness_id": "chrn_1"}},
                              operation_key="saved-one", required_capabilities=["max_step"])
            self.assertEqual([r["method"] for r in fixture.requests], ["GET"])
            self.assertEqual(client.require_capabilities("chrn_1", ["input_text"])["id"], "chrn_1")
            with self.assertRaisesRegex(uhp.UHPError, "unknown"):
                client.require_capabilities("chrn_1", ["unknown"])

    def test_preflight_is_rechecked_by_server_and_does_not_mutate_request(self):
        with server() as fixture:
            fixture.routes["GET", "/v1/harnesses/chrn_1"] = lambda h, r: fixture.reply(
                h, {"id": "chrn_1", "rundesk_capabilities": {"timeout_seconds": True}})

            def submit(handler, request):
                body = json.loads(request["body"])
                self.assertEqual(body["metadata"]["rundesk_require"], ["timeout_seconds"])
                # Capability changed after preflight: server refusal remains authoritative.
                fixture.reply(handler, {"error": {"code": "rundesk_capability_unsupported", "message": "No longer supported"}}, 422)

            fixture.routes["POST", "/v1/responses"] = submit
            request = {"input": "work", "metadata": {"harness_id": "chrn_1"}, "timeout_seconds": 10}
            with self.assertRaises(uhp.UHPError) as raised:
                self.client(fixture).submit(request, operation_key="saved-one", required_capabilities=["timeout_seconds"])
            self.assertEqual(raised.exception.code, "rundesk_capability_unsupported")
            self.assertNotIn("rundesk_require", request["metadata"])
            self.assertEqual([r["method"] for r in fixture.requests], ["GET", "POST"])

    def test_poll_returns_waiting_action_without_approving_or_cancelling(self):
        with server() as fixture:
            states = ["queued", "running", "waiting_approval"]
            seen = []

            def read(handler, _request):
                state = states[min(len(seen), len(states) - 1)]
                seen.append(state)
                progress = {"state": state}
                if state == "waiting_approval":
                    progress["required_action"] = {"type": "approval", "workbench_url": "/?session=host_1"}
                fixture.reply(handler, {"id": "resp_1", "status": "in_progress",
                                       "metadata": {"rundesk_progress": progress}})

            fixture.routes["GET", "/v1/responses/resp_1"] = read
            updates = []
            result = self.client(fixture).wait("resp_1", wait_seconds=2, poll_interval=.001,
                                               on_update=lambda r: updates.append(r["metadata"]["rundesk_progress"]["state"]))
            self.assertEqual(updates, states)
            self.assertEqual(result["metadata"]["rundesk_progress"]["required_action"]["workbench_url"], "/?session=host_1")
            self.assertTrue(all(r["method"] == "GET" for r in fixture.requests))

    def test_poll_timeout_keeps_task_running_and_cancel_is_explicit(self):
        with server() as fixture:
            fixture.routes["GET", "/v1/responses/resp_1"] = lambda h, r: fixture.reply(h, {"id": "resp_1", "status": "in_progress"})
            fixture.routes["POST", "/v1/responses/resp_1/cancel"] = lambda h, r: fixture.reply(h, {"id": "resp_1", "status": "cancelled"})
            client = self.client(fixture)
            self.assertEqual(client.wait("resp_1", wait_seconds=0)["status"], "in_progress")
            self.assertEqual(len(fixture.requests), 1)
            self.assertEqual(client.cancel("resp_1")["status"], "cancelled")
            self.assertEqual(fixture.requests[-1]["path"], "/v1/responses/resp_1/cancel")

    def test_all_authenticated_redirects_are_refused(self):
        with server() as first, server() as second:
            first.routes["GET", "/v1/harnesses"] = lambda h, r: first.reply(
                h, b"", 302, Location=second.url + "/stolen")
            with self.assertRaisesRegex(uhp.UHPError, "redirect_refused"):
                self.client(first).harnesses()
            self.assertEqual(len(second.requests), 0)
            self.assertEqual(first.requests[0]["headers"]["Authorization"], "Bearer " + TOKEN)
            first.routes["GET", "/v1/harnesses"] = lambda h, r: first.reply(
                h, b"", 307, Location=first.url + "/other")
            with self.assertRaises(uhp.UHPError):
                self.client(first).harnesses()
            self.assertEqual(len(first.requests), 2)

    def test_upload_multipart_preserves_original_bytes_and_filename(self):
        with server() as fixture, tempfile.TemporaryDirectory() as directory:
            data = b"\x00\xff\r\nattachment bytes\x00"
            source = Path(directory) / "notes.bin"
            source.write_bytes(data)

            def upload(handler, request):
                message = BytesParser(policy=default).parsebytes(
                    ("Content-Type: " + request["headers"]["Content-Type"] + "\r\nMIME-Version: 1.0\r\n\r\n").encode() + request["body"])
                parts = list(message.iter_parts())
                self.assertEqual(len(parts), 1)
                self.assertEqual(parts[0].get_param("name", header="Content-Disposition"), "file")
                self.assertEqual(parts[0].get_filename(), "notes.bin")
                self.assertEqual(parts[0].get_payload(decode=True), data)
                fixture.reply(handler, {"id": "file_uploaded", "bytes": len(data), "filename": source.name})

            fixture.routes["POST", "/v1/files"] = upload
            self.assertEqual(self.client(fixture).upload(source)["id"], "file_uploaded")
            self.assertEqual(len(fixture.requests), 1)

    def test_download_ignores_remote_filename_and_never_overwrites(self):
        with server() as fixture, tempfile.TemporaryDirectory() as directory:
            data = b"\x00\xfforiginal artifact\r\n"
            fixture.routes["GET", "/v1/containers/cntr_1/files/file_1/content"] = lambda h, r: fixture.reply(
                h, data, Content_Disposition='attachment; filename="../../escaped.txt"')
            target = Path(directory) / "chosen.bin"
            client = self.client(fixture)
            self.assertEqual(client.download("cntr_1", "file_1", target), target)
            self.assertEqual(target.read_bytes(), data)
            self.assertEqual(list(Path(directory).iterdir()), [target])
            target.write_bytes(b"existing")
            with self.assertRaises(FileExistsError):
                client.download("cntr_1", "file_1", target)
            self.assertEqual(target.read_bytes(), b"existing")
            self.assertEqual(list(Path(directory).iterdir()), [target])
            with self.assertRaises(ValueError):
                client.download("cntr_1", "../private", target)

    def test_partial_download_does_not_publish_or_leave_temp_file(self):
        with server() as fixture, tempfile.TemporaryDirectory() as directory:
            def download(handler, _request):
                handler.send_response(200)
                handler.send_header("Content-Length", "100")
                handler.end_headers()
                handler.wfile.write(b"partial")
                handler.close_connection = True

            fixture.routes["GET", "/v1/containers/cntr_1/files/file_1/content"] = download
            with self.assertRaises(uhp.UHPError):
                self.client(fixture).download("cntr_1", "file_1", Path(directory) / "output.bin")
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_sse_resume_comments_and_multiline_json(self):
        with server() as fixture:
            def events(handler, request):
                self.assertEqual(request["headers"].get("Last-Event-Id"), "3")
                body = b': keep-alive\n\nid: 4\ndata: {"type":\ndata: "response.completed"}\n\n'
                handler.send_response(200)
                handler.send_header("Content-Type", "text/event-stream")
                handler.send_header("Content-Length", str(len(body)))
                handler.end_headers()
                handler.wfile.write(body)

            fixture.routes["GET", "/v1/responses/resp_1/events"] = events
            events = list(self.client(fixture).events("resp_1", last_event_id=3))
            self.assertEqual(events, [("4", {"type": "response.completed"})])
            self.assertEqual(len(fixture.requests), 1)

    def test_discovery_file_listing_and_secret_redaction(self):
        with server() as fixture:
            fixture.routes["GET", "/v1/uhp"] = lambda h, r: fixture.reply(h, {"protocol": "uhp", "debug": TOKEN})
            fixture.routes["GET", "/v1/sessions/hsess1/files"] = lambda h, r: fixture.reply(h, {"files": [{"id": "file_1"}]})
            client = self.client(fixture)
            self.assertEqual(client.discovery()["debug"], "[redacted]")
            self.assertEqual(client.files("hsess1")["files"][0]["id"], "file_1")
            self.assertTrue(all(r["headers"]["Uhp-Version"] == uhp.PROTOCOL_VERSION for r in fixture.requests))

    def test_local_validation_and_cli_errors_do_not_make_requests(self):
        for url in ["file:///tmp/a", "ftp://server", "https://user:secret@example.com", "http://host/?x=1", "http://host/#x", "http://host:invalid"]:
            with self.assertRaises(ValueError):
                uhp.UHPClient(url)
        with server() as fixture:
            client = self.client(fixture)
            for key in ["", "bad\nkey", "x" * 256]:
                with self.assertRaises(ValueError):
                    client.submit({"input": "work"}, operation_key=key)
            with self.assertRaises(ValueError):
                client.submit({"input": "work"}, operation_key="saved", required_capabilities=["max_step"])
            self.assertEqual(fixture.requests, [])
        with mock.patch.dict(os.environ, {"UHP_API_KEY": ""}), mock.patch("sys.stderr", new_callable=io.StringIO) as error:
            self.assertEqual(uhp.main(["harnesses"]), 1)
            self.assertIn("UHP_API_KEY", error.getvalue())


if __name__ == "__main__":
    unittest.main(verbosity=2)
