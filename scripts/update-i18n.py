"""Embed locales-en.json into i18n.js without changing its runtime logic."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "internal/web"


def unique_pairs(pairs):
    catalog = {}
    for key, value in pairs:
        if key in catalog:
            raise ValueError(f"Duplicate translation key: {key!r}")
        catalog[key] = value
    return catalog


def render_source(source, catalog, *, allow_removal=False):
    if not isinstance(catalog, dict) or not all(isinstance(key, str) and isinstance(value, str)
                                                for key, value in catalog.items()):
        raise ValueError("The translation catalog must contain string keys and values")
    marker = "const catalog="
    if source.count(marker) != 1:
        raise ValueError("Expected one generated catalog declaration in i18n.js")
    start = source.index(marker) + len(marker)
    end = source.index(" const language=", start)
    runtime, consumed = json.JSONDecoder(object_pairs_hook=unique_pairs).raw_decode(source[start:])
    if not isinstance(runtime, dict):
        raise ValueError("The generated runtime catalog must be a JSON object")
    if source[start + consumed:end].strip() != ";":
        raise ValueError("Move runtime catalog extensions into locales-en.json before generating")
    missing = runtime.keys() - catalog.keys()
    if missing and not allow_removal:
        raise ValueError(f"{len(missing)} runtime translations are missing from locales-en.json; "
                         "restore them or use --allow-removal for intentional pruning")
    return source[:start] + json.dumps(catalog, ensure_ascii=False, separators=(",", ":")) + ";\n" + source[end:]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Check generated dictionary parity without writing")
    parser.add_argument("--allow-removal", action="store_true", help="Allow intentional removal of runtime translation keys")
    args = parser.parse_args()
    try:
        catalog = json.loads((ROOT / "locales-en.json").read_text(encoding="utf-8"), object_pairs_hook=unique_pairs)
        target = ROOT / "i18n.js"
        source = target.read_text(encoding="utf-8")
        rendered = render_source(source, catalog, allow_removal=args.allow_removal)
        if args.check and source != rendered:
            parser.exit(1, "i18n.js differs from locales-en.json; run python3 scripts/update-i18n.py\n")
        if not args.check and source != rendered:
            target.write_text(rendered, encoding="utf-8")
    except (OSError, ValueError) as error:
        parser.exit(1, f"i18n generation failed: {error}\n")
    print(f"English catalog {'checked' if args.check else 'generated'}: {len(catalog)} translations")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
