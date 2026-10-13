---
name: research-public-people
description: Research public professional information about named people, disambiguate names and produce source-backed profiles. Use for speakers, authors, researchers, founders, public careers and published work; exclude private contact discovery and unsolicited outreach.
---

# Research public professional profiles

Read /opt/rundesk-profile/profile.lock.json to identify the configured Exa MCP. Use a new output directory and preserve source evidence.

1. Extract the supplied name, organization, role, region and research purpose. Search names in relevant languages. Ask for a distinguishing fact only when the available evidence cannot resolve a material ambiguity.
2. Inspect the enabled search tool's schema. Search official organization profiles, publication pages and the person's public professional website. Use secondary sources for context.
3. Keep same-name candidates separate until multiple distinguishing facts agree. Separate current roles from dated roles and record the dates supported by sources.
4. Fetch public pages with MCP or `python3 /opt/rundesk-profile/tools/read_web.py webpage URL --out NEW_DIRECTORY`. Jina receives that public URL. Preserve raw responses, excerpts and failures; a result snippet is not enough to confirm identity.
5. Save sources.json with id, url, title, fetchedAt, publishedAt, evidencePath and status. Save people.json with candidateId, name, publicRoles, works, sourceIds, identityConfidence and unresolvedConflicts. Use null or an empty list when evidence is absent.
6. Write report.md with the requested professional findings, explicit identity uncertainty and clickable source URLs. Include only public, task-relevant professional facts. Do not infer sensitive traits, find private addresses/phone numbers, or contact people.
7. Verify source files and IDs before finishing. Distinguish failed retrieval from no matching person.

Agent-Reach may be present as an optional CLI; this profile does not configure private accounts or platform cookies. Do not run an environment installer during research.
