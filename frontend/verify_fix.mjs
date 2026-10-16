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
const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });
page.on('console', m => { if (m.type()==='error') console.log('PAGE ERR:', m.text().slice(0,200)); });

await page.route('**/api/**', async (route) => {
  const url = route.request().url();
  const json = (obj) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(obj) });
  if (url.includes('/api/projects/current')) return json({ name: 'Demo', language: 'en' });
  if (url.includes('/api/config')) return json({ story: {} });
  if (url.includes('/api/progress')) return json({});
  if (url.includes('/api/settings')) return json({});
  if (url.includes('/api/chat/sessions')) return json([]);
  if (url.includes('/api/status')) return json({ running: false });
  if (url.includes('/api/novel-params')) return json({});
  return json({});
});

await page.goto('http://127.0.0.1:4179/#novel-params', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(2500);

// 1) Labels present?
const text = await page.evaluate(() => document.body.innerText);
for (const label of ['Target audience','Romance Level','Sexual Content','Gore Level','World Darkness & Cruelty','Gender lean']) {
  console.log('LABEL', JSON.stringify(label), ':', text.includes(label) ? 'OK' : 'MISSING');
}

// 2) Double labels? count field-labels matching each control's aria-label
const dupes = await page.evaluate(() => {
  const groups = [...document.querySelectorAll('.seg-group')];
  return groups.map(g => {
    const cell = g.closest('div').parentElement.querySelector(':scope > .field-label');
    return { aria: g.getAttribute('aria-label'), hasFieldLabel: !!cell };
  });
});
console.log('CONTROLS:', JSON.stringify(dupes, null, 1));

// 3) Divider visibility: measure ::before line for every seg-divider
const dividers = await page.evaluate(() => {
  return [...document.querySelectorAll('[class*="seg-divider"]')].map(d => {
    const cs = getComputedStyle(d, '::before');
    const r = d.getBoundingClientRect();
    return {
      dividerW: r.width, dividerH: r.height,
      lineW: cs.width, lineBg: cs.backgroundColor, content: cs.content,
    };
  });
});
console.log('DIVIDERS:', JSON.stringify(dividers, null, 1));

// 4) Target audience pill alignment: click segment index 3 and compare pill rect vs button rect
const align = await page.evaluate(async () => {
  const group = [...document.querySelectorAll('.seg-group')].find(g => g.getAttribute('aria-label') === 'Target audience');
  if (!group) return 'no target-audience group';
  const btns = [...group.querySelectorAll('[class*="seg-btn"]')];
  const out = [];
  for (let i = 0; i < btns.length; i++) {
    btns[i].click();
    await new Promise(r => setTimeout(r, 350)); // pill transition 200ms
    const track = group.querySelector('[class*="seg-track"]');
    const pill = group.querySelector('[class*="seg-pill"]');
    const tr = track.getBoundingClientRect(), pr = pill.getBoundingClientRect(), br = btns[i].getBoundingClientRect();
    out.push({
      i, label: btns[i].textContent.trim(),
      pillLeftRelTrack: +(pr.left - tr.left).toFixed(2),
      btnLeftRelTrack: +(br.left - tr.left).toFixed(2),
      pillW: +pr.width.toFixed(2), btnW: +br.width.toFixed(2),
      trackInnerW: +(tr.width - 2).toFixed(2), sumBtns: +btns.reduce((a,b)=>a+b.getBoundingClientRect().width,0).toFixed(2),
    });
  }
  return out;
});
console.log('PILL ALIGNMENT (target audience):', JSON.stringify(align, null, 1));

await page.screenshot({ path: '/tmp/config_np.png', fullPage: true });
// zoomed screenshot of the segmented area
const el = await page.$('.seg-group');
if (el) await el.scrollIntoViewIfNeeded();
await page.screenshot({ path: '/tmp/config_np_view.png' });
await browser.close(); srv.close();
console.log('DONE');
