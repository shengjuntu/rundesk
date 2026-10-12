---
name: collect-news-evidence
description: Collect and verify news sources for news2douyin, then draft a sourced brief and short-video script. Use for news discovery, article reading, release coverage or video-transcript research; do not publish posts.
---

# Collect news evidence

Read /opt/rundesk-profile/profile.lock.json to identify the selected search backend and installed tools. Work within the supplied project directory. Create a new output subdirectory for each task; preserve previous evidence.

1. Resolve the topic, language, location and time window from the task. Distinguish event time, publication time and retrieval time. State unresolved dates explicitly.
2. Use the enabled Exa or Wigolo MCP search tools. Inspect the current tool schema rather than guessing parameter names. With Wigolo, request fresh results for current news and retain degradation, stale-cache and failed-engine indicators. Do not silently switch providers.
3. Open primary announcements and independent coverage. Read full sources; do not turn search snippets into verified facts. Use Jina only for public URLs: `python3 /opt/rundesk-profile/tools/read_web.py webpage URL --out NEW_DIRECTORY`. This sends the URL to Jina's service.
4. Parse a supplied public RSS/Atom feed with the same helper using `rss` instead of `webpage`. Record a feed's date as source metadata, not as proof of the event date.
5. When a relevant video is supplied, inspect metadata and available subtitles with yt-dlp. Save subtitle files and metadata before summarizing. Report absent subtitles; do not claim to have understood unseen frames. Download only the material needed for the task.
6. Write sources.json as an array of records with id, url, title, publishedAt, fetchedAt, tool, evidencePath, status, verified and limitations. Use null for unknown fields. Save actual tool responses separately; retain source excerpts, citation IDs and hashes where provided. A fetch success is not a truth verdict.
7. Write brief.md with checked claims, supporting source IDs, disagreements and missing evidence. Write script.md with source IDs attached to factual claims, estimated duration and proposed visuals. Keep proposed visuals distinct from retrieved footage.
8. Check that referenced evidence exists and that every factual claim in the script has an adequate source. Mark failures instead of filling gaps with invented facts.

Use FFmpeg/Pillow for explicitly requested media preparation and save commands and outputs. Keep generated files in the project. The profile does not contain a TTS model, a visual video-understanding model, the news2douyin application, or publishing credentials. Agent-Reach, if installed, is an auxiliary CLI: do not run its installer during a news task or assume all its channels are configured.
