import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });

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
  if (url.includes('/api/events') || url.includes('sse')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
  return json({});
});

await page.goto('http://127.0.0.1:4173/', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(3000);
const count = await page.locator('.seg-group').count();
console.log('seg-group count:', count);
for (let i=0;i<count;i++) {
  const label = await page.locator('.seg-group').nth(i).locator('..').locator('.field-label').first().textContent().catch(()=>'?');
  const segs = await page.locator('.seg-group').nth(i).locator('.seg-btn').count();
  console.log(i, JSON.stringify(label), 'segments:', segs);
}
const info = await page.evaluate(() => {
  const out = [];
  document.querySelectorAll('.seg-divider').forEach(d => {
    const r = d.getBoundingClientRect();
    const cs = getComputedStyle(d, '::before');
    out.push({ w: r.width, h: Math.round(r.height), beforeW: cs.width, beforeH: cs.height, bg: cs.backgroundColor });
  });
  return out;
});
console.log('dividers:', JSON.stringify(info));
const pads = await page.evaluate(() => Array.from(document.querySelectorAll('.seg-btn')).slice(0,4).map(b => getComputedStyle(b).paddingLeft + '/' + getComputedStyle(b).paddingRight));
console.log('seg-btn padding:', pads.join(', '));
for (let i=0;i<count;i++) {
  await page.locator('.seg-group').nth(i).scrollIntoViewIfNeeded();
  await page.waitForTimeout(150);
  await page.locator('.seg-group').nth(i).screenshot({ path: `/tmp/seg_${i}.png` });
}
await browser.close();
