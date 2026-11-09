// Explicit hardware acceptance helper. No phone numbers or secrets are written
// to disk; downlink is measured, not recorded. Use only with an approved test peer.
import {execFileSync} from 'node:child_process';
import {createHash,createHmac,randomBytes,randomUUID,timingSafeEqual} from 'node:crypto';
import {readFileSync} from 'node:fs';
import net from 'node:net';

const [mode,number,audioFile]=process.argv.slice(2);
if (!['health','observe','dial','incoming'].includes(mode)) throw Error('usage: node device-call-test.mjs health|observe|dial|incoming [number OR observe-seconds] [pcm-file]');
// Never use whichever Android happens to be first on ADB. This helper targets
// the MU300 explicitly; private paired keys are kept in memory, not printed.
const deviceRead=command=>execFileSync('ssh',['-o','BatchMode=yes','-o','ConnectTimeout=5','root@192.168.78.1',command],{encoding:'utf8',timeout:15000});
const state=JSON.parse(deviceRead('cat /mnt/mu300-disk/extra/airsim/data/vowlan-clients.json'));
if (!state.length) throw Error('pair the client first');
const key=Buffer.from(state.at(-1),'base64url');
const sign=s=>createHmac('sha256',key).update(s).digest('base64url');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function api(path,body) {
 const method=body===undefined?'GET':'POST', payload=body===undefined?'':JSON.stringify(body);
 const timestamp=String(Math.floor(Date.now()/1000)),nonce=randomBytes(18).toString('base64url');
 const signature=sign(`${method}\n${path}\n${createHash('sha256').update(payload).digest('hex')}\n${timestamp}\n${nonce}`);
 const response=await fetch('http://192.168.78.1:8581'+path,{method,body:body===undefined?undefined:payload,headers:{'Content-Type':'application/json','X-AirSIM-VoWLAN-Version':'1','X-AirSIM-VoWLAN-Timestamp':timestamp,'X-AirSIM-VoWLAN-Nonce':nonce,'X-AirSIM-VoWLAN-Signature':signature},signal:AbortSignal.timeout(18000)});
 const value=await response.json();
 if (!response.ok) throw Error(`gateway ${path}: HTTP ${response.status} ${value.error??''}`);
 return {value,nonce};
}
function same(a,b) {return typeof a==='string'&&a.length===b.length&&timingSafeEqual(Buffer.from(a),Buffer.from(b));}
const {value:health,nonce}=await api('/v1/health');
if (!same(health.identity_proof,sign(`AIRSIM-HEALTH1\n${nonce}\n${health.agent_id}`))||!health.canonical_call_ids) throw Error('authenticated call capability missing');
console.log(JSON.stringify({phase:'identity_verified',canonical_call_ids:true,phone_number_available:!!health.phone_number}));
if (mode==='health') process.exit(0);
if (mode==='observe') {
 const duration=number===undefined?600:Number(number);
 if(!Number.isInteger(duration)||duration<1||duration>1800)throw Error('observe duration must be 1..1800 seconds');
 console.log(JSON.stringify({phase:'observing_read_only',duration_seconds:duration}));
 const until=Date.now()+duration*1000;
 let last='';
 while(Date.now()<until) {
  const s=(await api('/api/calls/status')).value;
  const pick=c=>({id:c.id,index:c.index,state:c.state,direction:c.direction,ended_at:c.ended_at});
  const snapshot={active:s.active?pick(s.active):null,calls:(s.calls??[]).map(pick),history:(s.history??[]).map(pick),last_poll_error:s.last_poll_error};
  const json=JSON.stringify(snapshot);
  if(json!==last){console.log(JSON.stringify({time:new Date().toISOString(),...snapshot}));last=json;}
  await sleep(1000);
 }
 process.exit(0);
}
let ownedID;
async function media() {
 const speech=audioFile?readFileSync(audioFile):Buffer.alloc(0);
 const socket=net.createConnection({host:'192.168.78.1',port:8582});
 let buffer=Buffer.alloc(0),ready=false,bytes=0,peak=0,offset=0,timer;
 const ts=String(Math.floor(Date.now()/1000)),n=randomBytes(18).toString('base64url');
 const signature=sign(`PCM\n/v1/pcm\n${createHash('sha256').update('').digest('hex')}\n${ts}\n${n}`);
 const completed=new Promise((resolve,reject)=>{
  const deadline=setTimeout(()=>{socket.destroy();reject(Error('PCM handshake timeout'));},20000);
  socket.on('error',reject);
  socket.on('connect',()=>socket.write(`AIRSIMVWL1 ${ts} ${n} ${signature}\n`));
  socket.on('data',chunk=>{
   if (!ready) {
    buffer=Buffer.concat([buffer,chunk]);if(buffer.length<11)return;
    if(buffer.subarray(0,11).toString()!=='AIRSIMREADY'){socket.destroy();reject(Error('bad PCM handshake'));return;}
    ready=true;clearTimeout(deadline);console.log(JSON.stringify({phase:'pcm_ready'}));
    chunk=buffer.subarray(11);buffer=Buffer.alloc(0);
    timer=setInterval(()=>{const frame=Buffer.alloc(320);if(offset<speech.length)speech.copy(frame,0,offset,offset+320);offset+=320;socket.write(frame);},20);
    setTimeout(()=>socket.destroy(),14000);
   }
   bytes+=chunk.length;for(let i=0;i+1<chunk.length;i+=2)peak=Math.max(peak,Math.abs(chunk.readInt16LE(i)));
  });
  socket.on('close',()=>{clearTimeout(deadline);clearInterval(timer);if(!ready)reject(Error('PCM closed before ready'));else resolve();});
 });
 try {await completed;} finally {socket.destroy();clearInterval(timer);}
 console.log(JSON.stringify({phase:'pcm_complete',downlink_bytes:bytes,downlink_peak:peak,uplink_bytes:offset}));
 if (bytes<320*20||offset<320*20)throw Error('insufficient duplex PCM');
}
try {
 if(mode==='dial') {
  if(!/^\+?[0-9]{3,20}$/.test(number??''))throw Error('approved test number required');
  if((await api('/api/calls/status')).value.active)throw Error('device already has a call');
  const operation=randomUUID(),reply=await api('/api/calls/dial',{number,operation_id:operation});
  const r=reply.value;
  if(r.operation_id!==operation||!same(r.operation_proof,sign(`AIRSIM-DIAL1\n${reply.nonce}\n${operation}\n${r.android_id}\n${r.backend_id}`)))throw Error('dial receipt verification failed');
  ownedID=r.backend_id;console.log(JSON.stringify({phase:'dial_receipt_verified'}));
 } else {
  console.log(JSON.stringify({phase:'waiting_for_incoming',timeout_seconds:180}));
  const until=Date.now()+180000;
  while(Date.now()<until) {
   const c=(await api('/api/calls/status')).value.active;
   if(c?.direction==='incoming'&&['incoming','waiting'].includes(c.state)) {ownedID=c.id;if(c.android_id!==ownedID)throw Error('incoming identity missing');await api('/api/calls/answer',{call_id:ownedID});console.log(JSON.stringify({phase:'incoming_answer_sent'}));break;}
   await sleep(1000);
  }
  if(!ownedID)throw Error('no incoming test call received');
 }
 let active=false,last='';
 for(let i=0;i<60;i++) {
  const c=(await api('/api/calls/status')).value.active;
  if(!c||c.id!==ownedID)throw Error('test call ended before connection');
  if(c.state!==last){last=c.state;console.log(JSON.stringify({phase:'call_state',state:last}));}
  if(c.state==='active'){active=true;break;}
  await sleep(1000);
 }
 if(!active)throw Error('test peer did not answer');
 await media();
} finally {
 if(ownedID) {
  // Allow AP streams to close before asking the baseband to tear down the call.
  for(let i=0;i<10;i++) {
   const status=JSON.parse(deviceRead('wget -qO- http://127.0.0.1:7581/status'));
   if(!status.audio.active)break;await sleep(1000);
  }
  const current=(await api('/api/calls/status')).value.active;
  if(current?.id===ownedID)await api('/api/calls/hangup',{call_id:ownedID});
  console.log(JSON.stringify({phase:'test_call_cleanup'}));
 }
}
