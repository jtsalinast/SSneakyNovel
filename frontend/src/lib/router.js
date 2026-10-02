import { writable } from 'svelte/store';

// Known workspace pages. An unknown hash (typo, stale bookmark, leftover value
// after a refactor) used to render an empty main area — only the top bar, the
// nav rail and the chat were visible until the user pressed F5. Normalize any
// unknown page back to a valid one so the content always renders.
const KNOWN_PAGES = ['config', 'novel-params', 'lore', 'outline', 'writing', 'proofread', 'foreshadows', 'memory', 'relations', 'skills'];

function normalizePage(raw) {
  const p = (raw || '').trim();
  return KNOWN_PAGES.includes(p) ? p : 'config';
}

export const currentPage = writable(normalizePage(window.location.hash.slice(1)));

window.addEventListener('hashchange', () => {
  const page = normalizePage(window.location.hash.slice(1));
  // Keep the URL in sync when we normalized an invalid hash, otherwise the
  // browser would keep showing the broken route.
  if ('#' + page !== window.location.hash) {
    window.location.hash = '#' + page;
    return; // the hashchange listener will set the store on the way back
  }
  currentPage.set(page);
});

export function navigate(page) {
  window.location.hash = '#' + page;
}
