#!/usr/bin/env python3
"""Guard provider-generating resolve calls; leave native editing and ingest intact."""
import argparse
import json
import sys


def blocked_reason(argv):
    if len(argv) < 2 or argv[:2] != ["media-use", "resolve"]:
        return None
    # These are the actual fixed-release media-use resolver flags. Parsing string
    # values avoids mistaking a literal '--from' inside an intent for an ingest.
    parser = argparse.ArgumentParser(add_help=False, allow_abbrev=False)
    for name in ("type", "intent", "entity", "project", "days", "reuse", "from", "params", "for", "provider", "avatar-id", "voice-id", "source"):
        parser.add_argument("--" + name, **({"dest": "project"} if name == "project" else {}))
    parser.add_argument("-p", dest="project")
    for name in ("adopt", "candidates", "doctor", "stats", "dry-run", "analyze", "local-only", "json", "help"):
        parser.add_argument("--" + name, action="store_true")
    parser.add_argument("-h", dest="help", action="store_true")
    args, _ = parser.parse_known_args(argv[2:])
    if args.help or args.candidates or args.doctor or args.stats or args.dry_run:
        return None
    if getattr(args, "from") or args.reuse is not None or args.adopt:
        return None
    if args.type in ("voice", "image", "video"):
        return "Use video_app_media MCP or python3 /opt/rundesk-profile/tools/video_app_media.py " + ("tts" if args.type == "voice" else args.type) + "; then use native media-use resolve --from LOCAL_FILE. Upstream provider fallback is disabled in this environment."
    if args.type in ("bgm", "sfx"):
        return "Video App does not expose BGM/SFX generation here. Reuse existing local music/sound and native media-use resolve --from LOCAL_FILE; do not fall back to another generation model."
    return None


def main():
    reason = blocked_reason(sys.argv[1:])
    if reason:
        print(json.dumps({"ok": False, "code": "video_app_generation_required", "error": reason}), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
