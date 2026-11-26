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
      page.on('pageerror', error => errors.push(String(error)));
      page.on('response', response => {
        if (response.status() >= 400) errors.push(response.status()+' '+new URL(response.url()).pathname);
      });
      try {
        for (const path of ['/','/service']) {
          const response=await page.goto(base+path,{waitUntil:'load',timeout:20000});
          if (!response || response.status() !== 200) throw Error('unexpected status '+path);
          await page.waitForTimeout(500);
        }
      } catch(error) { errors.push(String(error)); }
      results.push({theme,errors,external});
      await ctx.close();
    }
    const ctx=await browser.newContext();
    const page=await ctx.newPage();
    const errors=[], external=[];
    await ctx.route('**/*', route => {
      if (new URL(route.request().url()).origin!==base) { external.push(route.request().url()); return route.abort(); }
      return route.continue();
    });
    page.on('pageerror', e=>errors.push(String(e)));
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
