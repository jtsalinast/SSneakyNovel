import { readable } from 'svelte/store';

// Known workspace pages. An unknown hash (typo, stale bookmark, leftover value
// after a refactor) used to render an empty main area — only the top bar, the
// nav rail and the chat were visible until the user pressed F5. Normalize any
// unknown page back to a valid one so the content always renders.
const KNOWN_PAGES = ['config', 'novel-params', 'lore', 'outline', 'writing', 'proofread', 'foreshadows', 'memory', 'relations', 'skills'];

function normalizePage(raw) {
  const p = (raw || '').trim();
  return KNOWN_PAGES.includes(p) ? p : 'config';
}

// Single source of truth: derive the current page from location.hash and keep
// it in sync with BOTH "hashchange" and "popstate". The old implementation
// used a writable store updated by a single window listener plus imperative
// .set() calls scattered around; some navigation paths (back/forward buttons,
// programmatic hash writes from panels) missed the update and left the main
// area blank (only header + nav rail rendered). A readable store that always
// re-reads location.hash cannot desynchronize.
export const currentPage = readable(normalizePage(window.location.hash.slice(1)), function start(set) {
  const update = () => {
    const page = normalizePage(window.location.hash.slice(1));
    // Keep the URL in sync when we normalized an invalid hash, otherwise the
    // browser would keep showing the broken route. This re-entrant hashchange
    // just re-runs update() with the corrected hash and settles.
    if ('#' + page !== window.location.hash) {
      window.location.replace('#' + page);
    }
    set(page);
  };
  update();
  window.addEventListener('hashchange', update);
  window.addEventListener('popstate', update);
  return function stop() {
    window.removeEventListener('hashchange', update);
    window.removeEventListener('popstate', update);
  };
});

export function navigate(page) {
  const target = '#' + normalizePage(page);
  if (window.location.hash === target) {
    // Same hash: no hashchange event fires; make sure the store agrees anyway.
    currentPage.subscribe(() => {})();
    return;
  }
  window.location.hash = target;
}
