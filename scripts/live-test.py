#!/usr/bin/env python3
"""Opt-in live tests against a disposable Hyprland session.
Uses real MCP JSON-RPC and a separate local UI connection as the test human.
Never run against a desktop containing personal work.
"""
import base64,json,os,socket,struct,time,subprocess
from pathlib import Path
if os.environ.get('COMPUTER_USE_DISPOSABLE')!='1':raise SystemExit('Set COMPUTER_USE_DISPOSABLE=1 only inside an isolated test compositor.')
artifacts=Path(os.environ.get('COMPUTER_USE_ARTIFACT_DIR',Path(__file__).resolve().parent.parent/'evidence'))
artifacts.mkdir(parents=True,exist_ok=True)
root=Path(os.environ['XDG_RUNTIME_DIR'])/'computer-use'
def local(q):
 s=socket.socket(socket.AF_UNIX);s.connect(str(root/'ui.sock'));s.sendall(json.dumps(q).encode()+b'\n')
 r=json.loads(s.makefile('r').readline());s.close();return r
class Client:
 def __init__(self):
  self.s=socket.socket(socket.AF_UNIX);self.s.connect(str(root/'mcp.sock'));self.f=self.s.makefile('r');self.seq=0
  self.rpc('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'live-test','version':'1'}})
  self.s.sendall(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
 def rpc(self,method,params):
  self.seq+=1;self.s.sendall(json.dumps({'jsonrpc':'2.0','id':self.seq,'method':method,'params':params}).encode()+b'\n')
  while True:
   r=json.loads(self.f.readline())
   if r.get('id')==self.seq:return r
 def call(self,name,args={}):return self.rpc('tools/call',{'name':name,'arguments':args})
 def close(self):self.f.close();self.s.close()
def result(r):
 assert 'error' not in r,r
 assert not r['result'].get('isError'),r
 v=r['result'].get('structuredContent')
 if v is None:
  texts=[c['text'] for c in r['result']['content'] if c['type']=='text'];v=json.loads(texts[0]) if texts else None
 return v
def denied(r):return bool(r.get('error') or r.get('result',{}).get('isError'))
def grant(r,seconds=120):
 v=result(r);assert v['status']=='approval_required',v
 state=local({'op':'approve','id':v['request_id'],'seconds':seconds});assert not state.get('error'),state
 return v['request_id']
def guard(q):
 s=socket.socket(socket.AF_UNIX);s.connect(str(root/'guard.sock'));s.sendall(json.dumps(q).encode()+b'\n');r=json.loads(s.makefile().readline());s.close();return r
checks=[]
def ok(name):checks.append(name);print('PASS',name,flush=True)
local({'op':'mode','mode':'approve'});local({'op':'pause','paused':False})
c=Client()
try:
 r=c.call('list_windows',{});v=result(r);assert v['status']=='ok' and v['windows'];assert not result(c.call('computer_status'))['grants'];grant(c.call('request_permission',{'capability':'observe','scope':{'kind':'workspace','id':'1'},'reason':'Live capture test'}));ok('metadata is free; workspace pixel access still requires a grant')
 windows=result(c.call('list_windows',{'workspace':1}))['windows'];target=next(w for w in windows if w['class']=='kitty');other=next(w for w in windows if 'Pinta' in w['class']);wid=target['id'];rev=target['revision']
 tools=c.rpc('tools/list',{})['result']['tools'];names={t['name'] for t in tools};assert not {'approve','set_mode','shell','ui'}&names;assert denied(c.call('set_mode',{'mode':'yolo'}));assert result(c.call('computer_status'))['mode']=='approve';ok('MCP cannot approve or switch modes')
 r=c.call('view_window',{'window_id':other['id']});result(r);im=next(x for x in r['result']['content'] if x['type']=='image');data=base64.b64decode(im['data']);dims=struct.unpack('>II',data[16:24]);assert dims==tuple(other['size']),(dims,other['size']);ok('native toplevel screenshot dimensions, not desktop crop')
 (artifacts/'live-window-capture.png').write_bytes(data)
 actions={'window_id':wid,'revision':rev,'actions':[{'type':'text','text':'echo LIVE_MCP_OK'},{'type':'key','key':'ENTER'}]}
 grant(c.call('input_window',actions),3);assert result(c.call('input_window',actions))['status']=='completed';ok('approved scoped keyboard input delivered')
 cross={'window_id':other['id'],'revision':other['revision'],'actions':[{'type':'click','x':10,'y':10}]};r=c.call('input_window',cross);assert result(r)['status']=='approval_required';local({'op':'deny','id':result(r)['request_id']});ok('window grant does not permit another window')
 r=c.call('input_window',dict(actions,revision='stale'));assert denied(r);ok('stale geometry rejected')
 r=c.call('input_window',dict(actions,actions=[{'type':'click','x':-1,'y':5}]));assert denied(r);ok('out-of-window coordinates rejected before effects')
 grants=local({'op':'state'})['grants'];g=next(g for g in grants if g['capability']=='control');assert not guard({'op':'pointer_transaction','kind':'move','token':g['id'],'revision':rev,'x':100000,'y':10})['ok'];ok('compositor independently rejects out-of-window pointer')
 time.sleep(3.2);assert result(c.call('input_window',actions))['status']=='approval_required';assert not guard({'op':'focus','token':g['id'],'revision':rev})['ok'];ok('expiry enforced by broker and compositor')
 local({'op':'mode','mode':'yolo'});assert result(c.call('input_window',dict(actions,actions=[{'type':'key','key':'CTRL+L'},{'type':'text','text':'echo YOLO_TEST'},{'type':'key','key':'ENTER'}])))['status']=='completed';ok('YOLO auto-allows without grants')
 local({'op':'pause','paused':True});assert denied(c.call('input_window',actions));local({'op':'pause','paused':False});ok('pause overrides YOLO')
 # Pointer path actually delivered to Pinta, away from the trusted overlay.
 before=c.call('view_window',{'window_id':other['id']});before=base64.b64decode(next(x for x in before['result']['content'] if x['type']=='image')['data'])
 a={'window_id':other['id'],'revision':other['revision'],'actions':[{'type':'click','x':28,'y':485},{'type':'drag','x':100,'y':350,'to_x':200,'to_y':390},{'type':'scroll','x':180,'y':400,'delta':10}]}
 assert result(c.call('input_window',a))['status']=='completed';time.sleep(.3)
 after=c.call('view_window',{'window_id':other['id']});after=base64.b64decode(next(x for x in after['result']['content'] if x['type']=='image')['data']);assert before!=after,'pointer actions did not change Pinta pixels'
 (artifacts/'pointer-before.png').write_bytes(before);(artifacts/'pointer-after.png').write_bytes(after);ok('scoped pointer click, drag, scroll execute and change Pinta pixels')
 rec=result(c.call('record_window',{'window_id':wid}));assert rec['status']=='recording';time.sleep(.7);result(c.call('input_window',dict(actions,actions=[{'type':'text','text':'echo RECORDING_TEST'},{'type':'key','key':'ENTER'}])));time.sleep(.7)
 local({'op':'mode','mode':'approve'});time.sleep(1.5);rs=result(c.call('list_recordings'));record=next(r for r in rs if r['id']==rec['recording_id']);assert record['status']=='stopped' and record['frames']>=2 and record.get('path') and not record.get('error'),record
 subprocess.run(['ffprobe','-v','error',record['path']],check=True);ok('window recording finalizes on mode revocation')
 grant(c.call('request_permission',{'capability':'observe','scope':{'kind':'workspace','id':'1'},'reason':'Live capture test'}));state=local({'op':'state'});client=next(g['client'] for g in state['grants']);c.close();time.sleep(.5);assert not any(g['client']==client for g in local({'op':'state'})['grants']);ok('client disconnect revokes its grants')
 c=Client();local({'op':'mode','mode':'approve'})
 # Window destruction and title reuse: use an explicitly disposable target.
 proc=subprocess.Popen(['kitty','--title','lifetime-test','bash','--noprofile','--norc'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);time.sleep(1)
 grant(c.call('request_permission',{'capability':'observe','scope':{'kind':'workspace','id':'1'},'reason':'Live capture test'}));ws=result(c.call('list_windows',{'workspace':1}))['windows'];w=next(w for w in ws if w['title']=='lifetime-test');req=c.call('request_permission',{'capability':'control','scope':{'kind':'window','id':w['id']},'reason':'lifetime test'});grant(req)
 g=next(g for g in local({'op':'state'})['grants'] if g['capability']=='control');proc.terminate();proc.wait();time.sleep(.5);assert not guard({'op':'focus','token':g['id'],'revision':w['revision']})['ok'];ok('destroyed target invalidates compositor lease')
finally:
 c.close();local({'op':'mode','mode':'approve'});local({'op':'revoke_all'})
print(json.dumps({'passed':len(checks),'checks':checks},indent=2))
