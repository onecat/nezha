const { spawn } = require('node:child_process');
const { runStreamSmoke } = require('./stream-smoke.cjs');
const { once } = require('node:events');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const sleep = ms => new Promise(resolve => setTimeout(resolve,ms));
async function until(label,timeout,fn) {
  const end=Date.now()+timeout;
  while(Date.now()<end) {
    if(await fn()) return;
    await sleep(1000);
  }
  throw Error('timeout: '+label);
}
async function runAgentSmoke(ctx,browser,base) {
  const binary=process.env.SMOKE_AGENT_BINARY;
  if(!binary || !fs.existsSync(binary)) throw Error('missing verified V0.20.5 agent');
  const work=fs.mkdtempSync(path.join(os.tmpdir(),'nezha-agent-smoke-'));
  const children=[];
  const origin={Origin:base};
  const get=async (route)=>{
    const r=await ctx.request.get(base+route,{timeout:10000});
    if(!r.ok()) throw Error('GET '+route+' status '+r.status());
    return r.json();
  };
  const post=async (route,data)=>{
    const r=await ctx.request.post(base+route,{data,headers:origin,timeout:10000});
    if(!r.ok()) throw Error('POST '+route+' status '+r.status());
    const j=await r.json();
    if(j.code!==200) throw Error('POST '+route+' application status '+j.code);
    return j;
  };
  const start=(secret)=>{
    const fd=fs.openSync(path.join(work,'agent.log'),'a');
    const child=spawn(binary,['-s','127.0.0.1:15555','-p',secret,
      '--disable-auto-update','--disable-force-update',
      '--skip-conn','--skip-procs','--report-delay','1'],
      {cwd:work,stdio:['ignore',fd,fd]});
    fs.closeSync(fd);
    child.on('error',()=>{});
    children.push(child);
    return child;
  };
  const stop=async(child)=>{
    if(child.exitCode!==null||child.signalCode!==null)return;
    const exited=once(child,'exit').catch(()=>{});
    child.kill('SIGTERM');
    await Promise.race([exited,sleep(2000)]);
    if(child.exitCode===null&&child.signalCode===null){child.kill('SIGKILL');await Promise.race([exited,sleep(2000)]);}
  };
  try {
    const registered=await post('/api/v1/server/register',{Name:'ci-public-agent',Tag:'ci',HideForGuest:'off'});
    if(typeof registered.secret!=='string'||!registered.secret) throw Error('agent secret missing');
    await post('/api/v1/server/register',{Name:'ci-hidden-agent',Tag:'ci',HideForGuest:'on'});
    let publicID,hiddenID;
    // Registration initializes empty host records; retrieve stable IDs first.
    const list=await get('/api/v1/server/list');
    publicID=list.result.find(s=>s.name==='ci-public-agent')?.id;
    hiddenID=list.result.find(s=>s.name==='ci-hidden-agent')?.id;
    if(!publicID||!hiddenID)throw Error('fixture server IDs missing');
    const child=start(registered.secret);
    let lastActive=0;
    await until('V0 Agent host and state report',120000,async()=>{
      if(child.exitCode!==null||child.signalCode!==null)throw Error('Agent exited before reporting');
      const status=await get('/api/v1/server/details?id='+publicID);
      const s=status.result?.[0];
      if(s?.last_active>0 && s?.host?.Platform && s?.status){
        lastActive=s.last_active;
        return true;
      }
      return false;
    });
    console.log('Real V0.20.5 Agent host and state reporting passed');
    await post('/api/monitor',{Name:'ci-local-tcp',Target:'127.0.0.1:18008',
      Type:3,Cover:0,Duration:3,SkipServersRaw:'[]',FailTriggerTasksRaw:'[]',
      RecoverTriggerTasksRaw:'[]',EnableShowInService:'on'});
    await until('tracked TCP monitor result',90000,async()=>{
      const j=await get('/api/v1/monitor/'+publicID);
      return j.code===0 && j.result?.some(item=>item.avg_delay?.length>0 && item.monitor_name==='ci-local-tcp');
    });
    console.log('Real V0.20.5 Agent monitored task receipt passed');
    await stop(child);
    await sleep(1500);
    const reconnected=start(registered.secret);
    await until('V0 Agent reconnect',120000,async()=>{
      if(reconnected.exitCode!==null||reconnected.signalCode!==null)throw Error('reconnected Agent exited');
      const j=await get('/api/v1/server/details?id='+publicID);
      return j.result?.[0]?.last_active>lastActive;
    });
    console.log('Real V0.20.5 Agent reconnect passed');
    const guest=await browser.newContext();
    try {
      for(const route of ['/network/'+hiddenID,'/api/v1/monitor/'+hiddenID]){
        const r=await guest.request.get(base+route);
        if(r.status()!==403)throw Error('hidden server access not denied: '+route+' status '+r.status());
      }
      // Render populated home, service and network pages on all built-in themes.
      for(const theme of ['default','daynight','mdui','hotaru','angel-kanade','server-status']){
        const themeCtx=await browser.newContext();
        const errors=[];
        try {
          await themeCtx.route('**/*',route=>{
            const u=new URL(route.request().url());
            if(u.origin!==base&&!['data:','blob:','about:'].includes(u.protocol)){
              errors.push('external request '+u.origin+u.pathname);return route.abort();
            }
            return route.continue();
          });
          await themeCtx.addCookies([{name:'preferred_theme',value:theme,url:base}]);
          const page=await themeCtx.newPage();
          page.on('pageerror',e=>errors.push(e.stack||String(e)));
          page.on('response',r=>{if(r.status()>=400)errors.push(r.status()+' '+new URL(r.url()).pathname)});
          for(const route of ['/','/service','/network/'+publicID]){
            const r=await page.goto(base+route,{waitUntil:'load',timeout:20000});
            if(!r||r.status()!==200)throw Error('populated page status: '+theme+' '+route);
            await page.waitForTimeout(1200);
          }
          if(errors.length)throw Error(theme+': '+errors.join('; '));
          console.log('Populated theme passed: '+theme);
        }finally{await themeCtx.close();}
      }
    }finally{await guest.close();}
    await runStreamSmoke(ctx,browser,base,publicID,work,post);
  }finally{
    for(const child of children)await stop(child);
    // CI credentials are disposable; do not publish Agent log or its config.
    fs.rmSync(work,{recursive:true,force:true});
  }
}
module.exports={runAgentSmoke};
