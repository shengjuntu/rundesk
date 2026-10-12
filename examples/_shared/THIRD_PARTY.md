# Profile dependencies

These build recipes reference upstream packages; this source ZIP does not vendor their code or binaries. Image builds install their distributions and accompanying licenses.

- Codex: https://github.com/openai/codex — Apache-2.0; exact release supplied at prepare/build time.
- Wigolo: https://github.com/KnockOutEZ/wigolo — AGPL-3.0-only, with separate commercial terms described by upstream. Optional, unmodified external MCP process; installing it does not relicense it under RunDesk's license. Retain its license and corresponding-source information when distributing an image. See https://github.com/KnockOutEZ/wigolo/blob/main/LICENSING.md .
- Agent-Reach: https://github.com/silently0801/agent-reach — MIT; optional, pinned fork commit in dependencies.json. The recipe installs its CLI but does not execute its environment installer or copy its upstream Skill.
- yt-dlp: https://github.com/yt-dlp/yt-dlp ; feedparser: https://github.com/kurtmckee/feedparser ; Pillow: https://github.com/python-pillow/Pillow . Consult the installed package notices, including transitive dependencies.
- Node/Debian, FFmpeg and Noto fonts are installed from their distributions; retain /usr/share/doc notices. FFmpeg build licensing depends on enabled components.
- Exa and Jina are external hosted services, not software embedded in this image. Their usage limits and service terms apply independently.

No third-party source was copied into RunDesk to implement these profiles. Full corresponding source for exact upstream components must be retained/provided as required when redistributing built images; this file alone is not a complete binary-image licensing inventory.
