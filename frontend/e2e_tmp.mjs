import { chromium } from 'playwright';

const b = await chromium.launch();
const page = await b.newPage({ viewport: { width: 1400, height: 900 } });
page.on('pageerror', e => console.log('PAGEERROR:', e.message));
await page.goto('http://localhost:49101/#config', { waitUntil: 'networkidle' });
await page.waitForTimeout(500);
console.log('URL:', page.url());
const txt = await page.evaluate(()=>document.body.innerText);
console.log('BODY:', txt.slice(0,600).replace(/\n+/g,' | '));
const combos = await page.$$('[role="combobox"]');
console.log('COMBOBOX COUNT:', combos.length);
for (let i=0;i<Math.min(combos.length,10);i++) console.log(i, (await combos[i].textContent()).trim().slice(0,40));
await page.screenshot({ path: '/tmp/shot_config.png' });
await b.close();
