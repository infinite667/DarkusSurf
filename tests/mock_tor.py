#!/usr/bin/env python3
# Offline test double, not a Tor implementation. Rejects all SOCKS requests.
import sys, pathlib, socketserver, threading, socket
args=sys.argv[1:]
def option(name): return args[args.index(name)+1]
cookie=bytes(range(32)); pathlib.Path(option('--CookieAuthFile')).write_bytes(cookie)
class Control(socketserver.StreamRequestHandler):
 def handle(self):
  if self.rfile.readline().strip()!=b'AUTHENTICATE '+cookie.hex().encode():
   self.wfile.write(b'515 Bad authentication\r\n');return
  self.wfile.write(b'250 OK\r\n'); line=self.rfile.readline().strip()
  if line==b'GETINFO status/bootstrap-phase':self.wfile.write(b'250-status/bootstrap-phase=NOTICE BOOTSTRAP PROGRESS=100 SUMMARY="Done"\r\n250 OK\r\n')
  elif line==b'GETINFO net/listeners/socks':self.wfile.write(f'250-net/listeners/socks="127.0.0.1:{socks.server_address[1]}"\r\n250 OK\r\n'.encode())
  elif line==b'GETINFO traffic/read traffic/written circuit-status':self.wfile.write(b'250-traffic/read=12345\r\n250-traffic/written=456\r\n250+circuit-status=\r\n12 BUILT $'+b'A'*40+b'~Guard,$'+b'B'*40+b'~Middle,$'+b'C'*40+b'~Exit PURPOSE=GENERAL\r\n.\r\n250 OK\r\n')
  elif line==b'SIGNAL NEWNYM':self.wfile.write(b'250 OK\r\n')
  else:self.wfile.write(b'552 Unrecognized\r\n')
class Socks(socketserver.BaseRequestHandler):
 def handle(self):
  self.request.settimeout(2)
  self.request.recv(3);self.request.sendall(b'\x05\x00');self.request.recv(1024);self.request.sendall(b'\x05\x01\x00\x01\x00\x00\x00\x00\x00\x00')
class Server(socketserver.ThreadingTCPServer): daemon_threads=True
socks=Server(('127.0.0.1',0),Socks);threading.Thread(target=socks.serve_forever,daemon=True).start()
control=Server(('127.0.0.1',0),Control);pathlib.Path(option('--ControlPortWriteToFile')).write_text(f'PORT=127.0.0.1:{control.server_address[1]}\n');control.serve_forever()
