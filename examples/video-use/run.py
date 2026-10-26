#!/usr/bin/env python3
"""Independent upstream video-use profile; the self-developed video-build stays separate."""
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from profiles import main
if __name__ == "__main__":
    raise SystemExit(main(default_profile="video-use"))
