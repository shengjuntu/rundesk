# Independent upstream environment

Repository: https://github.com/calesthio/OpenMontage
Commit: c8066452325cb57c8fc1ba8c2a71b6e8842e6b65
License: AGPL-3.0
Source scope: runtime-without-demo-media

The pinned code, Skills, documentation, tests, templates, fonts, icons and runtime assets are supplied in `vendor/source.tar.xz` and remain in `/opt/upstreams/openmontage`. Eight reviewed files are omitted: the three demonstration MP4 files already omitted in 0.69.0, four Backlot documentation-preview PNG files, and one unreferenced example logo PNG. All 2,163 retained files keep their original bytes, paths and executable permission semantics. Duplicate resource paths remain independent ordinary files; compression does not turn them into hard links or symbolic links. Read the original upstream LICENSE there.

[Source bundle provenance](upstream/source-bundle.json) records the original codeload URL, original archive SHA-256 (`dff7cd1cdf2cb0522398e00b3f0b75f32336b695a8be34ac8e6b6913055aab25`), shipped archive SHA-256 (`a46a636050a56fd86975d4429a0ea51c7ea4c164349e7e848f3ff6aa974c233d`), compression parameters, every retained file hash and the exact omitted paths/hashes. The original commit and license remain unchanged; this bundle is not represented as a byte-identical full codeload archive. Retained documentation may still reference omitted previews; the original source archive supplies those previews.

Runtime packages are installed separately from requirements/npm lockfiles. The shipped archive and reviewed integration sources remain in `/opt/rundesk-upstream`. Preserve the supplied source, integration sources and license files when redistributing an image. No upstream code is imported into RunDesk's Go server or the independent video-build workflow.
