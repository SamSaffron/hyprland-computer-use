#!/usr/bin/env python3
"""Send one command to the persistent demo MCP client (not a production API)."""
import json,os,socket,sys
path=os.environ.get('COMPUTER_USE_DEMO_SOCKET',os.environ.get('XDG_RUNTIME_DIR','/run/user/1000')+'/computer-use/demo.sock')
s=socket.socket(socket.AF_UNIX);s.settimeout(180);s.connect(path);s.sendall(sys.argv[1].encode()+b'\n');print(s.makefile().readline(),end='')
