#!/usr/bin/env python3
"""Persistent real stdio MCP client for lab demos. Local control socket is test-only.
Send {"tool":"list_windows","args":{"workspace":1}} over the control socket.
No approval/policy API exists here: decisions must happen separately in the UI.
"""
import argparse,json,os,socket,subprocess,time
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--socket',required=True);p.add_argument('--trace',required=True);a=p.parse_args()
proc=subprocess.Popen([a.binary,'mcp'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True,bufsize=1)
seq=0
trace=open(a.trace,'a',buffering=1)
def request(method,params):
 global seq
 seq+=1;req={'jsonrpc':'2.0','id':seq,'method':method,'params':params}
 trace.write(json.dumps({'time':time.time(),'request':req})+'\n')
 proc.stdin.write(json.dumps(req)+'\n');proc.stdin.flush()
 while True:
  line=proc.stdout.readline()
  if not line:raise RuntimeError('MCP server disconnected')
  response=json.loads(line)
  if response.get('id')==seq:
   summary=json.loads(json.dumps(response))
   for c in summary.get('result',{}).get('content',[]):
    if c.get('type')=='image':c['data']='<image omitted from trace>'
   trace.write(json.dumps({'time':time.time(),'response':summary})+'\n')
   return response
request('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'computer-use-demo','version':'0.1'}})
proc.stdin.write(json.dumps({'jsonrpc':'2.0','method':'notifications/initialized'})+'\n');proc.stdin.flush()
s=socket.socket(socket.AF_UNIX);s.bind(a.socket);os.chmod(a.socket,0o600);s.listen(4)
try:
 while True:
  c,_=s.accept()
  with c:
   data=b''
   while b'\n' not in data:
    part=c.recv(65536)
    if not part:break
    data+=part
    if len(data)>1048576:raise RuntimeError('oversized demo command')
   try:
    q=json.loads(data)
    if q.get('close'):c.sendall(b'{"closed":true}\n');break
    if 'tool' in q:r=request('tools/call',{'name':q['tool'],'arguments':q.get('args',{})})
    else:r=request(q.get('method','tools/list'),q.get('params',{}))
   except Exception as e:r={'error':str(e)}
   c.sendall(json.dumps(r).encode()+b'\n')
finally:
 proc.stdin.close();proc.terminate();proc.wait();s.close();os.unlink(a.socket);trace.close()
