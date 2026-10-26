#!/usr/bin/env python3
"""Real Video App HTTP/MCP + bridge CLI/stdio; local deterministic inference fixtures only."""
import argparse
import hashlib
import io
import json
import math
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import threading
import time
import uuid
import wave
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def http(method, url, token, body=None):
    request = Request(url, data=None if body is None else json.dumps(body).encode(), method=method,
                      headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        with urlopen(request, timeout=30) as response:
            raw = response.read()
            return response.status, json.loads(raw) if response.headers.get('Content-Type', '').startswith('application/json') else raw
    except HTTPError as error:
        return error.code, error.read().decode(errors='replace')


def png():
    def chunk(kind, data):
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data) & 0xffffffff)
    pixels = b''.join(b'\0' + b'\x28\x78\xc8' * 16 for _ in range(16))
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 16, 16, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(pixels)) + chunk(b'IEND', b'')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app-root', type=Path)
    parser.add_argument('--video-app', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    binary = (args.video_app or (args.app_root / 'bin/video-app' if args.app_root else root.parent.parent.parent / 'project/rva-merged/bin/video-app')).resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    data = output / 'app-data'
    if data.exists():
        raise RuntimeError('Use a fresh output directory; existing app-data is never overwritten')
    bridge = root / 'examples/_shared/video_app_media.py'
    version = subprocess.check_output([str(binary), '--version'], text=True).strip()
    assert '0.26.0' in version, version
    wav = io.BytesIO()
    with wave.open(wav, 'wb') as audio:
        audio.setnchannels(1); audio.setsampwidth(2); audio.setframerate(24000)
        audio.writeframes(b''.join(struct.pack('<h', int(8000 * math.sin(2 * math.pi * 440 * n / 24000))) for n in range(24000)))
    video = output / 'fixture.mp4'
    subprocess.run(['ffmpeg', '-v', 'error', '-y', '-f', 'lavfi', '-i', 'testsrc2=size=160x90:rate=10:duration=1', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', str(video)], check=True)
    media = {'/v1/audio/speech': ('audio/wav', wav.getvalue()), '/image': ('image/png', png()), '/video': ('video/mp4', video.read_bytes())}
    counts = {path: 0 for path in media}
    errors = []
    lock = threading.Lock()
    class Fixture(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass
        def do_POST(self):
            try:
                body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))))
                assert self.path in media
                assert self.headers.get('Authorization') == 'Bearer fixture-provider-secret'
                if self.path == '/v1/audio/speech':
                    assert body['input'] == 'Independent narration for upstream editing.'
                    assert body['voice'] == 'fixture-voice' and body['response_format'] == 'wav'
                else:
                    assert body['schema'] == 'video.generation-request.v1'
                    assert self.headers.get('Idempotency-Key') == body['requestId']
                with lock:
                    counts[self.path] += 1
                mime, payload = media[self.path]
                self.send_response(200); self.send_header('Content-Type', mime); self.send_header('Content-Length', str(len(payload))); self.end_headers(); self.wfile.write(payload)
            except Exception as error:
                errors.append(str(error)); self.send_error(500)
    fixture = ThreadingHTTPServer(('127.0.0.1', 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    provider = 'http://127.0.0.1:' + str(fixture.server_port)
    app_url = 'http://127.0.0.1:' + str(free_port())
    token = uuid.uuid4().hex + uuid.uuid4().hex
    env = dict(os.environ, VIDEO_APP_TOKEN=token, VIDEO_TTS_BACKEND='indextts', VIDEO_TTS_URL=provider + '/v1/audio/speech', VIDEO_TTS_MODEL='fixture-index-tts', VIDEO_TTS_VOICE='fixture-voice', VIDEO_TTS_LANGUAGE='en', VIDEO_TTS_API_KEY='fixture-provider-secret', VIDEO_IMAGE_GEN_URL=provider + '/image', VIDEO_IMAGE_GEN_MODEL='fixture-image', VIDEO_IMAGE_GEN_KEY='fixture-provider-secret', VIDEO_VIDEO_GEN_URL=provider + '/video', VIDEO_VIDEO_GEN_MODEL='fixture-video', VIDEO_VIDEO_GEN_KEY='fixture-provider-secret')
    for key in ('VIDEO_PRODUCTION_PROFILE', 'VIDEO_APP_PUBLIC_URL', 'VIDEO_APP_MCP_URL'):
        env.pop(key, None)
    checks = []
    process = None
    report = {'passed': False, 'scope': 'real Go Video App process, HTTP/MCP, durable jobs, media validation/archive and authenticated download; local deterministic inference fixtures', 'actualModelTested': False, 'realDockerTested': False, 'runDeskServerUsed': False, 'binaryVersion': version, 'checks': checks}
    try:
        with (output / 'video-app.log').open('w') as log:
            process = subprocess.Popen([str(binary), '--data', str(data), '--listen', app_url.removeprefix('http://'), '--rundesk', 'http://127.0.0.1:1'], env=env, stdout=log, stderr=subprocess.STDOUT)
            deadline = time.monotonic() + 15
            while True:
                try:
                    if http('GET', app_url + '/api/health', token)[0] == 200:
                        break
                except OSError:
                    pass
                if process.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError('Video App did not become healthy; inspect video-app.log')
                time.sleep(.05)
            def call(method, path, body=None):
                status, result = http(method, app_url + path, token, body)
                assert 200 <= status < 300, (status, result)
                return result
            project = call('POST', '/api/projects', {'name': 'Independent upstream media integration', 'requestId': uuid.uuid4().hex[:24]})
            pid = project['id']; route = '/api/projects/' + pid
            project['shots'] = [{'id': uuid.uuid4().hex[:24], 'title': 'Original shot', 'text': 'Keep original shot text.', 'duration': 2, 'assetId': ''}]
            project = call('PUT', route, project)
            project = call('PUT', route + '/narration', {'revision': project['revision'], 'text': 'Keep the saved editorial narration unchanged.'})
            dsl = {'version': 1, 'canvas': {'width': 320, 'height': 180, 'fps': 10}, 'tracks': [{'id': 'titles', 'kind': 'text'}], 'clips': [{'id': 'original_title', 'track': 'titles', 'start': 0, 'duration': 2, 'text': 'Original title', 'fontSize': 24}]}
            call('PUT', route + '/edit', {'revision': project['revision'], 'dsl': dsl})
            before = call('GET', route)
            protected = {key: before.get(key) for key in ('narrationText', 'shots', 'editDsl', 'editActive', 'audio', 'subtitles', 'hyperframes', 'workflow')}
            bridge_env = dict(env, VIDEO_APP_MCP_URL=app_url + '/api/mcp', VIDEO_APP_PROJECT_ID=pid, VIDEO_APP_OUTPUT_ROOT=str(output))
            def cli(arguments, expected=0):
                result = subprocess.run([sys.executable, str(bridge), *arguments], env=bridge_env, cwd=output, capture_output=True, text=True, timeout=45)
                assert result.returncode == expected, (arguments, result.returncode, result.stdout, result.stderr)
                return json.loads(result.stdout if expected == 0 else result.stderr)
            tts_id = uuid.uuid4().hex[:24]
            tts_args = ['tts', '--text', 'Independent narration for upstream editing.', '--revision', str(before['revision']), '--request-id', tts_id, '--output', 'downloaded/narration.wav', '--poll-interval', '.05', '--wait-timeout', '30']
            first = cli(tts_args); repeat = cli(tts_args)
            assert first['job']['jobId'] == repeat['job']['jobId'] and counts['/v1/audio/speech'] == 1
            audio_file = Path(first['files'][0]['path'])
            with wave.open(str(audio_file)) as audio:
                assert audio.getframerate() == 24000 and audio.getnframes() > 0 and any(audio.readframes(audio.getnframes()))
            assert hashlib.sha256(audio_file.read_bytes()).hexdigest() == first['files'][0]['sha256']
            after = call('GET', route)
            assert {key: after.get(key) for key in protected} == protected
            assert after['revision'] == before['revision']
            checks.append('literal_text_tts_real_job_wav_archive_download_and_identical_retry_one_inference_without_editor_changes')
            for command, path, name in [('image', '/image', 'image.png'), ('video', '/video', 'clip.mp4')]:
                current = call('GET', route)
                result = cli([command, '--prompt', 'Deterministic fixture asset.', '--revision', str(current['revision']), '--request-id', uuid.uuid4().hex[:24], '--output', 'downloaded/' + name, '--poll-interval', '.05', '--wait-timeout', '30'])
                assert result['job']['state'] == 'succeeded' and counts[path] == 1
                local = Path(result['files'][0]['path']); assert local.read_bytes() == media[path][1]
                archived = call('GET', route)
                assert any(asset.get('generated', {}).get('provider') == 'http_binary_v1' and asset['generated'].get('jobId') == result['job']['jobId'].removeprefix('generation:') for asset in archived['assets'])
                checks.append(command + '_real_job_validated_import_provenance_and_authenticated_download')
            forbidden = cli(['call', 'video_job_start', '--arguments', json.dumps({'type': 'render', 'revision': before['revision'], 'requestId': uuid.uuid4().hex[:24]})], 2)
            assert 'allowed' in forbidden['error']
            other = call('POST', '/api/projects', {'name': 'Other project', 'requestId': uuid.uuid4().hex[:24]})['id']
            forbidden = cli(['call', 'video_project_get', '--arguments', json.dumps({'projectId': other})], 2)
            assert 'configured project' in forbidden['error']
            checks.append('cli_rejects_non_generation_type_and_foreign_project')
            requests = [{'jsonrpc': '2.0', 'id': 1, 'method': 'initialize'}, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'}, {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call', 'params': {'name': 'video_job_start', 'arguments': {'type': 'render'}}}, {'jsonrpc': '2.0', 'id': 4, 'method': 'tools/call', 'params': {'name': 'video_project_get', 'arguments': {'projectId': other}}}]
            stdio = subprocess.run([sys.executable, str(bridge), 'serve-mcp'], input=''.join(json.dumps(row) + '\n' for row in requests), env=bridge_env, cwd=output, capture_output=True, text=True, timeout=30)
            assert stdio.returncode == 0, stdio.stderr
            responses = [json.loads(line) for line in stdio.stdout.splitlines()]
            tools = responses[1]['result']['tools']; names = {tool['name'] for tool in tools}
            assert 'video_media_download' in names and 'video_job_start' in names and 'video_project_update' not in names and 'video_hyperframes_update' not in names
            assert responses[2]['result']['isError'] and responses[3]['result']['isError']
            checks.append('stdio_real_app_tool_discovery_and_generation_only_project_scoped_rejections')
            assert http('GET', first['files'][0]['url'], '')[0] == 401
            assert http('GET', app_url + route, 'incorrect-fixture-token')[0] == 401
            checks.append('real_app_rejects_unauthenticated_artifact_and_wrong_bearer_token')
            final = call('GET', route)
            assert {key: final.get(key) for key in protected} == protected and not errors
            assert (data / 'projects' / pid / 'project.json').is_file()
            report.update(passed=True, checkCount=len(checks), inferenceFixtureCalls=counts, fixtureErrors=errors,
                          projectId=pid, downloadedFiles=[str(path.relative_to(output)) for path in (output / 'downloaded').iterdir()], editorialFieldsPreserved=list(protected), appArchivePresent=(data / 'projects' / pid / 'project.json').is_file())
    except Exception as error:
        report['error'] = str(error).replace(token, '[redacted]')
        raise
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill(); process.wait()
        fixture.shutdown(); fixture.server_close()
        (output / 'integration.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
