#!/usr/bin/env python3
"""Exercise build-time reuse/download decisions with process fixtures, no network."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SHARED = ROOT / "examples/_shared"


class BuildHelpersTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.log = self.root / "commands"
        self.env = dict(os.environ, PATH=str(self.bin) + ":" + os.environ["PATH"],
                        FIXTURE_LOG=str(self.log), CODEX_VERSION="0.157.1")

    def executable(self, name, body):
        target = self.bin / name
        target.write_text("#!/bin/sh\nset -eu\n" + body)
        target.chmod(0o755)
        return target

    def call(self, helper, *args, **overrides):
        return subprocess.run(["sh", str(SHARED / helper), *map(str, args)],
                              env=dict(self.env, **overrides), capture_output=True, text=True)

    def commands(self):
        return self.log.read_text() if self.log.exists() else ""

    def codex(self):
        return self.executable("codex", '''printf 'codex %s\n' "$*" >> "$FIXTURE_LOG"
case "$1" in
 --version) printf 'codex-cli %s\n' "${FIXTURE_VERSION:-0.157.1}" ;;
 app-server) exit "${FIXTURE_APP_SERVER_EXIT:-0}" ;;
 *) exit 1 ;;
esac
''')

    def test_existing_exact_runtime_avoids_npm(self):
        self.codex()
        self.executable("npm", 'echo npm >> "$FIXTURE_LOG"; exit 99\n')
        for mode in ("base", "auto"):
            result = self.call("install-codex.sh", RD_CODEX_MODE=mode)
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("npm", self.commands())
        self.assertIn("codex app-server --help", self.commands())

    def test_base_mismatch_or_missing_app_server_never_downloads(self):
        self.codex()
        self.executable("npm", 'echo npm >> "$FIXTURE_LOG"; exit 99\n')
        bad_version = self.call("install-codex.sh", RD_CODEX_MODE="base", FIXTURE_VERSION="0.1.0")
        self.assertNotEqual(bad_version.returncode, 0)
        self.assertIn("version mismatch", bad_version.stderr)
        bad_server = self.call("install-codex.sh", RD_CODEX_MODE="base", FIXTURE_APP_SERVER_EXIT="8")
        self.assertNotEqual(bad_server.returncode, 0)
        self.assertNotIn("npm", self.commands())

    def test_auto_install_keeps_exact_version_and_selected_registry(self):
        self.codex()
        self.executable("npm", '''printf 'npm %s registry=%s\n' "$*" "${npm_config_registry:-}" >> "$FIXTURE_LOG"
''')
        result = self.call("install-codex.sh", RD_CODEX_MODE="auto", FIXTURE_VERSION="0.1.0",
                           RD_NPM_REGISTRY="https://registry.example.test")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("npm install --global @openai/codex@0.157.1 registry=https://registry.example.test", self.commands())

    def test_native_payload_is_verified_before_symlink_and_preserves_companions(self):
        payload = self.root / "runtime/native/codex"
        payload.mkdir(parents=True)
        native = payload / "codex"
        native.write_bytes(self.codex().read_bytes())
        native.chmod(0o755)
        companion = payload.parent / "path/rg"
        companion.parent.mkdir()
        companion.write_text("companion unchanged")
        self.executable("ln", 'printf "link %s\\n" "$*" >> "$FIXTURE_LOG"\n')
        result = self.call("install-codex.sh", RD_CODEX_MODE="native", RD_CODEX_RUNTIME_DIR=str(payload.parent.parent))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("link -sf " + str(native) + " /usr/local/bin/codex", self.commands())
        self.assertLess(self.commands().index("app-server"), self.commands().index("link"))
        self.assertEqual(companion.read_text(), "companion unchanged")
        self.log.unlink()
        result = self.call("install-codex.sh", RD_CODEX_MODE="native", RD_CODEX_RUNTIME_DIR=str(payload.parent.parent), FIXTURE_VERSION="0.1.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("link", self.commands())

    def archive_fixture(self):
        content = self.root / "download-bytes"
        content.write_bytes(b"reviewed archive fixture\n")
        self.env["FIXTURE_ARCHIVE"] = str(content)
        self.executable("curl", '''printf 'curl %s\n' "$*" >> "$FIXTURE_LOG"
destination=
while [ "$#" -gt 0 ]; do
 if [ "$1" = --output ]; then destination=$2; shift; fi
 shift
done
cp "$FIXTURE_ARCHIVE" "$destination"
exit "${FIXTURE_CURL_EXIT:-0}"
''')
        return content, hashlib.sha256(content.read_bytes()).hexdigest(), self.root / "vendor/source.tar.gz"

    def test_archive_full_url_precedence_and_verified_reuse(self):
        _, digest, dest = self.archive_fixture()
        args = (digest, dest, "https://codeload.github.com/owner/repo/tar.gz/pin", "/owner/repo/tar.gz/pin")
        result = self.call("download-archive.sh", *args, RD_GITHUB_ARCHIVE_URL="https://proxy.example.test/https://github.com/owner/repo/archive/pin.tar.gz", RD_GITHUB_ARCHIVE_BASE="https://unused.example.test")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("https://proxy.example.test/https://github.com/owner/repo/archive/pin.tar.gz", self.commands())
        self.assertNotIn("unused.example.test", self.commands())
        self.log.unlink()
        result = self.call("download-archive.sh", *args)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.commands(), "")

    def test_archive_corruption_and_failed_download_do_not_commit(self):
        _, digest, dest = self.archive_fixture()
        args = (digest, dest, "https://official.example.test/archive", "/owner/repo/tar.gz/pin")
        result = self.call("download-archive.sh", *args, FIXTURE_CURL_EXIT="28")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(dest.exists())
        self.assertEqual(list(dest.parent.glob("*.part.*")), [])
        result = self.call("download-archive.sh", "0" * 64, *args[1:])
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(dest.exists())
        self.log.unlink()
        dest.write_bytes(b"corrupt cache")
        result = self.call("download-archive.sh", *args)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.commands(), "")
        self.assertEqual(dest.read_bytes(), b"corrupt cache")


if __name__ == "__main__":
    unittest.main()
