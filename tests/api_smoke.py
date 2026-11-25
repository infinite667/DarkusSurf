"""Offline lifecycle integration test against a fake Tor process. Linux only."""
import os, pathlib, subprocess, tempfile, time, shutil, json, urllib.request
root=pathlib.Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as tmp:
 d=pathlib.Path(tmp); url=d/'url'; shim=d/'xdg-open'; shim.write_text('#!/bin/sh\nprintf "%s" "$1" > "$DARKUS_TEST_URL"\n'); shim.chmod(0o755)
 tor=d/'tor';shutil.copy(root/'tests/mock_tor.py',tor);tor.chmod(0o755)
 env={**os.environ,'PATH':str(d)+os.pathsep+os.environ['PATH'],'DARKUS_TEST_URL':str(url),'XDG_CONFIG_HOME':str(d/'config'),'DARKUS_TEST_BROWSER':str(d/'browser-args')}
 browser=d/'firefox';browser.write_text('#!/bin/sh\nprintf "%s\\n" "$@" > "$DARKUS_TEST_BROWSER"\n');browser.chmod(0o755)
 proc=subprocess.Popen([str(root/'dist/DarkusSurf')],env=env)
 try:
  for _ in range(100):
   if url.exists() and url.stat().st_size:break
   time.sleep(.05)
  base,secret=url.read_text().split('#');opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
  def call(path,method='GET',body=None):
   r=urllib.request.Request(base+'api/'+path,method=method,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+secret,'Content-Type':'application/json'});return json.load(opener.open(r,timeout=5))
  def wait(pred):
   for _ in range(100):
    s=call('status')
    if pred(s):return s
    time.sleep(.1)
   raise AssertionError(s)
  assert 'DarkusSurf' in opener.open(base).read().decode()
  assert call('status')['state']=='disconnected';call('connect','POST');s=wait(lambda s:s['state']=='connected');assert s['proxy'].startswith('127.0.0.1:')
  s=wait(lambda s:s['monitor']['Available']);assert s['monitor']['Read']==12345 and s['monitor']['Circuits'][0]['Hops']==['Guard','Middle','Exit']
  assert any(b['id']=='firefox' and b['path'] for b in call('browsers'))
  call('preferences','POST',{'selected':{'firefox':True},'compact':True,'autoVerify':False});assert call('preferences')['selected']['firefox']
  call('launch-browser','POST',{'id':'firefox'})
  for _ in range(100):
   if (d/'browser-args').exists() and (d/'browser-args').stat().st_size:break
   time.sleep(.05)
  args=(d/'browser-args').read_text().splitlines();assert '-no-remote' in args and args[-1]=='https://check.torproject.org/'
  profile=pathlib.Path(args[args.index('-profile')+1]);assert 'network.proxy.socks_remote_dns' in (profile/'user.js').read_text()
  call('verify','POST');s=wait(lambda s:any('verification failed' in l for l in s['logs']));assert not s['verified']
  call('circuit','POST');wait(lambda s:any('Requested new circuits' in l for l in s['logs']))
  call('disconnect','POST');wait(lambda s:s['state']=='disconnected');call('quit','POST');proc.wait(timeout=10)
 finally:
  if proc.poll() is None:proc.terminate();proc.wait(timeout=10)
print('Offline API / connect / browser selection and launch arguments / failed verification / circuit / disconnect / exit integration passed.')
