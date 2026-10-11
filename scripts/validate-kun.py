#!/usr/bin/env python3
"""Run local Kun checks with durable logs and atomic, per-step receipts.

Requires Python 3, the Go version from go.mod, Node, and a C compiler for race.
No remote model credentials are required; HTTP uses the local model fixture.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
UI = ["kun-inspect-test.cjs", "kun-panels-test.cjs", "kun-hybrid-ui-test.cjs",
      "kun-harness-test.cjs", "kun-live-ui-test.cjs", "kun-runtime-harness-test.cjs",
      "kun-compare-test.cjs", "kun-hypothesis-test.cjs", "kun-evaluation-test.cjs", "kun-task-sets-test.cjs", "kun-k4-ui-test.cjs"]


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Go executable or absolute path")
    parser.add_argument("--output", default="validation/kun", help="new output directory (never overwrite an existing run)")
    parser.add_argument("--checks", default="go-test,go-vet,go-race,build,windows,syntax,ui,http")
    args = parser.parse_args()
    go = shutil.which(args.go)
    if not go:
        parser.error("Go not found; install the go.mod version or pass --go /path/to/go")
    version = re.search(r'const Version = "([^"]+)"', (ROOT / "internal/app/api_v1.go").read_text()).group(1)
    checks = args.checks.split(",")
    known = ["go-test", "go-vet", "go-race", "build", "windows", "syntax", "ui", "http"]
    if not checks or any(c not in known for c in checks) or len(checks) != len(set(checks)):
        parser.error("--checks must contain unique names from " + ",".join(known))
    if "http" in checks and "build" not in checks:
        parser.error("http requires build in this run so stale binaries cannot pass")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env.setdefault("GOMAXPROCS", "4")
    sources = sorted([ROOT / "go.mod", ROOT / "go.sum", ROOT / "Makefile"] +
                     [p for folder in ("cmd", "internal", "scripts") for p in (ROOT / folder).rglob("*")
                      if p.is_file() and "__pycache__" not in p.parts])
    fingerprint = hashlib.sha256()
    for p in sources:
        fingerprint.update(p.relative_to(ROOT).as_posix().encode() + b"\0" + p.read_bytes() + b"\0")
    report = {"version": version, "startedAt": now(), "status": "running", "passed": False,
              "sourceSHA256": fingerprint.hexdigest(), "selectedChecks": checks,
              "goVersion": subprocess.check_output([go, "version"], text=True).strip(), "checks": []}

    def save():
        temporary = output / "summary.json.tmp"
        temporary.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
        temporary.replace(output / "summary.json")

    def run(name, commands, timeout, extra_env=None):
        record = {"name": name, "status": "running", "startedAt": now(), "log": name + ".log"}
        report["checks"].append(record)
        save()
        print(name + ": running", flush=True)
        started = time.monotonic()
        child = None
        try:
            with (output / record["log"]).open("w") as log:
                for command in commands:
                    log.write("$ " + json.dumps(command, ensure_ascii=False) + "\n")
                    log.flush()
                    child = subprocess.Popen(command, cwd=ROOT, env={**env, **(extra_env or {})},
                                             stdout=log, stderr=subprocess.STDOUT,
                                             start_new_session=(os.name != "nt"))
                    code = child.wait(timeout=timeout)
                    if code:
                        record.update(status="failed", exitCode=code)
                        break
                else:
                    record.update(status="passed", exitCode=0)
        except (subprocess.TimeoutExpired, KeyboardInterrupt) as error:
            if child and child.poll() is None:
                if os.name != "nt":
                    os.killpg(child.pid, signal.SIGKILL)
                else:
                    child.kill()
                child.wait()
            record["status"] = "interrupted" if isinstance(error, KeyboardInterrupt) else "timed_out"
            if isinstance(error, KeyboardInterrupt):
                raise
        except OSError as error:
            record.update(status="failed", error=str(error))
        finally:
            record.update(finishedAt=now(), durationSeconds=round(time.monotonic() - started, 3))
            save()
            print(name + ": " + record["status"], flush=True)

    commands = {
        "go-test": ([[go, "test", "./...", "-count=1", "-timeout=300s"]], 420, {}),
        "go-vet": ([[go, "vet", "./..."]], 180, {}),
        "go-race": ([[go, "test", "-race", "./internal/app", "./internal/kun", "-run", "TestKunEvaluation|TestKunCompar|TestKunHypothesis|TestKunTaskSet|TestKunCase|TestKunSkill|TestFast", "-count=1", "-timeout=180s"]], 300, {}),
        "build": ([[go, "build", "-buildvcs=false", "-trimpath", "-o", "bin/" + binary, "./cmd/" + binary] for binary in ("rundesk", "kun")], 180, {"CGO_ENABLED": "0"}),
        "windows": ([[go, "build", "-buildvcs=false", "-trimpath", "-o", "bin/" + binary + "-windows-amd64.exe", "./cmd/" + binary] for binary in ("rundesk", "kun")], 180, {"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"}),
        "syntax": ([["node", "--check", str(p.relative_to(ROOT))] for folder in ("internal/web", "scripts") for p in sorted((ROOT / folder).rglob("*")) if p.suffix in (".js", ".cjs")], 30, {}),
        "ui": ([["node", "scripts/" + script] for script in UI], 60, {}),
        "http": ([["node", "scripts/kun-evaluation-http-test.cjs"], ["node", "scripts/kun-k4-http-test.cjs"]], 120, {}),
    }
    save()
    try:
        for name in known:
            if name not in checks:
                continue
            if name == "http" and not any(r["name"] == "build" and r["status"] == "passed" for r in report["checks"]):
                report["checks"].append({"name": name, "status": "blocked", "reason": "build did not pass"})
                save()
                continue
            run(name, *commands[name])
    except KeyboardInterrupt:
        report["status"] = "interrupted"
    finally:
        report["passed"] = len(report["checks"]) == len(checks) and all(r["status"] == "passed" for r in report["checks"])
        if report["status"] != "interrupted":
            report["status"] = "passed" if report["passed"] else "failed"
        report["finishedAt"] = now()
        save()
    print(str(output / "summary.json"), flush=True)
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
