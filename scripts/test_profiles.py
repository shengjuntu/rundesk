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
            try: code = profiles.main(list(args))
            except SystemExit as exc: code = exc.code
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
                for agent in ([False, True] if "agent-reach" in p["optionalComponents"] else [False]):
                    dest = self.root / (name + backend + str(agent)); dest.mkdir()
                    profiles.render(p, profiles.resolve(p, backend, agent), "0.144.6", "node:22-bookworm-slim", dest)
                    config = tomllib.loads((dest / "seed/config.toml").read_text())
                    docker = (dest / "Dockerfile").read_text()
                    self.assertNotIn("VOLUME ", docker)
                    self.assertNotIn("docker.sock", docker)
                    self.assertIn("COPY skills/ /etc/codex/skills/", docker)
                    if backend == "exa":
                        self.assertTrue(config["mcp_servers"]["exa"]["enabled"])
                    elif name in ("hyperframes", "video-use", "openmontage"):
                        self.assertEqual(config["mcp_servers"]["video_app_media"]["args"][-1], "serve-mcp")
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
            self.assertTrue(z.getinfo("tools/mcporter").external_attr >> 16 & 0o111)
        original = archives[0].read_bytes()
        code, _, _ = self.cli("pack", "news2douyin", "--codex-version", "0.144.6", "--output", str(archives[0]))
        self.assertEqual(code, 2); self.assertEqual(archives[0].read_bytes(), original)

    def test_dockerfile_is_read_from_editable_file(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        file = copy / "news2douyin/Dockerfile"
        file.write_text(file.read_text().replace("USER node", "RUN echo external-file-change\nUSER node"))
        with patch.object(profiles, "ROOT", copy):
            target = self.root / "edited-context"
            code, _, error = self.cli("prepare", "news2douyin", "--codex-version", "0.157.1", "--base-image", "registry.example.test/library/node:22-bookworm-slim", "--output", str(target))
            self.assertEqual(code, 0, error)
        docker = (target / "Dockerfile").read_text()
        self.assertIn("RUN echo external-file-change", docker)
        self.assertIn("ARG RD_BASE_IMAGE=registry.example.test/library/node:22-bookworm-slim", docker)
        self.assertIn("FROM ${RD_BASE_IMAGE}", docker)

    def test_source_arguments_apply_only_during_package_install(self):
        for name in profiles.PROFILE_IDS:
            with self.subTest(profile=name):
                target = self.root / name; target.mkdir()
                profile = profiles.load_profile(name)
                profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", target)
                docker = (target / "Dockerfile").read_text()
                for arg in ("RD_BASE_IMAGE", "RD_APT_MIRROR", "RD_APT_SECURITY_MIRROR", "RD_NPM_REGISTRY", "RD_PIP_INDEX_URL"):
                    self.assertIn("ARG " + arg + "=", docker)
                self.assertIn('RD_CODEX_MODE="${RD_CODEX_MODE}" RD_NPM_REGISTRY="${RD_NPM_REGISTRY}" sh /opt/rundesk-profile/tools/install-codex.sh', docker)
                self.assertTrue((target / "tools/install-codex.sh").is_file())
                self.assertTrue((target / "runtime/.keep").is_file())
                self.assertIn("COPY runtime/ /opt/rundesk-runtime/", docker)
                self.assertFalse(any(line.startswith("ENV ") and ("RD_NPM_REGISTRY" in line or "RD_PIP_INDEX_URL" in line) for line in docker.splitlines()))
                self.assertTrue((target / "tools/configure-apt.sh").is_file())
                if name in ("news2douyin", "people-search", "video-build"):
                    self.assertIn('PIP_INDEX_URL="${RD_PIP_INDEX_URL}" python3 -m pip install', docker)
                else:
                    self.assertIn('npm_config_registry="${RD_NPM_REGISTRY}" PIP_INDEX_URL="${RD_PIP_INDEX_URL}" sh /opt/rundesk-upstream/install.sh', docker)

    def test_debian_apt_source_override_preserves_format_and_signature(self):
        root = self.root / "apt-root"
        etc = root / "etc"; sources = etc / "apt/sources.list.d"
        sources.mkdir(parents=True)
        (etc / "os-release").write_text('ID=debian\nVERSION_CODENAME=bookworm\n')
        certificates = etc / "ssl/certs/ca-certificates.crt"
        certificates.parent.mkdir(parents=True)
        certificates.write_text("fixture: presence check only, no TLS connection")
        path = sources / "debian.sources"
        path.write_text("Types: deb\nURIs: http://deb.debian.org/debian\nSuites: bookworm bookworm-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n\nTypes: deb\nURIs: http://deb.debian.org/debian-security\nSuites: bookworm-security\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n")
        legacy = sources / "legacy.list"
        legacy.write_text("deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://deb.debian.org/debian bookworm main\ndeb https://third-party.example.test/repo stable main\n")
        env = dict(os.environ, RD_APT_MIRROR="https://mirror.example.test/debian", RD_APT_SECURITY_MIRROR="https://mirror.example.test/debian-security")
        script = ROOT / "examples/_shared/configure-apt.sh"
        subprocess.run(["sh", str(script), str(root)], env=env, check=True)
        content = path.read_text()
        self.assertIn("URIs: https://mirror.example.test/debian\n", content)
        self.assertIn("URIs: https://mirror.example.test/debian-security\n", content)
        self.assertIn("Suites: bookworm bookworm-updates", content)
        self.assertIn("Suites: bookworm-security", content)
        self.assertEqual(content.count("Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg"), 2)
        self.assertIn("https://third-party.example.test/repo stable main", legacy.read_text())
        # Idempotent on already configured images; a later network group may
        # replace RunDesk's own previous origins without touching other repos.
        subprocess.run(["sh", str(script), str(root)], env=env, check=True)
        self.assertEqual(content, path.read_text())
        env = dict(env, RD_APT_MIRROR="https://new.example.test/debian", RD_APT_SECURITY_MIRROR="https://new.example.test/debian-security")
        subprocess.run(["sh", str(script), str(root)], env=env, check=True)
        self.assertIn("URIs: https://new.example.test/debian\n", path.read_text())
        self.assertIn("https://third-party.example.test/repo stable main", legacy.read_text())
        certificates.unlink()
        result = subprocess.run(["sh", str(script), str(root)], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 65)
        self.assertIn("requires CA certificates", result.stderr)
        (etc / "os-release").write_text("ID=alpine\n")
        result = subprocess.run(["sh", str(script), str(root)], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 64)
        self.assertIn("only for Debian", result.stderr)

    def test_research_profiles_install_reviewed_local_agent_reach(self):
        for name in ("news2douyin", "people-search"):
            with self.subTest(profile=name):
                target = self.root / name; target.mkdir()
                profile = profiles.load_profile(name)
                profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", target)
                self.assertEqual(profile["version"], "1.1.2")
                requirements = (target / "requirements.txt").read_text()
                self.assertIn("/opt/rundesk-profile/vendor/agent-reach\n", requirements)
                self.assertNotIn("https://", requirements)
                docker = (target / "Dockerfile").read_text()
                self.assertLess(docker.index("COPY vendor/"), docker.index("python3 -m pip install"))
                self.assertIn("ARG CODEX_VERSION=0.157.1", docker)
                self.assertIn('io.rundesk.codex.version="0.157.1"', docker)
                self.assertEqual(json.loads((target / "profile.lock.json").read_text())["codexVersion"], "0.157.1")
                vendor = target / "vendor/agent-reach"
                self.assertNotIn("force-include", (vendor / "pyproject.toml").read_text())
                for resource in ("LICENSE", "agent_reach/skill/SKILL.md", "agent_reach/guides/setup-exa.md"):
                    self.assertTrue((vendor / resource).is_file(), resource)
                for line in (target / "vendor/agent-reach.sha256").read_text().splitlines():
                    digest, path = line.split("  ", 1)
                    self.assertEqual(hashlib.sha256((vendor / path).read_bytes()).hexdigest(), digest)
                lite = self.root / (name + "-lite"); lite.mkdir()
                profiles.render(profile, profiles.resolve(profile, agent_reach=False), "0.157.1", "node:22-bookworm-slim", lite)
                self.assertFalse((lite / "vendor").exists())
                self.assertNotIn("COPY vendor/", (lite / "Dockerfile").read_text())

    def test_agent_reach_rejects_modified_extra_or_linked_source(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        source = copy / "_shared/vendor/agent-reach"
        file = source / "pyproject.toml"
        original = file.read_bytes()
        with patch.object(profiles, "ROOT", copy):
            file.write_bytes(original + b"\n")
            with self.assertRaisesRegex(profiles.ProfileError, "source digest mismatch"):
                profiles.load_profile("news2douyin")
            file.write_bytes(original)
            extra = source / "unexpected.txt"; extra.write_text("do not sweep unreviewed files")
            with self.assertRaisesRegex(profiles.ProfileError, "file list mismatch"):
                profiles.load_profile("people-search")
            extra.unlink()
            guide = source / "agent_reach/guides/setup-exa.md"
            guide.unlink(); guide.symlink_to(file)
            with self.assertRaisesRegex(profiles.ProfileError, "unsafe Agent-Reach source"):
                profiles.load_profile("news2douyin")

    def test_video_build_defaults_to_pinned_hyperframes_without_video_use(self):
        profile = profiles.load_profile("video-build")
        self.assertEqual(profile["version"], "1.4.0")
        target = self.root / "video-context"; target.mkdir()
        profiles.render(profile, profiles.resolve(profile), "0.144.6", "node:22-bookworm-slim", target)
        docker = (target / "Dockerfile").read_text()
        self.assertIn('io.rundesk.hyperframes.version="0.8.87"', docker)
        self.assertIn('io.rundesk.gsap.version="3.15.0"', docker)
        self.assertIn('io.rundesk.video-use.enabled="false"', docker)
        self.assertIn('HYPERFRAMES_BROWSER_PATH=/usr/bin/chromium', docker)
        self.assertIn(' chromium ', docker)
        self.assertIn('npm ci --prefix /opt/hyperframes', docker)
        self.assertNotIn('COPY vendor/video-use', docker)
        self.assertNotIn('librosa', (target / "requirements.txt").read_text())
        self.assertFalse((target / "vendor").exists())
        for reference in profile["skillFiles"]:
            self.assertEqual((target / "skills/build-video-artifacts" / reference).read_bytes(), (ROOT / "examples/video-build/skills/build-video-artifacts" / reference).read_bytes())
        lock = json.loads((target / "profile.lock.json").read_text())
        self.assertFalse(lock["selection"]["videoUse"])
        self.assertEqual(hashlib.sha256((target / "hyperframes/package-lock.json").read_bytes()).hexdigest(), lock["dependencies"]["hyperframes"]["npmLockSHA256"])
        self.assertIn("hyperframes-html-gsap-render", (target / "tools/profile_doctor.py").read_text())
        self.assertIn("distinctFrames", (target / "tools/hyperframes_smoke.py").read_text())
        # Diagnostic protocol 2 and old archive readers remain available without installing the old editor.
        actual = json.loads(subprocess.check_output([sys.executable, "-I", str(target / "tools/video_use_adapter.py"), "--version"]))
        self.assertEqual(actual, {"schemaVersion": 1, "adapterVersion": 2, "upstreamCommit": lock["dependencies"]["videoUse"]["commit"]})

    def test_video_use_is_explicit_compatibility_component(self):
        target = self.root / "video-compatible"
        code, _, error = self.cli("prepare", "video-build", "--with-video-use", "--codex-version", "0.144.6", "--output", str(target))
        self.assertEqual(code, 0, error)
        docker = (target / "Dockerfile").read_text()
        self.assertIn('io.rundesk.video-use.enabled="true"', docker)
        self.assertIn('io.rundesk.video-use.adapter="2"', docker)
        self.assertIn('COPY vendor/video-use.tar.gz', docker)
        self.assertIn('--no-deps', docker)
        self.assertTrue((target / "vendor/video-use.tar.gz").is_file())
        self.assertIn('librosa==', (target / "requirements.txt").read_text())
        self.assertEqual(self.cli("validate", "news2douyin", "--with-video-use")[0], 2)

    def test_hyperframes_rejects_tampered_dependency_lock(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        lock = copy / "_shared/hyperframes/package-lock.json"
        lock.write_bytes(lock.read_bytes() + b" ")
        with patch.object(profiles, "ROOT", copy):
            code, _, error = self.cli("validate", "video-build")
        self.assertEqual(code, 2)
        self.assertIn("HyperFrames npm lock digest mismatch", json.loads(error)["error"])

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

    def test_declared_skill_rejects_traversal_and_nested_symlink(self):
        copy = self.root / "examples"
        shutil.copytree(ROOT / "examples", copy, ignore=shutil.ignore_patterns("__pycache__"))
        manifest = copy / "video-build/profile.json"
        original = json.loads(manifest.read_text())
        invalid = dict(original, skillFiles=original["skillFiles"] + ["references/../SKILL.md"])
        manifest.write_text(json.dumps(invalid))
        with patch.object(profiles, "ROOT", copy):
            self.assertEqual(self.cli("validate", "video-build")[0], 2)
            manifest.write_text(json.dumps(original))
            skill = copy / "video-build/skills/build-video-artifacts"
            refs = skill / "references"
            refs.rename(skill / "hidden-reference-target")
            refs.symlink_to(skill / "hidden-reference-target", target_is_directory=True)
            self.assertEqual(self.cli("validate", "video-build")[0], 2)

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

    def test_agent_reach_runtime_config_is_per_home_and_preserves_existing(self):
        config = module("agent_config", ROOT / "examples/_shared/agent_reach_config.py")
        first = self.root / 'first'; second = self.root / 'second'
        path = config.initialize(first)
        self.assertEqual(json.loads(path.read_text())['mcpServers']['exa']['baseUrl'], 'https://mcp.exa.ai/mcp')
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        path.write_text('{"mcpServers":{"custom":{"baseUrl":"https://example.com/mcp"}}}')
        original = path.read_bytes()
        self.assertEqual(config.initialize(first), path)
        self.assertEqual(path.read_bytes(), original)
        self.assertNotEqual(config.initialize(second), path)
        path.unlink(); alternate = path.with_suffix('.jsonc'); alternate.write_text('// keep JSONC\n{}')
        self.assertEqual(config.initialize(first), alternate)
        self.assertFalse(path.exists())

    def test_default_includes_complete_reach_tools_and_codex_layer_is_last(self):
        p = profiles.load_profile('news2douyin')
        self.assertTrue(profiles.resolve(p)['agentReach'])
        self.assertFalse(profiles.resolve(p, agent_reach=False)['agentReach'])
        target = self.root / 'context'; target.mkdir()
        profiles.render(p, profiles.resolve(p), '0.144.6', 'node:22-bookworm-slim', target)
        docker = (target / 'Dockerfile').read_text()
        self.assertLess(docker.index('npm ci'), docker.index('ARG CODEX_VERSION'))
        self.assertIn('/etc/codex/skills/agent-reach', docker)
        self.assertIn(' gh ', docker)
        self.assertNotIn('wigolo', docker)
        self.assertNotIn('chromium', docker)
        lock = json.loads((target / 'agent-tools/package-lock.json').read_text())
        for name, version in json.loads((ROOT / 'examples/_shared/dependencies.json').read_text())['npm'].items():
            self.assertEqual(lock['packages']['node_modules/' + name]['version'], version)
        self.assertIn('COPY tools/mcporter /usr/local/bin/mcporter', docker)

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
