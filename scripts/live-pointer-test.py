#!/usr/bin/env python3
"""Opt-in deterministic live regression; NOT a model evaluation.
Start interaction-workbench.py with the state path below in a disposable session.
COMPUTER_USE_DISPOSABLE=1 COMPUTER_USE_WORKBENCH_STATE=/.../state.json \
COMPUTER_USE_ARTIFACT_DIR=/.../evidence python scripts/live-pointer-test.py
Uses MCP for all agent operations; a separate local UI connection simulates the
human approver. Evaluator-only state is never supplied to an LLM.
"""
import base64
import json
import os
from pathlib import Path
import socket
import time

if os.environ.get('COMPUTER_USE_DISPOSABLE') != '1':
    raise SystemExit('Refusing: set COMPUTER_USE_DISPOSABLE=1 only in the disposable lab')
state_path = Path(os.environ['COMPUTER_USE_WORKBENCH_STATE'])
out = Path(os.environ['COMPUTER_USE_ARTIFACT_DIR'])
out.mkdir(parents=True, exist_ok=True)
root = Path(os.environ['XDG_RUNTIME_DIR']) / 'computer-use'
trace = []

def ui(q):
    with socket.socket(socket.AF_UNIX) as s:
        s.connect(str(root / 'ui.sock'))
        s.sendall(json.dumps(q).encode() + b'\n')
        return json.loads(s.makefile().readline())

sock = socket.socket(socket.AF_UNIX)
sock.connect(str(root / 'mcp.sock'))
reader = sock.makefile()
seq = 0

def rpc(method, params):
    global seq
    seq += 1
    sock.sendall(json.dumps(dict(jsonrpc='2.0', id=seq, method=method, params=params)).encode() + b'\n')
    while True:
        response = json.loads(reader.readline())
        if response.get('id') == seq:
            return response

def call(name, args):
    response = rpc('tools/call', dict(name=name, arguments=args))
    summary = response.get('result', {}).get('structuredContent')
    trace.append(dict(tool=name, arguments=args, metadata=summary,
                      error=response.get('error'), isError=response.get('result', {}).get('isError', False)))
    return response

def value(response):
    assert 'error' not in response, response
    r = response['result']
    assert not r.get('isError'), r
    return r.get('structuredContent') or json.loads(next(c['text'] for c in r['content'] if c['type'] == 'text'))

def denied(response):
    return bool(response.get('error') or response.get('result', {}).get('isError'))

try:
    rpc('initialize', dict(protocolVersion='2025-06-18', capabilities={}, clientInfo=dict(name='live-pointer-regression', version='1')))
    sock.sendall(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
    ui(dict(op='mode', mode='approve'))
    ui(dict(op='pause', paused=False))
    w = next(w for w in value(call('list_windows', {}))['windows'] if w['title'] == 'Computer-use interaction workbench')
    permission = value(call('request_permission', dict(capability='control', scope=dict(kind='window', id=w['id']), reason='Disposable pointer regression, simulated human approval')))
    if permission['status'] == 'approval_required':
        assert not ui(dict(op='approve', id=permission['request_id'], seconds=120)).get('error')
    # GTK may finish its configure after the compositor move/resize request.
    # Observe the settled geometry; never bless an old action with a new revision.
    time.sleep(.3)
    observed = value(call('view_window', dict(window_id=w['id'])))
    assert observed['logical_size'] == [1800, 880], observed
    base = dict(window_id=w['id'], revision=observed['revision'])
    actions = [
        dict(type='click', x=300, y=190, modifiers=['CTRL']),
        dict(type='scroll', x=900, y=190, delta_x=1, delta_y=0, unit='wheel_steps'),
        dict(type='click', x=300, y=375, click_count=2),
        dict(type='click', x=900, y=375, click_count=3),
        dict(type='drag', path=[dict(x=110, y=690), dict(x=570, y=580), dict(x=1090, y=690)]),
        dict(type='click', x=1400, y=412), dict(type='text', text='K7M2R9'),
    ]
    result = value(call('input_window', dict(base, actions=actions, then='screenshot',
        observation=dict(region=dict(x=1240, y=220, width=340, height=100), max_width=1360, max_height=400))))
    assert result['status'] == 'completed' and result['observation']['status'] == 'ok', result
    transform = result['observation']['image_to_window']
    assert transform['offset_x'] == 1240 and transform['offset_y'] == 220
    time.sleep(.4)
    fixture = json.loads(state_path.read_text())
    assert all(fixture['checks'].values()), fixture['checks']
    assert any(e['event'] == 'button' and e['state'] & 4 for e in fixture['events'])
    assert all(not e['state'] & 4 for e in fixture['events'] if e['event'] == 'button' and e['y'] > 300), 'modifier leaked into later gestures'
    assert any(e['event'] == 'stroke' and len(e['points']) >= 3 and e['passed'] for e in fixture['events'])
    for unit,dx,dy in [('logical_pixels',24,-12), ('wheel_steps',-2,3)]:
        assert value(call('input_window', dict(base, actions=[dict(type='scroll',x=900,y=190,delta_x=dx,delta_y=dy,unit=unit)])))['status'] == 'completed'
    time.sleep(.2)
    scrolls = [e for e in json.loads(state_path.read_text())['events'] if e['event'] == 'scroll']
    assert any(e['dx'] > 0 and e['dy'] < 0 for e in scrolls), scrolls
    assert any(e['dx'] < 0 and e['dy'] > 0 for e in scrolls), scrolls
    count = len(json.loads(state_path.read_text())['events'])
    for bad in [dict(type='click',y=10),dict(type='click',x=1,y=1,modifiers=['SUPER']),
                dict(type='drag',path=[dict(x=10,y=10),dict(x=w['size'][0]+1,y=20)]),
                dict(type='scroll',x=1,y=1,delta_x=1,delta_y=0,unit='bogus')]:
        assert denied(call('input_window',dict(base,actions=[dict(type='click',x=300,y=190),bad])))
    assert denied(call('input_window',dict(base,revision='stale',actions=actions)))
    time.sleep(.2)
    assert len(json.loads(state_path.read_text())['events']) == count, 'rejected batch delivered events'
    response = call('view_window',dict(window_id=w['id'],max_width=1800,max_height=1000))
    value(response)
    for content in response['result']['content']:
        if content['type'] == 'image':
            (out/'after.png').write_bytes(base64.b64decode(content['data']))
    mode = value(call('computer_status', {}))['input_mode']
    targets = ui(dict(op='state')).get('targets')
    ui(dict(op='revoke_all'))
    assert value(call('input_window',dict(base,actions=actions)))['status'] == 'approval_required'
    ui(dict(op='pause',paused=True))
    assert denied(call('view_window',dict(window_id=w['id'])))
    print(json.dumps(dict(status='PASS',input_mode=mode,checks=fixture['checks'],targets=targets),indent=2))
finally:
    reader.close()
    sock.close()
    ui(dict(op='revoke_all'))
    ui(dict(op='pause',paused=True))
    (out/'trace.json').write_text(json.dumps(trace,indent=2))
