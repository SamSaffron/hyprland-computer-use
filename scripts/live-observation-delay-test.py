#!/usr/bin/env python3
"""Opt-in real-application screenshot timing test; no model or fake app delays.
Runs a private Mousepad instance in a disposable Hyprland session. All editor
input and pixels use actual MCP. Only setup and a simulated local approver use
an evaluator channel. Set COMPUTER_USE_DISPOSABLE=1 and CU_DELAY_EVIDENCE.
CU_DELAY_VALUES=0 for old-binary reproduction; default 0,200 for paired trials.
Score downloaded images with: python scripts/live-observation-delay-test.py --score DIR
Scoring needs tesseract; warmups are excluded. Inspect images if OCR is ambiguous.
"""
import base64,json,os,socket,subprocess,time,sys,statistics
from pathlib import Path
def score(directory):
 rows=[]
 for path in sorted(directory.glob('trial-*/result.json')):
  data=json.loads(path.read_text())
  row={key:data[key] for key in ['trial','delay_ms','elapsed_seconds']}
  row['folder']=path.parent.name
  for kind in ['returned','reference']:
   text=subprocess.check_output(['tesseract',str(path.parent/(kind+'.png')),'stdout'],stderr=subprocess.DEVNULL,text=True)
   (path.parent/(kind+'.txt')).write_text(text)
   row[kind+'_fresh']=all(line in ' '.join(text.split()) for line in data['expected'].strip().splitlines())
   row[kind+'_ocr']=text
  rows.append(row)
 if not rows:raise SystemExit('No scored trials found')
 (directory/'scored.json').write_text(json.dumps(rows,indent=2))
 for delay in sorted({r['delay_ms'] for r in rows}):
  arm=[r for r in rows if r['delay_ms']==delay]
  print(f"{delay}ms: returned fresh {sum(r['returned_fresh'] for r in arm)}/{len(arm)}; reference correct {sum(r['reference_fresh'] for r in arm)}/{len(arm)}; median {statistics.median(r['elapsed_seconds'] for r in arm)*1000:.1f}ms")
 if not all(r['reference_fresh'] for r in rows):raise SystemExit('Invalid comparison: settled reference failed OCR; inspect the actual images')

if len(sys.argv)==3 and sys.argv[1]=='--score':
 score(Path(sys.argv[2]));raise SystemExit(0)

if os.environ.get('COMPUTER_USE_DISPOSABLE')!='1':raise SystemExit('Disposable lab only: set COMPUTER_USE_DISPOSABLE=1')
out=Path(os.environ['CU_DELAY_EVIDENCE']).resolve();out.mkdir(parents=True,exist_ok=True)
root=Path(os.environ['XDG_RUNTIME_DIR'])/'computer-use'
delays=[int(s) for s in os.environ.get('CU_DELAY_VALUES','0,200').split(',')]
repeats=int(os.environ.get('CU_DELAY_REPEATS','10'))
def ui(q):
 with socket.socket(socket.AF_UNIX) as s:
  s.connect(str(root/'ui.sock'));s.sendall(json.dumps(q).encode()+b'\n');return json.loads(s.makefile().readline())
s=socket.socket(socket.AF_UNIX);s.connect(str(root/'mcp.sock'));reader=s.makefile();seq=0;trace=[]
def rpc(method,params):
 global seq
 seq+=1;s.sendall(json.dumps(dict(jsonrpc='2.0',id=seq,method=method,params=params)).encode()+b'\n')
 while True:
  r=json.loads(reader.readline())
  if r.get('id')==seq:return r

def call(name,args):
 r=rpc('tools/call',dict(name=name,arguments=args))
 if 'error' in r:raise RuntimeError(r['error'])
 r=r['result'];meta=r.get('structuredContent') or json.loads(next(c['text'] for c in r['content'] if c['type']=='text'))
 trace.append(dict(name=name,arguments=args,metadata=meta,isError=r.get('isError',False)))
 if r.get('isError'):raise RuntimeError(meta)
 return meta,r

def save_png(r,path):
 image=next(c for c in r['content'] if c['type']=='image');path.write_bytes(base64.b64decode(image['data']))

