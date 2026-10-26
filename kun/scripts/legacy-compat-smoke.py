#!/usr/bin/env python3
"""Exercise unchanged 0.16 journals through two real Kun JSONL worker binaries.

Only the local model HTTP endpoint is a fixture. No journal/manifest/database is
modified by this test. The same data and workspace paths are passed to both
versions. It verifies a pending checkpoint and non-repetition of a completed write.
"""
import argparse
import copy
import hashlib
import http.server
import json
import pathlib
import queue
import subprocess
import tempfile
import threading
import time
from collections import deque


class Worker:
    def __init__(self, executable, data):
        self.process = subprocess.Popen(
            [str(executable), "--data", str(data)],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1,
        )
        self.replies = queue.Queue()
        self.errors = deque(maxlen=20)
        self.sequence = 0
        threading.Thread(target=self._read, daemon=True).start()
        threading.Thread(target=self._stderr, daemon=True).start()

    def _read(self):
        for line in self.process.stdout:
            self.replies.put(line)
        self.replies.put(None)

    def _stderr(self):
        for line in self.process.stderr:
            self.errors.append(line.rstrip()[:1000])

    def call(self, method, params=None):
        self.sequence += 1
        request_id = str(self.sequence)
        request = {"version": 16, "id": request_id, "method": method, "params": params}
        self.process.stdin.write(json.dumps(request, separators=(",", ":")) + "\n")
        self.process.stdin.flush()
        try:
            line = self.replies.get(timeout=8)
        except queue.Empty as exc:
            raise AssertionError(f"worker timed out for {method}: {list(self.errors)}") from exc
        if line is None:
            raise AssertionError(f"worker exited during {method}: {list(self.errors)}")
        response = json.loads(line)
        assert response["version"] == 16 and response["id"] == request_id, response
        assert not response.get("error"), response
        return response.get("result")

    def wait(self, status, predicate=lambda state: True):
        deadline = time.monotonic() + 8
        state = None
        while time.monotonic() < deadline:
            state = self.call("state")
            if state["status"] == status and predicate(state):
                return state
            assert state["status"] != "failed", state.get("error")
            time.sleep(0.01)
        raise AssertionError(f"did not reach {status}: {state}")

    def control(self, operation):
        state = self.call("state")
        return self.call("control", {
            "requestId": f"smoke-{state['runId']}-{state['revision']}-{operation}",
            "runId": state["runId"], "expectedStateRevision": state["revision"],
            "operation": operation,
        })

    def close(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=3)
                raise AssertionError("worker did not shut down cleanly")
        assert self.process.returncode == 0, list(self.errors)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def state_summary(state):
    return {key: state.get(key) for key in (
        "runId", "status", "phase", "revision", "step", "budget", "actions",
        "manifest", "resumedFrom",
    )} | {"pendingCallIds": [call["id"] for call in state.get("pending", [])]}


