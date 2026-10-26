#!/usr/bin/env python3
"""Check real Codex App Server startup with a local POST-only MCP fixture.

Creates a fresh temporary HOME/CODEX_HOME and a synthetic model provider. It
never starts a model turn, reads real account configuration, or calls plugin/*.
Run: python3 scripts/codex-build-mcp-smoke.py [--codex /path/to/codex]
"""

import argparse
import json
import os
from pathlib import Path
import queue
import shutil
import signal
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class BuildMCP(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def empty(self, status):
        self.send_response(status)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self):
        self.server.captures.append({"http": "GET", "path": self.path})
        self.send_response(405)
        self.send_header("Allow", "POST")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        authorized = self.headers.get("Authorization") == "Bearer fake-task-capability"
        method = body.get("method")
        self.server.captures.append({"http": "POST", "path": self.path,
                                     "method": method, "authorized": authorized})
        if self.path != "/mcp" or not authorized:
            self.empty(403)
            return
        if "id" not in body:
            self.empty(202)
            return
        response = {"jsonrpc": "2.0", "id": body["id"]}
        if method == "initialize":
            response["result"] = {
                "protocolVersion": body["params"]["protocolVersion"],
                "serverInfo": {"name": "rundesk-build-center", "version": "fixture"},
                "capabilities": {"tools": {}},
            }
        elif method == "tools/list":
            response["result"] = {"tools": [{
                "name": "build_recipes_list", "description": "Local smoke fixture only",
                "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
            }]}
        elif method == "ping":
            response["result"] = {}
        else:
            # Match RunDesk's restricted MCP endpoint: no resource capability.
            response["error"] = {"code": -32601, "message": "Only task-scoped build MCP methods are available"}
        payload = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


class AppServer:
    def __init__(self, executable, home, codex_home, workspace):
        env = {key: value for key, value in os.environ.items()
               if key in {"PATH", "LANG", "LC_ALL", "SYSTEMROOT", "TMPDIR", "TMP", "TEMP"}}
        env.update(HOME=str(home), CODEX_HOME=str(codex_home),
                   RUNDESK_CAPTURE_TEST_KEY="fake-model-key", NO_PROXY="127.0.0.1,localhost")
        self.process = subprocess.Popen([executable, "app-server"], cwd=workspace, env=env,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True, start_new_session=os.name == "posix")
        self.incoming = queue.Queue()
        self.stderr = []
        self.sequence = 0
        threading.Thread(target=self.read_stdout, daemon=True).start()
        threading.Thread(target=lambda: self.stderr.extend(self.process.stderr), daemon=True).start()

    def read_stdout(self):
        for line in self.process.stdout:
            self.incoming.put(json.loads(line))

    def send(self, body):
        self.process.stdin.write(json.dumps(body) + "\n")
        self.process.stdin.flush()

    def call(self, method, params):
        self.sequence += 1
        request_id = self.sequence
        self.send({"id": request_id, "method": method, "params": params})
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            item = self.incoming.get(timeout=max(0.01, deadline - time.monotonic()))
            if item.get("id") == request_id and "method" not in item:
                if "error" in item:
                    raise RuntimeError(method + ": " + json.dumps(item["error"]))
                return item["result"]
            if "method" in item and "id" in item:
                self.send({"id": item["id"], "error": {"code": -32601,
                           "message": "Fixture never approves tool calls"}})
        raise RuntimeError(method + " timed out")

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


def exercise(executable, server, root):
    home = root / "home"
    codex_home = home / "codex"
    workspace = root / "workspace"
    codex_home.mkdir(parents=True)
    workspace.mkdir()
    endpoint = "http://127.0.0.1:" + str(server.server_port)
    (codex_home / "config.toml").write_text(
        'model = "gpt-5.1-codex"\nmodel_provider = "fixture"\n'
        '[features]\nplugins = false\napps = false\nremote_plugin = false\n'
        'code_mode_host = false\nbrowser_use = false\nin_app_browser = false\nshell_snapshot = false\n'
        '[model_providers.fixture]\nname = "RunDesk fixture"\n'
        'base_url = "' + endpoint + '/v1"\nenv_key = "RUNDESK_CAPTURE_TEST_KEY"\n'
        'wire_api = "responses"\nrequires_openai_auth = false\nsupports_websockets = false\n')
    app = AppServer(executable, home, codex_home, workspace)
    try:
        initialized = app.call("initialize", {
            "clientInfo": {"name": "rundesk_native_mcp_fixture", "version": "1"},
            "capabilities": {"experimentalApi": True},
        })
        app.send({"method": "initialized"})
        config = {"url": endpoint + "/mcp",
                  "http_headers": {"Authorization": "Bearer fake-task-capability"},
                  "enabled": True, "required": True, "startup_timeout_sec": 15, "tool_timeout_sec": 60}
        started = app.call("thread/start", {
            "cwd": str(workspace), "approvalPolicy": "never", "sandbox": "workspace-write",
            "ephemeral": True, "environments": [],
            "config": {"mcp_servers.rundesk_build_center": config},
        })
        status = app.call("mcpServerStatus/list", {
            "threadId": started["thread"]["id"], "serverName": "rundesk_build_center", "limit": 100,
        })
        captures = list(server.captures)
        methods = [item.get("method") for item in captures if item["http"] == "POST"]
        assert "initialize" in methods and "tools/list" in methods, "MCP initialization/discovery missing"
        assert all(item.get("authorized", True) for item in captures), "Synthetic MCP authorization lost"
        assert all(not item["path"].startswith("/v1") for item in captures), "Unexpected model-provider request"
        entries = status.get("data", [])
        assert any(entry["name"] == "rundesk_build_center" and
                   "build_recipes_list" in entry.get("tools", {}) for entry in entries), "Build tool not discovered"
        return {
            "passed": True, "serverUserAgent": initialized.get("userAgent"),
            "mcpMethods": methods, "getCount": sum(item["http"] == "GET" for item in captures),
            "runtimeStatuses": [entry.get("runtimeStatus") for entry in entries],
            "toolNames": [list(entry.get("tools", {})) for entry in entries],
            "modelTurns": 0, "modelProviderRequests": 0, "pluginRPCs": 0,
            "authentication": "synthetic bearer verified", "temporaryHome": True,
            "statusRpcThreadScoped": True, "configKeys": list(config),
            "limitation": "Verifies local MCP startup/discovery only; no real Docker build or model turn.",
        }
    finally:
        app.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", default=shutil.which("codex") or "/opt/codex/versions/0.163.0/bin/codex")
    parser.add_argument("--output", help="Optional path for the sanitized JSON result")
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), BuildMCP)
    server.captures = []
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        # Current Codex helper setup rejects /tmp; use a temporary local folder.
        with tempfile.TemporaryDirectory(prefix="rundesk-build-mcp-", dir=Path.cwd()) as directory:
            result = exercise(args.codex, server, Path(directory))
        text = json.dumps(result, ensure_ascii=False, indent=2) + "\n"
        if args.output:
            Path(args.output).write_text(text)
        print(text, end="")
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
