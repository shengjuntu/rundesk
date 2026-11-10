#!/usr/bin/env python3
"""Exercise the real stdlib source extractor without Docker or network access."""
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

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("openmontage_source_extractor", ROOT / "recipes/openmontage/upstream/extract-source.py")
extractor = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(extractor)


class SourceExtractionTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.output = self.root / "parent" / "source"
        self.source = self.root / "source.archive"

    def archive(self, items, compression="xz"):
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as archive:
            for name, content, kind in items:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.mode = 0o755 if name.endswith(".py") else 0o644
                if kind in (tarfile.REGTYPE, tarfile.AREGTYPE):
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
                else:
                    member.linkname = content.decode() if content else ""
                    archive.addfile(member)
        data = lzma.compress(raw.getvalue()) if compression == "xz" else gzip.compress(raw.getvalue(), mtime=0)
        self.source.write_bytes(data)
        return hashlib.sha256(data).hexdigest()

    def run_extract(self, digest):
        extractor.extract(self.source, digest, self.output, "source")

    def assert_clean_failure(self, digest):
        with self.assertRaises((OSError, ValueError, tarfile.TarError, EOFError, lzma.LZMAError)):
            self.run_extract(digest)
        self.assertFalse(self.output.exists())
        self.assertEqual(list(self.output.parent.glob(".openmontage-source-*")), [])

    def test_gzip_and_xz_keep_paths_bytes_and_executable_permissions(self):
        for compression in ("gz", "xz"):
            with self.subTest(compression=compression):
                digest = self.archive([("source/LICENSE", b"license text", tarfile.REGTYPE),
                                       ("source/tools/run.py", b"print('source')\n", tarfile.REGTYPE)], compression)
                self.run_extract(digest)
                self.assertEqual((self.output / "LICENSE").read_bytes(), b"license text")
                self.assertEqual((self.output / "tools/run.py").read_bytes(), b"print('source')\n")
                self.assertEqual((self.output / "tools/run.py").stat().st_mode & 0o777, 0o755)
                self.assertEqual((self.output / "LICENSE").stat().st_mode & 0o777, 0o644)
                self.assertEqual(self.output.stat().st_mode & 0o777, 0o755)
                self.assertEqual(self.output.parent.stat().st_mode & 0o777, 0o755)
                self.assertEqual((self.output / "tools").stat().st_mode & 0o777, 0o755)
                import shutil
                shutil.rmtree(self.output)

    def test_wrong_hash_and_existing_or_linked_paths_are_rejected(self):
        digest = self.archive([("source/file", b"retained", tarfile.REGTYPE)])
        self.assert_clean_failure("0" * 64)
        self.output.mkdir(parents=True)
        marker = self.output / "marker"; marker.write_text("untouched")
        with self.assertRaises(FileExistsError):
            self.run_extract(digest)
        self.assertEqual(marker.read_text(), "untouched")
        self.output.rename(self.root / "existing")
        self.output.symlink_to(self.root / "existing", target_is_directory=True)
        with self.assertRaises(ValueError):
            self.run_extract(digest)
        self.output.unlink()
        self.source.rename(self.root / "original.archive")
        self.source.symlink_to(self.root / "original.archive")
        self.assert_clean_failure(digest)

    def test_unsafe_roots_paths_aliases_and_file_directory_conflicts_leave_no_output(self):
        cases = [
            [("wrong/file", b"x", tarfile.REGTYPE)],
            [("/source/file", b"x", tarfile.REGTYPE)],
            [("source/../outside", b"x", tarfile.REGTYPE)],
            [("source/./file", b"x", tarfile.REGTYPE)],
            [("source//file", b"x", tarfile.REGTYPE)],
            [("source/file/", b"x", tarfile.REGTYPE)],
            [("source/back\\slash", b"x", tarfile.REGTYPE)],
            [("source/file", b"x", tarfile.REGTYPE), ("source/file", b"y", tarfile.REGTYPE)],
            [("source/Dir/a", b"x", tarfile.REGTYPE), ("source/dir/b", b"y", tarfile.REGTYPE)],
            [("source/file", b"x", tarfile.REGTYPE), ("source/file/nested", b"y", tarfile.REGTYPE)],
        ]
        for entries in cases:
            with self.subTest(entries=[item[0] for item in entries]):
                self.assert_clean_failure(self.archive(entries))

    def test_links_and_special_entries_are_not_extracted(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.FIFOTYPE, tarfile.CHRTYPE):
            with self.subTest(kind=kind):
                self.assert_clean_failure(self.archive([("source/file", b"../../outside", kind)]))

    def test_archive_content_entry_and_decoder_memory_limits_are_enforced(self):
        digest = self.archive([("source/one", b"abc", tarfile.REGTYPE), ("source/two", b"def", tarfile.REGTYPE)])
        for name, limit in (("MAX_ARCHIVE", 1), ("MAX_CONTENT", 2), ("MAX_MEMBERS", 1), ("XZ_MEMORY_LIMIT", 1 << 20), ("MAX_TAR", 512)):
            with self.subTest(limit=name), patch.object(extractor, name, limit):
                self.assert_clean_failure(digest)

    def test_truncated_compressed_footer_and_trailing_xz_data_are_rejected(self):
        for compression in ("xz", "gz"):
            original_digest = self.archive([("source/file", b"source data", tarfile.REGTYPE)], compression)
            data = self.source.read_bytes()
            self.source.write_bytes(data[:-4])
            with self.subTest(compression=compression):
                self.assert_clean_failure(hashlib.sha256(data[:-4]).hexdigest())
        self.archive([("source/file", b"source data", tarfile.REGTYPE)])
        changed = self.source.read_bytes() + b"unrelated trailing stream"
        self.source.write_bytes(changed)
        self.assert_clean_failure(hashlib.sha256(changed).hexdigest())

    def test_actual_reviewed_bundle_extracts_every_preserved_file(self):
        recipe = ROOT / "recipes/openmontage"
        record = json.loads((recipe / "upstream/source-bundle.json").read_text())
        extractor.extract(recipe / record["bundlePath"], record["bundleSHA256"], self.output,
                          "OpenMontage-" + record["commit"])
        expected = record["preservedFiles"]
        actual = {p.relative_to(self.output).as_posix(): p for p in self.output.rglob("*") if p.is_file()}
        self.assertEqual(actual.keys(), expected.keys())
        for name, metadata in expected.items():
            self.assertEqual(hashlib.sha256(actual[name].read_bytes()).hexdigest(), metadata["sha256"], name)
            self.assertEqual(actual[name].stat().st_mode & 0o777, metadata["mode"], name)
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o755)
        self.assertTrue((self.output / "LICENSE").is_file())


if __name__ == "__main__":
    unittest.main()
