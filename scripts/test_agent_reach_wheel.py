#!/usr/bin/env python3
"""Build/install the vendored wheel offline and verify its non-Python resources.

Requires hatchling in the selected Python environment. Run in a throwaway venv;
this checks packaging only, not live channels, model calls or a Docker image.
"""
import importlib.metadata
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "examples"))
import profiles


def main():
    pin = profiles.read_json(ROOT / "examples/_shared/dependencies.json")["agentReach"]
    source, files = profiles.agent_reach_files(pin)
    with tempfile.TemporaryDirectory(prefix="rundesk-reach-wheel-") as tmp:
        temp = Path(tmp)
        local = temp / "source"
        shutil.copytree(source, local)
        env = dict(os.environ, PIP_NO_INDEX="1", PYTHONDONTWRITEBYTECODE="1")
        subprocess.run([sys.executable, "-m", "hatchling", "build", "-t", "wheel", "-d", str(temp / "wheels")], cwd=local, env=env, check=True)
        wheels = list((temp / "wheels").glob("*.whl"))
        assert len(wheels) == 1, wheels
        resources = [name for name in files if name.startswith(("agent_reach/guides/", "agent_reach/skill/"))]
        assert "agent_reach/guides/setup-exa.md" in resources and "agent_reach/skill/SKILL.md" in resources
        with zipfile.ZipFile(wheels[0]) as wheel:
            assert len(wheel.namelist()) == len(set(wheel.namelist())), "duplicate wheel paths"
            for name in resources:
                assert wheel.read(name) == (source / name).read_bytes(), name
        target = temp / "installed"
        subprocess.run([sys.executable, "-m", "pip", "install", "--no-index", "--no-deps", "--no-compile", "--target", str(target), str(wheels[0])], env=env, check=True, stdout=subprocess.PIPE)
        distribution, = list(importlib.metadata.distributions(path=[str(target)]))
        assert distribution.version == pin["version"]
        for name in resources:
            assert Path(distribution.locate_file(name)).read_bytes() == (source / name).read_bytes(), name
        print(json.dumps({"passed": True, "packageVersion": distribution.version, "hatchlingVersion": importlib.metadata.version("hatchling"), "resourceFiles": len(resources), "offlineBuildAndInstall": True, "runtimeDependenciesInstalled": False, "dockerTested": False}))


if __name__ == "__main__":
    main()
