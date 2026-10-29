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
await new Promise(r=>srv.listen(4179,r));
const browser = await chromium.launch();
for (const theme of ['sneakystyle-light','sneakystyle']) {
  const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    const json = (obj) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(obj) });
    if (url.includes('/api/projects/current')) return json({ name: 'Demo', language: 'en' });
    if (url.includes('/api/config')) return json({ story: { gender_bias: 'balanced', world_darkness: 'gritty', target_audience: 'ya' } });
    return json({});
  });
  await page.goto('http://127.0.0.1:4179/#novel-params', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(1500);
  await page.evaluate((t) => document.documentElement.setAttribute('data-theme', t), theme);
  await page.waitForTimeout(500);
  const group = await page.$('.seg-group');
  await group.scrollIntoViewIfNeeded();
  // screenshot the whole story grid area (parent of first few seg-groups)
  const box = await page.evaluate(() => {
    const gs = [...document.querySelectorAll('.seg-group')];
    const r0 = gs[0].getBoundingClientRect();
    const rn = gs[gs.length-1].getBoundingClientRect();
    return { x: Math.max(0, r0.left - 30), y: Math.max(0, window.scrollY + r0.top - 60), w: Math.min(1500, rn.right - r0.left + 80), h: rn.bottom - r0.top + 120 };
  });
  await page.screenshot({ path: `/tmp/seg_${theme}.png`, clip: { x: box.x, y: box.y, width: box.w, height: Math.min(box.h, 1100) } });
  await page.close();
}
await browser.close(); srv.close(); console.log('SHOTS OK');