original=ui({'op':'state'});app=None
try:
 rpc('initialize',dict(protocolVersion='2025-06-18',capabilities={},clientInfo=dict(name='mousepad-observation-delay-test',version='1')))
 s.sendall(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
 tools=rpc('tools/list',{})['result'];(out/'tools.json').write_text(json.dumps(tools,indent=2))
 if any(delays) and 'delay_ms' not in json.dumps(tools):raise RuntimeError('broker does not advertise observation.delay_ms')
 note=out/'release-note.txt';note.write_text('Draft release note\nAwaiting review.\n')
 app=subprocess.Popen(['mousepad','--disable-server',str(note)],stdout=open(out/'mousepad.log','w'),stderr=subprocess.STDOUT)
 w=None
 for _ in range(60):
  windows=call('list_windows',{})[0]['windows'];w=next((w for w in windows if 'release-note.txt' in w['title']),None)
  if w:break
  time.sleep(.1)
 if not w:raise RuntimeError('Mousepad window not found')
 clients=json.loads(subprocess.check_output(['hyprctl','clients','-j']));address=next(c['address'] for c in clients if c['pid']==app.pid)
 for action in [f'hl.dispatch(hl.dsp.window.float({{window="address:{address}"}}))',f'hl.dispatch(hl.dsp.window.resize({{window="address:{address}",x=1100,y=700}}))',f'hl.dispatch(hl.dsp.window.move({{window="address:{address}",x=60,y=120}}))']:
  subprocess.run(['hyprctl','eval',action],check=True,stdout=subprocess.DEVNULL)
 if original.get('paused'):ui({'op':'pause','paused':False})
 permission=call('request_permission',dict(capability='control',scope=dict(kind='window',id=w['id']),reason='Disposable real Mousepad before/after test; simulated local approval'))[0]
 if permission['status']=='approval_required':
  result=ui(dict(op='approve',id=permission['request_id'],seconds=1800))
  if result.get('error'):raise RuntimeError(result)
 time.sleep(.5)
 meta,r=call('view_window',dict(window_id=w['id']));save_png(r,out/'initial.png')
 base=dict(window_id=w['id'],revision=meta['revision'])
 # Focus only the editor body through the scoped MCP input path.
 call('input_window',dict(base,actions=[dict(type='click',x=200,y=180)]));time.sleep(.2)
 results=[]
 for trial in range(-2,repeats):
  order=delays if trial%2==0 else list(reversed(delays))
  for delay in order:
   label=f'{"warmup" if trial<0 else "trial"}-{abs(trial):02}-{delay}ms'
   folder=out/label;folder.mkdir(exist_ok=True)
   call('input_window',dict(base,actions=[dict(type='key',key='CTRL+A'),dict(type='text',text='Draft release note\nAwaiting review.\n')]))
   time.sleep(.4)
   initial,r=call('view_window',dict(window_id=w['id']));save_png(r,folder/'before.png')
   expected=f'Release ready round {abs(trial):02}\nAll checks passed. Ship the update.\n'
   args=dict(base,actions=[dict(type='key',key='CTRL+A'),dict(type='text',text=expected)],then='screenshot')
   if delay:args['observation']={'delay_ms':delay}
   started=time.monotonic();result,r=call('input_window',args);elapsed=time.monotonic()-started
   save_png(r,folder/'returned.png')
   # Evaluator-only later observation establishes what the app actually rendered.
   time.sleep(.5);final,r=call('view_window',dict(window_id=w['id']));save_png(r,folder/'reference.png')
   record=dict(trial=trial,delay_ms=delay,expected=expected,elapsed_seconds=elapsed,input_result=result,initial=initial,reference=final)
   (folder/'result.json').write_text(json.dumps(record,indent=2));results.append(record)
   print(label,round(elapsed,3),flush=True)
 (out/'results.json').write_text(json.dumps(results,indent=2))
 (out/'trace.json').write_text(json.dumps(trace,indent=2))
finally:
 s.close()
 if app:app.terminate();app.wait(timeout=5)
 if original.get('paused'):ui({'op':'pause','paused':True})
