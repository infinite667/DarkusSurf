'use strict';
const $=id=>document.getElementById(id);let secret=location.hash.slice(1);if(secret){sessionStorage.setItem('ds-token',secret);history.replaceState(null,'',location.pathname)}else{secret=sessionStorage.getItem('ds-token')||''}let current={},busy=false,exiting=false,autoDone=0,prefs={selected:{},compact:false,autoVerify:false},browsers=[],prefsReady=false,dirty=false,saving=false,scanning=false;
const show=msg=>{$('notice').textContent=msg;$('notice').hidden=false};
async function api(path,method='GET',body){const r=await fetch('/api/'+path,{method,headers:{Authorization:'Bearer '+secret,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),cache:'no-store'});if(!r.ok){let data;try{data=await r.json()}catch{}throw Error(data?.error||'Local backend unavailable ('+r.status+')')}return r.json()}
async function act(path){if(busy||exiting)return;busy=true;$('notice').hidden=true;try{await api(path,'POST');await refresh()}catch(e){show(e.message)}finally{busy=false}}
function render(s){current=s;let connected=s.state==='connected';let titles={connected:'Connected to Tor',starting:'Connecting to Tor',disconnected:'Disconnected',stopping:'Disconnecting',error:'Connection error'};$('heroTitle').textContent=titles[s.state]||s.state;$('status').textContent=s.state.toUpperCase();$('status').classList.toggle('connected',connected);$('detail').textContent=s.detail;$('progressText').textContent=s.progress+'% bootstrap';$('bar').style.width=s.progress+'%';$('ip').textContent=s.ip||'—';$('verified').textContent=s.checking?'Verifying…':s.verified?'Tor Project confirmed':'Not verified';$('proxy').textContent=s.proxy||'—';$('networkProxy').textContent=s.proxy||'Not connected';$('privacyState').textContent=connected?'Available — opt-in only':'Inactive';$('connect').disabled=!['disconnected','error'].includes(s.state);$('disconnect').disabled=!['starting','connected'].includes(s.state);$('copy').disabled=!s.proxy;document.querySelectorAll('.verify').forEach(b=>b.disabled=!connected||s.checking);document.querySelectorAll('.circuit').forEach(b=>b.disabled=!connected);$('logs').textContent=s.logs?.join('\n')||'No session events.';let t=s.started?Math.max(0,Math.floor(Date.now()/1000)-s.started):0;$('duration').textContent=[Math.floor(t/3600),Math.floor(t/60)%60,t%60].map(n=>String(n).padStart(2,'0')).join(':');updateLaunchButtons();if(connected&&prefsReady&&$('autoVerify').checked&&autoDone!==s.started&&!s.checking&&!busy){autoDone=s.started;act('verify')}}
async function refresh(){if(exiting)return;try{render(await api('status'))}catch(e){show(e.message);$('status').textContent='BACKEND OFFLINE';document.querySelectorAll('button:not(.nav)').forEach(b=>b.disabled=true)}}
document.querySelectorAll('.nav').forEach(b=>b.onclick=()=>{document.querySelectorAll('.page').forEach(p=>p.hidden=p.id!==b.dataset.page);document.querySelectorAll('.nav').forEach(n=>n.classList.toggle('active',n===b));$('heading').textContent=b.dataset.page[0].toUpperCase()+b.dataset.page.slice(1)});$('connect').onclick=()=>act('connect');$('disconnect').onclick=()=>act('disconnect');document.querySelectorAll('.verify').forEach(b=>b.onclick=()=>act('verify'));document.querySelectorAll('.circuit').forEach(b=>b.onclick=()=>act('circuit'));$('quit').onclick=async()=>{if(!confirm('Close the Tor-configured browser windows first. Stop Tor and exit DarkusSurf?'))return;exiting=true;try{await api('quit','POST')}catch{}document.querySelectorAll('button').forEach(b=>b.disabled=true);show('DarkusSurf stopped. You can close this window.');$('status').textContent='EXITED'};$('copy').onclick=()=>navigator.clipboard.writeText(current.proxy||'').catch(()=>show('Copy unavailable; select the address manually.'));$('copyLogs').onclick=()=>navigator.clipboard.writeText($('logs').textContent).catch(()=>show('Copy unavailable; select the log manually.'));function updateLaunchButtons(){
 const selected=Object.values(prefs.selected).filter(Boolean).length;
 $('selectedCount').textContent=selected;
 document.querySelectorAll('.appCheck').forEach(c=>{c.disabled=!prefsReady||saving});
 document.querySelectorAll('.launchBrowser').forEach(b=>{
  const app=browsers.find(x=>x.id===b.dataset.id);
  const reason=!app?.path?'Browser not detected':!prefs.selected[b.dataset.id]?'Select this browser first':dirty?'Save selection first':current.state!=='connected'?'Connect to Tor first':'';
  b.disabled=!!reason||saving||scanning;b.title=reason||'Open a separate Tor-configured window';
 });
 $('saveApps').disabled=!prefsReady||saving;
 $('scanApps').disabled=scanning||saving;
 $('selectionStatus').textContent=saving?'Saving…':dirty?'Unsaved changes — click Save selection.':prefsReady?'Selection saved. Connect to Tor to launch a detected browser.':'Loading preferences…';
}
function drawApps(){
 let list=$('browserList');list.replaceChildren();
 for(let app of browsers){
  let row=document.createElement('div');row.className='appRow';
  let name=document.createElement('div'),strong=document.createElement('strong'),small=document.createElement('small');
  strong.textContent=app.name;small.textContent=app.path?'Detected · separate profile':'Not detected · selection allowed, launch unavailable';name.append(strong,small);
  let label=document.createElement('label');label.className='appToggle';label.htmlFor='select-'+app.id;
  let check=document.createElement('input');check.type='checkbox';check.id='select-'+app.id;check.className='appCheck';check.checked=!!prefs.selected[app.id];
  let state=document.createElement('span');state.textContent=check.checked?'Use Tor ✓':'Use Tor';label.append(check,state);
  check.addEventListener('change',()=>{prefs.selected[app.id]=check.checked;state.textContent=check.checked?'Use Tor ✓':'Use Tor';dirty=true;updateLaunchButtons()});
  let launch=document.createElement('button');launch.className='secondary launchBrowser';launch.dataset.id=app.id;launch.textContent='Launch via Tor';
  launch.onclick=async()=>{if(busy||exiting||launch.disabled)return;busy=true;try{await api('launch-browser','POST',{id:app.id});show(app.name+' launch requested. Check the Tor Project page in the new window.')}catch(e){show(e.message)}finally{busy=false}};
  row.append(name,label,launch);list.append(row);
 }updateLaunchButtons();
}
async function loadPrefs(){try{const loaded=await api('preferences');prefs={...loaded,selected:loaded.selected||{}};prefsReady=true;for(const id of ['compact','autoVerify'])$(id).checked=!!prefs[id];document.body.classList.toggle('compact',prefs.compact);await scanApps()}catch(e){show(e.message)}}
async function savePrefs(){if(!prefsReady||saving||exiting)return;saving=true;updateLaunchButtons();try{prefs.compact=$('compact').checked;prefs.autoVerify=$('autoVerify').checked;await api('preferences','POST',prefs);dirty=false;show('Preferences saved locally.')}catch(e){show(e.message)}finally{saving=false;updateLaunchButtons()}}
async function scanApps(){if(scanning||saving)return;scanning=true;updateLaunchButtons();try{browsers=await api('browsers');drawApps()}catch(e){show(e.message)}finally{scanning=false;updateLaunchButtons()}}
$('saveApps').onclick=savePrefs;$('scanApps').onclick=scanApps;for(const id of ['compact','autoVerify']){$(id).onchange=()=>{document.body.classList.toggle('compact',$('compact').checked);dirty=true;updateLaunchButtons()}}$('saveSettings').onclick=savePrefs;loadPrefs();refresh();setInterval(refresh,1500);
