import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 1200 } });
page.on('console', m => { if (m.type()==='error') console.log('PAGE ERR:', m.text().slice(0,300)); });
page.on('requestfailed', r => console.log('REQFAIL:', r.url(), r.failure()?.errorText));

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

await page.goto('http://127.0.0.1:4173/', { waitUntil: 'domcontentloaded' }).catch(e=>console.log('nav:',e.message));
await page.waitForTimeout(3000);
console.log('URL now:', page.url());
console.log('body text:', (await page.evaluate(()=>document.body.innerText)).slice(0,400).replace(/\n/g,' | '));
await page.screenshot({ path: '/tmp/state.png' });
await browser.close();
