import { chromium } from 'playwright';
import http from 'http';
import fs from 'fs';
import path from 'path';

const MIME = { '.html':'text/html', '.js':'text/javascript', '.css':'text/css' };
const server = http.createServer((req,res)=>{
  let p = req.url.split('?')[0];
  if (p === '/') p = '/index.html';
  const f = path.join('dist', p);
  if (fs.existsSync(f) && fs.statSync(f).isFile()) {
    res.writeHead(200, {'Content-Type': MIME[path.extname(f)] || 'application/octet-stream'});
    res.end(fs.readFileSync(f));
  } else { res.writeHead(404); res.end('nf'); }
});
await new Promise(r=>server.listen(5199,r));

const browser = await chromium.launch({ executablePath: '/root/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome' });
const page = await browser.newPage();
page.on('pageerror', e => console.log('PAGE ERR:', e.message));
await page.goto('http://localhost:5199/', { waitUntil: 'load' });
await page.waitForTimeout(1500);
console.log('BODY:', JSON.stringify(await page.evaluate(()=>document.body.innerText.slice(0,300))));
await browser.close();
server.close();
