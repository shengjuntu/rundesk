"""Exercise adapters against a local copy of the exact upstream source.

Usage: python3 test_native_bridge.py /path/to/pinned/OpenMontage
Only native Python APIs and local files run; bridge subprocesses are fixtures.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

SOURCE = Path(sys.argv[1]).resolve()
PROFILE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SOURCE))


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    sys.modules[name] = result
    spec.loader.exec_module(result)
    return result


helper = module("lib.rundesk_video_app", PROFILE / "overlays/rundesk_video_app.py")
tts = module("tools.audio.video_app_tts", PROFILE / "overlays/video_app_tts.py")
image = module("tools.graphics.video_app_image", PROFILE / "overlays/video_app_image.py")
video = module("tools.video.video_app_video", PROFILE / "overlays/video_app_video.py")
launcher = module("om_launch_test", PROFILE / "launch.py")
runtime_patch = module("om_runtime_patch_test", PROFILE / "patch_runtime.py")

from tools.tool_registry import ToolRegistry
from tools.audio.tts_selector import TTSSelector
from tools.graphics.image_selector import ImageSelector
from tools.video.video_selector import VideoSelector
from tools.base_tool import ToolStatus


class NativeBridgeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.registry = ToolRegistry()
        for mod in (tts, image, video):
            self.registry.register_module(mod)
        self.registry._discovered_packages.add("tools")
        self.addCleanup(patch.stopall)
        patch("tools.tool_registry.registry", self.registry).start()
        patch.object(helper, "_bridge_module", return_value=types.SimpleNamespace(resolve_configuration=lambda: {k: "configured" for k in helper.CONFIG_KEYS})).start()

    def inputs(self, kind):
        return {"text" if kind == "tts" else "prompt": "Actual text", "revision": 12, "request_id": "a" * 24, "preferred_tool": "video_app_" + kind, "allowed_providers": ["video-app"], "output_path": str(self.root / (kind + ".bin"))}

    def receipt(self, kind):
        path = self.root / (kind + ".bin")
        path.write_bytes(b"verified local test artifact")
        return {"schemaVersion": 1, "ok": True, "requestId": "a" * 24, "projectId": "fixture-project", "job": {"id": "generation:test", "state": "completed"}, "files": [{"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}]}

    def test_native_registry_discovers_providers(self):
        with tempfile.TemporaryDirectory() as tmp:
            package = Path(tmp) / "native_bridge_providers"
            package.mkdir()
            (package / "__init__.py").write_text("")
            for name in ("tts", "image", "video"):
                shutil.copy2(PROFILE / f"overlays/video_app_{name}.py", package)
            sys.path.insert(0, tmp)
            try:
                registry = ToolRegistry()
                self.assertEqual(sorted(registry.discover("native_bridge_providers")), ["video_app_image", "video_app_tts", "video_app_video"])
            finally:
                sys.path.remove(tmp)

    def test_all_three_real_selectors_return_verified_files(self):
        for kind, cls in (("tts", TTSSelector), ("image", ImageSelector), ("video", VideoSelector)):
            with self.subTest(kind=kind), patch.object(helper.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(self.receipt(kind)), "")) as run:
                result = cls().execute(self.inputs(kind))
                self.assertTrue(result.success, result.error)
                self.assertEqual(result.data["selected_provider"], "video-app")
                self.assertEqual(result.artifacts, [str(self.root / (kind + ".bin"))])
                self.assertEqual(run.call_args.args[0][2], kind)

    def test_native_image_selector_preserves_rejected_controls(self):
        for control, value in (("seed", 1), ("negative_prompt", "none"), ("width", 1024), ("height", 768), ("n", 2)):
            with self.subTest(control=control), patch.object(helper.subprocess, "run") as run:
                result = ImageSelector().execute(dict(self.inputs("image"), **{control: value}))
                self.assertFalse(result.success)
                run.assert_not_called()

    def test_native_tts_controls_are_rejected_without_dispatch(self):
        for key, val in (("speaking_rate", 1.1), ("voice_speed", 1.1), ("voice_language", "en"), ("apply_text_normalization", "auto")):
            with self.subTest(key=key), patch.object(helper.subprocess, "run") as run:
                self.assertFalse(TTSSelector().execute(dict(self.inputs("tts"), **{key: val})).success)
                run.assert_not_called()

    def test_failure_retains_request_and_does_not_fallback(self):
        payload = {"ok": False, "jobId": "generation:accepted", "requestId": "a" * 24, "submissionUncertain": True}
        with patch.object(helper.subprocess, "run", return_value=subprocess.CompletedProcess([], 2, "", json.dumps(payload))) as run:
            result = VideoSelector().execute(self.inputs("video"))
            self.assertFalse(result.success)
            self.assertEqual(result.data["video_app_error"], payload)
            self.assertEqual(run.call_count, 1)

    def test_bad_receipt_and_missing_id_do_not_start_replacement(self):
        with patch.object(helper.subprocess, "run") as run:
            args = self.inputs("image")
            del args["request_id"]
            self.assertFalse(image.VideoAppImage().execute(args).success)
            run.assert_not_called()
        bad = self.receipt("image")
        bad["files"][0]["sha256"] = "0" * 64
        with patch.object(helper.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(bad), "")):
            result = image.VideoAppImage().execute(self.inputs("image"))
            self.assertFalse(result.success)
            self.assertTrue(result.data["submissionUncertain"])

    def test_writable_native_copy_is_distinct_and_reused(self):
        source = self.root / "installed"
        composer = source / "remotion-composer"
        (composer / "public").mkdir(parents=True)
        (composer / "node_modules/.cache").mkdir(parents=True)
        (source / "LICENSE").write_text("AGPL fixture")
        workspace = self.root / "workspace"
        workspace.mkdir()
        target = launcher.prepare(source, workspace, identity="1" * 64)
        (target / "remotion-composer/public/new.bin").write_bytes(b"production")
        (target / "remotion-composer/node_modules/.cache/new.bin").write_bytes(b"cache")
        self.assertFalse((composer / "public/new.bin").exists())
        self.assertEqual(launcher.prepare(source, workspace, identity="1" * 64), target)
        self.assertFalse(target.is_symlink())

    def test_overlay_lock_and_profile_updates_select_distinct_preserved_caches(self):
        installed = self.root / "installed-build"
        (installed / "npm").mkdir(parents=True)
        provider = installed / "provider.py"
        provider.write_text("old-provider")
        dependency_lock = installed / "npm/package-lock.json"
        dependency_lock.write_text('{"version":1}')
        profile_lock = self.root / "profile.lock.json"
        recipe = {"profile": {"id": "openmontage", "version": "1.0.0"}, "upstream": {"commit": launcher.COMMIT, "buildFiles": ["provider.py", "npm/package-lock.json"]}}
        profile_lock.write_text(json.dumps(recipe))
        source = self.root / "native-source"
        source.mkdir()
        (source / "provider.py").write_text("old-provider")
        workspace = self.root / "workspace"
        workspace.mkdir()
        identity1 = launcher.installed_identity(profile_lock, installed)
        target1 = launcher.prepare(source, workspace, identity=identity1)
        provider.write_text("new-provider")
        (source / "provider.py").write_text("new-provider")
        identity2 = launcher.installed_identity(profile_lock, installed)
        target2 = launcher.prepare(source, workspace, identity=identity2)
        self.assertNotEqual(target1, target2)
        self.assertEqual((target1 / "provider.py").read_text(), "old-provider")
        self.assertEqual((target2 / "provider.py").read_text(), "new-provider")
        dependency_lock.write_text('{"version":2}')
        identity3 = launcher.installed_identity(profile_lock, installed)
        self.assertNotEqual(identity2, identity3)
        target3 = launcher.prepare(source, workspace, identity=identity3)
        recipe["profile"]["version"] = "1.0.1"
        profile_lock.write_text(json.dumps(recipe))
        identity4 = launcher.installed_identity(profile_lock, installed)
        self.assertNotEqual(identity3, identity4)
        target4 = launcher.prepare(source, workspace, identity=identity4)
        self.assertEqual(len({target1, target2, target3, target4}), 4)
        self.assertTrue(all(p.exists() for p in (target1, target2, target3, target4)))
        (installed / "node_modules").mkdir()
        (installed / "node_modules/unlisted").write_text("not part of the manifest")
        self.assertEqual(launcher.installed_identity(profile_lock, installed), identity4)
        marker = json.loads((target4 / "RUNDESK_WORKSPACE_PIN.json").read_text())
        self.assertEqual(marker["installationSHA256"], identity4)

    def test_reviewed_runtime_patch_is_local_and_compiles(self):
        source = self.root / "patched"
        for rel in ("tools/video/hyperframes_compose.py", "tools/video/video_compose.py", "tools/video/remotion_caption_burn.py"):
            target = source / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(SOURCE / rel, target)
        changed = runtime_patch.patch(source)
        self.assertEqual(len(changed), 3)
        for rel in changed:
            compile((source / rel).read_text(), str(source / rel), "exec")
            self.assertEqual((source / "RUNDESK-ORIGINALS" / rel).read_bytes(), (SOURCE / rel).read_bytes())
        text = (source / "tools/video/hyperframes_compose.py").read_text()
        self.assertNotIn('[npm, "view", cls._NPM_PACKAGE, "version"]', text)
        self.assertNotIn('["npx", "--yes", "hyperframes", *args]', text)
        self.assertIn('["/usr/local/bin/hyperframes", *args]', text)
        self.assertEqual((source / "tools/video/video_compose.py").read_text().count('"--browser-executable=/usr/bin/chromium"'), 2)


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]], verbosity=2)