def execute(old, new):
    model_requests = []

    class Model(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            assert self.path == "/v1/chat/completions", self.path
            model_requests.append(request)
            if len(model_requests) == 1:
                message = {"role": "assistant", "content": "", "tool_calls": [
                    {"id": "legacy-first-write", "type": "function", "function": {
                        "name": "write_file", "arguments": json.dumps({"path": "first.txt", "content": "legacy write"}),
                    }},
                    {"id": "legacy-pending-write", "type": "function", "function": {
                        "name": "write_file", "arguments": json.dumps({"path": "second.txt", "content": "resumed write"}),
                    }},
                ]}
            else:
                message = {"role": "assistant", "content": "Completed from the recorded tool results."}
            body = json.dumps({"choices": [{"message": message, "finish_reason": "stop"}],
                               "usage": {"total_tokens": 7}}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Model)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix="kun-legacy-compat-") as temporary:
            root = pathlib.Path(temporary)
            data, workspace = root / "journal", root / "workspace"
            workspace.mkdir()
            start = {
                "sessionId": "legacy-binary-session", "runId": "legacy-source-run",
                "workspace": str(workspace), "input": "write two fixture files",
                "approvalPolicy": "on-request", "contextRevision": "legacy-compat-fixture-v1",
                "config": {"kind": "kun", "endpoint": f"http://127.0.0.1:{server.server_port}/v1",
                           "model": "local-compat-fixture", "maxSteps": 5, "timeoutSeconds": 5,
                           "allowWrite": True, "pauseBeforeModel": True},
            }
            old_worker = Worker(old, data)
            try:
                old_hello = old_worker.call("hello")
                assert old_hello["engineVersion"] == "0.16.0", old_hello
                old_worker.call("start", start)
                old_worker.wait("paused", lambda state: state["phase"] == "before_model")
                old_worker.control("step")
                old_worker.wait("paused", lambda state: len(state.get("pending", [])) == 2)
                old_worker.control("step")
                legacy_safe = old_worker.wait("paused", lambda state: len(state.get("pending", [])) == 1)
                assert len(model_requests) == 1, model_requests
                assert legacy_safe["manifest"]["engineVersion"] == "0.16.0", legacy_safe
                assert (workspace / "first.txt").read_text() == "legacy write"
                assert not (workspace / "second.txt").exists()
            finally:
                old_worker.close()
            # A completed write must never be dispatched again during recovery.
            (workspace / "first.txt").write_text("sentinel: do not overwrite", encoding="utf-8")
            new_worker = Worker(new, data)
            try:
                new_hello = new_worker.call("hello")
                assert new_hello["engineVersion"] == "0.17.0", new_hello
                opened = new_worker.call("state")
                assert opened["runId"] == start["runId"] and opened["status"] == "interrupted", opened
                assert opened["manifest"]["engineVersion"] == "0.16.0", opened
                assert len(model_requests) == 1, "opening journal unexpectedly called model"
                points = new_worker.call("fork.points", {"offset": 0, "limit": 50})
                assert points["items"], points
                selection = dict(points["selection"])
                selection["sequence"] = points["items"][-1]["sequence"]
                bundle = new_worker.call("fork.export", {"selection": selection, "mode": "hybrid"})
                assert bundle["state"]["manifest"]["engineVersion"] == "0.16.0", bundle
                checkpoint = new_worker.call("checkpoint", start)
                assert checkpoint["eligible"] and checkpoint["reason"] == "safe_checkpoint", checkpoint
                assert checkpoint["pending"] == 1, checkpoint
                resumed = copy.deepcopy(start)
                resumed["runId"] = "new-engine-resumed-run"
                resumed["resume"] = checkpoint["selection"]
                new_worker.call("start", resumed)
                recovered = new_worker.wait("paused", lambda state: len(state.get("pending", [])) == 1)
                assert recovered["resumedFrom"]["sourceRunId"] == start["runId"], recovered
                new_worker.control("resume")
                after_pending = new_worker.wait("paused", lambda state: not state.get("pending"))
                assert (workspace / "first.txt").read_text() == "sentinel: do not overwrite"
                assert (workspace / "second.txt").read_text() == "resumed write"
                assert len(model_requests) == 1, "pending tool recovery repeated the model call"
                new_worker.control("resume")
                complete = new_worker.wait("completed")
                assert len(model_requests) == 2 and complete["step"] == 2, complete
                assert complete["budget"]["toolCalls"] == 2, complete
                assert complete["budget"]["reportedTokens"] == 14, complete
                events = new_worker.call("events", {"after": 0})
                assert events, events
                return {
                    "status": "passed", "fixtureOnly": True,
                    "oldHello": old_hello, "newHello": new_hello,
                    "sameDataAndWorkspacePaths": True,
                    "journalModifiedByTest": False,
                    "legacyPausedState": state_summary(legacy_safe),
                    "newEngineOpenedLegacyJournal": state_summary(opened),
                    "legacyForkPoints": len(points["items"]),
                    "legacyForkExport": {"schema": bundle["schema"], "mode": bundle["mode"],
                                         "recordCount": len(bundle.get("records", [])),
                                         "manifestEngineVersion": bundle["state"]["manifest"]["engineVersion"]},
                    "checkpoint": checkpoint,
                    "recoveredPendingState": state_summary(recovered),
                    "completedState": state_summary(complete),
                    "modelHTTPRequests": len(model_requests),
                    "completedWritePreserved": True,
                    "pendingWriteExecuted": True,
                    "toolsRunTotal": complete["budget"]["toolCalls"],
                    "legacyEventsReadable": len(events),
                    "limitations": ["Local model HTTP fixture; no real model or RunDesk/Docker deployment.",
                                    "One safe pending checkpoint and hybrid fork export were exercised; not every historical journal shape."],
                }
            finally:
                new_worker.close()
    finally:
        server.shutdown()
        server.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old", type=pathlib.Path, required=True)
    parser.add_argument("--new", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path,
                        default=pathlib.Path(__file__).resolve().parents[1] / "docs/legacy-binary-compat-0.17.json")
    args = parser.parse_args()
    old, new = args.old.resolve(), args.new.resolve()
    report = {"schemaVersion": 1, "oldBinary": str(old), "newBinary": str(new),
              "oldBinarySHA256": digest(old), "newBinarySHA256": digest(new)}
    try:
        report.update(execute(old, new))
    except Exception as exc:
        report.update({"status": "failed", "error": str(exc)})
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        raise
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"status": report["status"], "output": str(args.output),
                      "modelHTTPRequests": report["modelHTTPRequests"], "toolsRunTotal": report["toolsRunTotal"]}))


if __name__ == "__main__":
    main()
