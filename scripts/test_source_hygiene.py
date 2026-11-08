#!/usr/bin/env python3
"""Source release boundaries with real file trees and checksums."""
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("source_hygiene", ROOT / "scripts/check-source.py")
hygiene = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(hygiene)


class SourceHygieneTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.write("internal/app/api_v1.go", 'package app\nconst Version = "0.66.0"\n')
        self.write("go.mod", "module fixture\n")
        self.write("docs/validation/README.md", "Validation provenance\n")
        self.write("docs/validation/current/release-066.json", json.dumps({"version": "0.66.0", "status": "passed"}))
        self.write("recipes/news/recipe.json", "{}\n")
        self.write("recipes/news/Dockerfile", "FROM fixture:1\n")
        self.write("recipes/news/vendor/agent-reach/LICENSE", "MIT fixture\n")
        self.write("recipes/news/vendor/agent-reach.sha256", "reviewed manifest fixture\n")
        self.write("recipes/news/vendor/agent-reach-wheel.patch", "reviewed patch fixture\n")
        self.write("recipes/hyperframes/vendor/source.tar.gz", gzip.compress(b"pinned source fixture"))
        self.write("tests/fixtures/elf-header", b"\x7fELFfixture")

    def write(self, name, data):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data.encode() if isinstance(data, str) else data)
        return path

    def manifest(self):
        entries = []
        for path in sorted(self.root.rglob("*")):
            if path.is_file() and path.name != "SHA256SUMS":
                entries.append(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.relative_to(self.root).as_posix())
        self.write("SHA256SUMS", "\n".join(entries) + "\n")

    def test_release_accepts_reviewed_archives_licenses_patches_and_explicit_fixtures(self):
        result = hygiene.inspect_source(self.root, release=True)
        self.assertTrue(result["passed"], result)
        self.manifest()
        self.assertTrue(hygiene.inspect_source(self.root, verify_manifest=True)["manifestVerified"])

    def test_shared_recipe_sources_keep_the_same_release_and_manifest_boundaries(self):
        self.write("recipes/common/tools/install-codex.sh", "#!/bin/sh\nset -eu\n")
        self.write("recipes/common/vendor/agent-reach/LICENSE", "MIT fixture\n")
        self.write("recipes/common/vendor/agent-reach/agent_reach/core.py", "value = 1\n")
        self.write("recipes/common/vendor/agent-reach.sha256", "reviewed manifest fixture\n")
        self.write("recipes/common/vendor/agent-reach-wheel.patch", "reviewed patch fixture\n")
        self.manifest()
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(result["manifestVerified"], result)
        self.write("recipes/common/vendor/agent-reach/agent_reach/core.py", "value = 2\n")
        self.assertTrue(hygiene.inspect_source(self.root)["passed"])
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(any("checksum mismatch: recipes/common/" in error for error in result["errors"]), result)

    def test_developer_outputs_ignored_but_cannot_enter_release(self):
        self.write("build/qa/test-result.json", "{}")
        self.write("bin/rundesk", b"\x7fELFfixture")
        self.write("data/state.db", b"runtime")
        self.write("__pycache__/local.pyc", b"cache")
        self.assertTrue(hygiene.inspect_source(self.root)["passed"])
        result = hygiene.inspect_source(self.root, release=True)
        self.assertFalse(result["passed"])
        self.assertEqual(len(result["errors"]), 4, result)

    def test_canonical_resources_reject_cache_and_competing_derived_metadata(self):
        names = [f"recipes/{base}/{suffix}" for base in ("news", "common")
                 for suffix in ("node_modules/package/index.js", "__pycache__/source.pyc",
                                "profile.lock.json", "SHA256SUMS", "upstream/upstream.json")]
        for name in names:
            with self.subTest(name=name):
                self.write(name, "generated")
                result = hygiene.inspect_source(self.root)
                self.assertFalse(result["passed"], result)
                (self.root / name).unlink()
                for directory in sorted((self.root / "recipes").rglob("*"), key=lambda p: len(p.parts), reverse=True):
                    if directory.is_dir() and not any(directory.iterdir()):
                        directory.rmdir()

    def test_native_output_temporary_file_and_resource_link_rejected(self):
        for name, data in (("rundesk", b"\x7fELFfixture"), ("recipes/news/testdata/native", b"\x7fELFfixture"),
                           ("recipes/common/vendor/native", b"\x7fELFfixture"), ("scripts/editor.py.bak", b"old")):
            with self.subTest(name=name):
                self.write(name, data)
                self.assertFalse(hygiene.inspect_source(self.root)["passed"])
                (self.root / name).unlink()
        linked = self.root / "recipes/news/tools"
        linked.symlink_to(self.root / "tests", target_is_directory=True)
        result = hygiene.inspect_source(self.root)
        self.assertFalse(result["passed"])
        self.assertTrue(any("symbolic link" in error for error in result["errors"]))

    def test_validation_keeps_only_current_summary_and_requires_actual_pass(self):
        self.write("docs/validation/current/release-065.json", "{}")
        self.assertFalse(hygiene.inspect_source(self.root)["passed"])
        (self.root / "docs/validation/current/release-065.json").unlink()
        self.write("docs/validation/current/release-066.json", json.dumps({"version": "0.66.0", "status": "validation in progress"}))
        self.assertTrue(hygiene.inspect_source(self.root)["passed"])
        self.assertFalse(hygiene.inspect_source(self.root, release=True)["passed"])

    def test_manifest_detects_edits_omissions_and_missing_or_unsafe_entries(self):
        self.manifest()
        self.write("recipes/news/Dockerfile", "FROM changed:2\n")
        self.assertTrue(hygiene.inspect_source(self.root)["passed"], "Normal editing must not depend on stale release checksums")
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(any("checksum mismatch" in error for error in result["errors"]))
        self.manifest()
        self.write("README.md", "unlisted new source")
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(any("not listed" in error for error in result["errors"]))
        self.manifest()
        (self.root / "README.md").unlink()
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(any("listed file is missing" in error for error in result["errors"]))
        self.write("SHA256SUMS", "0" * 64 + "  ../outside\n")
        result = hygiene.inspect_source(self.root, verify_manifest=True)
        self.assertTrue(any("unsafe" in error for error in result["errors"]))


if __name__ == "__main__":
    unittest.main()
