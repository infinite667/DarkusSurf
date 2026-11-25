// Offline UI state test. Minimal DOM double, not a real browser rendering test.
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
class El {
 constructor(){this.children=[];this.dataset={};this.classList={toggle(){}};this.style={};this.checked=false;this.disabled=false;this.textContent='';this.events={};}
 append(...xs){this.children.push(...xs)}
 replaceChildren(){this.children=[]}
 addEventListener(k,f){this.events[k]=f}
}
const ids=new Map();const get=id=>{if(!ids.has(id))ids.set(id,new El());return ids.get(id)};
function all(el){return [el,...el.children.filter(x=>x instanceof El).flatMap(all)]}
const document={getElementById:get,createElement:()=>new El(),body:new El(),querySelectorAll:q=>q.startsWith('.')?all(get('browserList')).filter(e=>(e.className||'').split(' ').includes(q.slice(1))):[]};
let saved={selected:{},compact:false,autoVerify:false}, status={state:'disconnected',progress:0,logs:[]};
const browserList=[{id:'brave',name:'Brave',path:'C:/Brave/brave.exe'},{id:'firefox',name:'Firefox',path:''}];
const context=vm.createContext({document,location:{hash:'#test',pathname:'/'},history:{replaceState(){}},sessionStorage:{setItem(){}},setInterval(){},Date,console,fetch:async(url,opt)=>{let data;switch(url){case '/api/status':data=status;break;case '/api/preferences':if(opt.method==='POST')saved=JSON.parse(opt.body);data=saved;break;case '/api/browsers':data=browserList;break;default:throw Error(url)}return {ok:true,json:async()=>JSON.parse(JSON.stringify(data))}}});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../ui/app.js'),'utf8'),context);
(async()=>{
 await new Promise(r=>setImmediate(r));
 const checks=()=>document.querySelectorAll('.appCheck');const launches=()=>document.querySelectorAll('.launchBrowser');
 assert.equal(checks().length,2);assert.equal(checks()[0].disabled,false);assert.equal(checks()[1].disabled,false);
 checks()[0].checked=true;checks()[0].events.change();assert.equal(get('selectedCount').textContent,1);assert.equal(launches()[0].disabled,true);
 checks()[1].checked=true;checks()[1].events.change();assert.equal(get('selectedCount').textContent,2);
 await get('saveApps').onclick();assert.equal(saved.selected.brave,true);assert.equal(saved.selected.firefox,true);
 status.state='connected';await vm.runInContext('refresh()',context);assert.equal(launches()[0].disabled,false);assert.equal(launches()[1].disabled,true);
 await get('scanApps').onclick();assert.equal(checks()[0].checked,true);assert.equal(checks()[1].checked,true);
 checks()[0].checked=false;checks()[0].events.change();assert.equal(launches()[0].disabled,true);await get('saveApps').onclick();assert.equal(saved.selected.brave,false);
 console.log('PASS: selection while disconnected, undetected selection, counts, save, launch gating, refresh preservation, deselection (DOM double).');
})().catch(e=>{console.error(e);process.exitCode=1});
