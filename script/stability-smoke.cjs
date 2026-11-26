const fs=require('node:fs');
const {execFileSync}=require('node:child_process');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));

async function runStabilitySmoke(ctx,browser,base,id){
  const pid=Number(process.env.SMOKE_DASHBOARD_PID);
  if(!Number.isSafeInteger(pid)||pid<=0)throw Error('missing Dashboard process ID');
  const hz=Number(execFileSync('getconf',['CLK_TCK'],{encoding:'utf8'}).trim());
  function sample(){
    const status=fs.readFileSync('/proc/'+pid+'/status','utf8');
    const stat=fs.readFileSync('/proc/'+pid+'/stat','utf8');
    const fields=stat.slice(stat.lastIndexOf(')')+2).trim().split(/\s+/);
    return {ms:Date.now(),rssKiB:Number(status.match(/^VmRSS:\s+(\d+)/m)[1]),
      threads:Number(status.match(/^Threads:\s+(\d+)/m)[1]),
      cpuSeconds:(Number(fields[11])+Number(fields[12]))/hz,
      fds:fs.readdirSync('/proc/'+pid+'/fd').length};
  }
  const samples=[];
  let requests=0;
  for(let i=0;i<=60;i++){
    if(i%5===0)samples.push(sample());
    if(i===60)break;
    const responses=await Promise.all(['/','/service','/api/v1/server/details?id='+id,'/api/v1/monitor/'+id].map(route=>ctx.request.get(base+route,{timeout:10000})));
    for(const r of responses){
      try{if(!r.ok())throw Error('stability request status '+r.status());requests++;}finally{await r.dispose();}
    }
    await sleep(1000);
  }
  const first=samples[0],last=samples.at(-1);
  const report={durationSeconds:(last.ms-first.ms)/1000,requests,
    averageCPUCores:(last.cpuSeconds-first.cpuSeconds)/((last.ms-first.ms)/1000),
    rssGrowthKiB:last.rssKiB-first.rssKiB,fdGrowth:last.fds-first.fds,samples};
  fs.writeFileSync('stability-smoke.json',JSON.stringify(report,null,2));
  console.log('Dashboard stability smoke',JSON.stringify(report));
  // Broad regression bounds, not production performance targets.
  if(report.rssGrowthKiB>65536||report.fdGrowth>32)throw Error('resource growth exceeds smoke-test bounds');
}
module.exports={runStabilitySmoke};
