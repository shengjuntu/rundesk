"""Embed the reviewed English catalog into i18n.js (no runtime fetch required)."""
import json
from pathlib import Path
root = Path(__file__).resolve().parents[1] / 'internal/web'
catalog = json.loads((root / 'locales-en.json').read_text())
p = root / 'i18n.js'
s = p.read_text()
a = s.index('const catalog=') + len('const catalog=')
b = s.index(';\n const language', a)
p.write_text(s[:a] + json.dumps(catalog, ensure_ascii=False) + s[b:])
