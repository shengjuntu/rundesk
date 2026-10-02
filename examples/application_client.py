#!/usr/bin/env python3
"""RunDesk v1 client. Persist the operation key before submitting business work.

No automatic write retries. On a timeout, query receipt(key), then retry the
same request/key only as appropriate. completed means HTTP handling completed,
not that the model task completed; inspect receipt.response and session status.
"""
import json
import urllib.request
import urllib.error


class RunDeskError(RuntimeError):
    def __init__(self, status, body):
        super().__init__(body.get('error', f'HTTP {status}'))
        self.status = status
        self.code = body.get('code')
        self.request_id = body.get('requestId')
        self.retryable = body.get('retryable', False)


class RunDesk:
    def __init__(self, base_url='http://127.0.0.1:3210', token='', timeout=30):
        self.base_url = base_url.rstrip('/') + '/api/v1'
        self.token, self.timeout = token, timeout

    def call(self, method, path, body=None, key=None):
        headers = {'Accept': 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        if body is not None:
            headers['Content-Type'] = 'application/json'
        if key:
            headers['Idempotency-Key'] = key
        request = urllib.request.Request(
            self.base_url + path, method=method, headers=headers,
            data=json.dumps(body).encode('utf-8') if body is not None else None)
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                body = json.load(error)
            except (ValueError, UnicodeError):
                body = {'error': f'HTTP {error.code}'}
            raise RunDeskError(error.code, body) from error

    def create_session(self, workspace_id, instance_id, app_id, task_id, key):
        return self.call('POST', '/sessions', {
            'workspaceId': workspace_id, 'instanceId': instance_id,
            'source': {'kind': 'application', 'appId': app_id, 'taskId': task_id},
        }, key=key)

    def start(self, session_id, text, key, files=None, skills=None):
        return self.call('POST', f'/sessions/{session_id}/turns', {
            'text': text, 'files': files or [], 'skills': skills or []}, key=key)

    def receipt(self, key):
        return self.call('GET', '/requests/' + key)

    def session(self, session_id):
        return self.call('GET', '/sessions/' + session_id)

    def events(self, session_id, after=0):
        return self.call('GET', f'/sessions/{session_id}/events?after={int(after)}&limit=1000')

    def stop(self, session_id, run_id):
        return self.call('POST', f'/sessions/{session_id}/stop', {'expectedRunId': run_id})

    def steer(self, session_id, turn_id, text, request_id):
        return self.call('POST', f'/sessions/{session_id}/steer', {
            'text': text, 'expectedTurnId': turn_id, 'requestId': request_id})
