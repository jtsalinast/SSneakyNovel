import { writable } from 'svelte/store';

// The shell renders the ribbon from whatever page currently owns it. Pages
// register their steps reactively; pages without steps fall back to a single
// step derived from the navigation map in App.svelte.
export const ribbon = writable(null);

export function setRibbon(page, steps, active, onSelect) {
  ribbon.set({ page, steps, active, onSelect });
}
