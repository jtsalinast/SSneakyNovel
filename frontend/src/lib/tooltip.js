// Lightweight tooltip helper (daisyUI-style): shows a small floating bubble
// near the hovered element and hides it on leave/click/scroll. Used for the
// novel-parameter fields (subgenre presets, narrative structures, etc.) so
// each option explains itself without cluttering the UI.
let tipEl = null;

function ensureTip() {
  if (tipEl) return tipEl;
  tipEl = document.createElement('div');
  tipEl.className = 'fixed z-[999] max-w-xs rounded-lg border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs shadow-lg pointer-events-none';
  tipEl.style.display = 'none';
  document.body.appendChild(tipEl);
  return tipEl;
}

export function showTip(e, text) {
  if (!text) return;
  const tip = ensureTip();
  tip.textContent = text;
  tip.style.display = 'block';
  const r = e.currentTarget.getBoundingClientRect ? e.currentTarget.getBoundingClientRect() : { top: e.clientY, bottom: e.clientY, left: e.clientX, right: e.clientX };
  const tw = tip.offsetWidth, th = tip.offsetHeight;
  let x = Math.min(Math.max(8, r.left), window.innerWidth - tw - 8);
  let y = r.bottom + 6;
  if (y + th > window.innerHeight - 8) y = r.top - th - 6; // flip above when no room below
  tip.style.left = x + 'px';
  tip.style.top = Math.max(8, y) + 'px';
}

export function hideTip() {
  if (tipEl) tipEl.style.display = 'none';
}

// Svelte action: use:tooltip={"Some explanation"} — works on any focusable element.
export function tooltip(node, text) {
  const over = (e) => showTip({ currentTarget: node }, text);
  const out = () => hideTip();
  node.addEventListener('mouseenter', over);
  node.addEventListener('mouseleave', out);
  node.addEventListener('focus', over);
  node.addEventListener('blur', out);
  node.addEventListener('click', out);
  node.addEventListener('keydown', (e) => { if (e.key === 'Escape') out(); });
  return {
    update(newText) { text = newText; },
    destroy() {
      node.removeEventListener('mouseenter', over);
      node.removeEventListener('mouseleave', out);
      node.removeEventListener('focus', over);
      node.removeEventListener('blur', out);
      node.removeEventListener('click', out);
      hideTip();
    },
  };
}
