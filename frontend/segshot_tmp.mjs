import { chromium } from 'playwright';
import { createServer } from 'http';
import { readFileSync, existsSync } from 'fs';
import { join, extname } from 'path';

const dist = '/workspace/frontend/dist';
const types = {'.html':'text/html','.js':'text/javascript','.css':'text/css','.webp':'image/webp'};
const srv = createServer((req,res)=>{
  let p = req.url.split('?')[0]; if (p==='/') p='/index.html';
  const f = join(dist,p);
  if (existsSync(f)) { res.setHeader('Content-Type', types[extname(f)]||'application/octet-stream'); res.end(readFileSync(f)); }
  else { res.setHeader('Content-Type','text/html'); res.end(readFileSync(join(dist,'index.html'))); }
});
await new Promise(r=>srv.listen(4173,r));

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
page.on('console', m => { if (m.type()==='error') console.log('PAGE ERR:', m.text()); });
await page.goto('http://127.0.0.1:4173/', { waitUntil: 'networkidle' }).catch(e=>console.log('nav:',e.message));
await page.waitForTimeout(800);

// Try to reach the Config page — inspect app state first
const html = await page.content();
console.log('has seg-group:', html.includes('seg-group'));
console.log('body text snippet:', (await page.evaluate(()=>document.body.innerText)).slice(0,300).replace(/\n/g,' | '));
await page.screenshot({ path: '/tmp/app_initial.png', fullPage: false });
await browser.close(); srv.close();
