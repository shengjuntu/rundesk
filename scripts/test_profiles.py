#!/usr/bin/env python3
"""Contract tests for generated profiles; no Docker/network/model required."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "examples"))
import profiles


def module(name, file):
    spec = importlib.util.spec_from_file_location(name, file)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value)
    return value


class ProfilesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def cli(self, *args):
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            code = profiles.main(list(args))
        return code, stdout.getvalue(), stderr.getvalue()

    def test_catalog_and_uniform_wrappers(self):
        code, out, _ = self.cli("list")
        self.assertEqual(code, 0)
        self.assertEqual([p["id"] for p in json.loads(out)["items"]], list(profiles.PROFILE_IDS))
        for name in profiles.PROFILE_IDS:
            data = subprocess.check_output([sys.executable, str(ROOT / "examples" / name / "run.py"), "inspect"], cwd=self.root)
            self.assertEqual(json.loads(data)["profile"]["id"], name)

    def test_all_context_variants_and_toml(self):
        import tomllib
        for name in profiles.PROFILE_IDS:
            p = profiles.load_profile(name)
            for backend in p["searchBackends"]:
                for agent in ([False, True] if p["optionalComponents"] else [False]):
                    dest = self.root / (name + backend + str(agent)); dest.mkdir()
                    profiles.render(p, profiles.resolve(p, backend, agent), "0.144.6", "node:22-bookworm-slim", dest)
                    config = tomllib.loads((dest / "seed/config.toml").read_text())
                    docker = (dest / "Dockerfile").read_text()
                    self.assertNotIn("VOLUME ", docker)
                    self.assertNotIn("docker.sock", docker)
                    self.assertIn("COPY skills/ /etc/codex/skills/", docker)
                    if backend == "wigolo":
                        self.assertFalse(config["mcp_servers"]["exa"]["enabled"])
                        self.assertEqual(config["mcp_servers"]["wigolo"]["args"], ["mcp"])
                        self.assertIn("wigolo@0.2.1", docker)
                        self.assertNotIn("research", config["mcp_servers"]["wigolo"]["enabled_tools"])
                    elif backend == "exa":
                        self.assertTrue(config["mcp_servers"]["exa"]["enabled"])
                    else:
                        self.assertNotIn("mcp_servers", config)
                    for line in (dest / "SHA256SUMS").read_text().splitlines():
                        digest, file = line.split("  ", 1)
                        self.assertEqual(hashlib.sha256((dest / file).read_bytes()).hexdigest(), digest)

    def test_reproducible_zip_and_no_existing_output_overwrite(self):
        archives = [self.root / "a.zip", self.root / "b.zip"]
        for output in archives:
            code, out, err = self.cli("pack", "news2douyin", "--codex-version", "0.144.6", "--output", str(output))
            self.assertEqual(code, 0, err); self.assertFalse(json.loads(out)["built"])
        self.assertEqual(archives[0].read_bytes(), archives[1].read_bytes())
        with zipfile.ZipFile(archives[0]) as z:
            self.assertIn("Dockerfile", z.namelist()); self.assertIsNone(z.testzip())
            self.assertTrue(z.getinfo("tools/rundesk-wigolo").external_attr >> 16 & 0o111)
        original = archives[0].read_bytes()
        code, _, _ = self.cli("pack", "news2douyin", "--codex-version", "0.144.6", "--output", str(archives[0]))
        self.assertEqual(code, 2); self.assertEqual(archives[0].read_bytes(), original)

    def test_invalid_versions_backends_and_injection(self):
        for version in ("latest", "", "0.1.0\nRUN echo injected", "$(touch pwned)"):
            code, _, _ = self.cli("prepare", "video-build", "--codex-version", version, "--output", str(self.root / "dest"))
            self.assertEqual(code, 2); self.assertFalse((self.root / "dest").exists())
        self.assertEqual(self.cli("validate", "video-build", "--search-backend", "wigolo")[0], 2)
        self.assertEqual(self.cli("validate", "video-build", "--with-agent-reach")[0], 2)

    def test_only_declared_skill_files_and_symlink_rejection(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        secret = copy / "news2douyin/skills/collect-news-evidence/auth.json"
        secret.write_text('{"token":"test-sentinel"}')
        with patch.object(profiles, "ROOT", copy):
            target = self.root / "context"
            code, _, err = self.cli("prepare", "news2douyin", "--codex-version", "0.144.6", "--output", str(target))
            self.assertEqual(code, 0, err); self.assertFalse(list(target.rglob("auth.json")))
            skill = secret.parent / "SKILL.md"; skill.unlink(); skill.symlink_to(secret)
            self.assertEqual(self.cli("validate", "news2douyin")[0], 2)

    def test_invalid_manifest_types_are_structured_errors(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        path = copy / "news2douyin/profile.json"
        data = json.loads(path.read_text()); data["resources"]["cpus"] = True
        path.write_text(json.dumps(data))
        with patch.object(profiles, "ROOT", copy):
            code, _, err = self.cli("validate", "news2douyin")
        self.assertEqual(code, 2); self.assertIn("Invalid resource", json.loads(err)["error"])

    def test_doctor_no_docker_and_wrong_image_fail_closed(self):
        p = profiles.load_profile("news2douyin")
        with patch.object(profiles.shutil, "which", return_value=None):
            self.assertFalse(profiles.doctor(p)["ready"])
        with patch.object(profiles, "docker_prefix", return_value=["docker"]), patch.object(profiles, "command", side_effect=[{"ok": True}, {"ok": True, "output": "video-build\n"}]) as run:
            result = profiles.doctor(p, "example:tag")
            self.assertFalse(result["ready"]); self.assertEqual(run.call_count, 2)

    def test_wigolo_wrapper_uses_runtime_home_and_no_inner_model(self):
        fake = self.root / "node"
        fake.write_text('#!' + sys.executable + '\nimport os,json,sys\nprint(json.dumps({"data":os.environ["WIGOLO_DATA_DIR"],"key":os.environ.get("OPENAI_API_KEY"),"provider":os.environ.get("WIGOLO_LLM_PROVIDER"),"ttl":os.environ["CACHE_TTL_SEARCH"],"args":sys.argv[1:]}))\n')
        fake.chmod(0o755)
        env = {"PATH": str(self.root) + os.pathsep + "/usr/bin:/bin", "HOME": str(self.root), "OPENAI_API_KEY": "fixture-key", "WIGOLO_LLM_PROVIDER": "openai"}
        out = subprocess.check_output(["sh", str(ROOT / "examples/_shared/rundesk-wigolo"), "mcp"], env=env)
        data = json.loads(out)
        self.assertEqual(data["data"], str(self.root / ".wigolo")); self.assertTrue((self.root / ".wigolo").is_dir())
        self.assertIsNone(data["key"]); self.assertIsNone(data["provider"])
        self.assertEqual(data["ttl"], "300"); self.assertEqual(data["args"][-1], "mcp")

    def test_probe_cleanup_after_client_failure(self):
        p = profiles.load_profile("news2douyin")
        with patch.object(profiles, "docker_prefix", return_value=["docker"]), patch.object(profiles, "command", side_effect=[{"ok": True}, {"ok": True, "output": "news2douyin\n"}, {"ok": False, "error": "fixture timeout"}, {"ok": True}]) as run:
            self.assertFalse(profiles.doctor(p, "image:tag")["ready"])
            launch = run.call_args_list[2].args[0]
            cleanup = run.call_args_list[3].args[0]
            self.assertEqual(launch[launch.index("--name") + 1], cleanup[-1])
            self.assertEqual(cleanup[1:3], ["rm", "--force"])

    def test_web_evidence_success_failure_and_no_overwrite(self):
        read = module("read_web", ROOT / "examples/_shared/read_web.py")
        class Response(io.BytesIO):
            status = 200; headers = {"Content-Type": "text/markdown"}
        def call(out, fail=False):
            with patch.object(sys, "argv", ["read_web.py", "webpage", "https://example.org/news", "--out", str(out)]), patch.object(read.urllib.request, "urlopen", side_effect=OSError("fixture blocked") if fail else lambda *a, **k: Response(b"# Fixture news")), contextlib.redirect_stdout(io.StringIO()):
                return read.main()
        out = self.root / "read"
        self.assertEqual(call(out), 0)
        evidence = json.loads((out / "source.json").read_text())
        self.assertFalse(evidence["verified"]); self.assertIsNone(evidence["publishedAt"])
        self.assertEqual(evidence["sha256"], hashlib.sha256((out / "content.md").read_bytes()).hexdigest())
        with self.assertRaises(FileExistsError): call(out)
        failed = self.root / "failed"
        self.assertEqual(call(failed, True), 1)
        self.assertEqual(json.loads((failed / "source.json").read_text())["status"], "failed")


if __name__ == "__main__":
    unittest.main(verbosity=2)
