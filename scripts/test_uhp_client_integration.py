#!/usr/bin/env python3
r"""Exercise the stdlib client against a compiled RunDesk demo service (Python 3.10+).

Build RunDesk separately, then run from any directory:
  python3 scripts/test_uhp_client_integration.py --binary /path/to/rundesk
  python3 scripts/test_uhp_client_integration.py --binary /path/to/rundesk \
    --output-dir /path/to/check-results

This always launches --demo on an ephemeral loopback port with a temporary HOME,
config, data and generated test credential. It cannot target a live service. No
real model credentials are inherited, no approvals are granted, and the service
and temporary data are removed on exit. Optional output includes a JSON report
and complete test/server logs with the test token and temporary paths redacted.
The native agent is the built-in protocol simulator, not a live model.
"""

import argparse
from contextlib import contextmanager
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import tempfile
import time
import traceback
import urllib.parse


ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    with path.open('rb') as source:
        checksum = hashlib.sha256()
        for chunk in iter(lambda: source.read(1 << 20), b''):
            checksum.update(chunk)
        return checksum.hexdigest()


def timestamp():
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())


@contextmanager
def demo_service(binary, temp, token, log_path):
    """Keep all service state isolated and stop descendants before deleting it."""
    env = {'PATH': os.environ.get('PATH', os.defpath), 'HOME': str(temp),
           'XDG_CONFIG_HOME': str(temp), 'RUNDESK_TOKEN': token,
           'CODEX_BASE_TOKEN': '', 'CODEX_HOME': str(temp / 'codex')}
    # Preserve Windows runtime essentials, never provider credentials/config.
    for key in ('SYSTEMROOT', 'WINDIR'):
        if key in os.environ:
            env[key] = os.environ[key]
    with log_path.open('w', encoding='utf-8') as server_log:
        proc = subprocess.Popen(
            [str(binary), '--demo', '--data', str(temp / 'data'),
             '--listen', '127.0.0.1:0'], env=env, stdout=server_log,
            stderr=subprocess.STDOUT, start_new_session=(os.name == 'posix'))
        try:
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                if proc.poll() is not None:
                    raise RuntimeError('RunDesk exited during startup')
                found = re.search(r'RunDesk (http://127\.0\.0\.1:\d+)',
                                  log_path.read_text(encoding='utf-8'))
                if found:
                    yield found[1]
                    return
                time.sleep(.05)
            raise RuntimeError('RunDesk did not log a listening address within 15 seconds')
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
            # Also remove any orphaned demo child after a failed service shutdown.
            if os.name == 'posix':
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass


