"""Offline Linux smoke test, requires Playwright and Chromium. Uses a test double, not Tor."""
import os, pathlib, subprocess, tempfile, time, shutil
from playwright.sync_api import sync_playwright
root=pathlib.Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as tmp:
 d=pathlib.Path(tmp); url=d/'url'; shim=d/'xdg-open'; shim.write_text('#!/bin/sh\nprintf "%s" "$1" > "$DARKUS_TEST_URL"\n'); shim.chmod(0o755)
 tor=d/'tor';shutil.copy(root/'tests/mock_tor.py',tor);tor.chmod(0o755)
 env={**os.environ,'PATH':str(d)+os.pathsep+os.environ['PATH'],'DARKUS_TEST_URL':str(url),'XDG_CONFIG_HOME':str(d/'config')}
 fake=d/'brave-browser'; fake.write_text('#!/bin/sh\nexit 0\n');fake.chmod(0o755)
 proc=subprocess.Popen([str(root/'dist/DarkusSurf')],env=env)
 try:
  for _ in range(100):
   if url.exists() and url.stat().st_size:break
   time.sleep(.05)
  with sync_playwright() as p:
   browser=p.chromium.launch(executable_path=shutil.which('chromium'),headless=True,args=['--no-sandbox']);page=browser.new_page(viewport={'width':1250,'height':850});page.on('dialog',lambda dialog:dialog.accept());page.goto(url.read_text());page.get_by_role('button',name='Applications').click();row=page.locator('.appRow').filter(has_text='Brave');row.locator('input').check();assert row.locator('input').is_checked();assert page.locator('#selectedCount').inner_text()=='1';page.get_by_role('button',name='Save selection').click();page.wait_for_function("document.getElementById('notice').textContent.includes('Preferences saved')");page.get_by_role('button',name='Dashboard').click();page.get_by_text('Connect to Tor',exact=True).click();page.wait_for_function("document.getElementById('heroTitle').textContent==='Connected to Tor'")
   assert page.locator('#proxy').inner_text().startswith('127.0.0.1:');page.get_by_role('button',name='Verify Tor exit').first.click();page.wait_for_function("document.getElementById('logs').textContent.includes('verification failed')",timeout=15000);assert page.locator('#verified').inner_text()=='Not verified';page.get_by_role('button',name='Request new circuit').first.click();page.wait_for_function("document.getElementById('logs').textContent.includes('Requested new circuits')")
   if os.getenv('DARKUS_SCREENSHOT'):page.screenshot(path=os.environ['DARKUS_SCREENSHOT'],full_page=True)
   page.get_by_role('button',name='Disconnect',exact=True).click();page.wait_for_function("document.getElementById('heroTitle').textContent==='Disconnected'");page.get_by_role('button',name='Exit DarkusSurf').click();proc.wait(timeout=10);browser.close()
 finally:
  if proc.poll() is None:proc.terminate();proc.wait(timeout=10)
print('Offline GUI / connect / verification failure / circuit / disconnect / exit smoke test passed.')
