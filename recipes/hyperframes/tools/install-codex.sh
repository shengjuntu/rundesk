#!/bin/sh
# Resolve an explicitly selected runtime without silently substituting versions.
set -eu

version=${CODEX_VERSION:-}
mode=${RD_CODEX_MODE:-auto}
runtime=${RD_CODEX_RUNTIME_DIR:-/opt/rundesk-runtime}
case "$version" in
    ''|0.0.0|*[!0-9A-Za-z.-]*) echo 'Set CODEX_VERSION to an exact release.' >&2; exit 64 ;;
esac
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || { echo 'Invalid CODEX_VERSION.' >&2; exit 64; }

verify_codex() {
    actual=$(codex --version) || return 1
    [ "$actual" = "codex-cli $version" ] || [ "$actual" = "codex $version" ] || {
        echo "Codex version mismatch: expected $version, found $actual" >&2
        return 1
    }
    codex app-server --help >/dev/null
}

case "$mode" in
    native)
        # Keep the complete native vendor tree, including path/rg companions.
        binary=
        for candidate in "$runtime/native/bin/codex" "$runtime/native/codex/codex"; do
            if [ -f "$candidate" ] && [ -x "$candidate" ]; then
                [ -z "$binary" ] || { echo 'Ambiguous native Codex payload.' >&2; exit 64; }
                binary=$candidate
            fi
        done
        [ -n "$binary" ] || { echo 'Native Codex payload is missing or not executable.' >&2; exit 64; }
        # The managed builder verifies the frozen payload hash before Docker.
        # This executable check also catches incompatible architecture/libc.
        "$binary" --version >/dev/null
        ln -sf "$binary" /usr/local/bin/codex
        PATH="/usr/local/bin:$PATH"
        export PATH
        verify_codex
        echo "Reused native Codex $version."
        ;;
    base)
        command -v codex >/dev/null || { echo 'The selected base image does not contain Codex.' >&2; exit 64; }
        verify_codex
        echo "Reused base-image Codex $version."
        ;;
    auto|npm)
        if [ "$mode" = auto ] && command -v codex >/dev/null && verify_codex; then
            echo "Reused installed Codex $version."
            exit 0
        fi
        npm_config_registry="${RD_NPM_REGISTRY:-https://registry.npmjs.org}" npm install --global "@openai/codex@$version"
        npm cache clean --force
        verify_codex
        ;;
    *) echo 'RD_CODEX_MODE must be auto, native, base, or npm.' >&2; exit 64 ;;
esac
