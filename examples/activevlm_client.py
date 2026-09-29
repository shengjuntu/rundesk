#!/usr/bin/env python3
"""Minimal server-side ActiveVLM integration using Python's standard library.
RUNDESK_URL=http://127.0.0.1:3210 RUNDESK_TOKEN=... python3 examples/activevlm_client.py
"""
import json
import os
import urllib.request

BASE = os.environ.get('RUNDESK_URL', 'http://127.0.0.1:3210').rstrip('/') + '/api'
HEADERS = {'Content-Type': 'application/json'}
if os.environ.get('RUNDESK_TOKEN'):
    HEADERS['Authorization'] = 'Bearer ' + os.environ['RUNDESK_TOKEN']


def call(path, value=None):
    req = urllib.request.Request(BASE + path, headers=HEADERS, data=json.dumps(value).encode() if value is not None else None)
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


workspace = call('/workspaces')[0]
session = call('/sessions', {'workspaceId': workspace['id'], 'instanceId': os.environ.get('RUNDESK_INSTANCE_ID', 'default'), 'title': 'ActiveVLM demo'})
sid = session['id']
print('Session:', sid)
call(f'/sessions/{sid}/turns', {'text': '简要说明这个工作区的当前状态，先不要修改任何文件。', 'files': [], 'skills': []})
request = urllib.request.Request(BASE + f'/sessions/{sid}/events?stream=1', headers=HEADERS)
with urllib.request.urlopen(request, timeout=120) as stream:
    for line in stream:
        if not line.startswith(b'data: '):
            continue
        event = json.loads(line[6:])
        method = event['method']
        if method == 'item/agentMessage/delta':
            print(event['data']['params']['delta'], end='', flush=True)
        elif method == 'approval/pending':
            print('\nApproval pending; open the admin UI. No automatic approval.')
        elif method == 'run/state':
            print('\nState:', event['data'])
            if event['data']['status'] in ('completed', 'interrupted', 'failed'):
                break
