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
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import recipes as profiles


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

    def test_catalog_and_unified_cli(self):
        code, out, _ = self.cli("list")
        self.assertEqual(code, 0)
        self.assertEqual([p["id"] for p in json.loads(out)["items"]], list(profiles.PROFILE_IDS))
        for name in profiles.PROFILE_IDS:
            data = subprocess.check_output([sys.executable, str(ROOT / "scripts/recipes.py"), "inspect", name], cwd=self.root)
            self.assertEqual(json.loads(data)["profile"]["id"], name)

    def test_all_context_variants_and_toml(self):
        import tomllib
        video_helpers = {"hyperframes_smoke.py", "video_smoke.py", "video_use_adapter.py", "video_use_smoke.py"}
        research_helpers = {"agent_reach_config.py", "mcporter", "read_web.py"}
        for name in profiles.PROFILE_IDS:
            p = profiles.load_profile(name)
            for backend in p["searchBackends"]:
                for agent in ([False, True] if "agent-reach" in p["optionalComponents"] else [False]):
                    dest = self.root / (name + backend + str(agent)); dest.mkdir()
                    profiles.render(p, profiles.resolve(p, backend, agent), "0.144.6", "node:22-bookworm-slim", dest)
                    config = tomllib.loads((dest / "seed/config.toml").read_text())
                    self.assertNotIn("sharedFiles", json.loads((dest / "profile.lock.json").read_text()))
                    docker = (dest / "Dockerfile").read_text()
                    tools = {file.name for file in (dest / "tools").iterdir()}
                    if name in ("news2douyin", "people-search"):
                        self.assertFalse(tools & video_helpers)
                        self.assertTrue({"profile_doctor.py", "read_web.py", "install-codex.sh"} <= tools)
                        if not agent:
                            # Run the real research doctor without video helpers.
                            # All process calls are mocked; an accidental video
                            # subprocess would fail rather than contacting tools.
                            doctor = module("research_doctor_" + name, dest / "tools/profile_doctor.py")
                            pins = json.loads((dest / "profile.lock.json").read_text())["dependencies"]["python"]
                            def checked_command(argv):
                                return {"ok": True, "exitCode": 0, "output": "codex-cli 0.144.6" if argv == ["codex", "--version"] else "fixture"}
                            with patch.object(sys, "argv", ["doctor", "--manifest", str(dest / "profile.lock.json")]), \
                                 patch.object(doctor, "run", side_effect=checked_command), \
                                 patch.object(doctor, "os", SimpleNamespace(environ={"HOME": str(self.root)})), \
                                 patch.object(doctor.importlib.metadata, "version", side_effect=lambda package: pins[package]), \
                                 patch.object(doctor.Path, "is_file", return_value=True), \
                                 patch.object(doctor.tempfile, "TemporaryDirectory", side_effect=lambda *a, **kw: contextlib.nullcontext(str(self.root))), \
                                 patch.object(doctor.subprocess, "run", side_effect=AssertionError("Unexpected video subprocess")), \
                                 contextlib.redirect_stdout(io.StringIO()) as output:
                                self.assertEqual(doctor.main(), 0)
                            self.assertTrue(json.loads(output.getvalue())["ready"])
                    elif name == "video-build":
                        self.assertTrue(video_helpers <= tools)
                        self.assertFalse(tools & research_helpers)
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
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
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

    def test_canonical_tool_and_config_edits_survive_default_export(self):
        copy = self.root / "canonical"
        shutil.copytree(ROOT / "recipes", copy)
        helper = copy / "news2douyin/tools/read_web.py"
        helper.parent.mkdir(parents=True, exist_ok=True)
        helper.write_text(profiles.context_file(copy, "news2douyin", "tools/read_web.py").read_text() + "\n# operator-tool-edit\n")
        seed = copy / "news2douyin/seed/config.toml"
        seed.write_text(seed.read_text() + "\n# operator-config-edit\n")
        with patch.object(profiles, "ROOT", copy):
            target = self.root / "edited"
            code, _, err = self.cli("prepare", "news2douyin", "--codex-version", "0.157.1", "--output", str(target))
            self.assertEqual(code, 0, err)
            self.assertEqual((target / "tools/read_web.py").read_bytes(), helper.read_bytes())
            self.assertEqual((target / "seed/config.toml").read_bytes(), seed.read_bytes())
            self.assertTrue((copy / "news2douyin/recipe.json").is_file())
            self.assertFalse((copy / "news2douyin/profile.lock.json").exists())
            self.assertFalse((target / "recipe.json").exists())
            self.assertFalse((target / "variants").exists())

    def test_output_cannot_enter_canonical_tree_even_through_alias(self):
        copy = self.root / "canonical"
        shutil.copytree(ROOT / "recipes", copy)
        alias = self.root / "source-alias"; alias.symlink_to(copy, target_is_directory=True)
        with patch.object(profiles, "ROOT", copy):
            for output in (copy / "news2douyin/generated", alias / "news2douyin/generated"):
                code, _, err = self.cli("prepare", "news2douyin", "--codex-version", "0.157.1", "--output", str(output))
                self.assertEqual(code, 2, err)
                self.assertFalse(output.exists())
        existing = self.root / "existing"; existing.mkdir()
        output_link = self.root / "out-link"; output_link.symlink_to(existing, target_is_directory=True)
        self.assertEqual(self.cli("prepare", "news2douyin", "--codex-version", "0.157.1", "--output", str(output_link))[0], 2)

    def test_generated_metadata_twins_are_rejected(self):
        copy = self.root / "canonical"
        shutil.copytree(ROOT / "recipes", copy)
        directory = copy / "news2douyin"
        (directory / "profile.lock.json").write_bytes((directory / "recipe.json").read_bytes())
        with patch.object(profiles, "ROOT", copy):
            self.assertEqual(self.cli("validate", "news2douyin")[0], 2)

    def test_legacy_validator_keeps_operator_edits_and_rejects_source_export(self):
        copy = self.root / "canonical"
        shutil.copytree(ROOT / "recipes", copy)
        helper = copy / "news2douyin/tools/read_web.py"
        helper.parent.mkdir(parents=True, exist_ok=True)
        helper.write_text(profiles.context_file(copy, "news2douyin", "tools/read_web.py").read_text() + "\n# retained-operator-edit\n")
        before = {str(p.relative_to(copy)): p.read_bytes() for p in copy.rglob("*") if p.is_file()}
        legacy = module("legacy_recipe_validator", ROOT / "scripts/embed-profiles.py")
        with patch.object(profiles, "ROOT", copy), patch.object(sys, "argv", ["embed-profiles.py", "--check"]), contextlib.redirect_stdout(io.StringIO()):
            legacy.main()
        after = {str(p.relative_to(copy)): p.read_bytes() for p in copy.rglob("*") if p.is_file()}
        self.assertEqual(before, after)
        exported = self.root / "exported"
        with patch.object(profiles, "ROOT", copy), patch.object(sys, "argv", ["embed-profiles.py", "--output-dir", str(exported), "--codex-version", "0.157.1"]), contextlib.redirect_stdout(io.StringIO()):
            legacy.main()
        for name in profiles.PROFILE_IDS:
            self.assertTrue((exported / name / "profile.lock.json").is_file())
            self.assertFalse((exported / name / "recipe.json").exists())
            self.assertFalse((exported / name / "variants").exists())
        self.assertEqual((exported / "news2douyin/tools/read_web.py").read_bytes(), helper.read_bytes())
        self.assertTrue((exported / "generic/Dockerfile").is_file())
        self.assertEqual(before, {str(p.relative_to(copy)): p.read_bytes() for p in copy.rglob("*") if p.is_file()})
        alias = self.root / "alias"; alias.symlink_to(copy, target_is_directory=True)
        with patch.object(profiles, "ROOT", copy), patch.object(sys, "argv", ["embed-profiles.py", "--output-dir", str(alias / "generated")]), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as result: legacy.main()
        self.assertEqual(result.exception.code, 2)
        self.assertFalse((copy / "generated").exists())

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
        script = profiles.context_file(ROOT / "recipes", "news2douyin", "tools/configure-apt.sh")
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
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
        source = copy / "common/vendor/agent-reach-cf70302e"
        file = source / "pyproject.toml"
        original = file.read_bytes()
        with patch.object(profiles, "ROOT", copy):
            file.write_bytes(original + b"\n")
            with self.assertRaisesRegex(profiles.ProfileError, "source digest mismatch"):
                profiles.load_profile("news2douyin")
            file.write_bytes(original)
            extra = source / "unexpected.txt"; extra.write_text("do not sweep unreviewed files")
            with self.assertRaisesRegex(profiles.ProfileError, "file list mismatch"):
                profiles.load_profile("news2douyin")
            extra.unlink()
            guide = source / "agent_reach/guides/setup-exa.md"
            guide.unlink(); guide.symlink_to(file)
            with self.assertRaisesRegex(ValueError, "[Ss]ymlink|[Uu]nsafe|[Ll]ink"):
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
            self.assertEqual((target / "skills/build-video-artifacts" / reference).read_bytes(), (ROOT / "recipes/video-build/skills/build-video-artifacts" / reference).read_bytes())
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
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
        lock = copy / "video-build/hyperframes/package-lock.json"
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
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
        secret = copy / "news2douyin/skills/collect-news-evidence/auth.json"
        secret.write_text('{"token":"test-sentinel"}')
        with patch.object(profiles, "ROOT", copy):
            target = self.root / "context"
            code, _, err = self.cli("prepare", "news2douyin", "--codex-version", "0.144.6", "--output", str(target))
            self.assertEqual(code, 0, err); self.assertFalse(list(target.rglob("auth.json")))
            skill = secret.parent / "SKILL.md"; skill.unlink(); skill.symlink_to(secret)
            self.assertEqual(self.cli("validate", "news2douyin")[0], 2)

    def test_declared_skill_rejects_traversal_and_nested_symlink(self):
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
        manifest = copy / "video-build/recipe.json"
        original = json.loads(manifest.read_text())
        invalid = json.loads(json.dumps(original)); invalid["profile"]["skillFiles"] += ["references/../SKILL.md"]
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
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy, ignore=shutil.ignore_patterns("__pycache__"))
        path = copy / "news2douyin/recipe.json"
        data = json.loads(path.read_text()); data["profile"]["resources"]["cpus"] = True
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

    def test_doctor_video_build_has_render_tmpfs_and_keeps_isolation(self):
        for name in profiles.PROFILE_IDS:
            for runtime_ok in ([True, False] if name == "video-build" else [True]):
                with self.subTest(profile=name, runtime_ok=runtime_ok):
                    calls = []

                    def docker_command(argv, timeout=30):
                        calls.append((list(argv), timeout))
                        if argv[1:3] == ["image", "inspect"]:
                            return {"ok": True, "output": name + "\n"}
                        if argv[1] == "run":
                            return {"ok": runtime_ok, "exitCode": 0 if runtime_ok else 2,
                                    "output": "fixture runtime check"}
                        return {"ok": True, "exitCode": 0}

                    with patch.object(profiles, "docker_prefix", return_value=["docker"]), \
                         patch.object(profiles, "command", side_effect=docker_command):
                        result = profiles.doctor(profiles.load_profile(name), "fixture:already-built")
                    self.assertEqual(result["ready"], runtime_ok)
                    self.assertFalse(result["modelTested"])
                    self.assertFalse(result["networkTested"])
                    run, timeout = next(call for call in calls if call[0][1] == "run")
                    flag = lambda name: run[run.index(name) + 1]
                    self.assertEqual(flag("--tmpfs"), "/tmp:rw,nosuid,nodev,size=" + ("2g" if name == "video-build" else "512m") + ",mode=1777")
                    self.assertEqual(flag("--network"), "none")
                    self.assertEqual(flag("--user"), "1000:1000")
                    self.assertEqual(flag("--cap-drop"), "ALL")
                    self.assertEqual(flag("--security-opt"), "no-new-privileges:true")
                    self.assertIn("--read-only", run)
                    self.assertNotIn("--privileged", run)
                    self.assertNotIn("--mount", run)
                    self.assertEqual(run[-3:], ["fixture:already-built", "/opt/rundesk-profile/tools/profile_doctor.py", "--json"])
                    self.assertEqual(timeout, 420)
                    cleanup = [call[0] for call in calls if call[0][1] == "rm"]
                    self.assertEqual(cleanup, [["docker", "rm", "--force", flag("--name")]])

    def test_agent_reach_runtime_config_is_per_home_and_preserves_existing(self):
        config = module("agent_config", profiles.context_file(ROOT / "recipes", "news2douyin", "tools/agent_reach_config.py"))
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
        for name, version in profiles.dependency_pins()['npm'].items():
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
        read = module("read_web", profiles.context_file(ROOT / "recipes", "news2douyin", "tools/read_web.py"))
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


