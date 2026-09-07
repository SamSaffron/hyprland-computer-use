#!/usr/bin/env python3
"""Disposable-lab test. Real stdio MCP; old pointer/wtype simulate the human.
Requires COMPUTER_USE_DISPOSABLE=1, COMPUTER_USE_HUMAN_POINTER, and the lab shortcut.
"""
import base64,json,os,socket,subprocess,time
from pathlib import Path
if os.environ.get('COMPUTER_USE_DISPOSABLE')!='1':raise SystemExit('Disposable compositor only')
root=Path(os.environ['XDG_RUNTIME_DIR'])/'computer-use'
binary=os.environ.get('COMPUTER_USE_BINARY',str(Path(__file__).resolve().parent.parent/'build/computer-use'))
pointer=os.environ['COMPUTER_USE_HUMAN_POINTER']
class Client:
 def __init__(self):
  self.p=subprocess.Popen([binary,'mcp'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True);self.seq=0
  self.rpc('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'sharing-test','version':'1'}})
  self.p.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n');self.p.stdin.flush()
 def rpc(self,m,args):
  self.seq+=1;self.p.stdin.write(json.dumps({'jsonrpc':'2.0','id':self.seq,'method':m,'params':args})+'\n');self.p.stdin.flush()
  while True:
   r=json.loads(self.p.stdout.readline())
   if r.get('id')==self.seq:return r
 def call(self,n,a={}):return self.rpc('tools/call',{'name':n,'arguments':a})
 def close(self):self.p.stdin.close();self.p.wait(timeout=5)
def result(r):
 assert not r.get('error') and not r['result'].get('isError'),r
 return r['result'].get('structuredContent')
def local(q):
 s=socket.socket(socket.AF_UNIX);s.connect(str(root/'ui.sock'));s.sendall(json.dumps(q).encode()+b'\n');r=json.loads(s.makefile().readline());s.close();return r
def click(x,y):subprocess.run([pointer],input=f'm {x} {y} 100\nd 0 0 80\nu 0 0 100\n',text=True,check=True);time.sleep(.6)
def share(*args):subprocess.run([binary,'share',*args],check=True);time.sleep(.6)
def key(*args):subprocess.run(['wtype',*args],check=True);time.sleep(.6)
def ok(s):print('PASS',s,flush=True)
local({'op':'mode','mode':'approve'});local({'op':'pause','paused':False})
a=Client();b=None
try:
 ws=result(a.call('list_windows'))['windows'];w=next(w for w in ws if w['class']=='kitty');other=next(w for w in ws if 'Pinta' in w['class']);aid=result(a.call('computer_status'))['client_id']
 assert not result(a.call('computer_status'))['grants'];ok('metadata discovery without asking or granting')
 share();assert local({'op':'state'})['picker']['client']==aid
 subprocess.run(['grim','/home/demo/lab/sharing-picker.png'],check=True)
 click(w['at'][0]+w['size'][0]//2,w['at'][1]+300)
 st=result(a.call('computer_status'));assert len(st['grants'])==1 and st['grants'][0]['capability']=='control' and st['grants'][0]['remaining_seconds']>290
 assert st['requests']==[];ok('CLI + actual window click creates five-minute control grant, without agent request')
 args={'window_id':w['id'],'revision':w['revision'],'actions':[{'type':'text','text':'echo PROACTIVE_SHARE_WORKS'},{'type':'key','key':'ENTER'}]}
 assert result(a.call('input_window',args))['status']=='completed';r=a.call('view_window',{'window_id':w['id']});result(r)
 Path('/home/demo/lab/sharing-terminal.png').write_bytes(base64.b64decode(next(c['data'] for c in r['result']['content'] if c['type']=='image')))
 assert result(a.call('record_window',{'window_id':w['id']}))['status']=='approval_required';ok('shared window accepts real input/pixels, but recording still asks')
 assert result(a.call('input_window',dict(args,window_id=other['id'],revision=other['revision'])))['status']=='approval_required';ok('control does not leak to another window')
 local({'op':'revoke_all'});assert result(a.call('input_window',args))['status']=='approval_required';ok('revoking a proactive share blocks subsequent input')
 local({'op':'revoke_all'});share();key('-k','Escape');assert not local({'op':'state'}).get('picker') and not result(a.call('computer_status'))['grants'];ok('Escape cancels without authority')
 key('-M','logo','-M','ctrl','-k','s','-m','ctrl','-m','logo');assert local({'op':'state'}).get('picker');key('-k','Escape');ok('actual Super+Ctrl+S invokes local picker')
 click(1270,16);assert local({'op':'state'}).get('picker');key('-k','Escape');ok('Waybar Share window button invokes the picker')
 share('--view-only');click(w['at'][0]+300,w['at'][1]+300)
 assert result(a.call('computer_status'))['grants'][0]['capability']=='observe';assert result(a.call('input_window',args))['status']=='approval_required';ok('explicit view-only sharing does not grant input')
 local({'op':'revoke_all'});b=Client();bid=result(b.call('computer_status'))['client_id'];share();assert local({'op':'state'})['picker']['client']==''
 click(w['at'][0]+300,w['at'][1]+300);assert not result(a.call('computer_status'))['grants'] and not result(b.call('computer_status'))['grants'];ok('multiple clients require an explicit recipient')
 # Select a recipient in the real dropdown, then click the window.
 first=local({'op':'state'})['clients'][0]['id'];click(400,82);key('-k','Down','-k','Return');click(w['at'][0]+300,w['at'][1]+300)
 grants=result(a.call('computer_status'))['grants']+result(b.call('computer_status'))['grants'];assert len(grants)==1 and grants[0]['client']==first;ok('actual recipient dropdown shares only to the chosen connection')
 local({'op':'revoke_all'})
 # Explicit CLI recipient followed by actual window click.
 share('--client',bid,'--seconds','3');click(w['at'][0]+300,w['at'][1]+300)
 assert len(result(b.call('computer_status'))['grants'])==1 and not result(a.call('computer_status'))['grants'];time.sleep(3.1)
 assert result(b.call('input_window',args))['status']=='approval_required';ok('recipient isolation and timed expiry')
 local({'op':'revoke_all'});b.close();b=None
 share();click(w['at'][0]+300,w['at'][1]+300);a.close();a=Client();assert not result(a.call('computer_status'))['grants'];ok('disconnect revokes; reconnect never inherits sharing')
 local({'op':'pause','paused':True});assert result(a.call('list_windows'))['windows'];r=subprocess.run([binary,'share'],capture_output=True);assert r.returncode!=0;ok('paused discovery stays free, proactive grants remain blocked')
 local({'op':'pause','paused':False});share();click(w['at'][0]+300,w['at'][1]+300)
 assert result(a.call('input_window',dict(args,actions=[{'type':'text','text':'echo FIVE_MINUTE_USER_SHARE'},{'type':'key','key':'ENTER'}])))['status']=='completed'
 subprocess.run(['grim','/home/demo/lab/sharing-result.png'],check=True)
 # Close permission panel, then activate the actual Waybar-hosted SNI icon.
 click(1348,86);click(1375,16);subprocess.run(['grim','/home/demo/lab/sharing-tray-open.png'],check=True);ok('Waybar tray activation exercised')
finally:
 if b:b.close()
 a.close();local({'op':'revoke_all'});local({'op':'pause','paused':True})
print('LIVE SHARING COMPLETE',flush=True)
