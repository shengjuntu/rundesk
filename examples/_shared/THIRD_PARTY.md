# Profile dependencies

These build recipes reference upstream packages; this source ZIP does not vendor their code or binaries. Image builds install their distributions and accompanying licenses.

- Codex: https://github.com/openai/codex — Apache-2.0; exact release supplied at prepare/build time.
- Agent-Reach: https://github.com/silently0801/agent-reach — MIT; pinned fork commit in dependencies.json. News and people profiles install the CLI plus gh, mcporter, bird and undici; the upstream Skill is copied to /etc/codex/skills at image build time. Credentials and optional external platform services remain runtime configuration.
- yt-dlp: https://github.com/yt-dlp/yt-dlp ; feedparser: https://github.com/kurtmckee/feedparser ; Pillow: https://github.com/python-pillow/Pillow . Consult the installed package notices, including transitive dependencies.
- mcporter: https://github.com/steipete/mcporter ; bird: https://github.com/steipete/bird ; undici: https://github.com/nodejs/undici ; GitHub CLI: https://github.com/cli/cli . Preserve their installed license files.
- Node/Debian, FFmpeg and Noto fonts are installed from their distributions; retain /usr/share/doc notices. FFmpeg build licensing depends on enabled components.
- Exa and Jina are external hosted services, not software embedded in this image. Their usage limits and service terms apply independently.

No third-party source was copied into RunDesk to implement these profiles. Full corresponding source for exact upstream components must be retained/provided as required when redistributing built images; this file alone is not a complete binary-image licensing inventory.

## Agent-Reach license

MIT License

Copyright (c) 2025 Agent Eyes

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
