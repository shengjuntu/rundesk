#!/usr/bin/env python3
"""Exercise actual shared-base orchestration with a recording Docker fixture."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import shutil
import shlex
import sys
import tempfile
from types import SimpleNamespace
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import recipes
import base_images


class DockerFixture:
    def __init__(self):
        self.root_id = "sha256:" + "1" * 64
        self.images = {"node:22-bookworm-slim": self.root_id, self.root_id: self.root_id}
        self.labels = {}
        self.calls = []
        self.fail_build = False
        self.build_generation = 0

    def __call__(self, argv, timeout, capture):
        self.calls.append(argv)
        args = argv[1:]
        if args[:2] == ["image", "inspect"]:
            iid = self.images.get(args[-1])
            return SimpleNamespace(returncode=0 if iid else 1, stdout=(iid + "|linux|amd64|" + self.labels.get(iid, "") + "\n") if iid else "Error: No such image")
        if args[0] == "build":
            if self.fail_build:
                return SimpleNamespace(returncode=1, stdout="fixture build failure")
            key = args[args.index("--label") + 1].split("=", 1)[1]
            iid = "sha256:" + hashlib.sha256((key + str(self.build_generation)).encode()).hexdigest()
            self.images[iid] = iid
            self.labels[iid] = key
            Path(args[args.index("--iidfile") + 1]).write_text(iid)
            isolated = Path(args[-1])
            # A stage never receives application source, secrets, or Skills.
            actual = {p.relative_to(isolated).as_posix() for p in isolated.rglob("*") if p.is_file()}
            assert actual <= {"bases/install.sh", "bases/configure-apt.sh", "bases/system.Dockerfile", "bases/media.Dockerfile", "bases/browser.Dockerfile"}
            assert len(actual) == 3
            return SimpleNamespace(returncode=0, stdout="")
        raise AssertionError(argv)

    def builds(self):
        return [a for a in self.calls if a[1] == "build"]


class BaseImagesTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.cache = self.root / "cache"
        self.docker = DockerFixture()

    def context(self, name, codex="0.157.1", suffix=""):
        path = self.root / (name + suffix); path.mkdir()
        profile = recipes.load_profile(name)
        recipes.render(profile, recipes.resolve(profile), codex, "node:22-bookworm-slim", path)
        return path

    def prepare(self, context):
        with contextlib.redirect_stderr(io.StringIO()):
            return base_images.prepare(context, ["docker-fixture"], 30, self.cache, self.docker)

    def test_browser_chain_and_cross_profile_reuse_ignore_codex_and_npm(self):
        first = self.context("video-build")
        snapshots, args = self.prepare(first)
        self.assertEqual([s["stage"] for s in snapshots], ["system", "media", "browser"])
        self.assertEqual(args, ["--build-arg", "RD_BASE_IMAGE=" + snapshots[-1]["imageId"], "--build-arg", "RD_TOOL_BASE_READY=1"])
        self.assertEqual(len(self.docker.builds()), 3)
        second = self.context("hyperframes", "0.158.0")
        file = second / "Dockerfile"
        file.write_text(file.read_text().replace("https://registry.npmjs.org", "https://npm.example.test"))
        again, _ = self.prepare(second)
        self.assertEqual(again, snapshots)
        self.assertEqual(len(self.docker.builds()), 3)
        self.assertEqual(json.loads((second / "build-bases.lock.json").read_text())["stages"], snapshots)

    def test_system_and_media_profiles_only_prepare_their_ancestors(self):
        system, _ = self.prepare(self.context("people-search"))
        media, _ = self.prepare(self.context("news2douyin"))
        self.assertEqual([s["stage"] for s in system], ["system"])
        self.assertEqual([s["stage"] for s in media], ["system", "media"])
        self.assertEqual(system[0], media[0])
        self.assertEqual(len(self.docker.builds()), 2)

    def test_apt_source_change_gets_distinct_bases(self):
        context = self.context("video-build")
        old, _ = self.prepare(context)
        for file in (context / "bases").glob("*.Dockerfile"):
            file.write_text(file.read_text().replace("http://deb.debian.org/debian", "http://apt.example.test/debian"))
        changed, _ = self.prepare(context)
        self.assertEqual(len(self.docker.builds()), 6)
        self.assertTrue(all(a["imageId"] != b["imageId"] for a, b in zip(old, changed)))

    def test_new_cli_invocation_rebuilds_pruned_cache_from_its_pinned_parent(self):
        context = self.context("video-build")
        snapshots, _ = self.prepare(context)
        del self.docker.images[snapshots[-1]["imageId"]]
        self.docker.build_generation = 1
        rebuilt, args = self.prepare(context)
        self.assertEqual(rebuilt[:-1], snapshots[:-1])
        self.assertNotEqual(rebuilt[-1]["imageId"], snapshots[-1]["imageId"])
        self.assertEqual(rebuilt[-1]["parentImageId"], snapshots[-1]["parentImageId"])
        self.assertIn("RD_BASE_IMAGE=" + rebuilt[-1]["imageId"], args)
        self.assertEqual(len(self.docker.builds()), 4)

    def test_cache_label_tampering_and_daemon_errors_fail_closed(self):
        context = self.context("video-build")
        snapshots, _ = self.prepare(context)
        self.docker.labels[snapshots[0]["imageId"]] = "wrong-key"
        with self.assertRaisesRegex(base_images.BaseImageError, "label changed"):
            self.prepare(context)
        self.assertEqual(len(self.docker.builds()), 3)
        def unavailable(argv, timeout, capture):
            self.assertEqual(argv[1:3], ["image", "inspect"])
            return SimpleNamespace(returncode=1, stdout="Cannot connect to the Docker daemon")
        with self.assertRaisesRegex(base_images.BaseImageError, "Cannot connect"):
            base_images.prepare(context, ["docker-fixture"], 30, self.cache, unavailable)

    def test_failed_base_prevents_leaf_inheritance_and_lock(self):
        context = self.context("video-build")
        self.docker.fail_build = True
        with self.assertRaisesRegex(base_images.BaseImageError, "command failed"):
            self.prepare(context)
        self.assertFalse((context / "build-bases.lock.json").exists())
        self.assertFalse(list(self.cache.glob("*.json")))

    def test_unsafe_or_cyclic_plan_rejected_before_docker(self):
        context = self.context("video-build")
        file = context / "bases/plan.json"
        value = json.loads(file.read_text())
        value["stages"][0]["parent"] = "browser"
        file.write_text(json.dumps(value))
        with self.assertRaisesRegex(base_images.BaseImageError, "parent|cyclic"):
            self.prepare(context)
        self.assertEqual(self.docker.calls, [])
        value["stages"][0]["parent"] = ""
        value["stages"][0]["inputs"] = ["../outside"]
        file.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, "relative"):
            self.prepare(context)
        self.assertEqual(self.docker.calls, [])

    def test_comment_only_guard_and_extra_from_cannot_claim_base_contract(self):
        context = self.context("video-build")
        file = context / "Dockerfile"
        original = file.read_text()
        file.write_text("\n".join(line for line in original.splitlines() if not line.startswith("RUN if [ ")) + "\n# ${RD_TOOL_BASE_READY}\n")
        with self.assertRaisesRegex(base_images.BaseImageError, "contract"):
            self.prepare(context)
        file.write_text(original + "FROM node:22-bookworm-slim\n")
        with self.assertRaisesRegex(base_images.BaseImageError, "contract"):
            self.prepare(context)
        self.assertEqual(self.docker.calls, [])
        file.write_text(original.replace("FROM ${RD_BASE_IMAGE}", "  from ${RD_BASE_IMAGE} as runtime ").replace("${RD_TOOL_BASE_READY}", "${RD_TOOL_BASE_READY:-0}"))
        self.assertEqual(base_images.plan(context)[0]["target"], "browser")

    def test_exported_copy_inputs_are_visible_through_each_dockerignore(self):
        # These recipes deliberately use a closed allowlist. Exercise its actual
        # supported grammar, including directory traversal, for every COPY input.
        # Reject new pattern syntax instead of silently pretending to implement
        # Docker's complete ignore language in a test helper.
        def visible(filename, patterns):
            for target in [*Path(filename).parents][:-1] + [Path(filename)]:
                relative = target.as_posix()
                included = True
                for pattern in patterns:
                    negated = pattern.startswith("!")
                    value = pattern[1:] if negated else pattern
                    value = value.rstrip("/")
                    self.assertTrue(value == "**" or ("*" not in value.rstrip("/**") and "?" not in value and "[" not in value), value)
                    matches = value == "**" or relative == value
                    if value.endswith("/**"):
                        matches = relative == value[:-3] or relative.startswith(value[:-3] + "/")
                    if matches:
                        included = negated
                if not included:
                    return False
            return True

        for name in recipes.PROFILE_IDS:
            profile = recipes.load_profile(name)
            selections = [recipes.resolve(profile)]
            if "agent-reach" in profile["optionalComponents"]:
                selections.append(recipes.resolve(profile, agent_reach=False))
            if "video-use" in profile["optionalComponents"]:
                selections.append(recipes.resolve(profile, video_use=True))
            for index, selection in enumerate(selections):
                with self.subTest(profile=name, selection=selection):
                    directory = self.root / (name + "-ignore-" + str(index)); directory.mkdir()
                    recipes.render(profile, selection, "0.157.1", "node:22-bookworm-slim", directory)
                    patterns = [line.strip() for line in (directory / ".dockerignore").read_text().splitlines() if line.strip() and not line.lstrip().startswith("#")]
                    legacy_patterns = [pattern for pattern in patterns if not pattern.startswith("!bases/")]
                    self.assertFalse(visible("bases/install.sh", legacy_patterns))
                    checked = set()
                    for line in (directory / "Dockerfile").read_text().splitlines():
                        if not line.startswith("COPY "):
                            continue
                        fields = shlex.split(line)
                        self.assertEqual(len(fields), 3, line)
                        source = directory / fields[1].rstrip("/")
                        self.assertTrue(source.exists(), line)
                        files = [source] if source.is_file() else list(source.rglob("*"))
                        for file in files:
                            if not file.is_file():
                                continue
                            relative = file.relative_to(directory).as_posix()
                            checked.add(relative)
                            self.assertTrue(visible(relative, patterns), f"{name}: COPY input excluded by .dockerignore: {relative}")
                    self.assertTrue({"bases/install.sh", "bases/configure-apt.sh"} <= checked)

    def test_base_input_edit_changes_key_and_standalone_variants_are_complete(self):
        context = self.context("video-build")
        old, _ = self.prepare(context)
        file = context / "bases/system.Dockerfile"
        file.write_text(file.read_text() + "# operator change\n")
        new, _ = self.prepare(context)
        self.assertNotEqual(old[0]["key"], new[0]["key"])
        for name in recipes.PROFILE_IDS:
            profile = recipes.load_profile(name)
            selections = [recipes.resolve(profile)]
            if "agent-reach" in profile["optionalComponents"]:
                selections.append(recipes.resolve(profile, agent_reach=False))
            if "video-use" in profile["optionalComponents"]:
                selections.append(recipes.resolve(profile, video_use=True))
            for index, selection in enumerate(selections):
                directory = self.root / (name + "-variant-" + str(index)); directory.mkdir()
                recipes.render(profile, selection, "0.157.1", "node:22-bookworm-slim", directory)
                _, chain = base_images.plan(directory)
                leaf = (directory / "Dockerfile").read_text()
                self.assertIn("FROM ${RD_BASE_IMAGE}\nUSER root", leaf)
                self.assertIn("USER node\n", leaf)
                self.assertIn('if [ "${RD_TOOL_BASE_READY}" != "1" ]', leaf)
                self.assertIn("install.sh all-" + chain[-1]["id"], leaf)
                self.assertNotIn("RUN python3 -m venv", leaf)
                self.assertTrue((directory / "bases/configure-apt.sh").is_file())


if __name__ == "__main__":
    unittest.main()
