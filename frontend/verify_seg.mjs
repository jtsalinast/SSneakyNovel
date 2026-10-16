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
await new Promise(r=>srv.listen(4193,r));
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });
page.on('console', m => { if (m.type()==='error') console.log('PAGE ERR:', m.text().slice(0,200)); });
await page.route('**/api/**', async (route) => {
  const url = route.request().url();
  const json = (obj) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(obj) });
  if (url.includes('/api/projects/current')) return json({ name: 'Demo', language: 'en' });
  if (url.includes('/api/config')) return json({ story: {} });
  if (url.includes('/api/chat/sessions')) return json([]);
  if (url.includes('/api/events') || url.includes('sse')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
  return json({});
});
await page.goto('http://127.0.0.1:4193/', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(1500);
await page.locator('#workspace-navigation button', { hasText: /Novel parameters/i }).first().click();
await page.waitForTimeout(1200);

// 1) Double-label check: field-labels inside each grid cell (col-span div)
console.log('CELLS:', await page.evaluate(() => JSON.stringify(
  Array.from(document.querySelectorAll('.seg-group')).map(g => {
    const cell = g.closest('.col-span-2') || g.parentElement.parentElement;
    return { label: cell.querySelector(':scope > .field-label')?.textContent.trim() ?? null,
             labelsInCell: cell.querySelectorAll(':scope > .field-label').length };
  }))));

// 2) Alignment per control per segment, measured after transitions settle
for (let i = 0; i < await page.locator('.seg-group').count(); i++) {
  const g = page.locator('.seg-group').nth(i);
  const segs = await g.locator('.seg-btn').count();
  const align = [];
  for (let s = 0; s < segs; s++) {
    await g.locator('.seg-btn').nth(s).click();
    await page.waitForTimeout(350);
    align.push(await page.evaluate(({gi, si}) => {
      const grp = document.querySelectorAll('.seg-group')[gi];
      const btn = grp.querySelectorAll('.seg-btn')[si];
      const pill = grp.querySelector('.seg-pill');
      const b = btn.getBoundingClientRect(), p = pill.getBoundingClientRect();
      return { dx: +(p.left - b.left).toFixed(2), dw: +(p.width - b.width).toFixed(2) };
    }, { gi: i, si: s }));
    if (s === Math.min(2, segs-1)) await g.screenshot({ path: `/tmp/seg_${i}.png` }).catch(()=>{});
  }
  const label = await page.evaluate((gi) => {
    const grp = document.querySelectorAll('.seg-group')[gi];
    const cell = grp.closest('.col-span-2');
    return cell?.querySelector(':scope > .field-label')?.textContent.trim();
  }, i);
  console.log(`#${i} [${label}]`, JSON.stringify(align));
}
await browser.close(); srv.close();
