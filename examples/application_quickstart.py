#!/usr/bin/env python3
"""Submit one durable task and observe it. Never automatically retry a write.

Set RUNDESK_URL, RUNDESK_APP_TOKEN, RUNDESK_WORKSPACE_ID and a previously
persisted RUNDESK_OPERATION_KEY. A retry of the same operation must use the
same token, key AND input text. Token rotation changes the idempotency namespace.
"""
import argparse
import json
import os
import time
from application_client import RunDesk


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("text")
    parser.add_argument("--wait", type=int, default=60, help="observation timeout, not task cancellation")
    args = parser.parse_args()
    client = RunDesk(os.environ.get("RUNDESK_URL", "http://127.0.0.1:3210"), os.environ["RUNDESK_APP_TOKEN"])
    operation = os.environ["RUNDESK_OPERATION_KEY"]
    task = client.create_task({"workspaceId": os.environ["RUNDESK_WORKSPACE_ID"], "input": {"text": args.text}}, key=operation)
    print(json.dumps({"taskId": task["id"], "sessionId": task["sessionId"], "operationKey": operation}, ensure_ascii=False), flush=True)
    deadline = time.monotonic() + max(0, args.wait)
    terminal = {"completed", "failed", "interrupted", "canceled", "unconfirmed"}
    # Even a replayed receipt is followed by a current-state read.
    while True:
        task = client.task(task["id"])
        if task["status"] in terminal or time.monotonic() >= deadline:
            print(json.dumps(task, ensure_ascii=False, indent=2))
            return 0 if task["status"] == "completed" else 2
        time.sleep(1)


if __name__ == "__main__":
    raise SystemExit(main())
