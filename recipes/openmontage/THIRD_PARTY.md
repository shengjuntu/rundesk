# Independent upstream environment

Repository: https://github.com/calesthio/OpenMontage
Commit: c8066452325cb57c8fc1ba8c2a71b6e8842e6b65
License: AGPL-3.0
Source scope: runtime-without-demo-media

The pinned code, Skills, documentation, tests, templates, fonts, icons and runtime assets are supplied in `vendor/source.tar.gz` and remain in `/opt/upstreams/openmontage`. Only three unreferenced demonstration MP4 files are omitted. The source bytes of all 2,168 retained files are unchanged. Read the original upstream LICENSE there.

[Source bundle provenance](upstream/source-bundle.json) records the original codeload URL, original archive SHA-256 (`dff7cd1cdf2cb0522398e00b3f0b75f32336b695a8be34ac8e6b6913055aab25`), shipped archive SHA-256 (`3d6ddce72b077bdcaed2ccc30aa61f80ec41869a55228ca1ceb0fe5d81be2ca5`), every retained file hash and the exact three omitted paths/hashes. The original commit and license remain unchanged; this bundle is not represented as a byte-identical full codeload archive.

Runtime packages are installed separately from requirements/npm lockfiles. The shipped archive and reviewed integration sources remain in `/opt/rundesk-upstream`. Preserve the supplied source, integration sources and license files when redistributing an image. No upstream code is imported into RunDesk's Go server or the independent video-build workflow.
