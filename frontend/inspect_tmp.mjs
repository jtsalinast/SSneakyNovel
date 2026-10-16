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
await new Promise(r=>srv.listen(4192,r));
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });
await page.route('**/api/**', async (route) => {
  const url = route.request().url();
  const json = (obj) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(obj) });
  if (url.includes('/api/projects/current')) return json({ name: 'Demo', language: 'en' });
  if (url.includes('/api/config')) return json({ story: {} });
  if (url.includes('/api/chat/sessions')) return json([]);
  if (url.includes('/api/events') || url.includes('sse')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
  return json({});
});
await page.goto('http://127.0.0.1:4192/', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(1500);
await page.locator('#workspace-navigation button', { hasText: /Novel parameters/i }).first().click();
await page.waitForTimeout(1200);
console.log(await page.evaluate(() => {
  const g = document.querySelector('.seg-group');
  const chain = [];
  let el = g;
  for (let i=0;i<4 && el;i++){ chain.push(el.tagName+'.'+(typeof el.className==='string'?el.className:'')); el = el.parentElement; }
  const cell = g.closest('div').parentElement; // walk up to the col-span div
  return JSON.stringify({chain, cellHTMLStart: cell.outerHTML.slice(0,600)}, null, 1);
}));
await browser.close(); srv.close();
