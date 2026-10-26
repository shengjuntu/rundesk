#!/usr/bin/env python3
"""Exercise provider admission at the installed HyperFrames CLI boundary."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent


class ProviderBoundaryTests(unittest.TestCase):
    def test_generation_requires_video_app(self):
        for kind in ("voice", "image", "video", "bgm", "sfx"):
            for option in (["--type", kind], ["--type=" + kind]):
                with self.subTest(kind=kind, option=option):
                    result = subprocess.run([sys.executable, str(ROOT / "generation_guard.py"), "media-use", "resolve", *option, "--intent", "an example"], text=True, capture_output=True)
                    self.assertEqual(result.returncode, 2)
                    self.assertEqual(json.loads(result.stderr)["code"], "video_app_generation_required")

    def test_existing_media_and_native_editing_are_not_blocked(self):
        calls = [
            ["media-use", "resolve", "--type", "voice", "--from", "./narration with spaces.wav"],
            ["media-use", "resolve", "--type=image", "--from=./existing.png"],
            ["media-use", "resolve", "--type", "video", "--reuse", "sha-prefix"],
            ["media-use", "resolve", "--type", "image", "--candidates"],
            ["media-use", "resolve", "--type", "video", "--dry-run"],
            ["media-use", "resolve", "--doctor"],
            ["media-use", "resolve", "--help"],
            ["render", "./native project", "--output", "./final.mp4"],
            ["check", "./native project"],
        ]
        for args in calls:
            with self.subTest(args=args):
                result = subprocess.run([sys.executable, str(ROOT / "generation_guard.py"), *args], text=True, capture_output=True)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_wrapper_denial_precedes_native_provider_and_preserves_arguments(self):
        with tempfile.TemporaryDirectory(prefix="hf-provider-boundary-") as directory:
            base = Path(directory)
            native = base / "native.mjs"
            native.write_text('console.log(JSON.stringify({nativeEntered:true,args:process.argv.slice(2)}));')
            wrapper = base / "hyperframes"
            # Only deployed absolute locations change; execute the shipped shell
            # wrapper and guard. The stand-in records whether native code ran.
            source = (ROOT / "hyperframes").read_text()
            source = source.replace("/opt/rundesk-upstream/generation_guard.py", str(ROOT / "generation_guard.py"))
            source = source.replace("/opt/hyperframes/node_modules/hyperframes/bin/hyperframes.mjs", str(native))
            wrapper.write_text(source)
            wrapper.chmod(0o755)
            result = subprocess.run([str(wrapper), "media-use", "resolve", "--type", "image", "--intent", "some image"], text=True, capture_output=True)
            self.assertEqual(result.returncode, 2)
            self.assertEqual(result.stdout, "")
            self.assertEqual(json.loads(result.stderr)["code"], "video_app_generation_required")
            args = ["media-use", "resolve", "--type", "image", "--from", "./our image.png"]
            result = subprocess.run([str(wrapper), *args], text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout), {"nativeEntered": True, "args": args})


if __name__ == "__main__":
    unittest.main()
