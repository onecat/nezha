const { chromium } = require('playwright');
const fs = require('node:fs');
const base = 'http://127.0.0.1:18008';
const results = [];
(async () => {
  const browser = await chromium.launch({headless:true});
  try {
    for (const theme of ['default','daynight','mdui','hotaru','angel-kanade','server-status']) {
      const ctx = await browser.newContext();
      const errors = [], external = [];
      await ctx.route('**/*', route => {
        const url = new URL(route.request().url());
        if (url.origin !== base && !['data:','blob:','about:'].includes(url.protocol)) {
          external.push(url.origin + url.pathname);
          return route.abort();
        }
        return route.continue();
      });
      await ctx.addCookies([{name:'preferred_theme',value:theme,url:base}]);
      const page = await ctx.newPage();
      page.on('pageerror', error => errors.push(error.stack || String(error)));
      page.on('console', msg => { if (msg.type()==='error' && !msg.text().includes('net::ERR_BLOCKED_BY_CLIENT')) errors.push(msg.text()); });
      page.on('response', response => {
        if (response.status() >= 400) errors.push(response.status()+' '+new URL(response.url()).pathname);
      });
      try {
        for (const path of ['/','/service']) {
          const response=await page.goto(base+path,{waitUntil:'load',timeout:20000});
          if (!response || response.status() !== 200) throw Error('unexpected status '+path);
          await page.waitForTimeout(path==='/' ? 2500 : 500);
        }
      } catch(error) { errors.push(String(error)); }
      results.push({theme,errors,external});
      await ctx.close();
    }
    // Test the exact served asset, not a separately installed Vue package.
    const securityCtx=await browser.newContext();
    const securityPage=await securityCtx.newPage();
    const securityErrors=[];
    try {
      await securityPage.goto(base+'/login');
      const result=await securityPage.evaluate(() => {
        const V=window.Vue;
        if (!V || V.version!=='2.7.16' || typeof V.compile!=='function') throw Error('unexpected Vue compiler');
        const valid=V.compile('<div><textarea>plain text</textarea><span>{{label}}</span></div>');
        const vm=new V({data:{label:'ok'},render:valid.render,staticRenderFns:valid.staticRenderFns}).$mount();
        if(vm.$el.querySelector('textarea').value!=='plain text' || vm.$el.querySelector('span').textContent!=='ok') throw Error('plain-text parser regression');
        // Malformed plain-text content must be processed in bounded time.
        const start=performance.now();
        V.compile('<div><textarea>'+'x'.repeat(100000));
        const elapsed=performance.now()-start;
        if(elapsed>1500) throw Error('Vue compiler time budget exceeded: '+elapsed+' ms');
        return {version:V.version,elapsed};
      });
      console.log('Vue compiler security test',JSON.stringify(result));
      await securityCtx.addCookies([{name:'preferred_theme',value:'server-status',url:base}]);
      await securityPage.goto(base+'/');
      const plugins=await securityPage.evaluate(()=>{
        const fn=window.jQuery.fn;
        return Object.fromEntries(['modal','collapse','dropdown','button','tooltip','popover'].map(k=>[k,typeof fn[k]]));
      });
      for(const name of ['modal','collapse','dropdown']) if(plugins[name]!=='function') throw Error('missing plugin '+name);
      for(const name of ['button','tooltip','popover']) if(plugins[name]!=='undefined') throw Error('unwanted plugin '+name);
      console.log('Bootstrap subset',JSON.stringify(plugins));
    } catch(e){securityErrors.push(e.stack||String(e));}
    results.push({theme:'frontend-security',errors:securityErrors,external:[]});
    await securityCtx.close();
    const ctx=await browser.newContext();
    const page=await ctx.newPage();
    const errors=[], external=[];
    await ctx.route('**/*', route => {
      if (new URL(route.request().url()).origin!==base) { external.push(route.request().url()); return route.abort(); }
      return route.continue();
    });
    page.on('pageerror', e=>errors.push(e.stack || String(e)));
    page.on('response', response => { if(response.status()>=400) errors.push(response.status()+' '+new URL(response.url()).pathname); });
    try {
      await page.goto(base+'/login');
      await page.locator('input[name=username]').fill('smoke');
      await page.locator('input[name=password]').fill(process.env.SMOKE_PASSWORD);
      await Promise.all([page.waitForURL('**/server',{timeout:15000}),page.locator('button[type=submit]').click()]);
      if (new URL(page.url()).pathname!=='/server') throw Error('login failed');
    } catch(e){ errors.push(String(e)); }
    results.push({theme:'dashboard-login',errors,external});
    await ctx.close();
  } finally {
    await browser.close();
    fs.writeFileSync('browser-smoke.json',JSON.stringify(results,null,2));
    console.log(JSON.stringify(results,null,2));
  }
  if(results.some(r=>r.errors.length||r.external.length)) process.exitCode=1;
})().catch(e=>{console.error(e);process.exitCode=1;});