def exercise(module, base, temp, token, report, emit):
    client = module.UHPClient(base, timeout=15)

    def passed(name, **details):
        check = {'name': name, 'result': 'passed', **details}
        report['checks'].append(check)
        emit('check', **check)

    def rdca(path):
        # A direct loopback connection does not follow authentication redirects.
        address = urllib.parse.urlsplit(base)
        connection = http.client.HTTPConnection(address.hostname, address.port, timeout=10)
        try:
            connection.request('GET', '/api/v1' + path,
                               headers={'Authorization': 'Bearer ' + token})
            response = connection.getresponse()
            assert response.status == 200, f'RDCA returned HTTP {response.status}'
            return json.loads(response.read())
        finally:
            connection.close()

    discovery = client.discovery()
    assert discovery['default_version'] == module.PROTOCOL_VERSION
    assert discovery['capabilities']['streaming'] is True
    report['serverVersion'] = discovery['implementation']['version']
    passed('authenticated discovery and fixed UHP protocol', version=discovery['default_version'])

    harnesses = client.harnesses()['harnesses']
    assert harnesses
    harness = next(h for h in harnesses if h['base'] == 'codex')
    hid = harness['id']
    assert harness['rundesk_capabilities']['timeout_seconds'] is True
    assert harness['rundesk_capabilities']['max_step'] is False
    assert harness['rundesk_capabilities']['native_instructions'] is False
    models = client.models(hid)
    assert any(m['id'] == 'demo-fixture' and m['available'] for m in models['models'])
    passed('configured harness capabilities and native demo model discovery', harness=hid)

    before = rdca('/tasks')
    strict = {'input': 'should be refused', 'metadata': {'harness_id': hid, 'rundesk_require': ['max_step']}, 'max_step': 1}
    try:
        client.submit(strict, operation_key='saved-client-strict-refusal')
        raise AssertionError('Unsupported capability was accepted')
    except module.UHPError as error:
        assert error.status == 422 and error.code == 'rundesk_capability_unsupported', str(error)
    assert rdca('/tasks') == before
    passed('server rejects unsupported strict capability before creating tasks')

    upload_path = Path(temp) / 'materials.txt'
    uploaded_bytes = 'Local integration attachment: exact bytes.\n材料\n'.encode('utf-8')
    upload_path.write_bytes(uploaded_bytes)
    uploaded = client.upload(upload_path)
    assert uploaded['bytes'] == len(uploaded_bytes)
    passed('multipart file upload through actual HTTP', bytes=len(uploaded_bytes), file_id=uploaded['id'])

    request = {'input': [{'role': 'user', 'content': [{'type': 'input_text', 'text': '检查附件并生成演示报告'},
                 {'type': 'input_file', 'file_id': uploaded['id']}]}],
               'metadata': {'harness_id': hid}, 'model': 'demo-fixture', 'timeout_seconds': 30}
    response = client.submit(request, operation_key='saved-client-uploaded-task', required_capabilities=['input_file', 'timeout_seconds', 'progress'])
    rid = response['id']
    assert response['status'] == 'in_progress'
    assert response['metadata']['rundesk_progress']['state'] == 'queued'
    assert response['metadata']['rundesk_execution']['timeout_seconds']['enforcement'] == 'task_deadline'
    passed('background submission preserves required capability and execution metadata', response_id=rid)

    stream = client.events(rid)
    early = []
    for _ in range(5):
        early.append(next(stream))
    stream.close()
    interim = client.read(rid)
    assert interim['status'] == 'in_progress'
    assert interim['metadata']['rundesk_progress']['state'] in {'running', 'dispatching'}
    passed('early SSE disconnect leaves task running', early_event_ids=[event_id for event_id, _ in early])
    resumed = list(client.events(rid, last_event_id=int(early[-1][0])))
    all_events = early + resumed
    ids = [int(i) for i, _ in all_events]
    assert ids == list(range(len(ids))), ids
    assert all(int(event_id) == data['sequence_number'] for event_id, data in all_events)
    final = client.read(rid)
    assert final['status'] == 'completed'
    assert final['metadata']['rundesk_progress']['state'] == 'completed'
    assert resumed[-1][1]['response']['status'] == 'completed'
    emit('response', data=final)
    passed('SSE Last-Event-ID resume is gapless and reaches the same terminal response', events=len(all_events))

    replayed = client.submit(request, operation_key='saved-client-uploaded-task', required_capabilities=['input_file', 'timeout_seconds', 'progress'])
    assert replayed['id'] == rid
    assert replayed['status'] == 'completed'
    passed('idempotent submission replay preserves response identity and completed result')

    session_id = final['metadata']['session_id']
    files = client.files(session_id)['files']
    assert files, final
    artifact = next(f for f in files if f['filename'] == 'demo-report.md')
    target = Path(temp) / 'downloaded-report.md'
    client.download(artifact['container_id'], artifact['id'], target)
    expected = '# RunDesk 演示产物\n\n此文件由协议模拟器生成，用于验证文件展示与下载。\n'.encode('utf-8')
    assert target.read_bytes() == expected
    passed('actual session artifact discovery and exact-byte download', bytes=len(expected), sha256=hashlib.sha256(expected).hexdigest())

    continuation = client.submit({'input': '继续同一会话，完成第二个任务', 'previous_response_id': rid,
                                  'metadata': {'harness_id': hid}, 'timeout_seconds': 30},
                                 operation_key='saved-client-continuation')
    continued = client.wait(continuation['id'], wait_seconds=15, poll_interval=.05)
    assert continued['id'] != rid and continued['status'] == 'completed'
    assert continued['metadata']['session_id'] == session_id
    assert client.read(rid)['metadata']['rundesk_progress']['state'] == 'completed'
    passed('continuation uses the same session and a distinct task; prior progress remains frozen')

    approval = client.submit({'input': '审批演示，请等待人工处理', 'metadata': {'harness_id': hid}, 'timeout_seconds': 30},
                             operation_key='saved-client-approval')
    waiting = client.wait(approval['id'], wait_seconds=15, poll_interval=.05)
    progress = waiting['metadata']['rundesk_progress']
    assert waiting['status'] == 'in_progress' and progress['state'] == 'waiting_approval', waiting
    assert progress['required_action']['type'] == 'approval'
    assert progress['required_action']['workbench_url'].startswith('/?session=')
    assert 'command' not in json.dumps(progress)
    passed('polling observes real stored approval and returns a workbench handoff', action=progress['required_action'])
    client.cancel(approval['id'])
    cancelled = client.wait(approval['id'], wait_seconds=10, poll_interval=.05, stop_for_action=False)
    assert cancelled['status'] == 'cancelled'
    assert cancelled['metadata']['rundesk_progress']['state'] == 'cancelled'
    assert 'required_action' not in cancelled['metadata']['rundesk_progress']
    passed('explicit cancellation stops the waiting task and clears its required action')

    timeout = client.submit({'input': '审批直到超时', 'metadata': {'harness_id': hid}, 'timeout_seconds': 1},
                            operation_key='saved-client-timeout')
    expired = client.wait(timeout['id'], wait_seconds=10, poll_interval=.05, stop_for_action=False)
    assert expired['status'] == 'incomplete' and expired['incomplete_details']['reason'] == 'timeout', expired
    passed('task deadline expires across approval wait and reports incomplete')

    disclosure = client.submit({'input': '报告明确忽略的预算', 'metadata': {'harness_id': hid},
                                'max_step': 1, 'max_output_tokens': 4, 'timeout_seconds': 10},
                               operation_key='saved-client-budget-disclosure')
    assert set(disclosure['metadata']['ignored_fields']) == {'max_step', 'max_output_tokens'}
    disclosed = client.wait(disclosure['id'], wait_seconds=15, poll_interval=.05)
    assert disclosed['status'] == 'completed'
    passed('unsupported budgets are explicitly disclosed rather than claimed as enforced')

    cli_env = {'UHP_API_KEY': token, 'PYTHONDONTWRITEBYTECODE': '1',
               'HOME': str(temp), 'NO_PROXY': '127.0.0.1', 'no_proxy': '127.0.0.1'}
    for key in ('SYSTEMROOT', 'WINDIR'):
        if key in os.environ:
            cli_env[key] = os.environ[key]
    cli = subprocess.run([sys.executable, str(ROOT / 'examples/uhp_client.py'), '--url', base, 'harnesses'],
                         env=cli_env, text=True, capture_output=True, timeout=10)
    assert cli.returncode == 0 and hid in cli.stdout and token not in cli.stdout + cli.stderr
    passed('CLI reaches the actual service without revealing token')

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--binary', required=True, type=Path, help='Previously built RunDesk executable')
    parser.add_argument('--output-dir', type=Path, help='Save client-integration.json and test/server logs here')
    args = parser.parse_args()
    if not __debug__:
        parser.error('Run without -O: integration assertions must remain enabled')
    binary = args.binary.expanduser().resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error('--binary must name an executable RunDesk file')
    # Import the repository client without generating files in its source tree.
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location('uhp_client', ROOT / 'examples/uhp_client.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    report = {
        'scope': 'Python UHP client against an actual compiled RunDesk HTTP service, SQLite and subprocess native demo JSONL simulator',
        'startedAt': timestamp(), 'binarySHA256': digest(binary),
        'clientSHA256': digest(ROOT / 'examples/uhp_client.py'),
        'testSHA256': digest(Path(__file__)),
        'checks': [], 'passed': False,
        'limits': [
            'The native backend is RunDesk explicit demo protocol simulation; no live provider or real Codex/Kun model calls.',
            'No Docker, real model authentication, visual quality or business task acceptance.',
            'Uploaded bytes were admitted/materialized; the simulator does not semantically read the attachment.',
            'The generated artifact is a deterministic simulator file, not an actual model-created report.',
            'Approval was observed then cancelled; no operation was automatically approved.',
        ],
    }
    test_log = io.StringIO()
    token = secrets.token_urlsafe(36)
    previous_env = {key: os.environ.get(key) for key in ('UHP_API_KEY', 'NO_PROXY', 'no_proxy')}
    with tempfile.TemporaryDirectory(prefix='rundesk-client-http-') as directory:
        temp = Path(directory)
        server_log_path = temp / 'server.log'

        def redact(value):
            return value.replace(token, '[test-token]').replace(directory, '[temporary-data]')

        def emit(kind, **details):
            test_log.write(redact(json.dumps({'kind': kind, **details}, ensure_ascii=False)) + '\n')

        try:
            os.environ['UHP_API_KEY'] = token
            os.environ['NO_PROXY'] = os.environ['no_proxy'] = '127.0.0.1'
            with demo_service(binary, temp, token, server_log_path) as base:
                exercise(module, base, temp, token, report, emit)
            report['passed'] = True
        except (Exception, KeyboardInterrupt) as error:
            report['failure'] = {'type': type(error).__name__, 'message': redact(str(error))}
            emit('failure', **report['failure'])
            test_log.write(redact(traceback.format_exc()))
        finally:
            for key, value in previous_env.items():
                if value is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = value
        report['finishedAt'] = timestamp()
        report['checksPassed'] = len(report['checks'])
        report_text = redact(json.dumps(report, ensure_ascii=False, indent=2)) + '\n'
        server_text = redact(server_log_path.read_text(encoding='utf-8', errors='replace')) if server_log_path.exists() else ''
        if args.output_dir:
            args.output_dir.mkdir(parents=True, exist_ok=True)
            for name, content in (
                ('client-integration.json', report_text),
                ('client-integration.log', test_log.getvalue()),
                ('client-integration-server.log', server_text),
            ):
                (args.output_dir / name).write_text(content, encoding='utf-8')
        if not report['passed']:
            print(test_log.getvalue(), file=sys.stderr, end='')
            print(server_text, file=sys.stderr, end='')
    print(json.dumps({'passed': report['passed'], 'checks': report['checksPassed'],
                      'failure': report.get('failure')}, ensure_ascii=False))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
