#!/usr/bin/env python3
"""Exercise real Codex configuration with an isolated CODEX_HOME. No inference.
python3 scripts/native-smoke.py --codex /absolute/path/to/codex
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--codex', default='codex')
    parser.add_argument('--binary', default='bin/rundesk')
    parser.add_argument('--require-sandbox', action='store_true', help='Fail if the host cannot start a Codex sandbox')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='rundesk-smoke-', dir=Path.cwd()) as temp:
        root = Path(temp)
        codex_home = root / 'codex-home'
        codex_home.mkdir()
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        env = dict(os.environ, CODEX_HOME=str(codex_home))
        env.pop('RUNDESK_TOKEN', None)
        env.pop('CODEX_BASE_TOKEN', None)
        proc = subprocess.Popen([str(Path(args.binary).resolve()), '--listen', f'127.0.0.1:{port}', '--data', str(root / 'data'), '--codex', args.codex], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        base = f'http://127.0.0.1:{port}/api'

        def call(path, method='GET', value=None):
            request = urllib.request.Request(base + path, data=json.dumps(value).encode() if value is not None else None, method=method, headers={'Content-Type': 'application/json'})
            try:
                with urllib.request.urlopen(request, timeout=60) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                raise RuntimeError(error.read().decode()) from error

        try:
            for _ in range(100):
                try:
                    call('/meta')
                    break
                except (urllib.error.URLError, ConnectionError):
                    time.sleep(0.1)
            workspace = call('/workspaces')[0]
            prefix = '/workspaces/' + workspace['id']
            config = call(prefix + '/config')
            assert 'config' in config
            print('PASS: initialize → config/read')
            skill = '---\nname: smoke-guide\ndescription: A local smoke test skill\n---\n\nOnly inspect the local workspace.\n'
            call(prefix + '/skills/smoke-guide', 'PUT', {'content': skill})
            found = call(prefix + '/skills')
            skills = [s for row in found['data'] for s in row['skills']]
            match = next(s for s in skills if s['name'] == 'smoke-guide')
            call(prefix + '/skills/toggle', 'POST', {'path': match['path'], 'enabled': False})
            disabled = call(prefix + '/skills')
            assert any(not s['enabled'] for row in disabled['data'] for s in row['skills'] if s['name'] == 'smoke-guide')
            print('PASS: skills/list → skills/config/write → reload')
            mcp = call(prefix + '/mcp')
            call(prefix + '/mcp/smoke', 'PUT', {'version': mcp['version'], 'config': {'command': 'disabled-smoke-command', 'enabled': False}})
            mcp = call(prefix + '/mcp')
            assert mcp['userServers']['smoke']['enabled'] is False
            assert not mcp['status'].get('error'), mcp['status']
            call(prefix + '/mcp/smoke', 'PUT', {'version': mcp['version'], 'remove': True})
            print('PASS: MCP config write → reload → status → remove')
            mcp = call(prefix + '/mcp')
            bundle = {'schemaVersion': 1, 'kind': 'rundesk.mcp', 'servers': {'bundle-smoke': {'command': 'unused-test-command', 'enabled': False}}}
            call(prefix + '/mcp/import', 'POST', {'version': mcp['version'], 'bundle': bundle, 'overwrite': False})
            exported = call(prefix + '/mcp/export')
            assert exported['servers']['bundle-smoke']['enabled'] is False
            print('PASS: native MCP bundle import/export')
            removed = call(prefix + '/skills/smoke-guide', 'DELETE')
            assert removed['backupPath']
            assert (Path(workspace['path']) / removed['backupPath']).exists()
            current = call(prefix + '/skills')
            assert not any(s['name'] == 'smoke-guide' for row in current['data'] for s in row['skills'])
            print('PASS: Skill removal and native rescan')
            checks = call(prefix + '/diagnostics')
            for check in checks['checks']:
                if check['name'] == '沙箱实际执行':
                    print('SANDBOX:', json.dumps(check, ensure_ascii=False))
                    if args.require_sandbox:
                        assert check['status'] == 'ok', check
                elif check['name'] != '系统 bubblewrap':
                    assert check['status'] == 'ok', check
            print('PASS: native diagnostic interfaces; sandbox result reported separately')
            # Same workspace, two independent Codex homes; no test service is started.
            instances = [call('/instances', 'POST', {'name': name}) for name in ['native-a', 'native-b']]
            for instance in instances:
                iid = instance['id']
                def scoped(path):
                    return prefix + path + '?instanceId=' + iid
                call(scoped('/skills/instance-guide') + '&scope=instance', 'PUT', {'content': skill.replace('smoke-guide', 'instance-guide')})
                found = call(scoped('/skills'))
                entries = [s for row in found['data'] for s in row['skills'] if s['name'] == 'instance-guide']
                assert len(entries) == 1 and entries[0]['path'].startswith(instance['codexHome']), entries
                mcp = call(scoped('/mcp'))
                assert not mcp['userServers'], mcp
                call(scoped('/mcp/isolated'), 'PUT', {'version': mcp['version'], 'config': {'command': instance['name'], 'enabled': False}})
                assert call(scoped('/account'))['account'] is None
            for instance in instances:
                iid = instance['id']
                mcp = call(prefix + '/mcp?instanceId=' + iid)
                assert mcp['userServers']['isolated']['command'] == instance['name']
            first, second = instances
            path = str(Path(first['codexHome']) / 'skills/instance-guide/SKILL.md')
            call(prefix + '/skills/toggle?instanceId=' + first['id'], 'POST', {'path': path, 'enabled': False})
            for instance in instances:
                entries = [s for row in call(prefix + '/skills?instanceId=' + instance['id'])['data'] for s in row['skills'] if s['name'] == 'instance-guide']
                assert entries[0]['enabled'] == (instance['id'] == second['id'])
            assert 'isolated' not in call(prefix + '/mcp')['userServers']
            call('/instances/' + first['id'] + '/reload', 'POST')
            mcp = call(prefix + '/mcp?instanceId=' + first['id'])
            assert mcp['userServers']['isolated']['command'] == first['name']
            print('PASS: native instance homes, Skills/MCP separation, account/read and idle reload')
            print('No model inference requested. No user Codex configuration modified.')
        finally:
            proc.terminate()
            try:
                _, stderr = proc.communicate(timeout=8)
            except subprocess.TimeoutExpired:
                proc.kill()
                _, stderr = proc.communicate()
            if proc.returncode not in (0, -15):
                print(stderr.decode(errors='replace')[-3000:])


if __name__ == '__main__':
    main()
