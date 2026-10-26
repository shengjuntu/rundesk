#!/bin/sh
# Run the unmodified official suite against an already-running RunDesk server.
# Dependencies and the pinned checkout are supplied by the caller; no download,
# server startup, credential provisioning, or provider call occurs implicitly.
set -eu

pin=caa3c0a109fffd84ef245dcdad23f2cdfeff7153
if [ -z "${UHP_SUITE_CHECKOUT:-}" ]; then
  echo "Set UHP_SUITE_CHECKOUT to a HarnessRouter checkout at $pin." >&2
  echo "Usage: UHP_API_KEY=... UHP_SUITE_CHECKOUT=... $0 --base-url URL --class core|extended --json REPORT --plain" >&2
  exit 2
fi

checkout=$(cd "$UHP_SUITE_CHECKOUT" && pwd)
actual=$(git -C "$checkout" rev-parse HEAD)
if [ "$actual" != "$pin" ]; then
  echo "Refusing a different suite revision: expected $pin, found $actual." >&2
  exit 2
fi
if ! git -C "$checkout" diff --quiet HEAD -- protocol/conformance; then
  echo "Refusing a modified official conformance suite." >&2
  exit 2
fi
untracked=$(git -C "$checkout" ls-files --others --exclude-standard -- protocol/conformance)
if [ -n "$untracked" ]; then
  echo "Refusing untracked files in the official conformance suite." >&2
  exit 2
fi

suite="$checkout/protocol/conformance"
if [ ! -f "$suite/uhp_conformance/cli.py" ]; then
  echo "The checkout must include protocol/conformance (including its packaged JSON schema)." >&2
  exit 2
fi
PYTHONPATH="$suite${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONPATH
python=${PYTHON:-python3}
"$python" -c 'import jsonschema' || {
  echo "Install the official suite dependency jsonschema for the selected PYTHON interpreter." >&2
  exit 2
}
exec "$python" -m uhp_conformance.cli "$@"