class SharedInputTest(unittest.TestCase):
    """Shared authoring files must become bounded, ordinary context files."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.local = self.root / "example"; self.local.mkdir()
        self.common = self.root / "common"; self.common.mkdir()
        self.bundle = self.common / "bundle"; self.bundle.mkdir()
        (self.bundle / "a").write_text("aaaa")
        (self.bundle / "b").write_text("bbbb")
        self.descriptor = {"sharedFiles": {"tools": "bundle"}}

    def files(self, descriptor=None):
        return profiles.context_files(self.root, "example", self.descriptor if descriptor is None else descriptor)

    def test_tree_files_and_local_overrides_resolve_without_mutating_source(self):
        (self.bundle / "a").chmod(0o755)
        local = self.local / "tools/a"; local.parent.mkdir(); local.write_text("override")
        before = {str(p.relative_to(self.root)): p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        files = self.files()
        self.assertEqual(files["tools/a"], local)
        self.assertEqual(files["tools/b"].read_bytes(), b"bbbb")
        self.assertEqual(self.files({"sharedFiles": {"upstream": "bundle"}})["upstream/a"].read_bytes(), b"aaaa")
        self.assertEqual(before, {str(p.relative_to(self.root)): p.read_bytes() for p in self.root.rglob("*") if p.is_file()})
        # A stale shared reference is an error even if a local override exists.
        (self.bundle / "a").unlink()
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files({"sharedFiles": {"tools/a": "bundle/a"}})

    def test_shared_paths_types_reserved_targets_and_overlap_are_rejected(self):
        invalid = ("", ".", "..", "/etc/passwd", "a//b", "a/./b", "a/../b", "a/", "a\\b", "C:/file", "bad\nfile", "bad\x00file", "bad\x85file")
        for path in invalid:
            for target, source in ((path, "bundle/a"), ("tools/a", path)):
                with self.subTest(target=target, source=source), self.assertRaises((ValueError, profiles.ProfileError)):
                    self.files({"sharedFiles": {target: source}})
        for target in ("recipe.json", "recipe.json/child", "profile.lock.json", "SHA256SUMS", "upstream/upstream.json", "upstream/upstream.json/child"):
            with self.subTest(reserved=target), self.assertRaises((ValueError, profiles.ProfileError)):
                self.files({"sharedFiles": {target: "bundle/a"}})
        for value in (None, [], "bundle", {"tools": None}, {"tools": ["bundle"]}, {"tools": "bundle", "tools/a": "bundle/a"}):
            with self.subTest(mapping=value), self.assertRaises((ValueError, profiles.ProfileError)):
                self.files({"sharedFiles": value})

    def test_duplicate_metadata_keys_are_rejected_before_resolution(self):
        metadata = self.local / "recipe.json"
        ambiguous = (
            '{"sharedFiles":{"tools/a":"bundle/a"},"sharedFiles":{"tools/b":"bundle/b"}}',
            '{"sharedFiles":{"tools/a":"bundle/a","tools/a":"bundle/b"}}',
        )
        for text in ambiguous:
            metadata.write_text(text)
            with self.subTest(metadata=text), self.assertRaises((ValueError, profiles.ProfileError)):
                profiles.context_files(self.root, "example")

    def test_source_and_local_links_and_special_files_are_rejected(self):
        external = self.root / "external"; external.mkdir()
        (external / "file").write_text("outside")
        link = self.common / "linked"; link.symlink_to(external, target_is_directory=True)
        for source in ("linked", "linked/file"):
            with self.subTest(source=source), self.assertRaises((ValueError, profiles.ProfileError)):
                self.files({"sharedFiles": {"tools": source}})
        nested = self.bundle / "link"; nested.symlink_to(external / "file")
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()
        nested.unlink()
        local = self.local / "tools"; local.symlink_to(external, target_is_directory=True)
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()
        local.unlink()
        if hasattr(os, "mkfifo"):
            special = self.bundle / "pipe"; os.mkfifo(special)
            with self.assertRaises((ValueError, profiles.ProfileError)):
                self.files()

    def test_file_directory_conflicts_are_not_implicit_tree_overrides(self):
        tools = self.local / "tools"; tools.write_text("not a directory")
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()
        tools.unlink(); (tools / "a").mkdir(parents=True)
        (tools / "a/child").write_text("not a file")
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()
        shutil.rmtree(tools)
        (self.local / "variants").write_text("local file")
        # Authoring variants are filtered only after the input boundary has
        # rejected a file that conflicts with a declared shared directory.
        with self.assertRaises((ValueError, profiles.ProfileError)):
            self.files({"sharedFiles": {"variants": "bundle"}})

    def test_shared_and_local_budgets_are_combined_before_override(self):
        tools = self.local / "tools"; tools.mkdir()
        for name in ("a", "b"):
            (tools / name).write_text("same")
        # Each tree fits alone; together there are four input files, even
        # though the two local files override the two shared output names.
        with patch.object(profiles.shared_inputs, "MAX_FILES", 3), self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()
        with patch.object(profiles.shared_inputs, "MAX_BYTES", 12), self.assertRaises((ValueError, profiles.ProfileError)):
            self.files()

    def render_fixture(self):
        """Make a tiny valid ordinary recipe; its source has no shared map."""
        descriptor = json.loads((ROOT / "recipes/news2douyin/recipe.json").read_text())
        descriptor.pop("sharedFiles", None)
        profile = descriptor["profile"]
        profile.update(id="example", skill="fixture", aptPackages=[], pythonPackages=[],
                       searchBackends=["none"], defaultSearchBackend="none", tools=[],
                       outputs=[], optionalComponents=[])
        descriptor["dependencies"] = {"python": {}, "npm": {}}
        descriptor["selection"] = {"searchBackend": "none", "agentReach": False, "videoUse": False}
        (self.local / "recipe.json").write_text(json.dumps(descriptor, separators=(",", ":")))
        labels = ("org.opencontainers.image.title", "org.opencontainers.image.version", "io.rundesk.profile",
                  "io.rundesk.profile.schema", "io.rundesk.codex.version", "io.rundesk.agent-reach",
                  "io.rundesk.capabilities.tools", "io.rundesk.capabilities.skills", "io.rundesk.capabilities.mcp")
        (self.local / "Dockerfile").write_text("ARG RD_BASE_IMAGE=node:22-bookworm-slim\nFROM ${RD_BASE_IMAGE}\nARG CODEX_VERSION=0.157.1\n" +
                                               "".join('LABEL ' + name + '="fixture"\n' for name in labels))
        skill = self.local / "skills/fixture"; (skill / "agents").mkdir(parents=True)
        (skill / "SKILL.md").write_text("---\nname: fixture\ndescription: A local build-boundary fixture.\n---\n")
        (skill / "agents/openai.yaml").write_text("display_name: Fixture\n")
        seed = self.local / "seed"; seed.mkdir()
        (seed / "config.toml").write_text("check_for_update_on_startup = false\n")
        with patch.object(profiles, "ROOT", self.root), patch.object(profiles, "PROFILE_IDS", ("example",)):
            return profiles.load_profile("example")

    def test_generated_snapshot_and_manifest_also_obey_context_budgets(self):
        profile = self.render_fixture()
        with patch.object(profiles, "ROOT", self.root):
            inputs = profiles.context_files(self.root, "example")
            input_bytes = sum(path.stat().st_size for path in inputs.values())
            baseline = self.root / "unlimited"
            profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", baseline)
            files = [path for path in baseline.rglob("*") if path.is_file()]
            without_manifest = sum(path.stat().st_size for path in files if path.name != "SHA256SUMS")
            self.assertEqual(len(files), len(inputs) + 1)
            self.assertGreater(without_manifest, input_bytes)
            with patch.object(profiles.shared_inputs, "MAX_FILES", len(inputs)):
                self.assertEqual(len(profiles.context_files(self.root, "example")), len(inputs))
                with self.assertRaises((ValueError, profiles.ProfileError)):
                    profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", self.root / "file-cap")
            # The first cap admits source inputs but excludes expanded generated
            # metadata. The second admits every output except the hash manifest.
            for name, limit in (("generated-metadata-cap", input_bytes), ("manifest-cap", without_manifest)):
                with self.subTest(cap=name), patch.object(profiles.shared_inputs, "MAX_BYTES", limit):
                    self.assertEqual(sum(path.stat().st_size for path in profiles.context_files(self.root, "example").values()), input_bytes)
                    with self.assertRaises((ValueError, profiles.ProfileError)):
                        profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", self.root / name)

    def test_direct_render_rejects_generated_metadata_directories(self):
        profile = self.render_fixture()
        for index, (name, child) in enumerate((("profile.lock.json", False), ("upstream/upstream.json", False),
                                               ("SHA256SUMS", False), ("SHA256SUMS", True))):
            reserved = self.local / name; reserved.mkdir(parents=True)
            if child:
                (reserved / "stale").write_text("must not hide behind manifest filtering")
            with self.subTest(source=name, child=child), patch.object(profiles, "ROOT", self.root):
                # Profile was already loaded before the conflicting directory
                # appeared; render must validate its own inputs again.
                with self.assertRaises((ValueError, profiles.ProfileError)):
                    profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", self.root / ("blocked-" + str(index)))
            shutil.rmtree(self.local / name.split("/")[0])

    def test_exports_freeze_shared_bytes_and_drop_authoring_references(self):
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy)
        with patch.object(profiles, "ROOT", copy):
            shared = profiles.context_file(copy, "news2douyin", "tools/install-codex.sh")
            first = self.root / "first"
            profile = profiles.load_profile("news2douyin")
            profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", first)
            frozen = (first / "tools/install-codex.sh").read_bytes()
            self.assertTrue((first / "tools/install-codex.sh").stat().st_mode & 0o111)
            shared.write_bytes(frozen + b"\n# later shared edit\n")
            second = self.root / "second"
            profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", second)
        self.assertEqual((first / "tools/install-codex.sh").read_bytes(), frozen)
        self.assertEqual((second / "tools/install-codex.sh").read_bytes(), shared.read_bytes())
        for context in (first, second):
            self.assertNotIn("sharedFiles", json.loads((context / "profile.lock.json").read_text()))
            self.assertFalse(any(p.is_symlink() for p in context.rglob("*")))
            self.assertFalse((context / "common").exists())
            for line in (context / "SHA256SUMS").read_text().splitlines():
                digest, path = line.split("  ", 1)
                self.assertEqual(hashlib.sha256((context / path).read_bytes()).hexdigest(), digest)

    def test_shared_and_ordinary_authoring_export_identical_contexts(self):
        copy = self.root / "recipes"
        shutil.copytree(ROOT / "recipes", copy)
        with patch.object(profiles, "ROOT", copy):
            for name in profiles.PROFILE_IDS:
                with self.subTest(profile=name):
                    profile = profiles.load_profile(name)
                    first = self.root / (name + "-shared")
                    profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", first)
                    for target, source in profiles.context_files(copy, name).items():
                        local = copy / name / target
                        if source != local:
                            local.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copyfile(source, local)
                            local.chmod(source.stat().st_mode & 0o777)
                    descriptor = copy / name / "recipe.json"
                    data = json.loads(descriptor.read_text()); data.pop("sharedFiles", None)
                    descriptor.write_text(json.dumps(data))
                    profile = profiles.load_profile(name)
                    second = self.root / (name + "-ordinary")
                    profiles.render(profile, profiles.resolve(profile), "0.157.1", "node:22-bookworm-slim", second)
                    def snapshot(path):
                        return {str(p.relative_to(path)): (p.read_bytes(), p.stat().st_mode & 0o111) for p in path.rglob("*") if p.is_file()}
                    self.assertEqual(snapshot(first), snapshot(second))


if __name__ == "__main__":
    unittest.main(verbosity=2)
