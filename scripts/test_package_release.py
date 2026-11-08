#!/usr/bin/env python3
"""Packaging boundary checks with tiny executable headers, not runtime smoke."""
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("package_release", ROOT / "scripts/package-release.py")
package = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(package)


def executable(path, platform="linux", machine=None):
    if platform == "linux":
        data = bytearray(64)
        data[:7] = b"\x7fELF\x02\x01\x01"
        struct.pack_into("<H", data, 18, machine or 62)
    else:
        data = bytearray(134)
        data[:2] = b"MZ"
        struct.pack_into("<I", data, 60, 128)
        data[128:132] = b"PE\0\0"
        struct.pack_into("<H", data, 132, machine or 0x8664)
    path.write_bytes(data)
    return path


class ReleasePackageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.source = self.base / "checkout"
        for profile in package.PROFILES:
            directory = self.source / "recipes" / profile
            directory.mkdir(parents=True)
            (directory / "Dockerfile").write_text("FROM fixture:1\n")
            (directory / "recipe.json").write_text(json.dumps({"schemaVersion": 1, "profile": {"id": profile}}))
        generic = self.source / "recipes/generic"
        (generic / "seed").mkdir(parents=True)
        (generic / "Dockerfile").write_text("FROM fixture:1\n")
        (generic / "seed/config.toml").write_text('name="fixture"\n')
        config = self.source / "config"
        config.mkdir()
        for name in ("build-sources.json", "public-images.json"):
            (config / name).write_text("{}\n")
        app = self.source / "internal/app"
        app.mkdir(parents=True)
        (app / "api_v1.go").write_text('const Version = "0.65.0"\n')
        (self.source / "LICENSE").write_text("RunDesk license fixture\n")
        (self.source / "THIRD_PARTY_NOTICES.md").write_text("Third-party attribution fixture\n")
        licenses = self.source / "LICENSES"
        licenses.mkdir()
        (licenses / "upstream-MIT.txt").write_text("Upstream license fixture\n")
        sdk = self.source / "third_party/kun-sdk"
        sdk.mkdir(parents=True)
        (sdk / "LICENSE").write_text("Kun SDK license fixture\n")
        self.binary = executable(self.base / "rundesk-input")
        self.output = self.base / "portable folder"

    def stage(self, kun=None):
        return package.stage_release(self.binary, self.output, kun, resource_root=self.source)

    def test_linux_resources_manifest_and_no_source_runtime_dependencies(self):
        generated = self.source / "recipes/news2douyin/__pycache__"
        generated.mkdir()
        (generated / "ignored.pyc").write_bytes(b"generated")
        result = self.stage()
        self.assertEqual((result["platform"], result["arch"]), ("linux", "amd64"))
        self.assertFalse(result["kunIncluded"])
        self.assertFalse((self.output / "PACKAGE-INCOMPLETE.txt").exists())
        self.assertFalse((self.output / "examples").exists())
        self.assertFalse((self.output / "clients").exists())
        self.assertFalse((self.output / "scripts").exists())
        self.assertFalse((self.output / "recipes/news2douyin/__pycache__").exists())
        launcher = (self.output / "start-rundesk.sh").read_text()
        self.assertIn('"$run_dir/data" "$@"', launcher)
        self.assertIn('"$run_dir/rundesk"', launcher)
        self.assertTrue((self.output / "start-rundesk.sh").stat().st_mode & 0o111)
        manifest = json.loads((self.output / "package-manifest.json").read_text())
        self.assertEqual(manifest["sourceVersion"], "0.65.0")
        self.assertNotIn("package-manifest.json", manifest["files"])
        for name in ("LICENSE", "THIRD_PARTY_NOTICES.md", "LICENSES/upstream-MIT.txt", "LICENSES/kun-sdk-LICENSE.txt"):
            self.assertIn(name, manifest["files"])
        self.assertEqual((self.output / "LICENSE").read_bytes(), (self.source / "LICENSE").read_bytes())
        self.assertEqual((self.output / "THIRD_PARTY_NOTICES.md").read_bytes(), (self.source / "THIRD_PARTY_NOTICES.md").read_bytes())
        self.assertEqual((self.output / "LICENSES/kun-sdk-LICENSE.txt").read_bytes(), (self.source / "third_party/kun-sdk/LICENSE").read_bytes())
        for name, meta in manifest["files"].items():
            data = (self.output / name).read_bytes()
            self.assertEqual(len(data), meta["bytes"])
            self.assertEqual(hashlib.sha256(data).hexdigest(), meta["sha256"])
        self.assertEqual((self.source / "recipes/news2douyin/Dockerfile").read_text(), "FROM fixture:1\n")

    def test_existing_and_checkout_destinations_never_overwritten(self):
        self.output.mkdir()
        marker = self.output / "user.txt"
        marker.write_text("keep")
        with self.assertRaises(FileExistsError):
            self.stage()
        self.assertEqual(marker.read_text(), "keep")
        for output in (self.source, self.source / "dist/package"):
            with self.assertRaisesRegex(ValueError, "outside"):
                package.stage_release(self.binary, output, resource_root=self.source)
        self.assertFalse((self.source / "dist").exists())

    def test_missing_or_ambiguous_recipe_fails_before_output_creation(self):
        manifest = self.source / "recipes/news2douyin/recipe.json"
        manifest.unlink()
        with self.assertRaisesRegex(ValueError, "Incomplete"):
            self.stage()
        self.assertFalse(self.output.exists())
        manifest.write_text("{}")
        (manifest.parent / "profile.lock.json").write_text("{}")
        with self.assertRaisesRegex(ValueError, "two editable"):
            self.stage()
        self.assertFalse(self.output.exists())

    def test_linked_resources_are_rejected_including_broken_links(self):
        source = self.source / "recipes/news2douyin/linked"
        try:
            source.symlink_to(self.base / "missing")
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"Symlinks unavailable in this environment: {error}")
        with self.assertRaisesRegex(ValueError, "symlinks"):
            self.stage()
        self.assertFalse(self.output.exists())

    def test_existing_broken_destination_link_is_not_followed(self):
        target = self.base / "missing-target"
        try:
            self.output.symlink_to(target, target_is_directory=True)
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"Symlinks unavailable in this environment: {error}")
        with self.assertRaises(FileExistsError):
            self.stage()
        self.assertTrue(self.output.is_symlink())
        self.assertFalse(target.exists())

    def test_windows_target_and_optional_matching_kun(self):
        executable(self.binary, "windows")
        kun = executable(self.base / "kun-input", "windows")
        result = self.stage(kun)
        self.assertEqual(result["platform"], "windows")
        self.assertTrue(result["kunIncluded"])
        self.assertTrue((self.output / "rundesk.exe").exists())
        self.assertEqual((self.output / "kun.exe").read_bytes(), kun.read_bytes())
        self.assertFalse((self.output / "start-rundesk.sh").exists())
        launcher = (self.output / "start-rundesk.cmd").read_bytes()
        self.assertIn(b'"%~dp0rundesk.exe" --data "%~dp0data" %*\r\n', launcher)
        self.assertIn(b"exit /b %errorlevel%\r\n", launcher)

    def test_mismatched_or_invalid_executables_fail_before_output(self):
        kun = executable(self.base / "kun-input", "windows")
        with self.assertRaisesRegex(ValueError, "same operating system"):
            self.stage(kun)
        executable(kun, "linux", 183)
        with self.assertRaisesRegex(ValueError, "same operating system"):
            self.stage(kun)
        self.binary.write_bytes(b"not a native executable")
        with self.assertRaisesRegex(ValueError, "Only Linux ELF"):
            self.stage()
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
