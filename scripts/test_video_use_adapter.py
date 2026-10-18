#!/usr/bin/env python3
"""Real FFmpeg integration and production-run boundary tests, offline."""
import array
import hashlib
import importlib.util
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[1]
ADAPTER = ROOT / "examples/_shared/video_use_adapter.py"
SMOKE = ADAPTER.with_name("video_use_smoke.py")


@unittest.skipUnless(shutil.which("ffmpeg") and shutil.which("ffprobe"), "FFmpeg required")
class VideoUseAdapterTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="rundesk-adapter-test-")
        cls.base = Path(cls.temp.name)
        cls.vendor = cls.base / "vendor"
        source = next((ROOT / "examples/video-build/vendor").glob("video-use-*.tar.gz"))
        with tarfile.open(source, "r:gz") as archive:
            for member in archive.getmembers():
                if not member.isfile():
                    continue
                relative = Path(member.name).relative_to("video-use")
                if ".." in relative.parts or relative.is_absolute():
                    raise AssertionError("unsafe vendor archive")
                target = cls.vendor / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.extractfile(member).read())
        (cls.vendor / "RUNDESK_PIN.json").write_text(json.dumps({"commit": "43cfc566833548093455c9d4a46bd69913b7e552"}))
        cls.runroot = cls.base / "production"
        p = subprocess.run([sys.executable, str(SMOKE), "--out", str(cls.runroot), "--video-use-root", str(cls.vendor)], capture_output=True, text=True, timeout=120)
        if p.returncode:
            raise AssertionError(p.stdout + p.stderr)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def command(self, root, **changes):
        command = [sys.executable, str(ADAPTER), "render", "--manifest", str(root / "input/manifest.json"), "--edl", str(root / "work/edl.json"), "--out", str(root / "artifacts/render-next"), "--video-use-root", str(self.vendor)]
        return subprocess.run(command, capture_output=True, text=True, timeout=30)

    def copy_inputs(self, name):
        root = self.base / name
        shutil.copytree(self.runroot / "input", root / "input")
        shutil.copytree(self.runroot / "work", root / "work")
        return root

    def test_real_render_completion_and_continuous_audio(self):
        completion = json.loads((self.runroot / "completion.json").read_text())
        self.assertEqual(completion["engine"], "video_use")
        self.assertEqual(completion["video"]["width"], 180)
        self.assertEqual(completion["video"]["height"], 320)
        self.assertEqual(completion["video"]["fps"], 12)
        artifacts = {row["id"]: row for row in completion["artifacts"]}
        self.assertTrue({"video", "edit", "validation", "probe"} <= artifacts.keys())
        for row in artifacts.values():
            path = self.runroot / row["path"]
            self.assertEqual(path.stat().st_size, row["size"])
            self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), row["sha256"])
        validation = json.loads((self.runroot / artifacts["validation"]["path"]).read_text())
        self.assertTrue(validation["passed"])
        for key in ("projectId", "productionRunId", "sourceHash", "inputHash"):
            self.assertEqual(validation[key], completion[key])
        self.assertEqual(validation["schemaVersion"], 1)
        self.assertEqual(validation["engine"], "video_use")
        self.assertEqual(validation["upstreamCommit"], completion["upstreamCommit"])
        self.assertEqual(validation["edlHash"], artifacts["edit"]["sha256"])
        manifest = json.loads((self.runroot / "input/manifest.json").read_text())
        self.assertEqual(validation["narrationSHA256"], manifest["narration"]["sha256"])
        self.assertTrue(validation["checks"])
        self.assertTrue(all(row["ok"] for row in validation["checks"]))
        self.assertFalse(validation["visualInspected"])
        self.assertFalse(validation["audioListened"])
        self.assertTrue(any("-af" in cmd and any("afade=" in str(a) for a in cmd) for cmd in validation["commands"]))
        self.assertTrue(any("concat" in cmd for cmd in validation["commands"]))
        # Neither the image boundary nor silent generated clip may mute/cut the
        # external narration. Decode it and check energy around the 1s cut.
        video = self.runroot / artifacts["video"]["path"]
        decoded = subprocess.check_output(["ffmpeg", "-v", "error", "-i", str(video), "-vn", "-ac", "1", "-ar", "24000", "-f", "f32le", "-"])
        samples = array.array("f"); samples.frombytes(decoded)
        if sys.byteorder != "little": samples.byteswap()
        self.assertGreater(len(samples), 47000)
        window = samples[23520:24480]
        rms = math.sqrt(sum(v*v for v in window)/len(window))
        self.assertGreater(rms, .02)
        self.assertEqual(len(validation["sampleFrames"]), 3)

    def test_edit_changed_during_render_does_not_change_snapshot(self):
        root = self.copy_inputs("edit-mutated")
        path = root / "work/edl.json"
        original = json.loads(path.read_text())
        spec = importlib.util.spec_from_file_location("adapter_edit_snapshot_test", ADAPTER)
        adapter = importlib.util.module_from_spec(spec); spec.loader.exec_module(adapter)
        actual_load = adapter.upstream_module
        def load_then_edit(vendor):
            module = actual_load(vendor)
            changed = json.loads(path.read_text())
            changed["canvas"]["width"] = 720
            changed["clips"][0]["duration"] = 999
            path.write_text(json.dumps(changed))
            return module
        args = SimpleNamespace(manifest=str(root / "input/manifest.json"), edl=str(path),
                               out=str(root / "artifacts/render-snapshot"), run_root=None, video_use_root=str(self.vendor))
        with patch.object(adapter, "upstream_module", side_effect=load_then_edit):
            adapter.render(args)
        completion = json.loads((root / "completion.json").read_text())
        edit = next(row for row in completion["artifacts"] if row["kind"] == "edit")
        validation = next(row for row in completion["artifacts"] if row["kind"] == "validation")
        frozen = root / edit["path"]
        self.assertEqual(json.loads(frozen.read_text()), original)
        self.assertNotEqual(json.loads(path.read_text()), original)
        report = json.loads((root / validation["path"]).read_text())
        self.assertEqual(report["edlHash"], hashlib.sha256(frozen.read_bytes()).hexdigest())
        self.assertEqual(report["edlHash"], edit["sha256"])
        self.assertEqual(completion["video"]["width"], original["canvas"]["width"])

    def test_completion_is_not_overwritten(self):
        original = (self.runroot / "completion.json").read_bytes()
        p = self.command(self.runroot)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn("already has completion", p.stderr)
        self.assertEqual((self.runroot / "completion.json").read_bytes(), original)

    def test_changed_input_and_symlink_rejected(self):
        for mode in ("changed", "symlink"):
            root = self.copy_inputs(mode)
            image = root / "input/image.ppm"
            if mode == "changed":
                image.write_bytes(b"changed")
            else:
                image.unlink(); image.symlink_to(self.runroot / "input/image.ppm")
            p = self.command(root)
            self.assertNotEqual(p.returncode, 0)
            self.assertFalse((root / "completion.json").exists())
            self.assertTrue("Input size changed" in p.stderr or "Symlinks" in p.stderr)

    def test_invalid_edl_fails_without_completion(self):
        for mode in ("gap", "escape", "source-range", "unknown"):
            root = self.copy_inputs(mode)
            path = root / "work/edl.json"; edl = json.loads(path.read_text())
            if mode == "gap": edl["clips"][1]["start"] = 1.1
            elif mode == "escape": edl["clips"][0]["path"] = "../outside.png"
            elif mode == "source-range": edl["clips"][1]["sourceIn"] = 10
            else: edl["clips"][0]["magicTransition"] = "ignored?"
            path.write_text(json.dumps(edl))
            p = self.command(root)
            self.assertNotEqual(p.returncode, 0)
            self.assertFalse((root / "completion.json").exists())

    def test_playlist_and_long_audio_do_not_bypass_visual_bounds(self):
        for mode in ("playlist", "long-audio"):
            root = self.copy_inputs(mode)
            video = root / "input/generated-silent.mp4"
            if mode == "playlist":
                (root / "input/inside.mp4").symlink_to(self.runroot / "input/generated-silent.mp4")
                video.write_text("ffconcat version 1.0\nfile 'inside.mp4'\nduration 1.5\n")
            else:
                video.unlink()
                subprocess.run(["ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "testsrc2=size=180x320:rate=12:duration=0.5", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "3", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", str(video)], check=True, timeout=30)
            path = root / "input/manifest.json"
            manifest = json.loads(path.read_text())
            manifest["media"][1].update(sha256=hashlib.sha256(video.read_bytes()).hexdigest(), size=video.stat().st_size)
            path.write_text(json.dumps(manifest))
            p = self.command(root)
            self.assertNotEqual(p.returncode, 0, p.stdout + p.stderr)
            self.assertFalse((root / "completion.json").exists())
            if mode == "long-audio": self.assertIn("exceeds video duration", p.stderr)

    def test_aac_padding_does_not_accumulate_across_one_frame_cuts(self):
        work = self.base / "one-frame-cuts"; work.mkdir()
        normalized = work / "normalized.mp4"
        subprocess.run(["ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "color=size=180x320:rate=60", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", str(1/60), "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", str(normalized)], check=True, timeout=30)
        spec = importlib.util.spec_from_file_location("adapter_cut_grid_test", ADAPTER)
        adapter = importlib.util.module_from_spec(spec); spec.loader.exec_module(adapter)
        upstream = adapter.upstream_module(self.vendor)
        visual = adapter.extract_visual_segment(upstream, normalized, 1/60, work, 0, 180, 320, 60)
        concat = work / "concat.mp4"
        upstream.concat_segments([visual] * 300, concat, work)
        info = adapter.probe(concat)
        self.assertFalse(any(s.get("codec_type") == "audio" for s in info["streams"]))
        self.assertEqual(int(info["streams"][0]["nb_frames"]), 300)
        self.assertAlmostEqual(adapter.visual_duration(info), 5, delta=.001)
        self.assertAlmostEqual(adapter.duration(info), 5, delta=.001)


if __name__ == "__main__":
    unittest.main()
