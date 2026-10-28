#!/usr/bin/env python3
"""Independent upstream recipe boundary tests; no Docker, network or models."""
import hashlib
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "examples"))
import profiles
import upstream_profiles


class SourceBoundaryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.base = self.root / "video-use"
        self.base.mkdir()
        self.profile = {"id": "video-use", "skill": "use-video-use", "upstream": "upstream.json"}
        for name in ("install.sh", "doctor.py"):
            (self.base / name).write_text("# fixture\n")
        self.archive()

    def archive(self, extras=None):
        path = self.base / "source.tar.gz"
        with tarfile.open(path, "w:gz") as out:
            for name, content in {"source/LICENSE": b"MIT fixture", "source/SKILL.md": b"# Native fixture"}.items():
                member = tarfile.TarInfo(name)
                member.size = len(content)
                out.addfile(member, io.BytesIO(content))
            for member, data in extras or []:
                out.addfile(member, io.BytesIO(data) if data is not None else None)
        self.manifest = {"schemaVersion": 1, "repository": upstream_profiles.REPOSITORIES["video-use"],
                         "commit": "a" * 40, "sourceArchive": "source.tar.gz",
                         "sourceSHA256": hashlib.sha256(path.read_bytes()).hexdigest(), "license": "MIT",
                         "installScript": "install.sh", "buildFiles": ["install.sh", "doctor.py"],
                         "skillDirectories": [{"source": ".", "name": "video-use"}], "requiredFiles": ["LICENSE", "SKILL.md"]}
        self.save()

    def save(self):
        (self.base / "upstream.json").write_text(json.dumps(self.manifest))

    def load(self):
        self.save()
        return upstream_profiles.load(self.root, self.profile)

    def test_exact_archive_identity_and_native_entry(self):
        self.assertEqual(self.load()["commit"], "a" * 40)
        self.manifest["sourceSHA256"] = "0" * 64
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            self.load()

    def test_malformed_archive_is_a_structured_validation_error(self):
        path = self.base / "source.tar.gz"
        path.write_bytes(b"not an archive")
        self.manifest["sourceSHA256"] = hashlib.sha256(path.read_bytes()).hexdigest()
        with self.assertRaisesRegex(ValueError, "Invalid upstream source archive"):
            self.load()

    def test_archive_path_and_link_escape_rejected(self):
        for name, link in (("source/../outside", None), ("source/file", "../../outside")):
            with self.subTest(name=name):
                member = tarfile.TarInfo(name)
                if link:
                    member.type = tarfile.SYMTYPE
                    member.linkname = link
                self.archive([(member, b"" if not link else None)])
                with self.assertRaisesRegex(ValueError, "archive"):
                    self.load()

    def test_declared_build_inputs_cannot_traverse_or_follow_symlinks(self):
        self.manifest["buildFiles"].append("../other")
        with self.assertRaisesRegex(ValueError, "relative path"):
            self.load()
        self.manifest["buildFiles"].pop()
        script = self.base / "install.sh"
        script.unlink()
        script.symlink_to(self.base / "doctor.py")
        with self.assertRaisesRegex(ValueError, "symlink"):
            self.load()

    def test_remote_source_is_exact_codeload_commit(self):
        self.manifest.pop("sourceArchive")
        url = "https://codeload.github.com/browser-use/video-use/tar.gz/" + "a" * 40
        self.manifest["sourceURL"] = url
        self.assertEqual(self.load()["sourceURL"], url)
        for other in (url.replace("codeload.github.com", "example.org"), url.replace("a" * 40, "main"), url + "?key=secret"):
            self.manifest["sourceURL"] = other
            with self.assertRaisesRegex(ValueError, "exact reviewed codeload"):
                self.load()

    def test_environment_rejects_credentials_expansion_and_newlines(self):
        for environment in ({"VIDEO_APP_TOKEN": "secret"}, {"HYPERFRAMES_API_KEY": "secret"}, {"NODE_PATH": "${HOME}"}, {"PYTHONPATH": "/tmp\nRUN bad"}):
            with self.subTest(keys=list(environment)):
                self.manifest["environment"] = environment
                with self.assertRaisesRegex(ValueError, "non-credential"):
                    self.load()
        self.manifest["environment"] = {"HYPERFRAMES_SKIP_SKILLS": "1", "NODE_PATH": "/opt/hyperframes/node_modules"}
        self.assertEqual(self.load()["environment"], self.manifest["environment"])


class ActualRecipeTest(unittest.TestCase):
    def test_three_independent_native_contexts_and_runtime_only_mcp(self):
        import tomllib
        for name in ("hyperframes", "video-use", "openmontage"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                profile = profiles.load_profile(name)
                self.assertEqual(profile["version"], "1.0.0")
                destination = Path(tmp)
                lock = profiles.render(profile, profiles.resolve(profile), "0.144.6", "node:22-bookworm-slim", destination)
                self.assertEqual(lock["selection"]["nativeUpstream"], name)
                docker = (destination / "Dockerfile").read_text()
                self.assertIn("/opt/upstreams/" + name, docker)
                self.assertIn('io.rundesk.video-app-media.contract="1"', docker)
                self.assertNotIn("video_use_adapter.py", docker)
                self.assertNotIn("build-video-artifacts", docker)
                seed = tomllib.loads((destination / "seed/config.toml").read_text())
                mcp = seed["mcp_servers"]["video_app_media"]
                self.assertEqual(mcp["env_vars"], ["VIDEO_APP_MCP_URL", "VIDEO_APP_TOKEN", "VIDEO_APP_PROJECT_ID"])
                self.assertNotIn("env", mcp)
                self.assertEqual(mcp["args"][-1], "serve-mcp")
                if name == "openmontage":
                    self.assertIn(lock["upstream"]["sourceURL"], docker)
                    self.assertIn("ARG RD_GITHUB_ARCHIVE_BASE=https://codeload.github.com", docker)
                    self.assertIn('"${RD_GITHUB_ARCHIVE_BASE}/calesthio/OpenMontage/tar.gz/' + lock["upstream"]["commit"] + '"', docker)
                    self.assertIn(lock["upstream"]["sourceSHA256"] + "  /opt/rundesk-upstream/source.tar.gz", docker)
                    self.assertNotIn("sourceArchive", lock["upstream"])
                    self.assertFalse((destination / "vendor").exists())
                else:
                    self.assertEqual(hashlib.sha256((destination / "vendor/source.tar.gz").read_bytes()).hexdigest(), lock["upstream"]["sourceSHA256"])
                for row in (destination / "SHA256SUMS").read_text().splitlines():
                    digest, filename = row.split("  ", 1)
                    self.assertEqual(hashlib.sha256((destination / filename).read_bytes()).hexdigest(), digest)


if __name__ == "__main__":
    unittest.main(verbosity=2)
