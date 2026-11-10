#!/usr/bin/env python3
"""Verify the local-only OpenMontage repack boundary; no network or Docker."""
import gzip
import hashlib
import importlib.util
import io
import json
import lzma
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().with_name("package_openmontage_source.py")
SPEC = importlib.util.spec_from_file_location("package_openmontage_source", SCRIPT)
bundle = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bundle)


class PackageOpenMontageSourceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def archive(self, *, extra=(), missing_demo=False, reference=False, preview_referrer=None):
        files = [(name, ("demo " + str(n)).encode(), 0o644) for n, name in enumerate(bundle.OMITTED)]
        if missing_demo:
            files.pop()
        files += [("LICENSE", b"reviewed source license\n", 0o644),
                  ("tools/tool_registry.py", b"registry fixture\n", 0o644),
                  ("scripts/run.sh", b"#!/bin/sh\necho fixture\n", 0o755)]
        if reference:
            files += [("README.md", ("Use " + Path(bundle.OMITTED[0]).name).encode(), 0o644)]
        if preview_referrer:
            files += [(preview_referrer, ("Preview " + Path(bundle.DOCUMENT_PREVIEWS[0]).name).encode(), 0o644)]
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz") as archive:
            for name, data, mode in files:
                member = tarfile.TarInfo(bundle.PREFIX + "/" + name)
                member.mode = mode
                member.size = len(data)
                member.mtime = 1723456789
                member.uid = 123
                member.gid = 456
                member.uname = "local-user"
                member.gname = "local-group"
                archive.addfile(member, io.BytesIO(data))
            for member in extra:
                archive.addfile(member, io.BytesIO(b""))
        source = self.root / "source.tar.gz"
        source.write_bytes(output.getvalue())
        return source, {name: (data, mode) for name, data, mode in files}

    def package(self, source, suffix="", extension=".tar.gz"):
        with patch.object(bundle, "SOURCE_SHA256", hashlib.sha256(source.read_bytes()).hexdigest()):
            return bundle.package(source, self.root / ("bundle" + suffix + extension),
                                  self.root / ("manifest" + suffix + ".json"))

    def test_determinism_preserves_code_license_paths_bytes_and_exec_permissions(self):
        source, original = self.archive()
        one = self.package(source, "-one")
        two = self.package(source, "-two")
        first = (self.root / "bundle-one.tar.gz").read_bytes()
        second = (self.root / "bundle-two.tar.gz").read_bytes()
        self.assertEqual(first, second)
        self.assertEqual((self.root / "manifest-one.json").read_bytes(), (self.root / "manifest-two.json").read_bytes())
        self.assertEqual(one, two)
        self.assertEqual(first[4:8], b"\0\0\0\0", "gzip timestamp must be deterministic")
        expected = set(original) - set(bundle.OMITTED)
        with tarfile.open(fileobj=io.BytesIO(first), mode="r:gz") as archive:
            members = archive.getmembers()
            self.assertEqual({member.name.split("/", 1)[1] for member in members}, expected)
            for member in members:
                relative = member.name.split("/", 1)[1]
                data, mode = original[relative]
                self.assertTrue(member.isfile(), "bundle must not introduce links")
                self.assertEqual(archive.extractfile(member).read(), data)
                self.assertEqual(member.mode, mode)
                self.assertEqual((member.uid, member.gid, member.mtime, member.uname, member.gname), (0, 0, 0, "", ""))
                self.assertEqual(one["preservedFiles"][relative], {"sha256": hashlib.sha256(data).hexdigest(), "bytes": len(data), "mode": mode})
        self.assertEqual(one["bundleFiles"], len(expected))
        self.assertEqual({item["path"] for item in one["omittedFiles"]}, set(bundle.OMITTED))
        self.assertEqual(one["bundleSHA256"], hashlib.sha256(first).hexdigest())

    def test_xz_is_deterministic_with_bounded_dictionary_and_independent_files(self):
        source, original = self.archive()
        one = self.package(source, "-one", ".tar.xz")
        two = self.package(source, "-two", ".tar.xz")
        first = (self.root / "bundle-one.tar.xz").read_bytes()
        self.assertEqual(first, (self.root / "bundle-two.tar.xz").read_bytes())
        self.assertEqual(one, two)
        self.assertEqual(one["bundlePath"], "vendor/source.tar.xz")
        self.assertEqual(one["bundleCompression"], "xz")
        self.assertEqual(one["bundleDictionaryBytes"], 32 << 20)
        self.assertEqual(one["bundleCheck"], "crc64")
        decoder = lzma.LZMADecompressor(format=lzma.FORMAT_XZ, memlimit=40 << 20)
        raw = decoder.decompress(first)
        self.assertTrue(decoder.eof)
        with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
            expected = set(original) - set(bundle.OMITTED)
            self.assertEqual({m.name.split("/", 1)[1] for m in archive}, expected)
            for member in archive.getmembers():
                name = member.name.split("/", 1)[1]
                self.assertTrue(member.isfile())
                self.assertEqual(archive.extractfile(member).read(), original[name][0])

    def test_document_preview_references_only_remain_in_reviewed_readme(self):
        source, original = self.archive(preview_referrer="README.md")
        record = self.package(source, "-readme")
        self.assertIn("README.md", record["preservedFiles"])
        self.assertNotIn(bundle.DOCUMENT_PREVIEWS[0], record["preservedFiles"])
        self.assertEqual({item["path"] for item in record["omittedFiles"]}, set(bundle.OMITTED))
        source, _ = self.archive(preview_referrer="tools/runtime.py")
        with self.assertRaisesRegex(ValueError, "references a removed preview asset"):
            self.package(source, "-runtime")
        self.assertFalse((self.root / "bundle-runtime.tar.gz").exists())

    def test_unknown_archive_is_rejected_before_creating_destinations(self):
        source, _ = self.archive()
        with self.assertRaisesRegex(ValueError, "SHA256 mismatch"):
            bundle.package(source, self.root / "new/bundle.tar.gz", self.root / "new/manifest.json")
        self.assertFalse((self.root / "new").exists())

    def test_existing_destinations_and_symlink_source_are_not_followed(self):
        source, _ = self.archive()
        output = self.root / "bundle.tar.gz"
        output.write_bytes(b"keep existing")
        with self.assertRaises(FileExistsError):
            self.package(source)
        self.assertEqual(output.read_bytes(), b"keep existing")
        linked = self.root / "linked.tar.gz"
        linked.symlink_to(source)
        with self.assertRaisesRegex(ValueError, "Symbolic links"):
            bundle.package(linked, self.root / "new.tar.gz", self.root / "manifest.json")

    def test_unsafe_duplicate_and_link_entries_fail_without_output(self):
        cases = []
        traversal = tarfile.TarInfo(bundle.PREFIX + "/../escaped")
        cases.append(traversal)
        duplicate = tarfile.TarInfo(bundle.PREFIX + "/license")
        cases.append(duplicate)
        linked = tarfile.TarInfo(bundle.PREFIX + "/tools/link")
        linked.type = tarfile.SYMTYPE
        linked.linkname = "../../outside"
        cases.append(linked)
        for member in cases:
            with self.subTest(member=member.name):
                source, _ = self.archive(extra=[member])
                with self.assertRaises(ValueError):
                    self.package(source)
                self.assertFalse((self.root / "bundle.tar.gz").exists())
                self.assertFalse((self.root / "manifest.json").exists())

    def test_only_reviewed_unreferenced_demo_files_may_be_removed(self):
        source, _ = self.archive(missing_demo=True)
        with self.assertRaisesRegex(ValueError, "Reviewed omitted files are missing"):
            self.package(source)
        source, _ = self.archive(reference=True)
        with self.assertRaisesRegex(ValueError, "references a removed preview asset"):
            self.package(source)
        self.assertFalse((self.root / "bundle.tar.gz").exists())


if __name__ == "__main__":
    unittest.main()
