"""Isolated canary origin. Never connects to the registry or its database."""
import base64,collections,hashlib,json,socket,threading,time
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from urllib.parse import urlsplit,parse_qs
counts=collections.Counter();lock=threading.Lock()
payload=Path('/app/payload.json').read_bytes()
class Handler(BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def log_message(self,*args):pass
 def do_HEAD(self):self.do_GET()
 def do_POST(self):self.do_GET()
 def do_GET(self):
  u=urlsplit(self.path);q=parse_qs(u.query)
  if self.headers.get('X-Fixture-Disconnect-Pod')==socket.gethostname():
   # Deliberately fail only the named disposable origin, after receiving the
   # request. This verifies read failover without replaying sent POST requests.
   self.close_connection=True;self.connection.shutdown(socket.SHUT_RDWR);self.connection.close();return
  if u.path=='/_fixture/websocket' and self.headers.get('Upgrade','').lower()=='websocket':
   key=self.headers.get('Sec-WebSocket-Key','')
   accept=base64.b64encode(hashlib.sha1((key+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
   self.send_response(101);self.send_header('Upgrade','websocket');self.send_header('Connection','Upgrade');self.send_header('Sec-WebSocket-Accept',accept);self.end_headers()
   # One small masked text frame is enough to verify the proxy tunnel.
   frame=self.rfile.read(2)
   if len(frame)==2 and frame[0]==0x81 and frame[1]&0x80 and (frame[1]&0x7f)<126:
    mask=self.rfile.read(4);data=self.rfile.read(frame[1]&0x7f)
    if len(mask)==4:
     data=bytes(b^mask[i%4] for i,b in enumerate(data));self.wfile.write(bytes([0x81,len(data)])+data);self.wfile.flush()
   self.close_connection=True;return
  if u.path=='/_fixture/stats':
   with lock:body=json.dumps(dict(counts)).encode()
   status=200;headers={}
  elif u.path=='/_fixture/echo':
   body=json.dumps({'method':self.command,'path':self.path,'headers':dict(self.headers),'client_port':self.client_address[1]}).encode();status=200;headers={}
  else:
   with lock:counts[self.path]+=1;sequence=counts[self.path]
   delay=min(float(q.get('delay',['0'])[0]),5);time.sleep(max(0,delay))
   mode=q.get('mode',['public'])[0];status=int(self.headers.get('X-Fixture-Status',q.get('status',['200'])[0]));status=status if 200<=status<600 else 400
   body=payload;headers={'X-Origin-Sequence':str(sequence),'Cache-Control':'private, no-store' if mode=='private' else 'public, max-age=30'}
   if mode=='set-cookie':headers['Set-Cookie']='fixture=1; Secure; HttpOnly'
  self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)))
  for k,v in headers.items():self.send_header(k,v)
  self.end_headers()
  if self.command!='HEAD':
   try:self.wfile.write(body)
   except (BrokenPipeError,ConnectionResetError):
    # A client may disconnect during a deliberate canary failure/replacement test.
    pass
ThreadingHTTPServer(('0.0.0.0',8080),Handler).serve_forever()
