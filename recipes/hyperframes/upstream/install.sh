#!/bin/sh
set -eu

src=/opt/upstreams/hyperframes
overlay=/opt/rundesk-upstream
test -f "$src/LICENSE"
test -f "$src/skills/hyperframes/SKILL.md"
mkdir -p /opt/hyperframes
cp "$overlay/npm/package.json" "$overlay/npm/package-lock.json" /opt/hyperframes/
npm ci --prefix /opt/hyperframes --omit=dev --no-audit --no-fund
npm cache clean --force
install -m 755 "$overlay/hyperframes" /usr/local/bin/hyperframes
node -e 'if (Number(process.versions.node.split(".")[0]) < 22) throw new Error("Node 22+ required"); if (require("/opt/hyperframes/node_modules/hyperframes/package.json").version !== "0.8.87") throw new Error("wrong HyperFrames version"); if (require("/opt/hyperframes/node_modules/gsap/package.json").version !== "3.15.0") throw new Error("wrong GSAP version")'
hyperframes --version
chromium --version
chmod -R a+rX /opt/hyperframes "$src"
