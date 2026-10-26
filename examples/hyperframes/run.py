#!/usr/bin/env python3
"""Compile or build the independent upstream HyperFrames environment."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from profiles import main

if __name__ == "__main__":
    raise SystemExit(main(default_profile="hyperframes"))
