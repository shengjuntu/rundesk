"""Fixed, offline runtime/challenge probe. No user-selected commands or reads."""
import hashlib
import base64
import importlib.metadata
import json
import os
import platform
import re
import signal
import stat
import subprocess
import sys
import tempfile
import struct
import urllib.request
import urllib.error
import urllib.parse


def small_json(path, limit=65536):
    with open(path, "rb") as stream:
        raw = stream.read(limit + 1)
    if len(raw) > limit:
        raise ValueError("oversize")
    return json.loads(raw)


def version_command(argv):
    # A runaway version command has a bounded disk/output budget and deadline.
    def limits():
        import resource
        resource.setrlimit(resource.RLIMIT_FSIZE, (16384, 16384))
    with tempfile.TemporaryFile() as output:
        child = subprocess.Popen(argv, stdout=output, stderr=subprocess.DEVNULL,
                                 start_new_session=True, preexec_fn=limits)
        try:
            code = child.wait(timeout=4)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
            raise ValueError("timeout")
        output.seek(0)
        raw = output.read(16385)
    if code != 0 or len(raw) > 16384:
        raise ValueError("version failed")
    return raw.decode("utf-8", "strict")


def runtime_probe():
    checks = {"profile": "missing", "adapter": "missing", "sourcePin": "missing", "ffmpeg": "missing", "ffprobe": "missing", "pythonDependencies": "missing"}
    result = {"status": "incomplete", "profileId": "", "profileVersion": "",
              "adapterContractVersion": 0, "videoUseCommit": "", "hasVideoUse": False,
              "pythonVersion": platform.python_version(), "ffmpegVersion": "", "ffprobeVersion": "", "checks": checks}
    lock = None
    try:
        lock = small_json("/opt/rundesk-profile/profile.lock.json")
        profile = lock["profile"]
        for field in ("id", "version"):
            if not isinstance(profile[field], str) or not re.fullmatch(r"[A-Za-z0-9_.-]{1,80}", profile[field]):
                raise ValueError("invalid profile")
        result["profileId"], result["profileVersion"] = profile["id"], profile["version"]
        checks["profile"] = "ready"
    except FileNotFoundError:
        pass
    except Exception:
        checks["profile"] = "invalid"
    for binary in ("ffmpeg", "ffprobe"):
        try:
            raw = version_command([binary, "-version"])
            found = re.match(binary + r" version ([A-Za-z0-9_.+~-]{1,120})(?:\s|$)", raw)
            if not found:
                raise ValueError("invalid media version")
            result[binary + "Version"] = found.group(1)
            checks[binary] = "ready"
        except FileNotFoundError:
            pass
        except Exception:
            checks[binary] = "failed"
    video = lock is not None and result["profileId"] == "video-build"
    version = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)(?:-[A-Za-z0-9.-]+)?", result["profileVersion"])
    hyperframes = video and version is not None and tuple(map(int, version.groups())) >= (1, 4, 0)
    use_legacy = video and not hyperframes
    if hyperframes:
        # New profile selection is explicit; a malformed lock never disables required checks.
        selected = lock.get("selection", {}).get("videoUse")
        if type(selected) is not bool:
            checks["profile"] = "invalid"
            use_legacy = True
        else:
            use_legacy = selected
        for key in ("node", "hyperframes", "gsap", "browser", "hyperframesLock"):
            checks[key] = "missing"
        try:
            expected = lock["dependencies"]["hyperframes"]
            versions = expected["npm"]
            if set(versions) != {"hyperframes", "gsap"} or not all(isinstance(pin, str) and re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", pin) for pin in versions.values()):
                raise ValueError("invalid npm lock")
            for package in ("hyperframes", "gsap"):
                actual = small_json("/opt/hyperframes/node_modules/" + package + "/package.json")
                found = actual.get("version", "")
                if not isinstance(found, str) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", found):
                    raise ValueError("invalid package version")
                result[package + "Version"] = found
                checks[package] = "ready" if found == versions[package] else "invalid"
            with open("/opt/hyperframes/package-lock.json", "rb") as stream:
                raw = stream.read(1024 * 1024 + 1)
            pin = expected["npmLockSHA256"]
            if len(raw) > 1024 * 1024 or not re.fullmatch(r"[0-9a-f]{64}", pin):
                raise ValueError("invalid npm lock digest")
            checks["hyperframesLock"] = "ready" if hashlib.sha256(raw).hexdigest() == pin else "invalid"
        except Exception:
            for key in ("hyperframes", "gsap", "hyperframesLock"):
                if checks[key] == "missing":
                    checks[key] = "invalid"
        try:
            raw = version_command(["node", "--version"]).strip()
            found = re.fullmatch(r"v([0-9]+)\.([0-9]+)\.([0-9]+)", raw)
            if not found:
                raise ValueError("invalid node version")
            result["nodeVersion"] = raw
            checks["node"] = "ready" if int(found.group(1)) >= 22 else "invalid"
        except Exception:
            checks["node"] = "failed"
        try:
            raw = version_command(["node", "/opt/hyperframes/node_modules/hyperframes/bin/hyperframes.mjs", "--version"]).strip()
            if raw != result.get("hyperframesVersion"):
                raise ValueError("CLI version mismatch")
        except Exception:
            checks["hyperframes"] = "failed"
        try:
            # The image bundles this fixed browser path; do not execute arbitrary environment commands.
            raw = version_command(["/usr/bin/chromium", "--version"]).strip()
            found = re.search(r"(?:Chromium|Google Chrome(?: for Testing)?) ([0-9]+(?:\.[0-9]+){1,3})(?:\s|$)", raw)
            if not found:
                raise ValueError("invalid browser version")
            result["browserVersion"] = found.group(1)
            checks["browser"] = "ready"
        except Exception:
            checks["browser"] = "failed"
    if use_legacy:
        try:
            packages = lock["dependencies"]["videoUse"]["python"]
            if not isinstance(packages, dict) or not 1 <= len(packages) <= 300:
                raise ValueError("missing dependency lock")
            for name, version in packages.items():
                if not re.fullmatch(r"[A-Za-z0-9_.-]{1,120}", name) or not isinstance(version, str) or not re.fullmatch(r"[A-Za-z0-9_.+!-]{1,120}", version):
                    raise ValueError("invalid dependency lock")
                if importlib.metadata.version(name) != version:
                    raise ValueError("dependency version mismatch")
            checks["pythonDependencies"] = "ready"
        except Exception:
            checks["pythonDependencies"] = "invalid"
    elif hyperframes:
        checks["pythonDependencies"] = "not_required"
        checks["sourcePin"] = "not_required"
    adapter = "/opt/rundesk-profile/tools/video_use_adapter.py"
    if os.path.isfile(adapter):
        try:
            value = json.loads(version_command([sys.executable, "-I", adapter, "--version"]))
            if value.get("schemaVersion") != 1 or type(value.get("adapterVersion")) is not int or not 1 <= value["adapterVersion"] <= 10000:
                raise ValueError("invalid adapter")
            if not re.fullmatch(r"[0-9a-f]{40}", value.get("upstreamCommit", "")):
                raise ValueError("invalid commit")
            result["adapterContractVersion"] = value["adapterVersion"]
            result["videoUseCommit"] = value["upstreamCommit"]
            checks["adapter"] = "ready"
        except Exception:
            checks["adapter"] = "invalid"
    if use_legacy:
        try:
            pin = small_json("/opt/video-use/RUNDESK_PIN.json", 4096)
            if not re.fullmatch(r"[0-9a-f]{40}", pin.get("commit", "")):
                raise ValueError("invalid commit")
            if not os.path.isfile("/opt/video-use/helpers/render.py") or not os.path.isfile("/opt/video-use/LICENSE"):
                raise ValueError("incomplete source")
            checks["sourcePin"] = "ready" if pin["commit"] == result["videoUseCommit"] else "invalid"
        except FileNotFoundError:
            pass
        except Exception:
            checks["sourcePin"] = "invalid"
        result["hasVideoUse"] = all(checks[key] == "ready" for key in ("pythonDependencies", "adapter", "sourcePin"))
    if checks["profile"] == "ready":
        required = ["ffmpeg", "ffprobe"]
        if use_legacy:
            required += ["pythonDependencies", "adapter", "sourcePin"]
        if hyperframes:
            required += ["node", "hyperframes", "gsap", "browser", "hyperframesLock"]
        result["status"] = "ready" if not video or all(checks[key] == "ready" for key in required) else "incomplete"
    return result


def challenge_probe(workspace, relative, nonce):
    result = {"status": "failed", "path": relative, "responsePath": relative.rsplit("/", 1)[0] + "/response.json", "nonce": nonce, "requestSHA256": ""}
    if not re.fullmatch(r"outputs/[A-Za-z0-9_-]{1,128}/\.rundesk-probes/[0-9a-f]{24}/request\.json", relative) or not re.fullmatch(r"[0-9a-f]{24}", nonce):
        return result
    descriptors = []
    temporary = "response." + nonce + ".tmp"
    temporary_created = False
    try:
        directory = os.open(workspace, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        descriptors.append(directory)
        for component in relative.split("/")[:-1]:
            directory = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
            descriptors.append(directory)
        fd = os.open("request.json", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > 4096 or info.st_nlink != 1:
                raise ValueError("invalid request file")
            raw = stream.read(4097)
        value = json.loads(raw)
        if len(raw) > 4096 or value != {"schemaVersion": 1, "nonce": nonce} or type(value.get("schemaVersion")) is not int:
            raise ValueError("invalid challenge")
        result["requestSHA256"] = hashlib.sha256(raw).hexdigest()
        response = {"schemaVersion": 1, "nonce": nonce, "requestSHA256": result["requestSHA256"]}
        encoded = (json.dumps(response, separators=(",", ":")) + "\n").encode("utf-8")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
        temporary_created = True
        with os.fdopen(fd, "wb") as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        # Hard-link creation is atomic and refuses an existing/symlink response.
        os.link(temporary, "response.json", src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
        result["status"] = "verified"
    except Exception:
        pass
    finally:
        if temporary_created:
            try:
                os.unlink(temporary, dir_fd=directory)
            except OSError:
                pass
        for fd in reversed(descriptors):
            os.close(fd)
    return result


def image_probe(config, nonce):
    result = {"state": "failed", "nonce": nonce, "code": "mcp_protocol_failed"}
    if not re.fullmatch(r"[0-9a-f]{24}", nonce):
        return result
    try:
        endpoint = config.get("url", "")
        parsed = urllib.parse.urlsplit(endpoint)
        if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.fragment:
            return dict(result, state="unsupported", code="mcp_transport_unsupported")
        headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
        for key, value in config.get("http_headers", {}).items():
            headers[key] = value
        for key, name in config.get("env_http_headers", {}).items():
            if name not in os.environ:
                return dict(result, code="mcp_auth_environment_missing")
            headers[key] = os.environ[name]
        if config.get("bearer_token_env_var"):
            name = config["bearer_token_env_var"]
            if name not in os.environ:
                return dict(result, code="mcp_auth_environment_missing")
            headers["Authorization"] = "Bearer " + os.environ[name]
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, hdrs, newurl):
                return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        def call(message, notification=False):
            body = json.dumps(message, separators=(",", ":")).encode("utf-8")
            request = urllib.request.Request(endpoint, data=body, headers=headers, method="POST")
            with opener.open(request, timeout=3) as response:
                if response.headers.get("Mcp-Session-Id"):
                    headers["Mcp-Session-Id"] = response.headers["Mcp-Session-Id"]
                raw = response.read(2 * 1024 * 1024 + 1)
                if len(raw) > 2 * 1024 * 1024:
                    raise ValueError("oversize")
                if notification and response.status in (200, 202, 204):
                    return None
                if response.headers.get_content_type() != "application/json":
                    raise NotImplementedError("HTTP JSON only")
                value = json.loads(raw)
                if value.get("jsonrpc") != "2.0" or value.get("id") != message.get("id") or "error" in value or not isinstance(value.get("result"), dict):
                    raise ValueError("invalid rpc")
                return value["result"]
        initialized = call({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
            "protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "rundesk-runtime-diagnostics", "version": "1"}}})
        if initialized.get("protocolVersion") not in ("2024-11-05", "2025-03-26", "2025-06-18"):
            return dict(result, state="unsupported", code="mcp_protocol_unsupported")
        headers["MCP-Protocol-Version"] = initialized["protocolVersion"]
        call({"jsonrpc": "2.0", "method": "notifications/initialized"}, notification=True)
        value = call({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {
            "name": "rundesk_diagnostic_image", "arguments": {"nonce": nonce}}})
        if value.get("isError"):
            return dict(result, code="mcp_tool_error")
        images = [block for block in value.get("content", []) if isinstance(block, dict) and block.get("type") == "image"]
        if len(images) != 1 or images[0].get("mimeType") != "image/png":
            return dict(result, code="mcp_image_missing")
        data = base64.b64decode(images[0].get("data", ""), validate=True)
        if len(data) < 33 or len(data) > 1024 * 1024 or data[:8] != b"\x89PNG\r\n\x1a\n" or data[8:16] != b"\x00\x00\x00\rIHDR":
            return dict(result, code="mcp_image_invalid")
        width, height = struct.unpack(">II", data[16:24])
        if not 1 <= width <= 4096 or not 1 <= height <= 4096 or width * height > 4194304:
            return dict(result, code="mcp_image_invalid")
        return {"state": "passed", "nonce": nonce, "imageSHA256": hashlib.sha256(data).hexdigest(), "mimeType": "image/png", "width": width, "height": height}
    except NotImplementedError:
        return dict(result, state="unsupported", code="mcp_transport_unsupported")
    except urllib.error.HTTPError as error:
        return dict(result, code="mcp_redirect_rejected" if 300 <= error.code < 400 else "mcp_http_failed")
    except Exception:
        return result


if __name__ == "__main__":
    # A deployment is Linux; isolated mode avoids workspace Python import hooks.
    signal.alarm(20)
    request = json.loads(sys.stdin.buffer.read(65537))
    answer = {"schemaVersion": 1, "runtime": runtime_probe()}
    if request.get("challenge"):
        answer["challenge"] = challenge_probe(request["workspace"], request["challenge"]["path"], request["nonce"])
    if request.get("mcpConfig"):
        answer["imageTransport"] = image_probe(request["mcpConfig"], request["nonce"])
    print(json.dumps(answer, separators=(",", ":")))
