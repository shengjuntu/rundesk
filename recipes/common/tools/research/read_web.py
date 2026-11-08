#!/usr/bin/env python3
"""Save a bounded public web/RSS read with source evidence. No model calls."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import sys
import urllib.parse
import urllib.request

LIMIT = 8 * 1024 * 1024


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=["webpage", "rss"])
    parser.add_argument("url")
    parser.add_argument("--out", required=True, help="new evidence directory; never overwrite")
    args = parser.parse_args()
    u = urllib.parse.urlsplit(args.url)
    if u.scheme not in ("https", "http") or not u.hostname or u.username or u.password:
        parser.error("Use a public HTTP(S) URL without credentials")
    output = Path(args.out)
    output.mkdir(parents=True, exist_ok=False)
    evidence = {"schemaVersion": 1, "url": args.url, "fetchedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "publishedAt": None, "tool": "jina-reader" if args.kind == "webpage" else "feedparser", "status": "failed"}
    try:
        url = "https://r.jina.ai/" + args.url if args.kind == "webpage" else args.url
        request = urllib.request.Request(url, headers={"User-Agent": "RunDesk-profile/1.0", "X-No-Cache": "true"})
        with urllib.request.urlopen(request, timeout=40) as response:
            raw = response.read(LIMIT + 1)
            evidence["httpStatus"] = response.status
            evidence["contentType"] = response.headers.get("Content-Type", "")
        if len(raw) > LIMIT:
            raise ValueError("Response exceeds 8 MiB; content not recorded as complete")
        filename = "content.md" if args.kind == "webpage" else "feed.xml"
        (output / filename).write_bytes(raw)
        evidence.update(status="fetched", sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw), contentFile=filename)
        if args.kind == "rss":
            import feedparser
            feed = feedparser.parse(raw)
            entries = [{"title": e.get("title"), "url": e.get("link"), "publishedAt": e.get("published"), "updatedAt": e.get("updated")} for e in feed.entries[:200]]
            (output / "entries.json").write_text(json.dumps(entries, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            evidence.update(entries=len(entries), parseWarning=bool(feed.bozo))
            if feed.bozo and not entries:
                raise ValueError("Response is not a usable RSS/Atom feed")
        # Fetch success is not truth verification or a promise of a complete article.
        evidence["verified"] = False
    except Exception as e:
        evidence.update(status="failed", error=str(e))
    (output / "source.json").write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(evidence, ensure_ascii=False))
    return 0 if evidence["status"] == "fetched" else 1


if __name__ == "__main__":
    raise SystemExit(main())
