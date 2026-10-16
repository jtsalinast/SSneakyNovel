// Verifies the "smart-field visual system" in app.css: plain inputs/textareas/
// selects must compute the SAME closed/hover/focus/disabled styles as the
// SmartSelect trigger box (.smart-select-root [role="combobox"]).
import { chromium } from 'playwright';
import http from 'http';
import fs from 'fs';
import path from 'path';

const MIME = { '.html':'text/html', '.js':'text/javascript', '.css':'text/css', '.webp':'image/webp' };
const server = http.createServer((req,res)=>{
  let p = req.url.split('?')[0];
  if (p === '/') p = '/index.html';
  const f = path.join('dist', p);
  if (fs.existsSync(f) && fs.statSync(f).isFile()) {
    res.writeHead(200, {'Content-Type': MIME[path.extname(f)] || 'application/octet-stream'});
    res.end(fs.readFileSync(f));
  } else { res.writeHead(404); res.end('nf'); }
});
await new Promise(r=>server.listen(5198,r));

const browser = await chromium.launch({ executablePath: '/root/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome' });
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
page.on('pageerror', e => console.log('PAGE ERR:', e.message));
await page.goto('http://localhost:5198/', { waitUntil: 'load' });
await page.waitForTimeout(1200);

const PROPS = ['minHeight','borderRadius','borderTopWidth','borderTopStyle','backgroundColor','color','transitionDuration','transitionProperty','opacity','cursor','boxShadow','fontSize','fontWeight'];

const results = await page.evaluate(async (PROPS) => {
  // Build a lab with one SmartSelect-like box + real .input/.textarea/.select.
  const lab = document.createElement('div');
  lab.innerHTML = `
    <div class="smart-select-root group relative w-full" style="width:300px">
      <div id="ssBox" role="combobox" tabindex="-1" aria-haspopup="listbox"
        class="min-h-[36px] border rounded-lg pl-3 pr-1 py-1 flex items-center gap-1 transition-all duration-150
               border-neutral-200/80 dark:border-base-300 bg-neutral-50/50 dark:bg-base-200/50 hover:bg-neutral-50 dark:hover:bg-base-200 hover:border-neutral-300 dark:hover:border-base-300">
        <input id="ssInput" class="ss-input flex-1 min-w-0 border-none outline-none text-xs font-medium text-neutral-800 dark:text-base-content bg-transparent py-1 pl-1 placeholder:text-neutral-400 focus:outline-none focus:ring-0" placeholder="hint"/>
      </div>
    </div>
    <input id="plainInput" class="input input-sm w-full" placeholder="hint" style="width:300px"/>
    <textarea id="plainArea" class="textarea textarea-sm w-full h-16" placeholder="hint" style="width:300px"></textarea>
    <select id="plainSelect" class="select select-sm w-full" style="width:300px"><option>a</option></select>
    <input id="disabledInput" class="input input-sm w-full" disabled style="width:300px"/>
  `;
  document.body.appendChild(lab);
  const get = (sel) => {
    const el = document.querySelector(sel);
    const cs = getComputedStyle(el);
    const o = {};
    for (const p of PROPS) o[p] = cs[p];
    return o;
  };
  const out = { theme: document.documentElement.getAttribute('data-theme') || document.body.closest('[data-theme]')?.getAttribute('data-theme') || '?', closed: {}, hover: {}, focus: {} };
  for (const [k, sel] of Object.entries({ ssBox:'#ssBox', input:'#plainInput', area:'#plainArea', select:'#plainSelect' })) out.closed[k] = get(sel);
  out.disabled = get('#disabledInput');
  // Focus parity: focus the smart INPUT inside the box vs the plain input.
  document.querySelector('#ssInput').focus();
  out.focus.ssInner = get('#ssInput');
  document.querySelector('#plainInput').focus();
  out.focus.plain = get('#plainInput');
  lab.remove();
  return out;
}, PROPS);

console.log('theme:', results.theme);
const cmp = (a, b, label, keys) => {
  const diffs = keys.filter(k => String(a[k]) !== String(b[k]));
  console.log(`[${label}] ${diffs.length ? 'DIFFS: ' + diffs.map(k=>`${k}: ${b[k]} vs ss ${a[k]}`).join(' | ') : 'MATCH'}`);
};
const KEYS = ['minHeight','borderTopWidth','borderTopStyle','backgroundColor','transitionDuration','opacity','fontSize','fontWeight','color'];
cmp(results.closed.ssBox, results.closed.input, 'closed input', KEYS);
cmp(results.closed.ssBox, results.closed.select, 'closed select', KEYS);
console.log('closed area bg/minH:', results.closed.area.backgroundColor, results.closed.area.minHeight);
console.log('disabled opacity/cursor:', results.disabled.opacity, results.disabled.cursor);
console.log('focus plain boxShadow:', results.focus.plain.boxShadow);
console.log('focus plain color/bg:', results.focus.plain.color, results.focus.plain.backgroundColor);

// Screenshot of Config-ish form area for eyeballing
await page.screenshot({ path: 'field_parity.png', fullPage: false });
await browser.close();
server.close();
