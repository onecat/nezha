const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');

async function runStreamSmoke(ctx,browser,base,id,work,post) {
  const page=await ctx.newPage();
  const errors=[];
  const outside=[];
  await page.route('**/*',route=>{
    const url=new URL(route.request().url());
    if(url.origin!==base&&!['data:','blob:','about:'].includes(url.protocol)){
      outside.push(url.origin+url.pathname);
      return route.abort();
    }
    return route.continue();
  });
  page.on('pageerror',e=>errors.push(e.stack||String(e)));
  page.on('dialog',d=>d.dismiss().catch(()=>{}));
  // Submit through the real browser, with the normal authenticated form.
  async function open(route,fields) {
    await page.goto(base+'/server');
    await Promise.all([
      page.waitForURL(base+route,{timeout:20000}),
      page.evaluate(({route,fields})=>{
        const form=document.createElement('form');
        form.method='POST';form.action=route;
        for(const [name,value] of Object.entries(fields)){
          const input=document.createElement('input');
          input.type='hidden';input.name=name;input.value=String(value);form.appendChild(input);
        }
        document.body.appendChild(form);form.submit();
      },{route,fields})
    ]);
    await page.waitForFunction(()=>typeof socket!=='undefined'&&socket.readyState===WebSocket.OPEN,{},{timeout:20000});
  }
  try {
    await open('/terminal',{ID:id});
    // Only this fixed harmless command runs on the disposable CI Agent.
    await page.evaluate(()=>socket.send("printf '%s%s\\n' 'NEZHA_' 'CI_OK'\n"));
    await page.waitForFunction(()=>{
      const buffer=term.buffer.active;
      for(let i=0;i<buffer.length;i++){
        if(buffer.getLine(i)?.translateToString(true).includes('NEZHA_CI_OK'))return true;
      }
      return false;
    },{},{timeout:20000});
    console.log('Real V0.20.5 terminal round trip passed');
    const terminalURL=await page.evaluate(()=>socket.url);
    const guest=await browser.newContext();
    try {
      const response=await guest.request.get(terminalURL.replace(/^ws/,'http'));
      if(response.status()!==403)throw Error('guest terminal access not denied: '+response.status());
    }finally{await guest.close();}
    await page.evaluate(()=>{socket.onclose=null;socket.close();});
    // Upload a deterministic file spanning two protocol chunks.
    await open('/file',{id});
    const filename='ci-stream-test.bin';
    const bytes=Buffer.alloc(1024*1024+257);
    for(let i=0;i<bytes.length;i++)bytes[i]=i%251;
    const expected=crypto.createHash('sha256').update(bytes).digest('hex');
    const dest=path.join(work,filename);
    await page.evaluate(({directory,name})=>{
      currentPath=directory+'/';
      const data=new Uint8Array(1024*1024+257);
      for(let i=0;i<data.length;i++)data[i]=i%251;
      isUpCompleted=false;
      return uploadFile(new File([data],name));
    },{directory:work,name:filename});
    await page.waitForFunction(()=>Array.from(document.querySelectorAll('#file-list .file-item')).some(el=>el.textContent.includes('ci-stream-test.bin')),{},{timeout:30000});
    const uploaded=crypto.createHash('sha256').update(fs.readFileSync(dest)).digest('hex');
    if(uploaded!==expected)throw Error('file upload checksum mismatch');
    console.log('Real V0.20.5 multi-chunk file upload passed');
    const [download]=await Promise.all([
      page.waitForEvent('download',{timeout:30000}),
      page.evaluate(({directory,name})=>{fileName=name;downloadFile(directory+'/'+name);},{directory:work,name:filename})
    ]);
    const downloaded=crypto.createHash('sha256').update(fs.readFileSync(await download.path())).digest('hex');
    if(downloaded!==expected)throw Error('file download checksum mismatch');
    await download.delete();
    await page.evaluate(()=>socket.close());
    console.log('Real V0.20.5 multi-chunk file download passed');
    // NAT points exclusively to a local disposable HTTP fixture.
    const backend=http.createServer((req,res)=>{
      res.writeHead(200,{'Content-Type':'text/plain','Connection':'close'});
      res.end('nezha-ci-nat-ok');
    });
    await new Promise((resolve,reject)=>{backend.once('error',reject);backend.listen(0,'127.0.0.1',resolve);});
    try {
      await post('/api/nat',{Name:'ci-nat',Domain:'nezha-ci-nat.invalid',Host:'127.0.0.1:'+backend.address().port,ServerID:id});
      for(let i=0;i<5;i++){
        const body=await new Promise((resolve,reject)=>{
          const req=http.get(base+'/',{headers:{Host:'nezha-ci-nat.invalid',Connection:'close'},timeout:15000},res=>{
            let data='';res.setEncoding('utf8');res.on('data',chunk=>data+=chunk);
            res.on('end',()=>res.statusCode===200?resolve(data):reject(Error('NAT status '+res.statusCode)));
            res.on('error',reject);
          });
          req.on('timeout',()=>req.destroy(Error('NAT timeout')));
          req.on('error',reject);
        });
        if(body!=='nezha-ci-nat-ok')throw Error('NAT body mismatch');
      }
      console.log('Real V0.20.5 NAT five sequential HTTP round trips passed');
    }finally{backend.closeAllConnections();await new Promise(resolve=>backend.close(resolve));}
    if(errors.length||outside.length)throw Error(errors.concat(outside).join('; '));
  }finally{await page.close();}
}
module.exports={runStreamSmoke};
