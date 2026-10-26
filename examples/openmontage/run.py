#!/usr/bin/env python3
"""Build the independent OpenMontage profile using the common RunDesk compiler."""
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from profiles import main
if __name__ == "__main__":
    raise SystemExit(main(default_profile="openmontage"))
